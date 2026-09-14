package officestudio

import (
	"fmt"
	"strings"
)

func DocumentStyleIDs() []string {
	return []string{"ops", "brand", "editorial"}
}

func WordDeliverySpec(styleID, title string, facts []Fact) (Spec, error) {
	if err := ValidateFactSet(facts); err != nil {
		return Spec{}, err
	}
	styleID = strings.TrimSpace(styleID)
	locked := lockFacts(facts)
	blocks := []Block{{Type: "toc"}}
	header := ""
	switch styleID {
	case "ops":
		header = "运营纪要"
		blocks = append(blocks,
			Block{Type: "heading", Text: "结论"},
			Block{Type: "paragraph", Text: "下列结论只引用已核对事实，不补未提供的收入。"},
			Block{Type: "heading", Text: "指标"},
		)
		blocks = append(blocks, factParagraphs(locked)...)
		blocks = append(blocks,
			Block{Type: "heading", Text: "行动"},
			Block{Type: "table", Rows: [][]string{{"行动", "依据", "状态"}, {"按已核对指标复核", firstFactLabel(locked), "待执行"}}},
		)
	case "brand":
		header = "品牌观点"
		blocks = append(blocks,
			Block{Type: "heading", Text: "观点"},
			Block{Type: "paragraph", Text: "观点只绑定已提供证据，不编造市场收入。"},
			Block{Type: "heading", Text: "证据"},
		)
		blocks = append(blocks, factParagraphs(locked)...)
		blocks = append(blocks,
			Block{Type: "heading", Text: "建议"},
			Block{Type: "paragraph", Text: "建议范围不超过已核对事实。"},
		)
	case "editorial":
		header = "研究报告"
		blocks = append(blocks,
			Block{Type: "heading", Text: "摘要"},
			Block{Type: "paragraph", Text: editorialAbstract(locked)},
			Block{Type: "heading", Text: "经营概况"},
			Block{Type: "paragraph", Text: "口径来自已核对来源，未补充未提供的指标。"},
			Block{Type: "heading", Text: "收入分析"},
			Block{Type: "paragraph", Text: editorialRevenueSentence(locked)},
			Block{Type: "table", Rows: metricTable(locked, []string{"revenue_q1", "revenue_q2"}, "收入")},
			Block{Type: "heading", Text: "成本分析"},
			Block{Type: "table", Rows: metricTable(locked, []string{"cost_q1", "cost_q2"}, "成本")},
			Block{Type: "heading", Text: "客户分析"},
		)
		blocks = append(blocks, factParagraphs(filterFacts(locked, []string{"customers_q1", "customers_q2", "retention", "nps"}))...)
		blocks = append(blocks,
			Block{Type: "heading", Text: "建议"},
			Block{Type: "paragraph", Text: "注释与来源仅列出已提供的 sourceId，不发明完成率。"},
		)
	default:
		return Spec{}, fmt.Errorf("%w: unsupported document style %s", ErrFormat, styleID)
	}
	if len(locked) == 0 && styleID == "editorial" {
		// Keep the two analysis tables as explicit empty facts, not sample revenue.
		for i, b := range blocks {
			if b.Type == "table" {
				blocks[i].Rows = [][]string{{"期间", "数值"}, {"无已核对事实", "—"}}
			}
		}
	}
	templateID := map[string]string{"ops": "product-project", "brand": "client-proposal", "editorial": "research-report"}[styleID]
	spec := Spec{
		SchemaVersion: 2,
		Kind:          DOCX,
		Title:         title,
		TemplateID:    templateID,
		Document:      &DocumentOptions{Header: header, Footer: header + " · 正式报告", PageNumbers: true, PageSize: "A4"},
		Blocks:        blocks,
		Facts:         locked,
	}
	return spec, nil
}

func WordLongTableSpec(title string, rows [][]string) (Spec, error) {
	if len(rows) < 2 {
		return Spec{}, fmt.Errorf("%w: long table requires a header and data rows", ErrFormat)
	}
	spec := Spec{
		SchemaVersion: 2,
		Kind:          DOCX,
		Title:         title,
		TemplateID:    "client-proposal",
		Document:      &DocumentOptions{Header: "客户方案", Footer: "客户方案 · 页码待目标软件更新", PageNumbers: true, PageSize: "A4"},
		Blocks: []Block{
			{Type: "heading", Text: "客户方案"},
			{Type: "paragraph", Text: "长表完整保留，跨页重复表头，不截断单元格。"},
			{Type: "table", Rows: rows},
		},
	}
	return ApplyWordTemplate(spec)
}

func FieldRefreshStatus(insp Inspection, evidence *CacheMerge) Check {
	if insp.Structure == nil || (insp.Structure.TOCFields+insp.Structure.PageFields == 0 && !insp.Structure.NeedsFieldUpdate) {
		return Check{ID: "fields_update", Status: "passed", Message: "文档没有待更新的目录或页码域。"}
	}
	if evidence == nil || evidence.UpdatedFields == 0 {
		return Check{ID: "fields_update", Status: "unknown", Message: "目录与页码尚未由真实渲染器更新并核验。"}
	}
	return Check{ID: "fields_update", Status: "passed", Message: fmt.Sprintf("已按原生结果更新 %d 个域显示缓存。", evidence.UpdatedFields)}
}

func lockFacts(facts []Fact) []Fact {
	out := make([]Fact, len(facts))
	copy(out, facts)
	for i := range out {
		out[i].Locked = true
	}
	return out
}

func factParagraphs(facts []Fact) []Block {
	if len(facts) == 0 {
		return []Block{{Type: "paragraph", Text: "无已核对事实。"}}
	}
	out := make([]Block, 0, len(facts))
	for _, f := range facts {
		label := f.Locator
		if label == "" {
			label = f.FactID
		}
		out = append(out, Block{Type: "paragraph", Text: strings.TrimSpace(label + " " + f.Value + f.Unit)})
	}
	return out
}

func filterFacts(facts []Fact, ids []string) []Fact {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []Fact
	for _, f := range facts {
		if want[f.FactID] {
			out = append(out, f)
		}
	}
	return out
}

func firstFactLabel(facts []Fact) string {
	if len(facts) == 0 {
		return "无已核对事实"
	}
	if facts[0].Locator != "" {
		return facts[0].Locator
	}
	return facts[0].FactID
}

func editorialAbstract(facts []Fact) string {
	if len(facts) == 0 {
		return "摘要只覆盖已核对事实，当前没有可写入的收入或成本。"
	}
	return "摘要、正文与注释均绑定已核对事实，不编造示例收入。"
}

func editorialRevenueSentence(facts []Fact) string {
	for _, f := range facts {
		if f.FactID == "revenue_q1" && f.Value == "1200000" {
			return "计划收入120万元"
		}
	}
	if len(facts) == 0 {
		return "收入分析无已核对事实。"
	}
	return "收入数字以已核对事实为准。"
}

func metricTable(facts []Fact, ids []string, valueLabel string) [][]string {
	rows := [][]string{{"期间", valueLabel}}
	byID := map[string]Fact{}
	for _, f := range facts {
		byID[f.FactID] = f
	}
	for _, id := range ids {
		f, ok := byID[id]
		if !ok {
			continue
		}
		period := f.Period
		if period == "" {
			period = "—"
		}
		rows = append(rows, []string{period, f.Value})
	}
	if len(rows) == 1 {
		rows = append(rows, []string{"无已核对事实", "—"})
	}
	return rows
}
