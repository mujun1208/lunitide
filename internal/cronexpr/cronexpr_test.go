package cronexpr

import (
	"testing"
	"time"
)

// at builds a UTC instant from a compact datetime literal (optionally with
// seconds).
func at(s string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	panic("bad test literal: " + s)
}

func TestNextStandardShapes(t *testing.T) {
	cases := []struct {
		expr string
		from string
		want string
	}{
		// every minute
		{"* * * * *", "2026-08-16 10:30:15", "2026-08-16 10:31"},
		// fixed hour+minute daily
		{"30 8 * * *", "2026-08-16 10:00", "2026-08-17 08:30"},
		{"30 8 * * *", "2026-08-16 08:30", "2026-08-17 08:30"}, // strictly after
		// step
		{"*/15 * * * *", "2026-08-16 10:07", "2026-08-16 10:15"},
		{"*/15 * * * *", "2026-08-16 10:15", "2026-08-16 10:30"},
		{"0 */6 * * *", "2026-08-16 07:00", "2026-08-16 12:00"},
		// range
		{"0 9-17 * * *", "2026-08-16 17:00", "2026-08-17 09:00"},
		{"30 8 * * 1-5", "2026-08-15 08:29", "2026-08-17 08:30"}, // 8/15,16=weekend
		// list
		{"0 8,18 * * *", "2026-08-16 09:00", "2026-08-16 18:00"},
		// dom/dow OR rule: both restricted, 2026-08-03 is a Monday
		{"0 0 1 * 1", "2026-08-02 00:00", "2026-08-03 00:00"},
		{"0 0 1 * 1", "2026-08-04 00:00", "2026-08-10 00:00"}, // next hit: Mon 8/10
		// month alias + dom
		{"0 0 1 jan *", "2026-08-16 10:00", "2027-01-01 00:00"},
		// dow alias with step
		{"0 9 * * mon-fri", "2026-08-15 10:00", "2026-08-17 09:00"},
		// sunday as 7
		{"0 7 * * 7", "2026-08-15 10:00", "2026-08-16 07:00"},
		// leap day exists in 2028
		{"0 0 29 2 *", "2026-08-16 10:00", "2028-02-29 00:00"},
	}
	for _, c := range cases {
		e, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("parse %q: %v", c.expr, err)
		}
		if got := e.Next(at(c.from)).UTC().Format("2006-01-02 15:04"); got != c.want {
			t.Errorf("Next(%q, %s) = %s, want %s", c.expr, c.from, got, c.want)
		}
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for _, expr := range []string{
		"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *",
		"*/0 * * * *", "5-1 * * * *", "a * * * *", "1,,2 * * * *", "0 0 32 * *", "0 0 1 13 *",
	} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q) accepted", expr)
		}
	}
}

func TestNextUnreachableIsZero(t *testing.T) {
	e, err := Parse("0 0 31 2 *")
	if err != nil {
		t.Fatal(err)
	}
	if !e.Next(at("2026-08-16 10:00")).IsZero() {
		t.Fatal("feb 31 should never fire")
	}
}
