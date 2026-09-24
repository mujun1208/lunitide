//go:build windows

package doctext

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestImageOCRScriptAwaitsOpenReadContentType(t *testing.T) {
	script := string(imageOCRScript)
	if !strings.Contains(script, "IRandomAccessStreamWithContentType") {
		t.Fatal("OpenReadAsync must await IRandomAccessStreamWithContentType")
	}
	if strings.Contains(script, "OpenReadAsync()) ([Windows.Storage.Streams.IRandomAccessStream])") {
		t.Fatal("OpenReadAsync must not await the raw IRandomAccessStream interface")
	}
}

func TestExtractImageOCRRejectsInvalidOrCancelledInputBeforeLaunching(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := ExtractImageOCR(ctx, png); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ExtractImageOCR(context.Background(), []byte("not an image")); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatal(err)
	}
	if _, err := ExtractImageOCR(context.Background(), make([]byte, MaxInputBytes+1)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatal(err)
	}
}
