package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agenthub"
)

func TestAgentHubStartUnavailableChinese(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	s.Look = func(string) (string, error) { return "", os.ErrNotExist }
	e := &Engine{agentHub: s}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.task.start", `{"agent":"codex","prompt":"hi","idempotencyKey":"k1"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "AGENT_NOT_AVAILABLE" {
		t.Fatalf("%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "未安装") {
		t.Fatalf("%q", resp.Error.Message)
	}
}

func TestAgentHubKimiStartsWhenAvailable(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	s.Look = func(string) (string, error) { return `C:\kimi.exe`, nil }
	s.Version = func(string, time.Duration) (string, error) { return "kimi 1", nil }
	s.Start = func(ctx context.Context, spec agenthub.ProcSpec, onLine func(string)) (int64, bool, error) {
		return 0, false, nil
	}
	e := &Engine{agentHub: s}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.task.start", `{"agent":"kimi","prompt":"hi","idempotencyKey":"kimi"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
}

func TestAgentHubPreviewRejectsEscape(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	s.Look = func(string) (string, error) { return `C:\codex.exe`, nil }
	s.Version = func(string, time.Duration) (string, error) { return "codex 1", nil }
	s.Start = func(ctx context.Context, spec agenthub.ProcSpec, onLine func(string)) (int64, bool, error) {
		return 0, false, nil
	}
	e := &Engine{agentHub: s}
	start := handleAgentHub(e, context.Background(), validRequest("agentHub.task.start", `{"agent":"codex","prompt":"hi","idempotencyKey":"preview"}`))
	if !start.OK {
		t.Fatalf("%#v", start)
	}
	var detail agenthub.TaskDetail
	raw, _ := json.Marshal(start.Payload)
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := handleAgentHub(e, context.Background(), validRequest("agentHub.task.get", `{"taskId":"`+detail.Task.ID+`"}`))
		_ = json.Unmarshal(hubJSON(got.Payload), &detail)
		if detail.Task.Status == "success" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.file.preview", `{"taskId":"`+detail.Task.ID+`","path":"..\\Windows\\win.ini"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PATH_OUTSIDE" {
		t.Fatalf("%#v", resp)
	}
	open := handleAgentHub(e, context.Background(), validRequest("agentHub.file.open", `{"taskId":"`+detail.Task.ID+`","path":"..\\Windows\\win.ini"}`))
	if open.OK || open.Error == nil || open.Error.Code != "PATH_OUTSIDE" {
		t.Fatalf("%#v", open)
	}
}

func TestAgentHubGetAfterStartHasEvents(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	s.Look = func(string) (string, error) { return `C:\codex.exe`, nil }
	s.Version = func(string, time.Duration) (string, error) { return "codex 1", nil }
	s.Start = func(ctx context.Context, spec agenthub.ProcSpec, onLine func(string)) (int64, bool, error) {
		onLine(`{"type":"started","title":"start"}`)
		_ = os.WriteFile(filepath.Join(spec.Dir, "hello.txt"), []byte("ok"), 0o644)
		return 0, false, nil
	}
	e := &Engine{agentHub: s}
	start := handleAgentHub(e, context.Background(), validRequest("agentHub.task.start", `{"agent":"codex","prompt":"hi","idempotencyKey":"events"}`))
	if !start.OK {
		t.Fatalf("%#v", start)
	}
	var detail agenthub.TaskDetail
	if err := json.Unmarshal(hubJSON(start.Payload), &detail); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := handleAgentHub(e, context.Background(), validRequest("agentHub.task.get", `{"taskId":"`+detail.Task.ID+`"}`))
		if err := json.Unmarshal(hubJSON(got.Payload), &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Task.Status == "success" && len(detail.Events) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("missing events: %+v", detail)
}

func TestAgentHubOpenFolderIsNotTreatedAsFile(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	s.Look = func(string) (string, error) { return `C:\codex.exe`, nil }
	s.Version = func(string, time.Duration) (string, error) { return "codex 1", nil }
	s.Start = func(ctx context.Context, spec agenthub.ProcSpec, onLine func(string)) (int64, bool, error) {
		return 0, false, nil
	}
	e := &Engine{agentHub: s}
	start := handleAgentHub(e, context.Background(), validRequest("agentHub.task.start", `{"agent":"codex","prompt":"hi","idempotencyKey":"open-dir"}`))
	if !start.OK {
		t.Fatalf("%#v", start)
	}
	var detail agenthub.TaskDetail
	if err := json.Unmarshal(hubJSON(start.Payload), &detail); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := handleAgentHub(e, context.Background(), validRequest("agentHub.task.get", `{"taskId":"`+detail.Task.ID+`"}`))
		if err := json.Unmarshal(hubJSON(got.Payload), &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Task.Status == "success" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var opened string
	var asFile bool
	old := openArtifactTarget
	t.Cleanup(func() { openArtifactTarget = old })
	openArtifactTarget = func(path string, file, reveal bool) error {
		opened = path
		asFile = file
		if !reveal {
			t.Fatal("expected reveal")
		}
		return nil
	}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.file.open", `{"taskId":"`+detail.Task.ID+`","reveal":true}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	if asFile {
		t.Fatal("work dir must open as a folder")
	}
	if opened == "" {
		t.Fatal("missing opened path")
	}
}

func TestAgentHubPickDirCanceled(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	s.Pick = func() (string, error) { return "", agenthub.ErrPickCanceled }
	e := &Engine{agentHub: s}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.dir.pick", `{}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw := string(hubJSON(resp.Payload))
	if !strings.Contains(raw, `"canceled":true`) {
		t.Fatalf("%s", raw)
	}
}

func TestAgentHubInboxDropEscapeKeepsPathUnsupported(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	e := &Engine{agentHub: s}
	raw, err := json.Marshal(map[string]string{
		"action":  "drop",
		"workDir": t.TempDir(),
		"name":    `..\secret.txt`,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.inbox", string(raw)))
	if resp.OK || resp.Error == nil || resp.Error.Code != "AGENT_HUB_FAILED" {
		t.Fatalf("%#v", resp)
	}
	if resp.Error.Message != "路径不受支持" {
		t.Fatalf("want 路径不受支持, got %q", resp.Error.Message)
	}
}

func TestAgentHubInboxCopiesWithInjectedPicker(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	e := &Engine{agentHub: s}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.inbox", `{"action":"files"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw := string(hubJSON(resp.Payload))
	if !strings.Contains(raw, `"canceled":false`) || !strings.Contains(raw, "note.txt") {
		t.Fatalf("%s", raw)
	}
}

func TestAgentHubMethodsAreNotDataScoped(t *testing.T) {
	for _, method := range []string{"agentHub.detect", "agentHub.dir.pick", "agentHub.inbox", "agentHub.task.start", "agentHub.task.get", "agentHub.file.preview"} {
		if dataScopedMethod(method) {
			t.Fatalf("%s must stay out of dataScopedMethod", method)
		}
	}
}

func hubJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
