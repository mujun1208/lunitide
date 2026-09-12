package officeapp

import (
	"fmt"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officerender"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func officeCheckPresent(checks []domain.Check, id string) bool {
	for _, c := range checks {
		if c.ID == id {
			return true
		}
	}
	return false
}

func requiredQualityCheck(id string) bool {
	switch id {
	case "native_render", "actual-render", "geometry_bounds", "fields_update", "full_recalculation":
		return true
	default:
		return false
	}
}

func evaluateOfficeQuality(checks []domain.Check, facts []content.Fact) content.QualityReport {
	mapped := make([]content.Check, 0, len(checks))
	for _, c := range checks {
		status := c.Status
		if status == "unsupported" && requiredQualityCheck(c.ID) {
			status = "missing"
		}
		mapped = append(mapped, content.Check{ID: c.ID, Status: status, Message: c.Detail})
	}
	report := content.EvaluateQuality(mapped, 0, facts)
	report.FormalOK = content.CanFormalDeliver(report)
	return report
}

func usabilityOptionalID(id string) bool {
	switch id {
	case "pdfa", "visual-model":
		return true
	default:
		return false
	}
}

func remapStudioStatus(id, status string) string {
	if status == "blocked" {
		return "failed"
	}
	if status == "missing" && !usabilityOptionalID(id) {
		return "unsupported"
	}
	return status
}

func upsertOfficeCheck(checks []domain.Check, next domain.Check) []domain.Check {
	for i, c := range checks {
		if c.ID == next.ID {
			checks[i] = next
			return checks
		}
	}
	return append(checks, next)
}

func applyUsabilityChecks(checks []domain.Check, pdf []byte) []domain.Check {
	pdfa := content.IndependentPDFACheck(pdf)
	visionReq := content.VisualModelRequest{Configured: content.VisionModelConfigured()}
	if len(pdf) > 0 && visionReq.Configured {
		visionReq.Pages = [][]byte{pdf}
		visionReq.Review = content.RunConfiguredVisualReview
	}
	vision := content.VisualModelCheck(visionReq)
	checks = upsertOfficeCheck(checks, domain.Check{
		ID: pdfa.ID, Label: officeCheckLabel(pdfa.ID), Status: pdfa.Status, Required: false, Detail: pdfa.Message,
	})
	return upsertOfficeCheck(checks, domain.Check{
		ID: vision.ID, Label: officeCheckLabel(vision.ID), Status: vision.Status, Required: false, Detail: vision.Message,
	})
}

func visualModelCoverageCheck() domain.Check {
	c := content.VisualModelCheck(content.VisualModelRequest{Configured: content.VisionModelConfigured()})
	return domain.Check{ID: c.ID, Label: officeCheckLabel(c.ID), Status: c.Status, Required: false, Detail: c.Message}
}

func pdfaCoverageCheck() domain.Check {
	c := content.IndependentPDFACheck()
	return domain.Check{ID: c.ID, Label: officeCheckLabel(c.ID), Status: c.Status, Required: false, Detail: c.Message}
}

func targetAppCoverageChecks() []domain.Check {
	return []domain.Check{
		{ID: "target-compatibility", Label: "Office/WPS 目标软件兼容性", Status: "unsupported", Required: false, Detail: "支持矩阵：PowerPoint、WPS、LibreOffice 均未完成目标软件打开验证；无头导出不能代替界面打开"},
		{ID: "target-powerpoint", Label: "Microsoft PowerPoint 打开验证", Status: "unsupported", Required: false, Detail: "尚未在 Microsoft PowerPoint 完成打开验证；版本未知"},
		{ID: "target-wps", Label: "WPS 打开验证", Status: "unsupported", Required: false, Detail: "尚未在 WPS 完成打开验证；版本未知"},
		{ID: "target-libreoffice", Label: "LibreOffice 界面打开验证", Status: "unsupported", Required: false, Detail: "无头导出不能代替 LibreOffice 界面打开验证"},
		visualModelCoverageCheck(),
		pdfaCoverageCheck(),
	}
}

func officeCheckLabel(id string) string {
	labels := map[string]string{
		"package": "文件结构与资源", "native_render": "实际排版预览", "design_system": "设计系统",
		"content_safety": "活动内容与外部引用", "pdf_structure": "PDF 页面结构",
		"fields_update": "原文件目录与页码缓存", "full_recalculation": "原文件公式与计算缓存",
		"geometry_bounds":      "几何越界",
		"rasterized_object":    "栅格对象",
		"target-compatibility": "Office/WPS 目标软件兼容性",
		"target-powerpoint":    "Microsoft PowerPoint 打开验证",
		"target-wps":           "WPS 打开验证",
		"target-libreoffice":   "LibreOffice 界面打开验证",
		"visual-model":         "视觉模型诊断",
		"pdfa":                 "PDF/A 合规",
		"independent_pdf":      "独立 PDF",
	}
	if label := labels[id]; label != "" {
		return label
	}
	return id
}

func nativeOptions(kind string, checks []domain.Check) officerender.NativeOptions {
	var options officerender.NativeOptions
	for _, check := range checks {
		if kind == "docx" && check.ID == "fields_update" {
			options.UpdateFields = true
		}
		if kind == "xlsx" && check.ID == "full_recalculation" {
			options.Recalculate = true
		}
	}
	return options
}

// Evidence applies to the isolated PDF preview. Never replace the source-file
// cache or target-software checks with a successful derived preview result.
func nativePreviewChecks(e officerender.NativeEvidence) []domain.Check {
	checks := []domain.Check{}
	if e.Scope != "derived-preview" || !e.SourceUnchanged {
		return []domain.Check{{ID: "preview-native", Label: "预览更新回执", Required: true, Status: "failed", Detail: "预览回执未明确原稿保留与验证范围"}}
	}
	if e.FieldsRefreshed {
		checks = append(checks, domain.Check{ID: "preview_fields", Label: "预览目录与页码", Required: true, Status: "passed", Detail: fmt.Sprintf("PDF 预览已刷新域并更新 %d 个目录索引；原文件未改写，其缓存仍需目标软件更新。", e.UpdatedIndexes)})
	}
	if e.Recalculated {
		status := "passed"
		detail := fmt.Sprintf("PDF 预览已执行完整重算，检查 %d 个公式单元格，发现 %d 个计算错误。", e.FormulaCells, e.FormulaErrorCount)
		if !e.FormulaScanFull {
			status = "unsupported"
			detail += "公式错误扫描达到上限，尚未检查剩余公式。"
		}
		if e.FormulaErrorCount > 0 {
			status = "failed"
			locations := []string{}
			for _, cell := range e.FormulaErrors[:min(10, len(e.FormulaErrors))] {
				locations = append(locations, fmt.Sprintf("第%d张表 R%dC%d（错误%d）", cell.SheetIndex+1, cell.Row, cell.Column, cell.Code))
			}
			if len(locations) > 0 {
				detail += strings.Join(locations, "、") + "。"
			}
			if e.FormulaErrorCount > len(locations) {
				detail += "此处仅列出前10个位置，完整扫描回执保留在检查记录中。"
			}
		}
		detail += "原文件未改写；此结果不代替原文件缓存与 Office/WPS 兼容验证。"
		checks = append(checks, domain.Check{ID: "preview_calculation", Label: "预览公式重算", Required: true, Status: status, Detail: detail})
	}
	return checks
}
