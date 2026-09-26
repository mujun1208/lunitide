package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestFirstSearchHitSkipsTheSearchPage(t *testing.T) {
	text := "query: 新闻\nresults_url: https://cn.bing.com/search?q=news\n\n1. 第一条\n   https://news.example/first\n\n2. 第二条\n   https://news.example/second\n"
	if got := firstSearchHitURL(text); got != "https://news.example/first" {
		t.Fatal(got)
	}
	messages := []llmadapter.Message{{Role: llmadapter.RoleTool, Content: text}}
	if got := firstSearchHitFromMessages(messages); got != "https://news.example/first" {
		t.Fatal(got)
	}
	e := &Engine{}
	e.rememberSearchHit("sess", text)
	if got := e.savedSearchHit("sess", nil); got != "https://news.example/first" {
		t.Fatal(got)
	}
	call := llmadapter.ToolCall{Name: "computer.act", Arguments: []byte(`{"action":"click","id":"83"}`)}
	rewriteNewsOpen("点开浏览器的第一条新闻让我查看", "sess", e, nil, &call)
	if call.Name != "desktop.browse" || !strings.Contains(string(call.Arguments), "https://news.example/first") {
		t.Fatalf("%s %s", call.Name, call.Arguments)
	}
}

func TestNamedSongOpensNeteaseBeforeTheModel(t *testing.T) {
	args := directSongPlayArgs("帮我播放一首生所爱")
	s := string(args)
	if !strings.Contains(s, `"target":"center"`) || !strings.Contains(s, "生所爱") || strings.Contains(s, "music.163.com") || strings.Contains(s, `"action":"open"`) {
		t.Fatal(s)
	}
	if strings.Contains(namedSongSpeech, "供应商拒绝了请求") {
		t.Fatal(namedSongSpeech)
	}
	if directSongPlayArgs("播放一部周星驰的电影，九品芝麻官") != nil {
		t.Fatal("a named film must stay on the film path")
	}
	if directSongPlayArgs("你试试爱奇艺能不能播放") != nil || !moviePlayGoal("你试试爱奇艺能不能播放") {
		t.Fatal("an iQiyi playback retry must stay on the film path")
	}
	if !moviePlayGoal("帮我找一部香港90年代的电影播放") {
		t.Fatal("a film request must stay on the film path")
	}
	if directSongPlayArgs("帮我播放一首歌") != nil && strings.Contains(string(directSongPlayArgs("帮我播放一首歌")), "生所爱") {
		t.Fatal("generic play must not reuse a previous title")
	}
	if directSongPlayArgs("在我的媒体中心播放") != nil || directSongPlayArgs("我让你播放的是电影 不要歌曲") != nil || directSongPlayArgs("已经登录了，播放吧") != nil && moviePlayGoal("帮我播放电影 武状元苏乞儿") && carryFilmGoal("帮我播放电影 武状元苏乞儿", "已经登录了，播放吧") == "已经登录了，播放吧" {
		t.Fatal("a movie follow-up must not open NetEase")
	}
	if got := carryFilmGoal("帮我播放电影 武状元苏乞儿", "播放武状元苏乞儿"); !moviePlayGoal(got) || !strings.Contains(got, "武状元苏乞儿") {
		t.Fatal(got)
	}
	if got := carryFilmGoal("帮我播放电影 武状元苏乞儿", "已经登录了，播放吧"); got != "帮我播放电影 武状元苏乞儿" {
		t.Fatal(got)
	}
	if got := carryFilmGoal("帮我播放一首生所爱", "播放吧"); got != "播放吧" {
		t.Fatal(got)
	}
	for _, goal := range []string{"比方电影", "播放电影", "播放武状元苏乞儿", "播放九品芝麻官", "我想看夜访吸血鬼", "我让你播放的是电影 不要歌曲", "在我的媒体中心播放"} {
		if goal != "在我的媒体中心播放" && !moviePlayGoal(goal) {
			t.Fatalf("%s must be a film", goal)
		}
		if directSongPlayArgs(goal) != nil {
			t.Fatalf("%s must not open NetEase: %s", goal, directSongPlayArgs(goal))
		}
		t.Logf("「%s」不打开网易云、爱奇艺、优酷", goal)
	}
	if moviePlayGoal("播放武状元苏乞儿的歌") {
		t.Fatal("a song of that title stays a song")
	}
}

func TestNewsOpenDoesNotMatchBareSearch(t *testing.T) {
	if newsOpenGoal("帮我搜索新闻") {
		t.Fatal("search alone is not open-the-first")
	}
	if !newsOpenGoal("点开浏览器的第一条新闻让我查看") {
		t.Fatal("open the first news link")
	}
	if !newsOpenGoal("打开第一个新闻链接") || !newsOpenGoal("第一个新闻链接") {
		t.Fatal("the first news link is the open-the-first goal")
	}
	if newsOpenGoal("打开浏览器，查询古天乐的最新新闻") {
		t.Fatal("searching the news is not opening the first link")
	}
	if newsOpenGoal("打开第一条") || newsOpenGoal("打开第一个") || newsOpenGoal("点开第一条") || newsOpenGoal("点第一条") {
		t.Fatal("a cut ordinal must not open")
	}
	if !newsOpenGoal("打开第一条新闻") || !newsOpenGoal("点开第一条新闻") {
		t.Fatal("the finished news phrase is the open")
	}
	if !newsOpenGoal("打开第一个链接") {
		t.Fatal("open the first link")
	}
}

func TestFirstOpenTargetUsesTheBrowserSearchJustDone(t *testing.T) {
	browsed := []llmadapter.Message{{
		Role:    llmadapter.RoleTool,
		Content: "已打开桌面浏览器：https://www.bing.com/search?q=%E5%8F%A4%E5%A4%A9%E4%B9%90%E6%9C%80%E6%96%B0%E6%96%B0%E9%97%BB",
	}}
	url, query := firstOpenTarget(browsed)
	if url != "" || query != "古天乐最新新闻" {
		t.Fatalf("url=%q query=%q", url, query)
	}
	older := []llmadapter.Message{
		{Role: llmadapter.RoleTool, Content: "1. 旧结果\n   https://news.example/old\n"},
		browsed[0],
	}
	url, query = firstOpenTarget(older)
	if url != "" || query != "古天乐最新新闻" {
		t.Fatalf("newer search must win: url=%q query=%q", url, query)
	}
	found := append(browsed, llmadapter.Message{
		Role:    llmadapter.RoleTool,
		Content: "query: 古天乐最新新闻\nresults_url: https://www.bing.com/search?q=news\n\n1. 第一条\n   https://news.example/first\n",
	})
	url, query = firstOpenTarget(found)
	if url != "https://news.example/first" || query != "" {
		t.Fatalf("url=%q query=%q", url, query)
	}
}

func TestOpenFirstLinkDoesNotClaimSuccessWithoutTheBrowser(t *testing.T) {
	e := &Engine{}
	e.rememberSearchHit("sess", "1. 第一条\n   https://news.example/first\n")
	var names []string
	speech, ok := e.openFirstNewsNow(context.Background(), executionModeApproval, "sess", "打开第一个链接", nil, func(ev bridge.Event) error {
		if ev.Tool != nil && ev.Type == bridge.EventToolCompleted {
			names = append(names, ev.Tool.Name)
		}
		return nil
	})
	if ok && !strings.Contains(strings.Join(names, ","), "desktop.browse") {
		t.Fatalf("claimed %q via %v", speech, names)
	}
}

func TestOpenFirstLinkSearchesThenOpensTheFirstResult(t *testing.T) {
	e := &Engine{}
	e.tools = &toolruntime.Runtime{}
	var calls []string
	prev := executeDirectTool
	executeDirectTool = func(_ *Engine, _ context.Context, _ executionMode, _, name string, args json.RawMessage) (toolruntime.Result, error) {
		calls = append(calls, name+" "+string(args))
		if name == "web.search" {
			t.Fatal("opening the first link must read the page already open")
		}
		return toolruntime.Result{Output: "已打开桌面浏览器：https://news.example/on-page"}, nil
	}
	t.Cleanup(func() { executeDirectTool = prev })
	var clicked bool
	prevHost := resultClickHost
	resultClickHost = func() resultClicker {
		return fakeResultClicker{
			windows: []ccapp.WindowInfo{{Title: "古天乐最新新闻 - 搜索", Process: "msedge.exe"}},
			nodes: []ccapp.UINode{
				{Role: "link", Name: "图片"},
				{Role: "link", Name: "新闻"},
				{Role: "link", Name: "古天乐最新动态"},
			},
			onInvoke: func(name string) {
				clicked = name == "古天乐最新动态"
			},
		}
	}
	t.Cleanup(func() { resultClickHost = prevHost })
	messages := []llmadapter.Message{{
		Role:    llmadapter.RoleTool,
		Content: "已打开桌面浏览器：https://www.bing.com/search?q=%E5%8F%A4%E5%A4%A9%E4%B9%90%E6%9C%80%E6%96%B0%E6%96%B0%E9%97%BB",
	}}
	speech, ok := e.openFirstNewsNow(context.Background(), executionModeApproval, "sess", "打开第一个链接", messages, func(bridge.Event) error { return nil })
	if !ok || speech != "已经打开第一条。" {
		t.Fatalf("speech=%q ok=%v", speech, ok)
	}
	if !clicked {
		t.Fatal("must click the first result already on the open page")
	}
	if len(calls) != 0 {
		t.Fatal(strings.Join(calls, "\n"))
	}
}

type fakeResultClicker struct {
	windows  []ccapp.WindowInfo
	nodes    []ccapp.UINode
	onInvoke func(string)
}

func (f fakeResultClicker) Available() bool { return true }
func (f fakeResultClicker) ListWindows() ([]ccapp.WindowInfo, error) {
	return f.windows, nil
}
func (f fakeResultClicker) FocusWindow(string) (ccapp.WindowInfo, error) {
	return f.windows[0], nil
}
func (f fakeResultClicker) ObserveUI(int) ([]ccapp.UINode, error) { return f.nodes, nil }
func (f fakeResultClicker) InvokeUI(name string) error {
	if f.onInvoke != nil {
		f.onInvoke(name)
	}
	return nil
}

func TestCutOrdinalDoesNotOpen(t *testing.T) {
	e := &Engine{}
	e.tools = &toolruntime.Runtime{}
	var invoked string
	prevHost := resultClickHost
	resultClickHost = func() resultClicker {
		return fakeResultClicker{
			windows: []ccapp.WindowInfo{{Title: "搜索", Process: "msedge.exe"}},
			nodes:   []ccapp.UINode{{Role: "link", Name: "古天乐最新动态"}},
			onInvoke: func(name string) { invoked = name },
		}
	}
	t.Cleanup(func() { resultClickHost = prevHost })
	messages := []llmadapter.Message{{
		Role:    llmadapter.RoleTool,
		Content: "已打开桌面浏览器：https://www.bing.com/search?q=news",
	}}
	speech, ok := e.openFirstNewsNow(context.Background(), executionModeApproval, "sess", "打开第一条", messages, func(bridge.Event) error { return nil })
	if ok || speech != "" || invoked != "" {
		t.Fatalf("speech=%q ok=%v invoked=%q", speech, ok, invoked)
	}
}
