package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

func TestMediaKeyWithoutReadbackIsUncertain(t *testing.T) {
	origActivate := activateWindow
	origSession := mediaSessionAction
	origPlay := sendForegroundPlay
	origClick := clickMusicTransport
	origSleep := mediaSleep
	t.Cleanup(func() {
		activateWindow = origActivate
		mediaSessionAction = origSession
		sendForegroundPlay = origPlay
		clickMusicTransport = origClick
		mediaSleep = origSleep
	})
	activateWindow = func(string) error { return nil }
	mediaSleep = func(time.Duration) {}
	clickMusicTransport = func(string) error { return errors.New("no window") }
	mediaSessionAction = func(context.Context, []string, string, bool) (winexec.MediaSessionResult, error) {
		return winexec.MediaSessionResult{}, errors.New("no smtc")
	}
	played := false
	sendForegroundPlay = func(string) error {
		played = true
		return nil
	}
	invoke := func(context.Context, string, string, json.RawMessage, bool) (Result, error) {
		return Result{Output: `{"count":0,"nodes":[]}`}, nil
	}
	res, err := executeMediaPlayForeground(context.Background(), invoke, "s1", "随机播放", "汽水音乐", true, true)
	if err != nil || !played {
		t.Fatalf("got %+v %v played=%v", res, err, played)
	}
	if strings.Contains(res.Output, `"passed":true`) || !strings.Contains(res.Output, `"uncertain":true`) || !strings.Contains(res.Output, "MEDIA_UNVERIFIED") {
		t.Fatalf("key-only play must stay unverified: %s", res.Output)
	}
	if strings.Contains(res.Output, "已播放") || strings.Contains(res.Output, "播放成功") {
		t.Fatalf("chinese copy must not claim playback: %s", res.Output)
	}
	if res.Receipt == nil {
		t.Fatal("operation receipt required")
	}
	if res.Receipt.Phase != "uncertain" || res.Receipt.VerificationStatus != "unconfirmed" || res.Receipt.VerificationSource != "none" {
		t.Fatalf("receipt %+v", res.Receipt)
	}
	if res.Receipt.ErrorCode != "MEDIA_UNVERIFIED" {
		t.Fatalf("error code %q", res.Receipt.ErrorCode)
	}
}

func TestMediaKeyConfirmedByLaterSession(t *testing.T) {
	origActivate := activateWindow
	origSession := mediaSessionAction
	origPlay := sendForegroundPlay
	origClick := clickMusicTransport
	origSleep := mediaSleep
	t.Cleanup(func() {
		activateWindow = origActivate
		mediaSessionAction = origSession
		sendForegroundPlay = origPlay
		clickMusicTransport = origClick
		mediaSleep = origSleep
	})
	activateWindow = func(string) error { return nil }
	mediaSleep = func(time.Duration) {}
	clicks := 0
	clickMusicTransport = func(string) error {
		clicks++
		return nil
	}
	calls := 0
	mediaSessionAction = func(context.Context, []string, string, bool) (winexec.MediaSessionResult, error) {
		calls++
		if calls == 1 {
			return winexec.MediaSessionResult{}, errors.New("no smtc yet")
		}
		return winexec.MediaSessionResult{Verified: true, Status: "Playing", Title: "随机歌曲", Artist: "汽水"}, nil
	}
	played := false
	sendForegroundPlay = func(string) error {
		played = true
		return nil
	}
	invoke := func(context.Context, string, string, json.RawMessage, bool) (Result, error) {
		t.Fatal("session confirm must not hunt the UI")
		return Result{}, errors.New("unexpected")
	}
	res, err := executeMediaPlayForeground(context.Background(), invoke, "s1", "随机播放", "汽水音乐", true, true)
	if err != nil || played || clicks != 1 {
		t.Fatalf("got %+v %v played=%v clicks=%d", res, err, played, clicks)
	}
	if !strings.Contains(res.Output, "verified playing") || !strings.Contains(res.Output, `"passed":true`) {
		t.Fatalf("transport click then session must verify: %s", res.Output)
	}
}
