package officestudio

import (
	"fmt"
	"strings"
)

func PlanNarrative(spec Spec, brief Brief) (NarrativePlan, error) {
	facts := append(append([]Fact(nil), brief.Facts...), factsFromSpec(spec)...)
	if err := ValidateFactSet(facts); err != nil {
		return NarrativePlan{}, err
	}
	plan := NarrativePlan{FitEvidence: "narrative from spec and brief; no invented facts"}
	switch spec.Kind {
	case PPTX:
		for i, slide := range spec.Slides {
			plan.Nodes = append(plan.Nodes, narrativeFromSlide(slide, i, brief))
		}
	case DOCX:
		for i, block := range spec.Blocks {
			if !isNarrativeHeading(block.Type) && i > 0 {
				continue
			}
			plan.Nodes = append(plan.Nodes, narrativeFromBlock(block, i, brief))
		}
		if len(plan.Nodes) == 0 {
			plan.Nodes = append(plan.Nodes, NarrativeNode{
				NodeID:  "doc-1",
				Title:   spec.Title,
				Purpose: firstNonEmpty(brief.Purpose, "正式报告"),
				Claim:   spec.Title,
			})
		}
	case XLSX:
		for i, sheet := range spec.Sheets {
			plan.Nodes = append(plan.Nodes, NarrativeNode{
				NodeID:  fmt.Sprintf("sheet-%d", i+1),
				Title:   sheet.Name,
				Purpose: sheetNarrativePurpose(sheet.Name),
				Claim:   firstNonEmpty(spec.Title, sheet.Name),
			})
		}
	default:
		plan.Nodes = append(plan.Nodes, NarrativeNode{
			NodeID:  "pdf-1",
			Title:   spec.Title,
			Purpose: firstNonEmpty(brief.Purpose, "独立阅读稿"),
			Claim:   spec.Title,
		})
	}
	return overlayOutline(plan, brief.Outline), nil
}

func ApplyNarrativePlan(spec Spec, plan NarrativePlan) Spec {
	switch spec.Kind {
	case PPTX:
		for i := range spec.Slides {
			if i >= len(plan.Nodes) {
				break
			}
			node := plan.Nodes[i]
			if strings.TrimSpace(spec.Slides[i].Purpose) == "" {
				spec.Slides[i].Purpose = node.Purpose
			}
			if strings.TrimSpace(spec.Slides[i].Claim) == "" {
				spec.Slides[i].Claim = node.Claim
			}
			if len(spec.Slides[i].EvidenceRefs) == 0 && len(node.EvidenceRefs) > 0 {
				spec.Slides[i].EvidenceRefs = append([]string(nil), node.EvidenceRefs...)
			}
		}
	case DOCX:
		hi := 0
		for i := range spec.Blocks {
			if i > 0 && !isNarrativeHeading(spec.Blocks[i].Type) {
				continue
			}
			if hi >= len(plan.Nodes) {
				break
			}
			node := plan.Nodes[hi]
			hi++
			if strings.TrimSpace(spec.Blocks[i].Purpose) == "" {
				spec.Blocks[i].Purpose = node.Purpose
			}
			if strings.TrimSpace(spec.Blocks[i].Claim) == "" {
				spec.Blocks[i].Claim = node.Claim
			}
			if len(spec.Blocks[i].EvidenceRefs) == 0 && len(node.EvidenceRefs) > 0 {
				spec.Blocks[i].EvidenceRefs = append([]string(nil), node.EvidenceRefs...)
			}
		}
	case XLSX:
		for i := range spec.Sheets {
			if spec.Sheets[i].Name != "说明" {
				continue
			}
			purpose := ""
			for _, node := range plan.Nodes {
				if strings.TrimSpace(node.Title) == spec.Sheets[i].Name && strings.TrimSpace(node.Purpose) != "" {
					purpose = node.Purpose
					break
				}
			}
			if purpose == "" {
				purpose = sheetNarrativePurpose("说明")
			}
			if len(spec.Sheets[i].Rows) == 0 || len(spec.Sheets[i].Rows[0]) == 0 {
				continue
			}
			cell := spec.Sheets[i].Rows[0][0]
			if cell.Type != "text" || strings.Contains(cell.Value, purpose) {
				continue
			}
			spec.Sheets[i].Rows[0][0].Value = strings.TrimSpace(cell.Value + " 用途：" + purpose)
		}
	}
	return spec
}

func narrativeFromSlide(slide Slide, index int, brief Brief) NarrativeNode {
	refs := uniqueNonEmpty(append(append([]string(nil), slide.EvidenceRefs...), evidenceFactIDs(slide)...))
	return NarrativeNode{
		NodeID:       fmt.Sprintf("slide-%d", index+1),
		Title:        slide.Title,
		Layout:       slide.Layout,
		Purpose:      firstNonEmpty(slide.Purpose, layoutPurpose(slide.Layout, brief.Purpose)),
		Claim:        firstNonEmpty(slide.Claim, slide.Subtitle, slideClaimFromContent(slide), slide.Title),
		EvidenceRefs: refs,
		Metrics:      slide.Metrics,
		Comparison:   slide.Comparison,
		Bullets:      slide.Bullets,
	}
}

func narrativeFromBlock(block Block, index int, brief Brief) NarrativeNode {
	return NarrativeNode{
		NodeID:  fmt.Sprintf("block-%d", index+1),
		Title:   firstNonEmpty(block.Text, fmt.Sprintf("节 %d", index+1)),
		Purpose: firstNonEmpty(brief.Purpose, "正式报告"),
		Claim:   firstNonEmpty(block.Text, brief.Purpose),
	}
}

func overlayOutline(plan NarrativePlan, outline []NarrativeNode) NarrativePlan {
	if len(outline) == 0 {
		return plan
	}
	for _, item := range outline {
		for i, node := range plan.Nodes {
			if !outlineMatches(node, item) {
				continue
			}
			if strings.TrimSpace(item.Purpose) != "" {
				plan.Nodes[i].Purpose = item.Purpose
			}
			if strings.TrimSpace(node.Claim) == "" && strings.TrimSpace(item.Claim) != "" {
				plan.Nodes[i].Claim = item.Claim
			}
		}
	}
	return plan
}

func outlineMatches(node, item NarrativeNode) bool {
	if id := strings.TrimSpace(item.NodeID); id != "" && id == node.NodeID {
		return true
	}
	title := strings.TrimSpace(item.Title)
	return title != "" && title == strings.TrimSpace(node.Title)
}

func evidenceFactIDs(slide Slide) []string {
	var ids []string
	for _, item := range slide.Evidence {
		ids = append(ids, item.FactID)
	}
	for _, metric := range slide.Metrics {
		ids = append(ids, metric.FactID)
	}
	return ids
}

func slideClaimFromContent(slide Slide) string {
	if len(slide.Metrics) > 0 {
		m := slide.Metrics[0]
		return strings.TrimSpace(strings.Join([]string{m.Label, m.Value, m.Unit}, " "))
	}
	if slide.Comparison != nil && len(slide.Comparison.Left) > 0 {
		return slide.Comparison.Left[0]
	}
	if len(slide.Bullets) > 0 {
		return slide.Bullets[0]
	}
	return ""
}

func layoutPurpose(layout, briefPurpose string) string {
	switch normalizeSemanticLayout(layout) {
	case "cover", "title":
		return "封面"
	case "section":
		return "章节"
	case "metrics":
		return "指标概览"
	case "comparison":
		return "方案对比"
	case "timeline":
		return "时间线"
	case "closing":
		return "结尾行动"
	case "two-column":
		return "双栏论证"
	default:
		return firstNonEmpty(briefPurpose, "说明")
	}
}

func sheetNarrativePurpose(name string) string {
	switch strings.TrimSpace(name) {
	case "说明":
		return "工作簿说明"
	case "原始数据":
		return "输入数据"
	case "计算":
		return "推导计算"
	case "看板":
		return "已核对输出"
	default:
		return "工作表"
	}
}

func isNarrativeHeading(kind string) bool {
	switch kind {
	case "heading", "heading2", "heading3":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func containsString(items []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
