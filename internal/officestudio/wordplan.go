package officestudio

import "fmt"

func WordTemplateIDs() []string {
	return []string{"research-report", "client-proposal", "product-project"}
}

func ApplyWordTemplate(spec Spec) (Spec, error) {
	if spec.Kind != DOCX {
		return spec, nil
	}
	id := spec.TemplateID
	if id == "" {
		return spec, nil
	}
	header, footer, ok := wordTemplateChrome(id)
	if !ok {
		return Spec{}, fmt.Errorf("%w: unsupported word template %s", ErrFormat, id)
	}
	out := spec
	if out.Document == nil {
		out.Document = &DocumentOptions{}
	}
	if out.Document.Header == "" {
		out.Document.Header = header
	}
	if out.Document.Footer == "" {
		out.Document.Footer = footer
	}
	out.Document.PageNumbers = true
	if out.Document.PageSize == "" {
		out.Document.PageSize = "A4"
	}
	return out, nil
}

func WordReportSpec(templateID, title string, facts []Fact) (Spec, error) {
	if err := ValidateFactSet(facts); err != nil {
		return Spec{}, err
	}
	header, _, ok := wordTemplateChrome(templateID)
	if !ok {
		return Spec{}, fmt.Errorf("%w: unsupported word template %s", ErrFormat, templateID)
	}
	blocks := []Block{{Type: "toc"}}
	switch templateID {
	case "research-report":
		blocks = append(blocks,
			Block{Type: "heading", Text: "研究范围"},
			Block{Type: "heading2", Text: "资料与口径"},
			Block{Type: "paragraph", Text: "下列数字来自已核对来源，未补充未提供的指标。"},
		)
	case "client-proposal":
		blocks = append(blocks,
			Block{Type: "heading", Text: "客户问题"},
			Block{Type: "heading2", Text: "方案要点"},
			Block{Type: "paragraph", Text: "方案只引用已确认事实，不编造供应商通过结论。"},
		)
	case "product-project":
		blocks = append(blocks,
			Block{Type: "heading", Text: "项目范围"},
			Block{Type: "heading2", Text: "当前状态"},
			Block{Type: "paragraph", Text: "进度只记录已提供事实，不发明完成率。"},
		)
	}
	locked := make([]Fact, len(facts))
	copy(locked, facts)
	for i := range locked {
		locked[i].Locked = true
		blocks = append(blocks, Block{Type: "paragraph", Text: locked[i].Locator + " " + locked[i].Value + locked[i].Unit})
	}
	spec := Spec{
		SchemaVersion: 2,
		Kind:          DOCX,
		Title:         title,
		TemplateID:    templateID,
		Document:      &DocumentOptions{Header: header, Footer: header + " · 正式报告", PageNumbers: true, PageSize: "A4"},
		Blocks:        blocks,
		Facts:         locked,
	}
	return ApplyWordTemplate(spec)
}

func wordTemplateChrome(id string) (header, footer string, ok bool) {
	switch id {
	case "research-report":
		return "研究报告", "研究报告 · 页码待目标软件更新", true
	case "client-proposal":
		return "客户方案", "客户方案 · 页码待目标软件更新", true
	case "product-project":
		return "产品项目文档", "产品项目文档 · 页码待目标软件更新", true
	default:
		return "", "", false
	}
}
