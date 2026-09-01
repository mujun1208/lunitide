// M8 FR-17 service tests (GT-01~GT-05 semantics, T-8.8.x): insufficient
// evidence with zero decision tokens, per-criterion fail listing, the
// single-use token lifecycle (unconfirmed preflight / expiry / replay),
// binding-drift rollback to disabled, fail-closed evidence sources and
// the evaluate idempotency - against a fully migrated SQLite store.
package m8app_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// fakeEvidence is the injected EvidenceSource fixture.
type fakeEvidence struct {
	e   m8core.GateEvidence
	err error
}

func (f *fakeEvidence) Aggregate(ctx context.Context, subjectID string, windowStart, windowEnd int64) (m8core.GateEvidence, error) {
	if f.err != nil {
		return m8core.GateEvidence{}, f.err
	}
	return f.e, nil
}

func openGateService(t *testing.T, ev m8core.GateEvidence) (*m8app.CollabGateService, *fakeEvidence, *fakeClock) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m8-gate.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	src := &fakeEvidence{e: ev}
	clock := &fakeClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	svc := m8app.NewCollabGateService(store.AgentRuntimeRepository(), src, m8core.WriteCollabBinding())
	svc.SetClock(clock)
	return svc, src, clock
}

// passingEvidence answers the full-threshold green fixture.
func passingEvidence() m8core.GateEvidence {
	return m8core.GateEvidence{
		WindowDays: 45, SubagentRuns: 600, RootRunsCovered: 30,
		WriteInterceptRate: 1, UndeclaredWrites: 0, ToctouReplayGuard: 1,
		OrphanSubagents: 0, CrashRecoveryRate: 1, TimeoutRatio: 0.02,
		CompensationSuccess: 0.995,
	}
}

const gateWinStart = int64(1750000000000)

func gateWin() (int64, int64) {
	return gateWinStart, gateWinStart + 45*24*3600*1000
}

// GT-01: window < 30d, runs < 500, root runs < 20 and partial evidence
// sources all answer insufficient_evidence with no decision minted; the
// snapshot stays append-only persisted.
func TestGateInsufficientEvidenceMintsNoDecision(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		ev   func() m8core.GateEvidence
		want []string
	}{
		{"short window", func() m8core.GateEvidence {
			e := passingEvidence()
			e.WindowDays = 20
			return e
		}, []string{m8core.CritWindowDays}},
		{"few runs", func() m8core.GateEvidence {
			e := passingEvidence()
			e.SubagentRuns = 499
			return e
		}, []string{m8core.CritSubagentRuns}},
		{"few root runs", func() m8core.GateEvidence {
			e := passingEvidence()
			e.RootRunsCovered = 19
			return e
		}, []string{m8core.CritRootRuns}},
		{"partial evidence source", func() m8core.GateEvidence {
			e := passingEvidence()
			e.Missing = []string{"intercept_audit"}
			return e
		}, []string{"intercept_audit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := openGateService(t, tc.ev())
			ws, we := gateWin()
			res, err := svc.Evaluate(ctx, m8app.EvaluateInput{
				SubjectID: "subject-1", WindowStart: ws, WindowEnd: we,
				CriteriaVersion: "write-collab-v1",
			})
			if err != nil || res.Outcome != m8core.EvalInsufficientEvidence {
				t.Fatalf("evaluate = %+v err=%v, want insufficient_evidence", res, err)
			}
			if strings.Join(res.FailedCriteria, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("failedCriteria = %v, want %v", res.FailedCriteria, tc.want)
			}
			// No decision token is minted on insufficient evidence.
			if d, has, _ := svc.PendingDecision(ctx, "subject-1"); has {
				t.Fatalf("insufficient evidence minted decision %+v", d)
			}
			// The snapshot is persisted (append-only) and idempotent.
			again, err := svc.Evaluate(ctx, m8app.EvaluateInput{
				SubjectID: "subject-1", WindowStart: ws, WindowEnd: we,
				CriteriaVersion: "write-collab-v1",
			})
			if err != nil || again.EvaluationID != res.EvaluationID {
				t.Fatalf("idempotent replay = %+v err=%v", again, err)
			}
		})
	}
}

// GT-02: each violated criterion lands in failedCriteria with outcome=fail
// and the capability stays disabled (re-evaluation is allowed).
func TestGateFailListsEveryViolatedCriterion(t *testing.T) {
	ctx := context.Background()
	svc, src, _ := openGateService(t, m8core.GateEvidence{})
	e := passingEvidence()
	e.WriteInterceptRate = 0.98
	e.UndeclaredWrites = 2
	e.ToctouReplayGuard = 0.9
	e.OrphanSubagents = 1
	e.CrashRecoveryRate = 0.8
	e.TimeoutRatio = 0.07
	e.CompensationSuccess = 0.97
	src.e = e
	ws, we := gateWin()
	res, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-2", WindowStart: ws, WindowEnd: we,
		CriteriaVersion: "write-collab-v1",
	})
	if err != nil || res.Outcome != m8core.EvalFail {
		t.Fatalf("evaluate = %+v err=%v, want fail", res, err)
	}
	want := []string{
		m8core.CritWriteInterceptRate, m8core.CritUndeclaredWrites,
		m8core.CritToctouReplayGuard, m8core.CritOrphanSubagents,
		m8core.CritCrashRecovery, m8core.CritTimeoutRatio,
		m8core.CritCompensationSuccess,
	}
	if strings.Join(res.FailedCriteria, ",") != strings.Join(want, ",") {
		t.Fatalf("failedCriteria = %v, want %v", res.FailedCriteria, want)
	}
	st, err := svc.Status(ctx, m8app.StatusInput{SubjectID: "subject-2"})
	if err != nil || st.Capability != m8core.CapabilityDisabled || st.DecisionID != "" {
		t.Fatalf("status after fail = %+v err=%v (zero detail while disabled)", st, err)
	}
	// A passing re-evaluation is allowed (new window key).
	src.e = passingEvidence()
	res2, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-2", WindowStart: ws + 1000, WindowEnd: we + 1000,
		CriteriaVersion: "write-collab-v1",
	})
	if err != nil || res2.Outcome != m8core.EvalPass {
		t.Fatalf("re-evaluation = %+v err=%v, want pass", res2, err)
	}
}

// GT-03 + confirm lifecycle: pass without confirm keeps M8-028 on the
// preflight; expiry and replay answer M8-031 (expired/revoked); a fresh
// token confirms once and re-confirming with the same token is the
// idempotent already-effective read; a wrong token revokes.
func TestGateConfirmSingleUseTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _, clock := openGateService(t, passingEvidence())
	ws, we := gateWin()
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-3", WindowStart: ws, WindowEnd: we,
		CriteriaVersion: "write-collab-v1",
	}); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// Pass without confirm: the preflight refuses (M8-028).
	if err := svc.CheckGate(ctx, "subject-3"); !errors.Is(err, m8app.ErrGateDisabled) {
		t.Fatalf("preflight before confirm err = %v, want ErrGateDisabled", err)
	}
	d, has, err := svc.PendingDecision(ctx, "subject-3")
	if err != nil || !has || d.Action != m8core.DecisionEnable {
		t.Fatalf("pending decision = %+v has=%v err=%v", d, has, err)
	}
	// A wrong token revokes the decision (single-use secret).
	if _, err := svc.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: d.DecisionID, DecisionToken: strings.Repeat("f", 64),
	}); !errors.Is(err, m8app.ErrDecisionTokenInvalid) {
		t.Fatalf("wrong token err = %v, want ErrDecisionTokenInvalid", err)
	}
	if _, err := svc.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: d.DecisionID, DecisionToken: d.DecisionToken,
	}); !errors.Is(err, m8app.ErrDecisionTokenInvalid) {
		t.Fatalf("revoked replay err = %v, want ErrDecisionTokenInvalid", err)
	}
	// New evaluation -> fresh pending decision; let it expire.
	_, has2, err := svc.PendingDecision(ctx, "subject-3")
	if err != nil {
		t.Fatalf("pending decision after revoke: %v", err)
	}
	if has2 {
		t.Fatalf("revoked decision still pending")
	}
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-3", WindowStart: ws + 2000, WindowEnd: we + 2000,
		CriteriaVersion: "write-collab-v1",
	}); err != nil {
		t.Fatalf("evaluate 2: %v", err)
	}
	d2, has2, err := svc.PendingDecision(ctx, "subject-3")
	if err != nil || !has2 {
		t.Fatalf("second pending decision err=%v has=%v", err, has2)
	}
	clock.now = clock.now.Add(25 * time.Hour) // beyond the 24h TTL
	if _, err := svc.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: d2.DecisionID, DecisionToken: d2.DecisionToken,
	}); !errors.Is(err, m8app.ErrDecisionTokenInvalid) {
		t.Fatalf("expired token err = %v, want ErrDecisionTokenInvalid", err)
	}
	// Third evaluation -> confirm within the TTL: capability flips enabled.
	clock.now = clock.now.Add(-25 * time.Hour).Add(time.Hour)
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-3", WindowStart: ws + 3000, WindowEnd: we + 3000,
		CriteriaVersion: "write-collab-v1",
	}); err != nil {
		t.Fatalf("evaluate 3: %v", err)
	}
	d3, has3, err := svc.PendingDecision(ctx, "subject-3")
	if err != nil || !has3 {
		t.Fatalf("third pending decision err=%v has=%v", err, has3)
	}
	confirmed, err := svc.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: d3.DecisionID, DecisionToken: d3.DecisionToken,
	})
	if err != nil || confirmed.Capability != m8core.CapabilityEnabled {
		t.Fatalf("confirm = %+v err=%v, want enabled", confirmed, err)
	}
	if err := svc.CheckGate(ctx, "subject-3"); err != nil {
		t.Fatalf("preflight after confirm: %v", err)
	}
	// Idempotent re-confirm with the same token reads the effective state.
	again, err := svc.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: d3.DecisionID, DecisionToken: d3.DecisionToken,
	})
	if err != nil || again.Capability != m8core.CapabilityEnabled || again.EffectiveAt != confirmed.EffectiveAt {
		t.Fatalf("idempotent re-confirm = %+v err=%v", again, err)
	}
	// Status exposes the binding quartet while enabled.
	st, err := svc.Status(ctx, m8app.StatusInput{SubjectID: "subject-3"})
	if err != nil || st.Capability != m8core.CapabilityEnabled ||
		st.DecisionID != d3.DecisionID || st.EvaluationID == "" ||
		st.PolicyVersion != m8core.WriteCollabPolicyVersion || st.CapabilityDigest == "" {
		t.Fatalf("enabled status = %+v err=%v", st, err)
	}
	// While enabled, an evaluation mints the pending disable decision (the
	// user shutdown keeps the same token path - no free-form off).
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-3", WindowStart: ws + 4000, WindowEnd: we + 4000,
		CriteriaVersion: "write-collab-v1",
	}); err != nil {
		t.Fatalf("evaluate 4: %v", err)
	}
	dd, has4, err := svc.PendingDecision(ctx, "subject-3")
	if err != nil || !has4 || dd.Action != m8core.DecisionDisable {
		t.Fatalf("disable decision = %+v has=%v err=%v", dd, has4, err)
	}
	off, err := svc.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: dd.DecisionID, DecisionToken: dd.DecisionToken,
	})
	if err != nil || off.Capability != m8core.CapabilityDisabled {
		t.Fatalf("disable confirm = %+v err=%v", off, err)
	}
	if err := svc.CheckGate(ctx, "subject-3"); !errors.Is(err, m8app.ErrGateDisabled) {
		t.Fatalf("preflight after disable err = %v, want ErrGateDisabled", err)
	}
}

// GT-04: after a confirmed enable, a runtime policy/digest drift rolls the
// capability back to disabled (M8-032) on the next read; the confirm
// preflight also refuses with the drift code and revokes the decision.
func TestGateBindingDriftRollsBackToDisabled(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m8-gate-drift.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	repo := store.AgentRuntimeRepository()
	src := &fakeEvidence{e: passingEvidence()}
	clock := &fakeClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	svc := m8app.NewCollabGateService(repo, src, m8core.WriteCollabBinding())
	svc.SetClock(clock)

	// Enable under the frozen binding.
	ws, we := gateWin()
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-4", WindowStart: ws, WindowEnd: we,
		CriteriaVersion: "write-collab-v1",
	}); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if err := svc.CheckGate(ctx, "subject-4"); !errors.Is(err, m8app.ErrGateDisabled) {
		t.Fatalf("preflight before confirm err = %v, want ErrGateDisabled", err)
	}
	d, has, err := svc.PendingDecision(ctx, "subject-4")
	if err != nil || !has {
		t.Fatalf("pending decision err=%v has=%v", err, has)
	}
	if _, err := svc.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: d.DecisionID, DecisionToken: d.DecisionToken,
	}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if err := svc.CheckGate(ctx, "subject-4"); err != nil {
		t.Fatalf("preflight after confirm: %v", err)
	}

	// Policy regeneration drifts the runtime binding over the same store.
	svcDrift := m8app.NewCollabGateService(repo, src, m8core.CapabilityBinding{
		PolicyVersion: "write-collab-v2", CapabilityDigest: strings.Repeat("cd", 32),
	})
	svcDrift.SetClock(clock)
	st, err := svcDrift.Status(ctx, m8app.StatusInput{SubjectID: "subject-4"})
	if err != nil || st.Capability != m8core.CapabilityDisabled {
		t.Fatalf("drifted status = %+v err=%v, want rolled back disabled", st, err)
	}
	if err := svcDrift.CheckGate(ctx, "subject-4"); !errors.Is(err, m8app.ErrGateDisabled) {
		t.Fatalf("drifted preflight err = %v, want ErrGateDisabled", err)
	}
	// A decision minted under the old binding is refused + revoked (M8-032).
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-4", WindowStart: ws + 5000, WindowEnd: we + 5000,
		CriteriaVersion: "write-collab-v1",
	}); err != nil {
		t.Fatalf("evaluate under drift: %v", err)
	}
	dd, has2, err := svc.PendingDecision(ctx, "subject-4")
	if err != nil || !has2 {
		t.Fatalf("pending decision under drift err=%v has=%v", err, has2)
	}
	if _, err := svcDrift.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: dd.DecisionID, DecisionToken: dd.DecisionToken,
	}); !errors.Is(err, m8app.ErrGateBindingDrift) {
		t.Fatalf("drifted confirm err = %v, want ErrGateBindingDrift", err)
	}
}

// GT-05: an unreadable evidence source refuses the whole evaluation with
// M8-033 fail-closed and persists zero snapshots.
func TestGateEvidenceUnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	svc, src, _ := openGateService(t, passingEvidence())
	src.err = errors.New("audit store unreadable")
	ws, we := gateWin()
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-5", WindowStart: ws, WindowEnd: we,
		CriteriaVersion: "write-collab-v1",
	}); !errors.Is(err, m8app.ErrEvidenceUnavailable) {
		t.Fatalf("unreadable source err = %v, want ErrEvidenceUnavailable", err)
	}
	// Zero snapshots: the same key evaluates cleanly once the source heals.
	src.err = nil
	res, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: "subject-5", WindowStart: ws, WindowEnd: we,
		CriteriaVersion: "write-collab-v1",
	})
	if err != nil || res.Outcome != m8core.EvalPass {
		t.Fatalf("healed evaluate = %+v err=%v", res, err)
	}
}
