package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEncodeCodexRequestOmitsJSONRPC(t *testing.T) {
	frame, err := encodeCodexRequest(1, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "lunitide", "title": "月汐", "version": "0.1.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(frame, []byte(`"jsonrpc"`)) {
		t.Fatalf("app-server wire must omit jsonrpc: %s", frame)
	}
	if !bytes.HasSuffix(frame, []byte("\n")) || bytes.Count(frame, []byte{'\n'}) != 1 {
		t.Fatalf("want one NDJSON line: %q", frame)
	}
	var msg map[string]any
	if err = json.Unmarshal(bytes.TrimSpace(frame), &msg); err != nil {
		t.Fatal(err)
	}
	if msg["method"] != "initialize" {
		t.Fatalf("method = %v", msg["method"])
	}
}

func TestCodexAppServerArgv(t *testing.T) {
	exe, args := codexAppServerArgv()
	if exe != "codex" || len(args) != 1 || args[0] != "app-server" {
		t.Fatalf("argv = %s %v, want codex [app-server]", exe, args)
	}
}

func TestResolveCodexAppServerUnwrapsNpmCmdShim(t *testing.T) {
	root := t.TempDir()
	cmdPath := filepath.Join(root, "codex.cmd")
	js := filepath.Join(root, "node_modules", "@openai", "codex", "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(js), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmdPath, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(js, []byte("console.log('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe, args, err := resolveCodexAppServer(func(string) (string, error) { return cmdPath, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Ext(exe), ".exe") || !strings.EqualFold(filepath.Base(exe), "node.exe") {
		t.Fatalf("exe = %q, want node.exe", exe)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "codex.js") || !strings.HasSuffix(joined, "app-server") {
		t.Fatalf("args = %v, want codex.js app-server", args)
	}
}

func TestCodexAppServerProcSpecRequiresAbsDir(t *testing.T) {
	spec, err := codexAppServerProcSpec(func(string) (string, error) {
		return filepath.Join(t.TempDir(), "codex.exe"), nil
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(spec.Dir) || spec.Dir == "" {
		t.Fatalf("dir = %q, want absolute cwd for StartPersistent", spec.Dir)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "app-server" {
		t.Fatalf("args = %v", spec.Args)
	}
}

func TestLiveCodexAppServerProbe(t *testing.T) {
	if os.Getenv("LUNITIDE_LIVE_CODEX") != "1" {
		t.Skip("set LUNITIDE_LIVE_CODEX=1 to probe the real CLI")
	}
	start := time.Now()
	ok := defaultProbeCodexAppServer(nil)
	t.Logf("probe=%v elapsed=%s", ok, time.Since(start))
	if !ok {
		t.Fatal("real codex app-server initialize handshake failed")
	}
}

func TestDetectCodexProbesWhenVersionFails(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return true }
	t.Cleanup(func() { probeCodexAppServer = prev })

	st := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "", context.DeadlineExceeded
	})
	if st.State != "available" || !st.Interactive || st.Protocol != "app-server" {
		t.Fatalf("codex detect = %+v, want available/true/app-server when app-server probe succeeds", st)
	}
}

func TestCodexInitializeParamsEnableExperimentalAPI(t *testing.T) {
	params := codexInitializeParams()
	caps, _ := params["capabilities"].(map[string]any)
	if caps["experimentalApi"] != true {
		t.Fatalf("initialize capabilities = %#v, want experimentalApi", params["capabilities"])
	}
}

func TestWaitCodexReplyTimesOutTurnStart(t *testing.T) {
	prev := codexCallTimeout
	codexCallTimeout = 20 * time.Millisecond
	t.Cleanup(func() { codexCallTimeout = prev })
	done := make(chan error, 1)
	go waitCodexReply("turn/start", make(chan *acpRPC), func(_ *acpRPC, err error) { done <- err })
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("err = %v, want timeout", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("turn/start wait did not time out")
	}
}

func TestDetectCodexAppServerWhenProbeSucceeds(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return true }
	t.Cleanup(func() { probeCodexAppServer = prev })

	st := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "codex-cli 0.1.0", nil
	})
	if st.State != "available" || !st.Interactive || st.Protocol != "app-server" {
		t.Fatalf("codex detect = %+v, want available/true/app-server", st)
	}
	if st.Hint != "可用" {
		t.Fatalf("hint = %q, want 可用", st.Hint)
	}
}

func TestCodexAutoAllowFileNotShell(t *testing.T) {
	if !codexAutoAllow("auto-edit", "item/fileChange/requestApproval") {
		t.Fatal("auto-edit must accept file changes")
	}
	if codexAutoAllow("auto-edit", "item/commandExecution/requestApproval") {
		t.Fatal("auto-edit must not accept shell")
	}
	if !codexAutoAllow("full-access", "item/commandExecution/requestApproval") {
		t.Fatal("full-access must accept shell")
	}
	if !codexAutoAllow("full-access", "item/permissions/requestApproval") {
		t.Fatal("full-access must accept permissions")
	}
	if codexAutoAllow("full-access", "item/tool/requestUserInput") {
		t.Fatal("business questions must always wait")
	}
	if codexAutoAllow("approval", "item/fileChange/requestApproval") {
		t.Fatal("approval mode must ask")
	}
}

func TestCodexThreadFallsBackToExecWhenAppServerFails(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Exec", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var got ProcSpec
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return nil, errors.New("app-server missing")
	}
	adapter.start = func(_ context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		got = spec
		onLine(`{"type":"agent.message","text":"exec fallback"}`)
		return 0, false, nil
	}
	if err := adapter.Open(thread); err != nil {
		t.Fatalf("Open must swallow app-server failure: %v", err)
	}
	gotThread, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotThread.Status != "idle" {
		t.Fatalf("status = %q, want idle after Open fallback", gotThread.Status)
	}
	if err = adapter.Prompt(thread.ID, "user as-is"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(got.Args, " "), "exec") {
		t.Fatalf("fallback argv = %v", got.Args)
	}
	if !hasPair(got.Args, "--sandbox", "workspace-write") {
		t.Fatalf("exec sandbox = %v, want workspace-write for approval", got.Args)
	}
	msgs, err := store.ListMessages(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 1 || msgs[0].Role != "notice" || msgs[0].Content != codexExecFallbackNotice {
		t.Fatalf("messages = %#v, want fallback notice first", msgs)
	}
	if err = adapter.Respond(thread.ID, "c", "yes", ""); err == nil || !strings.Contains(err.Error(), codexAvailableHint) {
		t.Fatalf("exec Respond = %v, want hint", err)
	}
}

func TestCodexThreadExecFallbackUsesFullAccessSandbox(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Full", false)
	thread.WorkspaceRoot = t.TempDir()
	thread.AccessMode = "full-access"
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var got ProcSpec
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return nil, errors.New("app-server missing")
	}
	adapter.start = func(_ context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		got = spec
		onLine(`{"type":"agent.message","text":"ok"}`)
		return 0, false, nil
	}
	if err := adapter.Prompt(thread.ID, "run"); err != nil {
		t.Fatal(err)
	}
	if !hasPair(got.Args, "--sandbox", "danger-full-access") {
		t.Fatalf("exec sandbox = %v, want danger-full-access", got.Args)
	}
}

func TestCodexThreadAppServerTurnAsksAndResponds(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "App", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	if err := insertThreadMessage(store, thread.ID, "system", sceneFix); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sawJSONRPC bool
	var sawModel bool
	var turnInput string
	var decisions []string
	asked := make(chan struct{})
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "codex.exe"), nil }
	adapter.startPersistent = func(_ context.Context, spec ProcSpec) (*PersistentProc, error) {
		if len(spec.Args) != 1 || spec.Args[0] != "app-server" {
			t.Fatalf("start argv = %#v, want [app-server]", spec.Args)
		}
		return fakeCodexPeer(t, func(msg map[string]any, write func(any)) {
			if _, ok := msg["jsonrpc"]; ok {
				mu.Lock()
				sawJSONRPC = true
				mu.Unlock()
			}
			switch msg["method"] {
			case "initialize":
				write(map[string]any{"id": msg["id"], "result": map[string]any{}})
			case "initialized":
				return
			case "thread/start":
				params, _ := msg["params"].(map[string]any)
				if params != nil {
					if _, ok := params["model"]; ok {
						mu.Lock()
						sawModel = true
						mu.Unlock()
					}
				}
				write(map[string]any{"id": msg["id"], "result": map[string]any{"thread": map[string]any{"id": "thr_1"}}})
			case "turn/start":
				raw, _ := json.Marshal(msg["params"])
				mu.Lock()
				turnInput = string(raw)
				mu.Unlock()
				write(map[string]any{"id": msg["id"], "result": map[string]any{"turn": map[string]any{"id": "turn_1", "status": "inProgress"}}})
				write(map[string]any{
					"id":     "ask1",
					"method": "item/tool/requestUserInput",
					"params": map[string]any{
						"questions": []any{map[string]any{
							"id":       "q1",
							"header":   "选哪个",
							"question": "选哪个",
							"options":  []any{map[string]any{"id": "yes", "label": "是"}},
						}},
					},
				})
				close(asked)
			default:
				if msg["method"] == nil && msg["id"] != nil {
					raw, _ := json.Marshal(msg)
					mu.Lock()
					decisions = append(decisions, string(raw))
					mu.Unlock()
					write(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "done via ask"}})
					write(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"totalTokens": 9}})
					write(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
				}
			}
		}), nil
	}
	if err := adapter.Open(thread); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prompt(thread.ID, "user as-is"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive requestUserInput")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Get(thread.ID)
		if err == nil && got.Status == "waiting_user" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := store.Get(thread.ID)
	if err != nil || got.Status != "waiting_user" {
		t.Fatalf("status = %#v %v, want waiting_user", got, err)
	}
	if got.NativeSessionID != "thr_1" {
		t.Fatalf("nativeSessionId = %q", got.NativeSessionID)
	}
	if err = adapter.Respond(thread.ID, "ask1", "yes", "补充"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err = store.Get(thread.ID)
		if err == nil && got.Status == "success" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err = store.Get(thread.ID)
	if err != nil || got.Status != "success" {
		t.Fatalf("status = %#v %v, want success", got, err)
	}
	role, content := loadLastMessage(t, store.db, thread.ID)
	if role != "assistant" || !strings.Contains(content, "done via ask") {
		t.Fatalf("assistant = %s %q", role, content)
	}
	mu.Lock()
	defer mu.Unlock()
	if sawJSONRPC {
		t.Fatal("client sent jsonrpc on the wire")
	}
	if sawModel {
		t.Fatal("thread/start must not hardcode model")
	}
	if !strings.Contains(turnInput, "user as-is") || !strings.Contains(turnInput, sceneFix) {
		t.Fatalf("turn input = %s", turnInput)
	}
	if len(decisions) == 0 || !strings.Contains(decisions[0], "yes") || !strings.Contains(decisions[0], "补充") {
		t.Fatalf("respond payload = %v", decisions)
	}
	tokens, err := store.TokensUsed(thread.ID)
	if err != nil || tokens != 9 {
		t.Fatalf("tokensUsed = %d %v, want 9", tokens, err)
	}
}

func TestCodexThreadSkipsAppServerWhenProbeFails(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return false }
	t.Cleanup(func() { probeCodexAppServer = prev })
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Skip", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var got ProcSpec
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.start = func(_ context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		got = spec
		onLine(`{"type":"agent.message","text":"exec only"}`)
		return 0, false, nil
	}
	if err := adapter.Prompt(thread.ID, "run"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(got.Args, " "), "exec") {
		t.Fatalf("argv = %v", got.Args)
	}
	msgs, err := store.ListMessages(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range msgs {
		if msg.Role == "notice" && msg.Content == codexExecFallbackNotice {
			t.Fatal("exec-only Codex must not claim app-server failed")
		}
	}
}

func TestCodexAppServerResumeKeepsIDWhenResultOmitsThread(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "ResumeOmit", false)
	thread.WorkspaceRoot = t.TempDir()
	thread.NativeSessionID = "thr_keep"
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "codex.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeCodexPeer(t, func(msg map[string]any, write func(any)) {
			switch msg["method"] {
			case "initialize":
				write(map[string]any{"id": msg["id"], "result": map[string]any{}})
			case "thread/resume":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"thread": map[string]any{}}})
			case "thread/start":
				t.Fatal("resume omit must keep native id, not start")
			case "turn/start":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"turn": map[string]any{"status": "inProgress"}}})
				write(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
			}
		}), nil
	}
	if err := adapter.Prompt(thread.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(thread.ID)
	if err != nil || got.NativeSessionID != "thr_keep" {
		t.Fatalf("native = %#v %v, want thr_keep", got, err)
	}
}

func TestCodexAppServerResumesNativeSession(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Resume", false)
	thread.WorkspaceRoot = t.TempDir()
	thread.NativeSessionID = "thr_old"
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var methods []string
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "codex.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeCodexPeer(t, func(msg map[string]any, write func(any)) {
			if method, _ := msg["method"].(string); method != "" {
				methods = append(methods, method)
			}
			switch msg["method"] {
			case "initialize":
				write(map[string]any{"id": msg["id"], "result": map[string]any{}})
			case "thread/resume":
				params, _ := msg["params"].(map[string]any)
				if params["threadId"] != "thr_old" {
					t.Fatalf("resume params = %#v", params)
				}
				write(map[string]any{"id": msg["id"], "result": map[string]any{"thread": map[string]any{"id": "thr_old"}}})
			case "thread/start":
				t.Fatal("resume must not fall through to thread/start")
			}
		}), nil
	}
	if err := adapter.Prompt(thread.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(methods, " ")
	if !strings.Contains(joined, "thread/resume") || strings.Contains(joined, "thread/start") {
		t.Fatalf("methods = %v, want resume not start", methods)
	}
}

func TestCodexAppServerResumeFallsBackToStart(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "ResumeFail", false)
	thread.WorkspaceRoot = t.TempDir()
	thread.NativeSessionID = "thr_dead"
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var methods []string
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "codex.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeCodexPeer(t, func(msg map[string]any, write func(any)) {
			if method, _ := msg["method"].(string); method != "" {
				methods = append(methods, method)
			}
			switch msg["method"] {
			case "initialize":
				write(map[string]any{"id": msg["id"], "result": map[string]any{}})
			case "thread/resume":
				write(map[string]any{"id": msg["id"], "error": map[string]any{"message": "unknown thread"}})
			case "thread/start":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"thread": map[string]any{"id": "thr_new"}}})
			}
		}), nil
	}
	if err := adapter.Prompt(thread.ID, "again"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(thread.ID)
	if err != nil || got.NativeSessionID != "thr_new" {
		t.Fatalf("native = %#v %v, want thr_new", got, err)
	}
	joined := strings.Join(methods, " ")
	if !strings.Contains(joined, "thread/resume") || !strings.Contains(joined, "thread/start") {
		t.Fatalf("methods = %v, want resume then start", methods)
	}
}

func TestCodexAppServerCancelNotOverwrittenByTurnCompleted(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Cancel", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var writeTurn func(any)
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "codex.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeCodexPeer(t, func(msg map[string]any, write func(any)) {
			switch msg["method"] {
			case "initialize":
				write(map[string]any{"id": msg["id"], "result": map[string]any{}})
			case "thread/start":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"thread": map[string]any{"id": "thr_c"}}})
			case "turn/start":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"turn": map[string]any{"status": "inProgress"}}})
				writeTurn = write
				close(started)
			}
		}), nil
	}
	if err := adapter.Prompt(thread.ID, "run"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start")
	}
	if err := setThreadStatus(store, thread.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if writeTurn != nil {
		writeTurn(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, err := store.Get(thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "cancelled" {
			t.Fatalf("status = %q, cancel must not become %q", got.Status, got.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCodexFaultAppDropsSessionSoPromptRetries(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Fault", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var starts int
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "codex.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		starts++
		n := starts
		return fakeCodexPeer(t, func(msg map[string]any, write func(any)) {
			switch msg["method"] {
			case "initialize":
				write(map[string]any{"id": msg["id"], "result": map[string]any{}})
			case "thread/start":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"thread": map[string]any{"id": "thr_f"}}})
			case "turn/start":
				if n == 1 {
					write(map[string]any{"id": msg["id"], "error": map[string]any{"message": "turn exploded"}})
					return
				}
				write(map[string]any{"id": msg["id"], "result": map[string]any{"turn": map[string]any{"status": "inProgress"}}})
				write(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "recovered"}})
				write(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
			}
		}), nil
	}
	if err := adapter.Prompt(thread.ID, "first"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Get(thread.ID)
		if err == nil && got.Status == "faulted" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := store.Get(thread.ID)
	if err != nil || got.Status != "faulted" {
		t.Fatalf("first turn = %#v %v, want faulted", got, err)
	}
	if err = adapter.Prompt(thread.ID, "second"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err = store.Get(thread.ID)
		if err == nil && got.Status == "success" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err = store.Get(thread.ID)
	if err != nil || got.Status != "success" {
		t.Fatalf("retry = %#v %v, want success", got, err)
	}
	if starts < 2 {
		t.Fatalf("starts = %d, want a new app-server after fault", starts)
	}
}

func TestCodexThreadAutoAllowsFileChangeOnly(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Auto", false)
	thread.WorkspaceRoot = t.TempDir()
	thread.AccessMode = "auto-edit"
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var decisions []string
	asked := make(chan struct{})
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "codex.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeCodexPeer(t, func(msg map[string]any, write func(any)) {
			switch msg["method"] {
			case "initialize":
				write(map[string]any{"id": msg["id"], "result": map[string]any{}})
			case "thread/start":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"thread": map[string]any{"id": "thr_auto"}}})
			case "turn/start":
				write(map[string]any{"id": msg["id"], "result": map[string]any{"turn": map[string]any{"status": "inProgress"}}})
				go func() {
					write(map[string]any{"id": "file1", "method": "item/fileChange/requestApproval", "params": map[string]any{}})
					write(map[string]any{"id": "cmd1", "method": "item/commandExecution/requestApproval", "params": map[string]any{"command": "rm"}})
				}()
			default:
				if msg["method"] == nil && msg["id"] != nil {
					raw, _ := json.Marshal(msg)
					mu.Lock()
					decisions = append(decisions, string(raw))
					n := len(decisions)
					mu.Unlock()
					if n == 1 {
						close(asked)
					}
				}
			}
		}), nil
	}
	if err := adapter.Open(thread); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prompt(thread.ID, "edit"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("expected auto file decision")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Get(thread.ID)
		if err == nil && got.Status == "waiting_user" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := store.Get(thread.ID)
	if err != nil || got.Status != "waiting_user" {
		t.Fatalf("status = %#v %v, want waiting_user for shell", got, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(decisions) != 1 || !strings.Contains(decisions[0], `"accept"`) {
		t.Fatalf("auto decisions = %v, want one accept for file", decisions)
	}
}

func fakeCodexPeer(t *testing.T, handle func(map[string]any, func(any))) *PersistentProc {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer outW.Close()
		br := bufio.NewReader(inR)
		write := func(v any) {
			raw, err := json.Marshal(v)
			if err != nil {
				return
			}
			_, _ = outW.Write(EncodeACPFrame(raw))
		}
		for {
			body, err := DecodeACPFrame(br)
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(body, &msg) != nil {
				continue
			}
			handle(msg, write)
		}
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outW.Close(); <-done })
	return &PersistentProc{stdin: inW, stdout: outR, closer: func() error {
		_ = inW.Close()
		_ = outW.Close()
		return nil
	}}
}
