package officestudio

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
)

func studioTestImage(t *testing.T, w, h int, jpg bool) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 11), 90, 255})
		}
	}
	var b bytes.Buffer
	var err error
	if jpg {
		err = jpeg.Encode(&b, im, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&b, im)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func studioImageSpec(data []byte) SlideImage {
	return SlideImage{SourceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", SHA256: digest(data), X: 1_000_000, Y: 2_000_000, Width: 4_000_000, Height: 3_000_000, Fit: "contain", Alt: "照片与来源", Data: data}
}
func generatedPictures(t *testing.T, images ...SlideImage) []byte {
	t.Helper()
	data, err := Generate(Spec{SchemaVersion: 1, Kind: PPTX, Title: "图片测试", Slides: []Slide{{Title: "保留文字", Layout: "content", Bullets: []string{"原始文字不能改"}, Images: images}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func imageNodes(t *testing.T, data []byte) []Node {
	t.Helper()
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	var out []Node
	for _, n := range i.Nodes {
		if n.Kind == "image" {
			out = append(out, n)
		}
	}
	return out
}

func TestPPTPicturesAreNativeObjectsWithBoundSourceAndFit(t *testing.T) {
	raw := studioTestImage(t, 120, 60, false)
	contain := studioImageSpec(raw)
	cover := contain
	cover.X = 6_000_000
	cover.Fit = "cover"
	data := generatedPictures(t, contain, cover)
	nodes := imageNodes(t, data)
	if len(nodes) != 2 || !nodes[0].Editable || nodes[0].Image.PixelWidth != 120 || nodes[0].Image.MediaSHA256 != digest(raw) || nodes[0].Image.SourceID != contain.SourceID {
		t.Fatalf("native indexed images missing: %+v", nodes)
	}
	if nodes[0].Image.Width != 4_000_000 || nodes[0].Image.Height != 2_000_000 || nodes[0].Image.Y != 2_500_000 {
		t.Fatal("contain stretched or lost centering")
	}
	if nodes[1].Image.CropLeft <= 0 || nodes[1].Image.Width != cover.Width {
		t.Fatal("cover lacks native crop")
	}
	p := zipParts(t, data)
	if !bytes.Equal(p[nodes[0].Image.MediaPart], raw) || nodes[0].Image.MediaPart != nodes[1].Image.MediaPart {
		t.Fatal("media was changed or not deduplicated")
	}
	encoded, _ := json.Marshal(contain)
	if bytes.Contains(encoded, raw) || bytes.Contains(encoded, []byte(`"Data"`)) || bytes.Contains(encoded, []byte(`"data"`)) {
		t.Fatal("model spec exposed embedded image bytes")
	}
}

func TestPPTPicturePatchCopiesSharedMediaAndPreservesAllNonTargets(t *testing.T) {
	old := studioTestImage(t, 80, 40, false)
	img := studioImageSpec(old)
	other := img
	other.X = 6_000_000
	data := generatedPictures(t, img, other)
	data = editZIP(t, data, map[string][]byte{"custom/opaque.dat": []byte("Unknown bytes stay exact")})
	before, _ := Inspect(PPTX, data)
	nodes := imageNodes(t, data)
	fresh := studioTestImage(t, 40, 80, true)
	req := PatchRequest{Kind: PPTX, BaseSHA256: before.SHA256, Images: []ImagePatch{{NodeID: nodes[0].ID, ExpectedDigest: nodes[0].Digest, SourceID: img.SourceID, SHA256: digest(fresh), Fit: "contain", Data: fresh}}}
	out, err := Patch(data, req)
	if err != nil {
		t.Fatal(err)
	}
	next := imageNodes(t, out.Data)
	if next[0].ID != nodes[0].ID || next[0].Digest == nodes[0].Digest || next[1].Digest != nodes[1].Digest || next[0].Image.MediaPart == next[1].Image.MediaPart {
		t.Fatal("picture replacement changed unrelated shared image")
	}
	p := zipParts(t, out.Data)
	if !bytes.Equal(p[nodes[0].Image.MediaPart], old) || !bytes.Equal(p[next[0].Image.MediaPart], fresh) {
		t.Fatal("copy-on-write media integrity failure")
	}
	changed := map[string]bool{}
	for _, n := range out.ChangedParts {
		changed[n] = true
	}
	oldZip, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	newZip, _ := zip.NewReader(bytes.NewReader(out.Data), int64(len(out.Data)))
	newByName := map[string]*zip.File{}
	for _, f := range newZip.File {
		newByName[f.Name] = f
	}
	for _, f := range oldZip.File {
		if changed[f.Name] {
			continue
		}
		ar, _ := f.OpenRaw()
		br, _ := newByName[f.Name].OpenRaw()
		a, _ := io.ReadAll(ar)
		b, _ := io.ReadAll(br)
		if !bytes.Equal(a, b) {
			t.Fatalf("untouched compressed ZIP entry changed: %s", f.Name)
		}
	}
	for _, bad := range []PatchRequest{{Kind: PPTX, BaseSHA256: "wrong", Images: req.Images}, {Kind: PPTX, BaseSHA256: before.SHA256, Images: []ImagePatch{req.Images[0], req.Images[0]}}} {
		if _, err := Patch(data, bad); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale/duplicate accepted: %v", err)
		}
	}
	req.Images[0].ExpectedDigest = strings.Repeat("0", 64)
	if _, err := Patch(data, req); !errors.Is(err, ErrConflict) {
		t.Fatal("stale node digest accepted")
	}
}

func TestPPTPicturesRejectWrongSourcePixelsAndUnsupportedImports(t *testing.T) {
	raw := studioTestImage(t, 80, 40, false)
	for _, change := range []func(*SlideImage){func(i *SlideImage) { i.SHA256 = strings.Repeat("0", 64) }, func(i *SlideImage) { i.SourceID = "unmanaged" }, func(i *SlideImage) { i.Fit = "stretch" }, func(i *SlideImage) { i.X = -1 }, func(i *SlideImage) { i.Width = 20_000_000 }, func(i *SlideImage) { i.Data = i.Data[:40]; i.SHA256 = digest(i.Data) }} {
		im := studioImageSpec(raw)
		change(&im)
		if _, err := Generate(Spec{SchemaVersion: 1, Kind: PPTX, Title: "reject", Slides: []Slide{{Title: "image", Images: []SlideImage{im}}}}); err == nil {
			t.Fatal("unsafe picture accepted")
		}
	}
	data := generatedPictures(t, studioImageSpec(raw))
	n := imageNodes(t, data)[0]
	bad := editZIP(t, data, map[string][]byte{n.Image.MediaPart: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><image href="https://outside.invalid/x"/></svg>`)})
	i, err := Inspect(PPTX, bad)
	if err != nil || i.RenderAllowed {
		t.Fatalf("unknown/active image was allowed to render: %v", err)
	}
	part := zipParts(t, data)[n.Part]
	// Locate the picture itself, not an earlier text box transform.
	start := bytes.Index(part, []byte(`<p:pic `))
	part = append(append([]byte(nil), part[:start]...), bytes.Replace(part[start:], []byte(`<a:xfrm>`), []byte(`<a:xfrm rot="3600000">`), 1)...)
	complex := editZIP(t, data, map[string][]byte{n.Part: part})
	for _, pic := range imageNodes(t, complex) {
		if pic.Editable {
			t.Fatal("rotated unsupported picture editable")
		}
	}
}

func TestPPTImportedPictureInheritedNamespacesRemainEditable(t *testing.T) {
	data := generatedPictures(t, studioImageSpec(studioTestImage(t, 40, 20, false)))
	n := imageNodes(t, data)[0]
	p := zipParts(t, data)
	decl := ` xmlns:p="` + presentationNS + `" xmlns:a="` + drawingNS + `" xmlns:r="` + officeRelNS + `"`
	modified := editZIP(t, data, map[string][]byte{n.Part: bytes.Replace(p[n.Part], []byte("<p:pic"+decl+">"), []byte("<p:pic>"), 1)})
	nodes := imageNodes(t, modified)
	if len(nodes) != 1 || !nodes[0].Editable {
		t.Fatal("normal inherited picture namespaces were not supported")
	}
}
