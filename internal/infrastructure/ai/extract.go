package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"gofile/internal/port"
)

const (
	// maxExtractBytes is the budget cap for extracted raw text to prevent OOM on oversized files
	maxExtractBytes = 1 << 20 // 1MB
	// maxSummaryChars is the maximum number of characters sent to the LLM
	maxSummaryChars = 8000
)

// Extracted is the text extraction result
type Extracted struct {
	Text string // Extracted plain text (may be empty, e.g., images/binaries)
}

// Extract reads a file from storage and extracts text (format-aware two-stage pipeline)
//
// Stage 1 dispatches parsers by MIME/extension; stage 2 samples the extracted plain text (head + tail + structural anchors).
// Reading is bounded by the maxExtractBytes budget.
func Extract(ctx context.Context, store port.Storage, filehash, fileName string) (*Extracted, error) {
	mt, ext := detectType(fileName)

	// Read the first maxExtractBytes bytes (for text files, reading from the start yields the content)
	reader, err := store.GetRange(ctx, filehash, 0, maxExtractBytes)
	if err != nil {
		return nil, fmt.Errorf("extract: read file failed: %w", err)
	}
	defer reader.Close()

	limited := io.LimitReader(reader, maxExtractBytes)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("extract: read bytes failed: %w", err)
	}

	var text string
	switch {
	case isTextMime(mt, ext):
		text = string(buf)
	case ext == ".pdf":
		text = extractPDF(buf)
	case ext == ".docx" || ext == ".xlsx" || ext == ".pptx":
		text = extractOffice(buf, ext)
	case isArchiveMime(mt, ext):
		text = extractArchiveList(buf, ext)
	default:
		// Images/audio/video/other binaries: no text extraction, tagging by filename is sufficient
		text = ""
	}

	return &Extracted{
		Text: sampleText(text, fileName),
	}, nil
}

// detectType infers MIME type and extension
func detectType(fileName string) (mime, ext string) {
	ext = "." + strings.TrimPrefix(getExt(fileName), ".")
	mt := mimetype.Lookup(ext)
	if mt != nil {
		return mt.String(), ext
	}
	return "", ext
}

func isTextMime(mt, ext string) bool {
	if mt == "" {
		// Fallback by extension
		switch ext {
		case ".txt", ".md", ".log", ".json", ".xml", ".yaml", ".yml", ".html", ".htm", ".css",
			".js", ".ts", ".go", ".py", ".java", ".rs", ".c", ".cpp", ".swift", ".kt",
			".php", ".rb", ".sh", ".sql", ".conf", ".ini", ".toml", ".env", ".csv":
			return true
		}
		return false
	}
	if strings.HasPrefix(mt, "text/") {
		return true
	}
	switch mt {
	case "application/json", "application/xml", "application/javascript",
		"application/x-yaml", "application/x-javascript":
		return true
	}
	return false
}

func isArchiveMime(mt, ext string) bool {
	if strings.HasPrefix(mt, "application/zip") || strings.HasPrefix(mt, "application/x-tar") ||
		strings.HasPrefix(mt, "application/gzip") {
		return true
	}
	switch ext {
	case ".zip", ".tar", ".gz", ".tgz", ".rar", ".7z":
		return true
	}
	return false
}

// extractPDF extracts PDF text (pure Go library rsc.io/pdf)
func extractPDF(buf []byte) string {
	// rsc.io/pdf requires io.ReaderAt + size
	r := bytes.NewReader(buf)
	pdf, err := readPDF(r, int64(len(buf)))
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for i := 1; i <= pdf.NumPage(); i++ {
		page := pdf.Page(i)
		for _, t := range page.Content().Text {
			sb.WriteString(t.S)
		}
		sb.WriteString("\n")
		if sb.Len() > maxSummaryChars*2 {
			break
		}
	}
	return sb.String()
}

// extractOffice extracts text from docx/xlsx/pptx (zip containers)
func extractOffice(buf []byte, ext string) string {
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return ""
	}
	switch ext {
	case ".docx":
		return extractDocx(zr)
	case ".xlsx":
		target := "xl/sharedStrings.xml"
		for _, f := range zr.File {
			if f.Name == target {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				b, _ := io.ReadAll(rc)
				rc.Close()
				return stripXML(string(b))
			}
		}
	case ".pptx":
		return extractPPTXSlides(zr)
	}
	return ""
}

// extractDocx extracts text from docx (organized by paragraphs, preserving structure)
func extractDocx(zr *zip.Reader) string {
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			// First convert paragraph delimiters to newlines, then strip tags:
			// If stripXML is called first, "</w:p>" would already be stripped and ReplaceAll would never match, losing paragraph structure
			text := strings.ReplaceAll(string(b), "</w:p>", "\n")
			text = stripXML(text)
			return text
		}
	}
	return ""
}

func extractPPTXSlides(zr *zip.Reader) string {
	var sb strings.Builder
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			sb.WriteString(stripXML(string(b)))
			sb.WriteString("\n")
			if sb.Len() > maxSummaryChars*2 {
				break
			}
		}
	}
	return sb.String()
}

// extractArchiveList only reads the file list inside archives (without expanding contents)
func extractArchiveList(buf []byte, ext string) string {
	if ext != ".zip" {
		return ""
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for i, f := range zr.File {
		if i >= 200 {
			sb.WriteString("...(更多)\n")
			break
		}
		sb.WriteString(f.Name)
		sb.WriteString("\n")
	}
	return sb.String()
}

// stripXML strips XML tags and preserves text content
// Handles: tag stripping, CDATA sections, and common XML entity decoding
func stripXML(s string) string {
	var sb strings.Builder
	inTag := false
	inCDATA := false
	i := 0
	runes := []rune(s)
	for i < len(runes) {
		r := runes[i]
		switch {
		case inCDATA:
			if i+2 < len(runes) && runes[i] == ']' && runes[i+1] == ']' && runes[i+2] == '>' {
				inCDATA = false
				i += 3
				continue
			}
			sb.WriteRune(r)
			i++
		case i+8 < len(runes) && string(runes[i:i+9]) == "<![CDATA[":
			inCDATA = true
			i += 9
		case r == '<':
			inTag = true
			i++
		case r == '>':
			inTag = false
			i++
		case !inTag:
			// Decode common XML entities
			if r == '&' && i+3 < len(runes) {
				entity := string(runes[i:min(i+6, len(runes))])
				switch {
				case strings.HasPrefix(entity, "&amp;"):
					sb.WriteByte('&')
					i += 5
				case strings.HasPrefix(entity, "&lt;"):
					sb.WriteByte('<')
					i += 4
				case strings.HasPrefix(entity, "&gt;"):
					sb.WriteByte('>')
					i += 4
				case strings.HasPrefix(entity, "&quot;"):
					sb.WriteByte('"')
					i += 6
				case strings.HasPrefix(entity, "&apos;"):
					sb.WriteByte('\'')
					i += 6
				default:
					sb.WriteRune(r)
					i++
				}
				continue
			}
			sb.WriteRune(r)
			i++
		default:
			i++
		}
	}
	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sampleText samples the extracted plain text (head + tail + structural anchors) to control length sent to the LLM
func sampleText(text, fileName string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) <= maxSummaryChars {
		return text
	}

	const head = 2000
	const tail = 2000

	// Structural anchor sampling
	anchors := extractAnchors(text)

	var sb strings.Builder
	sb.WriteString(text[:head])
	sb.WriteString("\n...\n")
	for _, a := range anchors {
		sb.WriteString(a)
		sb.WriteString("\n")
	}
	sb.WriteString("...\n")
	sb.WriteString(text[len(text)-tail:])

	out := sb.String()
	if len(out) > maxSummaryChars {
		out = out[:maxSummaryChars]
	}
	return out
}

// extractAnchors extracts structural anchors: markdown headings, code definition lines, json keys, and log error lines
func extractAnchors(text string) []string {
	var anchors []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if isAnchorLine(trim) {
			anchors = append(anchors, trim)
			if len(anchors) >= 20 {
				break
			}
		}
	}
	return anchors
}

func isAnchorLine(line string) bool {
	// markdown heading
	if strings.HasPrefix(line, "#") {
		return true
	}
	// code definition line
	for _, kw := range []string{"func ", "function ", "def ", "class ", "type ", "import ", "package ", "interface "} {
		if strings.HasPrefix(line, kw) {
			return true
		}
	}
	// json top-level key
	if strings.HasPrefix(line, `"`) && strings.Contains(line, `":`) {
		return true
	}
	// log error line
	lower := strings.ToLower(line)
	for _, kw := range []string{"error", "fail", "fatal", "panic", "exception"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
