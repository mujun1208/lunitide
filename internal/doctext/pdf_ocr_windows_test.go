//go:build windows

package doctext

import (
	"context"
	"errors"
	"testing"
)

func TestPDFOCRRejectsInvalidOrCancelledInputBeforeLaunching(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractPDFOCR(ctx, []byte("%PDF-1.4")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ExtractPDFOCR(context.Background(), []byte("not PDF")); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatal(err)
	}
	if _, err := ExtractPDFOCR(context.Background(), make([]byte, MaxInputBytes+1)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatal(err)
	}
}
