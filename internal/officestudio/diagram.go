package officestudio

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/officetools"
)

// Native relationship diagrams. A diagram slide carries a directed acyclic
// graph; this file validates it and computes a deterministic layered layout
// (longest-path layering, barycenter ordering). officetools then emits plain
// p:sp boxes and p:cxnSp connectors — fully editable shapes, never SmartArt.

const (
	diagramAreaX = int64(520000)
	diagramAreaY = int64(1900000)
	diagramAreaW = int64(11150000)
	diagramAreaH = int64(4280000)

	MaxDiagramNodes = 12
	MaxDiagramEdges = 16
	// Depth and width caps keep every box readable: deeper or wider graphs
	// must be split across pages instead of being shrunk into noise.
	maxDiagramDepth = 6
	maxDiagramWidth = 5

	diagramLabelMaxRunes     = 12
	diagramEdgeLabelMaxRunes = 8

	// Vertical channel reserved between layout lanes so edge-label chips fit
	// without touching neighbouring node boxes (geometry overlap must stay 0).
	diagramGapDown  = int64(340000)
	diagramGapRight = int64(620000)
)

// Connector sides on a node box in the ECMA-376 preset-geometry connection
// site order for roundRect/flowChartDecision: 0 left, 1 top, 2 right, 3 bottom.
const (
	diagramSideLeft   = 0
	diagramSideTop    = 1
	diagramSideRight  = 2
	diagramSideBottom = 3
)

func validateDiagram(d *DiagramSpec) error {
	if d == nil {
		return fmt.Errorf("%w: diagram slide requires a diagram payload", ErrFormat)
	}
	if len(d.Nodes) < 2 || len(d.Nodes) > MaxDiagramNodes {
		return fmt.Errorf("%w: diagram needs 2–%d nodes", ErrLimit, MaxDiagramNodes)
	}
	if len(d.Edges) < 1 || len(d.Edges) > MaxDiagramEdges {
		return fmt.Errorf("%w: diagram needs 1–%d edges", ErrLimit, MaxDiagramEdges)
	}
	switch d.Direction {
	case "", "auto", "down", "right":
	default:
		return fmt.Errorf("%w: diagram direction must be auto, down, or right", ErrFormat)
	}
	ids := make(map[string]bool, len(d.Nodes))
	for _, n := range d.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" || utf8.RuneCountInString(id) > 32 {
			return fmt.Errorf("%w: diagram node id is required", ErrFormat)
		}
		if ids[id] {
			return fmt.Errorf("%w: duplicate diagram node id %q", ErrFormat, id)
		}
		ids[id] = true
		label := strings.TrimSpace(n.Label)
		if label == "" || utf8.RuneCountInString(label) > diagramLabelMaxRunes {
			return fmt.Errorf("%w: diagram node label is required (≤%d characters)", ErrFormat, diagramLabelMaxRunes)
		}
		switch n.Kind {
		case "", "start", "process", "decision", "end":
		default:
			return fmt.Errorf("%w: diagram node kind must be start, process, decision, or end", ErrFormat)
		}
	}
	seenEdges := make(map[string]bool, len(d.Edges))
	for _, e := range d.Edges {
		if !ids[strings.TrimSpace(e.From)] || !ids[strings.TrimSpace(e.To)] {
			return fmt.Errorf("%w: diagram edge references unknown node", ErrFormat)
		}
		if e.From == e.To {
			return fmt.Errorf("%w: diagram edge cannot loop a node", ErrFormat)
		}
		key := strings.TrimSpace(e.From) + "\x00" + strings.TrimSpace(e.To)
		if seenEdges[key] {
			return fmt.Errorf("%w: duplicate diagram edge %s→%s; merge their labels", ErrFormat, e.From, e.To)
		}
		seenEdges[key] = true
		if utf8.RuneCountInString(e.Label) > diagramEdgeLabelMaxRunes {
			return fmt.Errorf("%w: diagram edge label is too long (≤%d characters)", ErrFormat, diagramEdgeLabelMaxRunes)
		}
	}
	layers, err := diagramLayers(d)
	if err != nil {
		return err
	}
	if len(layers) > maxDiagramDepth {
		return fmt.Errorf("%w: 关系层级超过 %d 层，请拆分为多页或改用 process 版式", ErrLimit, maxDiagramDepth)
	}
	width := 0
	for _, layer := range layers {
		if len(layer) > width {
			width = len(layer)
		}
	}
	if width > maxDiagramWidth {
		return fmt.Errorf("%w: 单层节点超过 %d 个，请拆分关系图", ErrLimit, maxDiagramWidth)
	}
	return nil
}

// diagramLayers assigns longest-path layers and validates the graph is a DAG.
func diagramLayers(d *DiagramSpec) ([][]string, error) {
	depth := make(map[string]int, len(d.Nodes))
	predecessors := make(map[string][]string, len(d.Nodes))
	successors := make(map[string][]string, len(d.Nodes))
	for _, e := range d.Edges {
		predecessors[e.To] = append(predecessors[e.To], e.From)
		successors[e.From] = append(successors[e.From], e.To)
	}
	queue := make([]string, 0, len(d.Nodes))
	indegree := make(map[string]int, len(d.Nodes))
	for _, n := range d.Nodes {
		indegree[n.ID] = len(predecessors[n.ID])
		if indegree[n.ID] == 0 {
			queue = append(queue, n.ID)
			depth[n.ID] = 0
		}
	}
	processed := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		processed++
		for _, next := range successors[current] {
			if depth[next] < depth[current]+1 {
				depth[next] = depth[current] + 1
			}
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if processed != len(d.Nodes) {
		return nil, fmt.Errorf("%w: 关系图存在循环依赖，请改用 process/timeline 版式表达循环流程", ErrFormat)
	}
	layers := make([][]string, 0, maxDiagramDepth)
	order := make(map[string]int, len(d.Nodes))
	for _, n := range d.Nodes {
		layer := depth[n.ID]
		for len(layers) <= layer {
			layers = append(layers, nil)
		}
		order[n.ID] = len(layers[layer])
		layers[layer] = append(layers[layer], n.ID)
	}
	// One barycenter pass: order each layer by the average predecessor slot
	// so fan-outs read naturally. Input order breaks ties deterministically.
	for li := 1; li < len(layers); li++ {
		layer := layers[li]
		bary := make([]struct {
			id    string
			value float64
			slot  int
		}, len(layer))
		for i, id := range layer {
			sum, count := 0.0, 0.0
			for _, p := range predecessors[id] {
				sum += float64(order[p])
				count++
			}
			if count == 0 {
				sum = float64(i)
				count = 1
			}
			bary[i] = struct {
				id    string
				value float64
				slot  int
			}{id, sum / count, i}
		}
		sort.SliceStable(bary, func(i, j int) bool {
			if bary[i].value != bary[j].value {
				return bary[i].value < bary[j].value
			}
			return bary[i].slot < bary[j].slot
		})
		for i, entry := range bary {
			layers[li][i] = entry.id
			order[entry.id] = i
		}
	}
	return layers, nil
}

// layoutDiagram computes box geometry for a validated diagram and returns the
// positioned payload consumed by the officetools renderer.
func layoutDiagram(d *DiagramSpec) (*officetools.SlideDiagram, error) {
	if err := validateDiagram(d); err != nil {
		return nil, err
	}
	layers, err := diagramLayers(d)
	if err != nil {
		return nil, err
	}
	direction := d.Direction
	if direction == "" || direction == "auto" {
		if len(layers) >= 4 {
			direction = "down"
		} else {
			direction = "right"
		}
	}
	width := 0
	for _, layer := range layers {
		if len(layer) > width {
			width = len(layer)
		}
	}
	nodes := make(map[string]*DiagramNode, len(d.Nodes))
	for i := range d.Nodes {
		nodes[d.Nodes[i].ID] = &d.Nodes[i]
	}
	slotW, slotH, gapV := int64(0), int64(0), int64(0)
	if direction == "down" {
		slotW = diagramAreaW / int64(width)
		slotH = diagramAreaH / int64(len(layers))
		gapV = diagramGapDown
	} else {
		slotW = diagramAreaW / int64(len(layers))
		slotH = diagramAreaH / int64(width)
		gapV = diagramGapRight
	}
	out := &officetools.SlideDiagram{Direction: direction}
	out.Boxes = make([]officetools.SlideDiagramBox, 0, len(d.Nodes))
	for li, layer := range layers {
		for si, id := range layer {
			node := nodes[id]
			w, h := diagramBoxSize(node.Label, slotW, slotH, gapV)
			var x, y int64
			if direction == "down" {
				x = diagramAreaX + int64(si)*slotW + (slotW-w)/2
				y = diagramAreaY + int64(li)*slotH + (slotH-h)/2
			} else {
				x = diagramAreaX + int64(li)*slotW + (slotW-w)/2
				y = diagramAreaY + int64(si)*slotH + (slotH-h)/2
			}
			out.Boxes = append(out.Boxes, officetools.SlideDiagramBox{ID: id, Label: node.Label, Kind: node.Kind, X: x, Y: y, W: w, H: h})
		}
	}
	out.Links = make([]officetools.SlideDiagramLink, 0, len(d.Edges))
	for _, e := range d.Edges {
		from, to := diagramSideRight, diagramSideLeft
		if direction == "down" {
			from, to = diagramSideBottom, diagramSideTop
		}
		out.Links = append(out.Links, officetools.SlideDiagramLink{From: e.From, To: e.To, Label: e.Label, FromSide: from, ToSide: to})
	}
	return out, nil
}

// diagramBoxSize estimates a box from its label and the available slot,
// clamped so every box stays readable and inside its slot. gapV reserves the
// inter-lane channel that carries edge-label chips without overlaps.
func diagramBoxSize(label string, slotW, slotH, gapV int64) (int64, int64) {
	runes := int64(utf8.RuneCountInString(strings.TrimSpace(label)))
	estimated := 420000 + runes*215000
	w := estimated
	if max := slotW - 320000; w > max {
		w = max
	}
	if w < 800000 {
		w = 800000
	}
	h := int64(680000)
	if max := slotH - gapV; h > max {
		h = max
	}
	if h < 260000 {
		h = 260000
	}
	return w, h
}
