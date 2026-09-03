package m8app

import (
	"context"
	"encoding/json"
	"errors"
)

type mroScenarioSeed struct {
	Title    string
	Summary  string
	PhaseKey string
	Scenario map[string]any
}

var mroScenarioSeeds = []mroScenarioSeed{
	{
		Title: "手册问答", Summary: "按机尾与日期检索受控手册并引用后回答。",
		PhaseKey: "RESEARCH_EVIDENCE",
		Scenario: map[string]any{"title": "手册问答", "phaseKey": "RESEARCH_EVIDENCE", "steps": []string{"确认机尾与日期", "kb.search", "kb.cite", "回答"}},
	},
	{
		Title: "排故诊断", Summary: "按症状组织故障树，每步引用手册并标置信度。",
		PhaseKey: "OPERATIONS_RETROSPECTIVE",
		Scenario: map[string]any{"title": "排故诊断", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"记录症状", "故障树", "引用", "置信度"}},
	},
	{
		Title: "视觉识件", Summary: "从附件图给出候选件号/ATA，必须落手册锚点。",
		PhaseKey: "RESEARCH_EVIDENCE",
		Scenario: map[string]any{"title": "视觉识件", "phaseKey": "RESEARCH_EVIDENCE", "steps": []string{"看附件图", "候选PN/ATA", "手册锚点", "不可用则说明"}},
	},
	{
		Title: "预测维护", Summary: "有数据源则列风险清单，无数据则诚实降级。",
		PhaseKey: "OPERATIONS_RETROSPECTIVE",
		Scenario: map[string]any{"title": "预测维护", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"检查数据源", "无数据则降级", "有数据则风险清单"}},
	},
	{
		Title: "合规工单", Summary: "把已引用步骤收成检查单，并写不构成放行。",
		PhaseKey: "OPERATIONS_RETROSPECTIVE",
		Scenario: map[string]any{"title": "合规工单", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"收集已引用步骤", "生成检查单", "写不构成放行"}},
	},
	{
		Title: "培训教官", Summary: "出情景题并引用要点，评分不代替放行。",
		PhaseKey: "RESEARCH_EVIDENCE",
		Scenario: map[string]any{"title": "培训教官", "phaseKey": "RESEARCH_EVIDENCE", "steps": []string{"出情景题", "要点引用", "评分不放行"}},
	},
}

// EnsureMROScenarios seeds the six aviation-MRO playbooks onto the shipped
// 航空机务维修专家. Missing expert is a no-op. Existing titles are skipped.
func EnsureMROScenarios(ctx context.Context, experts *ExpertService, scenarios *ScenarioService) error {
	if experts == nil || scenarios == nil {
		return nil
	}
	listed, err := experts.List(ctx, ExpertFilter{})
	if err != nil {
		return err
	}
	expertID := ""
	for _, row := range listed.Experts {
		if row.CatalogItemID == "mro-expert" || row.Name == "航空机务维修专家" || row.Name == "航空机务专家" {
			expertID = row.ExpertID
			break
		}
	}
	if expertID == "" {
		return nil
	}
	for _, seed := range mroScenarioSeeds {
		raw, err := json.Marshal(seed.Scenario)
		if err != nil {
			return err
		}
		_, err = scenarios.CreateScenario(ctx, ScenarioCreateInput{
			ExpertID: expertID,
			Title:    seed.Title,
			Summary:  seed.Summary,
			PhaseKey: seed.PhaseKey,
			Scenario: raw,
			Actor:    "bootstrap-mro-scenarios",
		})
		if err == nil || errors.Is(err, ErrScenarioDuplicate) {
			continue
		}
		return err
	}
	return nil
}
