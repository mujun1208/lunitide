package widgetapp

import (
	"path/filepath"
	"testing"
	"time"
)

func TestValidateRejectsArbitraryHTMLAndHostMessages(t *testing.T) {
	if err := Validate(Spec{ID: "w1", Owner: "u1", Title: "t", Widgets: []Widget{{ID: "a", Kind: "html"}}}); err == nil {
		t.Fatal("arbitrary html must be rejected")
	}
	if err := Validate(Spec{ID: "w1", Owner: "u1", Title: "t", Widgets: []Widget{{ID: "a", Kind: "script"}}}); err == nil {
		t.Fatal("script kind must be rejected")
	}
	if err := Validate(Spec{ID: "w1", Owner: "u1", Title: "t", Actions: []Action{{ID: "run", Kind: "host.message"}}}); err == nil {
		t.Fatal("host messages must be rejected")
	}
	if err := Validate(Spec{
		ID: "w1", Owner: "u1", Title: "timer",
		Widgets: []Widget{{ID: "a", Kind: "timer"}},
		Actions: []Action{{ID: "tick", Kind: "local.toggle"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateQueryArchiveSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widgets.json")
	s := NewFileStore(path)
	got, err := s.Create(Spec{ID: "w1", Owner: "u1", Title: "timer", Widgets: []Widget{{ID: "a", Kind: "timer"}}})
	if err != nil || got.Revision != 1 || got.Status != "active" {
		t.Fatalf("create %+v %v", got, err)
	}
	if _, err := s.Create(Spec{ID: "w1", Owner: "u2", Title: "x", Widgets: []Widget{{ID: "a", Kind: "timer"}}}); err == nil {
		t.Fatal("foreign owner must not overwrite")
	}
	reopened := NewFileStore(path)
	listed, err := reopened.Query("u1", "")
	if err != nil || len(listed) != 1 || listed[0].Title != "timer" {
		t.Fatalf("reload %+v %v", listed, err)
	}
	archived, err := reopened.Archive("u1", "w1", listed[0].Revision)
	if err != nil || archived.Status != "archived" {
		t.Fatalf("archive %+v %v", archived, err)
	}
}

func TestItemDeleteStopsLaterTriggers(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "items.json"))
	item, err := s.UpsertItem(Item{ID: "i1", Type: "bill", Title: "rent", Status: "open", RepeatRule: "monthly"})
	if err != nil || item.Status != "open" {
		t.Fatalf("upsert %+v %v", item, err)
	}
	if !s.HasItem("i1") || !s.ItemDue("i1") {
		t.Fatal("open item may trigger")
	}
	stopped, err := s.ArchiveItem("i1")
	if err != nil || stopped.Status != "stopped" {
		t.Fatalf("archive item %+v %v", stopped, err)
	}
	if !s.HasItem("i1") || s.ItemDue("i1") {
		t.Fatal("deleted item must not trigger again")
	}
	if _, err := s.UpsertItem(Item{ID: "mail", Type: "email", Title: "inbox"}); err == nil {
		t.Fatal("email items are out of scope")
	}
}

func TestItemDueRespectsDueAt(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "due.json"))
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := s.UpsertItem(Item{ID: "later", Type: "bill", Title: "rent", Status: "open", DueAt: future}); err != nil {
		t.Fatal(err)
	}
	if s.ItemDue("later") {
		t.Fatal("future dueAt must not fire")
	}
	if _, err := s.UpsertItem(Item{ID: "now", Type: "bill", Title: "due", Status: "open", DueAt: past}); err != nil {
		t.Fatal(err)
	}
	if !s.ItemDue("now") {
		t.Fatal("past dueAt may fire")
	}
	if _, err := s.UpsertItem(Item{ID: "open", Type: "action", Title: "do", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if !s.ItemDue("open") {
		t.Fatal("open item without dueAt may fire")
	}
}

func TestWidgetStateSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewFileStore(path)
	got, err := s.Create(Spec{
		ID: "w1", Owner: "u1", Title: "timer",
		Widgets: []Widget{{ID: "a", Kind: "timer", State: WidgetState{Seconds: 119, Running: true, UpdatedAt: "2026-09-10T12:00:00Z"}}},
	})
	if err != nil || got.Widgets[0].State.Seconds != 119 {
		t.Fatalf("create %+v %v", got, err)
	}
	reopened := NewFileStore(path)
	listed, err := reopened.Query("u1", "w1")
	if err != nil || len(listed) != 1 || listed[0].Widgets[0].State.Seconds != 119 || !listed[0].Widgets[0].State.Running {
		t.Fatalf("reload state %+v %v", listed, err)
	}
	if err := Validate(Spec{ID: "w2", Owner: "u1", Title: "bad", Widgets: []Widget{{ID: "a", Kind: "timer", State: WidgetState{Seconds: 999999}}}}); err == nil {
		t.Fatal("seconds out of range must fail")
	}
}

func TestItemDueUsesTimezoneForNaiveDueAt(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "tz.json"))
	if _, err := s.UpsertItem(Item{ID: "future", Type: "checkin", Title: "punch", Status: "open", DueAt: "2099-01-01 09:00:00", Timezone: "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	if s.ItemDue("future") {
		t.Fatal("naive future dueAt in timezone must not fire")
	}
	if _, err := s.UpsertItem(Item{ID: "past", Type: "checkin", Title: "old", Status: "open", DueAt: "2000-01-01 09:00:00", Timezone: "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	if !s.ItemDue("past") {
		t.Fatal("naive past dueAt in timezone may fire")
	}
	if _, err := s.UpsertItem(Item{ID: "badtz", Type: "bill", Title: "x", Status: "open", DueAt: "2000-01-01 09:00:00", Timezone: "Not/AZone"}); err != nil {
		t.Fatal(err)
	}
	if s.ItemDue("badtz") {
		t.Fatal("unknown timezone must fail closed")
	}
}

func TestDispatchActionIsIdempotentAndBound(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "act.json"))
	spec, err := s.Create(Spec{
		ID: "w1", Owner: "u1", Title: "list",
		Widgets: []Widget{{ID: "a", Kind: "checklist"}},
		Actions: []Action{{ID: "done", Kind: "local.toggle"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dispatch("u1", spec.ID, "missing", spec.Revision); err == nil {
		t.Fatal("unknown action must fail")
	}
	if _, err := s.Dispatch("u2", spec.ID, "done", spec.Revision); err == nil {
		t.Fatal("foreign owner must fail")
	}
	first, err := s.Dispatch("u1", spec.ID, "done", spec.Revision)
	if err != nil || first != 1 {
		t.Fatalf("first dispatch %d %v", first, err)
	}
	again, err := s.Dispatch("u1", spec.ID, "done", spec.Revision)
	if err != nil || again != 1 {
		t.Fatalf("repeat click must not send twice: %d %v", again, err)
	}
}
