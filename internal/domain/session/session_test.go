package session

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeAndValidate(t *testing.T) {
	if got, err := NormalizeTitle("  Moon\t Tide  "); err != nil || got != "Moon Tide" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := NormalizeTitle(strings.Repeat("😀", 201)); err == nil {
		t.Fatal("accepted oversized raw title")
	}
	now := time.Now().UTC()
	v := Session{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", Title: "Moon Tide", Status: StatusActive, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	v.Status = "archived"
	if err := v.Validate(); err == nil {
		t.Fatal("accepted non-active status")
	}
}
