package officestudio

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func overflowedPicture(t *testing.T) ([]byte, Node) {
	t.Helper()
	data := generatedPictures(t, studioImageSpec(studioTestImage(t, 40, 20, false)))
	n := imageNodes(t, data)[0]
	part := zipParts(t, data)[n.Part]
	old := fmt.Sprintf(`<a:off x="%d" y="%d"/>`, n.Image.X, n.Image.Y)
	next := fmt.Sprintf(`<a:off x="%d" y="%d"/>`, n.Image.X+50_000_000, n.Image.Y)
	if !bytes.Contains(part, []byte(old)) {
		t.Fatalf("source offset missing: %s", old)
	}
	return editZIP(t, data, map[string][]byte{n.Part: bytes.Replace(part, []byte(old), []byte(next), 1)}), n
}

func TestGeometryOverflowIsReportedAndSimpleObjectsAreTranslatedAtMostTwice(t *testing.T) {
	data, original := overflowedPicture(t)
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	if countGeometryOverflow(i.Issues) == 0 {
		t.Fatal("overflow was not reported")
	}
	v, err := Validate(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	if geometryCheck(i.Issues).Status != "failed" {
		t.Fatal("validation hid overflow")
	}
	found := false
	for _, c := range v.Checks {
		if c.ID == "geometry_bounds" && c.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("geometry_bounds missing")
	}
	report, repaired, err := RepairGeometryOverflow(data)
	if err != nil || report.Repaired == 0 || report.RemainingOverflow != 0 || report.Rounds > MaxGeometryRepairRounds {
		t.Fatalf("repair: %+v err=%v", report, err)
	}
	nodes := imageNodes(t, repaired)
	if len(nodes) != 1 || nodes[0].Image.Width != original.Image.Width || nodes[0].Image.Height != original.Image.Height || nodes[0].Image.MediaSHA256 != original.Image.MediaSHA256 {
		t.Fatal("repair changed picture bytes or size")
	}
	if nodes[0].Image.X+nodes[0].Image.Width > report.SlideWidth || nodes[0].Image.Y+nodes[0].Image.Height > report.SlideHeight {
		t.Fatal("repaired object still off canvas")
	}
	if !strings.Contains(string(zipParts(t, repaired)["ppt/slides/slide1.xml"]), "原始文字不能改") {
		t.Fatal("repair changed slide business text")
	}
	clean, err := Validate(PPTX, repaired)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range clean.Checks {
		if c.ID == "geometry_bounds" && c.Status != "passed" {
			t.Fatalf("repaired file not in bounds: %+v", c)
		}
	}
}

func TestGeometryRepairLeavesRotatedAndOversizedObjects(t *testing.T) {
	data, n := overflowedPicture(t)
	part := zipParts(t, data)[n.Part]
	start := bytes.Index(part, []byte(`<p:pic`))
	rotated := append(append([]byte(nil), part[:start]...), bytes.Replace(part[start:], []byte(`<a:xfrm>`), []byte(`<a:xfrm rot="3600000">`), 1)...)
	data = editZIP(t, data, map[string][]byte{n.Part: rotated})
	report, out, err := RepairGeometryOverflow(data)
	if err != nil || report.Repaired != 0 || bytes.Equal(out, data) && report.Unsupported == 0 && countGeometryOverflow(report.Issues) == 0 {
		t.Fatalf("rotated object was rewritten: %+v %v", report, err)
	}
	if !bytes.Equal(zipParts(t, out)[n.Image.MediaPart], zipParts(t, data)[n.Image.MediaPart]) {
		t.Fatal("rotation repair touched media")
	}

	wide := generatedPictures(t, studioImageSpec(studioTestImage(t, 40, 20, false)))
	pic := imageNodes(t, wide)[0]
	slide := zipParts(t, wide)[pic.Part]
	old := fmt.Sprintf(`<a:ext cx="%d" cy="%d"/>`, pic.Image.Width, pic.Image.Height)
	next := fmt.Sprintf(`<a:ext cx="%d" cy="%d"/>`, 200_000_000, pic.Image.Height)
	oversized := editZIP(t, wide, map[string][]byte{pic.Part: bytes.Replace(slide, []byte(old), []byte(next), 1)})
	report, _, err = RepairGeometryOverflow(oversized)
	if err != nil || report.Repaired != 0 || report.RemainingOverflow == 0 {
		t.Fatalf("oversized object was forced to fit: %+v %v", report, err)
	}
}

func TestGeneratedManagedDeckPassesGeometryBounds(t *testing.T) {
	data := generatedPictures(t, studioImageSpec(studioTestImage(t, 80, 40, false)))
	v, err := Validate(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Checks {
		if c.ID == "geometry_bounds" && c.Status != "passed" {
			t.Fatalf("managed deck failed geometry: %+v", c)
		}
	}
}

func TestGeometryExplicitEndTagAndExtensionPayloadPreserved(t *testing.T) {
	data, n := overflowedPicture(t)
	parts := zipParts(t, data)
	body := parts[n.Part]
	start := bytes.Index(body, []byte(`<p:pic`))
	tail := body[start:]
	off := bytes.Index(tail, []byte(`<a:off `))
	end := bytes.Index(tail[off:], []byte(`/>`)) + off
	tail = append(append([]byte{}, tail[:end]...), append([]byte(`></a:off>`), tail[end+2:]...)...)
	tail = bytes.Replace(tail, []byte(`</p:pic>`), []byte(`<p:extLst><p:ext uri="keep"><a:ext uri="custom"/><a:off x="999" y="998"/></p:ext></p:extLst></p:pic>`), 1)
	body = append(append([]byte{}, body[:start]...), tail...)
	data = editZIP(t, data, map[string][]byte{n.Part: body})
	report, out, err := RepairGeometryOverflow(data)
	if err != nil || report.Repaired != 1 || report.RemainingOverflow != 0 {
		t.Fatalf("%+v %v", report, err)
	}
	after := zipParts(t, out)[n.Part]
	if !bytes.Contains(after, []byte(`<a:ext uri="custom"/><a:off x="999" y="998"/>`)) {
		t.Fatal("extension payload modified")
	}
	if !bytes.Contains(after, []byte(`></a:off>`)) || bytes.Contains(after, []byte(`</a:off></a:off>`)) {
		t.Fatal("explicit closing tag broken")
	}
}

func TestGeometryInheritedAndMalformedDimensionsAreNotInvented(t *testing.T) {
	for _, extent := range []string{`<a:ext cx="broken" cy="200"/>`, `<a:ext cx="0" cy="200"/>`, ``} {
		body := []byte(`<p:sld xmlns:p="` + presentationNS + `" xmlns:a="` + drawingNS + `"><p:cSld><p:spTree><p:sp><p:spPr><a:xfrm><a:off x="-10" y="0"/>` + extent + `</a:xfrm></p:spPr></p:sp></p:spTree></p:cSld></p:sld>`)
		objects, _, err := scanSlideGeometry("ppt/slides/slide1.xml", body)
		if err != nil || len(objects) != 1 || objects[0].unsupported == "" {
			t.Fatalf("invented dimensions: %+v %v", objects, err)
		}
	}
}
