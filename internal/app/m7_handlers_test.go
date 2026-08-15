package app

import (
	"context"
	"fmt"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/projectapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type m7Harness struct {
	e   *Engine
	svc *m7app.WorkflowService
	pid string
}

func newM7Harness(t *testing.T) *m7Harness {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "m7wf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	p, err := projectapp.New(store, store).Create(ctx, "m7-key", "test",
		map[string]string{"name": "m7-project"}, project.Project{Name: "m7-project"})
	if err != nil {
		t.Fatal(err)
	}
	svc := m7app.NewWorkflowService(store.AgentRuntimeRepository())
	e := NewEngine(nil, "test")
	e.SetM7WorkflowServices(svc)
	return &m7Harness{e: e, svc: svc, pid: p.ID}
}

func m7Request(method bridge.Method, payload, key string) bridge.Request {
	r := validRequest(string(method), payload)
	r.IdempotencyKey = key
	return r
}

func m7Decode(t *testing.T, r bridge.Response, target any) {
	t.Helper()
	if !r.OK {
		t.Fatalf("request failed: code=%s msg=%s", r.Error.Code, r.Error.Message)
	}
	body, _ := json.Marshal(r.Payload)
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatal(err)
	}
}

func (h *m7Harness) createVersion(t *testing.T) (id string) {
	t.Helper()
	resp := h.e.Handle(context.Background(), m7Request(bridge.MethodWorkflowCreateVersion,
		`{"projectId":"`+h.pid+`","requestId":"req-1"}`, "idem-c-"+t.Name()))
	var out struct {
		WorkflowVersionID string `json:"workflowVersionId"`
	}
	m7Decode(t, resp, &out)
	return out.WorkflowVersionID
}

func (h *m7Harness) publish(t *testing.T, versionID string) {
	t.Helper()
	resp := h.e.Handle(context.Background(), m7Request(bridge.MethodWorkflowPublish,
		`{"workflowVersionId":"`+versionID+`","requestId":"req-2"}`, "idem-p-"+t.Name()))
	if !resp.OK {
		t.Fatalf("publish failed: %+v", resp.Error)
	}
}

func TestWorkflowCreatePublishRoundTrip(t *testing.T) {
	h := newM7Harness(t)
	ctx := context.Background()
	resp := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCreateVersion,
		`{"projectId":"`+h.pid+`","requestId":"req-1"}`, "idem-1"))
	var created struct {
		WorkflowVersionID string `json:"workflowVersionId"`
		Version           int64  `json:"version"`
		Status            string `json:"status"`
		DefinitionDigest  string `json:"definitionDigest"`
	}
	m7Decode(t, resp, &created)
	if created.Version != 1 || created.Status != "draft" || len(created.DefinitionDigest) != 64 {
		t.Fatalf("unexpected create result: %+v", created)
	}
	pub := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowPublish,
		`{"workflowVersionId":"`+created.WorkflowVersionID+`","requestId":"req-2"}`, "idem-2"))
	var published struct {
		Status      string `json:"status"`
		PublishedAt string `json:"publishedAt"`
	}
	m7Decode(t, pub, &published)
	if published.Status != "published" || published.PublishedAt == "" {
		t.Fatalf("unexpected publish result: %+v", published)
	}
	if _, err := time.Parse(time.RFC3339Nano, published.PublishedAt); err != nil {
		t.Fatalf("publishedAt not RFC3339: %q", published.PublishedAt)
	}
}

func TestWorkflowHandlerSchemaGuards(t *testing.T) {
	h := newM7Harness(t)
	ctx := context.Background()
	cases := []struct {
		method  bridge.Method
		payload string
	}{
		{bridge.MethodWorkflowCreateVersion, `{"projectId":"not-a-ulid","requestId":"r"}`},
		{bridge.MethodWorkflowCreateVersion, `{"projectId":"` + h.pid + `","requestId":""}`},
		{bridge.MethodWorkflowPublish, `{"workflowVersionId":"bad","requestId":"r"}`},
		{bridge.MethodWorkflowStartStage, `{"projectId":"` + h.pid + `","stageKey":"security"}`},
		{bridge.MethodWorkflowTransitionStage, `{"stageRunId":"01ARZ3NDEKTSV4RRFFQ69G5FA1","to":"draft","expectedVersion":1}`},
		{bridge.MethodWorkflowCaptureInput, `{"stageRunId":"01ARZ3NDEKTSV4RRFFQ69G5FA1","inputs":{"a":"b"},"requestId":""}`},
	}
	for _, tc := range cases {
		resp := h.e.Handle(ctx, m7Request(tc.method, tc.payload, ""))
		if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("%s: want BRIDGE_SCHEMA_INVALID, got %+v", tc.method, resp.Error)
		}
	}
}

func TestWorkflowStartTransitionCaptureFlow(t *testing.T) {
	h := newM7Harness(t)
	ctx := context.Background()
	h.publish(t, h.createVersion(t))
	start := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowStartStage,
		`{"projectId":"`+h.pid+`","stageKey":"INITIATION_BOUNDARY"}`, ""))
	var run struct {
		InstanceID      string `json:"instanceId"`
		StageRunID      string `json:"stageRunId"`
		State           string `json:"state"`
		AttemptNo       int64  `json:"attemptNo"`
		Created         bool   `json:"created"`
		DependenciesMet bool   `json:"dependenciesMet"`
	}
	m7Decode(t, start, &run)
	if !run.Created || run.State != "ready" || run.AttemptNo != 1 || !run.DependenciesMet {
		t.Fatalf("unexpected start result: %+v", run)
	}
	// Optimistic-lock conflict surfaces as M7-WF-004.
	stale := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowTransitionStage,
		`{"stageRunId":"`+run.StageRunID+`","to":"running","expectedVersion":42}`, "idem-t1"))
	if stale.OK || stale.Error.Code != "M7-WF-004" {
		t.Fatalf("stale lock want M7-WF-004, got %+v", stale.Error)
	}
	go1 := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowTransitionStage,
		`{"stageRunId":"`+run.StageRunID+`","to":"running","expectedVersion":1}`, "idem-t2"))
	if !go1.OK {
		t.Fatalf("running transition failed: %+v", go1.Error)
	}
	capture := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCaptureInput,
		`{"stageRunId":"`+run.StageRunID+`","inputs":{"brief":"v1"},"requestId":"req-c"}`, "idem-c1"))
	var snap struct {
		SnapshotDigest string `json:"snapshotDigest"`
		CapturedAt     string `json:"capturedAt"`
	}
	m7Decode(t, capture, &snap)
	if len(snap.SnapshotDigest) != 64 || snap.CapturedAt == "" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	// Changed input on a running stage maps to M7-SNP-002.
	changed := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCaptureInput,
		`{"stageRunId":"`+run.StageRunID+`","inputs":{"brief":"v2"},"requestId":"req-c2"}`, "idem-c2"))
	if changed.OK || changed.Error.Code != "M7-SNP-002" {
		t.Fatalf("stale snapshot want M7-SNP-002, got %+v", changed.Error)
	}
}

func TestWorkflowNotPublishedMapsToUnpublished(t *testing.T) {
	h := newM7Harness(t)
	ctx := context.Background()
	resp := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowStartStage,
		`{"projectId":"`+h.pid+`","stageKey":"INITIATION_BOUNDARY"}`, ""))
	if resp.OK || resp.Error.Code != "WORKFLOW_VERSION_UNPUBLISHED" {
		t.Fatalf("unpublished start want WORKFLOW_VERSION_UNPUBLISHED, got %+v", resp.Error)
	}
}

// TestWorkflowErrorMappingTable covers the service-level error mapping the
// wire schema cannot reach (stageKey is enum-validated before the engine),
// per the doc contract: fixed-set M7-WF-002, cycle M7-WF-003.
func TestWorkflowErrorMappingTable(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{fmt.Errorf("wrap: %w", m7flow.ErrStageFixedSet), "M7-WF-002"},
		{fmt.Errorf("wrap: %w", m7flow.ErrStageCycle), "M7-WF-003"},
		{m7app.ErrNotPublished, "WORKFLOW_VERSION_UNPUBLISHED"},
	}
	for _, tc := range cases {
		resp := m7WorkflowFailure(bridge.Request{ID: "r1", TraceID: "t1"}, tc.err, "workflow.test")
		if resp.OK || resp.Error.Code != tc.code {
			t.Fatalf("%v: want %s, got %+v", tc.err, tc.code, resp.Error)
		}
	}
}

func TestWorkflowM6TreeDigestMapsToSNP001(t *testing.T) {
	h := newM7Harness(t)
	ctx := context.Background()
	h.publish(t, h.createVersion(t))
	start := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowStartStage,
		`{"projectId":"`+h.pid+`","stageKey":"INITIATION_BOUNDARY"}`, ""))
	var run struct {
		StageRunID string `json:"stageRunId"`
	}
	m7Decode(t, start, &run)
	bad := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCaptureInput,
		`{"stageRunId":"`+run.StageRunID+`","inputs":{"m6.finalTree":{"rootId":"01ARZ3NDEKTSV4RRFFQ69G5FA9"}},"requestId":"req-m6"}`, "idem-m6"))
	if bad.OK || bad.Error.Code != "M7-SNP-001" {
		t.Fatalf("m6 tree without digest want M7-SNP-001, got %+v", bad.Error)
	}
	// The M6-native merge.finalize payload (finalDigest + testsRef) must be
	// accepted unmodified at the API layer (M6->M7 seam).
	good := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCaptureInput,
		`{"stageRunId":"`+run.StageRunID+`","inputs":{"m6.finalTree":{"rootId":"01ARZ3NDEKTSV4RRFFQ69G5FA9","finalDigest":"`+
			strings.Repeat("ab", 32)+`","testsRef":"run-0042"}},"requestId":"req-m6ok"}`, "idem-m6ok"))
	if !good.OK {
		t.Fatalf("M6-native finalDigest payload must be accepted, got %+v", good.Error)
	}
}
