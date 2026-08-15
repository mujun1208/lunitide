package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/merge"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

// m6MergeHarness wires the slice-4 services (root-writer merge walk +
// final-tree gate + transactional outbox) behind the merge.submit handler
// on a real store, with one live agent run as the governance root. The
// git side is faked: heads/patch/apply/marker are in-memory, so the tests
// exercise the durable walk (total order, CAS, fencing, recovery) rather
// than git plumbing (worktree_test.go covers that against real git).
type m6MergeHarness struct {
	e      *Engine
	rootID string
	svc    *m6app.MergeService
	outbox *m6app.OutboxService
	repo   *storage.AgentRuntimeRepository
	clock  *fakeMergeClock

	mu        sync.Mutex
	heads     map[string]string // rootID -> current head
	markerID  string            // intent stamped at HEAD (crash ground truth)
	failApply bool              // next apply call fails (interrupted apply)
	gatePass  bool
}

func newM6MergeHarness(t *testing.T) *m6MergeHarness {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "m6merge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	created, err := projects.Create(context.Background(), "m6-merge-project", "test", struct {
		Name string `json:"name"`
	}{"Root"}, project.Project{Name: "Root"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	sess, err := sessions.Create(context.Background(), "m6-merge-session", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{created.ID, "S"}, session.Session{ProjectID: created.ID, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	e.SetAgentRunService(agentrunapp.New(store.AgentRuntimeRepository()))
	repo := store.AgentRuntimeRepository()
	h := &m6MergeHarness{
		e: e, repo: repo, heads: map[string]string{},
		clock:    newFakeMergeClock(time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)),
		gatePass: true,
	}
	run := startAgentRun(t, e, sess.ID, "m6-merge-root-run-key")
	h.rootID = run.ID
	h.heads[h.rootID] = "h-base-0"
	h.outbox = m6app.NewOutboxService(repo)
	h.svc = m6app.NewMergeService(repo, h.outbox, h, h.headOf, h.patchOf, h.applyTo, h.markerOf)
	h.svc.SetClock(h.clock)
	e.SetM6MergeServices(h.svc)
	return h
}

// --- fakes (merge.TestGate + the service function hooks) -------------------

func (h *m6MergeHarness) RunFinalTests(ctx context.Context, rootID, treeHead string) (merge.TestOutcome, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.gatePass {
		return merge.TestOutcome{Passed: false, Detail: "final tests failed"}, nil
	}
	return merge.TestOutcome{Passed: true, TestsRef: "evidence://final/tests", FinalDigest: sha256Hex(treeHead)}, nil
}

func (h *m6MergeHarness) headOf(ctx context.Context, rootID string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.heads[rootID], nil
}

func (h *m6MergeHarness) patchOf(ctx context.Context, rootID, childID, patchDigest string) ([]byte, error) {
	return []byte("diff --git a/x.txt b/x.txt\n--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-a\n+b\n"), nil
}

func (h *m6MergeHarness) applyTo(ctx context.Context, rootID, intentID string, patch []byte, expectedHead string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failApply {
		h.failApply = false
		return "", errors.New("apply interrupted before landing")
	}
	if h.heads[rootID] != expectedHead {
		return "", errors.New("head moved under apply")
	}
	newHead := "h-" + intentID
	h.heads[rootID] = newHead
	h.markerID = intentID // the marker commit lands with the patch
	return newHead, nil
}

func (h *m6MergeHarness) markerOf(ctx context.Context, rootID string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.markerID, nil
}

func (h *m6MergeHarness) setHead(head string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.heads[h.rootID] = head
}

func (h *m6MergeHarness) currentHead() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.heads[h.rootID]
}

type fakeMergeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeMergeClock(start time.Time) *fakeMergeClock { return &fakeMergeClock{now: start} }

func (c *fakeMergeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeMergeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// --- helpers ---------------------------------------------------------------

func mergeSubmitPayload(rootID string, seq int64, expectedHead string) string {
	return `{"rootId":"` + rootID + `","sequence":` + jsonInt(seq) +
		`,"intent":{"childId":"child-` + jsonInt(seq) + `","expectedHead":"` + expectedHead +
		`","patchDigest":"` + sha256Hex("patch-"+jsonInt(seq)) + `","testsRef":"evidence://run/tests"}}`
}

func (h *m6MergeHarness) submit(t *testing.T, key, expectedHead string, seq int64) (string, string) {
	t.Helper()
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodMergeSubmit,
		mergeSubmitPayload(h.rootID, seq, expectedHead), key))
	if !resp.OK {
		t.Fatalf("merge.submit(seq %d) failed: code=%s msg=%s", seq, resp.Error.Code, resp.Error.Message)
	}
	var out struct {
		IntentID string `json:"intentId"`
		State    string `json:"state"`
	}
	m6Payload(t, resp, &out)
	return out.IntentID, out.State
}

func (h *m6MergeHarness) submitFail(t *testing.T, key, expectedHead string, seq int64) string {
	t.Helper()
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodMergeSubmit,
		mergeSubmitPayload(h.rootID, seq, expectedHead), key))
	if resp.OK {
		t.Fatalf("merge.submit(seq %d) unexpectedly succeeded", seq)
	}
	return resp.Error.Code
}

func (h *m6MergeHarness) intentState(t *testing.T, intentID string) m6supply.MergeIntent {
	t.Helper()
	var row m6supply.MergeIntent
	err := h.repo.TransactM6(context.Background(), func(tx m6app.Tx) error {
		var gerr error
		row, gerr = tx.GetM6MergeIntent(intentID)
		return gerr
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// --- handler tests ---------------------------------------------------------

func TestMergeSubmitQueuesAndIdempotentReplay(t *testing.T) {
	h := newM6MergeHarness(t)
	id1, state := h.submit(t, "k1", "h-base-0", 1)
	if state != "queued" {
		t.Fatalf("fresh CAS-match submit must queue, got %s", state)
	}
	// idempotent replay: same key + same body -> same intent
	replayID, replayState := h.submit(t, "k1", "h-base-0", 1)
	if replayID != id1 || replayState != "queued" {
		t.Fatalf("replay diverged: %s/%s vs %s/queued", replayID, replayState, id1)
	}
	// same key, different request -> conflict
	if code := h.submitFail(t, "k1", "h-base-0", 2); code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("key reuse with a different request must conflict, got %s", code)
	}
	// total-order slot conflict: another key claims the same (root, sequence)
	if code := h.submitFail(t, "k2", "h-base-0", 1); code != "M6_MERGE_SEQUENCE_CONFLICT" {
		t.Fatalf("sequence slot conflict expected M6_MERGE_SEQUENCE_CONFLICT, got %s", code)
	}
}

func TestMergeSubmitStaleFastFail(t *testing.T) {
	h := newM6MergeHarness(t)
	h.setHead("h-moved")
	_, state := h.submit(t, "k1", "h-base-0", 1)
	if state != "stale" {
		t.Fatalf("expected-head no longer at HEAD must answer stale, got %s", state)
	}
}

func TestMergeSubmitSchemaInvalid(t *testing.T) {
	h := newM6MergeHarness(t)
	bad := `{"rootId":"` + h.rootID + `","sequence":1,"intent":{"childId":"c",` +
		`"expectedHead":"h","patchDigest":"nope","testsRef":"evidence://t"}}`
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodMergeSubmit, bad, "k1"))
	if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("bad patch digest must fail schema validation, got ok=%v code=%s", resp.OK, resp.Error.Code)
	}
}

// --- writer walk -----------------------------------------------------------

func TestWriterWalkTotalOrderAndSerialRebase(t *testing.T) {
	h := newM6MergeHarness(t)
	id1, _ := h.submit(t, "k1", "h-base-0", 1)
	id2, _ := h.submit(t, "k2", "h-base-0", 2)
	lease, err := h.svc.AcquireWriterLease(h.rootID, "writer-1")
	if err != nil {
		t.Fatal(err)
	}
	// seq 1 merges onto h-base-0
	r1, err := h.svc.ProcessNext(context.Background(), h.rootID, lease)
	if err != nil || r1.State != m6supply.MergeIntentMerged || r1.IntentID != id1 {
		t.Fatalf("seq1 merge: %+v err=%v", r1, err)
	}
	// seq 2 patched against the old head: CAS conflict -> stale (MRG-001)
	r2, err := h.svc.ProcessNext(context.Background(), h.rootID, lease)
	if err != nil || r2.State != m6supply.MergeIntentStale || r2.IntentID != id2 {
		t.Fatalf("seq2 must go stale after the head moved: %+v err=%v", r2, err)
	}
	// recovery pins the serial-rebase target as the new expected head
	if _, err := h.svc.Recover(context.Background(), h.rootID); err != nil {
		t.Fatal(err)
	}
	row := h.intentState(t, id2)
	if row.State != m6supply.MergeIntentRebaseNeeded {
		t.Fatalf("stale intent must advance to rebase_required, got %s", row.State)
	}
	if row.ExpectedHead != h.currentHead() {
		t.Fatalf("rebase must pin the new expected head %s, got %s", h.currentHead(), row.ExpectedHead)
	}
	// the rebased walk now passes CAS and merges
	r3, err := h.svc.ProcessNext(context.Background(), h.rootID, lease)
	if err != nil || r3.State != m6supply.MergeIntentMerged || r3.IntentID != id2 {
		t.Fatalf("rebased seq2 merge: %+v err=%v", r3, err)
	}
	// queue drained
	r4, err := h.svc.ProcessNext(context.Background(), h.rootID, lease)
	if err != nil || r4.State != "empty" {
		t.Fatalf("drained queue must answer empty: %+v err=%v", r4, err)
	}
	// total order held: seq1 terminal before seq2 merged
	if h.intentState(t, id1).State != m6supply.MergeIntentMerged {
		t.Fatal("seq1 must stay merged")
	}
}

func TestWriterFencing(t *testing.T) {
	h := newM6MergeHarness(t)
	h.submit(t, "k1", "h-base-0", 1)
	w1, err := h.svc.AcquireWriterLease(h.rootID, "writer-1")
	if err != nil {
		t.Fatal(err)
	}
	// a live lease held by another owner is refused (MRG-002)
	if _, err := h.svc.AcquireWriterLease(h.rootID, "writer-2"); !errors.Is(err, merge.ErrWriterFenced) {
		t.Fatalf("second owner must be fenced, got %v", err)
	}
	// after expiry a takeover advances the epoch...
	h.clock.Advance(65 * time.Second)
	w2, err := h.svc.AcquireWriterLease(h.rootID, "writer-2")
	if err != nil {
		t.Fatal(err)
	}
	if w2.Epoch <= w1.Epoch {
		t.Fatalf("takeover must advance the epoch: %d -> %d", w1.Epoch, w2.Epoch)
	}
	// ...and the old epoch can never touch the final tree again
	if _, err := h.svc.ProcessNext(context.Background(), h.rootID, w1); !errors.Is(err, merge.ErrWriterFenced) {
		t.Fatalf("superseded epoch must be fenced, got %v", err)
	}
}

func TestRecoverRequeuesInterruptedApply(t *testing.T) {
	h := newM6MergeHarness(t)
	id1, _ := h.submit(t, "k1", "h-base-0", 1)
	lease, _ := h.svc.AcquireWriterLease(h.rootID, "writer-1")
	h.failApply = true
	if _, err := h.svc.ProcessNext(context.Background(), h.rootID, lease); !errors.Is(err, m6app.ErrApplyFailed) {
		t.Fatalf("interrupted apply must surface ErrApplyFailed, got %v", err)
	}
	if row := h.intentState(t, id1); row.State != m6supply.MergeIntentApplying {
		t.Fatalf("failed apply must leave the intent applying (EffectJournal), got %s", row.State)
	}
	// the apply never landed (no marker), so recovery requeues — retry is
	// idempotent and the patch is not skipped
	if _, err := h.svc.Recover(context.Background(), h.rootID); err != nil {
		t.Fatal(err)
	}
	if row := h.intentState(t, id1); row.State != m6supply.MergeIntentQueued {
		t.Fatalf("unmarked applying intent must requeue, got %s", row.State)
	}
	if _, err := h.svc.ProcessNext(context.Background(), h.rootID, lease); err != nil {
		t.Fatalf("retried merge failed: %v", err)
	}
	if row := h.intentState(t, id1); row.State != m6supply.MergeIntentMerged {
		t.Fatalf("retried intent must merge, got %s", row.State)
	}
}

func TestRecoverRecordsLandedApply(t *testing.T) {
	h := newM6MergeHarness(t)
	// a crash between the landed patch and the merged commit leaves the
	// intent applying while HEAD already carries its marker
	var intentID string
	err := h.repo.TransactM6(context.Background(), func(tx m6app.Tx) error {
		intentID = ulid.Make().String()
		return tx.PutM6MergeIntent(m6supply.MergeIntent{
			ID: intentID, RootID: h.rootID, ChildID: "child-crash", Sequence: 1,
			ExpectedHead: "h-base-0", CurrentHead: "h-landed",
			PatchDigest: sha256Hex("crash-patch"), TestsRef: "evidence://run/tests",
			State: m6supply.MergeIntentApplying, Version: 1,
			CreatedAt: h.clock.Now(), UpdatedAt: h.clock.Now(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.setHead("h-landed")
	h.mu.Lock()
	h.markerID = intentID
	h.mu.Unlock()
	actions, err := h.svc.Recover(context.Background(), h.rootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].To != m6supply.MergeIntentMerged {
		t.Fatalf("marker-matched intent must converge merged: %+v", actions)
	}
	row := h.intentState(t, intentID)
	if row.State != m6supply.MergeIntentMerged || row.CurrentHead != "h-landed" {
		t.Fatalf("landed effect must be recorded merged at the observed head: %+v", row)
	}
}

// --- final gate ------------------------------------------------------------

func TestFinalizeRootGatePass(t *testing.T) {
	h := newM6MergeHarness(t)
	h.submit(t, "k1", "h-base-0", 1)
	lease, _ := h.svc.AcquireWriterLease(h.rootID, "writer-1")
	if _, err := h.svc.ProcessNext(context.Background(), h.rootID, lease); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.FinalizeRoot(context.Background(), h.rootID)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != m6supply.FinalGateComplete {
		t.Fatalf("passing gate must complete, got %s", res.State)
	}
	if res.FinalDigest != sha256Hex(h.currentHead()) {
		t.Fatalf("final digest must bind the final tree head: %s", res.FinalDigest)
	}
}

func TestFinalizeRootGateFailNeverCompletes(t *testing.T) {
	h := newM6MergeHarness(t)
	id1, _ := h.submit(t, "k1", "h-base-0", 1)
	lease, _ := h.svc.AcquireWriterLease(h.rootID, "writer-1")
	if _, err := h.svc.ProcessNext(context.Background(), h.rootID, lease); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.gatePass = false
	h.mu.Unlock()
	res, err := h.svc.FinalizeRoot(context.Background(), h.rootID)
	if err != nil {
		t.Fatal(err)
	}
	// TST-001: a failing final run NEVER completes — child-local passes are
	// not a substitute
	if res.State != m6supply.FinalGateRecovery {
		t.Fatalf("failing gate must freeze recovery_required, got %s", res.State)
	}
	if len(res.RollbackCandidates) != 1 || res.RollbackCandidates[0] != id1 {
		t.Fatalf("rollback candidates must be reverse merge order: %v", res.RollbackCandidates)
	}
}

func TestFinalizeRootBlockedWhilePending(t *testing.T) {
	h := newM6MergeHarness(t)
	h.submit(t, "k1", "h-base-0", 1)
	if _, err := h.svc.FinalizeRoot(context.Background(), h.rootID); !errors.Is(err, merge.ErrFinalizePending) {
		t.Fatalf("pending intent must block the gate, got %v", err)
	}
}

// --- outbox ----------------------------------------------------------------

// capturingPublisher reports a configured prefix of the batch as
// delivered — everything else stays unpublished for the next drain.
type capturingPublisher struct {
	deliverFirst int
	seen         []m6supply.OutboxEvent
}

func (p *capturingPublisher) Publish(ctx context.Context, events []m6supply.OutboxEvent) ([]string, error) {
	p.seen = append(p.seen, events...)
	n := p.deliverFirst
	if n > len(events) {
		n = len(events)
	}
	ids := make([]string, 0, n)
	for _, e := range events[:n] {
		ids = append(ids, e.ID)
	}
	return ids, nil
}

func TestOutboxDrainAtLeastOnce(t *testing.T) {
	h := newM6MergeHarness(t)
	h.submit(t, "k1", "h-base-0", 1)
	lease, _ := h.svc.AcquireWriterLease(h.rootID, "writer-1")
	if _, err := h.svc.ProcessNext(context.Background(), h.rootID, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.FinalizeRoot(context.Background(), h.rootID); err != nil {
		t.Fatal(err)
	}
	// submitted + merged + final.testing + final.completed = 4 events
	pub := &capturingPublisher{deliverFirst: 2}
	drained, err := h.outbox.Drain(context.Background(), pub, 100)
	if err != nil {
		t.Fatal(err)
	}
	if drained.Fetched != 4 || drained.Delivered != 2 || drained.Remaining != 2 {
		t.Fatalf("partial delivery must leave rows unpublished: %+v", drained)
	}
	// the next drain re-delivers the unpublished half (at-least-once)
	pub.deliverFirst = 100
	drained, err = h.outbox.Drain(context.Background(), pub, 100)
	if err != nil {
		t.Fatal(err)
	}
	if drained.Delivered != 2 || drained.Remaining != 0 {
		t.Fatalf("second drain must clear the backlog: %+v", drained)
	}
	// consumers dedupe by event id: total sightings = 6 for 4 events
	if len(pub.seen) != 6 {
		t.Fatalf("at-least-once redelivery expected 6 sightings, got %d", len(pub.seen))
	}
	ids := map[string]int{}
	for _, e := range pub.seen {
		ids[e.ID]++
	}
	if len(ids) != 4 {
		t.Fatalf("expected 4 distinct events, got %d", len(ids))
	}
}
