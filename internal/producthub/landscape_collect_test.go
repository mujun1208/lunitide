package producthub

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubResearcher struct {
	quotes []LandscapeQuote
	skips  []string
}

func (s *stubResearcher) ResearchLandscape(context.Context, []string) ([]LandscapeQuote, []string) {
	return s.quotes, s.skips
}

func TestCollectLandscapeDropsQuotesThatFailTheStructuralGate(t *testing.T) {
	s := New(&MemoryPersist{})
	s.SetResearcher(&stubResearcher{quotes: []LandscapeQuote{
		{Name: "Cursor", Axis: "local", Quote: "The engine runs on your machine.", URL: "https://cursor.com/changelog", Date: "2026-09-29"},
		{Name: "Rando", Axis: "local", Quote: "name is not in the saved list", URL: "https://cursor.com/changelog", Date: "2026-09-29"},
		{Name: "Cursor", Axis: "pricing", Quote: "axis outside the four", URL: "https://cursor.com/changelog", Date: "2026-09-29"},
		{Name: "Cursor", Axis: "local", Quote: "   ", URL: "https://cursor.com/changelog", Date: "2026-09-29"},
		{Name: "Cursor", Axis: "local", Quote: "bad url", URL: "ftp://cursor.com", Date: "2026-09-29"},
		{Name: "Cursor", Axis: "local", Quote: "bad date", URL: "https://cursor.com/changelog", Date: "2026/09/29"},
	}})
	out, err := s.CollectLandscape(context.Background(), []string{"Cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if out["collected"] != 1 {
		t.Fatalf("collected %#v", out["collected"])
	}
	skips, _ := out["skipped"].([]string)
	if len(skips) != 5 {
		t.Fatalf("skips %#v", skips)
	}
	for _, skip := range skips {
		if !strings.Contains(skip, "没有通过结构校验") {
			t.Fatalf("skip %q", skip)
		}
	}
	drafts, err := s.LandscapeDrafts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].Status != "draft" || drafts[0].Name != "Cursor" || drafts[0].Axis != "local" {
		t.Fatalf("drafts %#v", drafts)
	}
	if drafts[0].Quote != "The engine runs on your machine." || drafts[0].URL != "https://cursor.com/changelog" || drafts[0].Date != "2026-09-29" {
		t.Fatalf("draft %#v", drafts[0])
	}
}

func TestCollectLandscapeExplainsItselfWithoutAListOrChannel(t *testing.T) {
	empty := New(&MemoryPersist{})
	out, err := empty.CollectLandscape(context.Background(), []string{"  ", "lunitide", "月汐"})
	if err != nil {
		t.Fatal(err)
	}
	skips, _ := out["skipped"].([]string)
	if out["collected"] != 0 || len(skips) != 1 || !strings.Contains(skips[0], "图景页还没有保存竞品名单") {
		t.Fatalf("out %#v", out)
	}

	noChannel := New(&MemoryPersist{})
	out, err = noChannel.CollectLandscape(context.Background(), []string{"Cursor"})
	if err != nil {
		t.Fatal(err)
	}
	skips, _ = out["skipped"].([]string)
	if out["collected"] != 0 || len(skips) != 1 || !strings.Contains(skips[0], "采集通道未装配") || !strings.Contains(skips[0], "不能编造摘录") {
		t.Fatalf("out %#v", out)
	}
}

func TestConfirmedDraftIsQuotedInTheReportAndNeverMovesTheScore(t *testing.T) {
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
	// "check" walks the same live + merge path as manual without the 30s
	// refresh cooldown, so two rounds can run back to back in one test.
	before, err := s.Generate(live, "check")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before.ReportMarkdown, "Rules, MCP and skills are the public path.") {
		t.Fatalf("a draft leaked into the report before confirmation")
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
	for _, needle := range []string{"Rules, MCP and skills are the public path.", "https://cursor.com/changelog", "2026-09-29"} {
		if !strings.Contains(after.ReportMarkdown, needle) {
			t.Fatalf("confirmed entry missing %q from the report", needle)
		}
	}
	if after.HealthScore != before.HealthScore {
		t.Fatalf("confirmation must not move the health score: %d vs %d", after.HealthScore, before.HealthScore)
	}
}

func TestDraftConfirmAndDiscardLifecycle(t *testing.T) {
	s := New(&MemoryPersist{})
	s.SetResearcher(&stubResearcher{quotes: []LandscapeQuote{
		{Name: "Cursor", Axis: "local", Quote: "first", URL: "https://cursor.com/changelog", Date: "2026-09-29"},
		{Name: "Cursor", Axis: "hub", Quote: "second", URL: "https://cursor.com/changelog", Date: "2026-09-29"},
	}})
	if _, err := s.CollectLandscape(context.Background(), []string{"Cursor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmLandscapeDraft(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id must surface ErrNotFound, got %v", err)
	}
	drafts, err := s.LandscapeDrafts(context.Background())
	if err != nil || len(drafts) != 2 {
		t.Fatalf("drafts %#v err %v", drafts, err)
	}
	if err := s.DiscardLandscapeDraft(context.Background(), drafts[1].ID); err != nil {
		t.Fatal(err)
	}
	kept, err := s.LandscapeDrafts(context.Background())
	if err != nil || len(kept) != 1 || kept[0].Quote != "first" {
		t.Fatalf("kept %#v err %v", kept, err)
	}
	again, err := s.ConfirmLandscapeDraft(context.Background(), kept[0].ID)
	if err != nil || again.Status != "confirmed" {
		t.Fatalf("confirm returned %#v err %v", again, err)
	}
	idempotent, err := s.ConfirmLandscapeDraft(context.Background(), kept[0].ID)
	if err != nil || idempotent.Status != "confirmed" {
		t.Fatalf("second confirm returned %#v err %v", idempotent, err)
	}
	all, err := s.LandscapeDrafts(context.Background())
	if err != nil || len(all) != 1 || all[0].Status != "confirmed" {
		t.Fatalf("all %#v err %v", all, err)
	}
}
