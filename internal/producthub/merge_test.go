package producthub

import "testing"

func TestMergeAddsLiveOnlyFeature(t *testing.T) {
	seed := []Card{{
		StableKey: "feature.dialog.music.play", Name: "播放歌曲", Summary: "播放当前曲目",
		Description: "种子讲解：核验 SMTC 后才算播放。",
		Chain: closedChain([]Step{
			{1, "选会话", "", "a"}, {2, "派发", "", "b"}, {3, "核验", "", "c"},
		}, 3, "已播放", "失败", "重试", "未核验"),
		Methods: []Method{{Type: "voice", Entry: "说播放"}},
	}}
	live := []Candidate{
		{StableKey: "feature.dialog.music.play", Name: "播放", Source: "media", ChainClass: "media-transport"},
		{StableKey: "feature.office.media.favorite", Name: "收藏", NameEN: "Favorite", Source: "bridge", Domain: "office", Module: "media"},
	}
	out := Merge(seed, live, nil)
	if !hasKey(out, "feature.office.media.favorite") {
		t.Fatal("live-only must be generated")
	}
	got := byKey(out, "feature.dialog.music.play")
	if got.Description != "种子讲解：核验 SMTC 后才算播放。" {
		t.Fatalf("seed prose must win, got %q", got.Description)
	}
	if got.Provenance != "seed+live" {
		t.Fatalf("provenance=%s", got.Provenance)
	}
}

func TestMergeRemovesMissingPage(t *testing.T) {
	prev := []Card{{StableKey: "feature.office.page.media", Name: "媒体中心"}}
	emptyLive := Merge(nil, []Candidate{}, prev)
	ch := Changelog(prev, emptyLive)
	if !hasKind(ch, "feature.office.page.media", "removed") {
		t.Fatalf("missing live page must retire: %+v out=%d", ch, len(emptyLive))
	}
}

func TestChangelogAdded(t *testing.T) {
	prev := []Card{{StableKey: "a"}}
	next := []Card{{StableKey: "a"}, {StableKey: "b"}}
	ch := Changelog(prev, next)
	if !hasKind(ch, "b", "added") {
		t.Fatalf("%+v", ch)
	}
}

func hasKey(cards []Card, key string) bool {
	for _, c := range cards {
		if c.StableKey == key {
			return true
		}
	}
	return false
}

func byKey(cards []Card, key string) Card {
	for _, c := range cards {
		if c.StableKey == key {
			return c
		}
	}
	return Card{}
}

func hasKind(ch []Change, key, kind string) bool {
	for _, c := range ch {
		if c.StableKey == key && c.Kind == kind {
			return true
		}
	}
	return false
}
