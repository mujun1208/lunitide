package officestudio

import (
	"bytes"
	"strings"
	"testing"
)

func TestPPTEditableObjectsAndCoverage(t *testing.T) {
	data, err := Generate(Spec{
		SchemaVersion: 2,
		Kind:          PPTX,
		Title:         "可编辑覆盖",
		Slides: []Slide{
			{
				Title:   "指标页",
				Layout:  "content",
				Bullets: []string{"订单口径已核对"},
				Charts:  []SlideChart{testChartSpec()},
			},
			{
				Title:   "说明页",
				Layout:  "content",
				Bullets: []string{"第二页正文必须进入视觉覆盖"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := PPTEditableInventory(data)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Slides != 2 || inv.Texts < 2 || inv.Charts < 1 || inv.Shapes < 1 {
		t.Fatalf("native objects missing: %+v", inv)
	}
	if !inv.RelationsOK {
		t.Fatal("chart or slide relationships missing")
	}
	if inv.VisualFullCoverage {
		t.Fatal("inventory must not mark visual full coverage without a per-page review")
	}
	if err = ValidateVisualManifest(VisualManifest{PageCount: inv.Slides, PageDigests: []string{"only-first"}}, inv.Slides); err == nil {
		t.Fatal("visual result missing last page must reject full marking")
	}
	if c := VisualPageCoverageCheck(inv.Slides, []int{0}); c.Status == "passed" {
		t.Fatalf("leak last page: %#v", c)
	}
	parts := zipParts(t, data)
	slide1 := parts["ppt/slides/slide1.xml"]
	if !bytes.Contains(slide1, []byte("<p:sp>")) || !bytes.Contains(slide1, []byte("graphicFrame")) {
		t.Fatalf("slide1 missing native shape/chart frame")
	}
	rels := parts["ppt/slides/_rels/slide1.xml.rels"]
	if !bytes.Contains(rels, []byte("relationships/chart")) {
		t.Fatalf("slide1 chart relationship missing: %s", rels)
	}
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	if findText(t, i, "订单口径已核对").ID == "" || findText(t, i, "第二页正文必须进入视觉覆盖").ID == "" {
		t.Fatal("editable text nodes missing")
	}
	if strings.Contains(strings.ToLower(i.Preview), "qualified") || strings.Contains(i.Preview, "高端商用") {
		t.Fatal("must not mark live qualified or 高端商用")
	}
}
