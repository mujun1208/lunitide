package officestudio

import (
	"fmt"
	"strconv"
	"strings"
)

func RuleVisualScore(checks []Check, issues []Issue) int {
	score := 50
	for _, c := range checks {
		switch c.Status {
		case "passed":
			score += 4
		case "failed", "blocked":
			score -= 15
		case "missing":
			score -= 6
		}
	}
	for _, issue := range issues {
		switch issue.Severity {
		case "warning":
			score -= 3
		case "blocked", "error":
			score -= 20
		}
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func RasterChecksFromInspection(insp Inspection) []Check {
	if insp.Kind != PPTX {
		return nil
	}
	type slideInfo struct{ images, body int }
	slides := map[string]*slideInfo{}
	for _, n := range insp.Nodes {
		if !isSlideContentPart(n.Part) {
			continue
		}
		info := slides[n.Part]
		if info == nil {
			info = &slideInfo{}
			slides[n.Part] = info
		}
		if n.Kind == "image" || n.Image != nil {
			info.images++
			continue
		}
		if n.Kind == "chart" || n.Chart != nil || strings.TrimSpace(n.Text) != "" {
			info.body++
		}
	}
	whole := 0
	for _, info := range slides {
		if info.images > 0 && info.body == 0 {
			whole++
		}
	}
	if whole == 0 {
		return nil
	}
	return []Check{RasterizedObjectCheck("whole-slide", whole)}
}

func isSlideContentPart(part string) bool {
	part = strings.ReplaceAll(part, "\\", "/")
	if strings.Contains(part, "/_rels/") || strings.Contains(part, "/notesSlides/") {
		return false
	}
	return strings.Contains(part, "/slides/slide") && strings.HasSuffix(part, ".xml")
}

func RasterizedObjectCheck(reason string, count int) Check {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unsupported-effect"
	}
	status := "warning"
	if reason == "whole-slide" {
		status = "failed"
	}
	return Check{ID: "rasterized_object", Status: status, Message: fmt.Sprintf("reason=%s count=%d；栅格对象不计入可编辑覆盖率", reason, count)}
}

func EvaluateQuality(checks []Check, visualScore int, facts []Fact) QualityReport {
	if visualScore == 0 {
		visualScore = RuleVisualScore(checks, nil)
	}
	report := QualityReport{Score: visualScore}
	for _, c := range checks {
		issue := Issue{Code: c.ID, Severity: c.Status, Message: c.Message}
		switch c.Status {
		case "failed", "blocked":
			report.Blockers = append(report.Blockers, issue)
		case "missing":
			if c.ID == "native_render" || c.ID == "actual-render" || c.ID == "external_adapter" || c.ID == "independent_pdf" || c.ID == "geometry_bounds" || c.ID == "fields_update" || c.ID == "full_recalculation" {
				report.Blockers = append(report.Blockers, issue)
			} else {
				report.Warnings = append(report.Warnings, issue)
			}
		}
	}
	for _, f := range facts {
		if f.Status == "conflict" {
			report.Blockers = append(report.Blockers, Issue{Code: "FACT_CONFLICT", Severity: "blocked", Message: f.FactID})
		}
	}
	report.Coverage = fmt.Sprintf("checks=%d blockers=%d; %s; visualScore=uncalibrated", len(checks), len(report.Blockers), TemplateVariantCoverage())
	rasterized := 0
	for _, c := range checks {
		if c.ID != "rasterized_object" {
			continue
		}
		if idx := strings.Index(c.Message, "count="); idx >= 0 {
			rest := c.Message[idx+len("count="):]
			if end := strings.Index(rest, "；"); end >= 0 {
				rest = rest[:end]
			}
			if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
				rasterized += n
				continue
			}
		}
		rasterized++
	}
	if rasterized > 0 {
		report.Coverage += fmt.Sprintf("; rasterized=%d", rasterized)
	}
	report.FormalOK = len(report.Blockers) == 0
	return report
}

func DiagnoseRender(available bool, issues []Issue) []Check {
	if !available {
		msg := "未检测到渲染组件，不能标为已验证"
		for _, issue := range issues {
			if issue.NodeID != "" {
				msg += "；" + issue.NodeID + " " + issue.Message
			}
		}
		return []Check{{ID: "native_render", Status: "missing", Message: msg}}
	}
	out := []Check{{ID: "native_render", Status: "passed", Message: "已渲染"}}
	for _, issue := range issues {
		status := "failed"
		if issue.Severity != "error" {
			status = "missing"
		}
		out = append(out, Check{ID: issue.Code, Status: status, Message: issue.NodeID + " " + issue.Message})
	}
	return out
}

func BoundedRepair(data []byte, facts []Fact) ([]byte, QualityReport, error) {
	report, out, err := RepairGeometryOverflow(data)
	if err != nil {
		return data, QualityReport{}, err
	}
	remain := MaxGeometryRepairRounds - report.Rounds
	overlap := GeometryReport{}
	if remain > 0 {
		overlap, out, err = RepairGeometryOverlap(out, remain)
		if err != nil {
			return data, QualityReport{}, err
		}
	}
	qa := QualityReport{
		RepairHistory: []string{
			fmt.Sprintf("geometry rounds=%d repaired=%d", report.Rounds, report.Repaired),
			fmt.Sprintf("overlap rounds=%d repaired=%d", overlap.Rounds, overlap.Repaired),
		},
		FormalOK: report.RemainingOverflow == 0,
	}
	if report.RemainingOverflow > 0 {
		qa.Blockers = append(qa.Blockers, Issue{Code: "OFFICE_GEOMETRY_OVERFLOW", Message: "仍有越界对象"})
	}
	insp, err := Inspect(PPTX, out)
	if err != nil {
		return out, qa, err
	}
	for _, f := range facts {
		if !f.Locked {
			continue
		}
		found := false
		for _, n := range insp.Nodes {
			if n.Text == f.Value {
				found = true
				break
			}
		}
		if !found {
			qa.Blockers = append(qa.Blockers, Issue{Code: "FACT_LOCK", Message: f.FactID})
			qa.FormalOK = false
		}
	}
	return out, qa, nil
}

func CanFormalDeliver(report QualityReport) bool {
	return report.FormalOK && len(report.Blockers) == 0
}
