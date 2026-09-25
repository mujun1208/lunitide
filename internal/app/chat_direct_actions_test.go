package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
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
	if call.Name != "web.fetch" || !strings.Contains(string(call.Arguments), "https://news.example/first") {
		t.Fatalf("%s %s", call.Name, call.Arguments)
	}
}

func TestNamedSongOpensNeteaseBeforeTheModel(t *testing.T) {
	args := directSongPlayArgs("帮我播放一首生所爱")
	s := string(args)
	if !strings.Contains(s, "https://music.163.com/#/search/m/?s=%E7%94%9F%E6%89%80%E7%88%B1") || !strings.Contains(s, `"action":"open"`) {
		t.Fatal(s)
	}
	if strings.Contains(s, `"target":"center"`) || strings.Contains(s, "iqiyi.com") || strings.Contains(s, "archive.org") || strings.Contains(s, "Night") {
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
}

func TestNewsOpenDoesNotMatchBareSearch(t *testing.T) {
	if newsOpenGoal("帮我搜索新闻") {
		t.Fatal("search alone is not open-the-first")
	}
	if !newsOpenGoal("点开浏览器的第一条新闻让我查看") {
		t.Fatal("open the first news link")
	}
}
