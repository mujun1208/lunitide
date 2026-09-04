package mroapp

import (
	"strings"
	"time"
)

const (
	DueStateOK      = "ok"
	DueStateDue     = "due"
	DueStateOverdue = "overdue"
	DueStateMissing = "missing"
	DueLabelMissing = "未录入"
)

type DueItem struct {
	ID          string
	ScopeID     string
	Kind        string
	LimitValue  float64
	UsedValue   float64
	UsedMissing bool
	DueAt       string
	Source      string
	UpdatedAt   string
}

type DueStatus struct {
	State     string
	Label     string
	Remaining float64
	DaysLeft  int
}

type DueView struct {
	DueItem
	Status DueStatus
}

// EvaluateDue is a pure function. Missing utilization or limits are
// "未录入", never 0. Calendar uses due_at-today; usage uses limit-used.
func EvaluateDue(item DueItem, today time.Time) DueStatus {
	today = today.UTC().Truncate(24 * time.Hour)
	kind := strings.ToUpper(strings.TrimSpace(item.Kind))
	// Calendar kinds are driven by due_at. UsedMissing is a usage-ledger flag
	// and must not hide a calendar date the user already recorded.
	if kind == "CAL" || kind == "CHECK" || kind == "AD" || kind == "MEL" {
		if strings.TrimSpace(item.DueAt) == "" {
			return DueStatus{State: DueStateMissing, Label: DueLabelMissing}
		}
		due, err := time.Parse("2006-01-02", item.DueAt)
		if err != nil {
			return DueStatus{State: DueStateMissing, Label: DueLabelMissing}
		}
		days := int(due.UTC().Truncate(24*time.Hour).Sub(today).Hours() / 24)
		state := DueStateOK
		if days < 0 {
			state = DueStateOverdue
		} else if days == 0 {
			state = DueStateDue
		}
		return DueStatus{State: state, Label: state, DaysLeft: days}
	}
	if item.UsedMissing {
		return DueStatus{State: DueStateMissing, Label: DueLabelMissing}
	}
	if item.LimitValue <= 0 {
		return DueStatus{State: DueStateMissing, Label: DueLabelMissing}
	}
	remain := item.LimitValue - item.UsedValue
	state := DueStateOK
	if remain < 0 {
		state = DueStateOverdue
	} else if remain == 0 {
		state = DueStateDue
	}
	return DueStatus{State: state, Label: state, Remaining: remain}
}

// UtilizationEvent is one recorded usage tick for a scope (tail/component).
type UtilizationEvent struct {
	ID            string
	ScopeID       string
	Hours         float64
	Cycles        float64
	BatteryCycles float64
	CreatedAt     string
}

// UtilizationTotals is the summed usage for one scope.
type UtilizationTotals struct {
	Hours         float64
	Cycles        float64
	BatteryCycles float64
}

// SumUtilization totals utilization events per scope. It never invents zeros:
// a scope with no events is simply absent from the map.
func SumUtilization(events []UtilizationEvent) map[string]UtilizationTotals {
	out := map[string]UtilizationTotals{}
	for _, e := range events {
		t := out[e.ScopeID]
		t.Hours += e.Hours
		t.Cycles += e.Cycles
		t.BatteryCycles += e.BatteryCycles
		out[e.ScopeID] = t
	}
	return out
}

// RecomputeUsed sets used_value from utilization totals for usage-based kinds
// (FH/FC/BC). Calendar kinds and LLP are untouched, and a scope with no
// recorded utilization keeps its prior value (missing stays 未录入, never 0).
func RecomputeUsed(item DueItem, totals map[string]UtilizationTotals) DueItem {
	t, ok := totals[item.ScopeID]
	if !ok {
		return item
	}
	switch strings.ToUpper(strings.TrimSpace(item.Kind)) {
	case "FH":
		item.UsedValue = t.Hours
		item.UsedMissing = false
	case "FC":
		item.UsedValue = t.Cycles
		item.UsedMissing = false
	case "BC":
		item.UsedValue = t.BatteryCycles
		item.UsedMissing = false
	}
	return item
}
