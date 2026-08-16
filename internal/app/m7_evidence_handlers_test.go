package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/projectapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// newM7EvidenceHarness wires the workflow + evidence services onto one
// engine so the slice-2 handlers can be exercised end-to-end with a real
// stage run behind them.
type m7EvidenceHarness struct {
	e   *Engine
	pid string
}

func newM7EvidenceHarness(t *testing.T) *m7EvidenceHarness {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "m7evd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	p, err := projectapp.New(store, store).Create(ctx, "m7-evd-key", "test",
		map[string]string{"name": "m7-evd"}, project.Project{Name: "m7-evd"})
	if err != nil {
		t.Fatal(err)
	}
	repo := store.AgentRuntimeRepository()
	traceSvc := m7app.NewTraceService(repo)
	e := NewEngine(nil, "test")
	e.SetM7WorkflowServices(m7app.NewWorkflowService(repo))
	e.SetM7EvidenceServices(traceSvc, m7app.NewGateService(repo), m7app.NewReviewService(repo, traceSvc))
	return &m7EvidenceHarness{e: e, pid: p.ID}
}

// runningStageRun drives createVersion -> publish -> start -> running ->
// capture and answers the stage run id.
func (h *m7EvidenceHarness) runningStageRun(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	resp := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCreateVersion,
		`{"projectId":"`+h.pid+`","requestId":"rv-`+t.Name()+`"}`, "idem-evd-cv-"+t.Name()))
	var v struct {
		WorkflowVersionID string `json:"workflowVersionId"`
	}
	m7Decode(t, resp, &v)
	if pub := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowPublish,
		`{"workflowVersionId":"`+v.WorkflowVersionID+`","requestId":"rp"}`, "idem-evd-pub-"+t.Name())); !pub.OK {
		t.Fatalf("publish failed: %+v", pub.Error)
	}
	start := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowStartStage,
		`{"projectId":"`+h.pid+`","stageKey":"INITIATION_BOUNDARY"}`, ""))
	var run struct {
		StageRunID string `json:"stageRunId"`
	}
	m7Decode(t, start, &run)
	if go1 := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowTransitionStage,
		`{"stageRunId":"`+run.StageRunID+`","to":"running","expectedVersion":1}`, "idem-evd-run-"+t.Name())); !go1.OK {
		t.Fatalf("running transition failed: %+v", go1.Error)
	}
	capture := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCaptureInput,
		`{"stageRunId":"`+run.StageRunID+`","inputs":{"brief":"evd-v1"},"requestId":"rc"}`, "idem-evd-cap-"+t.Name()))
	if !capture.OK {
		t.Fatalf("capture failed: %+v", capture.Error)
	}
	return run.StageRunID
}

const evdDigest = "0101010101010101010101010101010101010101010101010101010101010101"

func TestTraceAddEdgeQueryRoundTrip(t *testing.T) {
	h := newM7EvidenceHarness(t)
	ctx := context.Background()
	runID := h.runningStageRun(t)

	added := h.e.Handle(ctx, m7Request(bridge.MethodTraceAddEdge,
		`{"fromType":"stage_run","fromId":"`+runID+`","fromDigest":"`+evdDigest+`",`+
			`"relation":"derived_from","toType":"project","toId":"`+h.pid+`","toDigest":"`+evdDigest+`"}`, ""))
	var ae struct {
		EdgeID string `json:"edgeId"`
	}
	m7Decode(t, added, &ae)
	if len(ae.EdgeID) != 26 {
		t.Fatalf("unexpected edge: %+v", ae)
	}

	// identical replay answers the stored edge
	replay := h.e.Handle(ctx, m7Request(bridge.MethodTraceAddEdge,
		`{"fromType":"stage_run","fromId":"`+runID+`","fromDigest":"`+evdDigest+`",`+
			`"relation":"derived_from","toType":"project","toId":"`+h.pid+`","toDigest":"`+evdDigest+`"}`, ""))
	var re struct {
		EdgeID string `json:"edgeId"`
	}
	m7Decode(t, replay, &re)
	if re.EdgeID != ae.EdgeID {
		t.Fatalf("replay edge drift: %s vs %s", re.EdgeID, ae.EdgeID)
	}

	// same endpoints, conflicting digest (M7-TRC-003)
	conflict := h.e.Handle(ctx, m7Request(bridge.MethodTraceAddEdge,
		`{"fromType":"stage_run","fromId":"`+runID+`","fromDigest":"`+evdDigest+`",`+
			`"relation":"derived_from","toType":"project","toId":"`+h.pid+`","toDigest":"`+
			strings.Replace(evdDigest, "01", "02", 1)+`"}`, ""))
	if code := m7ErrCode(t, conflict); code != "M7-TRC-003" {
		t.Fatalf("conflict want M7-TRC-003, got %s", code)
	}

	// dangling endpoint refused (M7-TRC-001)
	dangling := h.e.Handle(ctx, m7Request(bridge.MethodTraceAddEdge,
		`{"fromType":"stage_run","fromId":"`+runID+`","fromDigest":"`+evdDigest+`",`+
			`"relation":"traces_to","toType":"project","toId":"`+ulid.Make().String()+`","toDigest":"`+evdDigest+`"}`, ""))
	if code := m7ErrCode(t, dangling); code != "M7-TRC-001" {
		t.Fatalf("dangling want M7-TRC-001, got %s", code)
	}

	// query down from the run reaches the edge into the project
	q := h.e.Handle(ctx, m7Request(bridge.MethodTraceQuery,
		`{"rootType":"stage_run","rootId":"`+runID+`","direction":"down","depth":3}`, ""))
	var qr struct {
		Edges []struct {
			EdgeID string `json:"edgeId"`
			ToID   string `json:"toId"`
		} `json:"edges"`
		NextCursor string `json:"nextCursor"`
	}
	m7Decode(t, q, &qr)
	found := false
	for _, ed := range qr.Edges {
		if ed.EdgeID == ae.EdgeID && ed.ToID == h.pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("query did not reach the new edge: %+v", qr)
	}
}

func TestTraceStaleBlocksGateUntilResolved(t *testing.T) {
	h := new7EvidenceHarnessFixed(t)
	ctx := context.Background()
	runID := h.runningStageRun(t)

	marked := h.e.Handle(ctx, m7Request(bridge.MethodTraceMarkStale,
		`{"subjectType":"stage_run","subjectId":"`+runID+`","causeEdge":"edge-cause-1"}`, ""))
	var mm struct {
		StaleMarkID string `json:"staleMarkId"`
	}
	m7Decode(t, marked, &mm)
	if len(mm.StaleMarkID) != 26 {
		t.Fatalf("unexpected stale mark: %+v", mm)
	}

	// outstanding stale blocks every gate first (TRC-002)
	blocked := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowEvaluateGate,
		`{"stageRunId":"`+runID+`","gateKey":"stage.exit"}`, ""))
	var br struct {
		Decision string `json:"decision"`
		Findings []struct {
			Code string `json:"code"`
		} `json:"findings"`
	}
	m7Decode(t, blocked, &br)
	if br.Decision != m7flow.GateBlocked || len(br.Findings) == 0 || br.Findings[0].Code != "M7-TRC-002" {
		t.Fatalf("expected BLOCKED with M7-TRC-002, got %+v", br)
	}

	// checkpoint creation is denied while the gate never passed (CHK-001)
	denied := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCreateCheckpoint,
		`{"stageRunId":"`+runID+`"}`, ""))
	if code := m7ErrCode(t, denied); code != "M7-CHK-001" {
		t.Fatalf("checkpoint want M7-CHK-001, got %s", code)
	}

	// resolving the mark unblocks evaluation (falls through to FAIL on
	// missing approved review, no longer BLOCKED)
	resolved := h.e.Handle(ctx, m7Request(bridge.MethodTraceResolveStale,
		`{"staleMarkId":"`+mm.StaleMarkID+`","resolutionType":"recaptured","resolvedBy":"op-1"}`, ""))
	var rr struct {
		ResolutionID string `json:"resolutionId"`
	}
	m7Decode(t, resolved, &rr)
	if len(rr.ResolutionID) != 26 {
		t.Fatalf("unexpected resolution: %+v", rr)
	}

	after := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowEvaluateGate,
		`{"stageRunId":"`+runID+`","gateKey":"stage.exit"}`, ""))
	var ar struct {
		Decision string `json:"decision"`
	}
	m7Decode(t, after, &ar)
	if ar.Decision == m7flow.GateBlocked {
		t.Fatalf("gate must not stay blocked after resolution: %+v", ar)
	}
}

// h7Alias keeps the helper name short in the second test.
func new7EvidenceHarnessFixed(t *testing.T) *m7EvidenceHarness { return newM7EvidenceHarness(t) }

func TestGatePassAndCheckpointSequencing(t *testing.T) {
	h := newM7EvidenceHarness(t)
	ctx := context.Background()
	runID := h.runningStageRun(t)

	// self review is refused (M7-REV-001)
	self := h.e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"stage_run","subjectId":"`+runID+`","verdict":"approve",`+
			`"reviewerId":"rev-1","authorId":"rev-1","reason":"same person"}`, ""))
	if code := m7ErrCode(t, self); code != "M7-REV-001" {
		t.Fatalf("self review want M7-REV-001, got %s", code)
	}

	approved := h.e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"stage_run","subjectId":"`+runID+`","verdict":"approve",`+
			`"reviewerId":"rev-1","authorId":"author-1","reason":"looks good"}`, ""))
	var rv struct {
		ReviewID    string `json:"reviewId"`
		TraceEdgeID string `json:"traceEdgeId"`
	}
	m7Decode(t, approved, &rv)
	if len(rv.ReviewID) != 26 || len(rv.TraceEdgeID) != 26 {
		t.Fatalf("unexpected review: %+v", rv)
	}

	evaluated := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowEvaluateGate,
		`{"stageRunId":"`+runID+`","gateKey":"stage.exit"}`, ""))
	var er struct {
		Decision string `json:"decision"`
	}
	m7Decode(t, evaluated, &er)
	if er.Decision != m7flow.GatePass {
		t.Fatalf("stage.exit must PASS, got %+v", er)
	}

	cp1 := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCreateCheckpoint,
		`{"stageRunId":"`+runID+`"}`, ""))
	var c1 struct {
		CheckpointID string `json:"checkpointId"`
		Sequence     int64  `json:"sequence"`
	}
	m7Decode(t, cp1, &c1)
	if len(c1.CheckpointID) != 26 || c1.Sequence != 1 {
		t.Fatalf("unexpected checkpoint: %+v", c1)
	}

	cp2 := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowCreateCheckpoint,
		`{"stageRunId":"`+runID+`"}`, ""))
	var c2 struct {
		Sequence int64 `json:"sequence"`
	}
	m7Decode(t, cp2, &c2)
	if c2.Sequence != 2 {
		t.Fatalf("checkpoint sequence must advance: %+v", c2)
	}
}

func TestDevTaskLifecycleAndIntegrationGate(t *testing.T) {
	h := newM7EvidenceHarness(t)
	ctx := context.Background()
	runID := h.runningStageRun(t)

	created := h.e.Handle(ctx, m7Request(bridge.MethodDevTaskCreate,
		`{"stageRunId":"`+runID+`","title":"补齐测试证据","priority":"P1","risk":"medium",`+
			`"acceptanceDigest":"`+evdDigest+`","assigneeId":"dev-1"}`, ""))
	var dc struct {
		TaskID      string `json:"taskId"`
		State       string `json:"state"`
		LockVersion int64  `json:"lockVersion"`
	}
	m7Decode(t, created, &dc)
	if dc.State != m7flow.TaskDraft || dc.LockVersion != 1 {
		t.Fatalf("unexpected create: %+v", dc)
	}

	// stale optimistic lock (M7-WF-004)
	stale := h.e.Handle(ctx, m7Request(bridge.MethodDevTaskTransition,
		`{"taskId":"`+dc.TaskID+`","expectedVersion":9,"to":"ready","reason":""}`, ""))
	if code := m7ErrCode(t, stale); code != "M7-WF-004" {
		t.Fatalf("stale lock want M7-WF-004, got %s", code)
	}

	// draft -> ready -> in_progress -> in_review -> done
	steps := []string{"ready", "in_progress", "in_review", "done"}
	version := dc.LockVersion
	for _, to := range steps {
		tr := h.e.Handle(ctx, m7Request(bridge.MethodDevTaskTransition,
			`{"taskId":"`+dc.TaskID+`","expectedVersion":`+itoa(version)+`,"to":"`+to+`","reason":"flow"}`, ""))
		var out struct {
			State       string `json:"state"`
			LockVersion int64  `json:"lockVersion"`
		}
		m7Decode(t, tr, &out)
		if out.State != to || out.LockVersion != version+1 {
			t.Fatalf("transition to %s failed: %+v", to, out)
		}
		version++
	}

	// illegal edge: done -> ready (M7-TSK-001)
	illegal := h.e.Handle(ctx, m7Request(bridge.MethodDevTaskTransition,
		`{"taskId":"`+dc.TaskID+`","expectedVersion":`+itoa(version)+`,"to":"ready","reason":"nope"}`, ""))
	if code := m7ErrCode(t, illegal); code != "M7-TSK-001" {
		t.Fatalf("illegal edge want M7-TSK-001, got %s", code)
	}

	// evidence rows attach under the run reference
	test := h.e.Handle(ctx, m7Request(bridge.MethodEvidenceAttachTest,
		`{"taskRef":"`+runID+`","result":"pass","reportDigest":"`+evdDigest+`"}`, ""))
	var tv struct {
		TestID string `json:"testId"`
	}
	m7Decode(t, test, &tv)
	if len(tv.TestID) != 26 {
		t.Fatalf("unexpected test run: %+v", tv)
	}
	scan := h.e.Handle(ctx, m7Request(bridge.MethodEvidenceAttachScan,
		`{"taskRef":"`+runID+`","scanner":"trivy","severityGate":"high","reportDigest":"`+evdDigest+`"}`, ""))
	var sv struct {
		ScanID string `json:"scanId"`
	}
	m7Decode(t, scan, &sv)
	if len(sv.ScanID) != 26 {
		t.Fatalf("unexpected scan run: %+v", sv)
	}

	// with the task closed and a passing test the integration gate passes
	integration := h.e.Handle(ctx, m7Request(bridge.MethodWorkflowEvaluateGate,
		`{"stageRunId":"`+runID+`","gateKey":"dev.integration"}`, ""))
	var ir struct {
		Decision string `json:"decision"`
	}
	m7Decode(t, integration, &ir)
	if ir.Decision != m7flow.GatePass {
		t.Fatalf("dev.integration must PASS, got %+v", ir)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
