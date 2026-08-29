package ai

import (
	"fmt"
	"io"

	pdf "rsc.io/pdf"
)

// readPDF opens a PDF with rsc.io/pdf and returns *pdf.Reader
func readPDF(r io.ReaderAt, size int64) (*pdf.Reader, error) {
	rdr, err := pdf.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open pdf failed: %w", err)
	}
	return rdr, nil
}
