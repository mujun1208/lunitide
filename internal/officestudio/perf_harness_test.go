package officestudio

import (
	"strings"
	"testing"
)

func TestMeasureOfficeFixturesPrintsBaseline(t *testing.T) {
	t.Setenv("LUNITIDE_TYPST", "")
	samples := MeasureOfficeFixtures(1)
	if len(samples) == 0 {
		t.Fatal("expected timed samples")
	}
	for _, s := range samples {
		t.Logf("office-perf fixture=%s kind=%s ms=%d skipped=%v reason=%s", s.Fixture, s.Kind, s.ElapsedMS, s.Skipped, s.Reason)
		if s.Skipped && s.Reason == "" {
			t.Fatalf("skipped sample missing reason: %#v", s)
		}
	}
	sum := SummarizePerf(samples)
	t.Logf("office-perf summary ready=%v n=%d p50=%d p95=%d (not a frozen gate)", sum.Ready, sum.N, sum.P50MS, sum.P95MS)
	if sum.Ready {
		t.Fatal("single-run baseline must not claim a frozen P50")
	}
	var sawPatch bool
	for _, s := range samples {
		if strings.HasSuffix(s.Fixture, "/patch") && !s.Skipped {
			sawPatch = true
		}
	}
	if !sawPatch {
		t.Fatal("expected a timed text patch sample")
	}
}
