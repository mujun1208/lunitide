package toolruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/winexec"
)

func TestMusicSessionRequiresVerifiedPlayback(t *testing.T) {
	original := mediaSessionAction
	t.Cleanup(func() { mediaSessionAction = original })
	for _, verified := range []bool{false, true} {
		mediaSessionAction = func(_ context.Context, aliases []string, action string, shuffle bool) (winexec.MediaSessionResult, error) {
			if action != "play" || !shuffle || aliases[0] != "汽水音乐" {
				t.Fatalf("wrong target/action: %v %s %v", aliases, action, shuffle)
			}
			return winexec.MediaSessionResult{Status: "Playing", Title: "Test track", Verified: verified}, nil
		}
		res, ok := controlMusicSession(context.Background(), "汽水", "play", true)
		if !ok || strings.Contains(res.Output, "verified playing") != verified {
			t.Fatalf("verified=%v got %+v %v", verified, res, ok)
		}
	}
}

func TestGenericMediaRequestsDoNotBecomeSongSearches(t *testing.T) {
	for _, query := range []string{"播放一首歌", "随机播放一首歌曲", "一首歌曲", "随便放一首", "播放音乐", "随机播放"} {
		if !isGenericMediaQuery(query) {
			t.Fatal(query)
		}
	}
	for _, query := range []string{"", "周杰伦", "晴天", "播放周杰伦的歌曲", "随机播放周杰伦"} {
		if isGenericMediaQuery(query) {
			t.Fatal("named request treated as generic: " + query)
		}
	}
}

func TestMusicSessionTimeoutDoesNotDispatchSecondAction(t *testing.T) {
	original := mediaSessionAction
	t.Cleanup(func() { mediaSessionAction = original })
	mediaSessionAction = func(context.Context, []string, string, bool) (winexec.MediaSessionResult, error) {
		return winexec.MediaSessionResult{}, context.DeadlineExceeded
	}
	res, handled := controlMusicSession(context.Background(), "汽水", "next", false)
	if !handled || !strings.Contains(res.Output, "unconfirmed") {
		t.Fatalf("ambiguous action must not be repeated: %+v %v", res, handled)
	}
}

func TestMusicSessionUnsupportedAppDoesNotControlAnotherPlayer(t *testing.T) {
	original := mediaSessionAction
	t.Cleanup(func() { mediaSessionAction = original })
	mediaSessionAction = func(context.Context, []string, string, bool) (winexec.MediaSessionResult, error) {
		t.Fatal("must not dispatch unknown application")
		return winexec.MediaSessionResult{}, errors.New("unexpected")
	}
	if _, ok := controlMusicSession(context.Background(), "unknown app", "play", false); ok {
		t.Fatal("unknown app was controlled")
	}
}
