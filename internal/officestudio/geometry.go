package officestudio

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const MaxGeometryRepairRounds = 2

// GeometryReport records canvas overflow for simple PPT objects. Text overflow
// and font metrics are not inferred from these EMU boxes.
type GeometryReport struct {
	SlideWidth        int64    `json:"slideWidth"`
	SlideHeight       int64    `json:"slideHeight"`
	Overflow          int      `json:"overflow"`
	Unsupported       int      `json:"unsupported"`
	Repaired          int      `json:"repaired"`
	Rounds            int      `json:"rounds"`
	RemainingOverflow int      `json:"remainingOverflow"`
	Issues            []Issue  `json:"issues"`
	ChangedParts      []string `json:"changedParts,omitempty"`
}

type geomObject struct {
	part, kind, unsupported, name, text string
	hasText                             bool
	x, y, w, h                          int64
	offStart, offEnd                    int
}

type geomFrame struct {
	local, space, unsupported, name, text string
	object, hasText                       bool
	x, y, w, h                            int64
	offStart, offEnd                      int
	hasOff, hasExt                        bool
}

func geometryCheck(issues []Issue) Check {
	overflow, unsupported := 0, 0
	for _, issue := range issues {
		switch issue.Code {
		case "OFFICE_GEOMETRY_OVERFLOW":
			overflow++
		case "OFFICE_GEOMETRY_UNSUPPORTED":
			unsupported++
		}
	}
	if overflow > 0 {
		return Check{ID: "geometry_bounds", Status: "failed", Message: fmt.Sprintf("有 %d 个简单对象超出幻灯片画布；复杂对象未盲缩字体，文字真实溢出需渲染证据。", overflow)}
	}
	if unsupported > 0 {
		return Check{ID: "geometry_bounds", Status: "missing", Message: fmt.Sprintf("有 %d 个旋转/组合等复杂对象未自动修复；简单对象均在画布内。", unsupported)}
	}
	return Check{ID: "geometry_bounds", Status: "passed", Message: "简单对象均在幻灯片画布内。文字溢出与字体度量未用像素估算代替。"}
}

func geometryIssues(parts map[string][]byte, nodes []Node) []Issue {
	sw, sh, err := deckSize(parts)
	if err != nil {
		return []Issue{{Code: "OFFICE_GEOMETRY_UNSUPPORTED", Severity: "info", Message: "无法读取幻灯片画布尺寸，未做几何越界判断。"}}
	}
	var issues []Issue
	for _, name := range sortedPartNames(parts) {
		if !isSlidePart(name) {
			continue
		}
		objects, scanIssues, err := scanSlideGeometry(name, parts[name])
		if err != nil {
			issues = append(issues, Issue{Code: "OFFICE_GEOMETRY_UNSUPPORTED", Severity: "info", Part: name, Message: "该页几何无法完整解析，保留原稿。"})
			continue
		}
		issues = append(issues, scanIssues...)
		for _, obj := range objects {
			if obj.unsupported != "" {
				issues = append(issues, Issue{Code: "OFFICE_GEOMETRY_UNSUPPORTED", Severity: "info", Part: name, Message: "旋转、翻转或组合对象不自动改几何：" + obj.unsupported})
				continue
			}
			if geometryOverflows(obj, sw, sh) {
				issues = append(issues, Issue{Code: "OFFICE_GEOMETRY_OVERFLOW", Severity: "warning", Part: name, NodeID: bindGeomNode(nodes, name, obj), Message: fmt.Sprintf("简单对象 %s 超出画布 (%d,%d %dx%d)。", obj.kind, obj.x, obj.y, obj.w, obj.h)})
			}
		}
		issues = append(issues, geometryOverlapIssues(name, objects, nodes)...)
	}
	return issues
}

func RepairGeometryOverflow(data []byte) (GeometryReport, []byte, error) {
	p, err := readPackage(data)
	if err != nil {
		return GeometryReport{}, nil, err
	}
	sw, sh, err := deckSize(p.parts)
	if err != nil {
		return GeometryReport{Issues: geometryIssues(p.parts, nil)}, data, nil
	}
	report := GeometryReport{SlideWidth: sw, SlideHeight: sh}
	replaced := map[string][]byte{}
	for round := 1; round <= MaxGeometryRepairRounds; round++ {
		moved := 0
		for _, name := range sortedPartNames(p.parts) {
			if !isSlidePart(name) {
				continue
			}
			body := p.parts[name]
			objects, scanIssues, err := scanSlideGeometry(name, body)
			if err != nil {
				report.Issues = append(report.Issues, Issue{Code: "OFFICE_GEOMETRY_UNSUPPORTED", Severity: "info", Part: name, Message: "该页几何无法完整解析，保留原稿。"})
				continue
			}
			report.Issues = append(report.Issues, scanIssues...)
			edits := []byteEdit{}
			for _, obj := range objects {
				if obj.unsupported != "" {
					if round == 1 {
						report.Unsupported++
					}
					continue
				}
				if !geometryOverflows(obj, sw, sh) {
					continue
				}
				if round == 1 {
					report.Overflow++
				}
				if obj.w > sw || obj.h > sh || obj.offEnd <= obj.offStart {
					continue
				}
				nx, ny := clampGeometry(obj, sw, sh)
				if nx == obj.x && ny == obj.y {
					continue
				}
				rewritten, err := rewriteOff(body[obj.offStart:obj.offEnd], nx, ny)
				if err != nil {
					continue
				}
				edits = append(edits, byteEdit{start: obj.offStart, end: obj.offEnd, body: rewritten})
				moved++
				report.Repaired++
			}
			if len(edits) == 0 {
				continue
			}
			next, err := splice(body, edits)
			if err != nil {
				return report, nil, err
			}
			replaced[name] = next
			p.parts[name] = next
		}
		report.Rounds = round
		if moved == 0 {
			break
		}
	}
	if len(replaced) == 0 {
		report.Issues = geometryIssues(p.parts, nil)
		report.RemainingOverflow = countGeometryOverflow(report.Issues)
		return report, data, nil
	}
	out, err := rewritePackage(p, replaced)
	if err != nil {
		return report, nil, err
	}
	if _, err = Inspect(PPTX, out); err != nil {
		return report, nil, err
	}
	report.Issues = geometryIssues(mustParts(out), nil)
	report.RemainingOverflow = countGeometryOverflow(report.Issues)
	for name := range replaced {
		report.ChangedParts = append(report.ChangedParts, name)
	}
	sort.Strings(report.ChangedParts)
	return report, out, nil
}

func clampGeometry(obj geomObject, sw, sh int64) (int64, int64) {
	nx, ny := obj.x, obj.y
	if nx < 0 {
		nx = 0
	}
	if ny < 0 {
		ny = 0
	}
	if nx > sw-obj.w {
		nx = sw - obj.w
	}
	if ny > sh-obj.h {
		ny = sh - obj.h
	}
	return nx, ny
}

func countGeometryOverflow(issues []Issue) int {
	n := 0
	for _, issue := range issues {
		if issue.Code == "OFFICE_GEOMETRY_OVERFLOW" {
			n++
		}
	}
	return n
}

func geometryOverflows(obj geomObject, sw, sh int64) bool {
	return obj.w <= 0 || obj.h <= 0 || obj.x < 0 || obj.y < 0 || obj.x > sw || obj.y > sh || obj.w > sw-obj.x || obj.h > sh-obj.y
}

func geometryOverlapIssues(part string, objects []geomObject, nodes []Node) []Issue {
	var issues []Issue
	for i := 0; i < len(objects); i++ {
		for j := i + 1; j < len(objects); j++ {
			a, b := objects[i], objects[j]
			if a.unsupported != "" || b.unsupported != "" || !overlapCandidates(a, b) || !boxesOverlap(a, b) {
				continue
			}
			nodeID := bindGeomNode(nodes, part, a)
			if nodeID == "" {
				nodeID = bindGeomNode(nodes, part, b)
			}
			issues = append(issues, Issue{
				Code: "OFFICE_GEOMETRY_OVERLAP", Severity: "warning",
				Part: part, NodeID: nodeID,
				Message: fmt.Sprintf("文本或图片对象重叠：%s / %s", firstNonEmpty(a.name, a.kind), firstNonEmpty(b.name, b.kind)),
			})
		}
	}
	return issues
}

func overlapCandidates(a, b geomObject) bool {
	return (a.hasText || a.kind == "pic") && (b.hasText || b.kind == "pic") && (a.hasText || b.hasText)
}

func boxesOverlap(a, b geomObject) bool {
	return a.w > 0 && b.w > 0 && a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h
}

func RepairGeometryOverlap(data []byte, maxRounds int) (GeometryReport, []byte, error) {
	if maxRounds < 1 {
		return GeometryReport{}, data, nil
	}
	p, err := readPackage(data)
	if err != nil {
		return GeometryReport{}, nil, err
	}
	sw, sh, err := deckSize(p.parts)
	if err != nil {
		return GeometryReport{Issues: geometryIssues(p.parts, nil)}, data, nil
	}
	report := GeometryReport{SlideWidth: sw, SlideHeight: sh}
	replaced := map[string][]byte{}
	for round := 1; round <= maxRounds; round++ {
		moved := 0
		for _, name := range sortedPartNames(p.parts) {
			if !isSlidePart(name) {
				continue
			}
			body := p.parts[name]
			objects, _, err := scanSlideGeometry(name, body)
			if err != nil {
				continue
			}
			edits := []byteEdit{}
			used := map[int]bool{}
			for i := 0; i < len(objects); i++ {
				for j := i + 1; j < len(objects); j++ {
					a, b := objects[i], objects[j]
					if a.unsupported != "" || b.unsupported != "" || !overlapCandidates(a, b) || !boxesOverlap(a, b) {
						continue
					}
					moveIdx := j
					if b.y < a.y {
						moveIdx = i
					}
					if used[moveIdx] || objects[moveIdx].offEnd <= objects[moveIdx].offStart {
						continue
					}
					other := a
					if moveIdx == i {
						other = b
					}
					ny, ok := overlapShiftY(objects[moveIdx], other, sh)
					if !ok {
						continue
					}
					rewritten, err := rewriteOff(body[objects[moveIdx].offStart:objects[moveIdx].offEnd], objects[moveIdx].x, ny)
					if err != nil {
						continue
					}
					edits = append(edits, byteEdit{start: objects[moveIdx].offStart, end: objects[moveIdx].offEnd, body: rewritten})
					objects[moveIdx].y = ny
					used[moveIdx] = true
					moved++
					report.Repaired++
				}
			}
			if len(edits) == 0 {
				continue
			}
			next, err := splice(body, edits)
			if err != nil {
				return report, nil, err
			}
			replaced[name] = next
			p.parts[name] = next
		}
		report.Rounds = round
		if moved == 0 {
			break
		}
	}
	if len(replaced) == 0 {
		report.Issues = geometryIssues(p.parts, nil)
		return report, data, nil
	}
	out, err := rewritePackage(p, replaced)
	if err != nil {
		return report, nil, err
	}
	if _, err = Inspect(PPTX, out); err != nil {
		return report, nil, err
	}
	report.Issues = geometryIssues(mustParts(out), nil)
	for name := range replaced {
		report.ChangedParts = append(report.ChangedParts, name)
	}
	sort.Strings(report.ChangedParts)
	return report, out, nil
}

func overlapShiftY(move, other geomObject, sh int64) (int64, bool) {
	overlap := other.y + other.h - move.y
	if overlap <= 0 {
		return move.y, false
	}
	ny := move.y + overlap + 127000
	if ny < 0 || ny+move.h > sh {
		return move.y, false
	}
	return ny, true
}

func bindGeomNode(nodes []Node, part string, obj geomObject) string {
	text := strings.TrimSpace(obj.text)
	for _, n := range nodes {
		if n.Part != part || strings.TrimSpace(n.Text) == "" {
			continue
		}
		if text != "" && (strings.Contains(text, n.Text) || strings.Contains(n.Text, text)) {
			return n.ID
		}
	}
	for _, n := range nodes {
		if n.Part == part && n.ID != "" {
			return n.ID
		}
	}
	return ""
}

func isSlidePart(name string) bool {
	return strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "/_rels/")
}

func scanSlideGeometry(part string, body []byte) ([]geomObject, []Issue, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var objects []geomObject
	var issues []Issue
	stack := []*geomFrame{}
	grouped := 0
	for {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		after := int(dec.InputOffset())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, ErrFormat
		}
		switch t := tok.(type) {
		case xml.StartElement:
			f := &geomFrame{local: t.Name.Local, space: t.Name.Space}
			if t.Name.Space == presentationNS && (t.Name.Local == "sp" || t.Name.Local == "pic" || t.Name.Local == "graphicFrame" || t.Name.Local == "cxnSp" || t.Name.Local == "grpSp") {
				f.object = true
				if t.Name.Local == "grpSp" || t.Name.Local == "cxnSp" {
					f.unsupported = t.Name.Local
				}
				if grouped > 0 {
					f.unsupported = "grpSp"
				}
				if t.Name.Local == "grpSp" {
					grouped++
				}
			}
			if obj := currentGeom(stack); obj != nil {
				if t.Name.Local == "cNvPr" {
					obj.name = xmlAttr(t, "name")
				}
				if t.Name.Local == "txBody" {
					obj.hasText = true
				}
			}
			if obj := currentGeom(stack); obj != nil && geomTransformChild(stack, obj, t) {
				switch t.Name.Local {
				case "xfrm":
					for _, a := range t.Attr {
						if a.Name.Local == "rot" && a.Value != "" && a.Value != "0" && obj.unsupported == "" {
							obj.unsupported = "rotation"
						}
						if (a.Name.Local == "flipH" || a.Name.Local == "flipV") && (a.Value == "1" || a.Value == "true") && obj.unsupported == "" {
							obj.unsupported = "flip"
						}
					}
				case "off":
					x, xerr := strconv.ParseInt(xmlAttr(t, "x"), 10, 64)
					y, yerr := strconv.ParseInt(xmlAttr(t, "y"), 10, 64)
					if xerr != nil || yerr != nil || obj.hasOff {
						obj.unsupported = "invalid offset"
					}
					obj.x, obj.y = x, y
					obj.offStart = before
					obj.offEnd = after
					obj.hasOff = true
				case "ext":
					w, werr := strconv.ParseInt(xmlAttr(t, "cx"), 10, 64)
					h, herr := strconv.ParseInt(xmlAttr(t, "cy"), 10, 64)
					if werr != nil || herr != nil || obj.hasExt || w <= 0 || h <= 0 {
						obj.unsupported = "invalid extent"
					}
					obj.w, obj.h = w, h
					obj.hasExt = true
				}
			}
			stack = append(stack, f)
		case xml.CharData:
			if obj := currentGeom(stack); obj != nil && obj.hasText && len(obj.text) < 200 {
				obj.text += string(t)
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, nil, ErrFormat
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.space == presentationNS && f.local == "grpSp" && grouped > 0 {
				grouped--
			}
			if !f.object {
				continue
			}
			if f.unsupported == "" && (!f.hasOff || !f.hasExt) {
				f.unsupported = "inherited or incomplete transform"
			}
			objects = append(objects, geomObject{part: part, kind: f.local, unsupported: f.unsupported, name: f.name, text: f.text, hasText: f.hasText, x: f.x, y: f.y, w: f.w, h: f.h, offStart: f.offStart, offEnd: f.offEnd})
		}
	}
	return objects, issues, nil
}

func currentGeom(stack []*geomFrame) *geomFrame {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].object {
			return stack[i]
		}
	}
	return nil
}

// Inspect only the object's own transform. Extension-list ext elements and
// nested graphic payloads must never override its extent or become write targets.
func geomTransformChild(stack []*geomFrame, obj *geomFrame, t xml.StartElement) bool {
	index := -1
	for i, f := range stack {
		if f == obj {
			index = i
			break
		}
	}
	if index < 0 {
		return false
	}
	path := stack[index+1:]
	validTransform := func(local, space string) bool {
		if obj.local == "graphicFrame" {
			return len(path) == 0 && local == "xfrm" && space == presentationNS
		}
		return len(path) == 1 && path[0].local == "spPr" && path[0].space == presentationNS && local == "xfrm" && space == drawingNS
	}
	if t.Name.Local == "xfrm" {
		return validTransform(t.Name.Local, t.Name.Space)
	}
	if t.Name.Space != drawingNS || (t.Name.Local != "off" && t.Name.Local != "ext") || len(path) == 0 {
		return false
	}
	last := path[len(path)-1]
	path = path[:len(path)-1]
	return validTransform(last.local, last.space)
}

var geometryCoordinateAttribute = regexp.MustCompile(`([\t\r\n ]+)(x|y)([\t\r\n ]*=[\t\r\n ]*)("[^"]*"|'[^']*')`)

func rewriteOff(raw []byte, x, y int64) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, ErrFormat
	}
	start, ok := tok.(xml.StartElement)
	if !ok || start.Name.Local != "off" {
		return nil, ErrFormat
	}
	// Preserve all other attributes, prefixes and the original empty-element
	// syntax. The span contains the opening token only, not an explicit end tag.
	seen := map[string]int{}
	out := geometryCoordinateAttribute.ReplaceAllFunc(raw, func(attribute []byte) []byte {
		m := geometryCoordinateAttribute.FindSubmatch(attribute)
		key := string(m[2])
		seen[key]++
		value := x
		if key == "y" {
			value = y
		}
		return []byte(string(m[1]) + key + string(m[3]) + string(m[4][:1]) + strconv.FormatInt(value, 10) + string(m[4][:1]))
	})
	if seen["x"] != 1 || seen["y"] != 1 {
		return nil, ErrFormat
	}
	return out, nil
}

func mustParts(data []byte) map[string][]byte {
	p, err := readPackage(data)
	if err != nil {
		return map[string][]byte{}
	}
	return p.parts
}

func sortedPartNames(parts map[string][]byte) []string {
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
