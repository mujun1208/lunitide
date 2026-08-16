package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
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
		writeJSONRPC(out, *req.ID, result)
		out.Flush()
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
