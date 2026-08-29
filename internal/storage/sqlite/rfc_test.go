package sqlite

import (
	"testing"
	"time"
)

func TestRFCLexicographicMatchesChronology(t *testing.T) {
	a := time.Date(2026, 8, 29, 10, 33, 45, 500_000_000, time.UTC)
	b := time.Date(2026, 8, 29, 10, 33, 45, 510_000_000, time.UTC)
	if got, want := rfc(a), "2026-08-29T10:33:45.500000000Z"; got != want {
		t.Fatalf("rfc(a)=%q want %q", got, want)
	}
	if got, want := rfc(b), "2026-08-29T10:33:45.510000000Z"; got != want {
		t.Fatalf("rfc(b)=%q want %q", got, want)
	}
	if rfc(a) >= rfc(b) {
		t.Fatalf("padded stamps must keep 500ms < 510ms, got %q >= %q", rfc(a), rfc(b))
	}
	// The unpadded RFC3339Nano forms are the production CHECK failure mode.
	if a.Format(time.RFC3339Nano) < b.Format(time.RFC3339Nano) {
		t.Fatalf("precondition: unpadded RFC3339Nano should invert .5 vs .51")
	}
}

func TestRFCZeroSentinel(t *testing.T) {
	if got := rfc(time.Time{}); got != "0001-01-01T00:00:00Z" {
		t.Fatalf("zero rfc=%q", got)
	}
	parsed, err := parseRFC(rfc(time.Date(2026, 1, 2, 3, 4, 5, 7, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Nanosecond() != 7 {
		t.Fatalf("roundtrip nanos=%d", parsed.Nanosecond())
	}
}
