package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

// TestS1G1Contracts locks the landable P07 slice: resume stays verify-only,
// OCR is not a seventh capability role, interrupted work is not replayed,
// and usage never invents a measured turn or a savings rate.
func TestS1G1Contracts(t *testing.T) {
	t.Run("resume-verify-only", testS1G1ResumeIsVerifyOnly)
	t.Run("ocr-not-capability-role", testS1G1OCRIsNotCapabilityRole)
	t.Run("recover-no-replay", testS1G1RecoverDoesNotReplay)
	t.Run("recover-call-attempts-unknown", testS1G1RecoverCallAttemptsStayUnknown)
	t.Run("usage-unknown-no-savings", testS1G1UsageUnknownNoInventedSavings)
	t.Run("hide-keeps-streams", testS1G1HideToTrayDoesNotCancelStreams)
	t.Run("cancel-does-not-revive", testS1G1CancelDoesNotReviveAfterRecover)
}

func testS1G1ResumeIsVerifyOnly(t *testing.T) {
	ctx := context.Background()
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "g1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	e.SetToolOperationStore(store)

	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session,
		ToolName: "files.apply", InputDigest: strings.Repeat("ab", 32),
		EffectClass: modelfit.EffectLocalReversible, State: modelfit.OpRunning,
		ExpectedVersion: 1, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetToolOperation(ctx, session, op.ID)
	if err != nil || got.State != modelfit.OpUnknown {
		t.Fatalf("interrupted apply must become unknown: %+v %v", got, err)
	}
	if action := modelfit.ResumeDecision(got.State, got.EffectClass); action != modelfit.ResumeVerifyUnknown {
		t.Fatalf("unknown must stay verify-only: %q", action)
	}

	desktop := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session,
		ToolName: "desktop.click", InputDigest: strings.Repeat("cd", 32),
		EffectClass: modelfit.EffectDesktopInteractive, State: modelfit.OpUnknown,
		ExpectedVersion: 2, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, desktop); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{op.ID, desktop.ID} {
		loaded, err := store.GetToolOperation(ctx, session, id)
		if err != nil {
			t.Fatal(err)
		}
		req := validRequest("operation.resume", `{"sessionId":"`+session+`","operationId":"`+id+`","expectedVersion":`+strconv.Itoa(loaded.ExpectedVersion)+`}`)
		req.IdempotencyKey = ulid.Make().String()
		resp := e.Handle(ctx, req)
		if !resp.OK {
			t.Fatalf("resume %#v", resp.Error)
		}
		raw, _ := json.Marshal(resp.Payload)
		out := mustDecodePayload[struct {
			Executed     bool   `json:"executed"`
			ResumeAction string `json:"resumeAction"`
			State        string `json:"state"`
		}](t, resp.Payload)
		if out.Executed || out.State != "unknown" || out.ResumeAction != string(modelfit.ResumeVerifyUnknown) {
			t.Fatalf("unknown/desktop resume must not re-run: %s", raw)
		}
	}

	readOp := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session,
		ToolName: "workspace.read", InputDigest: strings.Repeat("ef", 32),
		EffectClass: modelfit.EffectReadOnly, State: modelfit.OpFailed,
		ExpectedVersion: 1, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, readOp); err != nil {
		t.Fatal(err)
	}
	req := validRequest("operation.resume", `{"sessionId":"`+session+`","operationId":"`+readOp.ID+`","expectedVersion":1}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(ctx, req)
	if !resp.OK {
		t.Fatalf("read resume %#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Executed bool `json:"executed"`
	}](t, resp.Payload)
	if !out.Executed {
		t.Fatal("failed read_only may execute reread")
	}

	body, err := os.ReadFile(sentinel)
	if err != nil || string(body) != "untouched" {
		t.Fatalf("resume must not apply fileops: %q %v", body, err)
	}
}

func testS1G1OCRIsNotCapabilityRole(t *testing.T) {
	if provider.ValidKind("ocr") {
		t.Fatal("KindOCR must stay unused; OCR is not a catalog kind")
	}
	if provider.NormalizeKind("ocr") != provider.KindLLM {
		t.Fatalf("unknown ocr kind must not become a dedicated catalog: %q", provider.NormalizeKind("ocr"))
	}

	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(&memoryRoleStore{})
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	e.SetOCR(svc)

	routing := e.Handle(context.Background(), validRequest("ocr.routing.get", `{}`))
	if !routing.OK {
		t.Fatalf("ocr.routing stays independent %#v", routing.Error)
	}
	snap := mustDecodePayload[struct {
		Revision string `json:"revision"`
	}](t, routing.Payload)
	if snap.Revision == "" {
		t.Fatal("ocr routing must return its own CAS revision")
	}

	seven := map[string]any{
		"expectedRevision": sqlite.CapabilityRolesRevision(nil),
		"roles": []map[string]any{
			{"role": "chat"}, {"role": "flash"}, {"role": "vision"},
			{"role": "embed"}, {"role": "judge"}, {"role": "gui"},
			{"role": "ocr"},
		},
	}
	raw, _ := json.Marshal(seven)
	req := validRequest("capability.roles.set", string(raw))
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("seventh OCR role must be rejected %#v", resp)
	}

	swap := map[string]any{
		"expectedRevision": sqlite.CapabilityRolesRevision(nil),
		"roles": []map[string]any{
			{"role": "chat"}, {"role": "flash"}, {"role": "vision"},
			{"role": "embed"}, {"role": "judge"}, {"role": "ocr"},
		},
	}
	raw, _ = json.Marshal(swap)
	req = validRequest("capability.roles.set", string(raw))
	req.IdempotencyKey = ulid.Make().String()
	resp = e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("replacing gui with ocr must be rejected %#v", resp)
	}
}

func testS1G1RecoverDoesNotReplay(t *testing.T) {
	root := t.TempDir()
	store, err := scheduler.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := scheduler.Job{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", Name: "g1", Cron: "at:2026-01-01T00:00:00Z",
		Prompt: "生成日报", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "gpt-test",
		SessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Enabled: true,
	}
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendRun(scheduler.Run{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAZ", JobID: job.ID, State: scheduler.RunRunning,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	recovered, ok, err := store.GetJob(job.ID)
	if err != nil || !ok || recovered.Enabled {
		t.Fatalf("interrupted job must be disabled: %+v %v", recovered, err)
	}
	runs, err := store.ListRuns(job.ID, 10)
	if err != nil || len(runs) == 0 || !runs[0].OutcomeUnknown || runs[0].State != scheduler.RunFailed {
		t.Fatalf("recovery must mark unknown, not succeed: %+v %v", runs, err)
	}
	for _, run := range runs {
		if run.State == scheduler.RunSucceeded {
			t.Fatalf("recovery invented a successful replay: %+v", runs)
		}
	}

	reopened, err := scheduler.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	job2, ok, err := reopened.GetJob(job.ID)
	if err != nil || !ok || job2.Enabled {
		t.Fatalf("job must stay disabled after second recover: %+v %v", job2, err)
	}
	runs2, err := reopened.ListRuns(job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs2 {
		if run.State == scheduler.RunSucceeded {
			t.Fatalf("second recover replayed work: %+v", runs2)
		}
	}
}

func testS1G1RecoverCallAttemptsStayUnknown(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "g1-calls.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session := ulid.Make().String()
	now := time.Now().UTC()
	if err := store.PutCallAttemptIntent(ctx, sqlite.CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: session, TaskID: session,
		CallID: "call-crash", AttemptID: "attempt-1", Purpose: "chat",
		Status: string(modelfit.CallIntent), Integrity: string(modelfit.UsageUnknown),
		CostStatus: "unknown", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterruptedCallAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetCallAttempt(ctx, session, "call-crash", "attempt-1")
	if err != nil || got.Status != string(modelfit.CallUnknown) || got.InputTokens != 0 || got.OutputTokens != 0 {
		t.Fatalf("crash recover must mark unknown and not invent tokens: %+v %v", got, err)
	}

	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(store)
	resp := e.Handle(ctx, validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), "savings") || strings.Contains(string(raw), "native_complete") || strings.Contains(string(raw), "%") {
		t.Fatalf("recovered usage invented a savings claim: %s", raw)
	}
	out := mustDecodePayload[struct {
		Integrity    string `json:"integrity"`
		InputTokens  int    `json:"inputTokens"`
		OutputTokens int    `json:"outputTokens"`
		Attempts     []struct {
			Status    string `json:"status"`
			Integrity string `json:"integrity"`
		} `json:"attempts"`
	}](t, resp.Payload)
	if out.Integrity != "unknown" || out.InputTokens != 0 || out.OutputTokens != 0 {
		t.Fatalf("recovered ledger must stay unknown: %+v", out)
	}
	if len(out.Attempts) != 1 || out.Attempts[0].Status != "unknown" || out.Attempts[0].Integrity != "unknown" {
		t.Fatalf("recovered attempt must stay unknown: %+v", out.Attempts)
	}
}

func testS1G1CancelDoesNotReviveAfterRecover(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "g1-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session := ulid.Make().String()
	now := time.Now().UTC()
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session,
		ToolName: "desktop.type", InputDigest: strings.Repeat("ab", 32),
		EffectClass: modelfit.EffectDesktopInteractive, State: modelfit.OpCancelled,
		ExpectedVersion: 2, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolOperationStore(store)
	req := validRequest("operation.resume", `{"sessionId":"`+session+`","operationId":"`+op.ID+`","expectedVersion":2}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(ctx, req)
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Executed     bool   `json:"executed"`
		State        string `json:"state"`
		ResumeAction string `json:"resumeAction"`
		ResumeHint   string `json:"resumeHint"`
	}](t, resp.Payload)
	if out.Executed || out.State != "cancelled" || out.ResumeAction != string(modelfit.ResumeKeepStopped) {
		t.Fatalf("cancelled resume must stay stopped: %+v", out)
	}
	if !strings.Contains(out.ResumeHint, "不要复活") {
		t.Fatalf("hint must forbid revival: %+v", out)
	}
}

func testS1G1HideToTrayDoesNotCancelStreams(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	cancelled := false
	state := &streamState{cancel: func() { cancelled = true }, state: streamRunning}
	e.streams["hide-keep"] = state
	if cancelled || state.state != streamRunning {
		t.Fatal("hide-to-tray must leave in-flight work running")
	}
	e.CancelAllStreams()
	if !cancelled || state.state != streamCancelling {
		t.Fatalf("explicit exit must cancel streams: cancelled=%v state=%v", cancelled, state.state)
	}
}

func testS1G1UsageUnknownNoInventedSavings(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(&memCalls{})
	session := ulid.Make().String()
	resp := e.Handle(context.Background(), validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), "savings") || strings.Contains(string(raw), "native_complete") || strings.Contains(string(raw), "%") {
		t.Fatalf("usage invented a completeness or savings claim: %s", raw)
	}
	out := mustDecodePayload[struct {
		Collected    bool   `json:"collected"`
		Integrity    string `json:"integrity"`
		InputTokens  int    `json:"inputTokens"`
		OutputTokens int    `json:"outputTokens"`
		Attempts     []any  `json:"attempts"`
	}](t, resp.Payload)
	if out.Collected || out.Integrity != "unknown" || len(out.Attempts) != 0 {
		t.Fatalf("empty ledger must stay unknown: %+v", out)
	}
	if out.InputTokens != 0 || out.OutputTokens != 0 {
		t.Fatalf("empty ledger must not invent token counts: %+v", out)
	}
}

