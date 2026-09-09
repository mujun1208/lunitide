package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestChatModelStreamFailureDiagnosticsAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		source, want string
		retry        bool
	}{
		{"RESPONSE_TRUNCATED", "UPSTREAM_RESPONSE_TRUNCATED", false},
		{"RESPONSE_FILTERED", "UPSTREAM_RESPONSE_FILTERED", false},
		{"MALFORMED_RESPONSE", "UPSTREAM_MALFORMED_RESPONSE", true},
		{"REQUEST_TOO_LARGE", "REQUEST_TOO_LARGE", false},
		{"OUTCOME_UNKNOWN", "UPSTREAM_OUTCOME_UNKNOWN", false},
		{"MODEL_CHANNEL_UNAVAILABLE", "MODEL_CHANNEL_UNAVAILABLE", false},
		{"STREAM_INCOMPLETE", "UPSTREAM_STREAM_INCOMPLETE", true},
		{"UPSTREAM_STREAM_FAILED", "UPSTREAM_STREAM_FAILED", true},
		{"STREAM_BAD_REQUEST", "UPSTREAM_BAD_REQUEST", false},
		{"STREAM_AUTHENTICATION_FAILED", "PROVIDER_AUTHENTICATION_FAILED", false},
		{"STREAM_ACCESS_DENIED", "PROVIDER_ACCESS_DENIED", false},
		{"STREAM_NOT_FOUND", "UPSTREAM_NOT_FOUND", false},
		{"STREAM_RATE_LIMITED", "PROVIDER_RATE_LIMITED", true},
		{"STREAM_UNAVAILABLE", "UPSTREAM_UNAVAILABLE", true},
		{"STREAM_OVERLOADED", "UPSTREAM_UNAVAILABLE", true},
		{"RESPONSE_TOO_LARGE", "UPSTREAM_RESPONSE_TOO_LARGE", false},
		{"RESPONSE_BODY_TOO_LARGE", "UPSTREAM_RESPONSE_BODY_TOO_LARGE", false},
		{"RESPONSE_LINE_TOO_LARGE", "UPSTREAM_RESPONSE_LINE_TOO_LARGE", false},
		{"RESPONSE_EVENT_TOO_LARGE", "UPSTREAM_RESPONSE_EVENT_TOO_LARGE", false},
		{"CONNECTION_REFUSED", "UPSTREAM_CONNECTION_FAILED", true},
		{"CONNECTION_FAILED", "UPSTREAM_CONNECTION_FAILED", true},
		{"DNS_ERROR", "UPSTREAM_DNS_FAILED", true},
		{"TLS_ERROR", "UPSTREAM_TLS_FAILED", false},
		{"SSRF_BLOCKED", "UPSTREAM_CONNECTION_BLOCKED", false},
		{"REDIRECT_BLOCKED", "UPSTREAM_CONNECTION_BLOCKED", false},
		{"HTTPS_REQUIRED", "UPSTREAM_CONNECTION_BLOCKED", false},
		{"CANCELLED", "UPSTREAM_CANCELLED", true},
		{"TIMEOUT", "UPSTREAM_TIMEOUT", true},
		{"SECRET-CANARY", "UPSTREAM_FAILED", true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			err := &llmadapter.Error{Code: tc.source, Stage: llmadapter.StageStream, Message: "SECRET-CANARY https://user:password@example.test?key=SECRET-CANARY"}
			got := chatModelStreamError(err)
			if got.Code != tc.want || got.Retryable != tc.retry || !strings.Contains(got.Message, "响应流") {
				t.Fatalf("error=%+v", got)
			}
			diagnostic := chatModelFailureDiagnostic(err)
			if !strings.Contains(diagnostic, "stage=stream") || strings.Contains(diagnostic+got.Message, "SECRET-CANARY") {
				t.Fatalf("unsafe diagnostic: %s %+v", diagnostic, got)
			}
		})
	}
	for _, err := range []error{
		errors.New("SECRET-CANARY"),
		&llmadapter.Error{Code: "SECRET-CANARY", Stage: "SECRET-CANARY", Message: "SECRET-CANARY", HTTPStatus: -200},
		&networkpolicy.Error{Code: networkpolicy.CodeDNSError, Err: errors.New("SECRET-CANARY")},
	} {
		if strings.Contains(chatModelFailureDiagnostic(err)+chatModelStreamError(err).Message, "SECRET-CANARY") {
			t.Fatal("raw failure leaked")
		}
	}
	if got := chatModelStreamError(&llmadapter.Error{HTTPStatus: 404}); got.Code != "UPSTREAM_NOT_FOUND" || got.Retryable {
		t.Fatalf("not found=%+v", got)
	}
}

func TestChatModelLocalAdapterFailuresAreExplicitAndSanitized(t *testing.T) {
	const canary = "SECRET-CANARY https://user:password@example.test?key=SECRET-CANARY"
	for _, tc := range []struct {
		source, want, message string
		stage                 llmadapter.Stage
		status                int
		retry                 bool
	}{
		{"MALFORMED_RESPONSE", "UPSTREAM_MALFORMED_RESPONSE", "模型返回格式不完整，已保留收到的内容，请重试", llmadapter.StageDecode, 200, true},
		{"REQUEST_TOO_LARGE", "REQUEST_TOO_LARGE", "请求内容过大，请减少附件或上下文后重试", llmadapter.StageDecode, 0, false},
		{"OUTCOME_UNKNOWN", "UPSTREAM_OUTCOME_UNKNOWN", "模型请求结果尚无法确认，请先检查任务状态和已生成文件，避免重复执行", llmadapter.StageConnect, 0, false},
		{"MODEL_CHANNEL_UNAVAILABLE", "MODEL_CHANNEL_UNAVAILABLE", "当前模型没有可用的供应商通道，请检查模型通道配置或联系供应商", llmadapter.StageHTTP, 503, false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			upstream := &llmadapter.Error{Code: tc.source, Stage: tc.stage, HTTPStatus: tc.status, Message: canary}
			err := fmt.Errorf("%s: %w", canary, upstream)
			got := chatModelStreamError(err)
			if got.Code != tc.want || got.Message != tc.message || got.Retryable != tc.retry {
				t.Fatalf("error=%+v", got)
			}
			diagnostic := chatModelFailureDiagnostic(err)
			wantDiagnostic := fmt.Sprintf("code=%s stage=%s http_status=%d", tc.want, tc.stage, tc.status)
			if diagnostic != wantDiagnostic {
				t.Fatalf("diagnostic=%q, want %q", diagnostic, wantDiagnostic)
			}
			notice := chatModelOutcomeNotice(false, err)
			if notice != turnErrorNotice+tc.message || strings.Contains(diagnostic+got.Message+notice, "SECRET-CANARY") {
				t.Fatal("local adapter failure leaked vendor text or lost its diagnosis")
			}
		})
	}
}

type workflowStreamFailureAdapter struct {
	draftTrialAdapter
	failure   error
	partial   string
	loadTrial bool
}

func TestOfficeFallbackEmptyReplyKeepsModelFailureDiagnosis(t *testing.T) {
	storeEngine, _, sid, _ := messageEngine(t)
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.messages, e.sessions = storeEngine.messages, storeEngine.sessions
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e.tools = runtime
	defer e.StopChatMemoryWorkers()
	failure := &llmadapter.Error{Code: "RESPONSE_TOO_LARGE", Stage: llmadapter.StageStream, Message: "SECRET-CANARY"}
	a := &workflowStreamFailureAdapter{failure: failure}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	var events []bridge.Event
	e.runStream(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", state,
		provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"},
		llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "生成文档：将已提供的新闻参考资料转成 Word"}}},
		func(event bridge.Event) error { events = append(events, event); return nil }, sid, executionModeFullAccess)
	terminal := events[len(events)-1]
	if terminal.Type != bridge.EventFailed || terminal.Error.Code != "UPSTREAM_RESPONSE_TOO_LARGE" || len(a.requests) != 1 {
		t.Fatalf("terminal=%+v calls=%d", terminal, len(a.requests))
	}
	page, err := e.messages.List(ctx, messageapp.PageRequest{SessionID: sid})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("saved messages=%+v err=%v", page, err)
	}
	text := page.Items[0].Text
	if !strings.Contains(text, chatModelStreamError(failure).Message) || strings.Contains(text, "SECRET-CANARY") || strings.Contains(text, "请继续补齐实际内容") {
		t.Fatalf("model failure was hidden or misattributed: %s", text)
	}
	if artifacts := e.loadSessionArtifactsByMessage(sid)[page.Items[0].ID]; len(artifacts) != 0 {
		t.Fatalf("failed model response fabricated artifacts: %+v", artifacts)
	}
	for _, event := range events {
		if event.Tool != nil && event.Tool.Artifact != nil {
			t.Fatal("failed model response emitted an artifact")
		}
	}
}

func (a *workflowStreamFailureAdapter) Stream(ctx context.Context, secret []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if a.loadTrial && len(a.requests) == 0 {
		return a.draftTrialAdapter.Stream(ctx, secret, req, emit)
	}
	a.requests = append(a.requests, req)
	if a.partial != "" {
		if err := emit(llmadapter.Delta{Text: a.partial}); err != nil {
			return llmadapter.Response{}, err
		}
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: a.partial}, Usage: llmadapter.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}}, a.failure
}

func TestWeeklyTrialStreamFailureAfterLoadingKeepsDraftAndFails(t *testing.T) {
	e, _, sk := draftTrialFixture(t, "WEEKLY-CONSTRAINT: preserve supplied facts")
	a := &workflowStreamFailureAdapter{draftTrialAdapter: draftTrialAdapter{skillID: sk.ID}, loadTrial: true, partial: "已读取周报样例，文档尚未生成。", failure: &llmadapter.Error{Code: "CONNECTION_REFUSED", Stage: llmadapter.StageStream, Message: "SECRET-CANARY"}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	ctx, cancel := context.WithCancel(withSkillTrials(context.Background(), chatAttachmentSessionID, []string{sk.ID}))
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["weekly-stream-failure"] = state
	p, _ := (chatAttachmentProvider{}).Get(ctx, chatAttachmentProviderID)
	var events []bridge.Event
	e.runStream(ctx, "weekly-stream-failure", state, p, llmadapter.Request{Model: "model", Tools: []llmadapter.ToolDefinition{skillTrialToolDefinition()}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "试用周报草稿并生成 Word 文档"}}}, func(ev bridge.Event) error { events = append(events, ev); return nil }, chatAttachmentSessionID, executionModeFullAccess)
	if len(a.requests) != 2 {
		t.Fatalf("calls=%d", len(a.requests))
	}
	loaded := false
	for _, m := range a.requests[1].Messages {
		if m.Role == llmadapter.RoleTool && strings.Contains(m.Content, "WEEKLY-CONSTRAINT") {
			loaded = true
		}
	}
	if !loaded {
		t.Fatal("failure fixture never loaded the skill")
	}
	terminal := events[len(events)-1]
	if terminal.Type != bridge.EventFailed || terminal.Error.Code != "UPSTREAM_CONNECTION_FAILED" {
		t.Fatalf("terminal=%+v", terminal)
	}
	var text string
	for _, ev := range events {
		if ev.Delta != nil {
			text += ev.Delta.Text
		}
		if ev.Tool != nil && ev.Tool.Artifact != nil {
			t.Fatal("failure fabricated an artifact")
		}
	}
	if !strings.Contains(text, a.partial) || strings.Contains(text, "SECRET-CANARY") {
		t.Fatalf("partial content lost or unsafe: %s", text)
	}
	current, _ := e.skills.Get(ctx, sk.ID)
	if current.Status != skill.SkillStatusDraft {
		t.Fatal("failed trial changed draft state")
	}
}

func TestAutomationStreamFailurePersistsSeparateFailedRun(t *testing.T) {
	for _, tc := range []struct {
		name          string
		failure       *llmadapter.Error
		want, partial string
	}{
		{"bad request keeps tools", &llmadapter.Error{Code: "HTTP_400", Stage: llmadapter.StageHTTP, HTTPStatus: 400, Message: "SECRET-CANARY"}, "UPSTREAM_BAD_REQUEST", ""},
		{"interrupted partial", &llmadapter.Error{Code: "STREAM_INCOMPLETE", Stage: llmadapter.StageStream, Message: "SECRET-CANARY"}, "UPSTREAM_STREAM_INCOMPLETE", "已取得部分新闻线索，尚未完成核实。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			runtime, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			e.SetToolRuntime(runtime)
			a := &workflowStreamFailureAdapter{failure: tc.failure, partial: tc.partial}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
			store, err := scheduler.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			job := scheduler.Job{ID: ulid.Make().String(), Name: "News stream regression", Cron: "0 8 * * *", ProviderID: chatAttachmentProviderID, ModelID: "model", SessionID: chatAttachmentSessionID, Prompt: "搜索最新 AI 新闻并提供来源", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
			if err := store.PutJob(job); err != nil {
				t.Fatal(err)
			}
			old := scheduler.Run{ID: ulid.Make().String(), JobID: job.ID, State: scheduler.RunFailed, Error: "original UPSTREAM_FAILED", StartedAt: time.Now().Add(-time.Hour), FinishedAt: time.Now().Add(-time.Minute)}
			if err := store.AppendRun(old); err != nil {
				t.Fatal(err)
			}
			s := scheduler.New(store, e.AutomationHeadlessExecutor(), quietAutomationNotifier{})
			defer s.Close()
			if err := s.TriggerNow(job.ID); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				runs, err := store.LatestRuns(job.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(runs) == 2 && runs[0].ID != old.ID && runs[0].State != scheduler.RunRunning {
					got := runs[0]
					if got.State != scheduler.RunFailed || !strings.Contains(got.Error, tc.want) || strings.Contains(got.Error+got.Summary, "SECRET-CANARY") || got.FinishedAt.IsZero() || got.TotalTokens != 5 {
						t.Fatalf("failed receipt=%+v", got)
					}
					if tc.partial != "" && !strings.Contains(got.Summary, tc.partial) {
						t.Fatal("partial work lost")
					}
					if runs[1].ID != old.ID || runs[1].Error != old.Error || runs[1].State != old.State {
						t.Fatal("original history changed")
					}
					if len(a.requests) != 1 || len(a.requests[0].Tools) == 0 {
						t.Fatalf("tool request degraded or retried: %d", len(a.requests))
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("no final automation receipt")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
