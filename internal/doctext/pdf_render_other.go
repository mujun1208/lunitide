//go:build !windows

package doctext

import (
	"context"
	"errors"
)

func RenderPDFPages(ctx context.Context, raw []byte, pages []int) ([]RenderedPDFPage, error) {
	if err := rejectRenderPDFInput(ctx, raw, pages); err != nil {
		return nil, err
	}
	return nil, errors.New("当前平台未装配 PDF 页面渲染")
}
