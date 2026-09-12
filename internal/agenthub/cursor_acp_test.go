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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestACPFrameRoundTripHelloFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "cursor-acp-hello.jsonl"))
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
		header, body, ok := bytes.Cut(frame, []byte("\r\n\r\n"))
		if !ok {
			t.Fatalf("frame %d missing Content-Length CRLF separator (must not be NDJSON): %q", n, frame)
		}
		if !bytes.Equal(header, []byte("Content-Length: "+strconv.Itoa(len(line)))) {
			t.Fatalf("frame %d header = %q", n, header)
		}
		if bytes.Contains(header, []byte{'\n'}) && !bytes.HasPrefix(frame, []byte("Content-Length:")) {
			t.Fatalf("frame %d is not Content-Length framed", n)
		}
		got, err := DecodeACPFrame(bufio.NewReader(bytes.NewReader(frame)))
		if err != nil {
			t.Fatalf("decode line %d: %v", n, err)
		}
		if !jsonEqual(got, line) {
			t.Fatalf("round-trip line %d\n got %s\nwant %s", n, got, line)
		}
		if !bytes.Equal(body, line) {
			t.Fatalf("frame body != fixture line %d", n)
		}
	}
	if err = sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 3 {
		t.Fatalf("fixture objects = %d, want at least 3 JSON-RPC lines", n)
	}
}

func TestResolveCursorACPWindowsCmdUsesNode(t *testing.T) {
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
	cmd := filepath.Join(root, "cursor-agent.cmd")
	if err := os.WriteFile(node, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index, []byte("module.exports=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmd, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exe, args, err := resolveCursorACP(func(string) (string, error) { return cmd, nil })
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

func TestDetectCursorInteractiveACPWhenFound(t *testing.T) {
	st := detectOne("cursor", func(string) (string, error) { return `C:\cursor-agent.cmd`, nil }, func(string, time.Duration) (string, error) {
		return "2026.09.10", nil
	})
	if st.State != "available" || !st.NonInteractive {
		t.Fatalf("V1 task detect must stay available: %+v", st)
	}
	if !st.Interactive || st.Protocol != "acp" {
		t.Fatalf("cursor detect = interactive=%v protocol=%q, want true/acp: %+v", st.Interactive, st.Protocol, st)
	}

	codex := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "codex 1", nil
	})
	if codex.Interactive || codex.Protocol != "exec" {
		t.Fatalf("codex must stay false/exec: %+v", codex)
	}
	kimi := detectOne("kimi", func(string) (string, error) { return `C:\kimi.exe`, nil }, func(string, time.Duration) (string, error) {
		return "kimi 1", nil
	})
	if kimi.Interactive || kimi.Protocol != "exec" {
		t.Fatalf("kimi must stay false/exec: %+v", kimi)
	}
}

func TestCursorACPInitializeFailureFaultsThread(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "cursor", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	cli := strings.Repeat("unsupported ACP protocol version from cursor-agent ", 10)
	adapter := NewCursorACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "cursor-agent.exe"), nil }
	adapter.startPersistent = func(context.Context, ProcSpec) (*PersistentProc, error) {
		return fakeACPPeer(t, func(msg map[string]any) (any, string) {
			if msg["method"] == "initialize" {
				return nil, cli
			}
			return map[string]any{}, ""
		}), nil
	}
	_ = adapter.Open(thread)
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

func TestCursorACPPromptSendsUserText(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "cursor", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	if err := insertThreadMessage(store, thread.ID, "system", "在你选的文件夹里按你的规则创建子目录并写文件。不要把已有文件挪到别处。"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var prompts []string
	adapter := NewCursorACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "cursor-agent.exe"), nil }
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
	if strings.Contains(prompts[0], "--force") || strings.Contains(prompts[0], "-p") {
		t.Fatalf("must not stuff scene into argv: %v", prompts)
	}
}

func TestCursorACPPromptReturnsWhenPeerAsks(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "cursor", "ACP", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewCursorACP(store)
	adapter.look = func(string) (string, error) { return filepath.Join(thread.WorkspaceRoot, "cursor-agent.exe"), nil }
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

func TestThreadAdapterWiresCursorACP(t *testing.T) {
	s := &Service{Threads: NewThreadStore(openThreadDB(t))}
	adapter, err := s.threadAdapter("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*CursorACP); !ok {
		t.Fatalf("cursor adapter = %T", adapter)
	}
	_, err = s.threadAdapter("kimi")
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("kimi must stay unavailable: %v", err)
	}
}

var acpNoReply = &struct{}{}

func fakeACPPeer(t *testing.T, handle func(map[string]any) (any, string)) *PersistentProc {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer outW.Close()
		br := bufio.NewReader(inR)
		for {
			body, err := DecodeACPFrame(br)
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(body, &msg) != nil {
				continue
			}
			if msg["method"] == nil || msg["id"] == nil {
				continue
			}
			result, errMsg := handle(msg)
			if result == acpNoReply && errMsg == "" {
				continue
			}
			resp := map[string]any{"jsonrpc": "2.0", "id": msg["id"]}
			if errMsg != "" {
				resp["error"] = map[string]any{"code": -32000, "message": errMsg}
			} else {
				resp["result"] = result
			}
			raw, err := json.Marshal(resp)
			if err != nil {
				return
			}
			if _, err = outW.Write(EncodeACPFrame(raw)); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outW.Close(); <-done })
	return &PersistentProc{stdin: inW, stdout: outR, closer: func() error {
		_ = inW.Close()
		_ = outW.Close()
		return nil
	}}
}

func jsonEqual(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	la, _ := json.Marshal(left)
	lb, _ := json.Marshal(right)
	return bytes.Equal(la, lb)
}
