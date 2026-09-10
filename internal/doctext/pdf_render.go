package doctext

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

func formatPDFPageIndexes(pages []int) (string, error) {
	if len(pages) == 0 {
		return "", fmt.Errorf("doctext: no PDF pages to render")
	}
	if len(pages) > 100 {
		return "", ErrBudgetExceeded
	}
	seen := map[int]bool{}
	parts := make([]string, 0, len(pages))
	for _, page := range pages {
		if page < 1 || page > 100 {
			return "", fmt.Errorf("doctext: PDF page %d is outside the render budget", page)
		}
		if seen[page] {
			continue
		}
		seen[page] = true
		parts = append(parts, strconv.Itoa(page))
	}
	return strings.Join(parts, ","), nil
}

func rejectRenderPDFInput(ctx context.Context, raw []byte, pages []int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(raw) > MaxInputBytes {
		return ErrBudgetExceeded
	}
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		return ErrUnsupportedFormat
	}
	_, err := formatPDFPageIndexes(pages)
	return err
}
