//go:build livehub

package agenthub

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveDetectInstalledCLIs(t *testing.T) {
	for _, st := range DetectAll(nil, nil) {
		t.Logf("detect name=%s state=%s interactive=%v protocol=%s version=%q hint=%s",
			st.Name, st.State, st.Interactive, st.Protocol, st.Version, st.Hint)
	}
}

func TestLiveCursorThreadRoundtrip(t *testing.T) {
	liveThreadRoundtrip(t, "cursor", "只回复一个字：好。不要创建或修改任何文件。")
}

func TestLiveKimiThreadRoundtrip(t *testing.T) {
	liveThreadRoundtrip(t, "kimi", "只回复一个字：好。不要创建或修改任何文件。")
}

func TestLiveCodexThreadRoundtrip(t *testing.T) {
	liveThreadRoundtrip(t, "codex", "只回复一个字：好。不要创建或修改任何文件。")
}

func liveThreadRoundtrip(t *testing.T, harness, prompt string) {
	t.Helper()
	var st AgentStatus
	for _, item := range DetectAll(nil, nil) {
		if item.Name == harness {
			st = item
		}
	}
	if st.State != "available" {
		t.Fatalf("%s not available: %+v", harness, st)
	}
	s := testThreadService(t)
	work := t.TempDir()
	created, err := s.CreateThread(ThreadCreateRequest{
		HarnessID:     harness,
		Scene:         "free",
		WorkspaceRoot: work,
		Title:         "live-" + harness,
		AccessMode:    "approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("create status=%s native=%q msgs=%d", created.Thread.Status, created.Thread.NativeSessionID, len(created.Messages))
	for _, msg := range created.Messages {
		t.Logf("create msg %s %q", msg.Role, clip(msg.Content, 200))
	}
	if adapter, err := s.threadAdapter(harness); err == nil {
		t.Cleanup(func() { _ = adapter.Close(created.Thread.ID) })
	}
	if created.Thread.Status == "faulted" {
		t.Fatalf("open faulted: %+v", created.Thread)
	}
	if _, err = s.PromptThread(created.Thread.ID, prompt); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	var got ThreadDetail
	for time.Now().Before(deadline) {
		got, err = s.GetThread(created.Thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch got.Thread.Status {
		case "waiting_user":
			t.Logf("waiting_user call=%q prompt=%q", got.Prompt.CallID, clip(got.Prompt.Prompt, 200))
			logLiveThread(t, got, work)
			return
		case "success":
			logLiveThread(t, got, work)
			return
		case "faulted", "cancelled":
			logLiveThread(t, got, work)
			t.Fatalf("status=%s", got.Thread.Status)
		default:
			time.Sleep(400 * time.Millisecond)
		}
	}
	logLiveThread(t, got, work)
	t.Fatalf("timeout status=%s", got.Thread.Status)
}

func logLiveThread(t *testing.T, got ThreadDetail, work string) {
	t.Helper()
	t.Logf("done status=%s native=%q tokens=%d files=%d events=%d",
		got.Thread.Status, got.Thread.NativeSessionID, got.TokensUsed, len(got.Files), len(got.Events))
	for _, msg := range got.Messages {
		t.Logf("msg %s %q", msg.Role, clip(msg.Content, 240))
	}
	for _, ev := range got.Events {
		t.Logf("ev %s %s %q", ev.Type, ev.Title, clip(ev.Detail, 160))
	}
	entries, _ := os.ReadDir(work)
	for _, entry := range entries {
		t.Logf("work %s", filepath.Join(work, entry.Name()))
	}
}
