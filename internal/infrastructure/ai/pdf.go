package ai

import (
	"fmt"
	"io"

	pdf "rsc.io/pdf"
)

// readPDF 用 rsc.io/pdf 打开 PDF，返回 *pdf.Reader
func readPDF(r io.ReaderAt, size int64) (*pdf.Reader, error) {
	rdr, err := pdf.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open pdf failed: %w", err)
	}
	return rdr, nil
}
