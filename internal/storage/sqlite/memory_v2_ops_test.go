package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func TestMemoryGenerationBuildListAndDiscard(t *testing.T) {
	store := openRuntimeStore(t)
	ctx := context.Background()
	const subject = "local-user"
	if _, err := store.BuildMemoryGeneration(ctx, "", "user", "", "", "op", "op"); err == nil {
		t.Fatal("missing subject must fail")
	}
	ready, err := store.BuildMemoryGeneration(ctx, subject, "user", "", "", "build-1", "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if ready.State != "ready" || ready.GenerationID == "" || ready.ScopeID != subject {
		t.Fatalf("ready=%+v", ready)
	}
	replay, err := store.BuildMemoryGeneration(ctx, subject, "user", "", "", "build-1", "build-1")
	if err != nil || replay.GenerationID != ready.GenerationID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	listed, next, err := store.ListMemoryGenerations(ctx, subject, "user", subject, "", 10)
	if err != nil || len(listed) != 1 || listed[0].GenerationID != ready.GenerationID || next != "" {
		t.Fatalf("listed=%+v next=%q err=%v", listed, next, err)
	}
	got, err := store.GetMemoryGeneration(ctx, subject, ready.GenerationID)
	if err != nil || got.GenerationID != ready.GenerationID {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	preview, changes, cursor, err := store.PreviewMemoryGeneration(ctx, subject, ready.GenerationID, "", 10)
	if err != nil || preview.GenerationID != ready.GenerationID || len(changes) != 0 || cursor != "" {
		t.Fatalf("preview=%+v changes=%d cursor=%q err=%v", preview, len(changes), cursor, err)
	}
	if _, err := store.DiscardMemoryGeneration(ctx, subject, ready.GenerationID, "", "discard-1", 1); err == nil {
		t.Fatal("discard without an operation id must fail")
	}
	discarded, err := store.DiscardMemoryGeneration(ctx, subject, ready.GenerationID, "discard-1", "discard-1", 1)
	if err != nil || discarded.State != "discarded" {
		t.Fatalf("discarded=%+v err=%v", discarded, err)
	}
}

func TestMemoryGenerationActivateConflictsOnStaleRevision(t *testing.T) {
	store := openRuntimeStore(t)
	ctx := context.Background()
	const subject = "local-user"
	ready, err := store.BuildMemoryGeneration(ctx, subject, "", "", "", "build-a", "build-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActivateMemoryGeneration(ctx, subject, ready.GenerationID, "act", "act", 99); !errors.Is(err, m8core.ErrRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	active, err := store.ActivateMemoryGeneration(ctx, subject, ready.GenerationID, "act", "act", 1)
	if err != nil || active.State != "active" {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	again, err := store.ActivateMemoryGeneration(ctx, subject, ready.GenerationID, "act", "act", 1)
	if err != nil || again.GenerationID != ready.GenerationID {
		t.Fatalf("replay=%+v err=%v", again, err)
	}
	if _, err := store.ActivateMemoryGeneration(ctx, subject, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "other", "other", 1); !errors.Is(err, m8core.ErrGenerationNotFound) {
		t.Fatalf("missing generation: %v", err)
	}
}

func TestMemoryReviewAndPurgeGrant(t *testing.T) {
	store := openRuntimeStore(t)
	ctx := context.Background()
	const subject = "local-user"
	if err := store.PutMemoryReview(ctx, subject, m8core.MemoryReviewWrite{}); err == nil {
		t.Fatal("review without identity must fail")
	}
	err := store.PutMemoryReview(ctx, subject, m8core.MemoryReviewWrite{
		CandidateID: "01ARZ3NDEKTSV4RRFFQ69G5FB1", Kind: "episode", Text: "记住这件事", ReasonCodes: []string{"quiet_review"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListMemoryReviews(ctx, subject, "user", subject, 0)
	if err != nil || len(rows) != 1 || rows[0].Text == "" {
		t.Fatalf("reviews=%+v err=%v", rows, err)
	}
	var seq int64
	if err := store.db.QueryRow(`SELECT COALESCE(MAX(event_seq),0) FROM memory_event_log WHERE subject_id=?`, subject).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareMemoryPurgeGrant(ctx, subject, "user", subject, "purge", "purge", seq+9); !errors.Is(err, m8core.ErrRevisionConflict) {
		t.Fatalf("stale purge revision: %v", err)
	}
	grant, err := store.PrepareMemoryPurgeGrant(ctx, subject, "user", subject, "purge", "purge", seq)
	if err != nil || grant.ConfirmationToken == "" || grant.SnapshotDigest == "" {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
	replay, err := store.PrepareMemoryPurgeGrant(ctx, subject, "user", subject, "purge", "purge", seq)
	if err != nil || !replay.Replay || replay.ConfirmationToken != grant.ConfirmationToken {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
