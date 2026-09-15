package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

type roundTripConnector struct {
	rt http.RoundTripper
}

func (c roundTripConnector) NewRequest(ctx context.Context, method, p string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, "https://upstream.test/v1/"+p, body)
}

func (c roundTripConnector) Do(r *http.Request) (*http.Response, error) {
	return c.rt.RoundTrip(r)
}

func (c roundTripConnector) ReadSSE(r io.Reader) ([]byte, bool, error) {
	raw, err := io.ReadAll(r)
	return raw, true, err
}

func startChatTurnThatWouldCallModel(t *testing.T, transport http.RoundTripper) (llmadapter.Response, error) {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(&memCalls{failPut: true})
	inner := llmadapter.NewOpenAI(roundTripConnector{rt: transport}, llmadapter.Options{MaxAttempts: 1})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return inner, nil
	})
	a, err := e.adapter(context.Background(), provider.Provider{
		ID:       ulid.Make().String(),
		Protocol: provider.ProtocolOpenAICompatible,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: "chat"})
	return a.Complete(ctx, []byte("k"), llmadapter.Request{
		Model:    "m",
		Mode:     "standard",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "hi"}},
	})
}

func TestCallAttemptIntentFailureDoesNotSendHTTP(t *testing.T) {
	var sends int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[]}`))}, nil
	})
	// wire adapter with transport; store.PutCallAttemptIntent returns error
	_, err := startChatTurnThatWouldCallModel(t, transport)
	if sends != 0 {
		t.Fatalf("intent persist failed but HTTP sent %d times", sends)
	}
	if err == nil {
		t.Fatal("expected admit error, not a silent generate")
	}
}

type recordingBudget struct {
	mu      sync.Mutex
	digests []string
	reject  bool
}

func (r *recordingBudget) AdmitCall(_ context.Context, _ agentrun.ExecutionScope, est agentrun.CallEstimate) (agentrun.CallPermit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.digests = append(r.digests, est.RequestDigest)
	if r.reject {
		return agentrun.CallPermit{}, agentrun.ErrExecutionBudget
	}
	return agentrun.CallPermit{ReservationID: "res-" + est.AttemptID, AttemptID: est.AttemptID}, nil
}

func (r *recordingBudget) MarkDispatched(context.Context, agentrun.CallPermit) error { return nil }
func (r *recordingBudget) SettleCall(context.Context, agentrun.CallSettlement) error {
	return nil
}
func (r *recordingBudget) ReleaseUnsent(context.Context, agentrun.CallPermit, string) error {
	return nil
}
func (r *recordingBudget) Snapshot(context.Context, agentrun.ExecutionScope) (agentrun.BudgetSnapshot, error) {
	return agentrun.BudgetSnapshot{}, nil
}
func (r *recordingBudget) TransitionActivity(context.Context, agentrun.ExecutionScope, agentrun.ActivityTransition) (agentrun.ActivityResult, error) {
	return agentrun.ActivityResult{}, nil
}

func (r *recordingBudget) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.digests...)
}

func glmPreflightProfile() modelfit.ModelProfile {
	clear := false
	return modelfit.ModelProfile{
		ProfileID:       "glm-preflight-test",
		Family:          "glm",
		Protocol:        "openai_compatible",
		ContextWindow:   16000,
		MaxOutputTokens: 32768,
		Modes: map[string]modelfit.ModeParameters{
			"quick":    {ThinkingType: "enabled", Effort: "low", ClearThinking: &clear},
			"standard": {ThinkingType: "enabled", Effort: "high", ClearThinking: &clear},
			"deep":     {ThinkingType: "enabled", Effort: "max", ClearThinking: &clear},
		},
	}
}

func preflightAdapter(t *testing.T, transport http.RoundTripper, store *memCalls, budget agentrun.ExecutionBudget) llmadapter.Adapter {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(store)
	e.SetExecutionBudget(budget)
	e.SetCompileProfile(glmPreflightProfile())
	inner := llmadapter.NewOpenAI(roundTripConnector{rt: transport}, llmadapter.Options{MaxAttempts: 1})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return inner, nil
	})
	a, err := e.adapter(context.Background(), provider.Provider{
		ID:       ulid.Make().String(),
		Protocol: provider.ProtocolOpenAICompatible,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func okChatResponse() *http.Response {
	return &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(
			`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
		)),
	}
}

func TestFinalInputPreflightEveryAttempt(t *testing.T) {
	var sends int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends++
		return okChatResponse(), nil
	})
	budget := &recordingBudget{}
	a := preflightAdapter(t, transport, &memCalls{}, budget)
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Task: ulid.Make().String(), Purpose: "chat"})

	base := llmadapter.Request{
		Model:    "glm-5.3",
		Mode:     "standard",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "summarize paged evidence"}},
		Tools:    []llmadapter.ToolDefinition{{Name: "page.read", Description: "read a page", Schema: json.RawMessage(`{"type":"object"}`)}},
	}
	if _, err := a.Complete(ctx, []byte("k"), base); err != nil {
		t.Fatalf("base attempt: %v", err)
	}

	withTool := base
	withTool.Messages = append(append([]llmadapter.Message{}, base.Messages...),
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "call-1", Name: "page.read", Arguments: json.RawMessage(`{"page":1}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "call-1", Content: strings.Repeat("evidence ", 400)},
	)
	if _, err := a.Complete(ctx, []byte("k"), withTool); err != nil {
		t.Fatalf("tool-return attempt: %v", err)
	}

	withSchema := withTool
	withSchema.Tools = []llmadapter.ToolDefinition{{
		Name:        "page.read",
		Description: "read a page",
		Schema:      json.RawMessage(`{"type":"object","properties":{"page":{"type":"integer"}}}`),
	}}
	if _, err := a.Complete(ctx, []byte("k"), withSchema); err != nil {
		t.Fatalf("schema attempt: %v", err)
	}

	retry := withSchema
	retry.Mode = "quick"
	retry.MaxTokens = 256
	if _, err := a.Complete(ctx, []byte("k"), retry); err != nil {
		t.Fatalf("retry-param attempt: %v", err)
	}

	digests := budget.recorded()
	if len(digests) != 4 {
		t.Fatalf("BeforeSend/AdmitCall count=%d want 4 (compile+admit every attempt)", len(digests))
	}
	if digests[0] == "" || digests[0] == digests[1] {
		t.Fatalf("large tool return must recompile digest: %q %q", digests[0], digests[1])
	}
	if digests[1] == digests[2] {
		t.Fatalf("new tool schema must recompile digest: %q", digests[1])
	}
	if digests[2] == digests[3] {
		t.Fatalf("retry parameter change must recompile digest: %q", digests[2])
	}

	beforeReject := sends
	budget.reject = true
	for _, purpose := range []string{"chat", "office", "hub"} {
		scope := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: purpose})
		_, err := a.Complete(scope, []byte("k"), retry)
		if err == nil {
			t.Fatalf("purpose %s: expected budget reject", purpose)
		}
	}
	if sends != beforeReject {
		t.Fatalf("budget reject must leave mock HTTP count unchanged: before=%d after=%d", beforeReject, sends)
	}
}

type rejectBindUoW struct{}

func (rejectBindUoW) TransactExecutionBudget(context.Context, func(agentrunapp.ExecutionBudgetTx) error) error {
	return errors.New("session unbound")
}

func TestProductionCallMeterRequiresAdmitCallBeforeHTTP(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(&memCalls{})
	AttachProductionExecutionBudget(e, rejectBindUoW{})
	var sends int
	inner := llmadapter.NewOpenAI(roundTripConnector{rt: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends++
		return okChatResponse(), nil
	})}, llmadapter.Options{MaxAttempts: 1})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return inner, nil
	})
	a, err := e.adapter(context.Background(), provider.Provider{
		ID:       ulid.Make().String(),
		Protocol: provider.ProtocolOpenAICompatible,
	})
	if err != nil {
		t.Fatal(err)
	}
	meter, ok := a.(meteredAdapter)
	if !ok || meter.budget == nil {
		t.Fatal("production Engine construction left budget nil on the metered adapter")
	}

	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: "chat"})
	_, err = a.Complete(ctx, []byte("k"), llmadapter.Request{
		Model:    "m",
		Mode:     "standard",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "hi"}},
	})
	if sends != 0 {
		t.Fatalf("chat send reached Connector.Do without AdmitCall: HTTP sent %d times", sends)
	}
	if err == nil {
		t.Fatal("unbound chat send must fail closed")
	}
}

func TestCompiledHTTPBodySHAEqualsAdmitDigest(t *testing.T) {
	var bodySHA string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		bodySHA = hex.EncodeToString(sum[:])
		return okChatResponse(), nil
	})
	budget := &recordingBudget{}
	a := preflightAdapter(t, transport, &memCalls{}, budget)
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Task: ulid.Make().String(), Purpose: "chat"})
	req := llmadapter.Request{
		Model:    "glm-5.3",
		Mode:     "quick",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "hi"}},
	}
	if _, err := a.Complete(ctx, []byte("k"), req); err != nil {
		t.Fatal(err)
	}
	digests := budget.recorded()
	if len(digests) != 1 || digests[0] == "" {
		t.Fatalf("admit digest missing: %v", digests)
	}
	if bodySHA != digests[0] {
		t.Fatalf("wire body SHA %s != admit digest %s", bodySHA, digests[0])
	}
}

type reservationCountingUoW struct {
	agentrunapp.ExecutionBudgetUoW
	n int
}

func (u *reservationCountingUoW) TransactExecutionBudget(ctx context.Context, fn func(agentrunapp.ExecutionBudgetTx) error) error {
	return u.ExecutionBudgetUoW.TransactExecutionBudget(ctx, func(tx agentrunapp.ExecutionBudgetTx) error {
		return fn(&reservationCountingTx{ExecutionBudgetTx: tx, uow: u})
	})
}

type reservationCountingTx struct {
	agentrunapp.ExecutionBudgetTx
	uow *reservationCountingUoW
}

func (t *reservationCountingTx) InsertExecutionReservation(row agentrunapp.ExecutionReservationRow) error {
	if err := t.ExecutionBudgetTx.InsertExecutionReservation(row); err != nil {
		return err
	}
	t.uow.n++
	return nil
}

func TestMeetingNotesAndExpertTrialAdmitCallWithoutSessionULID(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "t07-dedicated.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(&memCalls{})
	counted := &reservationCountingUoW{ExecutionBudgetUoW: store.AgentRuntimeRepository()}
	AttachProductionExecutionBudget(e, counted)

	var sends int
	inner := llmadapter.NewOpenAI(roundTripConnector{rt: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends++
		return okChatResponse(), nil
	})}, llmadapter.Options{MaxAttempts: 1})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return inner, nil
	})
	a, err := e.adapter(context.Background(), provider.Provider{
		ID:       ulid.Make().String(),
		Protocol: provider.ProtocolOpenAICompatible,
	})
	if err != nil {
		t.Fatal(err)
	}
	if meter, ok := a.(meteredAdapter); !ok || meter.budget == nil {
		t.Fatal("production Engine construction left budget nil on the metered adapter")
	}

	req := llmadapter.Request{
		Model:    "m",
		Mode:     "standard",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "hi"}},
	}
	meetingID := ulid.Make().String()
	scopes := []continuityScope{
		{Owner: "meeting", Task: "meeting", Purpose: "meeting"},
		{Owner: "meeting:" + meetingID, Task: "meeting:" + meetingID, Purpose: "meeting"},
	}
	for _, scope := range scopes {
		before := sends
		beforeAdmit := counted.n
		_, err = a.Complete(withContinuityScope(context.Background(), scope), []byte("k"), req)
		if errors.Is(err, errExecutionUnbound) {
			t.Fatalf("scope %+v: meeting notes must AdmitCall without a sessions ULID, got %v", scope, err)
		}
		if err != nil {
			t.Fatalf("scope %+v: %v", scope, err)
		}
		if counted.n != beforeAdmit+1 {
			t.Fatalf("scope %+v: T06 AdmitCall reservation count=%d want %d", scope, counted.n, beforeAdmit+1)
		}
		if sends != before+1 {
			t.Fatalf("scope %+v: expected AdmitCall then HTTP, sends=%d", scope, sends)
		}
	}

	before := sends
	beforeAdmit := counted.n
	_, err = a.Complete(withCallPurpose(context.Background(), "expert"), []byte("k"), req)
	if errors.Is(err, errExecutionUnbound) {
		t.Fatal("expert trial must AdmitCall without a sessions ULID, got errExecutionUnbound")
	}
	if err != nil {
		t.Fatalf("expert trial: %v", err)
	}
	if counted.n != beforeAdmit+1 {
		t.Fatalf("expert trial: T06 AdmitCall reservation count=%d want %d", counted.n, beforeAdmit+1)
	}
	if sends != before+1 {
		t.Fatalf("expert trial must AdmitCall then HTTP, sends=%d", sends)
	}

	before = sends
	beforeAdmit = counted.n
	_, err = a.Complete(withCallPurpose(context.Background(), "diagnostic"), []byte("k"), req)
	if errors.Is(err, errExecutionUnbound) {
		t.Fatal("provider.test must AdmitCall without a sessions ULID, got errExecutionUnbound")
	}
	if err != nil {
		t.Fatalf("provider.test: %v", err)
	}
	if counted.n != beforeAdmit+1 {
		t.Fatalf("provider.test: T06 AdmitCall reservation count=%d want %d", counted.n, beforeAdmit+1)
	}
	if sends != before+1 {
		t.Fatalf("provider.test must AdmitCall then HTTP, sends=%d", sends)
	}

	before = sends
	beforeAdmit = counted.n
	_, err = a.Complete(withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: "chat"}), []byte("k"), req)
	if sends != before {
		t.Fatalf("unbound chat send reached Connector.Do without a session: HTTP sent %d times", sends-before)
	}
	if counted.n != beforeAdmit {
		t.Fatalf("unbound chat must not AdmitCall: reservations=%d", counted.n-beforeAdmit)
	}
	if err == nil {
		t.Fatal("chat without a session ULID must stay fail-closed")
	}
}

func TestMeteredDisableReasoningSurvivesEffectiveCompile(t *testing.T) {
	var body []byte
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		body = raw
		return okChatResponse(), nil
	})
	a := preflightAdapter(t, transport, &memCalls{}, &recordingBudget{})
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Task: ulid.Make().String(), Purpose: "chat"})
	req := llmadapter.Request{
		Model:            "glm-5.3",
		Mode:             "standard",
		DisableReasoning: true,
		Messages:         []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "hi"}},
	}
	if _, err := a.Complete(ctx, []byte("k"), req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"type":"disabled"`) || !strings.Contains(string(body), `"enable_thinking":false`) {
		t.Fatalf("metered DisableReasoning lost after Effective compile: %s", body)
	}
}
