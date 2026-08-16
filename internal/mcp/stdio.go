// MCP stdio transport (M6-MCP-004 gate opened 2026-08-16): one isolated
// child process per session, newline-delimited JSON-RPC 2.0 over the
// pipes, then the whole tree is killed. The child runs under the 5B/5C
// spawn engine (fresh Job Object, explicit environment block, process /
// commit quotas), so a hostile server cannot escape its process envelope
// or inherit host secrets. Command admission is the registry's job
// (npx/uvx/node whitelist, metacharacter-free args); this package only
// speaks the protocol.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lunitide/lunitide/internal/stdioworker"
)

var (
	// ErrStdioProtocol is MCP-002 family: the child violated the JSON-RPC
	// framing (oversized line, malformed JSON, id mismatch, error answer).
	ErrStdioProtocol = errors.New("mcp: stdio protocol violation (MCP-002)")
	// ErrStdioLaunch is MCP-001 family: the command could not be resolved
	// or spawned.
	ErrStdioLaunch = errors.New("mcp: stdio server launch failed (MCP-001)")
)

// Frozen stdio session parameters.
const (
	// StdioMaxLineBytes caps one JSON-RPC line (matches the 4 MiB frame
	// cap of the worker protocol).
	StdioMaxLineBytes = 4 << 20
	// StdioHandshakeTimeout bounds initialize + tools handshake.
	StdioHandshakeTimeout = 20 * time.Second
	// StdioCallTimeout bounds one tools/call round trip on top of the
	// caller's context.
	StdioCallTimeout = 30 * time.Second
	// StdioProtocolVersion is the negotiated MCP protocol version.
	StdioProtocolVersion = "2025-03-26"
)

// StdioSession is one live isolated MCP stdio server. Not safe for
// concurrent use: the registry serialises calls per endpoint.
type StdioSession struct {
	proc   *stdioworker.IsolatedProc
	stdin  *bufio.Writer
	stdout *bufio.Scanner
	nextID atomic.Int64
}

// stdioResolveCommand maps a whitelisted bare command onto a spawnable
// argv. On Windows npx/uvx are .cmd shims CreateProcess cannot execute
// directly, so they run through cmd.exe /d (AutoRun disabled) with the
// shim's absolute path; args are already metacharacter-free (registry
// admission), so the cmd.exe parsing surface carries no injections.
func stdioResolveCommand(command string, args []string) (string, []string, error) {
	resolved, err := exec.LookPath(command)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s not on PATH: %v", ErrStdioLaunch, command, err)
	}
	if runtime.GOOS != "windows" {
		return resolved, args, nil
	}
	lower := strings.ToLower(resolved)
	if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
		cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		if _, err := os.Stat(cmdExe); err != nil {
			return "", nil, fmt.Errorf("%w: cmd.exe unavailable: %v", ErrStdioLaunch, err)
		}
		return cmdExe, append([]string{"/d", "/c", resolved}, args...), nil
	}
	return resolved, args, nil
}

// StdioDial spawns the isolated server and completes the MCP initialize
// handshake. workDir receives the child's CWD (created when missing); the
// parent environment never leaks (explicit minimal block plus extraEnv,
// which carries operator-declared non-secret settings only).
func StdioDial(ctx context.Context, command string, args []string, workDir string, extraEnv []string) (*StdioSession, error) {
	if command == "" || len(args) == 0 {
		return nil, fmt.Errorf("%w: empty command or args", ErrStdioLaunch)
	}
	exe, argv, err := stdioResolveCommand(command, args)
	if err != nil {
		return nil, err
	}
	if workDir == "" {
		return nil, fmt.Errorf("%w: empty work dir", ErrStdioLaunch)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("%w: work dir: %v", ErrStdioLaunch, err)
	}
	env := []string{
		"STDIOMCP_SESSION=1",
		"PATH=" + os.Getenv("PATH"),
	}
	for _, kv := range extraEnv {
		if kv != "" && !strings.Contains(kv, "\x00") {
			env = append(env, kv)
		}
	}
	if root := os.Getenv("SystemRoot"); root != "" && runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+root)
	}
	if tv := os.Getenv("TEMP"); tv != "" {
		env = append(env, "TEMP="+tv, "TMP="+tv)
	}
	proc, err := stdioworker.SpawnIsolated(exe, argv, workDir, env, stdioworker.StdioQuotas())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStdioLaunch, err)
	}
	s := &StdioSession{
		proc:   proc,
		stdin:  bufio.NewWriter(proc.Stdin()),
		stdout: bufio.NewScanner(proc.Stdout()),
	}
	s.stdout.Buffer(make([]byte, 64*1024), StdioMaxLineBytes)
	hctx, cancel := context.WithTimeout(ctx, StdioHandshakeTimeout)
	defer cancel()
	if err := s.initialize(hctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// jsonrpcWire shapes shared by requests and responses.
type jsonrpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id,omitempty"`
	Method  string        `json:"method,omitempty"`
	Params  any           `json:"params,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonrpcError `json:"error,omitempty"`
}
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// initialize performs the MCP initialize handshake.
func (s *StdioSession) initialize(ctx context.Context) error {
	var answer struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := s.roundtrip(ctx, "initialize", map[string]any{
		"protocolVersion": StdioProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "lunitide", "version": "0.3.6"},
	}, &answer); err != nil {
		return err
	}
	// notifications/initialized carries no id and expects no answer.
	return s.notify("notifications/initialized")
}

// ListTools fetches the tool catalogue (tools/list).
func (s *StdioSession) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var answer struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := s.roundtrip(ctx, "tools/list", map[string]any{}, &answer); err != nil {
		return nil, err
	}
	return answer.Tools, nil
}

// StdioCallResult is one tools/call outcome. Content items are flattened
// into texts; structuredContent (when present) is preserved verbatim.
type StdioCallResult struct {
	Tool              string
	Texts             []string
	StructuredContent json.RawMessage
	IsError           bool
}

// CallTool executes one tools/call round trip.
func (s *StdioSession) CallTool(ctx context.Context, tool string, argsJSON []byte) (StdioCallResult, error) {
	var params map[string]any
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &params); err != nil {
			return StdioCallResult{}, fmt.Errorf("%w: args not a JSON object: %v", ErrStdioProtocol, err)
		}
	}
	var answer struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	cctx, cancel := context.WithTimeout(ctx, StdioCallTimeout)
	defer cancel()
	if err := s.roundtrip(cctx, "tools/call", map[string]any{
		"name":      tool,
		"arguments": params,
	}, &answer); err != nil {
		return StdioCallResult{}, err
	}
	out := StdioCallResult{Tool: tool, StructuredContent: answer.StructuredContent, IsError: answer.IsError}
	for _, c := range answer.Content {
		if c.Type == "text" || c.Type == "" {
			out.Texts = append(out.Texts, c.Text)
		}
	}
	return out, nil
}

// roundtrip sends one request and waits for the matching id answer,
// skipping server notifications. Line-level violations answer
// ErrStdioProtocol.
func (s *StdioSession) roundtrip(ctx context.Context, method string, params any, into any) error {
	id := s.nextID.Add(1)
	req := jsonrpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("%w: request marshal: %v", ErrStdioProtocol, err)
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: write %s: %v", ErrStdioProtocol, method, err)
	}
	if err := s.stdin.Flush(); err != nil {
		return fmt.Errorf("%w: flush %s: %v", ErrStdioProtocol, method, err)
	}
	type read struct {
		raw []byte
		err error
	}
	ch := make(chan read, 1)
	go func() {
		if s.stdout.Scan() {
			ch <- read{raw: s.stdout.Bytes()}
			return
		}
		err := s.stdout.Err()
		if err == nil {
			err = errors.New("stream closed")
		}
		ch <- read{err: fmt.Errorf("%w: read %s: %v", ErrStdioProtocol, method, err)}
	}()
	select {
	case <-ctx.Done():
		_ = s.proc.Kill()
		return fmt.Errorf("%w: %s deadline: %v", ErrStdioProtocol, method, ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		var env jsonrpcRequest
		if err := json.Unmarshal(r.raw, &env); err != nil {
			return fmt.Errorf("%w: %s answer not JSON: %v", ErrStdioProtocol, method, err)
		}
		if env.ID != id {
			return fmt.Errorf("%w: %s id mismatch (%d != %d)", ErrStdioProtocol, method, env.ID, id)
		}
		if env.Error != nil {
			return fmt.Errorf("%w: %s answered %d: %s", ErrStdioProtocol, method, env.Error.Code, env.Error.Message)
		}
		raw, err := json.Marshal(env.Result)
		if err != nil {
			return fmt.Errorf("%w: %s result re-marshal: %v", ErrStdioProtocol, method, err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			return fmt.Errorf("%w: %s result shape: %v", ErrStdioProtocol, method, err)
		}
		return nil
	}
}

// notify sends one directionless notification (no id, no answer waited).
func (s *StdioSession) notify(method string) error {
	line, err := json.Marshal(jsonrpcRequest{JSONRPC: "2.0", Method: method})
	if err != nil {
		return fmt.Errorf("%w: notify marshal: %v", ErrStdioProtocol, err)
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: notify write: %v", ErrStdioProtocol, err)
	}
	return s.stdin.Flush()
}

// Close kills the whole process tree and releases the handles.
func (s *StdioSession) Close() {
	if s == nil || s.proc == nil {
		return
	}
	s.proc.Close()
}
