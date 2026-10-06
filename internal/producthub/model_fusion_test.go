package producthub

import (
	"context"
	"strings"
	"testing"
)

// fusionFixture mirrors a real user setup: GLM (5.3 默认 + 4.5V 视觉)、
// DeepSeek（4 Pro 默认、无窗口登记）、一家停用的语音供应商。
func fusionFixture() {
	SetModelSlots([]ModelSlot{
		{ProviderID: "glm", ProviderName: "智谱 GLM", Protocol: "openai_compatible",
			ModelID: "glm-5.3", DisplayName: "GLM-5.3", Kind: "llm", IsDefault: true, KindDefault: true,
			ContextWindow: 131072, Status: "enabled", CredentialState: "configured"},
		{ProviderID: "glm", ProviderName: "智谱 GLM", Protocol: "openai_compatible",
			ModelID: "glm-4.5v", DisplayName: "GLM-4.5V", Kind: "vision", KindDefault: true,
			ContextWindow: 131072, Status: "enabled", CredentialState: "configured"},
		{ProviderID: "deepseek", ProviderName: "DeepSeek", Protocol: "openai_compatible",
			ModelID: "deepseek-4-pro", DisplayName: "DeepSeek 4 Pro", Kind: "llm", IsDefault: true, KindDefault: true,
			Status: "enabled", CredentialState: "configured"},
		{ProviderID: "volc", ProviderName: "火山语音", Protocol: "volc_speech",
			ModelID: "volc-asr", DisplayName: "语音识别", Kind: "asr", KindDefault: true,
			Status: "disabled", CredentialState: "missing"},
	})
}

func fusionFinding(in []Finding, code, key string) *Finding {
	for i := range in {
		if in[i].ErrorCode == code && in[i].StableKey == key {
			return &in[i]
		}
	}
	return nil
}

func TestAuditModelFusionDetectsUpgrades(t *testing.T) {
	defer SetModelSlots(nil)
	fusionFixture()
	a := AuditModelFusion()
	if a.Providers != 3 || a.Usable != 2 {
		t.Fatalf("供应商计数 got %d 家/可用 %d, want 3/2", a.Providers, a.Usable)
	}
	if len(a.Upgrades) != 2 {
		t.Fatalf("升级事件 got %d want 2: %+v", len(a.Upgrades), a.Upgrades)
	}
	gotGLM, gotDeepSeek := false, false
	for _, u := range a.Upgrades {
		if u.ProviderID == "glm" && u.CurrentID == "glm-5.3" && u.NextID == "glm-5" {
			gotGLM = true
			if len(u.Kinds) != 1 || u.Kinds[0] != "llm" {
				t.Fatalf("glm 升级应标注 llm 槽位: %+v", u.Kinds)
			}
		}
		if u.ProviderID == "deepseek" && u.CurrentID == "deepseek-4-pro" && u.NextID == "deepseek-5-pro" {
			gotDeepSeek = true
		}
	}
	if !gotGLM || !gotDeepSeek {
		t.Fatalf("应检出 glm-5.3→glm-5 与 deepseek-4-pro→deepseek-5-pro: %+v", a.Upgrades)
	}
	if len(a.UnknownModels) != 1 || a.UnknownModels[0] != "volc-asr" {
		t.Fatalf("未登记模型 got %+v want [volc-asr]", a.UnknownModels)
	}
	cov := map[string]bool{}
	for _, row := range a.KindCoverage {
		cov[row.Kind] = row.Covered
	}
	if !cov["llm"] || !cov["vision"] {
		t.Fatal("llm 与 vision 槽位应已覆盖")
	}
	if cov["asr"] || cov["tts"] {
		t.Fatal("停用供应商上的 asr/tts 不应算覆盖")
	}
}

func TestAuditModelFusionListingNextGenerationIsNotAdopting(t *testing.T) {
	defer SetModelSlots(nil)
	// model.sync 把新一代拉进清单，但默认主力仍是上一代：升级发现必须保留。
	SetModelSlots([]ModelSlot{
		{ProviderID: "glm", ProviderName: "智谱 GLM", Protocol: "openai_compatible",
			ModelID: "glm-5.3", DisplayName: "GLM-5.3", Kind: "llm", IsDefault: true, KindDefault: true,
			ContextWindow: 131072, Status: "enabled", CredentialState: "configured"},
		{ProviderID: "glm", ProviderName: "智谱 GLM", Protocol: "openai_compatible",
			ModelID: "glm-5", DisplayName: "GLM-5", Kind: "llm",
			ContextWindow: 131072, Status: "enabled", CredentialState: "configured"},
	})
	a := AuditModelFusion()
	if len(a.Upgrades) != 1 || a.Upgrades[0].CurrentID != "glm-5.3" || a.Upgrades[0].NextID != "glm-5" {
		t.Fatalf("清单里出现新一代但主力未换，升级发现应保留: %+v", a.Upgrades)
	}
}

func TestFusionFindingsFourClasses(t *testing.T) {
	defer SetModelSlots(nil)
	fusionFixture()
	fs := fusionFindings(AuditModelFusion())
	counts := map[string]int{}
	for _, f := range fs {
		counts[f.ErrorCode]++
	}
	// 缺槽位 6（gui/image/video/embedding/asr/tts），升级 2，窗口 1（deepseek-4-pro），供应商 1（volc）
	if counts["PH_M01"] != 6 || counts["PH_M02"] != 2 || counts["PH_M03"] != 1 || counts["PH_M04"] != 1 {
		t.Fatalf("发现计数 got %+v", counts)
	}
	asr := fusionFinding(fs, "PH_M01", "model.kind.asr")
	if asr == nil || asr.Severity != "warn" || !strings.Contains(asr.Evidence, "语音转写") {
		t.Fatalf("asr 缺槽应为 warn 且绑定具体功能: %+v", asr)
	}
	gui := fusionFinding(fs, "PH_M01", "model.kind.gui")
	if gui == nil || gui.Severity != "info" {
		t.Fatalf("gui 缺槽应为 info: %+v", gui)
	}
	up := fusionFinding(fs, "PH_M02", "model.upgrade.glm.glm-5-3")
	if up == nil {
		t.Fatal("缺少 glm 升级发现")
	}
	if !strings.Contains(up.Fix, "glm-5") || !strings.Contains(up.Fix, "执行净化") {
		t.Fatalf("升级 Fix 应含执行净化与新一代模型: %s", up.Fix)
	}
	if !strings.Contains(up.ApplyPrompt, "新旧模型能力差异") || !strings.Contains(up.ApplyPrompt, "GLM-5.3") {
		t.Fatalf("PH_M02 任务书应是升级深度分析: %s", up.ApplyPrompt)
	}
	if fusionFinding(fs, "PH_M03", "model.window.deepseek.deepseek-4-pro") == nil {
		t.Fatal("deepseek-4-pro 无窗口应有 PH_M03")
	}
	p := fusionFinding(fs, "PH_M04", "model.provider.volc")
	if p == nil || p.Severity != "warn" {
		t.Fatalf("停用供应商应为 warn: %+v", p)
	}
	// 未注入时零发现：旧报告与未注入测试不受影响。
	SetModelSlots(nil)
	if got := fusionFindings(AuditModelFusion()); len(got) != 0 {
		t.Fatalf("未注入时应无 PH_M 发现, got %d", len(got))
	}
}

func TestRefreshFindingsDropsAdoptedUpgrade(t *testing.T) {
	defer SetModelSlots(nil)
	fusionFixture()
	ed := Edition{Findings: fusionFindings(AuditModelFusion())}
	next, _, _ := refreshFindings(ed)
	if fusionFinding(next, "PH_M02", "model.upgrade.glm.glm-5-3") == nil {
		t.Fatal("升级未处理前 PH_M02 应保留")
	}
	// 用户确认方案后在供应商页把 GLM 默认模型换成 glm-5，再点重新检测。
	SetModelSlots([]ModelSlot{
		{ProviderID: "glm", ProviderName: "智谱 GLM", Protocol: "openai_compatible",
			ModelID: "glm-5", DisplayName: "GLM-5", Kind: "llm", IsDefault: true, KindDefault: true,
			ContextWindow: 131072, Status: "enabled", CredentialState: "configured"},
	})
	next, _, _ = refreshFindings(ed)
	if fusionFinding(next, "PH_M02", "model.upgrade.glm.glm-5-3") != nil {
		t.Fatal("换到 glm-5 后该 PH_M02 应消失")
	}
	if fusionFinding(next, "PH_M02", "model.upgrade.glm.glm-5") != nil {
		t.Fatal("glm-5 档案无后继，不应再报升级")
	}
}

func TestGenerateReportIncludesModelFusion(t *testing.T) {
	defer SetModelSlots(nil)
	fusionFixture()
	s := New(&MemoryPersist{})
	ed, err := s.Generate(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ed.ReportMarkdown, "模型融合") {
		t.Fatal("注入模型配置后报告应包含模型融合节")
	}
	if !strings.Contains(ed.ReportMarkdown, "glm-5.3 → glm-5") || !strings.Contains(ed.ReportMarkdown, "deepseek-4-pro → deepseek-5-pro") {
		t.Fatal("报告应写明两条升级路径")
	}
	m02 := 0
	for _, f := range ed.Findings {
		if f.ErrorCode == "PH_M02" {
			m02++
		}
	}
	if m02 != 2 {
		t.Fatalf("Generate 应并入 2 条 PH_M02, got %d", m02)
	}
	// 未注入环境下重新生成：模型融合节消失，报告保持零漂移。
	SetModelSlots(nil)
	plain, err := s.Generate(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.ReportMarkdown, "模型融合") {
		t.Fatal("未注入时不应出现模型融合节")
	}
	for _, f := range plain.Findings {
		if strings.HasPrefix(f.ErrorCode, "PH_M") {
			t.Fatalf("未注入时不应有 PH_M 发现: %+v", f)
		}
	}
}
