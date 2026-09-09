package officestudio

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CacheMerge is a selective merge, not a LibreOffice round-trip of the source.
// Only field displays or formula cache values may change. Untouched ZIP members
// are copied in their original compressed form by rewritePackage.
type CacheMerge struct {
	Data            []byte             `json:"-"`
	ChangedParts    []string           `json:"changedParts"`
	UpdatedCells    int                `json:"updatedCells"`
	UpdatedFields   int                `json:"updatedFields"`
	Charts          *CalculationReport `json:"charts,omitempty"`
	SourceSHA256    string             `json:"sourceSha256"`
	CandidateSHA256 string             `json:"candidateSha256"`
	OutputSHA256    string             `json:"outputSha256"`
}

func MergeNativeCaches(kind Kind, original, candidate []byte) (CacheMerge, error) {
	result := CacheMerge{SourceSHA256: digest(original), CandidateSHA256: digest(candidate), ChangedParts: []string{}}
	for _, data := range [][]byte{original, candidate} {
		i, err := Inspect(kind, data)
		if err != nil {
			return result, err
		}
		if !i.RenderAllowed {
			return result, ErrReadOnly
		}
	}
	p, err := readPackage(original)
	if err != nil {
		return result, err
	}
	q, err := readPackage(candidate)
	if err != nil {
		return result, err
	}
	replaced := map[string][]byte{}
	switch kind {
	case XLSX:
		result.UpdatedCells, err = mergeWorkbookCaches(p, q, replaced)
		if err == nil {
			result.Charts = &CalculationReport{}
			_, err = refreshSheetCharts(p, replaced, result.Charts, true)
		}
	case DOCX:
		result.UpdatedFields, err = mergeWordCaches(p, q, replaced)
	default:
		err = ErrReadOnly
	}
	if err != nil {
		return result, err
	}
	if result.UpdatedCells+result.UpdatedFields == 0 {
		return result, fmt.Errorf("%w: 文件没有可安全更新的缓存", ErrReadOnly)
	}
	for name := range replaced {
		result.ChangedParts = append(result.ChangedParts, name)
	}
	sort.Strings(result.ChangedParts)
	result.Data, err = rewritePackage(p, replaced)
	if err != nil {
		return result, err
	}
	if _, err = Inspect(kind, result.Data); err != nil {
		return result, err
	}
	result.OutputSHA256 = digest(result.Data)
	return result, nil
}

func workbookParts(p packageData) (map[string]string, error) {
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
		Props struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
	}
	if xml.Unmarshal(p.parts["xl/workbook.xml"], &book) != nil {
		return nil, ErrFormat
	}
	rels, err := pictureRelationships(p.parts["xl/_rels/workbook.xml.rels"])
	if err != nil {
		return nil, err
	}
	out := map[string]string{"\x00date1904": book.Props.Date1904}
	for _, s := range book.Sheets {
		if _, ok := out[s.Name]; ok || s.Name == "" {
			return nil, ErrFormat
		}
		r, ok := rels[s.ID]
		if !ok {
			return nil, ErrFormat
		}
		name, err := relationshipTarget("xl/_rels/workbook.xml.rels", r.target)
		if err != nil {
			return nil, err
		}
		if len(p.parts[name]) == 0 {
			return nil, ErrFormat
		}
		out[s.Name] = name
	}
	return out, nil
}

func mergeWorkbookCaches(p, q packageData, replaced map[string][]byte) (int, error) {
	ops, err := workbookParts(p)
	if err != nil {
		return 0, err
	}
	nps, err := workbookParts(q)
	if err != nil {
		return 0, err
	}
	if len(ops) != len(nps) {
		return 0, ErrConflict
	}
	if (ops["\x00date1904"] == "1" || ops["\x00date1904"] == "true") != (nps["\x00date1904"] == "1" || nps["\x00date1904"] == "true") {
		return 0, ErrConflict
	}
	ps, err := sharedStrings(p.parts["xl/sharedStrings.xml"])
	if err != nil {
		return 0, err
	}
	qs, err := sharedStrings(q.parts["xl/sharedStrings.xml"])
	if err != nil {
		return 0, err
	}
	updated := 0
	for sheet, part := range ops {
		if sheet == "\x00date1904" {
			continue
		}
		other, ok := nps[sheet]
		if !ok {
			return 0, ErrConflict
		}
		body := p.parts[part]
		cells, err := spans(body, sheetNS, "c")
		if err != nil {
			return 0, err
		}
		nc, err := spans(q.parts[other], sheetNS, "c")
		if err != nil {
			return 0, err
		}
		byAddr := map[string][]byte{}
		for _, c := range nc {
			addr := attr(c.element, "r")
			if _, ok := byAddr[addr]; ok {
				return 0, ErrFormat
			}
			byAddr[addr] = q.parts[other][c.start:c.end]
		}
		edits := []byteEdit{}
		seen := map[string]bool{}
		for _, c := range cells {
			seen[attr(c.element, "r")] = true
			raw := body[c.start:c.end]
			next, ok := byAddr[attr(c.element, "r")]
			oldText, oldType, _ := cellText(raw, ps)
			if !ok {
				if oldText == "" && oldType == "number" {
					continue
				}
				return 0, ErrConflict
			}
			newText, newType, _ := cellText(next, qs)
			if oldType != "formula" {
				if !sameCellInput(oldText, oldType, newText, newType) {
					return 0, fmt.Errorf("%w: 原生读取改变了 %s!%s 的输入语义", ErrConflict, sheet, attr(c.element, "r"))
				}
				continue
			}
			if newType != "formula" || oldText != newText {
				return 0, fmt.Errorf("%w: 原生转换改变了公式，拒绝复制缓存", ErrConflict)
			}
			fs, err := spans(raw, sheetNS, "f")
			if err != nil || len(fs) != 1 {
				return 0, ErrFormat
			}
			for _, a := range fs[0].element.Attr {
				if !(a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" || a.Name.Space == "http://www.w3.org/XML/1998/namespace" && a.Name.Local == "space") {
					return 0, fmt.Errorf("%w: 共享或数组公式缓存暂不写回", ErrReadOnly)
				}
			}
			patched, err := formulaCacheCell(raw, next)
			if err != nil {
				return 0, err
			}
			edits = append(edits, byteEdit{start: c.start, end: c.end, body: patched})
			updated++
		}
		for addr, raw := range byAddr {
			if !seen[addr] {
				value, typ, _ := cellText(raw, qs)
				if value != "" || typ == "formula" {
					return 0, fmt.Errorf("%w: 原生计算新增单元格 %s!%s，不能只复制缓存", ErrReadOnly, sheet, addr)
				}
			}
		}
		if len(edits) > 0 {
			replaced[part], err = splice(body, edits)
			if err != nil {
				return 0, err
			}
		}
	}
	return updated, nil
}

func sameCellInput(a, at, b, bt string) bool {
	if at == "richtext" {
		at = "text"
	}
	if bt == "richtext" {
		bt = "text"
	}
	if at != bt {
		return false
	}
	if at == "number" && a != "" && b != "" {
		x, xok := nativeNumber(a)
		y, yok := nativeNumber(b)
		return xok && yok && x.Cmp(y) == 0
	}
	return a == b
}

var nativeDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[Ee]([+-]?[0-9]{1,4}))?$`)

func nativeNumber(value string) (*big.Rat, bool) {
	// OOXML decimal numbers are not arbitrary big.Rat syntax. Bound exponents
	// before allocating powers of ten, including in an imported candidate.
	if len(value) > 256 {
		return nil, false
	}
	m := nativeDecimal.FindStringSubmatch(value)
	if m == nil {
		return nil, false
	}
	if m[1] != "" {
		exponent, err := strconv.Atoi(m[1])
		if err != nil || exponent < -324 || exponent > 308 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(value)
}

func formulaCacheCell(raw, next []byte) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		t, e := dec.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, ErrFormat
		}
		switch n := t.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && n.Name.Local != "f" && n.Name.Local != "v" {
				return nil, ErrReadOnly
			}
		case xml.EndElement:
			depth--
		}
	}
	var n struct {
		Type  string  `xml:"t,attr"`
		Value *string `xml:"v"`
	}
	if xml.Unmarshal(next, &n) != nil || n.Value == nil {
		return nil, ErrFormat
	}
	switch n.Type {
	case "", "n":
		if _, ok := nativeNumber(*n.Value); !ok {
			return nil, ErrFormat
		}
	case "b":
		if *n.Value != "0" && *n.Value != "1" {
			return nil, ErrFormat
		}
	case "str":
		if !validText(*n.Value) {
			return nil, ErrFormat
		}
	default:
		return nil, fmt.Errorf("%w: 原生计算存在错误或未支持的缓存类型", ErrReadOnly)
	}
	cs, err := spans(raw, sheetNS, "c")
	if err != nil || len(cs) != 1 {
		return nil, ErrFormat
	}
	c := cs[0]
	open := cellTypeAttr.ReplaceAll(raw[c.start:c.innerStart], nil)
	if n.Type != "" && n.Type != "n" {
		open = append(append([]byte{}, open[:len(open)-1]...), []byte(` t="`+n.Type+`">`)...)
	}
	vs, err := spans(raw, sheetNS, "v")
	if err != nil || len(vs) > 1 {
		return nil, ErrFormat
	}
	cache := []byte(`<v xmlns="` + sheetNS + `">` + escapeText(*n.Value) + `</v>`)
	edits := []byteEdit{{start: c.start, end: c.innerStart, body: open}}
	if len(vs) == 1 {
		edits = append(edits, byteEdit{start: vs[0].start, end: vs[0].end, body: cache})
	} else {
		edits = append(edits, byteEdit{start: c.innerEnd, end: c.innerEnd, body: cache})
	}
	return splice(raw, edits)
}

// Imported complex fields stay read-only. Simple fields generated by this
// product retain their exact instruction and parent styles; only display runs
// are refreshed from the native candidate's matched field result.
func mergeWordCaches(p, q packageData, replaced map[string][]byte) (int, error) {
	stories, err := matchNativeWordStories(p, q)
	if err != nil {
		return 0, err
	}
	count := 0
	for part, body := range p.parts {
		if !isNodePart(DOCX, part) {
			continue
		}
		if bytes.Contains(body, []byte(":fldChar")) {
			return 0, fmt.Errorf("%w: 复杂 Word 域保留原稿，请在目标软件更新", ErrReadOnly)
		}
		fields, err := spans(body, wordNS, "fldSimple")
		if err != nil {
			return 0, err
		}
		if len(fields) == 0 {
			continue
		}
		values, plain, err := matchingWordFieldResult(q, stories[part])
		if err != nil {
			return 0, err
		}
		_, originalPlain, err := wordFieldValues(body)
		if err != nil || plain != originalPlain {
			return 0, fmt.Errorf("%w: 域所在正文或页眉页脚已变化", ErrConflict)
		}
		if len(values) != len(fields) {
			return 0, fmt.Errorf("%w: 部件 %s 的域数量改变（%d/%d）", ErrConflict, part, len(values), len(fields))
		}
		edits := []byteEdit{}
		for i, f := range fields {
			code := strings.Fields(strings.ToUpper(attr(f.element, "instr")))
			if len(code) == 0 {
				return 0, ErrFormat
			}
			if code[0] != "TOC" && code[0] != "PAGE" && code[0] != "NUMPAGES" && code[0] != "SECTIONPAGES" {
				return 0, ErrReadOnly
			}
			if values[i].code != code[0] || strings.TrimSpace(values[i].value) == "" {
				return 0, fmt.Errorf("%w: 部件 %s 的第 %d 个 %s 域结果未匹配", ErrConflict, part, i+1, code[0])
			}
			properties, err := simpleFieldRunProperties(body[f.innerStart:f.innerEnd])
			if err != nil {
				return 0, err
			}
			var display strings.Builder
			for j, line := range strings.Split(strings.Trim(values[i].value, "\n"), "\n") {
				if j > 0 {
					display.WriteString(`<w:r xmlns:w="` + wordNS + `"><w:br/></w:r>`)
				}
				// Tabs are layout elements in WordprocessingML. A literal tab
				// inside w:t is collapsed by Word and loses TOC page alignment.
				for k, segment := range strings.Split(line, "\t") {
					if k > 0 {
						display.WriteString(`<w:r xmlns:w="` + wordNS + `">` + properties + `<w:tab/></w:r>`)
					}
					if segment != "" {
						display.WriteString(`<w:r xmlns:w="` + wordNS + `">` + properties + `<w:t xml:space="preserve">` + escapeText(segment) + `</w:t></w:r>`)
					}
				}
			}
			edits = append(edits, byteEdit{start: f.innerStart, end: f.innerEnd, body: []byte(display.String())})
			count++
		}
		replaced[part], err = splice(body, edits)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

type nativeWordField struct{ code, value string }

func wordFieldValues(body []byte) ([]nativeWordField, string, error) {
	type state struct {
		instruction, display strings.Builder
		collecting           bool
		simple               bool
	}
	stack := []*state{}
	values := []nativeWordField{}
	var plain strings.Builder
	inText, inInstruction := false, false
	inRun := false
	finish := func() error {
		if len(stack) == 0 {
			return ErrFormat
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if len(stack) > 0 {
			stack[len(stack)-1].display.WriteString(f.display.String())
			return nil
		}
		words := strings.Fields(strings.ToUpper(f.instruction.String()))
		if len(words) == 0 {
			return ErrFormat
		}
		values = append(values, nativeWordField{code: words[0], value: f.display.String()})
		return nil
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", ErrFormat
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Space != wordNS {
				continue
			}
			switch t.Name.Local {
			case "r":
				inRun = true
			case "t":
				inText = true
			case "instrText":
				inInstruction = true
			case "fldSimple":
				s := &state{simple: true}
				s.instruction.WriteString(attr(t, "instr"))
				stack = append(stack, s)
			case "fldChar":
				switch attr(t, "fldCharType") {
				case "begin":
					stack = append(stack, &state{collecting: true})
				case "separate":
					if len(stack) == 0 {
						return nil, "", ErrFormat
					}
					stack[len(stack)-1].collecting = false
				case "end":
					if err := finish(); err != nil {
						return nil, "", err
					}
				}
			case "tab":
				// Paragraph tab-stop definitions share this element name but
				// are not displayed characters in a complex TOC result.
				if inRun && len(stack) > 0 && !stack[len(stack)-1].collecting {
					stack[len(stack)-1].display.WriteByte('\t')
				}
			case "br":
				if inRun && len(stack) > 0 && !stack[len(stack)-1].collecting {
					stack[len(stack)-1].display.WriteByte('\n')
				}
			}
			if len(stack) > 64 || len(values) > 1000 {
				return nil, "", ErrLimit
			}
		case xml.CharData:
			if inInstruction && len(stack) > 0 && stack[len(stack)-1].collecting {
				stack[len(stack)-1].instruction.Write(t)
			}
			if inText {
				if len(stack) == 0 {
					plain.Write(t)
				} else if !stack[len(stack)-1].collecting {
					stack[len(stack)-1].display.Write(t)
				}
			}
		case xml.EndElement:
			if t.Name.Space != wordNS {
				continue
			}
			switch t.Name.Local {
			case "r":
				inRun = false
			case "t":
				inText = false
			case "instrText":
				inInstruction = false
			case "fldSimple":
				if len(stack) == 0 || !stack[len(stack)-1].simple {
					return nil, "", ErrFormat
				}
				if err := finish(); err != nil {
					return nil, "", err
				}
			case "p":
				if len(stack) > 0 && !stack[len(stack)-1].collecting {
					stack[len(stack)-1].display.WriteByte('\n')
				}
			}
		}
	}
	if len(stack) != 0 {
		return nil, "", ErrFormat
	}
	return values, plain.String(), nil
}
