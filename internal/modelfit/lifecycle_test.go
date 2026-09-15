package modelfit

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActivationCASAndRollback(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	ledger := NewBindingLedger()
	before := InFlightTask{
		TaskID:   "inflight-task-fr16",
		Budget:   []byte(`{"maxTotalTokens":1000,"consumed":120}`),
		Progress: []byte(`{"step":"write-report","passed":1}`),
	}

	seed := liveActivateInput(now)
	seed.BindingID = "bind-prev"
	seed.IdempotencyKey = "adopt-1"
	seed.InFlight = before
	first, err := ledger.Activate(seed)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var wins int
	for _, id := range []string{"bind-a", "bind-b"} {
		wg.Add(1)
		go func(bindingID string) {
			defer wg.Done()
			in := liveActivateInput(now)
			in.BindingID = bindingID
			in.ExpectedRevision = first.ActiveBinding.Revision
			in.IdempotencyKey = "race-" + bindingID
			in.InFlight = InFlightTask{TaskID: "reset-me", Budget: []byte(`{"consumed":0}`), Progress: []byte(`{"step":""}`)}
			got, activateErr := ledger.Activate(in)
			mu.Lock()
			defer mu.Unlock()
			if activateErr == nil {
				wins++
				_ = got
			} else if !errors.Is(activateErr, ErrBindingConflict) {
				t.Errorf("loser err=%v, want conflict", activateErr)
			}
		}(id)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent activate winners=%d, want 1", wins)
	}

	current, ok := ledger.Get(seed.OwnerScope, seed.ProviderID, seed.ModelID)
	if !ok {
		t.Fatal("active binding missing after race")
	}
	replay := liveActivateInput(now)
	replay.BindingID = "bind-dup"
	replay.ExpectedRevision = current.Revision
	replay.IdempotencyKey = "adopt-1"
	dup, err := ledger.Activate(replay)
	if err != nil {
		t.Fatal(err)
	}
	if dup.ActiveBinding.Revision != first.ActiveBinding.Revision || dup.ActiveBinding.BindingID != first.ActiveBinding.BindingID {
		t.Fatalf("repeat idempotencyKey minted a new revision: %+v vs %+v", dup.ActiveBinding, first.ActiveBinding)
	}

	rolled, err := ledger.Rollback(ActivateInput{
		OwnerScope: seed.OwnerScope, ProviderID: seed.ProviderID, ModelID: seed.ModelID,
		ExpectedRevision: current.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rolled.ActiveBinding.BindingID != "bind-prev" {
		t.Fatalf("rollback must restore previous binding: %+v", rolled.ActiveBinding)
	}
	if rolled.PreviousBinding == nil || rolled.PreviousBinding.BindingID == "" {
		t.Fatal("rollback must keep history")
	}

	empty := NewBindingLedger()
	if _, err = empty.Rollback(ActivateInput{OwnerScope: "x", ProviderID: "glm", ModelID: "glm-5", ExpectedRevision: 0}); !errors.Is(err, ErrBindingUnavailable) {
		t.Fatalf("no previous must be unavailable: %v", err)
	}

	if !sameInFlight(first.InFlight, before) {
		t.Fatalf("activate dropped in-flight task: %+v", first.InFlight)
	}
	rolledFlight := rolled.InFlight
	if !sameInFlight(rolledFlight, before) {
		t.Fatalf("rollback changed in-flight task: %+v", rolledFlight)
	}
	stored, ok := ledger.GetInFlight(seed.OwnerScope, seed.ProviderID, seed.ModelID)
	if !ok || !sameInFlight(stored, before) {
		t.Fatalf("ledger in-flight after rollback: %+v ok=%v", stored, ok)
	}
	after, ok := ledger.Get(seed.OwnerScope, seed.ProviderID, seed.ModelID)
	if !ok || after.BindingID != "bind-prev" {
		t.Fatalf("in-flight slot changed unexpectedly: %+v", after)
	}
}

func sameInFlight(got, want InFlightTask) bool {
	return got.TaskID == want.TaskID && string(got.Budget) == string(want.Budget) && string(got.Progress) == string(want.Progress)
}

func TestQualificationFixtureCannotPromote(t *testing.T) {
	now := time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC)

	t.Run("fixture_pass_row_cannot_activate", func(t *testing.T) {
		ledger := NewBindingLedger()
		in := liveActivateInput(now)
		row := DefaultQualification(in.Family, in.ModelID, in.CodecVersion)
		row.Status = QualifyFixturePass
		row.Evidence = "offline codec fixture"
		in.FixtureRow = &row

		got, err := ledger.Activate(in)
		if err == nil {
			t.Fatalf("fixture_pass must not activate live: %+v", got)
		}
		if !errors.Is(err, ErrFixtureCannotPromote) {
			t.Fatalf("err=%v, want ErrFixtureCannotPromote", err)
		}
		if _, ok := ledger.Get(in.OwnerScope, in.ProviderID, in.ModelID); ok {
			t.Fatal("fixture_pass must not persist a live binding")
		}
		if row.Status != QualifyFixturePass || row.Adopted {
			t.Fatalf("0152 row must stay fixture_pass and not adopted: %+v", row)
		}
		view := PresentModelFit(FitActivated, "fixture", QualifyFixturePass)
		if view.LiveQualified {
			t.Fatalf("fixture source must not present live qualified: %+v", view)
		}
	})

	t.Run("mock_evidence_cannot_activate", func(t *testing.T) {
		ledger := NewBindingLedger()
		in := liveActivateInput(now)
		in.EvidenceKind = EvidenceMock
		if _, err := ledger.Activate(in); !errors.Is(err, ErrFixtureCannotPromote) {
			t.Fatalf("mock evidence must not activate live: %v", err)
		}
	})

	t.Run("incomplete_scope_cannot_activate", func(t *testing.T) {
		ledger := NewBindingLedger()
		in := liveActivateInput(now)
		in.Scope.Renderer = ""
		if _, err := ledger.Activate(in); !errors.Is(err, ErrScopeIncomplete) {
			t.Fatalf("incomplete scope must not activate: %v", err)
		}
	})

	t.Run("expired_cannot_activate", func(t *testing.T) {
		ledger := NewBindingLedger()
		in := liveActivateInput(now)
		in.ExpiresAt = now.Add(-time.Second)
		if _, err := ledger.Activate(in); !errors.Is(err, ErrQualificationExpired) {
			t.Fatalf("expired qualification must not activate: %v", err)
		}
	})

	t.Run("source_digest_change_cannot_auto_pass", func(t *testing.T) {
		row := DefaultQualification("glm", "glm-5", CodecGLMV1)
		row.Status = QualifyFixturePass
		changed := RevalidateAfterSourceChange(row, strings.Repeat("c", 64), strings.Repeat("d", 64))
		if changed.Status == QualifyFixturePass || changed.Status == "qualified" || changed.Adopted {
			t.Fatalf("digest change must not auto-pass: %+v", changed)
		}

		ledger := NewBindingLedger()
		in := liveActivateInput(now)
		in.SourceDigest = strings.Repeat("d", 64)
		if _, err := ledger.Activate(in); !errors.Is(err, ErrSourceDigestChanged) {
			t.Fatalf("digest change must not activate: %v", err)
		}
	})

	t.Run("cas_expected_revision_keeps_previous", func(t *testing.T) {
		ledger := NewBindingLedger()
		seed := liveActivateInput(now)
		seed.BindingID = "bind-prev"
		first, err := ledger.Activate(seed)
		if err != nil {
			t.Fatal(err)
		}

		stale := liveActivateInput(now)
		stale.BindingID = "bind-stale"
		stale.ExpectedRevision = 0
		if _, err = ledger.Activate(stale); !errors.Is(err, ErrBindingConflict) {
			t.Fatalf("stale expectedRevision must conflict: %v", err)
		}
		current, ok := ledger.Get(seed.OwnerScope, seed.ProviderID, seed.ModelID)
		if !ok || current.BindingID != "bind-prev" {
			t.Fatalf("previous binding must be kept: %+v ok=%v", current, ok)
		}

		next := liveActivateInput(now)
		next.BindingID = "bind-next"
		next.ExpectedRevision = first.ActiveBinding.Revision
		won, err := ledger.Activate(next)
		if err != nil {
			t.Fatal(err)
		}
		if won.PreviousBinding == nil || won.PreviousBinding.BindingID != "bind-prev" {
			t.Fatalf("winner must persist previousBinding: %+v", won)
		}
		if won.ActiveBinding.PreviousBindingID != "bind-prev" {
			t.Fatalf("active previousBindingId=%q", won.ActiveBinding.PreviousBindingID)
		}
	})

	t.Run("concurrent_activate_one_winner", func(t *testing.T) {
		ledger := NewBindingLedger()
		seed := liveActivateInput(now)
		seed.BindingID = "bind-seed"
		first, err := ledger.Activate(seed)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		var wins int
		var winner ActivateResult
		for _, id := range []string{"bind-a", "bind-b"} {
			wg.Add(1)
			go func(bindingID string) {
				defer wg.Done()
				in := liveActivateInput(now)
				in.BindingID = bindingID
				in.ExpectedRevision = first.ActiveBinding.Revision
				got, activateErr := ledger.Activate(in)
				mu.Lock()
				defer mu.Unlock()
				if activateErr == nil {
					wins++
					winner = got
				} else if !errors.Is(activateErr, ErrBindingConflict) {
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

func liveActivateInput(now time.Time) ActivateInput {
	scope := QualificationScope{
		TargetDigest:  strings.Repeat("a", 64),
		ProfileID:     "glm-standard",
		ProfileDigest: strings.Repeat("b", 64),
		SuiteID:       "upgrade-v1",
		AppVersion:    "0.4.81",
		Renderer:      "libreoffice-24",
	}
	return ActivateInput{
		OwnerScope:        "sess",
		ProviderID:        "glm",
		Family:            "glm",
		ModelID:           "glm-5",
		CodecVersion:      CodecGLMV1,
		BindingID:         "bind-live-1",
		QualificationID:   "qual-live-1",
		ExpectedRevision:  0,
		EvidenceKind:      EvidenceLive,
		Scope:             scope,
		BoundScope:        scope,
		ExpiresAt:         now.Add(24 * time.Hour),
		Now:               now,
		SourceDigest:      strings.Repeat("c", 64),
		BoundSourceDigest: strings.Repeat("c", 64),
	}
}
