package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestBuildMediaSearchURLTargets(t *testing.T) {
	cases := []struct {
		target string
		query  string
		want   string
	}{
		{"netease", "晴天", "https://music.163.com/#/search/m/?s="},
		{"qqmusic", "晴天", "https://y.qq.com/n/ryqq/search?w="},
		{"browser", "hello", "https://music.youtube.com/search?q="},
	}
	for _, tc := range cases {
		got, err := buildMediaSearchURL(tc.target, tc.query)
		if err != nil {
			t.Fatalf("%s: %v", tc.target, err)
		}
		if !containsPrefix(got, tc.want) {
			t.Fatalf("target=%s got=%q want prefix %q", tc.target, got, tc.want)
		}
	}
}

func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestPickTrackNodePrefersNamedListItemOverNav(t *testing.T) {
	nodes := []mediaUINode{
		{Role: "button", Name: "我喜欢的音乐", Y: 40, H: 32},
		{Role: "listitem", Name: "复古公路歌", Y: 120, H: 48, W: 400},
		{Role: "listitem", Name: "真的用了心 (温情版)", Y: 180, H: 48, W: 400},
	}
	got := pickTrackNode(nodes, "复古公路歌")
	if got == nil || got.Name != "复古公路歌" {
		t.Fatalf("got %+v", got)
	}
}

func TestNowPlayingConfirmedIgnoresListAndRequiresBar(t *testing.T) {
	listAndWrongBar := []mediaUINode{
		{Role: "button", Name: "我喜欢的音乐", Y: 40, H: 32, W: 160},
		{Role: "listitem", Name: "复古公路歌", Y: 120, H: 48, W: 400},
		{Role: "text", Name: "真的用了心 (温情版)", Y: 900, H: 36, W: 280},
	}
	if nowPlayingConfirmed(listAndWrongBar, "汽水音乐", "复古公路歌") {
		t.Fatal("list row must not count as now-playing")
	}
	listAndRightBar := []mediaUINode{
		{Role: "listitem", Name: "复古公路歌", Y: 120, H: 48, W: 400},
		{Role: "text", Name: "复古公路歌", Y: 900, H: 36, W: 280},
	}
	if !nowPlayingConfirmed(listAndRightBar, "汽水音乐", "复古公路歌") {
		t.Fatal("player bar should confirm now-playing")
	}
	if !nowPlayingConfirmed(listAndWrongBar, "汽水音乐 - 复古公路歌", "复古公路歌") {
		t.Fatal("window title should confirm now-playing")
	}
	if !nowPlayingConfirmed(nil, "", "热门") {
		t.Fatal("generic query should skip verify")
	}
}

func TestPlayNamedTrackClicksAndVerifiesWithoutMediaKey(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	var tools []string
	playing := false
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		tools = append(tools, tool)
		switch tool {
		case ccapp.ToolMouseClick:
			var a struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(args, &a)
			if strings.Contains(a.Name, "复古公路歌") {
				playing = true
			}
			return result("clicked"), nil
		case ccapp.ToolObserveUI:
			nodes := []mediaUINode{
				{Role: "button", Name: "我喜欢的音乐", Y: 40, H: 32, W: 160},
				{Role: "listitem", Name: "复古公路歌", Y: 120, H: 48, W: 400},
				{Role: "listitem", Name: "真的用了心 (温情版)", Y: 180, H: 48, W: 400},
			}
			bar := "真的用了心 (温情版)"
			if playing {
				bar = "复古公路歌"
			}
			nodes = append(nodes, mediaUINode{Role: "text", Name: bar, Y: 900, H: 36, W: 280})
			raw, _ := json.Marshal(map[string]any{"nodes": nodes})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			if playing {
				return Result{Output: "汽水音乐 - 复古公路歌"}, nil
			}
			return Result{Output: "汽水音乐"}, nil
		default:
			t.Fatalf("unexpected tool %s", tool)
			return Result{}, nil
		}
	}
	res, err := playNamedTrackInForeground(context.Background(), invoke, "s1", "复古公路歌", "汽水音乐", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "复古公路歌") || !strings.Contains(res.Output, "verified") {
		t.Fatalf("output %q", res.Output)
	}
	if !playing {
		t.Fatal("named track was never clicked")
	}
	for _, name := range tools {
		switch name {
		case ccapp.ToolKeyboardShortcut, ccapp.ToolKeyboardType, ccapp.ToolSetValue:
			t.Fatalf("must not fall back to keyboard/search when named click works: %v", tools)
		}
	}
}

func TestPlayNamedTrackFailsWithoutVerify(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	invoke := func(_ context.Context, _, tool string, _ json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolMouseClick:
			return result("clicked"), nil
		case ccapp.ToolObserveUI:
			nodes := []mediaUINode{
				{Role: "button", Name: "我喜欢的音乐", Y: 40, H: 32, W: 160},
				{Role: "listitem", Name: "复古公路歌", Y: 120, H: 48, W: 400},
				{Role: "text", Name: "真的用了心 (温情版)", Y: 900, H: 36, W: 280},
			}
			raw, _ := json.Marshal(map[string]any{"nodes": nodes})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "汽水音乐"}, nil
		default:
			return Result{}, nil
		}
	}
	_, err := playNamedTrackInForeground(context.Background(), invoke, "s1", "复古公路歌", "汽水音乐", true)
	if err == nil || !strings.Contains(err.Error(), "未能核对正在播放「复古公路歌」") {
		t.Fatalf("want unverified failure, got %v", err)
	}
}
