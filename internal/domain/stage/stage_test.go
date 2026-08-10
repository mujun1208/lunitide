package stage

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeTitle(t *testing.T) {
	if got, err := NormalizeTitle("  Moon\t Tide  "); err != nil || got != "Moon Tide" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := NormalizeTitle(strings.Repeat("x", 201)); err == nil {
		t.Fatal("accepted oversized raw title")
	}
}

func TestValidate(t *testing.T) {
	now := time.Now().UTC()
	v := Stage{
		ID:        "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Phase:     1,
		Title:     "Requirements",
		Status:    StatusNotStarted,
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
	}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsInvalidPhase(t *testing.T) {
	now := time.Now().UTC()
	base := Stage{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Title: "Requirements", Status: StatusNotStarted,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	for _, phase := range []int{0, 10, -1} {
		v := base
		v.Phase = phase
		if err := v.Validate(); err == nil {
			t.Fatalf("accepted phase %d", phase)
		}
	}
}

func TestValidateAcceptsAllStatuses(t *testing.T) {
	now := time.Now().UTC()
	base := Stage{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Phase: 3, Title: "Architecture",
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	for _, status := range []Status{
		StatusNotStarted, StatusInProgress, StatusWaitingReview, StatusApproved, StatusCompleted,
		StatusRejected, StatusStale, StatusPaused, StatusBlocked, StatusCancelled,
	} {
		v := base
		v.Status = status
		if err := v.Validate(); err != nil {
			t.Fatalf("rejected status %s: %v", status, err)
		}
	}
	v := base
	v.Status = "unknown"
	if err := v.Validate(); err == nil {
		t.Fatal("accepted unknown status")
	}
}

func TestValidateRejectsNonCanonicalULID(t *testing.T) {
	now := time.Now().UTC()
	v := Stage{
		ID: "81ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Phase: 1, Title: "Requirements", Status: StatusNotStarted,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if err := v.Validate(); err == nil {
		t.Fatal("accepted non-canonical ULID")
	}
}

func TestValidateRejectsUnnormalizedTitle(t *testing.T) {
	now := time.Now().UTC()
	v := Stage{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Phase: 1, Title: " Untrimmed ", Status: StatusNotStarted,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if err := v.Validate(); err == nil {
		t.Fatal("accepted unnormalized title")
	}
}

func TestValidateRejectsInvalidLifecycle(t *testing.T) {
	now := time.Now().UTC()
	base := Stage{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Phase: 1, Title: "Requirements", Status: StatusNotStarted,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	v := base
	v.UpdatedAt = now.Add(-time.Hour)
	if err := v.Validate(); err == nil {
		t.Fatal("accepted updated_at before created_at")
	}
	v = base
	v.Version = 0
	if err := v.Validate(); err == nil {
		t.Fatal("accepted version 0")
	}
}
