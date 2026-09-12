package officestudio

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	wordNS    = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	drawingNS = "http://schemas.openxmlformats.org/drawingml/2006/main"
	sheetNS   = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
)

type nodeLocation struct {
	node                             Node
	start, innerStart, innerEnd, end int
}

func scanNodes(kind Kind, part string, body []byte, shared []sharedStringValue) ([]nodeLocation, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	depth, ordinal := 0, 0
	var active *nodeLocation
	activeDepth := 0
	var text strings.Builder
	var ancestors []string
	locations := []nodeLocation{}
	seen := map[string]bool{}
	// Fields and content controls depend on metadata outside a text run. Until
	// a field-aware patcher exists, preserve all such content as read-only.
	complexWord := kind == DOCX && (bytes.Contains(body, []byte(":fldChar")) || bytes.Contains(body, []byte(":sdt")))
	for {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		after := int(dec.InputOffset())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			candidate := kind == DOCX && t.Name.Space == wordNS && t.Name.Local == "t" || kind == PPTX && t.Name.Space == drawingNS && t.Name.Local == "t" || kind == XLSX && t.Name.Space == sheetNS && t.Name.Local == "c"
			if active == nil && candidate {
				ordinal++
				locator := "text:" + strconv.Itoa(ordinal)
				if kind == XLSX {
					for _, a := range t.Attr {
						if a.Name.Local == "r" {
							locator = "cell:" + a.Value
						}
					}
				}
				id := "node_" + digest([]byte(part + "\x00" + locator))[:24]
				if seen[id] {
					return nil, fmt.Errorf("%w: duplicate node locator", ErrFormat)
				}
				seen[id] = true
				n := Node{ID: id, Part: part, Kind: "text", Ordinal: ordinal, Editable: !complexWord, Locator: locator}
				for _, a := range ancestors {
					if a == "del" || a == "fldSimple" || a == "AlternateContent" {
						n.Editable = false
					}
				}
				active = &nodeLocation{node: n, start: before, innerStart: after}
				activeDepth = depth
				text.Reset()
			}
			ancestors = append(ancestors, t.Name.Local)
		case xml.CharData:
			if active != nil {
				text.Write(t)
			}
		case xml.EndElement:
			if active != nil && depth == activeDepth {
				active.innerEnd = before
				active.end = after
				raw := body[active.start:active.end]
				active.node.Digest = digest(raw)
				active.node.Text = text.String()
				if kind == XLSX {
					cell, cellType, simple := cellText(raw, shared)
					active.node.Text = cell
					active.node.Kind = "cell:" + cellType
					active.node.Editable = simple
					if bytes.Contains(raw, []byte("extLst")) {
						active.node.Editable = false
					}
				}
				if active.innerEnd < active.innerStart || bytes.HasSuffix(body[active.start:active.innerStart], []byte("/>")) {
					active.node.Editable = false
				}
				locations = append(locations, *active)
				active = nil
				if len(locations) > MaxNodes {
					return nil, ErrLimit
				}
			}
			depth--
			ancestors = ancestors[:len(ancestors)-1]
		}
	}
	return locations, nil
}

var spaceAttr = regexp.MustCompile(`\s+xml:space\s*=\s*(?:"[^"]*"|'[^']*')`)
var cellTypeAttr = regexp.MustCompile(`\s+t\s*=\s*(?:"[^"]*"|'[^']*')`)

func validText(s string) bool {
	if !utf8.ValidString(s) || len(s) > 1<<20 {
		return false
	}
	for _, r := range s {
		if !(r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF) {
			return false
		}
	}
	return true
}

func escapeText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Patch edits only explicit, supported text nodes. It preserves all unrelated
// ZIP payloads, run styles, relationships, notes and unsupported features. A
// different base digest, duplicate operation or unsupported node fails before
// producing any candidate output; callers publish a new immutable version.
func Patch(data []byte, req PatchRequest) (PatchResult, error) {
	if req.BaseSHA256 == "" || digest(data) != req.BaseSHA256 {
		return PatchResult{}, ErrConflict
	}
	if len(req.Images) > 0 {
		return patchImages(data, req)
	}
	if len(req.Charts) > 0 {
		return patchCharts(data, req)
	}
	if req.Kind == XLSX || len(req.Ranges) > 0 {
		return patchRanges(data, req)
	}
	if len(req.Operations) == 0 || len(req.Operations) > 1000 {
		return PatchResult{}, ErrLimit
	}
	before, err := Inspect(req.Kind, data)
	if err != nil {
		return PatchResult{}, err
	}
	if !before.RenderAllowed || before.Editability == "readonly" || before.Editability == "blocked" {
		return PatchResult{}, ErrReadOnly
	}
	p, err := readPackage(data)
	if err != nil {
		return PatchResult{}, err
	}
	shared, err := sharedStrings(p.parts["xl/sharedStrings.xml"])
	if err != nil {
		return PatchResult{}, err
	}
	byID := map[string]nodeLocation{}
	for part, body := range p.parts {
		if isNodePart(req.Kind, part) {
			nodes, err := scanNodes(req.Kind, part, body, shared)
			if err != nil {
				return PatchResult{}, err
			}
			for _, n := range nodes {
				byID[n.node.ID] = n
			}
		}
	}
	type replacement struct {
		loc  nodeLocation
		text string
	}
	changes := map[string][]replacement{}
	seen := map[string]bool{}
	for _, op := range req.Operations {
		if seen[op.NodeID] {
			return PatchResult{}, fmt.Errorf("%w: duplicate patch target", ErrConflict)
		}
		seen[op.NodeID] = true
		loc, ok := byID[op.NodeID]
		if !ok || op.ExpectedDigest == "" || op.ExpectedDigest != loc.node.Digest {
			return PatchResult{}, ErrConflict
		}
		if !loc.node.Editable {
			return PatchResult{}, ErrReadOnly
		}
		if !validText(op.Text) {
			return PatchResult{}, fmt.Errorf("%w: invalid replacement text", ErrFormat)
		}
		changes[loc.node.Part] = append(changes[loc.node.Part], replacement{loc: loc, text: op.Text})
	}
	replaced := map[string][]byte{}
	for part, ops := range changes {
		sort.Slice(ops, func(i, j int) bool { return ops[i].loc.start < ops[j].loc.start })
		body := p.parts[part]
		var b bytes.Buffer
		last := 0
		for _, op := range ops {
			loc := op.loc
			b.Write(body[last:loc.start])
			startTag := string(body[loc.start:loc.innerStart])
			endTag := string(body[loc.innerEnd:loc.end])
			if req.Kind == XLSX {
				// Convert a shared string reference to a local inline string. Other
				// cells sharing the old entry are untouched; styles remain on <c>.
				startTag = cellTypeAttr.ReplaceAllString(startTag, "")
				startTag = strings.TrimSuffix(startTag, ">") + ` t="inlineStr">`
				b.WriteString(startTag)
				b.WriteString(`<is xmlns="` + sheetNS + `"><t xml:space="preserve">`)
				b.WriteString(escapeText(op.text))
				b.WriteString(`</t></is>`)
				b.WriteString(endTag)
			} else {
				startTag = spaceAttr.ReplaceAllString(startTag, "")
				startTag = strings.TrimSuffix(startTag, ">") + ` xml:space="preserve">`
				b.WriteString(startTag)
				b.WriteString(escapeText(op.text))
				b.WriteString(endTag)
			}
			last = loc.end
		}
		b.Write(body[last:])
		if b.Len() > MaxPartBytes {
			return PatchResult{}, ErrLimit
		}
		replaced[part] = b.Bytes()
	}
	output, err := rewritePackage(p, replaced)
	if err != nil {
		return PatchResult{}, err
	}
	after, err := Inspect(req.Kind, output)
	if err != nil {
		return PatchResult{}, err
	}
	if !after.RenderAllowed {
		return PatchResult{}, fmt.Errorf("%w: candidate failed package checks", ErrFormat)
	}
	partHashes := map[string]string{}
	for _, p := range before.Parts {
		partHashes[p.Name] = p.SHA256
	}
	for _, p := range after.Parts {
		if _, changed := replaced[p.Name]; !changed && partHashes[p.Name] != p.SHA256 {
			return PatchResult{}, fmt.Errorf("%w: untouched part changed", ErrConflict)
		}
	}
	// Node identity and untouched node payloads must survive even within a
	// modified part. This catches accidental scope expansion and run loss.
	oldNodes := map[string]Node{}
	for _, n := range before.Nodes {
		oldNodes[n.ID] = n
	}
	if len(before.Nodes) != len(after.Nodes) {
		return PatchResult{}, ErrConflict
	}
	for _, n := range after.Nodes {
		old, ok := oldNodes[n.ID]
		if !ok || !seen[n.ID] && old.Digest != n.Digest {
			return PatchResult{}, fmt.Errorf("%w: unrelated node changed", ErrConflict)
		}
	}
	changedParts := make([]string, 0, len(replaced))
	for name := range replaced {
		changedParts = append(changedParts, name)
	}
	sort.Strings(changedParts)
	return PatchResult{Data: output, Inspection: after, ChangedParts: changedParts}, nil
}

func rewritePackage(p packageData, replaced map[string][]byte) ([]byte, error) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	written := map[string]bool{}
	for _, f := range p.reader.File {
		written[f.Name] = true
		body, changed := replaced[f.Name]
		if !changed {
			if err := zw.Copy(f); err != nil {
				return nil, err
			}
			continue
		}
		header := f.FileHeader
		header.CRC32 = 0
		header.CompressedSize64 = 0
		header.UncompressedSize64 = 0
		w, err := zw.CreateHeader(&header)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(body); err != nil {
			return nil, err
		}
	}
	var added []string
	for name := range replaced {
		if !written[name] {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	for _, name := range added {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(replaced[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if b.Len() > MaxInputBytes {
		return nil, ErrLimit
	}
	return b.Bytes(), nil
}

// Validate reports exactly the evidence available from the pure Go content
// checker. Native rendering and a full spreadsheet calculation are separate
// required checks, so a valid ZIP alone cannot produce a green final status.
func Validate(kind Kind, data []byte) (Validation, error) {
	return ValidateBrand(kind, data, DefaultBrand())
}

func ValidateBrand(kind Kind, data []byte, brand BrandProfile) (Validation, error) {
	i, err := Inspect(kind, data)
	if err == nil && kind == PPTX {
		i = WithBrandLogoIssues(i, brand)
	}
	if err != nil {
		return Validation{Status: "blocked", Checks: []Check{{ID: "package", Status: "blocked", Message: err.Error()}}, Issues: []Issue{}}, err
	}
	v := Validation{Status: "partial", Checks: []Check{{ID: "package", Status: "passed", Message: "已检查容器结构、XML、资源引用和安全边界。"}, {ID: "native_render", Status: "missing", Message: "尚无该版本在目标软件中的实际渲染证据。"}}, Issues: i.Issues}
	for _, issue := range i.Issues {
		if issue.Severity == "blocked" {
			v.Status = "blocked"
			v.Checks = append(v.Checks, Check{ID: "content_safety", Status: "blocked", Message: issue.Message})
			break
		}
	}
	if kind == PDF {
		checks := []Check{
			{ID: "pdf_structure", Status: "passed", Message: "已验证 PDF 文件头与结束标记；对象、字体和页面需隔离预览器阅读。"},
			IndependentPDFCheck(data),
			IndependentPDFACheck(data),
		}
		for _, c := range v.Checks {
			if c.ID == "content_safety" {
				checks = append(checks, c)
			}
		}
		v.Checks = checks
	}
	if kind == XLSX {
		formulaCount := 0
		for _, n := range i.Nodes {
			if n.Kind == "cell:formula" {
				formulaCount++
			}
		}
		if formulaCount > 0 {
			v.Checks = append(v.Checks, Check{ID: "full_recalculation", Status: "missing", Message: fmt.Sprintf("发现 %d 个公式；未把缓存值或部分公式求值当作完整重算。", formulaCount)})
		}
	}
	if i.Structure != nil && i.Structure.NeedsFieldUpdate {
		v.Checks = append(v.Checks, Check{ID: "fields_update", Status: "missing", Message: "文档包含实际 Word 域；目录、页码或引用等域缓存尚需目标软件更新并验证。"})
	}
	if kind == PPTX {
		v.Checks = append(v.Checks, geometryCheck(i.Issues))
		v.Checks = append(v.Checks, RasterChecksFromInspection(i)...)
	}
	return v, nil
}
