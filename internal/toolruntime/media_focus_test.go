package toolruntime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestMediaPlayerRefusesDocumentBeforeAnyLaunch(t *testing.T) {
	previous := openLaunchPath
	defer func() { openLaunchPath = previous }()
	openLaunchPath = func(string) error { t.Fatal("media.play opened a document"); return nil }
	if _, err := ensureMusicAppForeground("企业AI智能助手.txt"); err == nil {
		t.Fatal("document accepted as player")
	}
}

func TestDesktopInputFocusStaysWithItsConcurrentTask(t *testing.T) {
	var workers sync.WaitGroup
	for _, window := range []string{"汽水音乐", "合同文档", "微信"} {
		workers.Add(1)
		go func(window string) {
			defer workers.Done()
			ctx := withMediaInputWindow(context.Background(), window)
			invoke := func(_ context.Context, _, _ string, raw json.RawMessage, _ bool) (Result, error) {
				var args map[string]any
				if err := json.Unmarshal(raw, &args); err != nil {
					t.Error(err)
				}
				if args["window"] != window {
					t.Errorf("input sent to %v; want %s", args["window"], window)
				}
				return Result{}, nil
			}
			for i := 0; i < 30; i++ {
				if err := ccType(ctx, invoke, "session", "2100040404301", true); err != nil {
					t.Error(err)
				}
				if err := ccShortcut(ctx, invoke, "session", true, "ctrl", "f"); err != nil {
					t.Error(err)
				}
			}
		}(window)
	}
	workers.Wait()
	if _, exists := ccFocusArgs(context.Background(), nil)["window"]; exists {
		t.Fatal("focus leaked into an unrelated operation")
	}
}
