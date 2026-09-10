package config

import "testing"

func TestTokenEfficiencyProcessRollout(t *testing.T) {
	for _, value := range []string{"", "on", "1", "true", "invalid"} {
		t.Setenv("LUNITIDE_TOKEN_EFFICIENCY", value)
		if !TokenEfficiencyEnabled() {
			t.Fatalf("unexpected disable for %q", value)
		}
	}
	for _, value := range []string{"0", " false ", "OFF", "no"} {
		t.Setenv("LUNITIDE_TOKEN_EFFICIENCY", value)
		if TokenEfficiencyEnabled() {
			t.Fatalf("rollback ignored for %q", value)
		}
	}
}

func TestTokenEfficiencyScopeDoesNotClaimSummariesOrCaches(t *testing.T) {
	t.Setenv("LUNITIDE_TOKEN_EFFICIENCY", "on")
	on := CurrentTokenEfficiencyScope()
	if !on.RequestJSONCompact || !on.EvidenceDedup {
		t.Fatalf("enabled trim missing: %+v", on)
	}
	if on.IndependentSummary || on.ProviderCache || on.Metering || on.AppliesTo != "process_restart" {
		t.Fatalf("enabled scope leaked: %+v", on)
	}
	t.Setenv("LUNITIDE_TOKEN_EFFICIENCY", "off")
	off := CurrentTokenEfficiencyScope()
	if off.RequestJSONCompact || off.EvidenceDedup {
		t.Fatalf("disabled trim still on: %+v", off)
	}
	if off.IndependentSummary || off.ProviderCache || off.Metering || off.AppliesTo != "process_restart" {
		t.Fatalf("closing trim must not close summaries, caches, or metering: %+v", off)
	}
}
