package ccapp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestAssignNodeIDsPrefixesByRole(t *testing.T) {
	nodes := assignNodeIDs([]UINode{
		{Role: "button", Name: "OK"},
		{Role: "button", Name: "Cancel"},
		{Role: "edit", Name: "Name"},
		{Role: "link", Name: "Docs"},
	})
	want := []string{"B1", "B2", "E1", "L1"}
	for i, id := range want {
		if nodes[i].ID != id {
			t.Fatalf("node %d id = %q want %q", i, nodes[i].ID, id)
		}
	}
}

func TestAnnotateCaptureDrawsBadge(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 40, B: 80, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := AnnotateCapture(buf.Bytes(), []UINode{{ID: "B1", X: 8, Y: 6, W: 20, H: 12}}, 80, 40)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	// Badge fill starts at the node origin; (10,8) can land on white glyph ink.
	r, g, b, _ := got.At(8, 6).RGBA()
	if r>>8 < 200 || b>>8 < 100 || g>>8 > 80 {
		t.Fatalf("expected magenta badge pixel, got %d,%d,%d", r>>8, g>>8, b>>8)
	}
}
