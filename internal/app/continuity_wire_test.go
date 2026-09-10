package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

type memToolOps struct {
	mu         sync.Mutex
	ops        map[string]modelfit.ToolOperation
	failPut    bool
	failCancel error
}

func (m *memToolOps) key(scope, id string) string { return scope + "/" + id }

func (m *memToolOps) PutToolOperationIntent(_ context.Context, op modelfit.ToolOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPut {
		return errors.New("intent store unavailable")
	}
	if m.ops == nil {
		m.ops = map[string]modelfit.ToolOperation{}
	}
	m.ops[m.key(op.OwnerScope, op.ID)] = op
	return nil
}

func (m *memToolOps) MarkToolOperationRunning(_ context.Context, owner, id string, version int, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[m.key(owner, id)]
	if !ok || op.ExpectedVersion != version {
		return errors.New("version conflict")
	}
	op.State = modelfit.OpRunning
	op.ExpectedVersion++
	op.UpdatedAt = at
	m.ops[m.key(owner, id)] = op
	return nil
}

func (m *memToolOps) BindToolOperationTrack(_ context.Context, owner, id string, version int, externalID, evidenceRef string, artifactRefs []string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[m.key(owner, id)]
	if !ok || op.ExpectedVersion != version || op.State == modelfit.OpCancelled {
		return errors.New("version conflict")
	}
	if externalID != "" {
		op.ExternalID = externalID
	}
	if evidenceRef != "" {
		op.EvidenceRef = evidenceRef
	}
	if artifactRefs != nil {
		op.ArtifactRefs = append([]string(nil), artifactRefs...)
	}
	op.ExpectedVersion++
	op.UpdatedAt = at
	m.ops[m.key(owner, id)] = op
	return nil
}

func (m *memToolOps) FinishToolOperation(_ context.Context, owner, id string, version int, state modelfit.OperationState, errorKind string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[m.key(owner, id)]
	if !ok || op.ExpectedVersion != version || op.State == modelfit.OpCancelled || op.CancellationRequestedAt != "" {
		return errors.New("version conflict")
	}
	op.State = state
	op.ErrorKind = errorKind
	op.ExpectedVersion++
	op.UpdatedAt = at
	m.ops[m.key(owner, id)] = op
	return nil
}

func (m *memToolOps) RequestToolOperationCancel(_ context.Context, owner, id string, version int, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCancel != nil {
		return m.failCancel
	}
	op, ok := m.ops[m.key(owner, id)]
	if !ok || op.ExpectedVersion != version {
		return errors.New("version conflict")
	}
	op.CancellationRequestedAt = at.Format(time.RFC3339Nano)
	if op.State == modelfit.OpPending || op.State == modelfit.OpRunning {
		op.State = modelfit.OpCancelled
	}
	op.ExpectedVersion++
	op.UpdatedAt = at
	m.ops[m.key(owner, id)] = op
	return nil
}

func (m *memToolOps) RecoverInterruptedToolOperations(context.Context) error { return nil }

func (m *memToolOps) GetToolOperation(_ context.Context, owner, id string) (modelfit.ToolOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[m.key(owner, id)]
	if !ok {
		return modelfit.ToolOperation{}, errors.New("not found")
	}
	return op, nil
}

func (m *memToolOps) ListToolOperations(_ context.Context, owner string, limit int) ([]modelfit.ToolOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []modelfit.ToolOperation
	for _, op := range m.ops {
		if op.OwnerScope == owner {
			out = append(out, op)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memToolOps) last() modelfit.ToolOperation {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, op := range m.ops {
		return op
	}
	return modelfit.ToolOperation{}
}

func TestExecuteUserToolRecordsSucceededReceipt(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	e.toolExecHook = func(context.Context, executionMode, string, string, json.RawMessage) (toolruntime.Result, error) {
		return toolruntime.Result{Output: "ok"}, nil
	}
	session := ulid.Make().String()
	got, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "workspace.read", json.RawMessage(`{"path":"a.txt"}`))
	if err != nil || got.Output != "ok" {
		t.Fatalf("tool = %+v %v", got, err)
	}
	op := store.last()
	if op.State != modelfit.OpSucceeded || op.ToolName != "workspace.read" || op.EffectClass != modelfit.EffectReadOnly {
		t.Fatalf("receipt = %+v", op)
	}
	if len(op.InputDigest) != 64 {
		t.Fatalf("digest = %q", op.InputDigest)
	}
}

func TestExecuteUserToolDoesNotRunWhenIntentFails(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memToolOps{failPut: true}
	e.SetToolOperationStore(store)
	ran := false
	e.toolExecHook = func(context.Context, executionMode, string, string, json.RawMessage) (toolruntime.Result, error) {
		ran = true
		return toolruntime.Result{Output: "ran"}, nil
	}
	if _, err := e.executeUserTool(context.Background(), executionModeFullAccess, ulid.Make().String(), "workspace.write", json.RawMessage(`{"path":"a.txt","content":"x"}`)); err == nil {
		t.Fatal("expected intent failure")
	}
	if ran {
		t.Fatal("unrecorded tool must not execute")
	}
}

type memCalls struct {
	mu    sync.Mutex
	recs  []sqlite.CallAttemptRecord
	fin   []sqlite.CallAttemptReceipt
	byKey map[string]sqlite.CallAttemptRecord
}

func (m *memCalls) MarkCallAttemptSent(_ context.Context, owner, callID, attemptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := owner + "/" + callID + "/" + attemptID
	cur := m.byKey[key]
	if cur.Status == string(modelfit.CallIntent) {
		cur.Status = string(modelfit.CallSent)
		m.byKey[key] = cur
	}
	return nil
}

func (m *memCalls) PutCallAttemptIntent(_ context.Context, rec sqlite.CallAttemptRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recs = append(m.recs, rec)
	if m.byKey == nil {
		m.byKey = map[string]sqlite.CallAttemptRecord{}
	}
	m.byKey[rec.OwnerScope+"/"+rec.CallID+"/"+rec.AttemptID] = rec
	return nil
}

func (m *memCalls) FinishCallAttempt(_ context.Context, owner, callID, attemptID string, rec sqlite.CallAttemptReceipt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fin = append(m.fin, rec)
	key := owner + "/" + callID + "/" + attemptID
	cur := m.byKey[key]
	switch cur.Status {
	case string(modelfit.CallIntent), string(modelfit.CallSent), string(modelfit.CallUnknown), "":
		cur.Status = rec.Status
		cur.Integrity = rec.Integrity
		cur.InputTokens = rec.InputTokens
		cur.OutputTokens = rec.OutputTokens
		cur.CachedInputTokens = rec.CachedInputTokens
		cur.CacheWriteTokens = rec.CacheWriteTokens
		cur.ProviderRequestID = rec.ProviderRequestID
		cur.CostStatus = rec.CostStatus
		cur.EndedAt = rec.EndedAt
		m.byKey[key] = cur
	}
	return nil
}

func (m *memCalls) ListCallAttempts(_ context.Context, owner, taskID string, limit int) ([]sqlite.CallAttemptRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []sqlite.CallAttemptRecord
	for _, rec := range m.byKey {
		if rec.OwnerScope == owner && (taskID == "" || rec.TaskID == taskID) {
			out = append(out, rec)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memCalls) SumCallAttemptsByOwner(ctx context.Context, owner, taskID string) (sqlite.CallAttemptSum, error) {
	recs, err := m.ListCallAttempts(ctx, owner, taskID, 0)
	if err != nil {
		return sqlite.CallAttemptSum{}, err
	}
	sum := sqlite.CallAttemptSum{Calls: len(recs), Integrity: string(modelfit.UsageUnknown)}
	if len(recs) == 0 {
		return sum, nil
	}
	sum.Integrity = string(modelfit.UsageReported)
	for _, rec := range recs {
		sum.InputTokens += rec.InputTokens
		sum.OutputTokens += rec.OutputTokens
		if rec.Integrity == string(modelfit.UsageUnknown) {
			sum.Integrity = string(modelfit.UsageUnknown)
		} else if rec.Integrity == string(modelfit.UsagePartial) && sum.Integrity != string(modelfit.UsageUnknown) {
			sum.Integrity = string(modelfit.UsagePartial)
		}
	}
	return sum, nil
}

type meterAdapter struct {
	usage llmadapter.Usage
}

func (meterAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (meterAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a meterAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if emit != nil && a.usage.InputTokens+a.usage.OutputTokens > 0 {
		first := a.usage
		first.OutputTokens = 1
		_ = emit(llmadapter.Delta{Text: "hi", Usage: &first})
		_ = emit(llmadapter.Delta{Usage: &a.usage})
	} else if emit != nil {
		_ = emit(llmadapter.Delta{Text: "hi"})
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "hi"}, Usage: a.usage}, nil
}

type embedMeterAdapter struct{ meterAdapter }

func (embedMeterAdapter) Embed(_ context.Context, _ []byte, _ string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1}
	}
	return out, nil
}

func TestMeteredAdapterKeepsEmbedSurfaceAndRecordsCredential(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return embedMeterAdapter{}, nil
	})
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible, CredentialRef: "cred-gen-1"}
	a, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapterAs[llmadapter.Embedder](a); !ok {
		t.Fatal("meter must not hide the inner embed surface")
	}
	if _, ok := adapterAs[llmadapter.ImageGenerator](a); ok {
		t.Fatal("chat/embed adapter must not look like an image generator")
	}
	ctx := withCallPurpose(context.Background(), "embed")
	if _, err := embedThrough(ctx, a, nil, "emb-1", []string{"ping"}); err != nil {
		t.Fatal(err)
	}
	if len(store.recs) != 1 || store.recs[0].Purpose != "embed" || store.recs[0].CredentialGeneration != "cred-gen-1" || store.recs[0].Model != "emb-1" {
		t.Fatalf("embed attempt %+v", store.recs)
	}
}

func TestCallMeterRecordsEfficiencySnapshotFromRequest(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return meterAdapter{}, nil
	})
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible}
	a, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: "chat"})
	if _, err = a.Stream(ctx, nil, llmadapter.Request{
		Model:      "m",
		Efficiency: llmadapter.EfficiencySnapshot{PolicyVersion: "token-efficiency-v1", BytesBefore: 80, BytesAfter: 50},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.recs) != 1 || store.recs[0].PolicyVersion != "token-efficiency-v1" || store.recs[0].BytesBefore != 80 || store.recs[0].BytesAfter != 50 {
		t.Fatalf("meter must persist snapshot: %+v", store.recs)
	}
	store2 := &memCalls{}
	e.SetCallAttemptStore(store2)
	a, err = e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Stream(ctx, nil, llmadapter.Request{Model: "m"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(store2.recs) != 1 || store2.recs[0].PolicyVersion != "" || store2.recs[0].BytesBefore != 0 {
		t.Fatalf("empty snapshot must stay unknown: %+v", store2.recs)
	}
}

func TestCallMeterFillsEfficiencySnapshotFromPreparedRequest(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return meterAdapter{}, nil
	})
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible}
	a, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: "chat"})
	if _, err = a.Stream(ctx, nil, llmadapter.Request{
		Model:    "m",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "hello world"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.recs) != 1 || store.recs[0].PolicyVersion != "token-efficiency-v1" || store.recs[0].BytesBefore < len("hello world") {
		t.Fatalf("prepared request must record observation snapshot: %+v", store.recs)
	}
}

func TestCallMeterKeepsEmptyUsageUnknownAndDoesNotSumSnapshots(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return meterAdapter{}, nil
	})
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible}
	a, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: "chat"})
	if _, err = a.Stream(ctx, nil, llmadapter.Request{Model: "m"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.fin) != 1 || store.fin[0].Integrity != string(modelfit.UsageUnknown) || store.fin[0].InputTokens != 0 {
		t.Fatalf("empty usage must stay unknown: %+v", store.fin)
	}

	store2 := &memCalls{}
	e.SetCallAttemptStore(store2)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return meterAdapter{usage: llmadapter.Usage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14}}, nil
	})
	a, err = e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Stream(ctx, nil, llmadapter.Request{Model: "m"}, func(llmadapter.Delta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(store2.fin) != 1 || store2.fin[0].InputTokens != 10 || store2.fin[0].OutputTokens != 4 {
		t.Fatalf("snapshots must replace not sum: %+v", store2.fin)
	}
	if store2.fin[0].Integrity != string(modelfit.UsageReported) {
		t.Fatalf("reported integrity = %q", store2.fin[0].Integrity)
	}
}

func TestCallMeterMarksSentBeforeSupplierCall(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return sentProbeAdapter{store: store}, nil
	})
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible}
	a, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic", Purpose: "chat"})
	if _, err = a.Complete(ctx, nil, llmadapter.Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
}

type sentProbeAdapter struct{ store *memCalls }

func (sentProbeAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (sentProbeAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a sentProbeAdapter) Complete(ctx context.Context, _ []byte, _ llmadapter.Request) (llmadapter.Response, error) {
	recs, err := a.store.ListCallAttempts(ctx, "diagnostic", "", 0)
	if err != nil || len(recs) != 1 || recs[0].Status != string(modelfit.CallSent) {
		return llmadapter.Response{}, fmt.Errorf("want sent before supplier call, got %+v %v", recs, err)
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "ok"}}, nil
}

type completeMeterAdapter struct {
	usage   llmadapter.Usage
	content string
}

func (a completeMeterAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	content := a.content
	if content == "" {
		content = "ok"
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: content}, Usage: a.usage}, nil
}
func (completeMeterAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (completeMeterAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}

func TestMeteredAdapterDefaultsUnscopedPurposeToUnknown(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return completeMeterAdapter{usage: llmadapter.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}, nil
	})
	a, err := e.adapter(context.Background(), provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible})
	if err != nil {
		t.Fatal(err)
	}
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: "diagnostic"})
	if _, err = a.Complete(ctx, nil, llmadapter.Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if len(store.recs) != 1 || store.recs[0].Purpose != "unknown" {
		t.Fatalf("unscoped purpose must be unknown, got %+v", store.recs)
	}
	if store.recs[0].Purpose == "llm" {
		t.Fatal("unscoped calls must not look like generic chat spend")
	}
}

func TestAuxiliaryLLMCallsRecordDistinctPurposes(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	inner := completeMeterAdapter{usage: llmadapter.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return inner, nil
	})
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible}
	parent := withContinuityScope(context.Background(), continuityScope{Owner: "sess", Task: "sess", Turn: "turn", Purpose: "chat"})
	a, err := e.adapter(parent, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.completeJudge(parent, a, nil, "m", llmadapter.Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.executeSubagentLoop(parent, a, nil, "m", "sess", "survey", 1000, subagentProfileDef{MaxSteps: 1}, executionModeFullAccess); err != nil {
		t.Fatal(err)
	}
	factory := &compactionAdapterFactory{e: e}
	ca, err := factory.Adapter(parent, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ca.Complete(parent, nil, llmadapter.Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, rec := range store.recs {
		seen[rec.Purpose] = true
		if rec.OwnerScope != "sess" || rec.TaskID != "sess" {
			t.Fatalf("auxiliary call lost parent task: %+v", rec)
		}
	}
	for _, want := range []string{"judge", "subagent", "compaction"} {
		if !seen[want] {
			t.Fatalf("missing purpose %q in %+v", want, seen)
		}
	}
}
