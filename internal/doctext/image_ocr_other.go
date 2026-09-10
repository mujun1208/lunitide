//go:build !windows

package doctext

import (
	"context"
	"errors"
)

func ExtractImageOCR(ctx context.Context, raw []byte) (PDFOCRResult, error) {
	if err := ctx.Err(); err != nil {
		return PDFOCRResult{}, err
	}
	if len(raw) > MaxInputBytes {
		return PDFOCRResult{}, ErrBudgetExceeded
	}
	if !LooksLikeRasterImage("", "", raw) {
		return PDFOCRResult{}, ErrUnsupportedFormat
	}
	return PDFOCRResult{}, errors.New("当前平台未装配本地图片识别")
}
