package m8app

import (
	"context"
	"encoding/json"
	"errors"
)

type opsScenarioSeed struct {
	CatalogID string
	Title     string
	Summary   string
	PhaseKey  string
	Scenario  map[string]any
}

var opsScenarioSeeds = []opsScenarioSeed{
	{CatalogID: "uas-airworthiness-expert", Title: "法规问答", Summary: "按机型与运行分类检索 CCAR-92 等法规并引用后回答。", PhaseKey: "RESEARCH_EVIDENCE", Scenario: map[string]any{"title": "法规问答", "phaseKey": "RESEARCH_EVIDENCE", "steps": []string{"确认机型与运行分类", "kb.search", "kb.cite", "回答"}}},
	{CatalogID: "uas-airworthiness-expert", Title: "履历与触发器", Summary: "按部件 SN 读履历与到期引擎，缺利用率写未录入。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "履历与触发器", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"确认 SN", "读履历", "读到期引擎", "解释触发器"}}},
	{CatalogID: "uas-airworthiness-expert", Title: "PIREP 草稿", Summary: "结构化 PIREP 草稿，人确认后才转缺陷记录。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "PIREP 草稿", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"收集现象", "结构化草稿", "人审", "不签 RTS"}}},
	{CatalogID: "tooling-chemical-expert", Title: "工艺/SDS 问答", Summary: "检索 SDS 与工艺规范，无依据则拒答。", PhaseKey: "RESEARCH_EVIDENCE", Scenario: map[string]any{"title": "工艺/SDS 问答", "phaseKey": "RESEARCH_EVIDENCE", "steps": []string{"确认批次或材料", "kb.search", "kb.cite", "回答"}}},
	{CatalogID: "tooling-chemical-expert", Title: "校准与借还", Summary: "读校准到期；过期拒绝借出，借还只出草稿。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "校准与借还", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"读工具台账", "校准硬门", "草稿", "人审"}}},
	{CatalogID: "tooling-chemical-expert", Title: "套件备妥", Summary: "模板需求对照套件内容，列出缺件。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "套件备妥", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"读模板", "对照套件", "缺件列表", "可转航材待办"}}},
	{CatalogID: "parts-expert", Title: "库存与适用性", Summary: "读数据源或本机台账；替代件三重过滤。", PhaseKey: "RESEARCH_EVIDENCE", Scenario: map[string]any{"title": "库存与适用性", "phaseKey": "RESEARCH_EVIDENCE", "steps": []string{"确认件号与构型", "查库存", "三重过滤", "未绑定则降级"}}},
	{CatalogID: "parts-expert", Title: "AOG 草稿", Summary: "AOG 案件模板字段，一击确认，不发真邮件。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "AOG 草稿", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"抽字段", "一击确认", "模板草稿", "不发邮件"}}},
	{CatalogID: "parts-expert", Title: "采购模板草稿", Summary: "PO/询价用台账字段填充，禁止 LLM 填件号数量价格。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "采购模板草稿", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"读台账", "填模板", "人审", "不构成采购承诺"}}},
	{CatalogID: "mx-planning-expert", Title: "到期全景", Summary: "按机尾列出到期引擎数字，缺项写未录入。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "到期全景", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"选机尾", "读到期引擎", "标超限", "禁止估算"}}},
	{CatalogID: "mx-planning-expert", Title: "工作包组装", Summary: "四源组装工作包草稿：标准卡+AD/SB+MEL+未关闭项。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "工作包组装", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"收集四源", "组装草稿", "可见来源", "人审发布"}}},
	{CatalogID: "mx-planning-expert", Title: "间隔复审草案", Summary: "间隔调整必须双源引用，缺则拒绝，不自动改 interval_rules。", PhaseKey: "OPERATIONS_RETROSPECTIVE", Scenario: map[string]any{"title": "间隔复审草案", "phaseKey": "OPERATIONS_RETROSPECTIVE", "steps": []string{"MPD cite", "本队数据", "双源检查", "只出草案"}}},
}

// EnsureOpsExpertScenarios seeds three playbooks onto each of the four
// operations domain colleagues. Missing experts are skipped. Existing
// titles are skipped (idempotent).
func EnsureOpsExpertScenarios(ctx context.Context, experts *ExpertService, scenarios *ScenarioService) error {
	if experts == nil || scenarios == nil {
		return nil
	}
	listed, err := experts.List(ctx, ExpertFilter{})
	if err != nil {
		return err
	}
	byCatalog := map[string]string{}
	for _, row := range listed.Experts {
		if row.CatalogItemID != "" {
			byCatalog[row.CatalogItemID] = row.ExpertID
		}
	}
	for _, seed := range opsScenarioSeeds {
		expertID := byCatalog[seed.CatalogID]
		if expertID == "" {
			continue
		}
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
			Actor:    "bootstrap-ops-scenarios",
		})
		if err == nil || errors.Is(err, ErrScenarioDuplicate) {
			continue
		}
		return err
	}
	return nil
}
