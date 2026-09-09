package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m8app"
)

const manualExpertPayload = `{"source":"local","frontmatter":{"name":"Short Drama Expert","division":"design","description":"Story editor","semver":"1.0.0"},"sixSection":{"identity":"drama identity","mission":"dramatic mission","rules":"no invented facts","workflow":"outline then scenes","deliverableTemplate":"scene dialogue","successMetrics":"clear conflict"},"requestId":"manual-create"}`

type expertTrialAdapter struct {
	request      llmadapter.Request
	calls        int
	output       string
	err          error
	finishReason llmadapter.FinishReason
	toolCalls    []llmadapter.ToolCall
}

type expertTrialProviders struct{ providerRepositoryStub }

func (expertTrialProviders) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FB1", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://expert-trial.invalid", CredentialRef: "expert-trial-test-credential",
		Status: provider.StatusEnabled, CredentialState: provider.CredentialConfigured,
		Models: []provider.Model{{ModelID: "expert-trial-model", Kind: provider.KindLLM, IsDefault: true}},
	}, nil
}

func (p expertTrialProviders) List(ctx context.Context, _ provider.Filter) ([]provider.Provider, error) {
	// Trials resolve a model through List; a Get-only fixture has no model catalog.
	item, err := p.Get(ctx, "")
	return []provider.Provider{item}, err
}

func (a *expertTrialAdapter) Complete(_ context.Context, _ []byte, req llmadapter.Request) (llmadapter.Response, error) {
	a.request = req
	a.calls++
	return llmadapter.Response{Message: llmadapter.Message{Role: "assistant", Content: a.output, ToolCalls: a.toolCalls}, FinishReason: a.finishReason}, a.err
}

func (*expertTrialAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("expert trial unexpectedly called Stream")
}

func (*expertTrialAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("expert trial unexpectedly called Discover")
}

func createExpertThroughBridge(t *testing.T, e *Engine) m8app.CreateResult {
	t.Helper()
	r := e.Handle(context.Background(), nominationRequest("expert.create", manualExpertPayload))
	if !r.OK {
		t.Fatalf("create: %+v", r.Error)
	}
	var created m8app.CreateResult
	if err := json.Unmarshal(mustJSON(r.Payload), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Short Drama Expert" || created.State != "disabled" || created.CreationOrigin != "manual" {
		t.Fatalf("create response: %+v", created)
	}
	return created
}

func TestExpertBridgeTrialDoesNotEnableAndReportsFailure(t *testing.T) {
	e := NewEngineWithGateway(expertTrialProviders{}, "test", streamTestLease{})
	e.SetM8ExpertService(newExpertSkillsEngine(t).m8expert)
	a := &expertTrialAdapter{output: "A scene built around a clear conflict."}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	x := createExpertThroughBridge(t, e)
	request := func(method string, payload any) bridge.Response {
		return e.Handle(context.Background(), nominationRequest(method, string(mustJSON(payload))))
	}
	payload := map[string]any{"expertId": x.ExpertID, "expectedVersionId": x.VersionID, "input": "Draft an opening scene"}
	r := request("expert.try", payload)
	if !r.OK {
		t.Fatalf("trial: %+v (adapter calls=%d)", r.Error, a.calls)
	}
	if a.calls != 1 || a.request.Model != "expert-trial-model" || !a.request.DisableReasoning || len(a.request.Tools) != 0 || len(a.request.Messages) != 2 || a.request.Messages[1].Content != payload["input"] {
		t.Fatalf("trial request: %+v", a.request)
	}
	for _, part := range []string{"drama identity", "dramatic mission", "no invented facts", "outline then scenes", "scene dialogue", "clear conflict"} {
		if !strings.Contains(a.request.Messages[0].Content, part) {
			t.Fatalf("missing section %s", part)
		}
	}
	if !strings.Contains(string(mustJSON(r.Payload)), a.output) {
		t.Fatalf("answer missing: %+v", r.Payload)
	}
	a.err = errors.New("private provider response must not leak")
	r = request("expert.try", payload)
	if r.OK || r.Error.Code != "EXPERT_TRIAL_FAILED" || strings.Contains(r.Error.Message, "private") {
		t.Fatalf("failure: %+v", r)
	}
	a.err = nil
	a.output = ""
	if r = request("expert.try", payload); r.OK {
		t.Fatal("empty answer passed")
	}
	if r = request("expert.list", map[string]string{"creationOrigin": "manual"}); !r.OK || !strings.Contains(string(mustJSON(r.Payload)), `"state":"disabled"`) {
		t.Fatalf("trial enabled expert: %+v", r)
	}
	if r = request("expert.toggle", map[string]any{"expertId": x.ExpertID, "enabled": true}); !r.OK {
		t.Fatalf("enable: %+v", r.Error)
	}
	if r = request("expert.delete", m8app.ExpertDeleteInput{ExpertID: x.ExpertID, ExpectedVersionID: x.VersionID, ConfirmToken: m8app.ExpertDeleteToken(x.ExpertID, x.VersionID)}); !r.OK {
		t.Fatalf("delete: %+v", r.Error)
	}
	calls := a.calls
	if r = request("expert.try", payload); r.OK || a.calls != calls {
		t.Fatal("deleted expert trial reached model")
	}
}

func TestExpertTrialFinishReasonRejectsIncompleteTextWithoutEnabling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason llmadapter.FinishReason
		tools  []llmadapter.ToolCall
		code   string
	}{
		{name: "stop", reason: llmadapter.FinishReasonStop},
		{name: "missing reason preserves compatibility"},
		{name: "length", reason: llmadapter.FinishReasonLength, code: "EXPERT_TRIAL_INCOMPLETE"},
		{name: "content filter", reason: llmadapter.FinishReasonContentFilter, code: "EXPERT_TRIAL_FILTERED"},
		{name: "tool finish without tool payload", reason: llmadapter.FinishReasonToolCalls, code: "EXPERT_TRIAL_FAILED"},
		{name: "tools rejected even with stop", reason: llmadapter.FinishReasonStop, tools: []llmadapter.ToolCall{{ID: "call_1", Name: "unexpected"}}, code: "EXPERT_TRIAL_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngineWithGateway(expertTrialProviders{}, "test", streamTestLease{})
			e.SetM8ExpertService(newExpertSkillsEngine(t).m8expert)
			a := &expertTrialAdapter{output: "Nonempty model text.", finishReason: tc.reason, toolCalls: tc.tools}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
			x := createExpertThroughBridge(t, e)
			before, err := e.m8expert.Detail(context.Background(), m8app.DetailInput{ExpertID: x.ExpertID})
			if err != nil {
				t.Fatal(err)
			}
			r := e.Handle(context.Background(), nominationRequest("expert.try", string(mustJSON(map[string]any{"expertId": x.ExpertID, "expectedVersionId": x.VersionID, "input": "A complete scene"}))))
			if tc.code == "" {
				if !r.OK || !strings.Contains(string(mustJSON(r.Payload)), a.output) {
					t.Fatalf("complete answer rejected: %+v", r)
				}
			} else if r.OK || r.Error == nil || r.Error.Code != tc.code || r.Payload != nil {
				t.Fatalf("incomplete/tool response passed or leaked partial text: %+v", r)
			}
			if a.calls != 1 || !a.request.DisableReasoning || len(a.request.Tools) != 0 || a.request.MaxTokens != 2000 || a.request.MaxAttempts != 1 || !strings.Contains(a.request.Messages[0].Content, "concise, complete answer") {
				t.Fatalf("unexpected trial request: %+v calls=%d", a.request, a.calls)
			}
			after, err := e.m8expert.Detail(context.Background(), m8app.DetailInput{ExpertID: x.ExpertID})
			if err != nil || !reflect.DeepEqual(before, after) || after.Expert["state"] != "disabled" || len(after.Versions) != 1 {
				t.Fatalf("trial mutated expert: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}

func TestExpertBridgeTrialWithoutModelCatalogStaysDisabled(t *testing.T) {
	e := NewEngineWithGateway(providerRepositoryStub{}, "test", streamTestLease{})
	e.SetM8ExpertService(newExpertSkillsEngine(t).m8expert)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		t.Fatal("empty catalog must not reach the adapter")
		return nil, errors.New("unexpected adapter call")
	})
	x := createExpertThroughBridge(t, e)
	r := e.Handle(context.Background(), nominationRequest("expert.try", string(mustJSON(map[string]any{
		"expertId": x.ExpertID, "expectedVersionId": x.VersionID, "input": "Write a scene",
	}))))
	if r.OK || r.Error.Code != "EXPERT_TRIAL_FAILED" || !r.Error.Retryable {
		t.Fatalf("missing model response: %+v", r)
	}
	detail, err := e.m8expert.Detail(context.Background(), m8app.DetailInput{ExpertID: x.ExpertID})
	if err != nil || detail.Expert["state"] != "disabled" || len(detail.Versions) != 1 {
		t.Fatalf("missing model changed the expert: %+v %v", detail, err)
	}
}

func TestExpertCreateToolDescriptionKeepsDossier(t *testing.T) {
	e := newExpertSkillsEngine(t)
	for _, d := range e.expertToolDefinitions() {
		if d.Name != "expert.create" {
			continue
		}
		if !strings.Contains(d.Description, "GFM") || !strings.Contains(d.Description, "mcp:") || strings.Contains(d.Description, "tell the user to confirm skills in Expert Center") {
			t.Fatal(d.Description)
		}
		return
	}
	t.Fatal("expert.create missing")
}

func TestExpertToolCreationBindsMatchedSkillAndMCPKeys(t *testing.T) {
	e := newExpertSkillsEngine(t)
	out, err := e.invokeExpertCreateTool(context.Background(), "", json.RawMessage(`{"name":"网络漫剧爆款编剧专家","division":"design","description":"短剧与AI漫剧编剧","semver":"1.0.0","identity":"资深编剧","mission":"出爆款剧本","rules":"不泄密","workflow":"先定人设再写集","deliverableTemplate":"分集大纲","successMetrics":"完播与转发","skillKeys":["web-researcher","mcp:playwright"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "网络漫剧爆款编剧专家") || !strings.Contains(out.Output, `"state":"disabled"`) {
		t.Fatalf("creation output: %s", out.Output)
	}
	var created struct {
		ExpertID string `json:"expertId"`
	}
	body := out.Output[strings.Index(out.Output, "{"):]
	if json.Unmarshal([]byte(body), &created) != nil || created.ExpertID == "" {
		t.Fatalf("expertId missing: %s", out.Output)
	}
	keys, err := e.m8expert.ListBoundSkills(context.Background(), created.ExpertID)
	if err != nil || len(keys) < 2 {
		t.Fatalf("bound: %#v %v", keys, err)
	}
	hasSkill, hasMCP := false, false
	for _, key := range keys {
		if key == "web-researcher" {
			hasSkill = true
		}
		if key == "mcp:playwright" {
			hasMCP = true
		}
	}
	if !hasSkill || !hasMCP {
		t.Fatalf("matching keys not persisted: %#v", keys)
	}
}

func TestExpertToolCreationReturnsNamedDisabledManualExpert(t *testing.T) {
	e := newExpertSkillsEngine(t)
	out, err := e.invokeExpertCreateTool(context.Background(), "", json.RawMessage(`{"name":"Short Drama Expert","division":"design","description":"Story editor","semver":"1.0.0","identity":"i","mission":"m","rules":"r","workflow":"w","deliverableTemplate":"d","successMetrics":"s"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Short Drama Expert", `"state":"disabled"`, `"creationOrigin":"manual"`} {
		if !strings.Contains(out.Output, text) {
			t.Fatalf("creation output: %s", out.Output)
		}
	}
	list, err := e.m8expert.List(context.Background(), m8app.ExpertFilter{CreationOrigin: "manual"})
	if err != nil || len(list.Experts) != 1 || list.Experts[0].State != "disabled" {
		t.Fatalf("created list: %+v %v", list, err)
	}
}
