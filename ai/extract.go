package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"gofile/storage"
)

const (
	// maxExtractBytes 提取的原始文本预算上限，防止超大文件 OOM
	maxExtractBytes = 1 << 20 // 1MB
	// maxSummaryChars 送给 LLM 的内容最大字符数
	maxSummaryChars = 8000
)

// Extracted 文本提取结果
type Extracted struct {
	Text string // 提取出的纯文本（可能为空，如图片/二进制）
}

// Extract 从存储层读取文件并提取文本（格式感知两级管线）
//
// 第 1 级按 MIME/扩展名分派解析器；第 2 级对解出的纯文本做采样（首尾 + 结构锚点）。
// 读取受 maxExtractBytes 预算限制。
func Extract(ctx context.Context, store storage.Storage, filehash, fileName string) (*Extracted, error) {
	mt, ext := detectType(fileName)

	// 读取前 maxExtractBytes 字节（文本类文件从头读就是内容）
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
		// 图片/音频/视频/其他二进制：不提取文本，按文件名打标签即可
		text = ""
	}

	return &Extracted{
		Text: sampleText(text, fileName),
	}, nil
}

// detectType 推断 MIME 与扩展名
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
		// 按扩展名兜底
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

// extractPDF 提取 PDF 文本（纯 Go 库 rsc.io/pdf）
func extractPDF(buf []byte) string {
	// rsc.io/pdf 需要 io.ReaderAt + size
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

// extractOffice 从 docx/xlsx/pptx（zip 容器）提取文本
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

// extractDocx 从 docx 提取文本（按段落组织，保留结构）
func extractDocx(zr *zip.Reader) string {
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			// 先将段落分隔符转为换行，再剥离标签：
			// 若先 stripXML，"</w:p>" 已被剥掉，ReplaceAll 永远匹配不到，段落结构丢失
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

// extractArchiveList 只读压缩包内文件清单（不展开内容）
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

// stripXML 剥离 XML 标签，保留文本内容
// 处理：标签剥离、CDATA 段、常见 XML 实体解码
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
			// 解码常见 XML 实体
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

// sampleText 对解出的纯文本做采样（首尾 + 结构锚点），控制送给 LLM 的长度
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

	// 结构锚点采样
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

// extractAnchors 抽取结构锚点：markdown 标题、代码定义行、json keys、log 错误行
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
	// markdown 标题
	if strings.HasPrefix(line, "#") {
		return true
	}
	// 代码定义行
	for _, kw := range []string{"func ", "function ", "def ", "class ", "type ", "import ", "package ", "interface "} {
		if strings.HasPrefix(line, kw) {
			return true
		}
	}
	// json top-level key
	if strings.HasPrefix(line, `"`) && strings.Contains(line, `":`) {
		return true
	}
	// log 错误行
	lower := strings.ToLower(line)
	for _, kw := range []string{"error", "fail", "fatal", "panic", "exception"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
