package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/winexec"
)

func TestComputerPlayInstructionIsNotASongTitle(t *testing.T) {
	q, ok := ComputerPlayQuery("那你换一种用电脑操作的方式去播放啊电脑操作的方式")
	if !ok || q != "播放一首歌" {
		t.Fatalf("instruction query = %q ok=%v", q, ok)
	}
	q, ok = ComputerPlayQuery("用电脑操作播放晴天")
	if !ok || q != "晴天" {
		t.Fatalf("named instruction query = %q ok=%v", q, ok)
	}
	if _, ok := ComputerPlayQuery("晴天"); ok {
		t.Fatal("a real title must stay a title")
	}
	if _, ok := ComputerPlayQuery("周杰伦"); ok {
		t.Fatal("an artist must stay an artist")
	}
}

func TestInstructionPlaySucceedsWhenPlayerAlreadyShowsPause(t *testing.T) {
	dir := t.TempDir()
	origMemory := musicPlayMemoryOverride
	origActivate := activateWindow
	origSleep := mediaSleep
	origSession := mediaSessionAction
	origPlay := sendForegroundPlay
	origClick := clickMusicTransport
	t.Cleanup(func() {
		musicPlayMemoryOverride = origMemory
		activateWindow = origActivate
		mediaSleep = origSleep
		mediaSessionAction = origSession
		sendForegroundPlay = origPlay
		clickMusicTransport = origClick
	})
	musicPlayMemoryOverride = filepath.Join(dir, "music-play.json")
	activateWindow = func(string) error { return nil }
	mediaSleep = func(time.Duration) {}
	mediaSessionAction = func(context.Context, []string, string, bool) (winexec.MediaSessionResult, error) {
		t.Fatal("already-playing player must not take another session action")
		return winexec.MediaSessionResult{}, nil
	}
	keyed := false
	sendForegroundPlay = func(string) error {
		keyed = true
		return nil
	}
	clickMusicTransport = func(string) error {
		t.Fatal("already-playing player must not be clicked")
		return nil
	}
	typed := false
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolKeyboardType, ccapp.ToolPaste, ccapp.ToolSetValue:
			typed = true
			return result("typed"), nil
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "button", Name: "暂停"},
				{Role: "button", Name: "有歌碟"},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "汽水音乐"}, nil
		default:
			if strings.Contains(string(args), "电脑操作") {
				typed = true
			}
			return Result{Output: `{"nodes":[]}`}, nil
		}
	}
	res, err := executeMediaPlayForeground(context.Background(), invoke, "s1", "那你换一种用电脑操作的方式去播放啊电脑操作的方式", "汽水音乐", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "verified playing") || !strings.Contains(res.Output, `"passed":true`) {
		t.Fatalf("already-playing instruction must verify: %s", res.Output)
	}
	if keyed || typed {
		t.Fatalf("must not toggle or type the instruction, keyed=%v typed=%v", keyed, typed)
	}
	raw, err := os.ReadFile(musicPlayMemoryOverride)
	if err != nil || !strings.Contains(string(raw), "汽水音乐") {
		t.Fatalf("success must be remembered, err=%v body=%s", err, raw)
	}
	if got := PreferredMusicApp([]string{"网易云音乐", "汽水音乐"}); got != "汽水音乐" {
		t.Fatalf("next play must reuse the remembered player, got %q", got)
	}
}

func TestNamedTrackFailureDoesNotRemember(t *testing.T) {
	dir := t.TempDir()
	origMemory := musicPlayMemoryOverride
	origActivate := activateWindow
	origSleep := mediaSleep
	origPlay := sendForegroundPlay
	origRead := readForegroundFn
	t.Cleanup(func() {
		musicPlayMemoryOverride = origMemory
		activateWindow = origActivate
		mediaSleep = origSleep
		sendForegroundPlay = origPlay
		readForegroundFn = origRead
	})
	musicPlayMemoryOverride = filepath.Join(dir, "music-play.json")
	activateWindow = func(string) error { return nil }
	mediaSleep = func(time.Duration) {}
	sendForegroundPlay = func(string) error { return nil }
	readForegroundFn = func() (string, string, error) { return "汽水音乐", "sodamusic.exe", nil }
	invoke := func(_ context.Context, _, tool string, _ json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "button", Name: "暂停"},
				{Role: "button", Name: "有歌碟"},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolGetActiveWindow:
			return Result{Output: "汽水音乐"}, nil
		default:
			return Result{}, nil
		}
	}
	res, err := executeMediaPlayForeground(context.Background(), invoke, "s1", "用电脑操作播放复古公路歌", "汽水音乐", true, true)
	if err == nil || !strings.Contains(err.Error(), "复古公路歌") {
		t.Fatalf("named track must still fail closed, got %v output=%s", err, res.Output)
	}
	if _, statErr := os.Stat(musicPlayMemoryOverride); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed play must not be remembered, stat=%v", statErr)
	}
}
