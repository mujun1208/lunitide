package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestAgentHubFailureKeepsCursorNodeHint(t *testing.T) {
	resp := agentHubFailure(validRequest("agentHub.thread.prompt", `{"threadId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","text":"hi"}`), fmt.Errorf("cursor-agent 无法解析为 node"))
	if resp.OK || resp.Error == nil || resp.Error.Code != "AGENT_HUB_FAILED" {
		t.Fatalf("%#v", resp)
	}
	if resp.Error.Message == "AgentHub 操作失败" {
		t.Fatal("must not swallow the node handshake failure")
	}
	if !strings.Contains(resp.Error.Message, "Node") {
		t.Fatalf("got %q", resp.Error.Message)
	}
}

func TestAgentHubAuthenticationRequiredMapsToLogin(t *testing.T) {
	resp := agentHubFailure(validRequest("agentHub.thread.prompt", `{"threadId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","text":"hi"}`), fmt.Errorf("Authentication required"))
	if resp.OK || resp.Error == nil {
		t.Fatalf("%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "还没登录") {
		t.Fatalf("got %q", resp.Error.Message)
	}
}

func TestAgentHubEnglishPromptErrorIsNotSwallowed(t *testing.T) {
	resp := agentHubFailure(validRequest("agentHub.thread.prompt", `{"threadId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","text":"hi"}`), fmt.Errorf("spawn cursor-agent ENOENT"))
	if resp.OK || resp.Error == nil {
		t.Fatalf("%#v", resp)
	}
	if resp.Error.Message == "AgentHub 操作失败" {
		t.Fatal("must not hide the English spawn error")
	}
	if !strings.Contains(resp.Error.Message, "对话没发出去") {
		t.Fatalf("got %q", resp.Error.Message)
	}
}

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
	var detail agenthub.TaskDetail
	if err := json.Unmarshal(hubJSON(resp.Payload), &detail); err != nil {
		t.Fatal(err)
	}
	// StartTask returns as soon as the task is queued; execute() still walks
	// the allocated work dir. Returning here lets t.TempDir cleanup race that
	// walk on Windows ("The process cannot access the file").
	waitHubTask(t, e, detail.Task.ID, func(got agenthub.TaskDetail) bool { return got.Task.Status == "success" })
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
	waitHubTask(t, e, detail.Task.ID, func(got agenthub.TaskDetail) bool { return got.Task.Status == "success" })
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
	waitHubTask(t, e, detail.Task.ID, func(got agenthub.TaskDetail) bool {
		return got.Task.Status == "success" && len(got.Events) > 0
	})
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
	waitHubTask(t, e, detail.Task.ID, func(got agenthub.TaskDetail) bool { return got.Task.Status == "success" })
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
	for _, method := range []string{"agentHub.detect", "agentHub.dir.pick", "agentHub.inbox", "agentHub.install", "agentHub.task.start", "agentHub.task.get", "agentHub.file.preview"} {
		if dataScopedMethod(method) {
			t.Fatalf("%s must stay out of dataScopedMethod", method)
		}
	}
}

func waitHubTask(t *testing.T, e *Engine, taskID string, ready func(agenthub.TaskDetail) bool) agenthub.TaskDetail {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var detail agenthub.TaskDetail
	for time.Now().Before(deadline) {
		got := handleAgentHub(e, context.Background(), validRequest("agentHub.task.get", `{"taskId":"`+taskID+`"}`))
		if err := json.Unmarshal(hubJSON(got.Payload), &detail); err != nil {
			t.Fatal(err)
		}
		if ready(detail) {
			return detail
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("agent hub task did not finish: %+v", detail)
	return detail
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
		ExportDir     string `json:"exportDir"`
		Scene         string `json:"scene"`
		AccessMode    string `json:"accessMode"`
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

func TestAgentHubThreadCreateProjectIdIgnoresClientWorkspace(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "hub-project.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	root := t.TempDir()
	hub := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	hub.Threads = store.ThreadStore()
	e.agentHub = hub
	created, err := svc.Create(ctx, "hub-root", "test", map[string]string{"n": "x"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, Description: "d", Client: "c",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: root, Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	clientRoot := t.TempDir()
	body, err := json.Marshal(map[string]any{
		"harnessId": "loopback", "scene": "write_project", "projectId": created.ID, "workspaceRoot": clientRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleAgentHub(e, ctx, validRequest("agentHub.thread.create", string(body)))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	detail := decodeThreadHubDetail(t, resp.Payload)
	if filepath.Clean(detail.Thread.WorkspaceRoot) != filepath.Clean(root) {
		t.Fatalf("workspaceRoot=%q want project root %q (client sent %q)", detail.Thread.WorkspaceRoot, root, clientRoot)
	}
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

func TestAgentHubThreadUpdatePersistsWorkspaceExportAndScene(t *testing.T) {
	e, _ := newThreadHubEngine(t)
	created := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.create", `{"harnessId":"loopback","scene":"free","workspaceRoot":""}`))
	if !created.OK {
		t.Fatalf("%#v", created)
	}
	detail := decodeThreadHubDetail(t, created.Payload)
	workspace := filepath.Join(t.TempDir(), "proj")
	export := filepath.Join(t.TempDir(), "out")
	body, err := json.Marshal(map[string]string{
		"threadId":      detail.Thread.ID,
		"workspaceRoot": workspace,
		"exportDir":     export,
		"scene":         "ppt",
		"accessMode":    "full-access",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.thread.update", string(body)))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	got := decodeThreadHubDetail(t, resp.Payload)
	if filepath.Clean(got.Thread.WorkspaceRoot) != filepath.Clean(workspace) || filepath.Clean(got.Thread.ExportDir) != filepath.Clean(export) || got.Thread.Scene != "ppt" || got.Thread.AccessMode != "full-access" {
		t.Fatalf("%+v", got.Thread)
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
