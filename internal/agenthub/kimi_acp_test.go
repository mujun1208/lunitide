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
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestKimiACPFrameRoundTripHelloFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "kimi-acp-hello.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		n++
		if !json.Valid(line) {
			t.Fatalf("fixture line %d is not JSON: %s", n, line)
		}
		frame := EncodeACPFrame(line)
		if bytes.HasPrefix(frame, []byte("Content-Length:")) || bytes.Contains(frame, []byte("\r\n\r\n")) {
			t.Fatalf("frame %d uses Content-Length, want NDJSON: %q", n, frame)
		}
		if bytes.Count(frame, []byte{'\n'}) != 1 || !bytes.HasSuffix(frame, []byte{'\n'}) {
			t.Fatalf("frame %d must be one JSON line plus \\n: %q", n, frame)
		}
		if bytes.Contains(bytes.TrimSuffix(frame, []byte{'\n'}), []byte{'\n'}) {
			t.Fatalf("frame %d embeds a newline inside the JSON-RPC message: %q", n, frame)
		}
		got, err := DecodeACPFrame(bufio.NewReader(bytes.NewReader(frame)))
		if err != nil {
			t.Fatalf("decode line %d: %v", n, err)
		}
		if !jsonEqual(got, line) {
			t.Fatalf("round-trip line %d\n got %s\nwant %s", n, got, line)
		}
	}
	if err = sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 3 {
		t.Fatalf("fixture objects = %d, want at least 3 JSON-RPC lines", n)
	}
}

func TestKimiACPArgvIsAcpWithoutSkillsDir(t *testing.T) {
	exe, args := kimiACPArgv()
	if exe != "kimi" {
		t.Fatalf("exe = %q, want kimi", exe)
	}
	if len(args) != 1 || args[0] != "acp" {
		t.Fatalf("args = %#v, want [acp]", args)
	}
	if strings.Contains(strings.Join(args, " "), "--skills-dir") {
		t.Fatalf("thread argv must not include --skills-dir: %v", args)
	}
}

func TestDetectKimiInteractiveACPWhenFound(t *testing.T) {
	st := detectOne("kimi", func(string) (string, error) { return `C:\kimi.exe`, nil }, func(string, time.Duration) (string, error) {
		return "kimi 1", nil
	})
	if st.State != "available" || !st.NonInteractive {
		t.Fatalf("V1 task detect must stay available: %+v", st)
	}
	if !st.Interactive || st.Protocol != "acp" {
		t.Fatalf("kimi detect = interactive=%v protocol=%q, want true/acp: %+v", st.Interactive, st.Protocol, st)
	}

	codex := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "codex 1", nil
	})
	if codex.Interactive || codex.Protocol != "exec" {
		t.Fatalf("codex must stay false/exec: %+v", codex)
	}
}

func TestThreadAdapterWiresKimiACP(t *testing.T) {
	s := &Service{Threads: NewThreadStore(openThreadDB(t))}
	adapter, err := s.threadAdapter("kimi")
	if err != nil {
		t.Fatal(err)
	}
	first, ok := adapter.(*KimiACP)
	if !ok {
		t.Fatalf("kimi adapter = %T", adapter)
	}
	again, err := s.threadAdapter("kimi")
	if err != nil {
		t.Fatal(err)
	}
	second, ok := again.(*KimiACP)
	if !ok || first != second {
		t.Fatal("Open/Prompt must share the same KimiACP process map")
	}
}

func TestKimiACPStartsWithAcpOnlyArgv(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var spec ProcSpec
	var lookName string
	adapter := NewKimiACP(store)
	adapter.look = func(name string) (string, error) {
		lookName = name
		return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil
	}
	adapter.startPersistent = func(_ context.Context, got ProcSpec) (*PersistentProc, error) {
		mu.Lock()
		spec = got
		mu.Unlock()
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_kimi"}, ""
			default:
				return map[string]any{}, ""
			}
		}), nil
	}
	if err := adapter.Open(thread); err != nil {
		t.Fatal(err)
	}
	if lookName != "kimi" {
		t.Fatalf("look = %q, want kimi", lookName)
	}
	mu.Lock()
	defer mu.Unlock()
	if filepath.Base(spec.Exe) != "kimi.exe" {
		t.Fatalf("exe = %q, want kimi.exe", spec.Exe)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "acp" {
		t.Fatalf("args = %#v, want [acp]", spec.Args)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "--skills-dir") {
		t.Fatalf("thread argv must not include --skills-dir: %v", spec.Args)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "-p") || strings.Contains(strings.Join(spec.Args, " "), "stream-json") {
		t.Fatalf("thread argv must not use V1 -p / stream-json: %v", spec.Args)
	}
}

func TestKimiACPInitializeFailureFaultsThread(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	cli := strings.Repeat("unsupported ACP protocol version from kimi ", 10)
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			if msg["method"] == "initialize" {
				return nil, cli
			}
			return map[string]any{}, ""
		}), nil
	}
	if err := adapter.Open(thread); err == nil {
		t.Fatal("Open must return initialize failure")
	}
	if err := adapter.Prompt(thread.ID, "hi"); err == nil {
		t.Fatal("Prompt after ensure failure must return the error")
	}
	got, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", got.Status)
	}
	_, content := loadLastMessage(t, store.db, thread.ID)
	want := cli
	if utf8.RuneCountInString(want) > 200 {
		want = string([]rune(want)[:200])
	}
	if content != want {
		t.Fatalf("hint = %q, want first 200 of CLI text", content)
	}
}

func TestKimiACPInitializeDeathUsesCLIHint(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	cli := "unsupported ACP protocol version from kimi stderr"
	stdoutLine := "kimi: protocol mismatch"
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		logs := &safeLogBuf{}
		_, _ = logs.Write([]byte(cli))
		go func() { _, _ = io.Copy(io.Discard, inR) }()
		go func() {
			_, _ = outW.Write(append([]byte(stdoutLine), '\n'))
			_ = outW.Close()
		}()
		t.Cleanup(func() { _ = inW.Close(); _ = outW.Close(); _ = inR.Close() })
		return &PersistentProc{stdin: inW, stdout: outR, logs: logs, closer: func() error {
			_ = inW.Close()
			_ = outW.Close()
			return nil
		}}, nil
	}
	if err := adapter.Open(thread); err == nil {
		t.Fatal("Open must return initialize failure")
	}
	got, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", got.Status)
	}
	_, content := loadLastMessage(t, store.db, thread.ID)
	if content == "acp closed" || content == "acp timeout initialize" || !strings.Contains(content, "protocol") {
		t.Fatalf("hint = %q, want CLI stderr/stdout text", content)
	}
	if utf8.RuneCountInString(content) > 200 {
		t.Fatalf("hint longer than 200: %d", utf8.RuneCountInString(content))
	}
}

func TestKimiACPPromptSendsUserText(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	if err := insertThreadMessage(store, thread.ID, "system", "用 Kimi 自己的技能做文稿。pptx 写在工作区；指定了导出目录则完成时复制过去。"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var prompts []string
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_hello"}, ""
			case "session/prompt":
				raw, _ := json.Marshal(msg["params"])
				mu.Lock()
				prompts = append(prompts, string(raw))
				mu.Unlock()
				return map[string]any{"stopReason": "end_turn"}, ""
			default:
				return map[string]any{}, ""
			}
		}), nil
	}
	if err := adapter.Prompt(thread.ID, "hello as-is"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(prompts)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) == 0 || !strings.Contains(prompts[0], "hello as-is") {
		t.Fatalf("user text not sent as-is: %v", prompts)
	}
	if !strings.Contains(prompts[0], "用 Kimi 自己的技能做文稿。pptx 写在工作区；指定了导出目录则完成时复制过去。") {
		t.Fatalf("system text missing from prompt fixture: %v", prompts)
	}
	if strings.Contains(prompts[0], "--skills-dir") || strings.Contains(prompts[0], "--force") || strings.Contains(prompts[0], "-p") {
		t.Fatalf("must not stuff scene or skills-dir into argv/prompt: %v", prompts)
	}
}

func TestKimiACPSecondPromptWhileRunningIsBusy(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_busy"}, ""
			case "session/prompt":
				return acpNoReply, ""
			default:
				return map[string]any{}, ""
			}
		}), nil
	}
	if err := adapter.Prompt(thread.ID, "first"); err != nil {
		t.Fatal(err)
	}
	err := adapter.Prompt(thread.ID, "second")
	if err == nil {
		t.Fatal("second prompt while running must be busy")
	}
	var users int
	if scanErr := store.db.QueryRow(`SELECT COUNT(*) FROM agent_hub_messages WHERE thread_id=? AND role='user'`, thread.ID).Scan(&users); scanErr != nil {
		t.Fatal(scanErr)
	}
	if users != 1 {
		t.Fatalf("user messages = %d, want 1", users)
	}
}

func TestKimiACPPromptRehandshakesAfterProcessDeath(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var starts int
	var live *PersistentProc
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		proc := fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_re"}, ""
			case "session/prompt":
				return map[string]any{"stopReason": "end_turn"}, ""
			default:
				return map[string]any{}, ""
			}
		})
		mu.Lock()
		starts++
		live = proc
		mu.Unlock()
		return proc, nil
	}
	if err := adapter.Open(thread); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if starts != 1 || live == nil {
		mu.Unlock()
		t.Fatalf("starts = %d", starts)
	}
	proc := live
	mu.Unlock()
	if err := proc.Close(); err != nil {
		t.Fatal(err)
	}
	waitACPSessionGone(t, func() bool {
		adapter.mu.Lock()
		defer adapter.mu.Unlock()
		return adapter.sessions[thread.ID] != nil
	})
	if err := adapter.Prompt(thread.ID, "again"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if starts < 2 {
		t.Fatalf("ensure did not run again after death, starts=%d", starts)
	}
}

func TestKimiOpenReturnsEnsureError(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return "", errors.New("未安装 Kimi CLI") }
	err := adapter.Open(thread)
	if err == nil || !strings.Contains(err.Error(), "未安装 Kimi CLI") {
		t.Fatalf("Open = %v, want ensure error", err)
	}
}

func TestKimiACPPromptEnsureFailureReturnsError(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return "", errors.New("kimi missing") }
	err := adapter.Prompt(thread.ID, "hi")
	if err == nil {
		t.Fatal("ensure failure must return after fault")
	}
	got, getErr := store.Get(thread.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", got.Status)
	}
}

func TestKimiACPPromptSendFailureReturnsError(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_write"}, ""
			default:
				return map[string]any{}, ""
			}
		}), nil
	}
	if err := adapter.Open(thread); err != nil {
		t.Fatal(err)
	}
	adapter.mu.Lock()
	sess := adapter.sessions[thread.ID]
	adapter.mu.Unlock()
	if sess == nil || sess.proc == nil {
		t.Fatal("missing session")
	}
	_, broken := io.Pipe()
	_ = broken.Close()
	sess.proc.stdin = broken
	err := adapter.Prompt(thread.ID, "hi")
	if err == nil {
		t.Fatal("send failure must return after fault")
	}
	got, getErr := store.Get(thread.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", got.Status)
	}
}

func TestResolveKimiACPWindowsCmdUsesNode(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows .cmd resolve")
	}
	root := t.TempDir()
	ver := filepath.Join(root, "versions", "2026.01.02-deadbee")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(ver, "node.exe")
	index := filepath.Join(ver, "index.js")
	cmd := filepath.Join(root, "kimi.cmd")
	if err := os.WriteFile(node, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index, []byte("module.exports=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmd, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exe, args, err := resolveKimiACP(func(string) (string, error) { return cmd, nil })
	if err != nil {
		t.Fatal(err)
	}
	if exe != node {
		t.Fatalf("exe = %q, want %q", exe, node)
	}
	if len(args) != 2 || args[0] != index || args[1] != "acp" {
		t.Fatalf("args = %#v, want [index.js acp]", args)
	}
	if strings.EqualFold(filepath.Ext(exe), ".cmd") {
		t.Fatal("persistent session must not stay on the .cmd shim")
	}
}

func TestKimiACPPromptReturnsWhenPeerAsks(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_ask"}, ""
			case "session/prompt":
				return acpNoReply, ""
			default:
				return map[string]any{}, ""
			}
		}), nil
	}
	done := make(chan error, 1)
	go func() { done <- adapter.Prompt(thread.ID, "need a choice") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Prompt must return while the ACP turn is still open")
	}
}

func TestKimiACPCloseDuringHandshakeDoesNotLeak(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var closed bool
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		close(entered)
		<-release
		proc := fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_close"}, ""
			default:
				return map[string]any{}, ""
			}
		})
		orig := proc.closer
		proc.closer = func() error {
			mu.Lock()
			closed = true
			mu.Unlock()
			if orig != nil {
				return orig()
			}
			return nil
		}
		return proc, nil
	}
	opened := make(chan error, 1)
	go func() { opened <- adapter.Open(thread) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handshake did not reach StartPersistent")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- adapter.Close(thread.ID) }()
	time.Sleep(50 * time.Millisecond)
	close(release)
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("Open did not return after Close")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
	_ = adapter.Close(thread.ID)
	mu.Lock()
	defer mu.Unlock()
	if !closed {
		t.Fatal("Close during handshake leaked the Job Object")
	}
}

func TestKimiACPRespondDuringHandshakeDoesNotPanic(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	adapter := NewKimiACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "kimi.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		close(entered)
		<-release
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			switch msg["method"] {
			case "initialize":
				return map[string]any{"protocolVersion": 1}, ""
			case "session/new":
				return map[string]any{"sessionId": "sess_respond"}, ""
			default:
				return map[string]any{}, ""
			}
		}), nil
	}
	go func() { _ = adapter.Open(thread) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handshake did not reach StartPersistent")
	}
	defer close(release)
	err := adapter.Respond(thread.ID, "call1", "是", "")
	if err == nil {
		t.Fatal("Respond during handshake must not treat the placeholder as open")
	}
}
