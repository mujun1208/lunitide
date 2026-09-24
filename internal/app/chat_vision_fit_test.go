package app

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
)

func TestFitChatVisionShrinksADesktopScreenshot(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1800, 1000))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 17)
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, src); err != nil {
		t.Fatal(err)
	}
	fitted, mime, ok := fitChatVision(raw.Bytes())
	if !ok || mime != "image/jpeg" || len(fitted) == 0 || len(fitted) > attachmentapp.MaxVisionImageBytes {
		t.Fatalf("fit=%v mime=%s bytes=%d", ok, mime, len(fitted))
	}
	decoded, err := jpeg.Decode(bytes.NewReader(fitted))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() > chatVisionMaxEdge || decoded.Bounds().Dy() > chatVisionMaxEdge {
		t.Fatalf("still too wide: %v", decoded.Bounds())
	}
}

func TestOfficeTemplateCapFitsABatchOfDecks(t *testing.T) {
	if attachmentapp.MaxTemplateFileSize != 500<<20 || attachmentapp.MaxFileSize != 500<<20 {
		t.Fatalf("template cap=%d attachment cap=%d", attachmentapp.MaxTemplateFileSize, attachmentapp.MaxFileSize)
	}
	if got := templateFileTooLargeMessage(); got != "模板附件超过 500 MiB 限制" {
		t.Fatal(got)
	}
}
