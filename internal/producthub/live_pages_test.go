package producthub

import (
	"testing"

	"github.com/lunitide/lunitide/internal/producthub/generated"
)

func TestLiveCatalogCoversGeneratedPages(t *testing.T) {
	have := map[string]bool{}
	for _, card := range LiveCatalog() {
		for _, page := range card.Scaffold.Pages {
			have[page] = true
		}
	}
	for _, page := range generated.Pages {
		if !have[page.ID] {
			t.Fatalf("live catalog missing frontend page %s", page.ID)
		}
		// A page with no name or domain means the generator parsed the
		// renderer's PAGE_ATLAS but found nothing for this id, which would
		// silently produce a card keyed feature..page.<id>.
		if page.Name == "" || page.Domain == "" || page.Module == "" {
			t.Fatalf("generated page %s is missing metadata: %+v", page.ID, page)
		}
	}
	if len(generated.Pages) != 17 {
		t.Fatalf("expected 17 frontend pages, got %d", len(generated.Pages))
	}
	if len(generated.Settings) != 18 {
		t.Fatalf("expected 18 settings, got %d", len(generated.Settings))
	}
}

// Every page card the hub serves must be keyed with the same domain the
// renderer's PAGE_ATLAS uses, or the frontend looks up feature.office.page.media
// while the engine published feature.foundation.page.media and the page dossier
// comes back empty.
func TestPageCardKeysUseTheGeneratedDomain(t *testing.T) {
	keys := map[string]bool{}
	for _, card := range LiveCatalog() {
		keys[card.StableKey] = true
	}
	for _, page := range generated.Pages {
		want := "feature." + page.Domain + ".page." + page.ID
		if !keys[want] {
			t.Fatalf("page %s has no card at %s", page.ID, want)
		}
	}
}

func TestLiveVerbPagesMatchFrontend(t *testing.T) {
	want := map[string][]string{
		"feature.dialog.companion.voice-talk": {"home"},
		"feature.dialog.music.play":           {"home", "media"},
		"feature.dialog.chat.mention-skill":   {"home", "skill"},
		"feature.assets.memory.recall":        {"assets", "settings"},
		"feature.execution.computer.launch":   {"settings", "home"},
		"feature.foundation.provider.add":     {"providers", "settings"},
	}
	have := map[string][]string{}
	for _, card := range LiveCatalog() {
		have[card.StableKey] = card.Scaffold.Pages
	}
	for key, pages := range want {
		got := have[key]
		if !sameSet(got, pages) {
			t.Fatalf("%s pages=%v want %v", key, got, pages)
		}
	}
	if contains(have["feature.dialog.companion.voice-talk"], "voice") {
		t.Fatal("voice is a settings category, not a frontend page")
	}
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, item := range want {
		if !contains(got, item) {
			return false
		}
	}
	return true
}

func TestChangelogNamesPageImpact(t *testing.T) {
	next := []Card{{
		StableKey: "feature.office.page.media", Name: "进入媒体中心", Summary: "打开媒体中心页面。",
		Scaffold: Scaffold{Pages: []string{"media"}},
		Domain:   "office", Module: "media",
	}}
	ch := Changelog(nil, next)
	if len(ch) != 1 || ch[0].Title != "进入媒体中心" {
		t.Fatalf("%+v", ch)
	}
	if !contains(ch[0].Impacts, "media") || !contains(ch[0].Impacts, "page.media") {
		t.Fatalf("impacts=%v", ch[0].Impacts)
	}
}

func contains(in []string, v string) bool {
	for _, item := range in {
		if item == v {
			return true
		}
	}
	return false
}
