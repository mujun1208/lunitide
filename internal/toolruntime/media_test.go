package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/winexec"
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

func TestPickTrackAndPlaySkipLikedPlaylist(t *testing.T) {
	if !isLikedPlaylistName("我喜欢的音乐") || !isLikedPlaylistName("我的收藏") {
		t.Fatal("liked names")
	}
	nodes := []mediaUINode{
		{Role: "listitem", Name: "我喜欢的音乐", Y: 40, H: 32, W: 200},
		{Role: "button", Name: "收藏", Y: 80, H: 28, W: 64},
		{Role: "listitem", Name: "晴天", Y: 160, H: 48, W: 400},
		{Role: "button", Name: "播放", Y: 900, H: 32, W: 32},
	}
	got := pickTrackNode(nodes, "晴天")
	if got == nil || got.Name != "晴天" {
		t.Fatalf("track %+v", got)
	}
	if picked := pickTrackNode(nodes, "周杰伦"); picked != nil && isLikedPlaylistName(picked.Name) {
		t.Fatalf("liked playlist leaked into track pick %+v", picked)
	}
	play := pickPlayControl(nodes)
	if play == nil || play.Name != "播放" {
		t.Fatalf("play %+v", play)
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
	if nowPlayingConfirmed(nil, "", "热门") {
		t.Fatal("generic query with no playback evidence must not count as playing")
	}
	pauseOnly := []mediaUINode{{Role: "button", Name: "暂停", Y: 900, H: 32, W: 32}}
	if !nowPlayingConfirmed(pauseOnly, "汽水音乐", "热门") {
		t.Fatal("pause control should confirm generic playback")
	}
	playOnly := []mediaUINode{{Role: "button", Name: "播放", Y: 900, H: 32, W: 32}}
	if nowPlayingConfirmed(playOnly, "汽水音乐", "热门") {
		t.Fatal("visible 播放 means still paused")
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

func TestPlayArtistSearchClicksAResult(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	sendForegroundPlay = func(string) error { return nil }
	t.Cleanup(func() { sendForegroundPlay = winexec.SendMediaKey })

	searched := false
	clicked := ""
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolMouseClick:
			var a struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(args, &a)
			clicked = a.Name
			return result("clicked"), nil
		case ccapp.ToolSetValue, ccapp.ToolPress, ccapp.ToolKeyboardType, ccapp.ToolKeyboardShortcut:
			searched = true
			return result("ok"), nil
		case ccapp.ToolObserveUI:
			nodes := []mediaUINode{
				{Role: "edit", Name: "搜索", Y: 8, H: 28, W: 240},
			}
			if searched {
				nodes = append(nodes,
					mediaUINode{Role: "listitem", Name: "晴天", Y: 120, H: 48, W: 400},
					mediaUINode{Role: "listitem", Name: "夜曲", Y: 180, H: 48, W: 400},
					mediaUINode{Role: "text", Name: "夜曲", Y: 900, H: 36, W: 280},
				)
			} else {
				nodes = append(nodes, mediaUINode{Role: "text", Name: "私人漫游", Y: 900, H: 36, W: 280})
			}
			raw, _ := json.Marshal(map[string]any{"nodes": nodes})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "网易云音乐"}, nil
		default:
			t.Fatalf("unexpected tool %s", tool)
			return Result{}, nil
		}
	}
	res, err := playNamedTrackInForeground(context.Background(), invoke, "s1", "周杰伦", "网易云音乐", true)
	if err != nil {
		t.Fatal(err)
	}
	if !searched {
		t.Fatal("expected search")
	}
	if clicked != "夜曲" && clicked != "晴天" {
		t.Fatalf("clicked %q", clicked)
	}
	if !strings.Contains(res.Output, "周杰伦") && !strings.Contains(res.Output, "夜曲") && !strings.Contains(res.Output, "晴天") {
		t.Fatalf("output %q", res.Output)
	}
}

func TestQueryMustSearchFirstForArtistNotLongSongTitle(t *testing.T) {
	if !queryMustSearchFirst("周杰伦") {
		t.Fatal("周杰伦 must search first")
	}
	if queryMustSearchFirst("复古公路歌") {
		t.Fatal("on-screen song title must still click without search")
	}
	if queryMustSearchFirst("热门") {
		t.Fatal("generic query is not an artist search")
	}
}

func TestPlayArtistDoesNotMediaKeyWithoutSearch(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	keyed := false
	sendForegroundPlay = func(string) error {
		keyed = true
		return nil
	}
	t.Cleanup(func() { sendForegroundPlay = winexec.SendMediaKey })

	invoke := func(_ context.Context, _, tool string, _ json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolMouseClick:
			return result("clicked"), nil
		case ccapp.ToolObserveUI:
			nodes := []mediaUINode{
				{Role: "button", Name: "我喜欢的音乐", Y: 40, H: 32, W: 160},
				{Role: "text", Name: "私人漫游", Y: 900, H: 36, W: 280},
			}
			raw, _ := json.Marshal(map[string]any{"nodes": nodes})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "网易云音乐"}, nil
		default:
			return Result{}, nil
		}
	}
	_, err := playNamedTrackInForeground(context.Background(), invoke, "s1", "周杰伦", "网易云音乐", true)
	if err == nil {
		t.Fatal("want failure when the artist was never searched to a result")
	}
	if keyed {
		t.Fatal("must not send media keys when the search box was never used")
	}
}

func TestExecuteMediaPlayJayChouUsesDesktopSearchNotWeb(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	openedHTTP := ""
	openMediaURL = func(u string) error {
		openedHTTP = u
		return nil
	}
	t.Cleanup(func() { openMediaURL = openHTTPURL })

	launched := ""
	openLaunchPath = func(p string) error {
		launched = p
		return nil
	}
	t.Cleanup(func() { openLaunchPath = openWithDefaultApp })

	activateWindow = func(string) error { return errors.New("not running") }
	t.Cleanup(func() { activateWindow = winexec.ActivateWindowMatching })

	exe := `C:\fake\Netease\CloudMusic\cloudmusic.exe`
	origLookup := lookupKnownAppExecutables
	lookupKnownAppExecutables = func(app knownLaunchApp) []string {
		if app.Canonical == "网易云音乐" {
			return []string{exe}
		}
		return nil
	}
	t.Cleanup(func() { lookupKnownAppExecutables = origLookup })

	sendForegroundPlay = func(string) error { return nil }
	t.Cleanup(func() { sendForegroundPlay = winexec.SendMediaKey })

	searched := false
	typed := ""
	clicked := ""
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolWindowFocus:
			return result("ok"), nil
		case ccapp.ToolMouseClick:
			var a struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(args, &a)
			clicked = a.Name
			return result("clicked"), nil
		case ccapp.ToolSetValue:
			var a struct {
				Value string `json:"value"`
			}
			_ = json.Unmarshal(args, &a)
			typed = a.Value
			searched = true
			return result("ok"), nil
		case ccapp.ToolPress, ccapp.ToolKeyboardType, ccapp.ToolKeyboardShortcut:
			searched = true
			return result("ok"), nil
		case ccapp.ToolObserveUI:
			nodes := []mediaUINode{
				{Role: "edit", Name: "搜索", Y: 8, H: 28, W: 240},
			}
			if searched {
				bar := "晴天"
				if clicked == "夜曲" || clicked == "晴天" {
					bar = clicked
				}
				nodes = append(nodes,
					mediaUINode{Role: "listitem", Name: "晴天", Y: 120, H: 48, W: 400},
					mediaUINode{Role: "listitem", Name: "夜曲", Y: 180, H: 48, W: 400},
					mediaUINode{Role: "text", Name: bar, Y: 900, H: 36, W: 280},
				)
			} else {
				nodes = append(nodes, mediaUINode{Role: "text", Name: "私人漫游", Y: 900, H: 36, W: 280})
			}
			raw, _ := json.Marshal(map[string]any{"nodes": nodes})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "网易云音乐"}, nil
		default:
			t.Fatalf("unexpected tool %s", tool)
			return Result{}, nil
		}
	}

	for _, raw := range []string{
		`{"action":"play","query":"周杰伦","target":"auto","app":"网易云音乐"}`,
		`{"action":"play","query":"周杰伦","target":"netease"}`,
		`{"action":"play","query":"周杰伦","target":"","app":"网易云音乐"}`,
	} {
		openedHTTP = ""
		launched = ""
		searched = false
		typed = ""
		clicked = ""
		res, err := executeMediaPlayWithCC(context.Background(), invoke, "s1", json.RawMessage(raw), true, true)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if openedHTTP != "" || strings.Contains(res.Output, "163.com") || strings.Contains(res.Output, "music.163") {
			t.Fatalf("%s opened web %q output %q", raw, openedHTTP, res.Output)
		}
		if launched == "" || strings.Contains(launched, "163.com") {
			t.Fatalf("%s launched %q want desktop cloudmusic, not web", raw, launched)
		}
		if !strings.Contains(strings.ToLower(launched), "cloudmusic") && !strings.Contains(launched, "网易云") {
			t.Fatalf("%s launched %q want desktop 网易云 / cloudmusic.exe", raw, launched)
		}
		if !searched || typed != "周杰伦" {
			t.Fatalf("%s searched=%v typed=%q", raw, searched, typed)
		}
		if clicked != "晴天" && clicked != "夜曲" {
			t.Fatalf("%s clicked %q want a search result", raw, clicked)
		}
	}
}

func TestFillSearchFieldTypesAfterSetValueNoOp(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	typed := ""
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolSetValue:
			return result("ok"), nil
		case ccapp.ToolKeyboardType:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			typed = a.Text
			return result("ok"), nil
		case ccapp.ToolMouseClick, ccapp.ToolPress, ccapp.ToolKeyboardShortcut:
			return result("ok"), nil
		default:
			return Result{}, nil
		}
	}
	if err := fillSearchField(context.Background(), invoke, "s1", mediaUINode{Role: "edit", Name: "搜索"}, "周杰伦", true); err != nil {
		t.Fatal(err)
	}
	if typed != "周杰伦" {
		t.Fatalf("typed %q; Electron SetValue success must still type the query", typed)
	}
}

func TestPickSearchNodeMatchesFindAndInput(t *testing.T) {
	got := pickSearchNode([]mediaUINode{{Role: "edit", Name: "查找歌曲", Y: 8, H: 28}})
	if got == nil || got.Name != "查找歌曲" {
		t.Fatalf("got %+v", got)
	}
}

func TestUiaTreeSparseAndPlayControls(t *testing.T) {
	if !uiaTreeSparse(nil) {
		t.Fatal("empty tree is sparse")
	}
	if !uiaTreeSparse([]mediaUINode{{Role: "button", Name: "设置", Y: 8, H: 24}}) {
		t.Fatal("chrome-only tree is sparse")
	}
	if uiaTreeSparse([]mediaUINode{{Role: "button", Name: "播放", Y: 900, H: 32}}) {
		t.Fatal("play control is not sparse")
	}
	play := pickPlayControl([]mediaUINode{
		{Role: "button", Name: "暂停", Y: 800, H: 32},
		{Role: "button", Name: "播放", Y: 900, H: 32},
	})
	if play == nil || play.Name != "播放" {
		t.Fatalf("play %+v", play)
	}
	shuffle := pickPlayControl([]mediaUINode{
		{Role: "button", Name: "播放", Y: 900, H: 32},
		{Role: "button", Name: "随机播放", Y: 880, H: 32},
	})
	if shuffle == nil || shuffle.Name != "随机播放" {
		t.Fatalf("shuffle %+v", shuffle)
	}
	if !isPlayControlName("随机播放") || !isPlayControlName("Shuffle") {
		t.Fatal("随机播放 must count as play, not chrome")
	}
	if pickPauseControl([]mediaUINode{{Role: "button", Name: "暂停", Y: 900}}) == nil {
		t.Fatal("expected pause control")
	}
	if !playbackLooksPaused([]mediaUINode{{Role: "button", Name: "播放", Y: 900, H: 32}}) {
		t.Fatal("播放 without 暂停 is paused")
	}
	if playbackLooksPaused([]mediaUINode{{Role: "button", Name: "暂停", Y: 900, H: 32}}) {
		t.Fatal("暂停 means playing")
	}
}

func TestParseImageSizeFromCaptureSummary(t *testing.T) {
	w, h := parseImageSize("captured foreground window 1920x1080; use image coordinates 800x600 for cc.mouse_click")
	if w != 800 || h != 600 {
		t.Fatalf("got %dx%d", w, h)
	}
	pts := playClickPoints(800, 600)
	if len(pts) == 0 || pts[0][1] < 500 {
		t.Fatalf("play points %+v should sit on the bottom bar", pts)
	}
}

func TestPlayQishuiRetriesKeyboardWhenUIAEmpty(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	sendForegroundPlay = func(string) error { return nil }
	t.Cleanup(func() { sendForegroundPlay = winexec.SendMediaKey })

	pressedSpace := false
	screenshotClick := false
	playing := false
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolPress:
			var a struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(args, &a)
			if strings.EqualFold(a.Key, "space") {
				pressedSpace = true
				playing = true
			}
			return result("ok"), nil
		case ccapp.ToolKeyboardShortcut:
			var a struct {
				Keys []string `json:"keys"`
			}
			_ = json.Unmarshal(args, &a)
			for _, k := range a.Keys {
				if strings.EqualFold(k, "space") {
					pressedSpace = true
					playing = true
				}
			}
			return result("ok"), nil
		case ccapp.ToolScreenCapture:
			return result("captured foreground window 800x600; use image coordinates 800x600 for cc.mouse_move/cc.mouse_click/cc.mouse_drag"), nil
		case ccapp.ToolMouseClick:
			var a struct {
				Name string `json:"name"`
				X    *int   `json:"x"`
				Y    *int   `json:"y"`
			}
			_ = json.Unmarshal(args, &a)
			if a.X != nil && a.Y != nil {
				screenshotClick = true
			}
			return result("clicked"), nil
		case ccapp.ToolWait:
			if playing {
				return result("captured desktop after wait 800x600; use image coordinates 800x600"), nil
			}
			return result("waited 700ms; screen unchanged"), nil
		case ccapp.ToolObserveUI:
			nodes := []mediaUINode{}
			if playing {
				nodes = []mediaUINode{{Role: "button", Name: "暂停", Y: 900, H: 32, W: 32}}
			}
			raw, _ := json.Marshal(map[string]any{"nodes": nodes})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "汽水音乐"}, nil
		case ccapp.ToolWindowFocus, ccapp.ToolSetValue, ccapp.ToolKeyboardType:
			return result("ok"), nil
		default:
			return result("ok"), nil
		}
	}
	res, err := playNamedTrackInForeground(context.Background(), invoke, "s1", "热门", "汽水音乐", true)
	if err != nil {
		t.Fatal(err)
	}
	if !pressedSpace && !screenshotClick {
		t.Fatal("UIA-empty 汽水 play must retry with Space or screenshot click")
	}
	if !strings.Contains(res.Output, "playing") {
		t.Fatalf("output %q", res.Output)
	}
}

func TestPlayQishuiDoesNotClaimSuccessIfStillPaused(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	sendForegroundPlay = func(string) error { return nil }
	t.Cleanup(func() { sendForegroundPlay = winexec.SendMediaKey })

	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			nodes := []mediaUINode{{Role: "button", Name: "播放", Y: 900, H: 32, W: 32}}
			raw, _ := json.Marshal(map[string]any{"nodes": nodes})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "汽水音乐"}, nil
		case ccapp.ToolScreenCapture:
			return result("captured foreground window 800x600; use image coordinates 800x600 for cc.mouse_click"), nil
		case ccapp.ToolWait:
			return result("waited 700ms; screen unchanged"), nil
		case ccapp.ToolMouseClick, ccapp.ToolPress, ccapp.ToolKeyboardShortcut, ccapp.ToolKeyboardType, ccapp.ToolSetValue, ccapp.ToolWindowFocus:
			return result("ok"), nil
		default:
			return result("ok"), nil
		}
	}
	res, err := playNamedTrackInForeground(context.Background(), invoke, "s1", "热门", "汽水音乐", true)
	if err == nil {
		t.Fatalf("must not claim success while 播放 is still visible, got %q", res.Output)
	}
	if strings.Contains(res.Output, "verified") || strings.Contains(res.Output, "started playing") {
		t.Fatalf("success leaked into result: %q", res.Output)
	}
	if !strings.Contains(err.Error(), "仍暂停") && !strings.Contains(err.Error(), "未能") {
		t.Fatalf("want still-paused failure, got %v", err)
	}
}

func TestPlayQishuiEmptyTreeUnchangedDoesNotSucceed(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	sendForegroundPlay = func(string) error { return nil }
	t.Cleanup(func() { sendForegroundPlay = winexec.SendMediaKey })

	pressed := false
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolPress:
			var a struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(args, &a)
			if strings.EqualFold(a.Key, "space") {
				pressed = true
			}
			return result("ok"), nil
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "汽水音乐"}, nil
		case ccapp.ToolScreenCapture:
			return result("captured foreground window 800x600; use image coordinates 800x600 for cc.mouse_click"), nil
		case ccapp.ToolWait:
			return result("waited 700ms; screen unchanged"), nil
		default:
			return result("ok"), nil
		}
	}
	res, err := playNamedTrackInForeground(context.Background(), invoke, "s1", "热门", "汽水音乐", true)
	if err == nil {
		t.Fatalf("empty UIA + unchanged pixels must not claim success, got %q", res.Output)
	}
	if !pressed {
		t.Fatal("should still try Space when the tree is empty")
	}
	if strings.Contains(res.Output, "verified") || strings.Contains(res.Output, "started playing") {
		t.Fatalf("success leaked: %q", res.Output)
	}
}
