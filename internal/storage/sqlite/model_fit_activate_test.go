package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/modelfit"
)

func TestQualificationFixtureCannotPromote(t *testing.T) {
	ctx := context.Background()
	db, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "qualify-activate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC)

	t.Run("fixture_pass_row_cannot_activate", func(t *testing.T) {
		row := modelfit.DefaultQualification("deepseek", "deepseek-chat", modelfit.CodecDeepSeekV1)
		row.Status = modelfit.QualifyFixturePass
		row.Evidence = "offline codec fixture"
		if err := db.PutModelFitQualification(ctx, "sess", row); err != nil {
			t.Fatal(err)
		}

		in := storeLiveActivateInput(now)
		in.OwnerScope = "sess"
		in.Family = "deepseek"
		in.ProviderID = "deepseek"
		in.ModelID = "deepseek-chat"
		in.CodecVersion = modelfit.CodecDeepSeekV1

		got, activateErr := db.ActivateModelFit(ctx, in)
		if activateErr == nil {
			t.Fatalf("fixture_pass must not activate live qualified: %+v", got)
		}
		if !errors.Is(activateErr, modelfit.ErrFixtureCannotPromote) {
			t.Fatalf("err=%v, want ErrFixtureCannotPromote", activateErr)
		}

		saved, err := db.GetModelFitQualification(ctx, "sess", "deepseek", "deepseek-chat", modelfit.CodecDeepSeekV1)
		if err != nil || saved.Status != modelfit.QualifyFixturePass || saved.Adopted {
			t.Fatalf("row must stay fixture_pass and not adopted: %+v %v", saved, err)
		}
		if _, ok := db.GetModelFitActiveBinding("sess", "deepseek", "deepseek-chat"); ok {
			t.Fatal("fixture_pass must not persist a live binding")
		}
	})

	t.Run("mock_evidence_cannot_activate", func(t *testing.T) {
		in := storeLiveActivateInput(now)
		in.OwnerScope = "sess-mock"
		in.EvidenceKind = modelfit.EvidenceMock
		if _, err := db.ActivateModelFit(ctx, in); !errors.Is(err, modelfit.ErrFixtureCannotPromote) {
			t.Fatalf("mock evidence must not activate: %v", err)
		}
	})

	t.Run("incomplete_scope_cannot_activate", func(t *testing.T) {
		in := storeLiveActivateInput(now)
		in.OwnerScope = "sess-scope"
		in.Scope.SuiteID = ""
		if _, err := db.ActivateModelFit(ctx, in); !errors.Is(err, modelfit.ErrScopeIncomplete) {
			t.Fatalf("incomplete scope must not activate: %v", err)
		}
	})

	t.Run("expired_cannot_activate", func(t *testing.T) {
		in := storeLiveActivateInput(now)
		in.OwnerScope = "sess-exp"
		in.ExpiresAt = now.Add(-time.Minute)
		if _, err := db.ActivateModelFit(ctx, in); !errors.Is(err, modelfit.ErrQualificationExpired) {
			t.Fatalf("expired must not activate: %v", err)
		}
	})

	t.Run("source_digest_change_cannot_auto_pass", func(t *testing.T) {
		row := modelfit.DefaultQualification("glm", "glm-5", modelfit.CodecGLMV1)
		row.Status = modelfit.QualifyFixturePass
		changed := modelfit.RevalidateAfterSourceChange(row, strings.Repeat("c", 64), strings.Repeat("d", 64))
		if changed.Status == modelfit.QualifyFixturePass || changed.Status == "qualified" || changed.Adopted {
			t.Fatalf("digest change must not auto-pass: %+v", changed)
		}
		if err := db.PutModelFitQualification(ctx, "sess-digest", changed); err != nil {
			t.Fatal(err)
		}
		saved, err := db.GetModelFitQualification(ctx, "sess-digest", "glm", "glm-5", modelfit.CodecGLMV1)
		if err != nil || saved.Status == "qualified" || saved.Adopted {
			t.Fatalf("0152 must not become qualified: %+v %v", saved, err)
		}

		in := storeLiveActivateInput(now)
		in.OwnerScope = "sess-digest-live"
		in.SourceDigest = strings.Repeat("d", 64)
		if _, err := db.ActivateModelFit(ctx, in); !errors.Is(err, modelfit.ErrSourceDigestChanged) {
			t.Fatalf("digest change must not activate: %v", err)
		}
	})

	t.Run("cas_expected_revision_keeps_previous", func(t *testing.T) {
		seed := storeLiveActivateInput(now)
		seed.OwnerScope = "sess-cas"
		seed.BindingID = "bind-prev"
		first, err := db.ActivateModelFit(ctx, seed)
		if err != nil {
			t.Fatal(err)
		}

		stale := seed
		stale.BindingID = "bind-stale"
		stale.ExpectedRevision = 0
		if _, err = db.ActivateModelFit(ctx, stale); !errors.Is(err, modelfit.ErrBindingConflict) {
			t.Fatalf("stale expectedRevision must conflict: %v", err)
		}
		current, ok := db.GetModelFitActiveBinding(seed.OwnerScope, seed.ProviderID, seed.ModelID)
		if !ok || current.BindingID != "bind-prev" {
			t.Fatalf("previous binding must be kept: %+v ok=%v", current, ok)
		}

		next := seed
		next.BindingID = "bind-next"
		next.ExpectedRevision = first.ActiveBinding.Revision
		won, err := db.ActivateModelFit(ctx, next)
		if err != nil {
			t.Fatal(err)
		}
		if won.PreviousBinding == nil || won.PreviousBinding.BindingID != "bind-prev" {
			t.Fatalf("winner must persist previousBinding: %+v", won)
		}
	})

	t.Run("concurrent_activate_one_winner", func(t *testing.T) {
		seed := storeLiveActivateInput(now)
		seed.OwnerScope = "sess-race"
		seed.BindingID = "bind-seed"
		first, err := db.ActivateModelFit(ctx, seed)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		var wins int
		var winner modelfit.ActivateResult
		for _, id := range []string{"bind-a", "bind-b"} {
			wg.Add(1)
			go func(bindingID string) {
				defer wg.Done()
				in := seed
				in.BindingID = bindingID
				in.ExpectedRevision = first.ActiveBinding.Revision
				got, activateErr := db.ActivateModelFit(ctx, in)
				mu.Lock()
				defer mu.Unlock()
				if activateErr == nil {
					wins++
					winner = got
				} else if !errors.Is(activateErr, modelfit.ErrBindingConflict) {
					t.Errorf("loser err=%v, want conflict", activateErr)
				}
			}(id)
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("concurrent activates: %d winners, want 1", wins)
		}
		if winner.PreviousBinding == nil || winner.PreviousBinding.BindingID != "bind-seed" {
			t.Fatalf("winner must persist previousBinding: %+v", winner)
		}
	})
}

func storeLiveActivateInput(now time.Time) modelfit.ActivateInput {
	scope := modelfit.QualificationScope{
		TargetDigest:  strings.Repeat("a", 64),
		ProfileID:     "glm-standard",
		ProfileDigest: strings.Repeat("b", 64),
		SuiteID:       "upgrade-v1",
		AppVersion:    "0.4.81",
		Renderer:      "libreoffice-24",
	}
	return modelfit.ActivateInput{
		OwnerScope:        "sess-live",
		ProviderID:        "glm",
		Family:            "glm",
		ModelID:           "glm-5",
		CodecVersion:      modelfit.CodecGLMV1,
		BindingID:         "bind-live-1",
		QualificationID:   "qual-live-1",
		ExpectedRevision:  0,
		EvidenceKind:      modelfit.EvidenceLive,
		Scope:             scope,
		BoundScope:        scope,
		ExpiresAt:         now.Add(24 * time.Hour),
		Now:               now,
		SourceDigest:      strings.Repeat("c", 64),
		BoundSourceDigest: strings.Repeat("c", 64),
	}
}
