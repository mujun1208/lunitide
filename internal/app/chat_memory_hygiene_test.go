package app

import (
	"context"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

type hygieneNomStub struct {
	items     []m8app.NominationView
	withdrawn []string
}

func (s *hygieneNomStub) Nominate(context.Context, m8app.NominateInput) (m8app.NominateResult, error) {
	return m8app.NominateResult{}, nil
}
func (s *hygieneNomStub) ListNominations(context.Context, string, int) ([]m8app.NominationView, error) {
	return s.items, nil
}
func (s *hygieneNomStub) ListNominationsFor(context.Context, string, string, int) ([]m8app.NominationView, error) {
	return s.items, nil
}
func (s *hygieneNomStub) Withdraw(context.Context, string, string) error { return nil }
func (s *hygieneNomStub) WithdrawFor(_ context.Context, _, id, _ string) error {
	s.withdrawn = append(s.withdrawn, id)
	return nil
}

func TestMemoryHygieneDedupesSameGistAndNeverConfirms(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	stub := &hygieneNomStub{items: []m8app.NominationView{
		{NominationID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Content: "同一gist", State: m8core.NomNominated, CreatedAt: now},
		{NominationID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Content: "同一gist", State: m8core.NomNominated, CreatedAt: now},
	}}
	got := runMemoryHygieneOn(context.Background(), stub, "local-user")
	if got.Seen != 2 || got.Deduped != 1 {
		t.Fatalf("hygiene=%+v", got)
	}
	if len(stub.withdrawn) != 1 || stub.withdrawn[0] != "01ARZ3NDEKTSV4RRFFQ69G5FAW" {
		t.Fatalf("withdrawn=%v", stub.withdrawn)
	}
}

func TestNoteEngineActivityRunsHygieneAfterIdle(t *testing.T) {
	hygieneMu.Lock()
	lastEngineActivity = time.Now().Add(-memoryHygieneIdleAfter - time.Second)
	hygieneLastRun = time.Time{}
	hygieneMu.Unlock()
	e := &Engine{}
	e.noteEngineActivityAndMaybeHygiene(context.Background())
	e.noteEngineActivityAndMaybeHygiene(context.Background())
}

func TestFlushMemoryBeforeCompactionIsFailOpen(t *testing.T) {
	e := &Engine{}
	e.flushMemoryBeforeCompaction(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "记住默认中文", "好的")
}
