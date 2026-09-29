package producthub

import (
	"context"
	"strings"
	"testing"
)

func TestUpgradeFindingsDegradewithoutConfirmedQuotes(t *testing.T) {
	for _, drafts := range [][]LandscapeDraft{
		nil,
		{{ID: "d1", Name: "Cursor", Axis: "local", Quote: "still a draft", URL: "https://cursor.com/changelog", Date: "2026-09-29", Status: "draft"}},
	} {
		got := UpgradeFindings(drafts, Edition{})
		if len(got) != 1 || got[0].ErrorCode != "PH_L92" || got[0].StableKey != "landscape.upgrade.none" {
			t.Fatalf("got %#v", got)
		}
		if !strings.Contains(got[0].Evidence, "还没有人工确认的竞品摘录") || !strings.Contains(got[0].Evidence, "优化方案") {
			t.Fatalf("degraded note must point at the local plans: %q", got[0].Evidence)
		}
		if got[0].Severity != "info" || got[0].Status != "note" {
			t.Fatalf("upgrade notes must stay out of the health score: %#v", got[0])
		}
	}
}

func TestUpgradeFindingsMergeConfirmedQuotesWithOwnGaps(t *testing.T) {
	ed := Edition{Features: []Card{{StableKey: "feature.demo.play", Name: "放歌"}}}
	drafts := []LandscapeDraft{
		{ID: "c1", Name: "Cursor", Axis: "assets", Quote: "Rules, MCP and skills are the public path.", URL: "https://cursor.com/changelog", Date: "2026-09-29", Status: "confirmed"},
		{ID: "c2", Name: "Copilot", Axis: "local", Quote: "Runs in a cloud session.", URL: "https://github.blog/changelog/label/copilot/", Date: "2026-09-28", Status: "confirmed"},
		{ID: "d1", Name: "Cursor", Axis: "hub", Quote: "still a draft", URL: "https://cursor.com/changelog", Date: "2026-09-29", Status: "draft"},
		{ID: "c3", Name: "Cursor", Axis: "pricing", Quote: "bad axis", URL: "https://cursor.com/changelog", Date: "2026-09-29", Status: "confirmed"},
	}
	got := UpgradeFindings(drafts, ed)
	if len(got) != 2 {
		t.Fatalf("only axes with confirmed quotes appear, got %#v", got)
	}
	if got[0].StableKey != "landscape.upgrade.local" || got[1].StableKey != "landscape.upgrade.assets" {
		t.Fatalf("axes must keep the fixed order: %#v", got)
	}
	for _, f := range got {
		if f.Severity != "info" || f.Status != "note" || !strings.HasPrefix(f.Title, "升级对照 · ") {
			t.Fatalf("finding %#v", f)
		}
		if !strings.Contains(f.Evidence, "竞品确认摘录：") || !strings.Contains(f.Evidence, "本产品缺口：") {
			t.Fatalf("both sides must be present: %q", f.Evidence)
		}
	}
	local := got[0].Evidence
	if !strings.Contains(local, "Copilot「Runs in a cloud session.」（https://github.blog/changelog/label/copilot/ 2026-09-28）") {
		t.Fatalf("quote must carry its source and date verbatim: %q", local)
	}
	if !strings.Contains(local, "步骤还不清楚 1 张") || !strings.Contains(local, "没有入口方法 1 张") {
		t.Fatalf("own gaps must come from this edition: %q", local)
	}
	assets := got[1].Evidence
	if !strings.Contains(assets, "Cursor「Rules, MCP and skills are the public path.」（https://cursor.com/changelog 2026-09-29）") {
		t.Fatalf("assets quote: %q", assets)
	}
}

func TestUpgradeFindingsOnACleanEdition(t *testing.T) {
	got := UpgradeFindings([]LandscapeDraft{
		{ID: "c1", Name: "Cursor", Axis: "local", Quote: "Local-first by default.", URL: "https://cursor.com/changelog", Date: "2026-09-29", Status: "confirmed"},
	}, Edition{})
	if len(got) != 1 || !strings.Contains(got[0].Evidence, "本版没有待修缺口") {
		t.Fatalf("clean edition wording: %#v", got)
	}
}

func TestGenerateEmitsUpgradeFindings(t *testing.T) {
	s := New(&MemoryPersist{})
	s.SetResearcher(&stubResearcher{quotes: []LandscapeQuote{
		{Name: "Cursor", Axis: "assets", Quote: "Rules, MCP and skills are the public path.", URL: "https://cursor.com/changelog", Date: "2026-09-29"},
	}})
	if _, err := s.CollectLandscape(context.Background(), []string{"Cursor"}); err != nil {
		t.Fatal(err)
	}
	live := WithLiveTasks(context.Background(), func(context.Context) ([]TaskResult, string, []LandscapeNote) {
		return []TaskResult{{ID: "play", Title: "播放", Status: "pass", Evidence: "回读到 playing"}}, "", nil
	})
	before, err := s.Generate(live, "check")
	if err != nil {
		t.Fatal(err)
	}
	if len(upgradeNames(before.Findings)) != 1 || !strings.Contains(before.ReportMarkdown, "还没有人工确认的竞品摘录") {
		t.Fatalf("unconfirmed drafts must degrade the upgrade section: %#v", before.Findings)
	}
	drafts, err := s.LandscapeDrafts(context.Background())
	if err != nil || len(drafts) != 1 {
		t.Fatalf("drafts %#v err %v", drafts, err)
	}
	if _, err := s.ConfirmLandscapeDraft(context.Background(), drafts[0].ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.Generate(live, "check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after.ReportMarkdown, "升级对照") {
		t.Fatalf("report must carry the upgrade section")
	}
	if !strings.Contains(after.ReportMarkdown, "Rules, MCP and skills are the public path.") || !strings.Contains(after.ReportMarkdown, "本产品缺口：") {
		t.Fatalf("confirmed quote must be merged with own gaps")
	}
	if after.HealthScore != before.HealthScore {
		t.Fatalf("upgrade notes must not move the health score: %d vs %d", after.HealthScore, before.HealthScore)
	}
}
