//go:build !windows

package doctext

import (
	"context"
	"errors"
)

func ExtractPDFOCR(ctx context.Context, raw []byte) (PDFOCRResult, error) {
	return PDFOCRResult{}, errors.New("local PDF OCR is unavailable on this platform; provide a text-bearing source")
}
