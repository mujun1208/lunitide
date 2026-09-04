package mroapp

import (
	"strings"
	"sort"
)

// Component is a serialized/traceable part instance (low-altitude airworthiness).
type Component struct {
	ID        string
	SN        string
	PN        string
	LifeCount float64
	CreatedAt string
}

// LifeEvent is one genealogy event on a component.
type LifeEvent struct {
	ID          string
	ComponentID string
	Kind        string // install|remove|transfer|repair|scrap
	OccurredAt  string
	Note        string
}

// PirepDraft is a pilot/operator report draft awaiting confirmation.
type PirepDraft struct {
	ID        string
	TailNo    string
	BodyJSON  string
	State     string // draft|confirmed|rejected
	CreatedAt string
}

// AOGCase is an aircraft-on-ground intake draft (never auto-purchases).
type AOGCase struct {
	ID        string
	TailNo    string
	PN        string
	Qty       string
	Note      string
	State     string // draft|confirmed|rejected
	CreatedAt string
}

// PODraft is a purchase-order draft (human confirmation only).
type PODraft struct {
	ID        string
	PN        string
	Qty       string
	Price     string
	State     string // draft|confirmed|rejected
	CreatedAt string
}

// GenealogyView is the resolved life history of a component.
type GenealogyView struct {
	Component
	Events    []LifeEvent
	Installed bool
	TailNo    string
}

// Genealogy orders a component's life events chronologically and resolves the
// current install state. It is a pure projection: install marks installed,
// remove/transfer/scrap clears it, and repair is neutral.
func Genealogy(c Component, events []LifeEvent) GenealogyView {
	own := make([]LifeEvent, 0, len(events))
	for _, e := range events {
		if e.ComponentID == c.ID {
			own = append(own, e)
		}
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].OccurredAt < own[j].OccurredAt })
	installed := false
	tail := ""
	for _, e := range own {
		switch e.Kind {
		case "install":
			installed = true
			tail = extractTailNo(e.Note)
		case "remove", "transfer", "scrap":
			installed = false
			tail = ""
		}
	}
	return GenealogyView{Component: c, Events: own, Installed: installed, TailNo: tail}
}

func extractTailNo(note string) string {
	for _, f := range strings.Fields(note) {
		f = strings.Trim(f, ".,;:")
		if strings.Contains(f, "-") && len(f) <= 32 {
			return f
		}
	}
	return strings.TrimSpace(note)
}

// TriggerRow is a derived airworthiness trigger recommendation. Triggers are
// never actions themselves; they only surface a due/overdue scope for a human
// to schedule an inspection or replacement.
type TriggerRow struct {
	ScopeID  string
	Kind     string
	State    string
	Action   string
	Category string
}

var triggerCategories = []struct {
	kind, category string
	match          func(string) bool
}{
	{"usage", "登记到期", func(k string) bool { return k == "FH" || k == "FC" || k == "CHECK" }},
	{"llp", "LLP剩余", func(k string) bool { return k == "LLP" }},
	{"ad", "AD到期", func(k string) bool { return k == "AD" }},
	{"bc", "电池循环", func(k string) bool { return k == "BC" }},
	{"cal", "日历", func(k string) bool { return k == "CAL" || k == "MEL" }},
}

func worseDueState(cur, next string) string {
	rank := map[string]int{DueStateOK: 0, DueStateMissing: 1, DueStateDue: 2, DueStateOverdue: 3}
	if rank[next] > rank[cur] {
		return next
	}
	return cur
}

// TriggerStatus always returns the five airworthiness trigger categories.
// State is the worst matching due item (overdue > due > missing > ok).
// No matching dues stay ok — the engine invents nothing.
func TriggerStatus(dues []DueView) []TriggerRow {
	out := make([]TriggerRow, 0, len(triggerCategories))
	for _, cat := range triggerCategories {
		state := DueStateOK
		action := "无到期项"
		for _, d := range dues {
			if !cat.match(strings.ToUpper(strings.TrimSpace(d.Kind))) {
				continue
			}
			state = worseDueState(state, d.Status.State)
		}
		switch state {
		case DueStateDue, DueStateOverdue:
			action = "安排检查或换件（人工确认）"
		case DueStateMissing:
			action = "未录入，待补利用率或到期日"
		}
		out = append(out, TriggerRow{Kind: cat.kind, State: state, Action: action, Category: cat.category})
	}
	return out
}

// DeriveTriggers keeps the older item-level projection for due/overdue scopes.
func DeriveTriggers(dues []DueView) []TriggerRow {
	out := make([]TriggerRow, 0)
	for _, d := range dues {
		if d.Status.State == DueStateDue || d.Status.State == DueStateOverdue {
			out = append(out, TriggerRow{
				ScopeID: d.ScopeID,
				Kind:    d.Kind,
				State:   d.Status.State,
				Action:  "安排检查或换件（人工确认）",
			})
		}
	}
	return out
}
