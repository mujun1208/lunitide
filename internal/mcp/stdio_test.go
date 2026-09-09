package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The stdio tests re-exec the test binary itself as a fake MCP server
// (STDIO_MCP_FAKE=1), so the full isolated-spawn + JSON-RPC path runs
// against a real child process without any external runtime dependency.
func TestMain(m *testing.M) {
	if os.Getenv("STDIO_MCP_FAKE") == "1" {
		fakeStdioMcpServer(os.Getenv("STDIO_MCP_FAKE_MODE"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeStdioMcpServer speaks just enough MCP: initialize, tools/list,
// tools/call. mode "mute" closes stdout immediately (handshake failure),
// mode "garbage" answers non-JSON lines.
func fakeStdioMcpServer(mode string) {
	if mode == "stderr" {
		_, _ = os.Stderr.WriteString(strings.Repeat("package installer progress\n", 8192))
	}
	out := bufio.NewWriter(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		if mode == "mute" {
			return
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req struct {
			ID     *int64         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.Method == "" {
			continue
		} // response to the server ping
		if req.ID == nil {
			continue // notification, no answer
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": StdioProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "fake-stdio", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "echoes back",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
			}}}
		case "tools/call":
			name, _ := req.Params["name"].(string)
			args, _ := req.Params["arguments"].(map[string]any)
			text, _ := args["text"].(string)
			if strings.Contains(name, "boom") {
				writeJSONRPC(out, *req.ID, map[string]any{
					"content": []map[string]any{{"type": "text", "text": "exploded"}},
					"isError": true,
				})
				out.Flush()
				return
			}
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echo:" + text}},
				"isError": false,
			}
		default:
			result = map[string]any{}
		}
		if strings.HasPrefix(mode, "version:") && req.Method == "initialize" {
			result.(map[string]any)["protocolVersion"] = strings.TrimPrefix(mode, "version:")
		}
		if mode == "notifications" || mode == "flood" {
			count := 1
			if mode == "flood" {
				count = 258
			}
			for i := 0; i < count; i++ {
				_, _ = out.WriteString(`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","data":"ready"}}` + "\n")
			}
		}
		if mode == "requests" {
			_, _ = out.WriteString(`{"jsonrpc":"2.0","id":"server-ping","method":"ping"}` + "\n")
			_, _ = out.WriteString(`{"jsonrpc":"2.0","id":"roots","method":"roots/list"}` + "\n")
		}
		writeJSONRPC(out, *req.ID, result)
		out.Flush()
		if mode == "stop-reading" && req.Method == "initialize" {
			// Keep the pipe open but never consume initialized or tools/call bytes.
			// The parent must kill us when its write deadline/cancellation fires.
			for {
				time.Sleep(time.Second)
			}
		}
	}
}

func writeJSONRPC(w *bufio.Writer, id int64, result any) {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = w.Write(append(line, '\n'))
}

// dialFake spawns the re-exec'd fake server under the isolation engine.
// The fake-mode markers ride extraEnv (the child sees a minimal explicit
// environment block, never the parent's).
func dialFake(t *testing.T, mode string) *StdioSession {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("test binary path unavailable: %v", err)
	}
	extra := []string{"STDIO_MCP_FAKE=1"}
	if mode != "" {
		extra = append(extra, "STDIO_MCP_FAKE_MODE="+mode)
	}
	s, err := StdioDial(context.Background(), exe, []string{"-test.run=TestMain"}, t.TempDir(), extra)
	if err != nil {
		t.Fatalf("dial fake server: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestStdioSessionRoundTrip(t *testing.T) {
	s := dialFake(t, "")
	tools, err := s.ListTools(context.Background())
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" || tools[0].Description != "echoes back" {
		t.Fatalf("bad catalogue: %+v", tools)
	}
	out, err := s.CallTool(context.Background(), "echo", []byte(`{"text":"héllo"}`))
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if out.IsError || len(out.Texts) != 1 || out.Texts[0] != "echo:héllo" {
		t.Fatalf("bad call result: %+v", out)
	}
}

func TestStdioSessionSurfacesErrorAnswers(t *testing.T) {
	s := dialFake(t, "")
	out, err := s.CallTool(context.Background(), "boom", []byte(`{}`))
	if err != nil {
		t.Fatalf("tool-level errors ride the result envelope: %v", err)
	}
	if !out.IsError || len(out.Texts) != 1 || out.Texts[0] != "exploded" {
		t.Fatalf("want isError result carrying the server message, got %+v", out)
	}
}

func TestStdioDialRejectsMuteServer(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("test binary path unavailable: %v", err)
	}
	if _, err := StdioDial(context.Background(), exe, []string{"-test.run=TestMain"}, t.TempDir(),
		[]string{"STDIO_MCP_FAKE=1", "STDIO_MCP_FAKE_MODE=mute"}); err == nil {
		t.Fatal("want handshake failure for a mute server")
	}
}

func TestStdioResolveCommandRejectsUnknown(t *testing.T) {
	if _, _, err := stdioResolveCommand("definitely-not-on-path-xyz", []string{"a"}); err == nil {
		t.Fatal("want launch failure for an unknown command")
	}
}

func TestStdioDialRejectsEmptyVector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := StdioDial(ctx, "", nil, t.TempDir(), nil); err == nil {
		t.Fatal("want launch failure for empty command")
	}
	if _, err := StdioDial(ctx, "npx", nil, "", nil); err == nil {
		t.Fatal("want launch failure for empty work dir")
	}
}

func TestStdioLaunchEnvForwardsProxyAndCache(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:15715")
	t.Setenv("UV_CACHE_DIR", "E:/cache/uv")
	joined := strings.Join(stdioLaunchEnv(nil), "\n")
	if !strings.Contains(joined, "HTTPS_PROXY=http://127.0.0.1:15715") {
		t.Fatalf("missing proxy: %s", joined)
	}
	if !strings.Contains(joined, "UV_CACHE_DIR=E:/cache/uv") {
		t.Fatalf("missing uv cache: %s", joined)
	}
}

func TestStdioSessionInstallerStderrDoesNotCorruptProtocol(t *testing.T) {
	s := dialFake(t, "stderr")
	tools, err := s.ListTools(context.Background())
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("stderr corrupted MCP: %v %v", tools, err)
	}
}

func TestStdioWindowsShimPathWithSpaces(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows shim transport")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "runtime with spaces")
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(root, "fixture.cmd")
	if err = os.WriteFile(shim, []byte("@echo off\r\n\""+exe+"\" %*\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := StdioDial(context.Background(), shim, []string{"-test.run=TestMain"}, root, []string{"STDIO_MCP_FAKE=1"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tools, err := s.ListTools(context.Background())
	if err != nil || len(tools) != 1 {
		t.Fatalf("shim protocol: %v", err)
	}
}

func TestStdioNegotiatesInstalledServerVersionsAndInterleaving(t *testing.T) {
	for _, mode := range []string{"notifications", "requests", "version:2024-11-05", "version:2025-06-18", "version:2025-11-25"} {
		t.Run(mode, func(t *testing.T) {
			s := dialFake(t, mode)
			tools, err := s.ListTools(context.Background())
			if err != nil || len(tools) != 1 {
				t.Fatalf("tools after initialize: %v %v", tools, err)
			}
			out, err := s.CallTool(context.Background(), "echo", []byte(`{"text":"still connected"}`))
			if err != nil || len(out.Texts) != 1 || out.Texts[0] != "echo:still connected" {
				t.Fatalf("call after notifications: %+v %v", out, err)
			}
		})
	}
}
func TestStdioRejectsUnknownVersionAndNotificationFlood(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"version:2099-01-01", "flood"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := StdioDial(ctx, exe, []string{"-test.run=TestMain"}, t.TempDir(), []string{"STDIO_MCP_FAKE=1", "STDIO_MCP_FAKE_MODE=" + mode})
			if s != nil {
				s.Close()
			}
			if err == nil {
				t.Fatal("invalid server accepted")
			}
		})
	}
}

func TestStdioSetupBudgetRetainsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	setup := WithStdioStartupBudget(ctx)
	if stdioHandshakeBudget(ctx) != 20*time.Second || stdioHandshakeBudget(setup) != 60*time.Second {
		t.Fatal("startup and normal budgets changed")
	}
	cancel()
	if !errors.Is(setup.Err(), context.Canceled) {
		t.Fatal("setup escaped cancellation")
	}
}

func TestStdioRepliesToServerRequestsWithoutGrantingRoots(t *testing.T) {
	var written bytes.Buffer
	s := &StdioSession{stdin: bufio.NewWriter(&written), stdout: bufio.NewScanner(strings.NewReader(`{"jsonrpc":"2.0","id":"p","method":"ping"}` + "\n" + `{"jsonrpc":"2.0","id":"r","method":"roots/list"}` + "\n" + `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` + "\n"))}
	var result map[string]any
	if err := s.readResponse(1, "tools/list", &result); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(written.String()), "\n")
	if len(lines) != 2 {
		t.Fatal(written.String())
	}
	var ping, roots map[string]json.RawMessage
	if json.Unmarshal([]byte(lines[0]), &ping) != nil || string(ping["id"]) != `"p"` || string(ping["result"]) != `{}` {
		t.Fatal(lines[0])
	}
	if json.Unmarshal([]byte(lines[1]), &roots) != nil || string(roots["id"]) != `"r"` || !strings.Contains(string(roots["error"]), "-32601") || roots["result"] != nil {
		t.Fatal(lines[1])
	}
}

func TestStdioCancellationReleasesBackpressuredRequestWriter(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "deadline"
		if explicit {
			name = "explicit cancel"
		}
		t.Run(name, func(t *testing.T) {
			s := dialFake(t, "stop-reading")
			var ctx context.Context
			var cancel context.CancelFunc
			if explicit {
				ctx, cancel = context.WithCancel(context.Background())
				timer := time.AfterFunc(200*time.Millisecond, cancel)
				defer timer.Stop()
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
			}
			defer cancel()
			args, _ := json.Marshal(map[string]string{"text": strings.Repeat("x", 2<<20)})
			done := make(chan error, 1)
			started := time.Now()
			go func() { _, err := s.CallTool(ctx, "echo", args); done <- err }()
			select {
			case err := <-done:
				expected := context.DeadlineExceeded
				if explicit {
					expected = context.Canceled
				}
				if !errors.Is(err, expected) {
					t.Fatalf("lost cancellation: %v", err)
				}
			case <-time.After(3 * time.Second):
				// Failure cleanup also releases the old implementation's blocked writer.
				s.Close()
				<-done
				t.Fatal("request write ignored cancellation")
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("unbounded cleanup")
			}
			// A canceled session retires; an independent connection is usable immediately.
			fresh := dialFake(t, "")
			result, err := fresh.CallTool(context.Background(), "echo", []byte(`{"text":"after cancel"}`))
			if err != nil || len(result.Texts) != 1 || result.Texts[0] != "echo:after cancel" {
				t.Fatalf("next session: %+v %v", result, err)
			}
		})
	}
}
