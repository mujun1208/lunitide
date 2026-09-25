//go:build windows

package doctext

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestWindowsOCRReadsDrawnChineseAndSkipsABlankShape(t *testing.T) {
	dir := t.TempDir()
	han := filepath.Join(dir, "han.png")
	shape := filepath.Join(dir, "shape.png")
	script := filepath.Join(dir, "draw.ps1")
	body := "Add-Type -AssemblyName System.Drawing\n" +
		"$han = New-Object System.Drawing.Bitmap 520, 180\n" +
		"$g = [System.Drawing.Graphics]::FromImage($han)\n" +
		"$g.Clear([System.Drawing.Color]::White)\n" +
		"$font = New-Object System.Drawing.Font 'Microsoft YaHei', 42\n" +
		"$label = -join ([char]0x6D4B, [char]0x8BD5, [char]0x6C49, [char]0x5B57)\n" +
		"$g.DrawString($label, $font, [System.Drawing.Brushes]::Black, 24, 48)\n" +
		"$han.Save('" + han + "', [System.Drawing.Imaging.ImageFormat]::Png)\n" +
		"$g.Dispose(); $han.Dispose()\n" +
		"$box = New-Object System.Drawing.Bitmap 320, 180\n" +
		"$g2 = [System.Drawing.Graphics]::FromImage($box)\n" +
		"$g2.Clear([System.Drawing.Color]::White)\n" +
		"$g2.FillEllipse([System.Drawing.Brushes]::Orange, 40, 20, 140, 140)\n" +
		"$box.Save('" + shape + "', [System.Drawing.Imaging.ImageFormat]::Png)\n" +
		"$g2.Dispose(); $box.Dispose()\n"
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("powershell", "-NoProfile", "-File", script).CombinedOutput(); err != nil {
		t.Fatalf("draw: %v %s", err, out)
	}
	hanPNG, err := os.ReadFile(han)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExtractImageOCR(context.Background(), hanPNG)
	if err != nil {
		t.Fatal(err)
	}
	var raw strings.Builder
	for _, page := range got.Pages {
		raw.WriteString(page.Text)
	}
	text := strings.ReplaceAll(raw.String(), " ", "")
	if !strings.Contains(text, "测试汉字") {
		t.Fatalf("windows ocr text=%q", text)
	}
	shapePNG, err := os.ReadFile(shape)
	if err != nil {
		t.Fatal(err)
	}
	shapeGot, err := ExtractImageOCR(context.Background(), shapePNG)
	if err != nil {
		t.Fatal(err)
	}
	shapeText := ""
	for _, page := range shapeGot.Pages {
		shapeText += page.Text
	}
	if strings.Contains(shapeText, "测试") || strings.Contains(shapeText, "汉字") {
		t.Fatalf("shape ocr=%q", shapeText)
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
