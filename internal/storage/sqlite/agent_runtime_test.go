package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

var rtAt = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

const (
	rtProjectULID = "01ARZ3NDEKTSV4RRFFQ69G5F00"
	rtSessionULID = "01ARZ3NDEKTSV4RRFFQ69G5F0A"
	rtRunULID     = "01ARZ3NDEKTSV4RRFFQ69G5F0B"
	rtTurnULID    = "01ARZ3NDEKTSV4RRFFQ69G5F0C"
	rtStepULID    = "01ARZ3NDEKTSV4RRFFQ69G5F0D"
	rtCallULID    = "01ARZ3NDEKTSV4RRFFQ69G5F0E"
	rtObsULID     = "01ARZ3NDEKTSV4RRFFQ69G5F0F"
	rtEffectULID  = "01ARZ3NDEKTSV4RRFFQ69G5F10"
	rtEventULID   = "01ARZ3NDEKTSV4RRFFQ69G5F11"
	rtRegULID     = "01ARZ3NDEKTSV4RRFFQ69G5F12"
	rtGrantULID   = "01ARZ3NDEKTSV4RRFFQ69G5F13"
	rtLeaseULID   = "01ARZ3NDEKTSV4RRFFQ69G5F14"
	rtCsULID      = "01ARZ3NDEKTSV4RRFFQ69G5F15"
	rtJobULID     = "01ARZ3NDEKTSV4RRFFQ69G5F16"
	rtPlanULID    = "01ARZ3NDEKTSV4RRFFQ69G5F17"
	rtEvULID      = "01ARZ3NDEKTSV4RRFFQ69G5F18"
	rtRevULID     = "01ARZ3NDEKTSV4RRFFQ69G5F19"
)

func openRuntimeStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now := rtAt.Format(time.RFC3339Nano)
	if _, err = store.db.Exec(`INSERT INTO projects(id,name,project_code,status,created_at,updated_at,version) VALUES(?,?,?, 'active',?,?,1)`,
		rtProjectULID, "Demo", "ITM00001", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?)`,
		rtSessionULID, rtProjectULID, "S", "active", now, now, 1); err != nil {
		t.Fatal(err)
	}
	return store
}

func rtBudget() agentrun.Budget {
	return agentrun.Budget{
		MaxModelTurns: 8, MaxToolCalls: 32, MaxTokens: 100000, MaxCostMicros: 500000,
		MaxWallClockSeconds: 3600, MaxOutputBytes: 1 << 20, MaxRetries: 3, MaxNoProgress: 5,
		HardCeiling: true,
	}
}

func rtRun() agentrun.AgentRun {
	return agentrun.AgentRun{
		ID: rtRunULID, SessionID: rtSessionULID, Status: agentrun.RunQueued,
		Budget: rtBudget(), Version: 1, CreatedAt: rtAt, UpdatedAt: rtAt,
	}
}

func TestAgentRuntimeRunLifecycleAndCAS(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		if err := tx.PutRun(rtRun()); err != nil {
			return err
		}
		got, err := tx.GetRun(rtRunULID)
		if err != nil || got.Status != agentrun.RunQueued || got.Budget.MaxTokens != 100000 {
			t.Fatalf("get run: %v %+v", err, got)
		}
		// CAS transition queued -> running
		next, err := tx.TransitionRun(rtRunULID, 1, agentrun.RunRunning, rtAt.Add(time.Second))
		if err != nil || next.Status != agentrun.RunRunning || next.Version != 2 {
			t.Fatalf("transition: %v %+v", err, next)
		}
		// stale version conflicts
		if _, err = tx.TransitionRun(rtRunULID, 1, agentrun.RunPausedReview, rtAt); !errors.Is(err, agentrun.ErrVersionConflict) {
			t.Fatalf("stale version accepted: %v", err)
		}
		// illegal transition rejected inside tx (running -> queued)
		if _, err = tx.TransitionRun(rtRunULID, 2, agentrun.RunQueued, rtAt); !errors.Is(err, agentrun.ErrInvalidTransition) {
			t.Fatalf("running->queued accepted: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentRuntimeRunIllegalTransitionRollsBack(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		if err := tx.PutRun(rtRun()); err != nil {
			return err
		}
		// queued -> completed is illegal; error rolls the tx back
		if _, err := tx.TransitionRun(rtRunULID, 1, agentrun.RunCompleted, rtAt); err != nil {
			return err
		}
		return nil
	})
	if !errors.Is(err, agentrun.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
	// rollback: run must not exist
	_ = repo.Transact(ctx, func(tx agentrun.Tx) error {
		if _, err := tx.GetRun(rtRunULID); !errors.Is(err, agentrun.ErrNotFound) {
			t.Fatalf("run survived rollback: %v", err)
		}
		return nil
	})
}

func TestAgentRuntimeTurnStepCallObservation(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		if err := tx.PutRun(rtRun()); err != nil {
			return err
		}
		turn := agentrun.AgentTurn{ID: rtTurnULID, RunID: rtRunULID, TurnNo: 1, Status: agentrun.TurnRunning, Version: 1, CreatedAt: rtAt, UpdatedAt: rtAt}
		if err := tx.PutTurn(turn); err != nil {
			return err
		}
		step := agentrun.AgentStep{ID: rtStepULID, TurnID: rtTurnULID, StepNo: 1, Kind: agentrun.StepTool, Status: agentrun.StepRunning, CreatedAt: rtAt, UpdatedAt: rtAt}
		if err := tx.PutStep(step); err != nil {
			return err
		}
		call := agentrun.ToolCall{ID: rtCallULID, StepID: rtStepULID, ToolName: "fs.read", ArgsDigest: strings.Repeat("a", 64), Status: agentrun.CallProposed, CreatedAt: rtAt, UpdatedAt: rtAt}
		if err := tx.PutToolCall(call); err != nil {
			return err
		}
		obs := agentrun.Observation{ID: rtObsULID, StepID: rtStepULID, Kind: "fs.read.result", ContentDigest: strings.Repeat("b", 64), CapturedAt: rtAt, CreatedAt: rtAt}
		if err := tx.AppendObservation(obs); err != nil {
			return err
		}

		turns, err := tx.ListTurns(rtRunULID)
		if err != nil || len(turns) != 1 || turns[0].TurnNo != 1 {
			t.Fatalf("turns: %v %+v", err, turns)
		}
		steps, err := tx.ListSteps(rtTurnULID)
		if err != nil || len(steps) != 1 || steps[0].Kind != agentrun.StepTool {
			t.Fatalf("steps: %v %+v", err, steps)
		}
		calls, err := tx.ListToolCalls(rtStepULID)
		if err != nil || len(calls) != 1 || calls[0].ToolName != "fs.read" {
			t.Fatalf("calls: %v %+v", err, calls)
		}
		observations, err := tx.ListObservations(rtStepULID)
		if err != nil || len(observations) != 1 {
			t.Fatalf("observations: %v %+v", err, observations)
		}
		// invalid entity rejected before write
		bad := call
		bad.ArgsDigest = "not-hex"
		if err = tx.PutToolCall(bad); !errors.Is(err, agentrun.ErrInvalid) {
			t.Fatalf("invalid call written: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentRuntimeEffectJournalIdempotency(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		if err := tx.PutRun(rtRun()); err != nil {
			return err
		}
		e := agentrun.EffectJournal{
			ID: rtEffectULID, RunID: rtRunULID, EffectKey: "changeset.apply/1",
			RequestDigest: strings.Repeat("c", 64), Status: agentrun.EffectPrepared,
			CreatedAt: rtAt, UpdatedAt: rtAt,
		}
		if err := tx.PutEffect(e); err != nil {
			return err
		}
		got, err := tx.GetEffectByKey("changeset.apply/1")
		if err != nil || got.Status != agentrun.EffectPrepared {
			t.Fatalf("effect by key: %v %+v", err, got)
		}
		// duplicate effect_key under a different id must fail (idempotency)
		dup := e
		dup.ID = rtCallULID
		if err = tx.PutEffect(dup); err == nil {
			t.Fatal("duplicate effect_key accepted")
		}
		return nil
	})
	if err == nil {
		t.Fatal("transaction with duplicate effect_key committed")
	}
}

func TestAgentRuntimeEventsAppendOnlyOrdered(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		if err := tx.PutRun(rtRun()); err != nil {
			return err
		}
		for i, typ := range []string{"AgentRunStartCompleted", "AgentRunGetCompleted"} {
			e := agentrun.RunEvent{
				ID: rtEventULID[:24] + string(rune('A'+i)) + "0", RunID: rtRunULID,
				Sequence: int64(i + 1), EventType: typ, Payload: []byte(`{"k":1}`), CreatedAt: rtAt,
			}
			if err := tx.AppendEvent(e); err != nil {
				return err
			}
		}
		events, err := tx.ListEvents(rtRunULID)
		if err != nil || len(events) != 2 || events[0].Sequence != 1 || events[1].EventType != "AgentRunGetCompleted" {
			t.Fatalf("events: %v %+v", err, events)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// duplicate (run_id, sequence) rejected
	err = repo.Transact(ctx, func(tx agentrun.Tx) error {
		e := agentrun.RunEvent{ID: rtCallULID, RunID: rtRunULID, Sequence: 1, EventType: "X", Payload: []byte(`{}`), CreatedAt: rtAt}
		return tx.AppendEvent(e)
	})
	if err == nil {
		t.Fatal("duplicate run sequence accepted")
	}
}

func TestAgentRuntimeWorkspaceFencing(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		reg := agentrun.WorkspaceRegistration{
			ID: rtRegULID, CanonicalRoot: `E:\ws\demo`, RootDigest: strings.Repeat("d", 64),
			Status: agentrun.RegistrationActive, Version: 1, CreatedAt: rtAt, UpdatedAt: rtAt,
		}
		if err := tx.PutRegistration(reg); err != nil {
			return err
		}
		got, err := tx.GetRegistrationByRoot(`E:\ws\demo`)
		if err != nil || got.RootDigest != reg.RootDigest {
			t.Fatalf("registration: %v %+v", err, got)
		}
		grant := agentrun.WorkspaceGrant{
			ID: rtGrantULID, RegistrationID: rtRegULID, Scope: []byte(`{"paths":["**"],"ops":["read"]}`),
			ExpiresAt: rtAt.Add(time.Hour), Status: agentrun.GrantActive, CreatedAt: rtAt, UpdatedAt: rtAt,
		}
		if err = tx.PutGrant(grant); err != nil {
			return err
		}
		// fencing tokens increase monotonically per grant
		tok1, err := tx.NextFencingToken(rtGrantULID)
		if err != nil || tok1 != 1 {
			t.Fatalf("first token: %v %d", err, tok1)
		}
		lease := agentrun.WorkspaceLease{
			ID: rtLeaseULID, GrantID: rtGrantULID, FencingToken: tok1,
			ExpiresAt: rtAt.Add(time.Minute), Status: agentrun.LeaseActive, CreatedAt: rtAt, UpdatedAt: rtAt,
		}
		if err = tx.PutLease(lease); err != nil {
			return err
		}
		tok2, err := tx.NextFencingToken(rtGrantULID)
		if err != nil || tok2 != 2 {
			t.Fatalf("second token: %v %d", err, tok2)
		}
		gotLease, err := tx.GetLease(rtLeaseULID)
		if err != nil || gotLease.FencingToken != 1 {
			t.Fatalf("lease: %v %+v", err, gotLease)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentRuntimeChangeSetJobPlanEvidenceReview(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		if err := tx.PutRun(rtRun()); err != nil {
			return err
		}
		cs := agentrun.ChangeSet{
			ID: rtCsULID, RunID: rtRunULID, BaseDigest: strings.Repeat("e", 64),
			ApprovalDigest: strings.Repeat("f", 64), Status: agentrun.ChangeSetDraft,
			Version: 1, CreatedAt: rtAt, UpdatedAt: rtAt,
		}
		if err := tx.PutChangeSet(cs); err != nil {
			return err
		}
		got, err := tx.GetChangeSet(rtCsULID)
		if err != nil || got.Status != agentrun.ChangeSetDraft {
			t.Fatalf("changeset: %v %+v", err, got)
		}
		exit := int64(0)
		job := agentrun.CommandJob{
			ID: rtJobULID, RunID: rtRunULID, CommandSpecDigest: strings.Repeat("1", 64),
			Status: agentrun.JobCompleted, ExitCode: &exit, CreatedAt: rtAt, UpdatedAt: rtAt,
		}
		if err = tx.PutCommandJob(job); err != nil {
			return err
		}
		gotJob, err := tx.GetCommandJob(rtJobULID)
		if err != nil || gotJob.ExitCode == nil || *gotJob.ExitCode != 0 {
			t.Fatalf("job: %v %+v", err, gotJob)
		}
		plan := agentrun.RunPlan{
			ID: rtPlanULID, RunID: rtRunULID, PlanDigest: strings.Repeat("2", 64),
			Content: []byte(`{"steps":[]}`), Version: 1, CreatedAt: rtAt, UpdatedAt: rtAt,
		}
		if err = tx.PutRunPlan(plan); err != nil {
			return err
		}
		gotPlan, err := tx.GetRunPlan(rtRunULID)
		if err != nil || gotPlan.PlanDigest != plan.PlanDigest {
			t.Fatalf("plan: %v %+v", err, gotPlan)
		}
		ev := agentrun.Evidence{
			ID: rtEvULID, RunID: rtRunULID, Kind: "web.fetch", SourceURI: "https://example.com/a",
			ContentDigest: strings.Repeat("3", 64), CapturedAt: rtAt, CreatedAt: rtAt,
		}
		if err = tx.AppendEvidence(ev); err != nil {
			return err
		}
		revs, err := tx.ListEvidence(rtRunULID)
		if err != nil || len(revs) != 1 {
			t.Fatalf("evidence: %v %+v", err, revs)
		}
		rev := agentrun.RunReview{
			ID: rtRevULID, RunID: rtRunULID, ApprovalDigest: strings.Repeat("4", 64),
			Decision: agentrun.ReviewApproved, DecidedBy: "owner", DecidedAt: rtAt, CreatedAt: rtAt,
		}
		if err = tx.AppendReview(rev); err != nil {
			return err
		}
		reviews, err := tx.ListReviews(rtRunULID)
		if err != nil || len(reviews) != 1 || reviews[0].Decision != agentrun.ReviewApproved {
			t.Fatalf("reviews: %v %+v", err, reviews)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentRuntimeCascadeDeleteRun(t *testing.T) {
	store := openRuntimeStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()

	if err := repo.Transact(ctx, func(tx agentrun.Tx) error {
		if err := tx.PutRun(rtRun()); err != nil {
			return err
		}
		turn := agentrun.AgentTurn{ID: rtTurnULID, RunID: rtRunULID, TurnNo: 1, Status: agentrun.TurnRunning, Version: 1, CreatedAt: rtAt, UpdatedAt: rtAt}
		return tx.PutTurn(turn)
	}); err != nil {
		t.Fatal(err)
	}
	// session deletion cascades the run and its turns
	if _, err := store.db.Exec(`DELETE FROM sessions WHERE id=?`, rtSessionULID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM agent_run WHERE id=?`, rtRunULID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("run not cascaded: %v n=%d", err, n)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM agent_turn WHERE id=?`, rtTurnULID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("turn not cascaded: %v n=%d", err, n)
	}
}
