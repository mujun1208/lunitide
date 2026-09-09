package officeapp

import (
	"fmt"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officerender"
)

func officeCheckLabel(id string) string {
	labels := map[string]string{
		"package": "文件结构与资源", "native_render": "实际排版预览",
		"content_safety": "活动内容与外部引用", "pdf_structure": "PDF 页面结构",
		"fields_update": "原文件目录与页码缓存", "full_recalculation": "原文件公式与计算缓存",
		"geometry_bounds": "几何越界",
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
