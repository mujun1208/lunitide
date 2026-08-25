package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// newM7EvidenceHarness opens a store with one project, a published workflow
// version and a running DEVELOPMENT_CHANGE stage run carrying an input
// snapshot — the minimum fixture for evidence-graph tests.
func newM7EvidenceHarness(t *testing.T) (*m7app.TraceService, *m7app.GateService, *m7app.ReviewService, *Store, string) {
	t.Helper()
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m7evd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	wf := m7app.NewWorkflowService(repo)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	pid := "01ARZ3NDEKTSV4RRFFQ69G5FA0"
	if _, err := store.db.Exec(`INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?, 'ITM00001', ?,?)`,
		pid, "m7-evd-project", now, now); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	v, err := wf.CreateVersion(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wf.Publish(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	res, err := wf.StartStage(ctx, pid, "DEVELOPMENT_CHANGE")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wf.CaptureInput(ctx, res.Run.ID, map[string]any{"m6.finalTree": map[string]any{
		"rootId": "01ARZ3NDEKTSV4RRFFQ69G5FA1", "digest": strings.Repeat("a1", 32),
	}}); err != nil {
		t.Fatal(err)
	}
	trace := m7app.NewTraceService(repo)
	gate := m7app.NewGateService(repo)
	review := m7app.NewReviewService(repo, trace)
	return trace, gate, review, store, res.Run.ID
}

func digestOf(v ...any) string { return m7flow.Digest256(v) }

// TRC-001: edges with a missing endpoint are rejected; identical re-adds are
// idempotent; digest conflicts on the same endpoints are refused.
func TestTraceEdgeLifecycle(t *testing.T) {
	trace, _, _, store, runID := newM7EvidenceHarness(t)
	ctx := context.Background()

	_, err := trace.AddEdge(ctx, m7flow.TraceEdge{
		FromType: "stage_run", FromID: runID, FromDigest: digestOf("run"),
		Relation: m7flow.RelProduces, ToType: "artifact_version", ToID: "01ARZ3NDEKTSV4RRFFQ69G5FB2",
		ToDigest: digestOf("art"),
	})
	if !errors.Is(err, m7flow.ErrDanglingEdge) {
		t.Fatalf("missing artifact endpoint must fail with ErrDanglingEdge, got %v", err)
	}

	e1 := m7flow.TraceEdge{
		FromType: "stage_run", FromID: runID, FromDigest: digestOf("run"),
		Relation: m7flow.RelProduces,
		ToType:   "stage_run", ToID: runID, ToDigest: digestOf("self"),
	}
	first, err := trace.AddEdge(ctx, e1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := trace.AddEdge(ctx, e1)
	if err != nil {
		t.Fatalf("identical re-add must be idempotent: %v", err)
	}
	if again.ID != first.ID {
		t.Fatal("idempotent re-add must answer the stored edge")
	}
	e1.FromDigest = digestOf("drift")
	if _, err := trace.AddEdge(ctx, e1); !errors.Is(err, m7app.ErrDuplicateEdge) {
		t.Fatalf("digest drift on same endpoints must fail with ErrDuplicateEdge, got %v", err)
	}
	if _, err := trace.AddEdge(ctx, m7flow.TraceEdge{
		FromType: "stage_run", FromID: runID, FromDigest: digestOf("x"),
		Relation: "_contains", ToType: "stage_run", ToID: runID, ToDigest: digestOf("y"),
	}); !errors.Is(err, m7flow.ErrBadRelation) {
		t.Fatalf("unknown relation must fail with ErrBadRelation, got %v", err)
	}

	// M7-EVD-001: direct UPDATE/DELETE on trace_edges trips the trigger.
	if _, err := store.db.Exec(`UPDATE trace_edges SET relation='reviews' WHERE id=?`, first.ID); err == nil ||
		!strings.Contains(err.Error(), "M7-EVD-001") {
		t.Fatalf("append-only trigger must reject updates, got %v", err)
	}
	if _, err := store.db.Exec(`DELETE FROM trace_edges WHERE id=?`, first.ID); err == nil ||
		!strings.Contains(err.Error(), "M7-EVD-001") {
		t.Fatalf("append-only trigger must reject deletes, got %v", err)
	}
}

// trace.query walks down and up across a two-hop chain with cursor paging.
func TestTraceQueryWalksBothDirections(t *testing.T) {
	trace, _, _, _, runID := newM7EvidenceHarness(t)
	ctx := context.Background()
	// Downward hop: the stage run traces to its own snapshot (self-loop on
	// the only guaranteed node), plus a dev task implementing the run.
	task, err := trace.CreateDevTask(ctx, m7flow.DevTask{
		StageRunID: runID, Title: "implement gate evaluator",
		Priority: m7flow.PriorityP1, Risk: m7flow.RiskMedium,
		AcceptanceDigest: digestOf("acceptance"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trace.AddEdge(ctx, m7flow.TraceEdge{
		FromType: "stage_run", FromID: runID, FromDigest: digestOf("run"),
		Relation: m7flow.RelTracesTo,
		ToType:   "stage_run", ToID: runID, ToDigest: digestOf("self"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := trace.AddEdge(ctx, m7flow.TraceEdge{
		FromType: "dev_task", FromID: task.ID, FromDigest: digestOf("task"),
		Relation: m7flow.RelImplements,
		ToType:   "stage_run", ToID: runID, ToDigest: digestOf("run"),
	}); err != nil {
		t.Fatal(err)
	}
	down, err := trace.Query(ctx, "stage_run", runID, "down", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(down.Edges) == 0 {
		t.Fatal("downward query from stage_run must return the produced edges")
	}
	up, err := trace.Query(ctx, "stage_run", runID, "up", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range up.Edges {
		if e.FromType == "dev_task" && e.FromID == task.ID && e.Relation == m7flow.RelImplements {
			found = true
		}
	}
	if !found {
		t.Fatalf("upward query must surface the implementing dev task, got %+v", up.Edges)
	}
	if _, err := trace.Query(ctx, "stage_run", runID, "sideways", 2, ""); !errors.Is(err, m7app.ErrBadDirection) {
		t.Fatalf("bad direction must fail with ErrBadDirection, got %v", err)
	}
	if _, err := trace.Query(ctx, "stage_run", runID, "down", 11, ""); !errors.Is(err, m7app.ErrBadDepth) {
		t.Fatalf("depth 11 must fail with ErrBadDepth, got %v", err)
	}
}

// TRC-002: stale marks block gates until a resolution row is appended.
func TestStaleLifecycleBlocksThenResolves(t *testing.T) {
	trace, gate, _, _, runID := newM7EvidenceHarness(t)
	ctx := context.Background()
	mark, err := trace.MarkStale(ctx, "stage_run", runID, "cause-edge-1")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := trace.MarkStale(ctx, "stage_run", runID, "other-cause")
	if err != nil || twice.ID != mark.ID {
		t.Fatalf("marking stale must be idempotent per subject: %v", err)
	}
	if ok, _ := trace.OutstandingStale(ctx, "stage_run", runID); !ok {
		t.Fatal("fresh mark must be outstanding")
	}
	// The stage.exit gate is BLOCKED while the mark is outstanding.
	ev, err := gate.Evaluate(ctx, runID, "stage.exit")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Decision != m7flow.GateBlocked {
		t.Fatalf("outstanding stale must BLOCK, got %s", ev.Decision)
	}
	if len(ev.Findings) == 0 || ev.Findings[0].Code != "M7-TRC-002" {
		t.Fatalf("blocked evaluation must carry M7-TRC-002, got %+v", ev.Findings)
	}
	if _, err := trace.ResolveStale(ctx, mark.ID, "wave-off", "", "reviewer-1"); !errors.Is(err, m7flow.ErrBadResolution) {
		t.Fatalf("unknown resolution must fail with ErrBadResolution, got %v", err)
	}
	if _, err := trace.ResolveStale(ctx, mark.ID, m7flow.ResolveRecaptured, "", "reviewer-1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := trace.OutstandingStale(ctx, "stage_run", runID); ok {
		t.Fatal("resolved mark must not be outstanding")
	}
}

// GAT-001 + CHK-001: stage.exit fails without an approved review, passes
// after one, and only PASS over the current digest may checkpoint.
func TestGateEvaluationAndCheckpoint(t *testing.T) {
	trace, gate, review, store, runID := newM7EvidenceHarness(t)
	ctx := context.Background()

	fail, err := gate.Evaluate(ctx, runID, "stage.exit")
	if err != nil {
		t.Fatal(err)
	}
	if fail.Decision != m7flow.GateFail {
		t.Fatalf("stage.exit without review must FAIL, got %s", fail.Decision)
	}
	if _, err := gate.CreateCheckpoint(ctx, runID); !errors.Is(err, m7flow.ErrCheckpointDenied) {
		t.Fatalf("checkpoint before PASS must be denied, got %v", err)
	}
	unknown, err := gate.Evaluate(ctx, runID, "no.such.gate")
	if err != nil || unknown.Decision != m7flow.GateFail {
		t.Fatalf("unknown gate key must FAIL with M7-GAT-002, got %v / %s", err, unknown.Decision)
	}

	rev, edgeID, err := review.SubmitReview(ctx, m7flow.Review{
		SubjectType: "stage_run", SubjectID: runID, Verdict: m7flow.VerdictApprove,
		ReviewerID: "reviewer-a", Reason: "LGTM",
	}, "author-x")
	if err != nil {
		t.Fatal(err)
	}
	if rev.ID == "" || edgeID == "" {
		t.Fatal("review and trace edge IDs must be answered")
	}
	pass, err := gate.Evaluate(ctx, runID, "stage.exit")
	if err != nil {
		t.Fatal(err)
	}
	if pass.Decision != m7flow.GatePass {
		t.Fatalf("approved review must make stage.exit PASS, got %s %+v", pass.Decision, pass.Findings)
	}
	// Idempotent: the same input digest answers the stored evaluation.
	repeat, err := gate.Evaluate(ctx, runID, "stage.exit")
	if err != nil || repeat.ID != pass.ID {
		t.Fatalf("identical inputs must answer the stored evaluation: %v", err)
	}
	cp, err := gate.CreateCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Sequence != 1 {
		t.Fatalf("first checkpoint sequence must be 1, got %d", cp.Sequence)
	}
	cp2, err := gate.CreateCheckpoint(ctx, runID)
	if err != nil || cp2.Sequence != 2 {
		t.Fatalf("second checkpoint must be sequence 2: %v", err)
	}
	// REV-001: self review is refused.
	if _, _, err := review.SubmitReview(ctx, m7flow.Review{
		SubjectType: "stage_run", SubjectID: runID, Verdict: m7flow.VerdictReject,
		ReviewerID: "reviewer-a", Reason: "self",
	}, "reviewer-a"); !errors.Is(err, m7flow.ErrSelfReview) {
		t.Fatalf("self review must fail with ErrSelfReview, got %v", err)
	}
	// Append-only reviews.
	if _, err := store.db.Exec(`UPDATE reviews SET verdict='reject' WHERE id=?`, rev.ID); err == nil ||
		!strings.Contains(err.Error(), "M7-EVD-001") {
		t.Fatalf("reviews must be append-only, got %v", err)
	}
	_ = trace
}

// dev.integration needs every task closed plus green tests; the dev-task
// machine is guarded and optimistic-locked.
func TestDevTaskMachineAndIntegrationGate(t *testing.T) {
	trace, gate, _, _, runID := newM7EvidenceHarness(t)
	ctx := context.Background()
	task, err := trace.CreateDevTask(ctx, m7flow.DevTask{
		StageRunID: runID, Title: "slice-2 work",
		Priority: m7flow.PriorityP0, Risk: m7flow.RiskHigh,
		AcceptanceDigest: digestOf("acc"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Illegal jump draft -> done.
	if _, err := trace.TransitionDevTask(ctx, task.ID, 1, m7flow.TaskDone, ""); !errors.Is(err, m7app.ErrTaskTransition) {
		t.Fatalf("draft->done must be illegal, got %v", err)
	}
	for _, to := range []string{m7flow.TaskReady, m7flow.TaskInProgress, m7flow.TaskInReview, m7flow.TaskDone} {
		task, err = trace.TransitionDevTask(ctx, task.ID, task.LockVersion, to, "")
		if err != nil {
			t.Fatalf("transition to %s failed: %v", to, err)
		}
	}
	if task.State != m7flow.TaskDone || task.LockVersion != 5 {
		t.Fatalf("task must be done at lock 5, got %s/%d", task.State, task.LockVersion)
	}
	// Stale lock version is a conflict.
	if _, err := trace.TransitionDevTask(ctx, task.ID, 1, m7flow.TaskReopened, "defect"); !errors.Is(err, m7app.ErrVersionConflict) {
		t.Fatalf("stale lock must conflict, got %v", err)
	}
	// Without test evidence the integration gate fails.
	noTests, err := gate.Evaluate(ctx, runID, "dev.integration")
	if err != nil || noTests.Decision != m7flow.GateFail {
		t.Fatalf("integration gate without tests must FAIL, got %v/%s", err, noTests.Decision)
	}
	if _, err := trace.AttachTestRun(ctx, runID, "pass", digestOf("report")); err != nil {
		t.Fatal(err)
	}
	withTests, err := gate.Evaluate(ctx, runID, "dev.integration")
	if err != nil || withTests.Decision != m7flow.GatePass {
		t.Fatalf("closed tasks + green tests must PASS, got %v/%s %+v", err, withTests.Decision, withTests.Findings)
	}
	// Reopen the task: the gate must fail again.
	if _, err := trace.TransitionDevTask(ctx, task.ID, task.LockVersion, m7flow.TaskReopened, "defect found in QA"); err != nil {
		t.Fatal(err)
	}
	reopened, err := gate.Evaluate(ctx, runID, "dev.integration")
	if err != nil || reopened.Decision != m7flow.GateFail {
		t.Fatalf("reopened task must fail the integration gate, got %v/%s", err, reopened.Decision)
	}
}

// verify.security / release.package key off scan evidence.
func TestSecurityAndPackageGatesNeedScans(t *testing.T) {
	trace, gate, _, _, runID := newM7EvidenceHarness(t)
	ctx := context.Background()
	sec, err := gate.Evaluate(ctx, runID, "verify.security")
	if err != nil || sec.Decision != m7flow.GateFail {
		t.Fatalf("security gate without scans must FAIL, got %v/%s", err, sec.Decision)
	}
	if _, err := trace.AttachScanRun(ctx, runID, "sast", "high", digestOf("scan")); err != nil {
		t.Fatal(err)
	}
	secPass, err := gate.Evaluate(ctx, runID, "verify.security")
	if err != nil || secPass.Decision != m7flow.GatePass {
		t.Fatalf("recorded scan must pass the security gate, got %v/%s", err, secPass.Decision)
	}
}
