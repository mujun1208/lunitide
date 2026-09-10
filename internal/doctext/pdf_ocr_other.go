//go:build !windows

package doctext

import (
	"context"
	"errors"
)

func ExtractPDFOCR(ctx context.Context, raw []byte) (PDFOCRResult, error) {
	return PDFOCRResult{}, errors.New("当前平台未装配本地 PDF 识别，请提供带文字层的源文件")
}
