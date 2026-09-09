package officestudio

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrLimit    = errors.New("office: content limit exceeded")
	ErrFormat   = errors.New("office: invalid or unsupported document")
	ErrConflict = errors.New("office: source version or node changed")
	ErrReadOnly = errors.New("office: this content cannot be safely modified")
)

type packageData struct {
	reader *zip.Reader
	parts  map[string][]byte
}

func digest(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func readPackage(data []byte) (packageData, error) {
	if len(data) == 0 || len(data) > MaxInputBytes {
		return packageData{}, ErrLimit
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return packageData{}, fmt.Errorf("%w: ZIP: %v", ErrFormat, err)
	}
	if len(zr.File) > MaxParts {
		return packageData{}, ErrLimit
	}
	p := packageData{reader: zr, parts: make(map[string][]byte, len(zr.File))}
	seen := map[string]bool{}
	total := uint64(0)
	for _, f := range zr.File {
		name := f.Name
		if f.FileInfo().IsDir() {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" || strings.ContainsAny(name, "\\:\x00") || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") || name == ".." || f.Mode()&fs.ModeType&^fs.ModeDir != 0 {
			return packageData{}, fmt.Errorf("%w: unsafe package entry %q", ErrFormat, name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return packageData{}, fmt.Errorf("%w: duplicate package entry", ErrFormat)
		}
		seen[key] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if f.Flags&1 != 0 {
			return packageData{}, fmt.Errorf("%w: encrypted ZIP", ErrFormat)
		}
		if f.UncompressedSize64 > MaxPartBytes || total+f.UncompressedSize64 > MaxExpandedBytes {
			return packageData{}, ErrLimit
		}
		total += f.UncompressedSize64
		if f.UncompressedSize64 > 1<<20 && f.UncompressedSize64/max(f.CompressedSize64, 1) > 1000 {
			return packageData{}, ErrLimit
		}
		r, err := f.Open()
		if err != nil {
			return packageData{}, fmt.Errorf("%w: ZIP entry", ErrFormat)
		}
		body, readErr := io.ReadAll(io.LimitReader(r, MaxPartBytes+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			return packageData{}, fmt.Errorf("%w: unreadable ZIP entry", ErrFormat)
		}
		if len(body) > MaxPartBytes || uint64(len(body)) != f.UncompressedSize64 {
			return packageData{}, ErrLimit
		}
		p.parts[name] = body
	}
	return p, nil
}

// Inspect does not extract paths, execute macros, follow relationships or
// reconstruct a source file. A blocked result is still useful for explaining
// why the immutable original can only be downloaded.
func Inspect(kind Kind, data []byte) (Inspection, error) {
	out := Inspection{Kind: kind, SHA256: digest(data), Size: len(data), RenderAllowed: true, Editability: "partial", Nodes: []Node{}, Parts: []Part{}, Issues: []Issue{}}
	if len(data) == 0 || len(data) > MaxInputBytes {
		return out, ErrLimit
	}
	if kind == PDF {
		return inspectPDF(out, data)
	}
	mainPart := map[Kind]string{DOCX: "word/document.xml", PPTX: "ppt/presentation.xml", XLSX: "xl/workbook.xml"}[kind]
	if mainPart == "" {
		return out, ErrFormat
	}
	p, err := readPackage(data)
	if err != nil {
		return out, err
	}
	for _, required := range []string{"[Content_Types].xml", "_rels/.rels", mainPart} {
		if len(p.parts[required]) == 0 {
			return out, fmt.Errorf("%w: missing %s", ErrFormat, required)
		}
	}
	names := make([]string, 0, len(p.parts))
	for name := range p.parts {
		names = append(names, name)
	}
	sort.Strings(names)
	imageBudget := &pictureScanBudget{}
	safeEmbeddings := map[string]bool{}
	if kind == PPTX {
		var embeddedIssues []Issue
		safeEmbeddings, embeddedIssues = safeChartWorkbooks(p.parts)
		out.Issues = append(out.Issues, embeddedIssues...)
	}
	for _, name := range names {
		body := p.parts[name]
		out.Parts = append(out.Parts, Part{Name: name, SHA256: digest(body), Size: len(body)})
		lower := strings.ToLower(name)
		if strings.Contains(lower, "vbaproject") || strings.Contains(lower, "/activex/") || strings.Contains(lower, "/embeddings/") && !safeEmbeddings[name] || strings.Contains(lower, "/macrosheets/") || strings.Contains(lower, "/dialogsheets/") {
			out.Issues = append(out.Issues, Issue{Code: "OFFICE_ACTIVE_CONTENT", Severity: "blocked", Message: "文件含宏、嵌入对象或活动控件，保留原文件，禁止自动渲染与修改。", Part: name})
		}
		if strings.HasPrefix(lower, "_xmlsignatures/") {
			out.Issues = append(out.Issues, Issue{Code: "OFFICE_SIGNED_PACKAGE", Severity: "blocked", Message: "修改会破坏原文件的数字签名，当前版本只读。", Part: name})
		}
		if strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels") {
			issues, err := inspectXML(name, body, p.parts)
			if err != nil {
				return out, err
			}
			out.Issues = append(out.Issues, issues...)
		}
	}
	if err := verifyMainPart(kind, p.parts); err != nil {
		return out, err
	}
	out.Structure, err = inspectStructure(kind, p.parts)
	if err != nil {
		return out, err
	}
	shared, err := sharedStrings(p.parts["xl/sharedStrings.xml"])
	if err != nil {
		return out, err
	}
	for _, name := range names {
		if isNodePart(kind, name) {
			locations, err := scanNodes(kind, name, p.parts[name], shared)
			if err != nil {
				return out, err
			}
			for _, loc := range locations {
				out.Nodes = append(out.Nodes, loc.node)
			}
			if kind == PPTX {
				pictures, issues, err := scanPictures(name, p.parts, imageBudget)
				if err != nil {
					return out, err
				}
				for _, pic := range pictures {
					out.Nodes = append(out.Nodes, pic.node)
				}
				out.Issues = append(out.Issues, issues...)
				charts, chartIssues, err := scanCharts(name, p.parts)
				if err != nil {
					return out, err
				}
				for _, chart := range charts {
					out.Nodes = append(out.Nodes, chart.node)
				}
				out.Issues = append(out.Issues, chartIssues...)
			}
			if kind == XLSX {
				charts, issues, err := scanSheetCharts(name, p.parts)
				if err != nil {
					return out, err
				}
				out.Nodes = append(out.Nodes, charts...)
				out.Issues = append(out.Issues, issues...)
			}
			if len(out.Nodes) > MaxNodes {
				return out, ErrLimit
			}
		}
	}
	if kind == PPTX {
		out.Issues = append(out.Issues, geometryIssues(p.parts)...)
	}
	for _, issue := range out.Issues {
		if issue.Severity == "blocked" {
			out.RenderAllowed = false
			out.Editability = "blocked"
		} else if issue.Code == "OFFICE_DOCUMENT_PROTECTED" && out.Editability != "blocked" {
			out.Editability = "readonly"
		}
	}
	if !out.RenderAllowed || out.Editability == "readonly" {
		for i := range out.Nodes {
			out.Nodes[i].Editable = false
		}
	}
	if len(out.Nodes) == 0 && out.Editability != "blocked" && kind != XLSX {
		out.Editability = "readonly"
	}
	var preview strings.Builder
	for _, node := range out.Nodes {
		if preview.Len() > 60000 {
			preview.WriteString("\n…")
			break
		}
		if node.Text != "" {
			preview.WriteString(node.Text)
			preview.WriteByte('\n')
		}
	}
	out.Preview = preview.String()
	return out, nil
}

func isNodePart(kind Kind, name string) bool {
	switch kind {
	case DOCX:
		return name == "word/document.xml" || (strings.HasPrefix(name, "word/header") || strings.HasPrefix(name, "word/footer")) && strings.HasSuffix(name, ".xml")
	case PPTX:
		return strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "/_rels/")
	case XLSX:
		return strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "/_rels/")
	}
	return false
}

func inspectXML(name string, body []byte, parts map[string][]byte) ([]Issue, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	depth, tokens, roots := 0, 0, 0
	issues := []Issue{}
	var formula strings.Builder
	inFormula := false
	var wordField strings.Builder
	inWordField := false
	var compoundFields []*strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: malformed XML %s", ErrFormat, name)
		}
		tokens++
		if tokens > 2_000_000 {
			return nil, ErrLimit
		}
		switch t := tok.(type) {
		case xml.Directive:
			return nil, fmt.Errorf("%w: XML directives are disabled", ErrFormat)
		case xml.ProcInst:
			if strings.EqualFold(t.Target, "xml-stylesheet") {
				issues = append(issues, Issue{Code: "OFFICE_EXTERNAL_STYLESHEET", Severity: "blocked", Message: "XML 引用了外部样式处理指令，禁止自动渲染。", Part: name})
			}
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return nil, ErrFormat
				}
			}
			depth++
			if depth > 128 {
				return nil, ErrLimit
			}
			local := strings.ToLower(t.Name.Local)
			if t.Name.Space == sheetNS && (local == "f" || local == "definedname") || t.Name.Space == chartNS && local == "f" {
				inFormula = true
				formula.Reset()
			}
			if t.Name.Space == wordNS && local == "instrtext" {
				inWordField = true
				wordField.Reset()
			}
			if t.Name.Space == wordNS && local == "fldchar" {
				for _, a := range t.Attr {
					if a.Name.Local != "fldCharType" {
						continue
					}
					switch a.Value {
					case "begin":
						if len(compoundFields) >= 64 {
							return nil, ErrLimit
						}
						compoundFields = append(compoundFields, &strings.Builder{})
					case "end":
						if len(compoundFields) > 0 {
							last := compoundFields[len(compoundFields)-1]
							if activeWordField.MatchString(last.String()) {
								issues = append(issues, Issue{Code: "OFFICE_ACTIVE_FIELD", Severity: "blocked", Message: "Word 复合字段可执行外部动作，禁止自动渲染。", Part: name})
							}
							compoundFields = compoundFields[:len(compoundFields)-1]
						}
					}
				}
			}
			if t.Name.Space == wordNS && local == "fldsimple" {
				for _, a := range t.Attr {
					if a.Name.Local == "instr" && activeWordField.MatchString(a.Value) {
						issues = append(issues, Issue{Code: "OFFICE_ACTIVE_FIELD", Severity: "blocked", Message: "Word 字段可访问外部文档或执行动态动作，禁止自动渲染。", Part: name})
					}
				}
			}
			if t.Name.Space == wordNS && local == "documentprotection" || t.Name.Space == sheetNS && (local == "workbookprotection" || local == "sheetprotection") {
				issues = append(issues, Issue{Code: "OFFICE_DOCUMENT_PROTECTED", Severity: "info", Message: "文件设置了编辑保护，保留保护设置，仅提供只读检查与预览。", Part: name})
			}
			if local == "oleobject" || local == "control" || local == "ddeitem" || local == "ddelink" || local == "externalbook" || local == "connection" || local == "altchunk" {
				issues = append(issues, Issue{Code: "OFFICE_ACTIVE_CONTENT", Severity: "blocked", Message: "文件含外部数据、嵌入内容或动态对象，当前仅保留原件。", Part: name})
			}
			for _, a := range t.Attr {
				if a.Name.Local == "ContentType" && strings.Contains(strings.ToLower(a.Value), "macroenabled") {
					issues = append(issues, Issue{Code: "OFFICE_MACRO_CONTENT_TYPE", Severity: "blocked", Message: "文件声明了宏格式，禁止自动运行或修改。", Part: name})
				}
			}
			if local == "relationship" {
				target, mode, relType := "", "", ""
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "Target":
						target = a.Value
					case "TargetMode":
						mode = a.Value
					case "Type":
						relType = a.Value
					}
				}
				if strings.EqualFold(mode, "External") || strings.Contains(strings.ToLower(relType), "externallink") {
					issues = append(issues, Issue{Code: "OFFICE_EXTERNAL_RELATIONSHIP", Severity: "blocked", Message: "检测到外部链接，未访问链接；安全导入保留原件，禁止自动渲染和修改。", Part: name})
					continue
				}
				if target != "" {
					resolved, err := relationshipTarget(name, target)
					if err != nil {
						return nil, err
					}
					if _, ok := parts[resolved]; !ok {
						issues = append(issues, Issue{Code: "OFFICE_MISSING_PART", Severity: "blocked", Message: "文件引用的内部资源不存在，不能认定可正常打开。", Part: name})
					}
				}
			}
		case xml.CharData:
			if inFormula {
				formula.Write(t)
			}
			if inWordField {
				wordField.Write(t)
				if len(compoundFields) > 0 {
					compoundFields[len(compoundFields)-1].Write(t)
				}
			}
		case xml.EndElement:
			if inWordField && t.Name.Space == wordNS && t.Name.Local == "instrText" {
				if activeWordField.MatchString(wordField.String()) {
					issues = append(issues, Issue{Code: "OFFICE_ACTIVE_FIELD", Severity: "blocked", Message: "Word 字段可访问外部文档或执行动态动作，禁止自动渲染。", Part: name})
				}
				inWordField = false
			}
			if inFormula && (t.Name.Space == sheetNS && (t.Name.Local == "f" || t.Name.Local == "definedName") || t.Name.Space == chartNS && t.Name.Local == "f") {
				if dangerousFormula.MatchString(formula.String()) {
					issues = append(issues, Issue{Code: "OFFICE_ACTIVE_FORMULA", Severity: "blocked", Message: "文件含可访问外部服务或启动外部动作的公式，禁止自动渲染与重算。", Part: name})
				}
				inFormula = false
			}
			depth--
		}
	}
	if depth != 0 || roots != 1 {
		return nil, ErrFormat
	}
	for _, field := range compoundFields {
		if activeWordField.MatchString(field.String()) {
			issues = append(issues, Issue{Code: "OFFICE_ACTIVE_FIELD", Severity: "blocked", Message: "未闭合的 Word 复合字段含外部动作，禁止自动渲染。", Part: name})
		}
	}
	return issues, nil
}

var activeWordField = regexp.MustCompile(`(?i)\b(?:DDEAUTO|DDE|INCLUDETEXT|INCLUDEPICTURE|LINK|DATABASE|IMPORT|HYPERLINK)\b`)

func verifyMainPart(kind Kind, parts map[string][]byte) error {
	name, namespace, local := "", "", ""
	switch kind {
	case DOCX:
		name, namespace, local = "word/document.xml", wordNS, "document"
	case PPTX:
		name, namespace, local = "ppt/presentation.xml", "http://schemas.openxmlformats.org/presentationml/2006/main", "presentation"
	case XLSX:
		name, namespace, local = "xl/workbook.xml", sheetNS, "workbook"
	}
	dec := xml.NewDecoder(bytes.NewReader(parts[name]))
	valid := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if start, ok := tok.(xml.StartElement); ok {
			valid = start.Name.Local == local && start.Name.Space == namespace
			break
		}
	}
	if !valid {
		return fmt.Errorf("%w: main part does not match document type", ErrFormat)
	}
	var relationships struct {
		Items []struct {
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(parts["_rels/.rels"], &relationships); err != nil {
		return ErrFormat
	}
	found := false
	for _, r := range relationships.Items {
		if strings.HasSuffix(r.Type, "/officeDocument") {
			target, err := relationshipTarget("_rels/.rels", r.Target)
			if err != nil || target != name || found {
				return fmt.Errorf("%w: mismatched office document relationship", ErrFormat)
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: office document relationship is missing", ErrFormat)
	}
	return nil
}

func relationshipTarget(rels, target string) (string, error) {
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" {
		return "", fmt.Errorf("%w: invalid relationship target", ErrFormat)
	}
	target = u.Path
	if strings.ContainsAny(target, "\\:\x00") {
		return "", ErrFormat
	}
	var resolved string
	if strings.HasPrefix(target, "/") {
		resolved = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		base := path.Dir(path.Dir(rels))
		if rels == "_rels/.rels" {
			base = "."
		}
		resolved = path.Clean(path.Join(base, target))
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") || resolved == "." {
		return "", fmt.Errorf("%w: relationship escapes package", ErrFormat)
	}
	return resolved, nil
}

func inspectPDF(out Inspection, data []byte) (Inspection, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.Contains(data[max(0, len(data)-4096):], []byte("%%EOF")) {
		return out, ErrFormat
	}
	out.Editability = "readonly"
	// PDF object streams may hide actions. A lexical scan is deliberately not
	// advertised as a security validator or permission to open a native worker.
	out.RenderAllowed = false
	out.Issues = append(out.Issues, Issue{Code: "PDF_READONLY", Severity: "info", Message: "PDF 保留原件，只读预览；未执行脚本、嵌入文件或外部链接。"})
	out.Preview = "PDF 文件；内容与页数应由隔离的 PDF 预览器读取。"
	// A plain /OpenAction may only select the first page. It is not by itself
	// evidence of an executable action (and never enables RenderAllowed here).
	if pdfActiveToken.Match(data) {
		out.Issues = append(out.Issues, Issue{Code: "PDF_ACTIVE_OR_ENCRYPTED", Severity: "blocked", Message: "PDF 含活动内容或加密标记，禁止自动修改或原生渲染。"})
		out.Editability = "blocked"
	}
	return out, nil
}

var pdfActiveToken = regexp.MustCompile(`/(?:JavaScript|Launch|EmbeddedFile|Encrypt)(?:[\s/><\[\](){}%]|$)`)

type sharedStringValue struct {
	Text string
	Rich bool
}

func sharedStrings(data []byte) ([]sharedStringValue, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc struct {
		Items []struct {
			Text string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, ErrFormat
	}
	if len(doc.Items) > MaxNodes {
		return nil, ErrLimit
	}
	out := make([]sharedStringValue, len(doc.Items))
	for i, item := range doc.Items {
		out[i] = sharedStringValue{Text: item.Text, Rich: len(item.Runs) > 0}
		for _, r := range item.Runs {
			out[i].Text += r.Text
		}
	}
	return out, nil
}

func cellText(raw []byte, shared []sharedStringValue) (text, cellType string, simple bool) {
	var cell struct {
		Type    string  `xml:"t,attr"`
		V       string  `xml:"v"`
		Formula *string `xml:"f"`
		Inline  struct {
			Text string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		} `xml:"is"`
	}
	if err := xml.Unmarshal(raw, &cell); err != nil {
		return "", "", false
	}
	if cell.Formula != nil {
		return "=" + strings.TrimPrefix(*cell.Formula, "="), "formula", false
	}
	switch cell.Type {
	case "s":
		idx, err := strconv.Atoi(cell.V)
		if err == nil && idx >= 0 && idx < len(shared) {
			if shared[idx].Rich {
				return shared[idx].Text, "richtext", false
			}
			return shared[idx].Text, "text", true
		}
		return "[无效文本索引]", "invalid", false
	case "inlineStr":
		if len(cell.Inline.Runs) > 0 {
			var b strings.Builder
			for _, r := range cell.Inline.Runs {
				b.WriteString(r.Text)
			}
			return b.String(), "richtext", false
		}
		return cell.Inline.Text, "text", true
	case "str":
		return cell.V, "text", true
	case "b":
		return cell.V, "boolean", false
	case "d":
		return cell.V, "date", false
	case "e":
		return cell.V, "error", false
	default:
		return cell.V, "number", false
	}
}
