package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
)

func newSkillCASStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "skill-cas.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedSkillForCAS(t *testing.T, store *Store) skill.Skill {
	t.Helper()
	sk, err := store.CreateSkill(context.Background(), skill.Skill{
		Name: "cas-skill", DisplayName: "CAS Skill", Description: "d", Version: "1.0.0",
		Status: skill.SkillStatusDraft, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin:summarize-input", ManifestJSON: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	return sk
}

// TestUpdateSkillFieldsCASConflict: an update carrying a stale rev touches zero
// rows and surfaces ErrSkillVersionConflict (same-semver lost update), while a
// matching rev updates the row and bumps rev.
func TestUpdateSkillFieldsCASConflict(t *testing.T) {
	store := newSkillCASStore(t)
	sk := seedSkillForCAS(t, store)
	ctx := context.Background()
	// Freshly created skill has rev 0. A stale rev must be rejected.
	if err := store.UpdateSkillFields(ctx, sk.ID, "New", "d", "builtin:summarize-input", "{}", `["read_only"]`, nil, 99); !errors.Is(err, skillapp.ErrSkillVersionConflict) {
		t.Fatalf("stale rev: want ErrSkillVersionConflict, got %v", err)
	}
	if err := store.UpdateSkillFields(ctx, sk.ID, "New", "d", "builtin:summarize-input", "{}", `["read_only"]`, nil, 0); err != nil {
		t.Fatalf("matching rev: %v", err)
	}
	got, err := store.GetSkill(ctx, sk.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "New" || got.Version != "1.0.0" {
		t.Fatalf("post-update = %q/%q, want New/1.0.0", got.DisplayName, got.Version)
	}
	if got.Rev != 1 {
		t.Fatalf("rev not bumped: got %d, want 1", got.Rev)
	}
	// A second update on the now-stale rev 0 must conflict; rev 1 succeeds.
	if err := store.UpdateSkillFields(ctx, sk.ID, "Newer", "d", "builtin:summarize-input", "{}", `["read_only"]`, nil, 0); !errors.Is(err, skillapp.ErrSkillVersionConflict) {
		t.Fatalf("stale rev after bump: want ErrSkillVersionConflict, got %v", err)
	}
	if err := store.UpdateSkillFields(ctx, sk.ID, "Newer", "d", "builtin:summarize-input", "{}", `["read_only"]`, nil, 1); err != nil {
		t.Fatalf("matching rev 1: %v", err)
	}
}

// TestUpdateSkillFieldsCASNotFound: a missing id is reported as not-found, not
// a version conflict.
func TestUpdateSkillFieldsCASNotFound(t *testing.T) {
	store := newSkillCASStore(t)
	err := store.UpdateSkillFields(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "x", "d", "builtin:summarize-input", "{}", `["read_only"]`, nil, 0)
	if err == nil || errors.Is(err, skillapp.ErrSkillVersionConflict) {
		t.Fatalf("missing id: want not-found (non-conflict) error, got %v", err)
	}
}

// TestSkillInvocationPersistenceAndCAS covers the durable invocation lifecycle:
// insert, read back, and the consume CAS where only one caller wins.
func TestSkillInvocationPersistenceAndCAS(t *testing.T) {
	store := newSkillCASStore(t)
	sk := seedSkillForCAS(t, store)
	ctx := context.Background()
	now := time.Now().UTC()
	inv := skillapp.Invocation{
		ID:             "01ARZ3NDEKTSV4RRFFQ69G5FB1",
		SkillID:        sk.ID,
		SkillVersion:   "1.0.0",
		SessionID:      "session-1",
		Input:          "hello",
		InputDigest:    "aa" + repeatHex(62),
		ManifestDigest: "bb" + repeatHex(62),
		Risk:           "low",
		Mode:           "auto",
		ExpiresAt:      now.Add(5 * time.Minute),
	}
	if err := store.InsertSkillInvocation(ctx, inv); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := store.GetSkillInvocation(ctx, inv.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v got=%v", err, got)
	}
	if got.SkillID != sk.ID || got.SessionID != "session-1" || got.Consumed {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	// First consume wins.
	won, err := store.MarkSkillInvocationConsumed(ctx, inv.ID)
	if err != nil || !won {
		t.Fatalf("first consume: won=%v err=%v", won, err)
	}
	// Second consume loses the CAS.
	won, err = store.MarkSkillInvocationConsumed(ctx, inv.ID)
	if err != nil || won {
		t.Fatalf("second consume: won=%v err=%v (want false)", won, err)
	}
	after, err := store.GetSkillInvocation(ctx, inv.ID)
	if err != nil || after == nil || !after.Consumed {
		t.Fatalf("consumed flag not persisted: %+v err=%v", after, err)
	}
}

// TestDeleteExpiredSkillInvocations purges only rows past their TTL.
func TestDeleteExpiredSkillInvocations(t *testing.T) {
	store := newSkillCASStore(t)
	sk := seedSkillForCAS(t, store)
	ctx := context.Background()
	now := time.Now().UTC()
	mk := func(id string, expires time.Time) {
		if err := store.InsertSkillInvocation(ctx, skillapp.Invocation{
			ID: id, SkillID: sk.ID, SkillVersion: "1.0.0", SessionID: "s",
			Input: "i", InputDigest: "aa" + repeatHex(62), ManifestDigest: "bb" + repeatHex(62),
			Risk: "low", Mode: "auto", ExpiresAt: expires,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("01ARZ3NDEKTSV4RRFFQ69G5FB2", now.Add(-time.Minute))
	mk("01ARZ3NDEKTSV4RRFFQ69G5FB3", now.Add(time.Hour))
	n, err := store.DeleteExpiredSkillInvocations(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d, want 1", n)
	}
	if got, _ := store.GetSkillInvocation(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FB2"); got != nil {
		t.Fatal("expired invocation survived purge")
	}
	if got, _ := store.GetSkillInvocation(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FB3"); got == nil {
		t.Fatal("live invocation was purged")
	}
}

func repeatHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}