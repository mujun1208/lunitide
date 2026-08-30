package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestResolveMediaPlayArgsUsesSessionMusicApp(t *testing.T) {
	root := t.TempDir()
	tools, err := toolruntime.New(root)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	e.saveCompanionContext("s1", companionActionContext{
		ActiveAppName: "汽水音乐",
		Kind:          "music_app",
	})
	out := e.resolveMediaPlayArgs("s1", json.RawMessage(`{"action":"play","query":"周杰伦","target":"auto"}`))
	var parsed map[string]string
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["target"] != "foreground" {
		t.Fatalf("target = %q", parsed["target"])
	}
	if parsed["app"] != "汽水音乐" {
		t.Fatalf("app = %q", parsed["app"])
	}
}

func TestResolveMediaPlayArgsDefaultQueryForRandomPlay(t *testing.T) {
	root := t.TempDir()
	tools, err := toolruntime.New(root)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	e.saveCompanionContext("s1", companionActionContext{
		ActiveAppName: "汽水音乐",
		Kind:          "music_app",
	})
	out := e.resolveMediaPlayArgs("s1", json.RawMessage(`{"action":"play","query":"","target":"auto"}`))
	var parsed map[string]string
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["target"] != "foreground" || parsed["query"] != "热门" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestCompanionAutoMediaPlayArgs(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	e.saveCompanionContext("s1", companionActionContext{ActiveAppName: "汽水音乐", Kind: "music_app"})
	args, ok := e.companionAutoMediaPlayArgs("s1", "打开汽水音乐，播放一首歌曲")
	if !ok {
		t.Fatal("expected auto media play args")
	}
	var parsed map[string]string
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["target"] != "foreground" || parsed["query"] != "热门" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestCompanionExtractMusicQueryKeepsArtist(t *testing.T) {
	if got := companionDefaultMusicQuery("播放一首周杰伦的歌曲"); got != "周杰伦" {
		t.Fatalf("got %q", got)
	}
	if got := companionDefaultMusicQuery("打开网易云音乐，播放一首周杰伦的歌曲"); got != "周杰伦" {
		t.Fatalf("got %q", got)
	}
	if got := companionNamedMusicApp("打开网易云音乐播放周杰伦"); got != "网易云音乐" {
		t.Fatalf("got %q", got)
	}
}

func TestCompanionExtractMusicQueryExactDesktopJayChouUtterance(t *testing.T) {
	const uttered = "打开桌面网易云音乐软件，搜索周杰伦歌曲，放一首"
	if got := companionNamedMusicApp(uttered); got != "网易云音乐" {
		t.Fatalf("app = %q", got)
	}
	if got := companionDefaultMusicQuery(uttered); got != "周杰伦" {
		t.Fatalf("query = %q want 周杰伦 (not 热门, not leftover 打开软件搜索…)", got)
	}
	if got := companionDefaultMusicQuery("随便放一首"); got != "热门" {
		t.Fatalf("generic play = %q", got)
	}
	if got := companionDefaultMusicQuery("随便放一首周杰伦"); got != "周杰伦" {
		t.Fatalf("named artist with 随便 = %q", got)
	}
}

func TestCompanionAutoMediaPlayExactDesktopJayChouUtterance(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	args, ok := e.companionAutoMediaPlayArgs("s1", "打开桌面网易云音乐软件，搜索周杰伦歌曲，放一首")
	if !ok {
		t.Fatal("expected auto media play for desktop 网易云 + 周杰伦")
	}
	var parsed map[string]string
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["target"] != "foreground" || parsed["app"] != "网易云音乐" || parsed["query"] != "周杰伦" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if strings.Contains(string(args), "163.com") || strings.Contains(string(args), "热门") {
		t.Fatalf("must not prefer web or 热门: %s", args)
	}
}

func TestResolveMediaPlayArgsRewritesNeteaseToForeground(t *testing.T) {
	root := t.TempDir()
	tools, err := toolruntime.New(root)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	out := e.resolveMediaPlayArgs("s1", json.RawMessage(`{"action":"play","query":"周杰伦","target":"netease"}`))
	var parsed map[string]string
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["target"] != "foreground" || parsed["app"] != "网易云音乐" || parsed["query"] != "周杰伦" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestCompanionAutoMediaPlayArgsFromUtteranceWithoutSessionApp(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	args, ok := e.companionAutoMediaPlayArgs("s1", "打开网易云音乐，播放一首周杰伦的歌曲")
	if !ok {
		t.Fatal("expected auto media play from named app in the utterance")
	}
	var parsed map[string]string
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["target"] != "foreground" || parsed["app"] != "网易云音乐" || parsed["query"] != "周杰伦" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestCompanionAutoMediaPlayJayChouWithoutNamedApp(t *testing.T) {
	installed := toolruntime.FirstInstalledMusicApp()
	if installed == "" {
		t.Skip("no known desktop music app installed on this PC")
	}
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	args, ok := e.companionAutoMediaPlayArgs("s1", "播放一首周杰伦的歌曲")
	if !ok {
		t.Fatal("expected auto media play on an installed desktop player")
	}
	var parsed map[string]string
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["target"] != "foreground" || parsed["app"] != installed || parsed["query"] != "周杰伦" {
		t.Fatalf("parsed = %#v installed=%q", parsed, installed)
	}
}

func TestCompanionWantsToolsForPlayFollowUp(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	e.saveCompanionContext("s1", companionActionContext{ActiveAppName: "汽水音乐", Kind: "music_app"})
	if !e.companionWantsToolsForTurn("s1", "播放周杰伦") {
		t.Fatal("expected tools for play follow-up with active app")
	}
	if !e.companionWantsToolsForTurn("s1", "周杰伦") {
		t.Fatal("expected tools for bare artist name with music app open")
	}
	if !e.companionWantsToolsForTurn("s1", "随便放一首") {
		t.Fatal("expected tools for random play follow-up")
	}
	if e.companionWantsToolsForTurn("s1", "你好") {
		t.Fatal("expected no tools for idle chat")
	}
}

func TestCompanionSessionInjection(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	e.saveCompanionContext("s1", companionActionContext{
		ActiveAppName: "汽水音乐",
		ActiveAppPath: `C:\Users\me\Desktop\汽水音乐.lnk`,
		Kind:          "music_app",
	})
	got := e.companionSessionInjection("s1", "播放亚森")
	if got == "" || !strings.Contains(got, "汽水音乐") || !strings.Contains(got, "foreground") {
		t.Fatalf("injection = %q", got)
	}
}

func TestCompanionWantsToolsForDesktopFollowUp(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	e.noteCompanionToolSuccess("s1", "cc.mouse_click", json.RawMessage(`{"name":"保存"}`), "clicked 保存")
	if !e.companionWantsToolsForTurn("s1", "继续填表") {
		t.Fatal("expected tools after recent desktop act")
	}
	if e.companionWantsToolsForTurn("s1", "你好") {
		t.Fatal("idle chat must stay tool-free")
	}
	if !companionDesktopToolLoop(e, "s1", "继续") {
		t.Fatal("DesktopActive session must start the 24-step desktop loop")
	}
	if companionDesktopToolLoop(e, "", "今晚月色如何") {
		t.Fatal("idle chat must keep the short companion loop")
	}
}

func TestNoteCompanionDesktopOpen(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	e.noteCompanionToolSuccess("s1", "desktop.open", json.RawMessage(`{"name":"汽水音乐"}`), "opened C:\\Users\\me\\Desktop\\汽水音乐.lnk")
	ctx := e.loadCompanionContext("s1")
	if ctx.ActiveAppName != "汽水音乐" || ctx.Kind != "music_app" {
		t.Fatalf("ctx = %+v", ctx)
	}
}
