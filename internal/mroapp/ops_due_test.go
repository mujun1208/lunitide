package mroapp

import (
	"testing"
	"time"
)

func TestEvaluateDueMissingIsNotZero(t *testing.T) {
	today := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	got := EvaluateDue(DueItem{Kind: "FH", LimitValue: 100, UsedMissing: true}, today)
	if got.State != DueStateMissing || got.Label != DueLabelMissing || got.Remaining != 0 {
		t.Fatalf("missing used = %+v", got)
	}
	cal := EvaluateDue(DueItem{Kind: "CAL"}, today)
	if cal.State != DueStateMissing || cal.Label != DueLabelMissing {
		t.Fatalf("missing due_at = %+v", cal)
	}
}

func TestEvaluateDueUsageAndCalendar(t *testing.T) {
	today := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	ok := EvaluateDue(DueItem{Kind: "FH", LimitValue: 100, UsedValue: 40}, today)
	if ok.State != DueStateOK || ok.Remaining != 60 {
		t.Fatalf("ok = %+v", ok)
	}
	over := EvaluateDue(DueItem{Kind: "FH", LimitValue: 100, UsedValue: 110}, today)
	if over.State != DueStateOverdue {
		t.Fatalf("overdue = %+v", over)
	}
	due := EvaluateDue(DueItem{Kind: "CAL", DueAt: "2026-09-04"}, today)
	if due.State != DueStateDue || due.DaysLeft != 0 {
		t.Fatalf("due today = %+v", due)
	}
	late := EvaluateDue(DueItem{Kind: "AD", DueAt: "2026-09-01"}, today)
	if late.State != DueStateOverdue || late.DaysLeft != -3 {
		t.Fatalf("late = %+v", late)
	}
	// Upsert always stores UsedMissing=true until utilization is recorded.
	// Calendar items with a due_at must still evaluate against the date.
	calMissingUsed := EvaluateDue(DueItem{Kind: "CAL", DueAt: "2026-09-01", UsedMissing: true}, today)
	if calMissingUsed.State != DueStateOverdue || calMissingUsed.DaysLeft != -3 {
		t.Fatalf("calendar with used-missing = %+v", calMissingUsed)
	}
}
