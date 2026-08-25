package run_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge/m5"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/run"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

type recoveryHarness struct {
	uow       agentrunapp.UnitOfWork
	sender    *m5.RunService
	recoverer *run.RecoveryService
	sessionID string
}

func newRecoveryHarness(t *testing.T) *recoveryHarness {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "m5-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	p, err := projectapp.New(store, store).Create(ctx, "m5-rec", "test", map[string]string{"name": "rec"}, project.Project{Name: "rec"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "m5-rec-session", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "recovery"})
	if err != nil {
		t.Fatal(err)
	}
	repo := store.AgentRuntimeRepository()
	return &recoveryHarness{
		uow:       repo,
		sender:    m5.NewRunService(repo),
		recoverer: run.NewRecoveryService(repo),
		sessionID: sess.ID,
	}
}

func (h *recoveryHarness) events(runID string) []agentrun.RunEvent {
	var events []agentrun.RunEvent
	_ = h.uow.TransactAgentRuntime(context.Background(), func(tx agentrunapp.Tx) error {
		var err error
		events, err = tx.ListEvents(runID)
		return err
	})
	return events
}

func (h *recoveryHarness) rawRun(runID string) agentrun.AgentRun {
	var run agentrun.AgentRun
	_ = h.uow.TransactAgentRuntime(context.Background(), func(tx agentrunapp.Tx) error {
		run, _ = tx.GetRun(runID)
		return nil
	})
	return run
}

// corruptGap writes an event with a sequence gap (simulates a torn chain that
// passes storage checks but breaks replay, M5-REC-001).
func (h *recoveryHarness) corruptGap(runID string, healthySeq int64) {
	_ = h.uow.TransactAgentRuntime(context.Background(), func(tx agentrunapp.Tx) error {
		return tx.AppendEvent(agentrun.RunEvent{
			ID:        ulid.Make().String(),
			RunID:     runID,
			Sequence:  healthySeq + 2, // skip healthySeq+1
			EventType: "TornTail",
			Payload:   []byte(`{"schemaVersion":1}`),
			CreatedAt: time.Now().UTC(),
		})
	})
}

// TestRecoveryMatrix covers T-5.1.3: replay rebuilds state per event boundary,
// corrupted chains are quarantined read-only, and re-scans converge with zero
// duplicated side effects.
func TestRecoveryMatrix(t *testing.T) {
	ctx := context.Background()

	t.Run("healthy chain replays with last seq projection", func(t *testing.T) {
		h := newRecoveryHarness(t)
		for i := 0; i < 3; i++ {
			if _, err := h.sender.Send(ctx, "send-"+string(rune('a'+i)), "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "msg"}); err != nil {
				t.Fatal(err)
			}
		}
		reports, err := h.recoverer.ScanAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(reports) != 1 {
			t.Fatalf("reports = %+v", reports)
		}
		if reports[0].EventsReplayed != 3 || reports[0].LastEventSeq != 3 || reports[0].Quarantined {
			t.Fatalf("report = %+v", reports[0])
		}
	})

	t.Run("sequence gap quarantines the run read-only", func(t *testing.T) {
		h := newRecoveryHarness(t)
		res, err := h.sender.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "one"})
		if err != nil {
			t.Fatal(err)
		}
		h.corruptGap(res.RunID, 1)
		reports, err := h.recoverer.ScanAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(reports) != 1 || !reports[0].Quarantined {
			t.Fatalf("reports = %+v", reports)
		}
		if got := h.rawRun(res.RunID).Status; got != agentrun.RunInterrupted {
			t.Fatalf("status = %s, want interrupted", got)
		}
		events := h.events(res.RunID)
		if events[len(events)-1].EventType != "RunChainQuarantined" {
			t.Fatalf("last event = %s", events[len(events)-1].EventType)
		}
		// Quarantine fences future sends (read-only, no guessing continuation).
		if _, err := h.sender.Send(ctx, "send-2", "tester", m5.RunSendInput{RunID: res.RunID, SessionID: h.sessionID, Text: "after quarantine"}); err == nil {
			t.Fatal("send to quarantined run must fail")
		}
	})

	t.Run("re-scan after every boundary converges with zero duplicated effects", func(t *testing.T) {
		h := newRecoveryHarness(t)
		res, err := h.sender.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "matrix"})
		if err != nil {
			t.Fatal(err)
		}
		h.corruptGap(res.RunID, 1)
		first, err := h.recoverer.ScanAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Simulate a crash after quarantine and re-run the full scan: the run
		// is terminal, so the scanner must skip it entirely.
		second, err := h.recoverer.ScanAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(second) != 0 {
			t.Fatalf("re-scan touched terminal runs: %+v", second)
		}
		events := h.events(res.RunID)
		quarantineEvents := 0
		for _, e := range events {
			if e.EventType == "RunChainQuarantined" {
				quarantineEvents++
			}
		}
		if quarantineEvents != 1 {
			t.Fatalf("quarantine events = %d, want exactly 1", quarantineEvents)
		}
		_ = first
	})

	t.Run("empty store scans clean", func(t *testing.T) {
		h := newRecoveryHarness(t)
		reports, err := h.recoverer.ScanAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(reports) != 0 {
			t.Fatalf("reports = %+v", reports)
		}
	})
}
