package brapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── local host ──────────────────────────────────────────────────────────────

// LocalHost probes the filesystem for chrome/edge, dials the extension
// bridge port, launches chrome/edge with a CDP debugging port under a
// dedicated profile directory, and drives navigation through the
// DevTools HTTP endpoints. Builtin mode represents the WebView2 host
// (always available, no CDP navigation channel).
type LocalHost struct {
	mu          sync.Mutex
	procs       map[string]*exec.Cmd
	profileRoot string
}

// NewLocalHost returns the default host rooted at profileRoot.
func NewLocalHost(profileRoot string) *LocalHost {
	return &LocalHost{procs: make(map[string]*exec.Cmd), profileRoot: profileRoot}
}

func chromeCandidates() []string {
	var out []string
	if p := osGetenv("ProgramFiles"); p != "" {
		out = append(out, filepath.Join(p, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if p := osGetenv("ProgramFiles(x86)"); p != "" {
		out = append(out, filepath.Join(p, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if p := osGetenv("LOCALAPPDATA"); p != "" {
		out = append(out, filepath.Join(p, "Google", "Chrome", "Application", "chrome.exe"))
	}
	return append(out, "/usr/bin/google-chrome", "/usr/bin/chromium-browser", "/usr/bin/chromium")
}

func edgeCandidates() []string {
	var out []string
	if p := osGetenv("ProgramFiles(x86)"); p != "" {
		out = append(out, filepath.Join(p, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	if p := osGetenv("ProgramFiles"); p != "" {
		out = append(out, filepath.Join(p, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	return append(out, "/usr/bin/microsoft-edge", "/usr/bin/microsoft-edge-stable")
}

// Detect reports local browser availability.
func (h *LocalHost) Detect(ctx context.Context, s Settings) (DetectReport, error) {
	report := DetectReport{Builtin: true, Extension: PortProbe{Port: s.ExtensionPort}}
	report.Chrome = probePath(s.ChromePath, chromeCandidates())
	report.Edge = probePath(s.EdgePath, edgeCandidates())
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.ExtensionPort)))
	if err == nil {
		_ = conn.Close()
		report.Extension.Available = true
	}
	return report, nil
}

func probePath(configured string, candidates []string) PathProbe {
	if configured != "" {
		if fileExists(configured) {
			return PathProbe{Available: true, Path: configured}
		}
		return PathProbe{Available: false, Path: configured}
	}
	for _, c := range candidates {
		if fileExists(c) {
			return PathProbe{Available: true, Path: c}
		}
	}
	return PathProbe{}
}

// Connect performs the mode handshake and answers the CDP ws url.
func (h *LocalHost) Connect(ctx context.Context, sessionID, mode string, s Settings) (string, error) {
	switch mode {
	case ModeBuiltin:
		return "", nil
	case ModeExtension:
		return cdpVersionWS(ctx, "127.0.0.1", s.ExtensionPort)
	case ModeChrome, ModeEdge:
		path := ""
		if mode == ModeChrome {
			path = firstExisting(s.ChromePath, chromeCandidates())
		} else {
			path = firstExisting(s.EdgePath, edgeCandidates())
		}
		if path == "" {
			return "", fmt.Errorf("%w: %s executable not found", ErrBrMode, mode)
		}
		port, err := freePort()
		if err != nil {
			return "", err
		}
		profileDir := filepath.Join(h.profileRoot, mode+"-profile")
		cmd := exec.Command(path,
			"--remote-debugging-port="+strconv.Itoa(port),
			"--user-data-dir="+profileDir,
			"--no-first-run", "--no-default-browser-check",
			"--headless=new", "about:blank")
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("%w: launch failed: %v", ErrBrMode, err)
		}
		h.mu.Lock()
		h.procs[sessionID] = cmd
		h.mu.Unlock()
		ws, werr := cdpPollVersionWS(ctx, "127.0.0.1", port, BrConnectTimeout)
		if werr != nil {
			h.killProc(sessionID)
			return "", fmt.Errorf("%w: CDP handshake failed: %v", ErrBrMode, werr)
		}
		return ws, nil
	case ModeAsk:
		return "", fmt.Errorf("%w: ask 模式等待用户选择", ErrBrMode)
	default:
		return "", fmt.Errorf("%w: mode %q", ErrBrSchema, mode)
	}
}

// Disconnect terminates a spawned browser process (chrome/edge).
func (h *LocalHost) Disconnect(_ context.Context, sessionID, mode string) error {
	if mode == ModeChrome || mode == ModeEdge {
		h.killProc(sessionID)
	}
	return nil
}

// Navigate opens one URL through the DevTools HTTP new-tab endpoint.
func (h *LocalHost) Navigate(ctx context.Context, sess Session, rawURL string) error {
	if sess.Mode == ModeBuiltin {
		return fmt.Errorf("%w: builtin 会话不支持 CDP 导航（使用 browser.act）", ErrBrMode)
	}
	host, port, err := wsHostPort(sess.WsURL)
	if err != nil {
		return fmt.Errorf("%w: session ws url missing", ErrBrMode)
	}
	endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/json/new?" + url.QueryEscape(rawURL)
	req, err := NewHTTPRequestContext(ctx, "PUT", endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBrMode, err)
	}
	resp, err := DefaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: navigate failed: %v", ErrBrMode, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: navigate status %d", ErrBrMode, resp.StatusCode)
	}
	return nil
}

// SnapshotUsage walks the mode profile directory (chrome/edge).
func (h *LocalHost) SnapshotUsage(_ context.Context, mode string) (int64, int64, int64) {
	if mode != ModeChrome && mode != ModeEdge {
		return 0, 0, 0
	}
	root := filepath.Join(h.profileRoot, mode+"-profile")
	var profile, cache, cookies int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		size := info.Size()
		profile += size
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isCachePath(rel) {
			cache += size
		}
		if isCookiesPath(rel) {
			cookies += size
		}
		return nil
	})
	return profile, cache, cookies
}

// ClearData removes cache/cookie artifacts older than the cutoff.
func (h *LocalHost) ClearData(_ context.Context, mode string, olderThan time.Time) (int64, error) {
	if mode != ModeChrome && mode != ModeEdge {
		return 0, nil
	}
	root := filepath.Join(h.profileRoot, mode+"-profile")
	var freed int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isCachePath(rel) && !isCookiesPath(rel) {
			return nil
		}
		if !info.ModTime().Before(olderThan) {
			return nil
		}
		if rmErr := removeFile(path); rmErr == nil {
			freed += info.Size()
		}
		return nil
	})
	return freed, nil
}

func (h *LocalHost) killProc(sessionID string) {
	h.mu.Lock()
	cmd, ok := h.procs[sessionID]
	if ok {
		delete(h.procs, sessionID)
	}
	h.mu.Unlock()
	if ok {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

func isCachePath(rel string) bool {
	for _, prefix := range []string{"Cache/", "Code Cache/", "GPUCache/", "Service Worker/CacheStorage/", "Media Cache/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return rel == "Cache" || rel == "Code Cache"
}

func isCookiesPath(rel string) bool {
	return rel == "Cookies" || rel == "Network/Cookies" || strings.HasPrefix(rel, "Network/Cookies")
}

// cdpVersionWS fetches /json/version once and extracts the browser ws url.
func cdpVersionWS(ctx context.Context, host string, port int) (string, error) {
	endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/json/version"
	req, err := NewHTTPRequestContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := DefaultHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: bridge port %d unreachable", ErrBrMode, port)
	}
	defer resp.Body.Close()
	var doc struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	dec := json.NewDecoder(resp.Body)
	if derr := dec.Decode(&doc); derr != nil || doc.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%w: bridge port %d not a CDP endpoint", ErrBrMode, port)
	}
	if !strings.HasPrefix(doc.WebSocketDebuggerURL, "ws://") {
		return "", fmt.Errorf("%w: unexpected ws url", ErrBrMode)
	}
	return doc.WebSocketDebuggerURL, nil
}

// cdpPollVersionWS retries cdpVersionWS until the deadline (launch ramp).
func cdpPollVersionWS(ctx context.Context, host string, port int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		ws, err := cdpVersionWS(ctx, host, port)
		if err == nil {
			return ws, nil
		}
		if time.Now().After(deadline) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// wsHostPort extracts host/port from a ws:// debugger url.
func wsHostPort(wsURL string) (string, int, error) {
	u, err := url.Parse(wsURL)
	if err != nil || u.Host == "" {
		return "", 0, fmt.Errorf("ws url invalid")
	}
	port := 80
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return "", 0, err
		}
	}
	return u.Hostname(), port, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
