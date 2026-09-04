package mroapp

import "testing"

func TestGenealogyResolvesInstallTail(t *testing.T) {
	c := Component{ID: "c1", SN: "SN-1", PN: "PN-1"}
	got := Genealogy(c, []LifeEvent{
		{ComponentID: "c1", Kind: "install", OccurredAt: "2026-01-01", Note: "on B-9"},
		{ComponentID: "other", Kind: "install", OccurredAt: "2026-01-02", Note: "B-8"},
	})
	if !got.Installed || got.TailNo != "B-9" || len(got.Events) != 1 {
		t.Fatalf("installed = %+v", got)
	}
	got = Genealogy(c, []LifeEvent{
		{ComponentID: "c1", Kind: "install", OccurredAt: "2026-01-01", Note: "B-9"},
		{ComponentID: "c1", Kind: "remove", OccurredAt: "2026-02-01", Note: "shop"},
	})
	if got.Installed || got.TailNo != "" {
		t.Fatalf("removed should clear tail = %+v", got)
	}
}

func TestTriggerStatusAlwaysReturnsFiveCategories(t *testing.T) {
	rows := TriggerStatus(nil)
	if len(rows) != 5 {
		t.Fatalf("empty = %+v", rows)
	}
	seen := map[string]string{}
	for _, r := range rows {
		seen[r.Kind] = r.State
		if r.State != DueStateOK {
			t.Fatalf("no dues should be ok: %+v", r)
		}
	}
	for _, kind := range []string{"usage", "llp", "ad", "bc", "cal"} {
		if _, ok := seen[kind]; !ok {
			t.Fatalf("missing category %s in %+v", kind, rows)
		}
	}
	dues := []DueView{
		{DueItem: DueItem{ScopeID: "B-1", Kind: "FH"}, Status: DueStatus{State: DueStateOverdue}},
		{DueItem: DueItem{ScopeID: "B-1", Kind: "LLP"}, Status: DueStatus{State: DueStateDue}},
		{DueItem: DueItem{ScopeID: "B-1", Kind: "AD"}, Status: DueStatus{State: DueStateOverdue}},
		{DueItem: DueItem{ScopeID: "B-1", Kind: "BC"}, Status: DueStatus{State: DueStateDue}},
		{DueItem: DueItem{ScopeID: "B-1", Kind: "CAL"}, Status: DueStatus{State: DueStateMissing}},
	}
	got := TriggerStatus(dues)
	want := map[string]string{"usage": DueStateOverdue, "llp": DueStateDue, "ad": DueStateOverdue, "bc": DueStateDue, "cal": DueStateMissing}
	for _, r := range got {
		if r.Category == "" {
			t.Fatalf("category label missing: %+v", r)
		}
		if want[r.Kind] != r.State {
			t.Fatalf("%s state = %q want %q", r.Kind, r.State, want[r.Kind])
		}
	}
}
