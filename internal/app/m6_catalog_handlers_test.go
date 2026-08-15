package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m6app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// m6ExecHarness wires the slice-2 services (connector metadata catalog +
// worker dispatch) behind the bridge handlers on a real store.
type m6ExecHarness struct {
	e *Engine
}

func newM6ExecHarness(t *testing.T) *m6ExecHarness {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "m6exec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	fetch := func(ctx context.Context, connectorID, metadataScope string) (map[string]any, error) {
		return map[string]any{
			"connector": connectorID,
			"scope":     metadataScope,
			"objects":   []string{"orders", "users"},
		}, nil
	}
	verifier := func(jobSpecDigest, budgetLeaseID, token string) bool {
		return token == "captok:valid"
	}
	dispatch := m6app.NewDispatchService(repo, verifier, 30*time.Second)
	dispatch.SetSandboxRoot(func(string) string { return filepath.Join(t.TempDir(), "worktree") })
	e := NewEngine(nil, "test")
	e.SetM6ExecutionServices(m6app.NewCatalogService(repo, fetch), dispatch)
	return &m6ExecHarness{e: e}
}

// DB-001 double allowlist is enforced before the driver; snapshots persist
// with a per-connector monotonic version (0045 UNIQUE).
func TestConnectorSnapshotMonotonicVersions(t *testing.T) {
	h := newM6ExecHarness(t)
	var first struct {
		SnapshotVersion int64          `json:"snapshotVersion"`
		Objects         map[string]any `json:"objects"`
		FetchedAt       string         `json:"fetchedAt"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodConnectorSnapshot,
		`{"connectorId":"pg-main","metadataScope":"table"}`, "")), &first)
	if first.SnapshotVersion != 1 {
		t.Fatalf("first snapshot must be version 1, got %d", first.SnapshotVersion)
	}
	if len(first.Objects) == 0 || first.FetchedAt == "" {
		t.Fatalf("snapshot payload malformed: %+v", first)
	}
	if _, err := time.Parse(time.RFC3339Nano, first.FetchedAt); err != nil {
		t.Fatalf("fetchedAt not RFC3339: %q", first.FetchedAt)
	}
	var second struct {
		SnapshotVersion int64 `json:"snapshotVersion"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodConnectorSnapshot,
		`{"connectorId":"pg-main","metadataScope":"schema"}`, "")), &second)
	if second.SnapshotVersion != 2 {
		t.Fatalf("second snapshot must be version 2, got %d", second.SnapshotVersion)
	}
	// A different connector starts its own version sequence.
	var other struct {
		SnapshotVersion int64 `json:"snapshotVersion"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodConnectorSnapshot,
		`{"connectorId":"pg-replica","metadataScope":"table"}`, "")), &other)
	if other.SnapshotVersion != 1 {
		t.Fatalf("other connector must restart at version 1, got %d", other.SnapshotVersion)
	}
}

// DB-002: an out-of-enum metadataScope answers the exact catalog code; a
// malformed connectorId answers the generic schema-invalid code.
func TestConnectorSnapshotScopeDenied(t *testing.T) {
	h := newM6ExecHarness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodConnectorSnapshot,
		`{"connectorId":"pg-main","metadataScope":"rows"}`, ""))
	if resp.OK || resp.Error.Code != "M6-DB-002" {
		t.Fatalf("want M6-DB-002, got %+v", resp.Error)
	}
	badID := h.e.Handle(context.Background(), m6Request(bridge.MethodConnectorSnapshot,
		`{"connectorId":"pg_main","metadataScope":"table"}`, ""))
	if badID.OK || badID.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want BRIDGE_SCHEMA_INVALID for bad connectorId, got %+v", badID.Error)
	}
}

// Upstream fetch failure maps to the integration-unavailable code (matrix
// dependency column: CRD-001/HLT-001).
func TestConnectorSnapshotUpstreamFailure(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "m6fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	fetch := func(ctx context.Context, connectorID, metadataScope string) (map[string]any, error) {
		return nil, errors.New("upstream gone")
	}
	e := NewEngine(nil, "test")
	e.SetM6ExecutionServices(m6app.NewCatalogService(store.AgentRuntimeRepository(), fetch), nil)
	resp := e.Handle(context.Background(), m6Request(bridge.MethodConnectorSnapshot,
		`{"connectorId":"pg-main","metadataScope":"table"}`, ""))
	if resp.OK || resp.Error.Code != "M6-HLT-001" {
		t.Fatalf("want M6-HLT-001, got %+v", resp.Error)
	}
}

// TSK-001 same-key-same-digest semantics: replaying a dispatch returns the
// original worker/task pair instead of creating a second one.
func TestWorkerDispatchIdempotentReplay(t *testing.T) {
	h := newM6ExecHarness(t)
	payload := `{"jobSpecDigest":"` + sha256Hex("job-a") + `","capabilityToken":"captok:valid","budgetLeaseId":"lease-1"}`
	var first struct {
		WorkerID    string `json:"workerId"`
		TaskID      string `json:"taskId"`
		WorktreeRef string `json:"worktreeRef"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodWorkerDispatch, payload, "")), &first)
	if first.WorkerID == "" || first.TaskID == "" || first.WorktreeRef == "" {
		t.Fatalf("dispatch payload malformed: %+v", first)
	}
	var replay struct {
		WorkerID string `json:"workerId"`
		TaskID   string `json:"taskId"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodWorkerDispatch, payload, "")), &replay)
	if replay.TaskID != first.TaskID || replay.WorkerID != first.WorkerID {
		t.Fatalf("replay must return original task %s/%s, got %s/%s", first.TaskID, first.WorkerID, replay.TaskID, replay.WorkerID)
	}
	// A different budget lease is a different dispatch (new task row).
	var other struct {
		TaskID string `json:"taskId"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodWorkerDispatch,
		`{"jobSpecDigest":"`+sha256Hex("job-a")+`","capabilityToken":"captok:valid","budgetLeaseId":"lease-2"}`, "")), &other)
	if other.TaskID == first.TaskID {
		t.Fatalf("different budget lease must create a new task, got same %s", other.TaskID)
	}
}

// DLG-001: a capability token that does not verify rejects the dispatch
// before any worker or task row is created.
func TestWorkerDispatchCapabilityRejected(t *testing.T) {
	h := newM6ExecHarness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodWorkerDispatch,
		`{"jobSpecDigest":"`+sha256Hex("job-b")+`","capabilityToken":"captok:wrong","budgetLeaseId":"lease-1"}`, ""))
	if resp.OK || resp.Error.Code != "M6-DLG-001" {
		t.Fatalf("want M6-DLG-001, got %+v", resp.Error)
	}
}

func TestWorkerDispatchInvalidPayload(t *testing.T) {
	h := newM6ExecHarness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodWorkerDispatch,
		`{"jobSpecDigest":"not-hex","capabilityToken":"captok:valid","budgetLeaseId":"lease-1"}`, ""))
	if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want BRIDGE_SCHEMA_INVALID, got %+v", resp.Error)
	}
}

func TestM6ExecutionServicesUnwired(t *testing.T) {
	e := NewEngine(nil, "test")
	resp := e.Handle(context.Background(), m6Request(bridge.MethodConnectorSnapshot,
		`{"connectorId":"pg-main","metadataScope":"table"}`, ""))
	if resp.OK || resp.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("connector.snapshot unwired: %+v", resp.Error)
	}
	resp = e.Handle(context.Background(), m6Request(bridge.MethodWorkerDispatch,
		`{"jobSpecDigest":"`+sha256Hex("job-x")+`","capabilityToken":"captok:valid","budgetLeaseId":"lease-1"}`, ""))
	if resp.OK || resp.Error.Code != "FEATURE_DISABLED" {
		t.Fatalf("worker.dispatch unwired: %+v", resp.Error)
	}
}
