package m8app_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// Auto-accepting a low-risk candidate that arrived via the nomination
// workflow must also settle the nomination (as the human confirm handler
// does), leaving no dangling pending nomination.
func TestAutoAcceptSettlesNomination(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "m8-autoaccept-nom.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clock := &fakeClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	repo := store.AgentRuntimeRepository()
	mem := m8app.NewMemoryService(repo, "local-user")
	mem.SetClock(clock)
	nom := m8app.NewNominationService(repo, mem)

	res, err := nom.Nominate(ctx, m8app.NominateInput{
		SubjectID: "local-user",
		Doc:       leafDoc("subject:local-user", "喜欢深色主题", ""),
		Reason:    "本轮对话自动提名",
		Nominator: "chat.auto",
		Actor:     "engine",
	})
	if err != nil {
		t.Fatalf("nominate: %v", err)
	}
	pending, err := nom.ListNominations(ctx, m8core.NomNominated, 50)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending nominations before accept = %d err=%v", len(pending), err)
	}

	acc, err := mem.AutoAcceptCandidate(ctx, res.CandidateID, "chat.auto")
	if err != nil || !acc.Accepted {
		t.Fatalf("auto-accept = %+v err=%v", acc, err)
	}
	// This is the engine-side settlement the hook performs.
	if err := nom.MarkDecided(ctx, res.CandidateID); err != nil {
		t.Fatalf("mark decided: %v", err)
	}

	stillPending, err := nom.ListNominations(ctx, m8core.NomNominated, 50)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(stillPending) != 0 {
		t.Fatalf("dangling pending nominations after auto-accept: %d", len(stillPending))
	}
	decided, err := nom.ListNominations(ctx, m8core.NomDecided, 50)
	if err != nil || len(decided) != 1 {
		t.Fatalf("decided nominations = %d err=%v", len(decided), err)
	}
}
