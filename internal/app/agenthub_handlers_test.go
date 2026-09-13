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
	"github.com/lunitide/lunitide/internal/storage/sqlite"
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

type threadHubDetail struct {
	Thread struct {
		ID            string `json:"threadId"`
		WorkspaceRoot string `json:"workspaceRoot"`
		Status        string `json:"status"`
	} `json:"thread"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Files []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"files"`
	Prompt *struct {
		CallID string `json:"callId"`
	} `json:"prompt"`
}

func TestAgentHubThreadCreateEmptyWorkspaceUsesRootThreads(t *testing.T) {
	e, root := newThreadHubEngine(t)
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.create", `{"harnessId":"loopback","scene":"free","workspaceRoot":""}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	detail := decodeThreadHubDetail(t, resp.Payload)
	if detail.Thread.ID == "" {
		t.Fatal("missing threadId")
	}
	want := agenthub.DefaultThreadDir(root, detail.Thread.ID)
	if filepath.Clean(detail.Thread.WorkspaceRoot) != filepath.Clean(want) {
		t.Fatalf("workspaceRoot = %q, want %q", detail.Thread.WorkspaceRoot, want)
	}
	info, err := os.Stat(detail.Thread.WorkspaceRoot)
	if err != nil || !info.IsDir() {
		t.Fatalf("workspace dir: %v", err)
	}
}

func TestAgentHubPreviewThreadRejectsEscape(t *testing.T) {
	e, _ := newThreadHubEngine(t)
	created := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.create", `{"harnessId":"loopback","scene":"free","workspaceRoot":""}`))
	if !created.OK {
		t.Fatalf("%#v", created)
	}
	detail := decodeThreadHubDetail(t, created.Payload)
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.file.preview", `{"threadId":"`+detail.Thread.ID+`","path":"..\\Windows\\win.ini"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PATH_OUTSIDE" {
		t.Fatalf("%#v", resp)
	}
}

func TestAgentHubLoopbackCreatePromptRespond(t *testing.T) {
	t.Setenv("LUNITIDE_HARNESS_LOOPBACK", "1")
	e, _ := newThreadHubEngine(t)
	created := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.create", `{"harnessId":"loopback","scene":"free","workspaceRoot":""}`))
	if !created.OK {
		t.Fatalf("%#v", created)
	}
	detail := decodeThreadHubDetail(t, created.Payload)
	prompt := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.prompt", `{"threadId":"`+detail.Thread.ID+`","text":"选哪个?"}`))
	if !prompt.OK {
		t.Fatalf("%#v", prompt)
	}
	waiting := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.get", `{"threadId":"`+detail.Thread.ID+`"}`))
	if !waiting.OK {
		t.Fatalf("%#v", waiting)
	}
	got := decodeThreadHubDetail(t, waiting.Payload)
	if got.Thread.Status != "waiting_user" {
		t.Fatalf("status after prompt = %q, want waiting_user", got.Thread.Status)
	}
	if got.Prompt == nil || got.Prompt.CallID == "" {
		t.Fatalf("missing open prompt: %#v", got.Prompt)
	}
	respond := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.respond", `{"threadId":"`+detail.Thread.ID+`","callId":"`+got.Prompt.CallID+`","optionId":"是"}`))
	if !respond.OK {
		t.Fatalf("%#v", respond)
	}
	done := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.get", `{"threadId":"`+detail.Thread.ID+`"}`))
	if !done.OK {
		t.Fatalf("%#v", done)
	}
	after := decodeThreadHubDetail(t, done.Payload)
	var assistant string
	for _, msg := range after.Messages {
		if msg.Role == "assistant" && msg.Content != "" {
			assistant = msg.Content
			break
		}
	}
	if assistant == "" {
		t.Fatalf("missing assistant text: %#v", after.Messages)
	}
	if threadHubHasLoopbackFile(after.Files) {
		return
	}
	listed := handleAgentHub(e, context.Background(), validRequest("agentHub.workspace.list", `{"threadId":"`+detail.Thread.ID+`"}`))
	if !listed.OK {
		t.Fatalf("%#v", listed)
	}
	var workspace struct {
		Items []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"items"`
	}
	if err := json.Unmarshal(hubJSON(listed.Payload), &workspace); err != nil {
		t.Fatal(err)
	}
	if !threadHubHasLoopbackFile(workspace.Items) {
		t.Fatalf("loopback.txt missing from get.files and workspace.list: files=%#v list=%s", after.Files, hubJSON(listed.Payload))
	}
}

func newThreadHubEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	store, err := sqlite.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "hub-threads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := t.TempDir()
	s := agenthub.New(agenthub.NewMemoryStore(), root, nil)
	s.Threads = store.ThreadStore()
	return &Engine{agentHub: s}, root
}

func decodeThreadHubDetail(t *testing.T, payload any) threadHubDetail {
	t.Helper()
	var out threadHubDetail
	if err := json.Unmarshal(hubJSON(payload), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func threadHubHasLoopbackFile(items []struct {
	Name string `json:"name"`
	Path string `json:"path"`
}) bool {
	for _, item := range items {
		if item.Name == "loopback.txt" || strings.Contains(filepath.ToSlash(item.Path), "loopback.txt") {
			return true
		}
	}
	return false
}
