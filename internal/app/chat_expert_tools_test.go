package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestFullSubagentReadCapsIs全部权限Pack(t *testing.T) {
	caps := fullSubagentReadCaps()
	want := []string{
		"fs.read", "fs.tree", "fs.grep", "fs.glob", "fs.stat", "fs.readMany",
		"web.search", "web.fetch",
		"browser.act:navigate", "browser.act:read", "browser.act:snapshot",
		"evidence.list",
	}
	if len(caps) != len(want) {
		t.Fatalf("caps = %#v", caps)
	}
	for i, c := range want {
		if caps[i] != c {
			t.Fatalf("caps[%d]=%q want %q", i, caps[i], c)
		}
		if !m7CapAllowedForSpawn(c) {
			t.Fatalf("%s not on M7 spawn whitelist", c)
		}
	}
	if capsIncludeAll(defaultSubagentProfileCaps(), caps) {
		t.Fatal("legacy general-purpose default must stay narrower than 全部权限")
	}
}

func TestExpertSpawnUsesFullReadCapsAndBrowser(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &subagentFakeAdapter{}
	policy := subTestPolicy()
	policy.ExpertWork = true
	if _, err := e.invokeSubagentTool(context.Background(), adapter, nil, "model-x", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"survey sources","profile":"research"}`), policy); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, name := range adapter.seenTools {
		have[name] = true
		if name == "workspace.write" || name == "html.gen" {
			t.Fatalf("expert subagent must stay read-only, got %s", name)
		}
	}
	for _, name := range []string{"web.search", "web.fetch", "workspace.read", "browser.act"} {
		if !have[name] {
			t.Fatalf("expert/council spawn missing %s: %#v", name, adapter.seenTools)
		}
	}
}

func TestSpecialistChatStartPinsComposeSkills(t *testing.T) {
	cases := []struct {
		expert string
		turn   string
		skills []string
		tools  []string
	}{
		{"PPT专家", "请做一份介绍", []string{"tpl-slide-builder", "tpl-web-researcher", "tpl-mermaid-diagrams"}, []string{"web.search", "pptx.gen", "skill.invoke"}},
		{"报告编写专家", "写一份调研报告", []string{"tpl-web-researcher", "tpl-docx-writer", "tpl-anti-ai-prose"}, []string{"web.search", "docx.gen", "skill.invoke"}},
		{"小说编写专家", "写一章开篇", []string{"tpl-docx-writer", "tpl-anti-ai-prose", "tpl-fiction-continuity"}, []string{"docx.gen", "skill.invoke"}},
		{"Excel表格制作专家", "做半年财报表", []string{"tpl-excel-analyst", "tpl-csv-workbook"}, []string{"excel.gen", "excel.parse"}},
		{"UI专家", "画一个设置页", []string{"frontend-design", "ui-components", "design-system"}, []string{"html.gen", "skill.invoke"}},
		{"产品经理专家", "写一份 PRD", []string{"pm-skill", "tpl-grill-me", "tpl-to-spec"}, []string{"web.search", "skill.invoke"}},
		{"系统架构师专家", "画 C4", []string{"tpl-improve-architecture", "tpl-mermaid-diagrams"}, []string{"workspace.read", "skill.invoke"}},
		{"数据库设计专家", "设计订单库", []string{"tpl-mermaid-diagrams", "tpl-pm-phase-3"}, []string{"skill.invoke"}},
		{"系统项目结构规范专家", "整理目录树", []string{"tpl-knowledge-index", "tpl-mermaid-diagrams"}, []string{"workspace.list", "workspace.read"}},
		{"开发规范专家", "写 AGENTS.md", []string{"tpl-code-reviewer", "tpl-grill-me"}, []string{"skill.invoke", "workspace.read"}},
		{"系统测试专家", "写 E2E 场景", []string{"tpl-test-writer", "tpl-e2e-browser", "browser-automation"}, []string{"browser.act", "skill.invoke"}},
		{"硬件配置专家", "选一台办公电脑", []string{"tpl-web-researcher", "tpl-hardware-bom"}, []string{"web.search", "excel.gen"}},
		{"开发专家", "修这个编译错误", []string{"tpl-implement", "tpl-tdd-loop", "tpl-debugger"}, []string{"workspace.edit", "command.run", "skill.invoke"}},
	}
	published := []skill.Skill{
		catalogTestSkill("tpl-slide-builder", "演示文稿助手。", `{"triggers":["做 ppt"]}`),
		catalogTestSkill("tpl-web-researcher", "联网调研。", `{"triggers":["调研"]}`),
		catalogTestSkill("tpl-mermaid-diagrams", "Mermaid 结构图。", `{"triggers":["mermaid"]}`),
		catalogTestSkill("tpl-docx-writer", "文档撰写。", `{"triggers":["写文档"]}`),
		catalogTestSkill("tpl-anti-ai-prose", "去AI味。", `{"triggers":["去AI味"]}`),
		catalogTestSkill("tpl-fiction-continuity", "小说连续性。", `{"triggers":["连续性"]}`),
		catalogTestSkill("tpl-excel-analyst", "表格分析。", `{"triggers":["xlsx"]}`),
		catalogTestSkill("tpl-csv-workbook", "CSV 工作簿。", `{"triggers":["csv"]}`),
		catalogTestSkill("frontend-design", "前端设计。", `{"triggers":["UI"]}`),
		catalogTestSkill("ui-components", "组件。", `{"triggers":["组件"]}`),
		catalogTestSkill("design-system", "设计系统。", `{"triggers":["令牌"]}`),
		catalogTestSkill("pm-skill", "产品经理。", `{"triggers":["PRD"]}`),
		catalogTestSkill("tpl-grill-me", "深度追问。", `{"triggers":["追问"]}`),
		catalogTestSkill("tpl-to-spec", "写规格。", `{"triggers":["规格"]}`),
		catalogTestSkill("tpl-improve-architecture", "架构改进。", `{"triggers":["架构"]}`),
		catalogTestSkill("tpl-pm-phase-3", "数据库交付。", `{"triggers":["数据库"]}`),
		catalogTestSkill("tpl-knowledge-index", "知识索引。", `{"triggers":["索引"]}`),
		catalogTestSkill("tpl-code-reviewer", "代码审查。", `{"triggers":["审查"]}`),
		catalogTestSkill("tpl-test-writer", "测试补全。", `{"triggers":["单测"]}`),
		catalogTestSkill("tpl-e2e-browser", "E2E。", `{"triggers":["E2E"]}`),
		catalogTestSkill("browser-automation", "浏览器自动化。", `{"triggers":["填表"]}`),
		catalogTestSkill("tpl-hardware-bom", "硬件 BOM。", `{"triggers":["BOM"]}`),
		catalogTestSkill("tpl-implement", "驱动实现。", `{"triggers":["写代码"]}`),
		catalogTestSkill("tpl-tdd-loop", "TDD。", `{"triggers":["TDD"]}`),
		catalogTestSkill("tpl-debugger", "排障。", `{"triggers":["报错"]}`),
		catalogTestSkill("unrelated-skill", "无关。", `{"triggers":["无关"]}`),
	}
	for _, tc := range cases {
		t.Run(tc.expert, func(t *testing.T) {
			requests := make(chan gateway.Request, 1)
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			tools, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { tools.Close() })
			e.SetToolRuntime(tools)
			e.skills = &skillCatalogStub{items: published}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
				return chatAttachmentAdapter{requests: requests}, nil
			})
			turn := `[引用专家 ` + tc.expert + `|` + chatAttachmentProviderID + `] ` + tc.turn
			payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","executionMode":"full-access","messages":[{"role":"user","content":` + specialistJSONString(turn) + `}]}`
			response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
			if !response.OK {
				t.Fatalf("chat.start failed: %#v", response)
			}
			req := capturedChatRequest(t, requests)
			if !hasSpecialistOfficeAndWeb(req.Tools) {
				t.Fatalf("missing office+web+skills: %#v", specialistToolNames(req.Tools))
			}
			sys := ""
			if len(req.Messages) > 0 {
				sys = req.Messages[0].Content
			}
			if !strings.Contains(sys, "[专家自动挂载]") || !strings.Contains(sys, tc.expert) {
				t.Fatalf("compose hint missing for %s:\n%s", tc.expert, sys)
			}
			for _, name := range tc.skills {
				if !strings.Contains(sys, name) {
					t.Fatalf("%s compose missing skill %q:\n%s", tc.expert, name, sys)
				}
			}
			for _, tool := range tc.tools {
				if !strings.Contains(sys, tool) {
					t.Fatalf("%s compose missing tool %q:\n%s", tc.expert, tool, sys)
				}
			}
		})
	}
}

func TestSpecialistChatStartIncludesOfficeWebAndSkills(t *testing.T) {
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tools.Close() })
	e.SetToolRuntime(tools)
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("demo", "unused", `{}`)}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	turn := `[引用专家 PPT专家|` + chatAttachmentProviderID + `] 请做一份介绍`
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","executionMode":"full-access","messages":[{"role":"user","content":` + specialistJSONString(turn) + `}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	req := capturedChatRequest(t, requests)
	if !hasSpecialistOfficeAndWeb(req.Tools) {
		t.Fatalf("specialist session missing office+web+skills: %#v", specialistToolNames(req.Tools))
	}
	sys := ""
	if len(req.Messages) > 0 {
		sys = req.Messages[0].Content
	}
	for _, needle := range []string{"对话专家能力", "skill.invoke", "web.search", "mermaid", "pptx.gen", "desktop=true", "docx.gen", "报告"} {
		if !strings.Contains(sys, needle) {
			t.Fatalf("specialist runtime instruction missing %q:\n%s", needle, sys)
		}
	}
}

func specialistJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestDeliberateExpertOffersSpecialistTools(t *testing.T) {
	e := &Engine{}
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tools.Close() })
	e.SetToolRuntime(tools)
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("demo", "unused", `{}`)}}
	adapter := &deliberateToolsAdapter{}
	op := e.deliberateExpert(context.Background(), adapter, nil, "m", councilExpert{ID: "ppt", Name: "PPT专家", Body: "做演示"}, "做一份介绍", "", false, executionModeFullAccess, subTestSession)
	if op.Text != "独立意见" {
		t.Fatalf("opinion = %+v adapter=%+v", op, adapter)
	}
	if !hasSpecialistOfficeAndWeb(adapter.tools) {
		t.Fatalf("expert.deliberate stripped office/web tools: %#v", specialistToolNames(adapter.tools))
	}
	if adapter.disableReasoning {
		t.Fatal("desktop council must not disable thinking")
	}
}

type deliberateToolsAdapter struct {
	tools            []gateway.ToolDefinition
	disableReasoning bool
}

func (a *deliberateToolsAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	a.tools = req.Tools
	a.disableReasoning = req.DisableReasoning
	return gateway.Response{Message: gateway.Message{Content: "独立意见"}}, nil
}
func (a *deliberateToolsAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, nil
}
func (a *deliberateToolsAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
}

func TestCouncilChairInstructsTools(t *testing.T) {
	chair := councilChairInstruction(formatCouncilBrief("x", nil), false)
	if !strings.Contains(chair, "## 综合结论") {
		t.Fatalf("chair = %q", chair)
	}
	if !councilChairMustUseTools(chair) {
		t.Fatalf("chair must keep office/web/skills: %q", chair)
	}
}

func TestSpecialistToolAllowlist(t *testing.T) {
	defs := specialistToolDefinitions(append(engineToolDefinitions(), gateway.ToolDefinition{Name: "skill.invoke"}))
	if !hasSpecialistOfficeAndWeb(defs) {
		t.Fatalf("allowlist missing office+web: %#v", specialistToolNames(defs))
	}
	for _, d := range defs {
		if d.Name == "subagent.spawn" || strings.HasPrefix(d.Name, "cc.") {
			t.Fatalf("council tools should not include %s", d.Name)
		}
	}
}

func TestApplyExpertSpawnCapsOverridesResearchProfile(t *testing.T) {
	def, _, ok := resolveSubagentProfile(defaultSubagentChatPolicy(), "research")
	if !ok {
		t.Fatal("research missing")
	}
	got := applyExpertSpawnCaps(def)
	if !capsIncludeAll(got.ReadCaps, fullSubagentReadCaps()) {
		t.Fatalf("expert spawn caps = %#v", got.ReadCaps)
	}
}
