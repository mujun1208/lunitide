package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestLiveDesktopObservationsAreNotDeduplicated(t *testing.T) {
	for _, action := range []string{"screenshot", "observe", "wait", "get_active_window"} {
		raw, _ := json.Marshal(map[string]string{"action": action})
		if !liveDesktopObservation("computer.act", raw) {
			t.Fatal(action)
		}
	}
	if liveDesktopObservation("computer.act", json.RawMessage(`{"action":"click"}`)) {
		t.Fatal("mutations must retain duplicate protection")
	}
}

func TestDesktopTransportToolsSurviveTaskRouting(t *testing.T) {
	for _, goal := range []string{"打开桌面的默认浏览器，打开https://www.bing.com", "彻底退出汽水音乐和微信"} {
		route, allow := classifyTaskRoute(goal, true, true)
		if route != RouteR2 || !allow["desktop.browse"] || !allow["desktop.quit"] || !allow["computer.act"] {
			t.Fatalf("%s: %s %v", goal, route, allow)
		}
	}
	for _, tool := range []string{"desktop.browse", "desktop.quit"} {
		if !companionToolPreapproved(tool, false, true) || companionToolPreapproved(tool, false, false) {
			t.Fatal("standing CC approval mismatch: " + tool)
		}
		if guardCurrentTurnTool("今天上海天气怎么样", tool) == nil {
			t.Fatal("lookup-only turn must not execute stale desktop work: " + tool)
		}
	}
}

func TestSpokenPlaybackCloseoutDoesNotCreateOfficeDocument(t *testing.T) {
	goal := "打开桌面的汽水音乐，随机播放一首歌曲。必须核对是否开始播放，结果只用一句话报告。"
	if looksLikeReportTask(goal) || officeGenToolForGoal(goal) != "" {
		t.Fatal("playback closeout routed to document generation")
	}
	if !looksLikeReportTask("生成一份测试报告文档") {
		t.Fatal("explicit report creation lost")
	}
}

func TestMusicQueryDoesNotIncludeSpokenCloseoutInstructions(t *testing.T) {
	goal := "帮我打开桌面汽水音乐，随机播放一首歌曲。结果只用一句话报告。"
	if got := companionDefaultMusicQuery(goal); got != "热门" {
		t.Fatal(got)
	}
	args := mediaArgsForGoal(goal, json.RawMessage(`{"action":"play","query":"结果只用一句话报告"}`))
	var fields map[string]string
	_ = json.Unmarshal(args, &fields)
	if fields["query"] != "random" || fields["app"] != "汽水音乐" {
		t.Fatalf("%s", args)
	}
	if got := companionDefaultMusicQuery("打开汽水音乐，播放一首周杰伦的歌曲，结果只用一句话报告"); got != "周杰伦" {
		t.Fatal(got)
	}
}

func TestDesktopCloseoutMatchesObservedResults(t *testing.T) {
	for action, want := range map[string]string{"next": "已切换到下一首并开始播放。", "pause": "已暂停播放。", "stop": "已停止播放。"} {
		out := "verified " + action + " in player\n" + `{"l0":{"kind":"media-session","passed":true,"uncertain":false}}`
		if got := mediaControlReceiptSpeech(out); got != want {
			t.Fatalf("%s: %s", action, got)
		}
	}
	browse := receiptMessages("desktop.browse", `{}`, "已向系统默认桌面浏览器发送打开请求：https://www.bing.com")
	if got := companionFinalResult(browse, "页面正常", "打开默认浏览器"); got != "已向默认浏览器发送打开请求，尚未核对页面。" {
		t.Fatal(got)
	}
	media := receiptMessages("media.play", `{}`, "verified playing in player; shuffle=false\n"+`{"l0":{"kind":"media-session","passed":true,"uncertain":false}}`)
	if got := companionFinalResult(media, "随机播放成功", "随机播放歌曲"); got != "已开始播放，但未确认随机模式。" {
		t.Fatal(got)
	}
}

func TestSeedanceModelUsesTaskAPIWithoutChangingImageRoute(t *testing.T) {
	e := &Engine{}
	var base string
	e.adapterFactory = func(_ context.Context, p provider.Provider) (llmadapter.Adapter, error) {
		base = p.BaseURL
		return nil, nil
	}
	p := provider.Provider{Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://z.apiyihe.org/v1/images/generations"}
	_, _ = e.adapterForModel(context.Background(), p, provider.Model{ModelID: "doubao-seedance-2-0-260128", Kind: provider.KindVideo})
	if base != "https://z.apiyihe.org/api/v3" {
		t.Fatal(base)
	}
	_, _ = e.adapterForModel(context.Background(), p, provider.Model{ModelID: "doubao-seedream-5-0-260128", Kind: provider.KindImage})
	if base != p.BaseURL {
		t.Fatal("image route changed: " + base)
	}
}
