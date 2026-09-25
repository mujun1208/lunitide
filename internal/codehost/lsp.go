package codehost

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Location struct {
	Path   string
	Line   int
	Column int
}

type Diagnostic struct {
	Path    string
	Line    int
	Message string
}

type rpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type Session struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	wmu      sync.Mutex
	mu       sync.Mutex
	next     int
	wait     map[string]chan rpc
	diags    map[string][]Diagnostic
	seen     map[string]bool
	closed   bool
	rootURI  string
	rootName string
	stderr   *tailWriter
}

func Start(root string) (*Session, error) {
	bin, err := toolBin("gopls")
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "serve")
	cmd.Dir = abs
	cmd.Env = toolEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tailWriter{max: 4096}
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	s := &Session{cmd: cmd, in: stdin, wait: map[string]chan rpc{}, diags: map[string][]Diagnostic{}, seen: map[string]bool{}, stderr: stderr, rootName: filepath.Base(abs)}
	go s.read(stdout)
	uri := fileURI(abs)
	s.rootURI = uri
	if _, err = s.call("initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   uri,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition":         map[string]any{},
				"publishDiagnostics": map[string]any{},
			},
		},
		"workspaceFolders": []any{map[string]any{"uri": uri, "name": filepath.Base(abs)}},
	}, 30*time.Second); err != nil {
		s.Close()
		return nil, err
	}
	if err = s.notify("initialized", map[string]any{}); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) Open(path, text string) error {
	return s.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        fileURI(path),
			"languageId": "go",
			"version":    1,
			"text":       text,
		},
	})
}

func (s *Session) Definition(path string, line, column int) (Location, error) {
	raw, err := s.callWhenReady("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(path)},
		"position":     map[string]any{"line": line - 1, "character": column - 1},
	}, 20*time.Second)
	if err != nil {
		return Location{}, err
	}
	var one struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
		TargetURI   string `json:"targetUri"`
		TargetRange struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"targetRange"`
	}
	var many []json.RawMessage
	if json.Unmarshal(raw, &many) == nil && len(many) > 0 {
		raw = many[0]
	}
	if err = json.Unmarshal(raw, &one); err != nil {
		return Location{}, err
	}
	uri := one.URI
	lineNo := one.Range.Start.Line
	col := one.Range.Start.Character
	if one.TargetURI != "" {
		uri = one.TargetURI
		lineNo = one.TargetRange.Start.Line
		col = one.TargetRange.Start.Character
	}
	if uri == "" {
		return Location{}, errors.New("no definition")
	}
	local, err := pathFromURI(uri)
	if err != nil {
		return Location{}, err
	}
	return Location{Path: local, Line: lineNo + 1, Column: col + 1}, nil
}

func (s *Session) References(path string, line, column int) ([]Location, error) {
	raw, err := s.callWhenReady("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(path)},
		"position":     map[string]any{"line": line - 1, "character": column - 1},
		"context":      map[string]any{"includeDeclaration": false},
	}, 20*time.Second)
	if err != nil {
		return nil, err
	}
	var many []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
		} `json:"range"`
	}
	if json.Unmarshal(raw, &many) != nil {
		return nil, errors.New("no references")
	}
	out := make([]Location, 0, len(many))
	for _, item := range many {
		local, err := pathFromURI(item.URI)
		if err != nil {
			continue
		}
		out = append(out, Location{Path: local, Line: item.Range.Start.Line + 1})
	}
	return out, nil
}

func (s *Session) WaitDiagnostics(path string, timeout time.Duration) []Diagnostic {
	deadline := time.Now().Add(timeout)
	var quiet time.Time
	uri := fileURI(path)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		seen := s.seen[uri]
		items := append([]Diagnostic(nil), s.diags[uri]...)
		s.mu.Unlock()
		if len(items) > 0 {
			return items
		}
		if seen {
			if quiet.IsZero() {
				quiet = time.Now()
			}
			if time.Since(quiet) > 1200*time.Millisecond {
				return items
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	return s.Diagnostics(path)
}

func (s *Session) Diagnostics(path string) []Diagnostic {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Diagnostic(nil), s.diags[fileURI(path)]...)
	return out
}

func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.notify("shutdown", map[string]any{})
	_ = s.notify("exit", map[string]any{})
	_ = s.in.Close()
	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = s.cmd.Process.Kill()
	}
}

// callWhenReady retries while gopls is still loading the module. A cold
// process answers "no views" until that load finishes.
func (s *Session) callWhenReady(method string, params any, wait time.Duration) (json.RawMessage, error) {
	deadline := time.Now().Add(wait)
	var last error
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			if last != nil {
				return nil, last
			}
			return nil, s.annotate(fmt.Errorf("%s timed out", method))
		}
		raw, err := s.call(method, params, remain)
		if err == nil || !strings.Contains(err.Error(), "no views") {
			return raw, s.annotate(err)
		}
		last = s.annotate(err)
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *Session) annotate(err error) error {
	if err == nil || s.stderr == nil {
		return err
	}
	if tail := strings.TrimSpace(s.stderr.String()); tail != "" {
		return fmt.Errorf("%w\n%s", err, tail)
	}
	return err
}

func (s *Session) call(method string, params any, wait time.Duration) (json.RawMessage, error) {
	s.mu.Lock()
	s.next++
	id := s.next
	key := strconv.Itoa(id)
	ch := make(chan rpc, 1)
	s.wait[key] = ch
	s.mu.Unlock()
	if err := s.write(rpc{JSONRPC: "2.0", ID: json.RawMessage(key), Method: method, Params: mustJSON(params)}); err != nil {
		return nil, err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, errors.New(msg.Error.Message)
		}
		return msg.Result, nil
	case <-time.After(wait):
		return nil, fmt.Errorf("%s timed out", method)
	}
}

func (s *Session) notify(method string, params any) error {
	return s.write(rpc{JSONRPC: "2.0", Method: method, Params: mustJSON(params)})
}

func (s *Session) write(msg rpc) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err = fmt.Fprintf(s.in, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

func (s *Session) read(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		var length int
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				length, _ = strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			}
		}
		if length <= 0 {
			continue
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(br, buf); err != nil {
			return
		}
		var msg rpc
		if json.Unmarshal(buf, &msg) != nil {
			continue
		}
		if msg.Method == "textDocument/publishDiagnostics" {
			s.storeDiagnostics(msg.Params)
			continue
		}
		if msg.Method != "" && len(msg.ID) > 0 {
			s.replyServer(msg)
			continue
		}
		if len(msg.ID) > 0 {
			key := strings.Trim(string(msg.ID), `"`)
			s.mu.Lock()
			ch := s.wait[key]
			delete(s.wait, key)
			s.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		}
	}
}

func (s *Session) replyServer(msg rpc) {
	var result any
	switch msg.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		n := len(params.Items)
		if n == 0 {
			n = 1
		}
		result = make([]any, n)
	case "workspace/workspaceFolders":
		result = []any{map[string]any{"uri": s.rootURI, "name": s.rootName}}
	}
	var id json.RawMessage
	if len(msg.ID) > 0 {
		id = msg.ID
	}
	body, _ := json.Marshal(result)
	_ = s.write(rpc{JSONRPC: "2.0", ID: id, Result: body})
}

func (s *Session) storeDiagnostics(params json.RawMessage) {
	var body struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Message string `json:"message"`
			Range   struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"range"`
		} `json:"diagnostics"`
	}
	if json.Unmarshal(params, &body) != nil || body.URI == "" {
		return
	}
	items := make([]Diagnostic, 0, len(body.Diagnostics))
	path, _ := pathFromURI(body.URI)
	for _, d := range body.Diagnostics {
		items = append(items, Diagnostic{Path: path, Line: d.Range.Start.Line + 1, Message: d.Message})
	}
	s.mu.Lock()
	s.diags[body.URI] = items
	s.seen[body.URI] = true
	s.mu.Unlock()
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.ToSlash(abs)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

func pathFromURI(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	p := u.Path
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}

func toolBin(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	var dirs []string
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		dirs = append(dirs, filepath.Join(gopath, "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	for _, dir := range dirs {
		for _, file := range []string{name + ".exe", name} {
			p := filepath.Join(dir, file)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s is not installed", name)
}

func toolEnv() []string {
	allow := map[string]bool{
		"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "TEMP": true, "TMP": true,
		"USERPROFILE": true, "HOMEDRIVE": true, "HOMEPATH": true, "LOCALAPPDATA": true, "APPDATA": true,
		"GOPATH": true, "GOROOT": true, "GOCACHE": true, "GOMODCACHE": true, "GOPROXY": true,
		"GOTOOLCHAIN": true, "CGO_ENABLED": true, "GOSUMDB": true, "GO111MODULE": true,
	}
	var out []string
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && allow[strings.ToUpper(key)] {
			out = append(out, kv)
		}
	}
	out = append(out, "GOTELEMETRY=off")
	if os.Getenv("GOTOOLCHAIN") == "" {
		out = append(out, "GOTOOLCHAIN=local")
	}
	return out
}

// tailWriter keeps the last max bytes of gopls stderr so a "no views"
// failure can show why the module never loaded.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.max:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
