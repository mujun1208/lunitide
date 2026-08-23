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
