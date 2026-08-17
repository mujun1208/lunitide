// M10 nomination service tests: nominate -> list -> withdraw / mark-decided
// against a fully migrated SQLite store (migration 0071 + 0061 path).
package m8app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func newNominationService(t *testing.T) (*m8app.NominationService, *fakeClock) {
	t.Helper()
	store := openSliceStore(t)
	clock := &fakeClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	mem := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	mem.SetClock(clock)
	svc := m8app.NewNominationService(store.AgentRuntimeRepository(), mem)
	svc.SetClock(clock)
	return svc, clock
}

func TestNominateCreatesWrappedCandidate(t *testing.T) {
	svc, _ := newNominationService(t)
	res, err := svc.Nominate(context.Background(), m8app.NominateInput{
		SubjectID: "user-1",
		Doc:       leafDoc("scope-1", "prefer concise answers", ""),
		Reason:    "repeated preference across three sessions",
		SourceSessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
	})
	if err != nil {
		t.Fatalf("nominate: %v", err)
	}
	if res.Nomination.State != m8core.NomNominated {
		t.Fatalf("state = %s, want nominated", res.Nomination.State)
	}
	if res.ConfirmToken == "" {
		t.Fatal("confirm token missing")
	}
	views, err := svc.ListNominations(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	v := views[0]
	if v.Reason != "repeated preference across three sessions" {
		t.Fatalf("reason = %q", v.Reason)
	}
	if v.Content != "prefer concise answers" {
		t.Fatalf("content = %q", v.Content)
	}
	if v.ConfirmationToken == "" {
		t.Fatal("joined candidate token missing")
	}
}

func TestNominateRejectsBlankReason(t *testing.T) {
	svc, _ := newNominationService(t)
	_, err := svc.Nominate(context.Background(), m8app.NominateInput{
		SubjectID: "user-1",
		Doc:       leafDoc("scope-1", "x", ""),
		Reason:    "",
	})
	if !errors.Is(err, m8app.ErrNominationReasonInvalid) {
		t.Fatalf("err = %v, want ErrNominationReasonInvalid", err)
	}
}

func TestWithdrawThenTerminal(t *testing.T) {
	svc, _ := newNominationService(t)
	res, err := svc.Nominate(context.Background(), m8app.NominateInput{
		SubjectID: "user-1",
		Doc:       leafDoc("scope-1", "likes dark mode", ""),
		Reason:    "stated twice",
	})
	if err != nil {
		t.Fatalf("nominate: %v", err)
	}
	if err := svc.Withdraw(context.Background(), res.Nomination.NominationID, "local-user"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	err = svc.Withdraw(context.Background(), res.Nomination.NominationID, "local-user")
	if !errors.Is(err, m8app.ErrNominationTerminal) {
		t.Fatalf("second withdraw err = %v, want ErrNominationTerminal", err)
	}
}

func TestWithdrawUnknownNomination(t *testing.T) {
	svc, _ := newNominationService(t)
	err := svc.Withdraw(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "local-user")
	if !errors.Is(err, m8app.ErrNominationNotFound) {
		t.Fatalf("err = %v, want ErrNominationNotFound", err)
	}
}

func TestMarkDecidedSettlesAndIgnoresMissing(t *testing.T) {
	svc, _ := newNominationService(t)
	res, err := svc.Nominate(context.Background(), m8app.NominateInput{
		SubjectID: "user-1",
		Doc:       leafDoc("scope-1", "prefers Go examples", ""),
		Reason:    "asked for Go three times",
	})
	if err != nil {
		t.Fatalf("nominate: %v", err)
	}
	if err := svc.MarkDecided(context.Background(), res.CandidateID); err != nil {
		t.Fatalf("mark decided: %v", err)
	}
	// Idempotent on terminal rows.
	if err := svc.MarkDecided(context.Background(), res.CandidateID); err != nil {
		t.Fatalf("re-mark decided: %v", err)
	}
	// Plain candidates (no nomination row) are silently ignored.
	if err := svc.MarkDecided(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatalf("missing candidate mark decided: %v", err)
	}
	views, err := svc.ListNominations(context.Background(), m8core.NomDecided, 10)
	if err != nil {
		t.Fatalf("list decided: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("decided views = %d, want 1", len(views))
	}
}
