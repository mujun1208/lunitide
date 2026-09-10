package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func mustDecodePayload[T any](t *testing.T, payload any) T {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func TestChatUsageGetReturnsStablePrefixHash(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(&memCalls{})
	session := ulid.Make().String()
	resp := e.Handle(context.Background(), validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Collected        bool   `json:"collected"`
		StablePrefixHash string `json:"stablePrefixHash"`
	}](t, resp.Payload)
	if out.Collected || out.StablePrefixHash != "" {
		t.Fatalf("empty ledger must not advertise a prefix fingerprint: %+v", out)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), `"durationMs":0`) {
		t.Fatalf("empty ledger invented a 0 duration row: %s", raw)
	}
}

func TestChatUsageGetMarksLegacyUncollected(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(&memCalls{})
	session := ulid.Make().String()
	resp := e.Handle(context.Background(), validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Collected bool   `json:"collected"`
		Integrity string `json:"integrity"`
	}](t, resp.Payload)
	if out.Collected || out.Integrity != "unknown" {
		t.Fatalf("legacy must stay uncollected: %+v", out)
	}
}

func TestChatUsageGetReturnsRecordedAttempts(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	session := ulid.Make().String()
	_ = store.PutCallAttemptIntent(context.Background(), sqlite.CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: session, CallID: "c1", AttemptID: "a1",
		Purpose: "chat", Status: "succeeded", Integrity: string(modelfit.UsageReported),
		InputTokens: 11, OutputTokens: 3, CachedInputTokens: 2,
	})
	resp := e.Handle(context.Background(), validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Collected   bool `json:"collected"`
		InputTokens int  `json:"inputTokens"`
		Attempts    []struct {
			CallID string `json:"callId"`
		} `json:"attempts"`
	}](t, resp.Payload)
	if !out.Collected || out.InputTokens != 11 || len(out.Attempts) != 1 || out.Attempts[0].CallID != "c1" {
		t.Fatalf("ledger %+v", out)
	}
	got := mustDecodePayload[struct {
		StablePrefixHash string `json:"stablePrefixHash"`
	}](t, resp.Payload)
	if got.StablePrefixHash != typedDefaultStablePrefixHash() {
		t.Fatalf("collected ledger must include fixed-segment fingerprint, got %q", got.StablePrefixHash)
	}
}

func TestChatUsageGetExposesEfficiencySnapshotWithoutSavingsPercent(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	session := ulid.Make().String()
	_ = store.PutCallAttemptIntent(context.Background(), sqlite.CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: session, CallID: "c1", AttemptID: "a1",
		Purpose: "chat", Status: "succeeded", Integrity: string(modelfit.UsageReported),
		InputTokens: 11, OutputTokens: 3, PolicyVersion: "token-efficiency-v1", BytesBefore: 80, BytesAfter: 50,
	})
	resp := e.Handle(context.Background(), validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), "%") || strings.Contains(string(raw), "savings") {
		t.Fatalf("usage payload invented a savings rate: %s", raw)
	}
	out := mustDecodePayload[struct {
		Attempts []struct {
			PolicyVersion string `json:"policyVersion"`
			BytesBefore   int    `json:"bytesBefore"`
			BytesAfter    int    `json:"bytesAfter"`
		} `json:"attempts"`
	}](t, resp.Payload)
	if len(out.Attempts) != 1 || out.Attempts[0].PolicyVersion != "token-efficiency-v1" || out.Attempts[0].BytesBefore != 80 || out.Attempts[0].BytesAfter != 50 {
		t.Fatalf("bridge snapshot %+v", out)
	}
}

func TestChatUsageGetIncludesAttemptDuration(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	session := ulid.Make().String()
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	_ = store.PutCallAttemptIntent(context.Background(), sqlite.CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: session, CallID: "c1", AttemptID: "a1",
		Purpose: "chat", Status: "succeeded", Integrity: string(modelfit.UsageReported),
		InputTokens: 11, OutputTokens: 3, CostStatus: "unknown",
		StartedAt: start, EndedAt: start.Add(time.Second),
	})
	resp := e.Handle(context.Background(), validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), "%") || strings.Contains(string(raw), `"costStatus":"ok"`) {
		t.Fatalf("usage invented a price or percent: %s", raw)
	}
	out := mustDecodePayload[struct {
		Collected bool `json:"collected"`
		Attempts  []struct {
			DurationMS int    `json:"durationMs"`
			CostStatus string `json:"costStatus"`
		} `json:"attempts"`
	}](t, resp.Payload)
	if !out.Collected || len(out.Attempts) != 1 || out.Attempts[0].DurationMS != 1000 {
		t.Fatalf("duration %+v", out)
	}
	if out.Attempts[0].CostStatus != "unknown" {
		t.Fatalf("pass through stored unknown costStatus only: %+v", out.Attempts[0])
	}
}

func TestChatUsageGetDurationAfterMeteredComplete(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return compactionCompleteAdapter{}, nil
	})
	session := ulid.Make().String()
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible}
	a, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: session, Purpose: "chat"})
	if _, err := a.Complete(ctx, nil, llmadapter.Request{Model: "model"}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListCallAttempts(context.Background(), session, "", 0)
	if err != nil || len(listed) != 1 || listed[0].StartedAt.IsZero() || listed[0].EndedAt.IsZero() {
		t.Fatalf("metered finish must keep both timestamps: %+v %v", listed, err)
	}
	resp := e.Handle(context.Background(), validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Collected bool `json:"collected"`
		Attempts  []struct {
			DurationMS int    `json:"durationMs"`
			CostStatus string `json:"costStatus"`
		} `json:"attempts"`
	}](t, resp.Payload)
	if !out.Collected || len(out.Attempts) != 1 {
		t.Fatalf("metered usage %+v", out)
	}
	if out.Attempts[0].DurationMS < 0 || out.Attempts[0].CostStatus != "unknown" {
		t.Fatalf("duration/cost %+v", out.Attempts[0])
	}
}

func TestOCRRoutingGetIncludesLastFailureAndLocalReady(t *testing.T) {
	e := NewEngineWithGateway(roleCatalog{}, "test", streamTestLease{})
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	e.SetOCR(svc)
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(ocrapp.Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		return "", errors.New("401 unauthorized")
	})
	svc.SetLocalImage(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "ok"}}}, nil
	})
	if _, err := svc.RecognizeImage(context.Background(), []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}); err != nil {
		t.Fatal(err)
	}
	got := e.Handle(context.Background(), validRequest("ocr.routing.get", `{}`))
	if !got.OK {
		t.Fatalf("get %#v", got.Error)
	}
	raw, _ := json.Marshal(got.Payload)
	if strings.Contains(string(raw), `"failures":0`) {
		t.Fatalf("do not invent zero failures: %s", raw)
	}
	out := mustDecodePayload[struct {
		LastFailure *struct {
			Class     string `json:"class"`
			Operation string `json:"operation"`
			Until     string `json:"until"`
		} `json:"lastFailure"`
		LocalReady struct {
			PDF     bool   `json:"pdf"`
			Image   bool   `json:"image"`
			Backend string `json:"backend"`
		} `json:"localReady"`
	}](t, got.Payload)
	if out.LastFailure == nil || out.LastFailure.Class != "auth" || out.LastFailure.Operation != "image-ocr" || out.LastFailure.Until == "" {
		t.Fatalf("lastFailure %+v", out)
	}
	if out.LocalReady.Backend != "windows-ocr" && out.LocalReady.Backend != "unavailable" {
		t.Fatalf("localReady %+v", out.LocalReady)
	}
}

func TestOCRRoutingGetSetAndConflict(t *testing.T) {
	e := NewEngineWithGateway(roleCatalog{}, "test", streamTestLease{})
	e.SetOCR(ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json"))))
	got := e.Handle(context.Background(), validRequest("ocr.routing.get", `{}`))
	if !got.OK {
		t.Fatalf("get %#v", got.Error)
	}
	snap := mustDecodePayload[struct {
		Revision       string `json:"revision"`
		PreferProvider bool   `json:"preferProvider"`
	}](t, got.Payload)
	if snap.Revision == "" || !snap.PreferProvider {
		t.Fatalf("default %+v", snap)
	}
	set := validRequest("ocr.routing.set", `{"preferProvider":false,"expectedRevision":"`+snap.Revision+`"}`)
	set.IdempotencyKey = ulid.Make().String()
	saved := e.Handle(context.Background(), set)
	if !saved.OK {
		t.Fatalf("set %#v", saved.Error)
	}
	stale := validRequest("ocr.routing.set", `{"preferProvider":true,"expectedRevision":"`+snap.Revision+`"}`)
	stale.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), stale); resp.OK || resp.Error == nil || resp.Error.Code != "SETTINGS_VERSION_CONFLICT" {
		t.Fatalf("conflict %#v", resp)
	}
}

func TestOperationResumeIsVerifyOnly(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)
	session := ulid.Make().String()
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session, ToolName: "workspace.write",
		InputDigest: "ab", EffectClass: modelfit.EffectLocalReversible, State: modelfit.OpUnknown,
		ExpectedVersion: 2, Attempt: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := ops.PutToolOperationIntent(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	req := validRequest("operation.resume", `{"sessionId":"`+session+`","operationId":"`+op.ID+`","expectedVersion":2}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Executed     bool   `json:"executed"`
		ResumeAction string `json:"resumeAction"`
		State        string `json:"state"`
	}](t, resp.Payload)
	if out.Executed || out.ResumeAction != string(modelfit.ResumeVerifyUnknown) || out.State != "unknown" {
		t.Fatalf("resume must not re-run: %+v", out)
	}
}

func TestOperationGetExposesExternalIDAndResumeQueriesExisting(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)
	session := ulid.Make().String()
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session, ToolName: "video.generate",
		InputDigest: "cd", EffectClass: modelfit.EffectRemoteTrackable, State: modelfit.OpRunning,
		ExpectedVersion: 2, Attempt: 1, ExternalID: "supplier-job-9",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := ops.PutToolOperationIntent(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	got := e.Handle(context.Background(), validRequest("operation.get", `{"sessionId":"`+session+`","operationId":"`+op.ID+`"}`))
	if !got.OK {
		t.Fatalf("get %#v", got.Error)
	}
	view := mustDecodePayload[struct {
		ExternalID   string `json:"externalId"`
		ResumeAction string `json:"resumeAction"`
		ResumeHint   string `json:"resumeHint"`
	}](t, got.Payload)
	if view.ExternalID != "supplier-job-9" || view.ResumeAction != string(modelfit.ResumeQueryExisting) {
		t.Fatalf("remote job must be visible for query-existing: %+v", view)
	}
	if !strings.Contains(view.ResumeHint, "不要重新生成") {
		t.Fatalf("resume hint must tell the operator not to regenerate: %+v", view)
	}
	req := validRequest("operation.resume", `{"sessionId":"`+session+`","operationId":"`+op.ID+`","expectedVersion":2}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if !resp.OK {
		t.Fatalf("resume %#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		Executed     bool   `json:"executed"`
		ExternalID   string `json:"externalId"`
		ResumeAction string `json:"resumeAction"`
	}](t, resp.Payload)
	if !out.Executed || out.ExternalID != "supplier-job-9" || out.ResumeAction != string(modelfit.ResumeQueryExisting) {
		t.Fatalf("remote_trackable resume may query the existing job, never mint a new id: %+v", out)
	}
}

func TestOperationCancelStorageErrorIsRetryable(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	ops := &memToolOps{failCancel: errors.New("disk unavailable")}
	e.SetToolOperationStore(ops)
	session := ulid.Make().String()
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session, ToolName: "files.apply",
		InputDigest: "ab", EffectClass: modelfit.EffectLocalReversible, State: modelfit.OpRunning,
		ExpectedVersion: 1, Attempt: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := ops.PutToolOperationIntent(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	req := validRequest("operation.cancel", `{"sessionId":"`+session+`","operationId":"`+op.ID+`","expectedVersion":1}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "STORAGE_UNAVAILABLE" || !resp.Error.Retryable {
		t.Fatalf("storage cancel must be retryable, not version-changed: %#v", resp.Error)
	}
	if strings.Contains(strings.ToLower(resp.Error.Message), "disk unavailable") {
		t.Fatalf("must not leak English storage: %#v", resp.Error)
	}
}

func TestOperationCancelVersionConflictStaysInputChanged(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)
	session := ulid.Make().String()
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session, ToolName: "files.apply",
		InputDigest: "ab", EffectClass: modelfit.EffectLocalReversible, State: modelfit.OpRunning,
		ExpectedVersion: 1, Attempt: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := ops.PutToolOperationIntent(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	req := validRequest("operation.cancel", `{"sessionId":"`+session+`","operationId":"`+op.ID+`","expectedVersion":9}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "INPUT_CHANGED" {
		t.Fatalf("stale cancel must stay INPUT_CHANGED: %#v", resp.Error)
	}
}

func TestOperationCancelUnknownKeepsOutcomeButStopsResume(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)
	session := ulid.Make().String()
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: session, SessionID: session, ToolName: "image.generate",
		InputDigest: "ab", EffectClass: modelfit.EffectRemoteTrackable, State: modelfit.OpUnknown,
		ExpectedVersion: 3, Attempt: 1, ExternalID: "supplier-job-8",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := ops.PutToolOperationIntent(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	req := validRequest("operation.cancel", `{"sessionId":"`+session+`","operationId":"`+op.ID+`","expectedVersion":3}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		State        string `json:"state"`
		ResumeAction string `json:"resumeAction"`
		ResumeHint   string `json:"resumeHint"`
		ExternalID   string `json:"externalId"`
	}](t, resp.Payload)
	if out.State != "unknown" || out.ResumeAction != string(modelfit.ResumeKeepStopped) {
		t.Fatalf("cancel-requested unknown must stay unknown and stopped: %+v", out)
	}
	if out.ExternalID != "supplier-job-8" {
		t.Fatalf("must keep the supplier id for verify: %+v", out)
	}
	if !strings.Contains(out.ResumeHint, "不要复活") {
		t.Fatalf("hint must forbid revival: %+v", out)
	}
	resume := validRequest("operation.resume", `{"sessionId":"`+session+`","operationId":"`+op.ID+`","expectedVersion":4}`)
	resume.IdempotencyKey = ulid.Make().String()
	got := e.Handle(context.Background(), resume)
	if !got.OK {
		t.Fatalf("resume %#v", got.Error)
	}
	view := mustDecodePayload[struct {
		Executed     bool   `json:"executed"`
		State        string `json:"state"`
		ResumeAction string `json:"resumeAction"`
	}](t, got.Payload)
	if view.Executed || view.State != "unknown" || view.ResumeAction != string(modelfit.ResumeKeepStopped) {
		t.Fatalf("resume must stay verify-only and stopped: %+v", view)
	}
}

func TestFilesPlanApplyStatusUndoBridge(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.pdf"), []byte("%PDF-1.4"), 0600); err != nil {
		t.Fatal(err)
	}
	planReq := validRequest("files.plan", `{"sessionId":"`+session+`","recipe":"classify","files":["a.pdf"]}`)
	planReq.IdempotencyKey = ulid.Make().String()
	plan := e.Handle(context.Background(), planReq)
	if !plan.OK {
		t.Fatalf("plan %#v", plan.Error)
	}
	created := mustDecodePayload[struct {
		ID    string `json:"id"`
		Items []struct {
			From         string `json:"from"`
			SourceDigest string `json:"sourceDigest"`
		} `json:"items"`
	}](t, plan.Payload)
	if created.ID == "" {
		t.Fatalf("plan payload %+v", created)
	}
	digestOK := false
	for _, item := range created.Items {
		if item.From == "" {
			continue
		}
		if len(item.SourceDigest) != 64 {
			t.Fatalf("sourceDigest must be 64 hex when from exists: %+v", item)
		}
		if _, err := hex.DecodeString(item.SourceDigest); err != nil {
			t.Fatalf("sourceDigest must be hex: %q", item.SourceDigest)
		}
		digestOK = true
	}
	if !digestOK {
		t.Fatalf("classify/move item must expose sourceDigest: %+v", created.Items)
	}
	reject := validRequest("files.plan", `{"sessionId":"`+session+`","items":[{"action":"move","from":"a.pdf","to":"pdf/a.pdf","sourceDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
	reject.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), reject); resp.OK {
		t.Fatal("client-supplied sourceDigest must be rejected")
	}
	apply := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	apply.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), apply); !resp.OK {
		t.Fatalf("apply %#v", resp.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "pdf", "a.pdf")); err != nil {
		t.Fatal(err)
	}
	status := e.Handle(context.Background(), validRequest("files.status", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`))
	if !status.OK {
		t.Fatalf("status %#v", status.Error)
	}
	undo := validRequest("files.undo", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	undo.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), undo); !resp.OK {
		t.Fatalf("undo %#v", resp.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "a.pdf")); err != nil {
		t.Fatal("undo should restore a.pdf")
	}
}

func TestFilesPlanInvalidArgumentsAreChinese(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	if err := os.MkdirAll(filepath.Join(runtime.WorkspaceRoot(), session), 0700); err != nil {
		t.Fatal(err)
	}
	req := validRequest("files.plan", `{"sessionId":"`+session+`","items":[{"action":"move","from":"a.txt","unknown":true}]}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("unknown plan field must fail schema: %#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "文件计划参数无效") {
		t.Fatalf("plan schema error must be Chinese: %#v", resp.Error)
	}
	if strings.Contains(strings.ToLower(resp.Error.Message), "invalid files.plan") {
		t.Fatalf("must not leak English plan args: %#v", resp.Error)
	}
}

func TestFilesApplyInvalidArgumentsAreChinese(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	if err := os.MkdirAll(filepath.Join(runtime.WorkspaceRoot(), session), 0700); err != nil {
		t.Fatal(err)
	}
	req := validRequest("files.apply", `{"sessionId":"`+session+`"}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil {
		t.Fatalf("missing planId must fail: %#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "文件操作参数无效") {
		t.Fatalf("apply args must be Chinese: %#v", resp.Error)
	}
	if strings.Contains(strings.ToLower(resp.Error.Message), "invalid files.apply") {
		t.Fatalf("must not leak English apply args: %#v", resp.Error)
	}
}

func TestFilesApplyChangedSourceReturnsStatus(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	planReq := validRequest("files.plan", `{"sessionId":"`+session+`","items":[{"action":"move","from":"notes.txt","to":"txt/notes.txt"}]}`)
	planReq.IdempotencyKey = ulid.Make().String()
	plan := e.Handle(context.Background(), planReq)
	if !plan.OK {
		t.Fatalf("plan %#v", plan.Error)
	}
	created := mustDecodePayload[struct {
		ID string `json:"id"`
	}](t, plan.Payload)
	if err := os.WriteFile(src, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	apply := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	apply.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), apply)
	if !resp.OK {
		t.Fatalf("changed source must return status, not drop it: %#v", resp.Error)
	}
	st := mustDecodePayload[struct {
		State string `json:"state"`
		Items []struct {
			State string `json:"state"`
			Error string `json:"error"`
		} `json:"items"`
	}](t, resp.Payload)
	if st.State != "partial" || len(st.Items) == 0 || st.Items[0].State != "failed" {
		t.Fatalf("status %+v", st)
	}
	if !strings.Contains(st.Items[0].Error, "源文件在计划后已被修改，已停止执行，未覆盖原文件") {
		t.Fatalf("Bridge status item.error must be Chinese: %+v", st.Items[0])
	}
}

func TestFilesApplyMissingSourceReturnsChineseStatus(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	planReq := validRequest("files.plan", `{"sessionId":"`+session+`","items":[{"action":"write","to":"ok.md","body":"ok"},{"action":"move","from":"missing.txt","to":"txt/missing.txt"}]}`)
	planReq.IdempotencyKey = ulid.Make().String()
	plan := e.Handle(context.Background(), planReq)
	if !plan.OK {
		t.Fatalf("plan %#v", plan.Error)
	}
	created := mustDecodePayload[struct {
		ID string `json:"id"`
	}](t, plan.Payload)
	apply := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	apply.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), apply)
	if !resp.OK {
		t.Fatalf("partial apply must keep status: %#v", resp.Error)
	}
	st := mustDecodePayload[struct {
		State string `json:"state"`
		Items []struct {
			State string `json:"state"`
			Error string `json:"error"`
		} `json:"items"`
	}](t, resp.Payload)
	if st.State != "partial" {
		t.Fatalf("status %+v", st)
	}
	found := false
	for _, item := range st.Items {
		if item.State == "failed" && strings.Contains(item.Error, "源文件不存在，已停止执行") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Bridge missing-source item.error must be Chinese: %+v", st.Items)
	}
	if _, err := os.Stat(filepath.Join(root, "ok.md")); err != nil {
		t.Fatal("succeeded prefix must stay")
	}
}

func TestFilesApplyBridgeWritesToolOperationReceipt(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	store, err := sqlite.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "files-ops.db"))
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
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	planReq := validRequest("files.plan", `{"sessionId":"`+session+`","items":[{"action":"move","from":"notes.txt","to":"txt/notes.txt"}]}`)
	planReq.IdempotencyKey = ulid.Make().String()
	plan := e.Handle(context.Background(), planReq)
	if !plan.OK {
		t.Fatalf("plan %#v", plan.Error)
	}
	created := mustDecodePayload[struct {
		ID string `json:"id"`
	}](t, plan.Payload)
	apply := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	apply.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), apply); !resp.OK {
		t.Fatalf("apply %#v", resp.Error)
	}
	listed := e.Handle(context.Background(), validRequest("operation.list", `{"sessionId":"`+session+`"}`))
	if !listed.OK {
		t.Fatalf("list %#v", listed.Error)
	}
	ops := mustDecodePayload[struct {
		Items []struct {
			ToolName string `json:"toolName"`
			State    string `json:"state"`
		} `json:"items"`
	}](t, listed.Payload)
	found := false
	for _, op := range ops.Items {
		if op.ToolName == "files.apply" && op.State == "succeeded" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bridge apply must write a succeeded files.apply receipt: %+v", ops.Items)
	}
	if _, err := os.Stat(filepath.Join(root, "txt", "notes.txt")); err != nil {
		t.Fatal(err)
	}
	again := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	again.IdempotencyKey = ulid.Make().String()
	againResp := e.Handle(context.Background(), again)
	if !againResp.OK {
		t.Fatalf("second apply %#v", againResp.Error)
	}
	st := mustDecodePayload[struct {
		State string `json:"state"`
	}](t, againResp.Payload)
	if st.State != "applied" {
		t.Fatalf("already-applied plan must stay applied: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); err == nil {
		t.Fatal("second apply must not move the file again")
	}
	if _, err := os.Stat(filepath.Join(root, "txt", "notes.txt")); err != nil {
		t.Fatal("applied destination must stay")
	}
}

func TestFilesApplyBridgePartialWritesPartialReceipt(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	store, err := sqlite.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "files-partial.db"))
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
	planReq := validRequest("files.plan", `{"sessionId":"`+session+`","items":[{"action":"write","to":"ok.md","body":"ok"},{"action":"move","from":"missing.txt","to":"txt/missing.txt"}]}`)
	planReq.IdempotencyKey = ulid.Make().String()
	plan := e.Handle(context.Background(), planReq)
	if !plan.OK {
		t.Fatalf("plan %#v", plan.Error)
	}
	created := mustDecodePayload[struct {
		ID string `json:"id"`
	}](t, plan.Payload)
	apply := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	apply.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), apply)
	if !resp.OK {
		t.Fatalf("partial apply must keep status: %#v", resp.Error)
	}
	listed := e.Handle(context.Background(), validRequest("operation.list", `{"sessionId":"`+session+`"}`))
	if !listed.OK {
		t.Fatalf("list %#v", listed.Error)
	}
	ops := mustDecodePayload[struct {
		Items []struct {
			ToolName string `json:"toolName"`
			State    string `json:"state"`
		} `json:"items"`
	}](t, listed.Payload)
	found := false
	for _, op := range ops.Items {
		if op.ToolName == "files.apply" {
			if op.State != "partial" {
				t.Fatalf("partial apply receipt must be partial, not %q", op.State)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("partial apply must write a files.apply receipt: %+v", ops.Items)
	}
}

func TestFilesApplyUndonePlanFailsClosed(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	planReq := validRequest("files.plan", `{"sessionId":"`+session+`","items":[{"action":"move","from":"notes.txt","to":"txt/notes.txt"}]}`)
	planReq.IdempotencyKey = ulid.Make().String()
	plan := e.Handle(context.Background(), planReq)
	if !plan.OK {
		t.Fatalf("plan %#v", plan.Error)
	}
	created := mustDecodePayload[struct {
		ID string `json:"id"`
	}](t, plan.Payload)
	apply := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	apply.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), apply); !resp.OK {
		t.Fatalf("apply %#v", resp.Error)
	}
	undo := validRequest("files.undo", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	undo.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), undo); !resp.OK {
		t.Fatalf("undo %#v", resp.Error)
	}
	again := validRequest("files.apply", `{"sessionId":"`+session+`","planId":"`+created.ID+`"}`)
	again.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), again)
	if resp.OK || resp.Error == nil || resp.Error.Code != "INPUT_CHANGED" {
		t.Fatalf("re-apply undone must fail closed: %#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "已撤销的计划不能再次执行") {
		t.Fatalf("re-apply undone must be Chinese: %#v", resp.Error)
	}
}
