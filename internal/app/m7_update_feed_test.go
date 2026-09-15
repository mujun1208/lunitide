package app

import (
	"context"
	"strings"
	"testing"
)

func TestCheckAdoptsLocalFeedWhenLedgerEmpty(t *testing.T) {
	h := newUpdateHarness(t)
	digest := strings.Repeat("ab", 32)
	h.svc.SetFeedLookup(func(channel string) (string, string, bool, error) {
		if channel != "stable" {
			t.Fatalf("channel %q", channel)
		}
		return "0.4.84", digest, true, nil
	})
	got, err := h.svc.Check(context.Background(), "stable", "0.4.81")
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdateID == "" || got.Version != "0.4.84" || got.Digest != digest || got.Mandatory {
		t.Fatalf("check = %+v", got)
	}
	again, err := h.svc.Check(context.Background(), "stable", "0.4.81")
	if err != nil {
		t.Fatal(err)
	}
	if again.UpdateID != got.UpdateID {
		t.Fatalf("adopt must be stable, %q vs %q", again.UpdateID, got.UpdateID)
	}
	current, err := h.svc.Check(context.Background(), "stable", "0.4.84")
	if err != nil {
		t.Fatal(err)
	}
	if current.UpdateID != "" {
		t.Fatalf("same version must be current, got %+v", current)
	}
}
