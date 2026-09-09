package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestRandomPlaybackUsesPlayAllWithoutSearchingHot(t *testing.T) {
	old := mediaSleep
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = old })
	clicked := false
	invoke := func(ctx context.Context, session, tool string, raw json.RawMessage, approved bool) (Result, error) {
		var a map[string]any
		_ = json.Unmarshal(raw, &a)
		switch tool {
		case ccapp.ToolObserveUI:
			if clicked {
				return result(`{"nodes":[{"role":"button","name":"暂停"}]}`), nil
			}
			return result(`{"nodes":[{"role":"button","name":"播放全部 135"},{"role":"edit","name":"搜索"}]}`), nil
		case ccapp.ToolMouseClick:
			if a["name"] != "播放全部 135" || a["window"] != "汽水音乐" || a["clicks"] != float64(1) {
				t.Fatalf("unexpected click: %s", raw)
			}
			clicked = true
		case ccapp.ToolKeyboardType, ccapp.ToolPaste, ccapp.ToolSetValue:
			t.Fatalf("random playback must not search: %s", raw)
		}
		return result("ok"), nil
	}
	out, err := playNamedTrackInForeground(context.Background(), invoke, "session", "热门", "汽水音乐", true)
	if err != nil || !clicked || !strings.Contains(out.Output, "verified playing") {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestMediaDoesNotConfirmPlaybackFromPixelChanges(t *testing.T) {
	if _, ok := confirmGenericPlayback(nil, "汽水音乐", "汽水音乐", "changed", true); ok {
		t.Fatal("screen change is not playback evidence")
	}
	if playbackLooksPaused([]mediaUINode{{Name: "播放全部 135"}}) {
		t.Fatal("playlist action is not paused status")
	}
}

func TestMediaPropagatesNestedComputerFailure(t *testing.T) {
	invoke := func(context.Context, string, string, json.RawMessage, bool) (Result, error) {
		return result("ok:false COMPUTER_STALE_FRAME"), nil
	}
	if err := ccClickName(context.Background(), invoke, "s", "播放", 1, true); err == nil {
		t.Fatal("nested rejected input must fail")
	}
}
