package doctext

import (
	"context"
	"errors"
	"testing"
)

func TestRenderPDFPagesRejectsInvalidOrCancelledInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RenderPDFPages(ctx, []byte("%PDF-1.4"), []int{1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled render: %v", err)
	}
	if _, err := RenderPDFPages(context.Background(), []byte("not pdf"), []int{1}); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("non-PDF render: %v", err)
	}
	if _, err := RenderPDFPages(context.Background(), make([]byte, MaxInputBytes+1), []int{1}); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("oversize render: %v", err)
	}
	if _, err := RenderPDFPages(context.Background(), []byte("%PDF-1.4"), nil); err == nil {
		t.Fatal("empty page list must fail before launch")
	}
}
