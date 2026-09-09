package officeapp

import (
	"context"
	"fmt"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officerender"
)

func (s *Service) fontChecks(ctx context.Context, kind string, data []byte) ([]domain.Check, *officerender.FontReport) {
	if kind == "pdf" {
		return nil, nil
	}
	check := domain.Check{ID: "font_availability", Label: "文档字体家族可用性", Required: true, Status: "unsupported"}
	report, err := s.Renderer.FontReport(ctx, kind, data)
	if err != nil {
		check.Detail = "字体声明检查未完成；不影响保留原文件，不能认定字体一致。"
		return []domain.Check{check}, nil
	}
	check.Detail = fmt.Sprintf("核对 %d 个声明家族；缺少同名字体 %d 项，尚未确认 %d 项。", len(report.Families), report.MissingCount, report.UnknownCount)
	if report.InventoryComplete && report.DeclarationScanComplete && len(report.Families) > 0 && report.MissingCount == 0 && report.UnknownCount == 0 {
		check.Status = "passed"
	}
	missing := []string{}
	for _, family := range report.Families {
		if family.Status == "missing" {
			missing = append(missing, family.Family)
		}
	}
	if len(missing) > 0 {
		names := strings.Join(missing, "、")
		if len(names) > 4000 {
			names = string([]rune(names)[:min(1000, len([]rune(names)))]) + "…"
		}
		check.Detail += " 未找到：" + names + "。"
	}
	if !report.DeclarationScanComplete {
		check.Detail += "声明超出本次有界检查范围，完整文件仍保留。"
	}
	check.Detail += "此结果不证明逐字符覆盖或实际排版采用的替代字体；没有自动改字体。"
	actual := domain.Check{ID: "font_actual_substitution", Label: "实际字体替代与度量", Required: true, Status: "unsupported", Detail: "本次可用性清单不能证明渲染器实际选择的字形、字体替代、字号度量或几何溢出；需独立排版证据。"}
	return []domain.Check{check, actual}, &report
}
