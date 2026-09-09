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
