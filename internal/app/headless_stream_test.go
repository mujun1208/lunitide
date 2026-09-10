package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestHeadlessStreamWaitsForActualTerminalAfterStartAck(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	entered, finish := make(chan struct{}), make(chan struct{})
	adapter := budgetAdapter{run: func(ctx context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		close(entered)
		select {
		case <-finish:
		case <-ctx.Done():
			return llmadapter.Response{}, ctx.Err()
		}
		if err := emit(llmadapter.Delta{Text: strings.Repeat("完成", 400)}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: strings.Repeat("完成", 400)}, Usage: llmadapter.Usage{TotalTokens: 7}}, nil
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out := make(chan scheduler.Outcome, 1)
	go func() {
		out <- e.runHeadlessStream(ctx, validRequest("chat.start", `{"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","modelId":"model","messages":[{"role":"user","content":"你好"}]}`))
	}()
	select {
	case <-entered:
	case result := <-out:
		t.Fatalf("before provider: %+v", result)
	case <-ctx.Done():
		t.Fatal("stream never started")
	}
	select {
	case result := <-out:
		t.Fatalf("start ACK was mistaken for completion: %+v", result)
	case <-time.After(25 * time.Millisecond):
	}
	close(finish)
	select {
	case result := <-out:
		if result.Err != nil || len([]rune(result.Summary)) != 500 || result.TotalTokens != 7 {
			t.Fatalf("outcome: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("terminal never reached caller")
	}
}

func TestHeadlessAutomationRecordsPurposeAutomation(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
			if emit != nil {
				_ = emit(llmadapter.Delta{Text: "完成"})
			}
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "完成"}, Usage: llmadapter.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}, nil
		}}, nil
	})
	out := e.runHeadlessStream(withCallPurpose(context.Background(), "automation"), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","messages":[{"role":"user","content":"你好"}]}`))
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if len(store.recs) == 0 {
		t.Fatal("automation chat must record a metered attempt")
	}
	for _, rec := range store.recs {
		if rec.Purpose != "automation" {
			t.Fatalf("automation purpose = %+v", rec)
		}
	}
}

func TestHeadlessCollectorRejectsPersistenceFailureAndCancelledTerminal(t *testing.T) {
	for _, event := range []bridge.Event{{Type: bridge.EventCompleted, Completed: &bridge.CompletedEvent{PersistFailed: true}}, {Type: bridge.EventCancelled}, {Type: bridge.EventFailed}, {Type: bridge.EventApprovalRequired}} {
		c := &headlessCollector{done: make(chan struct{})}
		if err := c.emit(event); err != nil {
			t.Fatal(err)
		}
		select {
		case <-c.done:
		default:
			t.Fatal("terminal not signaled")
		}
		if c.outcome().Err == nil {
			t.Fatalf("false success: %+v", event)
		}
		if event.Type == bridge.EventCancelled && !errors.Is(c.outcome().Err, context.Canceled) {
			t.Fatal("cancel lost")
		}
	}
}

func TestAutomationRunKeepsSchedulerBudgetBeyondStartupDeadline(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	var observed time.Duration
	adapter := budgetAdapter{run: func(ctx context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return llmadapter.Response{}, errors.New("execution lost its bounded lifetime")
		}
		observed = time.Until(deadline)
		if err := emit(llmadapter.Delta{Text: "已完成新闻简报"}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "已完成新闻简报"}}, nil
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	job := scheduler.Job{ProviderID: chatAttachmentProviderID, ModelID: "model", SessionID: chatAttachmentSessionID, Prompt: "生成新闻简报"}
	out := e.AutomationHeadlessExecutor()(ctx, job)
	if out.Err != nil || observed <= time.Duration(bridge.ChatStartDeadlineMS)*time.Millisecond {
		t.Fatalf("whole run was cut down to startup budget: remaining=%v outcome=%+v", observed, out)
	}
	if observed > turnGenerationMaxTime || out.SessionID != job.SessionID || out.Summary != "已完成新闻简报" {
		t.Fatalf("generation budget or execution receipt lost: remaining=%v outcome=%+v", observed, out)
	}
}

func TestAutomationHeadlessWritesDispatchIdentityOnContinuation(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
			if emit != nil {
				_ = emit(llmadapter.Delta{Text: "已完成新闻简报"})
			}
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "已完成新闻简报"}}, nil
		}}, nil
	})
	job := scheduler.Job{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAJ", ProviderID: chatAttachmentProviderID, ModelID: "model",
		SessionID: chatAttachmentSessionID, Prompt: "生成新闻简报",
	}
	runID := "01ARZ3NDEKTSV4RRFFQ69G5FAR"
	out := e.runAutomationHeadless(context.Background(), job, scheduler.RunContext{
		RunID: runID, SessionID: job.SessionID, DispatchKey: job.ID + ":" + runID, Recover: false,
	})
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	got := e.loadTurnCheckpoint(job.SessionID)
	if got.Continuation == nil || got.Continuation.AutomationRunID != runID || got.Continuation.DispatchKey != job.ID+":"+runID {
		t.Fatalf("automation dispatch identity missing from continuation: %#v", got.Continuation)
	}
	if got.Continuation.Completeness == modelfit.CompletenessNativeComplete {
		t.Fatal("S1 must not claim native_complete on automation continuation")
	}
}

func TestHeadlessStartRefusalDoesNotClaimAnUnknownExecution(t *testing.T) {
	e := NewEngine(nil, "test")
	out := e.runHeadlessStream(context.Background(), validRequest("chat.start", `{}`))
	if out.Err == nil || !out.NotStarted {
		t.Fatalf("preflight rejection must distinguish unstarted work: %+v", out)
	}
}

func TestHeadlessChatStartFailsClosedWhenCatalogEmpty(t *testing.T) {
	e := NewEngineWithGateway(providerRepositoryStub{}, "test", streamTestLease{})
	out := e.runHeadlessStream(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","messages":[{"role":"user","content":"你好"}]}`))
	if out.Err == nil || !out.NotStarted {
		t.Fatalf("empty catalog must not start automation: %+v", out)
	}
	if !strings.Contains(out.Err.Error(), "CAPABILITY_NOT_READY") || !strings.Contains(out.Err.Error(), "请先配置并启用供应商、凭据和模型") {
		t.Fatalf("headless must surface HAT-05 Chinese + code: %v", out.Err)
	}
}
