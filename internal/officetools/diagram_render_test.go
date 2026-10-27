package officetools

import (
	"bytes"
	"strings"
	"testing"
)

func diagramSlideSpec() SlideSpec {
	return SlideSpec{
		Title:  "审批链",
		Layout: "diagram",
		Diagram: &SlideDiagram{
			Direction: "right",
			Boxes: []SlideDiagramBox{
				{ID: "apply", Label: "发起申请", Kind: "start", X: 520000, Y: 2500000, W: 1800000, H: 680000},
				{ID: "review", Label: "主管审批", Kind: "decision", X: 3200000, Y: 2500000, W: 1800000, H: 680000},
				{ID: "done", Label: "归档完成", Kind: "end", X: 5880000, Y: 2500000, W: 1800000, H: 680000},
			},
			Links: []SlideDiagramLink{
				{From: "apply", To: "review", Label: "提交", FromSide: 2, ToSide: 0},
				{From: "review", To: "done", FromSide: 2, ToSide: 0},
			},
		},
	}
}

// Diagram slides must validate, carry their own semantic layout part, and
// emit editable shapes plus snapped connectors — never SmartArt XML.
func TestDiagramSlideRendersNativeShapes(t *testing.T) {
	slides := []SlideSpec{diagramSlideSpec()}
	data, err := GenStudioPptxThemed("审批流", slides, nil, ClassicSlideTheme())
	if err != nil {
		t.Fatal(err)
	}
	body := zipPartBody(t, data, "ppt/slides/slide1.xml")
	for _, want := range []string{"发起申请", "主管审批", "归档完成", "flowChartDecision", "<p:cxnSp", `prst="straightConnector1"`, `<a:stCxn id="4" idx="2"/>`, `<a:endCxn id="5" idx="0"/>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("diagram slide missing %q: %.500s", want, body)
		}
	}
	if strings.Contains(body, "smartArt") {
		t.Fatal("diagram must not degrade into SmartArt")
	}
	if got := strings.Count(body, "Node_"); got != 3 {
		t.Fatalf("diagram slide must name 3 node shapes, got %d", got)
	}
	rels := zipPartBody(t, data, "ppt/slides/_rels/slide1.xml.rels")
	if !strings.Contains(rels, "../slideLayouts/slideLayout17.xml") {
		t.Fatalf("diagram slide must point at the semantic layout part: %s", rels)
	}
	if !zipHasPart(data, "ppt/slideLayouts/slideLayout17.xml") {
		t.Fatal("diagram layout part slideLayout17.xml must exist")
	}
}

func TestDiagramPayloadGuards(t *testing.T) {
	if _, err := GenStudioPptxThemed("空", []SlideSpec{{Title: "空", Layout: "diagram"}}, nil, ClassicSlideTheme()); err == nil {
		t.Fatal("diagram layout without payload must fail")
	}
	spec := diagramSlideSpec()
	spec.Layout = "content"
	if _, err := GenStudioPptxThemed("错位", []SlideSpec{spec}, nil, ClassicSlideTheme()); err == nil {
		t.Fatal("diagram payload outside diagram layout must fail")
	}
	spec = diagramSlideSpec()
	spec.Bullets = []string{"要点"}
	if _, err := GenStudioPptxThemed("混排", []SlideSpec{spec}, nil, ClassicSlideTheme()); err == nil {
		t.Fatal("diagram slide with bullets must fail")
	}
	broken := diagramSlideSpec()
	broken.Diagram.Links = append(broken.Diagram.Links, SlideDiagramLink{From: "apply", To: "ghost", FromSide: 2, ToSide: 0})
	if _, err := GenStudioPptxThemed("断链", []SlideSpec{broken}, nil, ClassicSlideTheme()); err == nil {
		t.Fatal("connector to unknown node must fail")
	}
}

func TestDiagramDeckDeterministic(t *testing.T) {
	slides := []SlideSpec{diagramSlideSpec()}
	first, err := GenStudioPptxThemed("审批流", slides, nil, ClassicSlideTheme())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := GenStudioPptxThemed("审批流", slides, nil, ClassicSlideTheme())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("diagram deck run %d diverged", i)
		}
	}
}
