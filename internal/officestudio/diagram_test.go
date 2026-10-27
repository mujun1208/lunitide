package officestudio

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/officetools"
)

func diagramSpec(direction string) *DiagramSpec {
	return &DiagramSpec{
		Direction: direction,
		Nodes: []DiagramNode{
			{ID: "start", Label: "线索入库", Kind: "start"},
			{ID: "triage", Label: "分诊定级", Kind: "decision"},
			{ID: "repair", Label: "排故修理", Kind: "process"},
			{ID: "release", Label: "放行交付", Kind: "end"},
		},
		Edges: []DiagramEdge{
			{From: "start", To: "triage", Label: "提交"},
			{From: "triage", To: "repair", Label: "需排故"},
			{From: "repair", To: "release"},
		},
	}
}

func TestDiagramValidation(t *testing.T) {
	cycle := &DiagramSpec{
		Nodes: []DiagramNode{{ID: "a", Label: "甲"}, {ID: "b", Label: "乙"}},
		Edges: []DiagramEdge{{From: "a", To: "b"}, {From: "b", To: "a"}},
	}
	if err := validateDiagram(cycle); !errors.Is(err, ErrFormat) {
		t.Fatalf("cyclic diagram must fail with ErrFormat, got %v", err)
	}
	unknown := &DiagramSpec{
		Nodes: []DiagramNode{{ID: "a", Label: "甲"}, {ID: "b", Label: "乙"}},
		Edges: []DiagramEdge{{From: "a", To: "ghost"}},
	}
	if err := validateDiagram(unknown); !errors.Is(err, ErrFormat) {
		t.Fatalf("unknown edge target must fail with ErrFormat, got %v", err)
	}
	dup := &DiagramSpec{
		Nodes: []DiagramNode{{ID: "a", Label: "甲"}, {ID: "b", Label: "乙"}},
		Edges: []DiagramEdge{{From: "a", To: "b", Label: "一"}, {From: "a", To: "b", Label: "二"}},
	}
	if err := validateDiagram(dup); !errors.Is(err, ErrFormat) {
		t.Fatalf("duplicate edge must fail with ErrFormat, got %v", err)
	}
	badDirection := diagramSpec("diagonal")
	if err := validateDiagram(badDirection); !errors.Is(err, ErrFormat) {
		t.Fatalf("invalid direction must fail with ErrFormat, got %v", err)
	}
	longLabel := diagramSpec("")
	longLabel.Nodes[0].Label = strings.Repeat("长", 13)
	if err := validateDiagram(longLabel); !errors.Is(err, ErrFormat) {
		t.Fatalf("13-rune node label must fail with ErrFormat, got %v", err)
	}
	longEdge := diagramSpec("")
	longEdge.Edges[0].Label = strings.Repeat("关", 9)
	if err := validateDiagram(longEdge); !errors.Is(err, ErrFormat) {
		t.Fatalf("9-rune edge label must fail with ErrFormat, got %v", err)
	}
	empty := &DiagramSpec{}
	if err := validateDiagram(empty); !errors.Is(err, ErrLimit) {
		t.Fatalf("empty diagram must fail with ErrLimit, got %v", err)
	}
}

func TestDiagramLayoutAutoDirection(t *testing.T) {
	shallow := diagramSpec("") // 4 layers → down
	laid, err := layoutDiagram(shallow)
	if err != nil {
		t.Fatal(err)
	}
	if laid.Direction != "down" {
		t.Fatalf("auto with 4 layers must resolve down, got %q", laid.Direction)
	}
	deep := &DiagramSpec{
		Nodes: []DiagramNode{{ID: "a", Label: "甲"}, {ID: "b", Label: "乙"}, {ID: "c", Label: "丙"}},
		Edges: []DiagramEdge{{From: "a", To: "b"}, {From: "b", To: "c"}},
	}
	laid, err = layoutDiagram(deep)
	if err != nil {
		t.Fatal(err)
	}
	if laid.Direction != "right" {
		t.Fatalf("auto with 3 layers must resolve right, got %q", laid.Direction)
	}
	forced := diagramSpec("right")
	laid, err = layoutDiagram(forced)
	if err != nil {
		t.Fatal(err)
	}
	if laid.Direction != "right" {
		t.Fatalf("forced direction must be preserved, got %q", laid.Direction)
	}
}

func TestDiagramLayoutDeterministic(t *testing.T) {
	spec := diagramSpec("")
	first, err := layoutDiagram(spec)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		again, err := layoutDiagram(spec)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Boxes) != len(first.Boxes) || len(again.Links) != len(first.Links) {
			t.Fatalf("layout run %d changed shape", i)
		}
		for j := range again.Boxes {
			if again.Boxes[j] != first.Boxes[j] {
				t.Fatalf("layout run %d moved box %s", i, again.Boxes[j].ID)
			}
		}
		for j := range again.Links {
			if again.Links[j] != first.Links[j] {
				t.Fatalf("layout run %d changed link %d", i, j)
			}
		}
	}
}

func TestDiagramSlideGenerateEndToEnd(t *testing.T) {
	spec := Spec{
		SchemaVersion: 1, Kind: PPTX, Title: "排故闭环",
		Slides: []Slide{
			{Title: "闭环总览", Layout: "diagram", Diagram: diagramSpec("")},
		},
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("diagram deck generation must be deterministic")
	}
	p, err := readPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	slide := p.parts["ppt/slides/slide1.xml"]
	for _, want := range []string{"线索入库", "分诊定级", "排故修理", "放行交付", "<p:cxnSp", "straightConnector1", "flowChartDecision", "需排故"} {
		if !bytes.Contains(slide, []byte(want)) {
			t.Fatalf("diagram slide missing %q: %.400s", want, slide)
		}
	}
	if got := bytes.Count(slide, []byte("<p:cxnSp>")); got != 3 {
		t.Fatalf("diagram slide must carry 3 connectors, got %d", got)
	}
	if got := bytes.Count(slide, []byte("<a:stCxn ")); got != 3 {
		t.Fatalf("each connector must snap to its source node, got %d stCxn", got)
	}
	insp, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range insp.Issues {
		if strings.HasPrefix(issue.Code, "OFFICE_GEOMETRY_") && issue.Severity != "info" {
			t.Fatalf("diagram slide geometry regression %s: %s", issue.Code, issue.Message)
		}
	}
}

func TestDiagramSlideRejectsMixedPayloads(t *testing.T) {
	mixed := Spec{
		SchemaVersion: 1, Kind: PPTX, Title: "混排",
		Slides: []Slide{
			{Title: "混排", Layout: "diagram", Bullets: []string{"要点"}, Diagram: diagramSpec("")},
		},
	}
	if _, err := Generate(mixed); !errors.Is(err, ErrFormat) {
		t.Fatalf("diagram slide with bullets must fail with ErrFormat, got %v", err)
	}
	misplaced := Spec{
		SchemaVersion: 1, Kind: PPTX, Title: "错位",
		Slides: []Slide{
			{Title: "错位", Layout: "content", Diagram: diagramSpec("")},
		},
	}
	if _, err := Generate(misplaced); !errors.Is(err, ErrFormat) {
		t.Fatalf("diagram payload outside diagram layout must fail with ErrFormat, got %v", err)
	}
	missing := Spec{
		SchemaVersion: 1, Kind: PPTX, Title: "空图",
		Slides: []Slide{{Title: "空图", Layout: "diagram"}},
	}
	if _, err := Generate(missing); !errors.Is(err, ErrFormat) {
		t.Fatalf("diagram layout without payload must fail with ErrFormat, got %v", err)
	}
}

func TestDiagramBoxesStayInsideCanvas(t *testing.T) {
	wide := &DiagramSpec{
		Nodes: []DiagramNode{
			{ID: "n1", Label: "一二三四五六七八九十"},
			{ID: "n2", Label: "乙"}, {ID: "n3", Label: "丙"}, {ID: "n4", Label: "丁"},
			{ID: "n5", Label: "戊"},
		},
		Edges: []DiagramEdge{
			{From: "n1", To: "n2"}, {From: "n1", To: "n3"},
			{From: "n1", To: "n4"}, {From: "n1", To: "n5"}, {From: "n2", To: "n5"},
		},
	}
	laid, err := layoutDiagram(wide)
	if err != nil {
		t.Fatal(err)
	}
	var boxes []officetools.SlideDiagramBox
	boxes = append(boxes, laid.Boxes...)
	chips := collectChips(laid)
	for _, box := range boxes {
		if box.X < 0 || box.Y < 0 || box.X+box.W > 12192000 || box.Y+box.H > 6180000+300000 {
			t.Fatalf("box %s escapes the canvas: %+v", box.ID, box)
		}
	}
	for i, a := range boxes {
		for j := i + 1; j < len(boxes); j++ {
			if b := boxes[j]; a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Fatalf("boxes %s and %s overlap: %+v %+v", a.ID, b.ID, a, b)
			}
		}
	}
	for i, a := range chips {
		if a[0] < 0 || a[1] < 0 || a[0]+a[2] > 12192000 || a[1]+a[3] > 6400000 {
			t.Fatalf("chip %d escapes the canvas: %v", i, a)
		}
		for j := i + 1; j < len(chips); j++ {
			if b := chips[j]; a[0] < b[0]+b[2] && b[0] < a[0]+a[2] && a[1] < b[1]+b[3] && b[1] < a[1]+a[3] {
				t.Fatalf("chips %d and %d overlap: %v %v", i, j, a, b)
			}
		}
		for _, box := range boxes {
			if a[0] < box.X+box.W && box.X < a[0]+a[2] && a[1] < box.Y+box.H && box.Y < a[1]+a[3] {
				t.Fatalf("chip %d overlaps box %s: %v %+v", i, box.ID, a, box)
			}
		}
	}
}

// collectChips mirrors the officetools renderer's chip geometry so layout
// tests can prove chips never touch boxes or each other.
func collectChips(d *officetools.SlideDiagram) [][4]int64 {
	var out [][4]int64
	boxes := make(map[string]officetools.SlideDiagramBox, len(d.Boxes))
	for _, box := range d.Boxes {
		boxes[box.ID] = box
	}
	for _, link := range d.Links {
		if strings.TrimSpace(link.Label) == "" {
			continue
		}
		from, to := boxes[link.From], boxes[link.To]
		anchor := func(b officetools.SlideDiagramBox, side int) (int64, int64) {
			switch side {
			case 1:
				return b.X + b.W/2, b.Y
			case 2:
				return b.X + b.W, b.Y + b.H/2
			case 3:
				return b.X + b.W/2, b.Y + b.H
			default:
				return b.X, b.Y + b.H/2
			}
		}
		x1, y1 := anchor(from, link.FromSide)
		x2, y2 := anchor(to, link.ToSide)
		runes := int64(len([]rune(strings.TrimSpace(link.Label))))
		chipW := runes*160000 + 120000
		if chipW < 640000 {
			chipW = 640000
		}
		if chipW > 1440000 {
			chipW = 1440000
		}
		var chipY int64
		if d.Direction == "down" {
			chipY = (y1+y2)/2 - 140000
		} else {
			chipY = from.Y - 296000
		}
		chipX := (x1+x2)/2 - chipW/2
		if chipX < 200000 {
			chipX = 200000
		}
		if max := int64(12192000) - 200000 - chipW; chipX > max {
			chipX = max
		}
		out = append(out, [4]int64{chipX, chipY, chipW, 280000})
	}
	return out
}
