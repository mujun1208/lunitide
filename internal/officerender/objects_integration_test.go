package officerender_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
	"github.com/lunitide/lunitide/internal/officerender"
	"github.com/lunitide/lunitide/internal/officestudio"
)

func TestOfficeNativePicturesAndCharts(t *testing.T) {
	executable := os.Getenv("OFFICE_STUDIO_TEST_LIBREOFFICE")
	if executable == "" {
		t.Skip("set OFFICE_STUDIO_TEST_LIBREOFFICE for real object rendering")
	}
	r := &officerender.Renderer{Root: t.TempDir(), Executable: executable, Preflight: func(kind string, data []byte) error {
		i, err := officestudio.Inspect(officestudio.Kind(kind), data)
		if err != nil {
			return err
		}
		if !i.RenderAllowed {
			return fmt.Errorf("fixture preflight blocked")
		}
		return nil
	}}
	img := image.NewNRGBA(image.Rect(0, 0, 120, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 2), G: uint8(y * 4), B: 140, A: 255})
		}
	}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(imageData.Bytes())
	deck := officestudio.Spec{SchemaVersion: 1, Kind: officestudio.PPTX, Title: "真实对象验证"}
	for _, kind := range []string{"column", "bar", "line", "pie"} {
		deck.Slides = append(deck.Slides, officestudio.Slide{Title: kind + " chart", Images: []officestudio.SlideImage{{SourceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", SHA256: hex.EncodeToString(sum[:]), Data: imageData.Bytes(), X: 500000, Y: 2000000, Width: 1500000, Height: 1500000, Fit: "contain", Alt: "合成色块"}}, Charts: []officestudio.SlideChart{{Type: kind, Title: "Verified " + kind, Categories: []string{"Alpha", "Beta"}, Series: []officestudio.ChartSeries{{Name: "Revenue", Values: []string{"12.5", "25"}}}, X: 2500000, Y: 1700000, Width: 6500000, Height: 3900000, Legend: true}}})
	}
	book := officestudio.Spec{SchemaVersion: 1, Kind: officestudio.XLSX, Title: "真实表格图表", Sheets: []officestudio.Sheet{{Name: "Data", Rows: [][]officestudio.Cell{{{Type: "text", Value: "Category"}, {Type: "text", Value: "Value"}}, {{Type: "text", Value: "Alpha"}, {Type: "number", Value: "12.5"}}, {{Type: "text", Value: "Beta"}, {Type: "number", Value: "25"}}}, Charts: []officestudio.SheetChart{{Type: "column", Title: "Verified worksheet chart", Categories: "A2:A3", Series: []officestudio.SheetChartSeries{{Name: "Revenue", Range: "B2:B3"}}, Anchor: "E2", Width: 640, Height: 360, Legend: true}}}}}
	for _, spec := range []officestudio.Spec{deck, book} {
		t.Run(string(spec.Kind), func(t *testing.T) {
			data, err := officestudio.Generate(spec)
			if err != nil {
				t.Fatal(err)
			}
			result, err := r.Render(context.Background(), string(spec.Kind), data)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := pdf.NewReader(bytes.NewReader(result.PDF), int64(len(result.PDF)))
			if err != nil {
				t.Fatal(err)
			}
			plain, err := reader.GetPlainText()
			if err != nil {
				t.Fatal(err)
			}
			text, err := io.ReadAll(plain)
			if err != nil {
				t.Fatal(err)
			}
			if dir := os.Getenv("OFFICE_STUDIO_TEST_EVIDENCE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for name, body := range map[string][]byte{"objects." + string(spec.Kind): data, "objects-" + string(spec.Kind) + ".pdf": result.PDF} {
					if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if spec.Kind == officestudio.PPTX && reader.NumPage() != 4 {
				t.Fatalf("expected 4 real chart pages, got %d", reader.NumPage())
			}
			labels := []string{"Verified worksheet chart"}
			if spec.Kind == officestudio.PPTX {
				labels = []string{"Verified column", "Verified bar", "Verified line", "Verified pie"}
			}
			for _, label := range labels {
				if !strings.Contains(string(text), label) {
					t.Fatalf("chart title not rendered: %q\n%s", label, text)
				}
			}
			if !strings.Contains(string(text), "Alpha") || !strings.Contains(string(text), "Revenue") {
				t.Fatalf("native chart labels missing: %s", text)
			}
		})
	}
}
