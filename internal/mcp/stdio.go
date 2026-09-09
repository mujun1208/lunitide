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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lunitide/lunitide/internal/buildinfo"
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
	stderr   *os.File
	proc     *stdioworker.IsolatedProc
	stdin    *bufio.Writer
	stdout   *bufio.Scanner
	nextID   atomic.Int64
	identity string
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

// stdioLaunchEnv is the explicit child block. Host secrets stay out; proxy
// and cache roots are the minimum npx/uvx need to finish a first download.
func stdioLaunchEnv(extraEnv []string) []string {
	env := []string{
		"STDIOMCP_SESSION=1",
		"PATH=" + os.Getenv("PATH"),
	}
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "UV_CACHE_DIR"} {
		if v := os.Getenv(key); v != "" && !strings.ContainsAny(v, "\x00\r\n") {
			env = append(env, key+"="+v)
		}
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
	return env
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
	env := stdioLaunchEnv(extraEnv)
	// npx/uvx and MCP servers write diagnostics to stderr. Never feed those
	// bytes into JSON-RPC, and continuously drain them without unbounded storage
	// or logging potentially sensitive server output.
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("%w: stderr pipe: %v", ErrStdioLaunch, err)
	}
	diagnostics := &stderrClassifier{}
	stderrDone := make(chan struct{})
	go func() { _, _ = io.Copy(diagnostics, stderrRead); _ = stderrRead.Close(); close(stderrDone) }()
	proc, err := stdioworker.SpawnIsolatedWithStderr(exe, argv, workDir, env, stdioworker.StdioQuotas(), stderrWrite)
	_ = stderrWrite.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStdioLaunch, err)
	}
	s := &StdioSession{
		stderr: stderrRead,
		proc:   proc,
		stdin:  bufio.NewWriter(proc.Stdin()),
		stdout: bufio.NewScanner(proc.Stdout()),
	}
	s.stdout.Buffer(make([]byte, 64*1024), StdioMaxLineBytes)
	hctx, cancel := context.WithTimeout(ctx, stdioHandshakeBudget(ctx))
	defer cancel()
	if err := s.initialize(hctx); err != nil {
		s.proc.Close()
		<-stderrDone
		_ = stderrRead.Close()
		return nil, diagnostics.classify(err)
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
		"clientInfo":      map[string]any{"name": "lunitide", "version": buildinfo.Version},
	}, &answer); err != nil {
		return err
	}
	if !stdioProtocolSupported(answer.ProtocolVersion) || strings.TrimSpace(answer.ServerInfo.Name) == "" || strings.TrimSpace(answer.ServerInfo.Version) == "" || len(answer.ServerInfo.Name) > 512 || len(answer.ServerInfo.Version) > 128 {
		return fmt.Errorf("%w: unsupported protocol or missing server identity", ErrStdioProtocol)
	}
	identity, _ := json.Marshal(answer)
	s.identity = string(identity)
	// notifications/initialized carries no id and expects no answer.
	return s.notify("notifications/initialized")
}

func (s *StdioSession) Identity() string { return s.identity }

// ListTools fetches the tool catalogue (tools/list).
func (s *StdioSession) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var out []ToolInfo
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 16; page++ {
		var answer struct {
			Tools      []ToolInfo `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := s.roundtrip(ctx, "tools/list", params, &answer); err != nil {
			return nil, err
		}
		out = append(out, answer.Tools...)
		if len(out) > 512 {
			return nil, ErrResponseTooLarge
		}
		if answer.NextCursor == "" {
			return out, nil
		}
		if len(answer.NextCursor) > 4096 || seen[answer.NextCursor] {
			return nil, ErrStdioProtocol
		}
		seen[answer.NextCursor] = true
		cursor = answer.NextCursor
	}
	return nil, ErrResponseTooLarge
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
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %s canceled: %w", ErrStdioProtocol, method, err)
	}
	done := make(chan error, 1)
	go func() {
		if _, err := s.stdin.Write(append(line, '\n')); err != nil {
			done <- fmt.Errorf("%w: write %s: %w", ErrStdioProtocol, method, err)
			return
		}
		if err := s.stdin.Flush(); err != nil {
			done <- fmt.Errorf("%w: flush %s: %w", ErrStdioProtocol, method, err)
			return
		}
		done <- s.readResponse(id, method, into)
	}()
	select {
	case <-ctx.Done():
		// A server may stop reading stdin after its handshake. Closing the killed
		// process pipes releases a blocked write as well as a blocked response read.
		s.proc.Close()
		// Never return with a worker that can touch this session's buffers later.
		<-done
		return fmt.Errorf("%w: %s deadline: %w", ErrStdioProtocol, method, ctx.Err())
	case err := <-done:
		return err
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
	if s.stderr != nil {
		_ = s.stderr.Close()
	}
}

// The basic stdio tools protocol is compatible across these published versions.
// A server selects a version it implements; old installed servers may reply
// 2024-11-05 instead of echoing our preferred version.
func stdioProtocolSupported(version string) bool {
	switch version {
	case "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25":
		return true
	default:
		return false
	}
}

func (s *StdioSession) readResponse(id int64, method string, into any) error {
	var ancillaryBytes int
	for frames := 0; frames < 257; frames++ {
		if !s.stdout.Scan() {
			return fmt.Errorf("%w: read %s: stream closed", ErrStdioProtocol, method)
		}
		raw := s.stdout.Bytes()
		var env struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Result  json.RawMessage `json:"result"`
			Error   *jsonrpcError   `json:"error"`
		}
		if json.Unmarshal(raw, &env) != nil || env.JSONRPC != "2.0" {
			return fmt.Errorf("%w: %s answer not JSON-RPC", ErrStdioProtocol, method)
		}
		if env.Method != "" {
			ancillaryBytes += len(raw)
			if ancillaryBytes > 8<<20 || len(env.Result) != 0 || env.Error != nil {
				return ErrStdioProtocol
			}
			if len(env.ID) == 0 {
				continue
			} // progress, log and list-change notifications
			if string(env.ID) == "null" {
				return ErrStdioProtocol
			}
			var number int64
			var text string
			if json.Unmarshal(env.ID, &number) != nil && (json.Unmarshal(env.ID, &text) != nil || len(text) > 256) {
				return ErrStdioProtocol
			}
			// Only ping is implemented. Roots/sampling/elicitation are not advertised
			// and receive an explicit error, without exposing files or asking a model.
			reply := map[string]any{"jsonrpc": "2.0", "id": env.ID}
			if env.Method == "ping" {
				reply["result"] = map[string]any{}
			} else {
				reply["error"] = jsonrpcError{Code: -32601, Message: "Method not supported by this client"}
			}
			data, _ := json.Marshal(reply)
			if _, err := s.stdin.Write(append(data, '\n')); err != nil {
				return fmt.Errorf("%w: reply write", ErrStdioProtocol)
			}
			if err := s.stdin.Flush(); err != nil {
				return fmt.Errorf("%w: reply flush", ErrStdioProtocol)
			}
			continue
		}
		var responseID int64
		if len(env.ID) == 0 || string(env.ID) == "null" || json.Unmarshal(env.ID, &responseID) != nil || responseID != id {
			return fmt.Errorf("%w: %s id mismatch", ErrStdioProtocol, method)
		}
		if env.Error != nil {
			if len(env.Result) != 0 {
				return ErrStdioProtocol
			}
			return fmt.Errorf("%w: %s answered %d", ErrStdioProtocol, method, env.Error.Code)
		}
		if len(env.Result) == 0 || string(env.Result) == "null" || json.Unmarshal(env.Result, into) != nil {
			return fmt.Errorf("%w: %s result shape", ErrStdioProtocol, method)
		}
		return nil
	}
	return fmt.Errorf("%w: too many interleaved notifications", ErrStdioProtocol)
}

type stdioStartupBudgetKey struct{}

// WithStdioStartupBudget gives an explicit settings connection more time for
// a cold npx/uvx dependency download. Tool invocations retain the 20s handshake.
func WithStdioStartupBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, stdioStartupBudgetKey{}, true)
}
func stdioHandshakeBudget(ctx context.Context) time.Duration {
	if allowed, _ := ctx.Value(stdioStartupBudgetKey{}).(bool); allowed {
		return 60 * time.Second
	}
	return StdioHandshakeTimeout
}
