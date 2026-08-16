package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/skillapp"
)

// skillCatalogStub backs the SkillService surface the catalog injection
// reads (List); every lifecycle method is out of scope for these tests.
type skillCatalogStub struct {
	items   []skill.Skill
	listErr error
}

func (s *skillCatalogStub) Get(context.Context, string) (*skill.Skill, error) {
	return nil, skillapp.ErrSkillNotFound
}
func (s *skillCatalogStub) List(_ context.Context, status skill.SkillStatus) ([]skill.Skill, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if status != "" && status != skill.SkillStatusPublished {
		return nil, nil
	}
	return append([]skill.Skill(nil), s.items...), nil
}
func (s *skillCatalogStub) Match(context.Context, string) ([]skill.SkillMatch, error) {
	return nil, nil
}
func (s *skillCatalogStub) Create(context.Context, skill.Skill) (skill.Skill, error) {
	return skill.Skill{}, nil
}
func (s *skillCatalogStub) InstallFromCatalog(context.Context, string) (skill.Skill, error) {
	return skill.Skill{}, nil
}
func (s *skillCatalogStub) UpdateFields(context.Context, string, *string, *string, *string, *string, []skill.PermissionLevel, *string, int64) (*skill.Skill, error) {
	return nil, nil
}
func (s *skillCatalogStub) Delete(context.Context, string) error    { return nil }
func (s *skillCatalogStub) Publish(context.Context, string) error   { return nil }
func (s *skillCatalogStub) Deprecate(context.Context, string) error { return nil }
func (s *skillCatalogStub) Disable(context.Context, string) error   { return nil }
func (s *skillCatalogStub) Invoke(context.Context, string, string, string, string) (skillapp.Invocation, error) {
	return skillapp.Invocation{}, nil
}
func (s *skillCatalogStub) Execute(context.Context, string, string, bool) (skillapp.Execution, error) {
	return skillapp.Execution{}, nil
}

func catalogTestSkill(name, description, manifest string) skill.Skill {
	return skill.Skill{
		Name: name, DisplayName: name, Description: description,
		Version: "1.0.0", Status: skill.SkillStatusPublished,
		Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint:  "builtin://test", ManifestJSON: manifest,
	}
}

func startSkillCatalogChat(t *testing.T, skills SkillService, executionMode string) (bridge.Response, <-chan gateway.Request) {
	t.Helper()
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.skills = skills
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","executionMode":"` + executionMode + `","messages":[{"role":"user","content":"hi"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	return response, requests
}

func capturedSkillChatSystem(t *testing.T, requests <-chan gateway.Request) string {
	t.Helper()
	req := capturedChatRequest(t, requests)
	if len(req.Messages) == 0 || req.Messages[0].Role != gateway.RoleSystem {
		t.Fatalf("first message is not system: %#v", req.Messages)
	}
	return req.Messages[0].Content
}

func TestChatStartInjectsSkillCatalog(t *testing.T) {
	stub := &skillCatalogStub{items: []skill.Skill{
		catalogTestSkill("manual-parser", "解析 AMM/IPC/SRM 手册章节并抽取工卡字段。更多格式细节见附录。", `{"triggers":["手册解析","工卡抽取"],"prompt":"x"}`),
		catalogTestSkill("report-writer", "生成合规检查报告。", `{"triggers":["生成报告","检查报告"]}`),
	}}
	response, requests := startSkillCatalogChat(t, stub, "approval")
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	sys := capturedSkillChatSystem(t, requests)
	for _, want := range []string{
		"Execution mode: approval.", // existing instruction preserved, then appended
		"[可用技能目录]",
		"- manual-parser：解析 AMM/IPC/SRM 手册章节并抽取工卡字段。当用户提到“手册解析、工卡抽取”时使用。",
		"- report-writer：生成合规检查报告。当用户提到“生成报告、检查报告”时使用。",
		"使用规则：当用户请求与某技能触发场景匹配时，先声明“将使用技能 X”，再执行。",
	} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system instruction missing %q:\n%s", want, sys)
		}
	}
	if !strings.HasPrefix(sys, "Execution mode: approval.") {
		t.Fatalf("existing instruction must stay first:\n%s", sys)
	}
}

func TestChatStartInjectsSkillCatalogInPlanMode(t *testing.T) {
	stub := &skillCatalogStub{items: []skill.Skill{
		catalogTestSkill("plan-skill", "规划辅助。", `{"triggers":["规划"]}`),
	}}
	response, requests := startSkillCatalogChat(t, stub, "plan")
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	sys := capturedSkillChatSystem(t, requests)
	if !strings.HasPrefix(sys, "Execution mode: plan.") {
		t.Fatalf("plan instruction missing:\n%s", sys)
	}
	if !strings.Contains(sys, "[可用技能目录]") || !strings.Contains(sys, "plan-skill") {
		t.Fatalf("skill catalog missing in plan mode:\n%s", sys)
	}
}

func TestChatStartSkillCatalogTruncatesAtMaxItems(t *testing.T) {
	items := make([]skill.Skill, 15)
	for i := range items {
		items[i] = catalogTestSkill(fmt.Sprintf("skill-%02d", i), "解析测试数据并生成摘要报告。", `{"triggers":["测试解析","生成摘要"]}`)
	}
	response, requests := startSkillCatalogChat(t, &skillCatalogStub{items: items}, "approval")
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	sys := capturedSkillChatSystem(t, requests)
	for i := 0; i < skillInjectMaxItems; i++ {
		if want := fmt.Sprintf("skill-%02d", i); !strings.Contains(sys, want) {
			t.Fatalf("first %d skills must be injected, missing %s:\n%s", skillInjectMaxItems, want, sys)
		}
	}
	for i := skillInjectMaxItems; i < 15; i++ {
		if banned := fmt.Sprintf("skill-%02d", i); strings.Contains(sys, banned) {
			t.Fatalf("skill beyond max items injected: %s:\n%s", banned, sys)
		}
	}
	if !strings.Contains(sys, "技能目录已截断") {
		t.Fatalf("truncation notice missing:\n%s", sys)
	}
}

func TestChatStartSkillCatalogTruncatesAtMaxBytes(t *testing.T) {
	items := make([]skill.Skill, 15)
	for i := range items {
		items[i] = catalogTestSkill(fmt.Sprintf("skill-%02d", i), strings.Repeat("长", 200), `{"triggers":["测试"]}`)
	}
	response, requests := startSkillCatalogChat(t, &skillCatalogStub{items: items}, "approval")
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	sys := capturedSkillChatSystem(t, requests)
	if !strings.Contains(sys, "技能目录已截断") {
		t.Fatalf("truncation notice missing:\n%s", sys)
	}
	if !strings.Contains(sys, "skill-00") {
		t.Fatalf("first skill missing:\n%s", sys)
	}
	if strings.Contains(sys, "skill-14") {
		t.Fatalf("last skill must be dropped by the byte budget:\n%s", sys)
	}
	if idx := strings.Index(sys, "[可用技能目录]"); idx < 0 || len(sys[idx:]) > skillInjectMaxBytes {
		t.Fatalf("catalog block exceeds %d bytes: %d", skillInjectMaxBytes, len(sys[idx:]))
	}
}

func TestChatStartSkillCatalogReadFailureDoesNotBlockChat(t *testing.T) {
	stub := &skillCatalogStub{listErr: errors.New("temporary storage failure")}
	response, requests := startSkillCatalogChat(t, stub, "approval")
	if !response.OK {
		t.Fatalf("chat.start must not be blocked by a skill read failure: %#v", response)
	}
	sys := capturedSkillChatSystem(t, requests)
	if strings.Contains(sys, "[可用技能目录]") {
		t.Fatalf("catalog must not be injected on read failure:\n%s", sys)
	}
	if !strings.HasPrefix(sys, "Execution mode: approval.") {
		t.Fatalf("base instruction missing on read failure:\n%s", sys)
	}
}
