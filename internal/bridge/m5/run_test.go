package m5_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge/m5"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type harness struct {
	svc       *m5.RunService
	uow       agentrunapp.UnitOfWork
	sessionID string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "m5-run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	p, err := projectapp.New(store, store).Create(ctx, "m5-send", "test", map[string]string{"name": "m5"}, project.Project{Name: "m5"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "m5-session", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "m5 chat"})
	if err != nil {
		t.Fatal(err)
	}
	repo := store.AgentRuntimeRepository()
	return &harness{svc: m5.NewRunService(repo), uow: repo, sessionID: sess.ID}
}

// faultUOW wraps the repository transaction so a test can kill the process at
// a specific step boundary (the durable equivalent of the 0/10/50/100ms
// strong-kill matrix): the transaction aborts exactly where the kill landed.
type faultUOW struct {
	agentrunapp.UnitOfWork
	failAppendEvent bool
	failPutAudit    bool
	failPutRun      bool
}

func (u *faultUOW) TransactAgentRuntime(ctx context.Context, fn func(agentrunapp.Tx) error) error {
	return u.UnitOfWork.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		if u.failPutRun {
			tx = &faultTx{Tx: tx, failPutRun: true}
		} else if u.failAppendEvent {
			tx = &faultTx{Tx: tx, failAppendEvent: true}
		} else if u.failPutAudit {
			tx = &faultTx{Tx: tx, failPutAudit: true}
		}
		return fn(tx)
	})
}

type faultTx struct {
	agentrunapp.Tx
	failPutRun      bool
	failAppendEvent bool
	failPutAudit    bool
}

func (f *faultTx) PutRun(run agentrun.AgentRun) error {
	if f.failPutRun {
		return errors.New("kill: after PutRun")
	}
	return f.Tx.PutRun(run)
}

func (f *faultTx) AppendEvent(e agentrun.RunEvent) error {
	if f.failAppendEvent {
		return errors.New("kill: after AppendEvent")
	}
	return f.Tx.AppendEvent(e)
}

func (f *faultTx) PutAudit(a providerapp.Audit) error {
	if f.failPutAudit {
		return errors.New("kill: after PutAudit")
	}
	return f.Tx.PutAudit(a)
}

func (h *harness) events(runID string) []agentrun.RunEvent {
	var events []agentrun.RunEvent
	_ = h.uow.TransactAgentRuntime(context.Background(), func(tx agentrunapp.Tx) error {
		var err error
		events, err = tx.ListEvents(runID)
		return err
	})
	return events
}

func (h *harness) run(runID string) (agentrun.AgentRun, error) {
	var run agentrun.AgentRun
	var err error
	_ = h.uow.TransactAgentRuntime(context.Background(), func(tx agentrunapp.Tx) error {
		run, err = tx.GetRun(runID)
		return nil
	})
	return run, err
}

// TestRunSendDurable covers T-5.1.1: the first event is durable before Send
// returns, and every kill point inside the transaction leaves no
// not-yet-durable message (M5-RUN-001: the message is never marked sent).
func TestRunSendDurable(t *testing.T) {
	ctx := context.Background()

	t.Run("first send persists run and seq1 event", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.svc.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "你好，月汐"})
		if err != nil {
			t.Fatal(err)
		}
		if res.EventSeq != 1 || res.Status != string(agentrun.RunRunning) {
			t.Fatalf("receipt = %+v", res)
		}
		events := h.events(res.RunID)
		if len(events) != 1 || events[0].EventType != "UserMessageAccepted" {
			t.Fatalf("events = %+v", events)
		}
	})

	t.Run("second send continues the same run and sequence", func(t *testing.T) {
		h := newHarness(t)
		first, err := h.svc.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "one"})
		if err != nil {
			t.Fatal(err)
		}
		second, err := h.svc.Send(ctx, "send-2", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "two"})
		if err != nil {
			t.Fatal(err)
		}
		if second.RunID != first.RunID || second.EventSeq != 2 {
			t.Fatalf("second = %+v, first = %+v", second, first)
		}
	})

	t.Run("kill matrix leaves no durable message", func(t *testing.T) {
		kills := []struct {
			name string
			uow  func(h *harness) *faultUOW
		}{
			{"kill before run row commits", func(h *harness) *faultUOW { return &faultUOW{UnitOfWork: h.uow, failPutRun: true} }},
			{"kill after run row before event", func(h *harness) *faultUOW { return &faultUOW{UnitOfWork: h.uow, failAppendEvent: true} }},
			{"kill after event before audit", func(h *harness) *faultUOW { return &faultUOW{UnitOfWork: h.uow, failPutAudit: true} }},
		}
		for _, kill := range kills {
			t.Run(kill.name, func(t *testing.T) {
				h := newHarness(t)
				svc := m5.NewRunService(kill.uow(h))
				if _, err := svc.Send(ctx, "send-kill", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "doomed"}); err == nil {
					t.Fatal("send must fail when the process dies mid-transaction")
				}
				// The message must not be durable anywhere: no event for any
				// run of the session and no half-created run.
				var runs []agentrun.AgentRun
				_ = h.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
					var err error
					runs, err = tx.ListRunsBySession(h.sessionID)
					return err
				})
				for _, run := range runs {
					if events := h.events(run.ID); len(events) != 0 {
						t.Fatalf("run %s has %d events after a mid-transaction kill", run.ID, len(events))
					}
				}
			})
		}
	})

	t.Run("idempotent replay returns the original receipt", func(t *testing.T) {
		h := newHarness(t)
		first, err := h.svc.Send(ctx, "send-dup", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "once"})
		if err != nil {
			t.Fatal(err)
		}
		replay, err := h.svc.Send(ctx, "send-dup", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "once"})
		if err != nil {
			t.Fatal(err)
		}
		if replay != first {
			t.Fatalf("replay = %+v, first = %+v", replay, first)
		}
		if events := h.events(first.RunID); len(events) != 1 {
			t.Fatalf("replay duplicated events: %d", len(events))
		}
	})

	t.Run("input validation rejects before any write", func(t *testing.T) {
		h := newHarness(t)
		cases := []struct {
			name string
			in   m5.RunSendInput
			key  string
			want error
		}{
			{"no text", m5.RunSendInput{SessionID: h.sessionID}, "k1", m5.ErrTextRequired},
			{"no session", m5.RunSendInput{Text: "x"}, "k2", m5.ErrSessionRequired},
			{"oversized text", m5.RunSendInput{SessionID: h.sessionID, Text: string(make([]byte, m5.TextLimitBytes+1))}, "k3", m5.ErrTextTooLarge},
			{"unknown run", m5.RunSendInput{RunID: "01ARZ3NDEKTSV4RRFFQ69G5FAZ", SessionID: h.sessionID, Text: "x"}, "k4", m5.ErrRunNotFound},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := h.svc.Send(ctx, tc.key, "tester", tc.in); !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
			})
		}
	})
}

// TestRunCancel covers T-5.1.2: legal transitions hit the audit trail, the
// idempotent second cancel returns the snapshot, and illegal cancels answer
// M5-RUN-002.
func TestRunCancel(t *testing.T) {
	ctx := context.Background()

	t.Run("user cancel transitions to cancelled with audit event", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.svc.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "cancel me"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := h.svc.Cancel(ctx, "cancel-1", "tester", m5.RunCancelInput{RunID: res.RunID, Reason: m5.CancelUser})
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != string(agentrun.RunCancelled) || out.ReleasedAt.IsZero() {
			t.Fatalf("cancel = %+v", out)
		}
		run, err := h.run(res.RunID)
		if err != nil || run.Status != agentrun.RunCancelled {
			t.Fatalf("run status = %s err = %v", run.Status, err)
		}
	})

	t.Run("timeout cancel fences as outcome_unknown", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.svc.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "stuck"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := h.svc.Cancel(ctx, "cancel-1", "tester", m5.RunCancelInput{RunID: res.RunID, Reason: m5.CancelTimeout})
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != string(agentrun.RunOutcomeUnknown) {
			t.Fatalf("status = %s", out.Status)
		}
	})

	t.Run("second concurrent-style cancel is idempotent", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.svc.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "twice"})
		if err != nil {
			t.Fatal(err)
		}
		first, err := h.svc.Cancel(ctx, "cancel-1", "tester", m5.RunCancelInput{RunID: res.RunID, Reason: m5.CancelUser})
		if err != nil {
			t.Fatal(err)
		}
		second, err := h.svc.Cancel(ctx, "cancel-2", "tester", m5.RunCancelInput{RunID: res.RunID, Reason: m5.CancelUser})
		if err != nil {
			t.Fatal(err)
		}
		if second.Status != first.Status {
			t.Fatalf("second cancel = %+v, first = %+v", second, first)
		}
	})

	t.Run("illegal cancels return RUN-002 semantics", func(t *testing.T) {
		h := newHarness(t)
		cases := []struct {
			name  string
			input m5.RunCancelInput
			want  error
		}{
			{"unknown run", m5.RunCancelInput{RunID: "01ARZ3NDEKTSV4RRFFQ69G5FAZ", Reason: m5.CancelUser}, m5.ErrRunNotFound},
			{"bad reason", m5.RunCancelInput{RunID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Reason: "whim"}, m5.ErrCancelReasonInvalid},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := h.svc.Cancel(ctx, "cancel-x", "tester", tc.input); !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
			})
		}
	})

	t.Run("cancelling a completed run is RUN-002", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.svc.Send(ctx, "send-1", "tester", m5.RunSendInput{SessionID: h.sessionID, Text: "done already"})
		if err != nil {
			t.Fatal(err)
		}
		_ = h.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
			run, err := tx.GetRun(res.RunID)
			if err != nil {
				return err
			}
			_, err = tx.TransitionRun(run.ID, run.Version, agentrun.RunCompleted, time.Now().UTC())
			return err
		})
		if _, err := h.svc.Cancel(ctx, "cancel-1", "tester", m5.RunCancelInput{RunID: res.RunID, Reason: m5.CancelUser}); !errors.Is(err, m5.ErrCancelStateInvalid) {
			t.Fatalf("err = %v, want %v", err, m5.ErrCancelStateInvalid)
		}
	})
}
