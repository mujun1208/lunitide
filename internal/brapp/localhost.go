package brapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/browsernetwork"
	"github.com/lunitide/lunitide/internal/commandworker"
)

// ── local host ──────────────────────────────────────────────────────────────

// LocalHost probes the filesystem for chrome/edge, dials the extension
// bridge port, launches chrome/edge with a CDP debugging port under a
// dedicated profile directory, and drives navigation through the
// DevTools HTTP endpoints. Builtin mode represents the WebView2 host
// (always available, no CDP navigation channel).
type LocalHost struct {
	mu          sync.Mutex
	procs       map[string]*managedBrowser
	profileRoot string
}

type managedBrowser struct {
	cancel  context.CancelFunc
	done    chan struct{}
	proxy   *browsernetwork.Proxy
	context *cdpContext
}

// NewLocalHost returns the default host rooted at profileRoot.
func NewLocalHost(profileRoot string) *LocalHost {
	return &LocalHost{procs: make(map[string]*managedBrowser), profileRoot: profileRoot}
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
		endpoint, err := cdpVersionWS(ctx, "127.0.0.1", s.ExtensionPort)
		if err != nil {
			return "", err
		}
		if err := verifyExternalBrowserNetworkFlags(ctx, endpoint); err != nil {
			return "", err
		}
		proxy, err := browsernetwork.Start(browsernetwork.Policy{AllowHTTP: true, AllowPrivate: !s.BlockPrivateNetwork, AllowedOrigins: s.Allowlist})
		if err != nil {
			return "", err
		}
		owned, err := createCDPContext(ctx, endpoint, proxy.URL())
		if err != nil {
			_ = proxy.Close()
			return "", fmt.Errorf("%w: 无法创建受策略约束的私有浏览器上下文: %v", ErrBrMode, err)
		}
		h.mu.Lock()
		h.procs[sessionID] = &managedBrowser{proxy: proxy, context: owned}
		h.mu.Unlock()
		return endpoint, nil
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
		profileBase, err := filepath.Abs(filepath.Join(h.profileRoot, mode+"-profile"))
		if err != nil {
			return "", err
		}
		digest := sha256.Sum256([]byte(sessionID))
		profileDir := filepath.Join(profileBase, fmt.Sprintf("%x", digest[:16]))
		if err := os.MkdirAll(profileDir, 0700); err != nil {
			return "", err
		}
		guard, err := commandworker.PinWorkingDirectory(h.profileRoot, profileDir)
		if err != nil {
			return "", err
		}
		proxy, err := browsernetwork.Start(browsernetwork.Policy{AllowHTTP: true, AllowPrivate: !s.BlockPrivateNetwork, AllowedOrigins: s.Allowlist})
		if err != nil {
			_ = guard.Close()
			return "", err
		}
		workerCtx, cancel := context.WithCancel(context.Background())
		managed := &managedBrowser{cancel: cancel, done: make(chan struct{}), proxy: proxy}
		exe, err := filepath.Abs(path)
		if err != nil {
			cancel()
			_ = proxy.Close()
			_ = guard.Close()
			return "", err
		}
		h.mu.Lock()
		h.procs[sessionID] = managed
		h.mu.Unlock()
		go func() {
			defer close(managed.done)
			defer proxy.Close()
			_, _ = commandworker.Run(workerCtx, commandworker.Spec{Exe: exe, Dir: profileDir, Env: browserEnvironment(), Timeout: commandworker.TimeoutHardCap, MaxOutputBytes: 4096, Args: []string{
				"--remote-debugging-port=" + strconv.Itoa(port), "--user-data-dir=" + profileDir, "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--headless=new",
				"--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--proxy-server=" + proxy.URL(), "--proxy-bypass-list=<-loopback>", "about:blank",
			}}, guard, nil)
		}()
		ws, werr := cdpPollVersionWS(ctx, "127.0.0.1", port, BrConnectTimeout)
		if werr == nil {
			managed.context, werr = createCDPContext(ctx, ws, proxy.URL())
		}
		if werr != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), BrConnectTimeout)
			defer cleanupCancel()
			cleanupErr := h.killProc(cleanupCtx, sessionID)
			return "", fmt.Errorf("%w: CDP handshake failed: %v", ErrBrMode, errors.Join(werr, cleanupErr))
		}
		return ws, nil
	case ModeAsk:
		return "", fmt.Errorf("%w: ask 模式等待用户选择", ErrBrMode)
	default:
		return "", fmt.Errorf("%w: mode %q", ErrBrSchema, mode)
	}
}

// Disconnect terminates a spawned browser process (chrome/edge).
func (h *LocalHost) Disconnect(ctx context.Context, sessionID, mode string) error {
	return h.killProc(ctx, sessionID)
}

func (h *LocalHost) Navigate(ctx context.Context, sess Session, rawURL string) error {
	if sess.Mode == ModeBuiltin {
		return fmt.Errorf("%w: builtin 会话不支持 CDP 导航（使用 browser.act）", ErrBrMode)
	}
	h.mu.Lock()
	managed := h.procs[sess.SessionID]
	h.mu.Unlock()
	if managed == nil || managed.context == nil {
		return fmt.Errorf("%w: 浏览器上下文已结束，请重新连接", ErrBrMode)
	}
	if managed.done != nil {
		select {
		case <-managed.done:
			return fmt.Errorf("%w: 浏览器会话已到期，请重新连接", ErrBrMode)
		default:
		}
	}
	return managed.context.Navigate(ctx, rawURL)
}

// walkProfile validates the managed boundary and refuses links/junctions. The
// walk is bounded in time and entries; callers receive partial-work errors.
func (h *LocalHost) walkProfile(ctx context.Context, mode string, visit func(string, string, os.FileInfo) error) error {
	if mode != ModeChrome && mode != ModeEdge {
		return nil
	}
	root, err := filepath.Abs(filepath.Join(h.profileRoot, mode+"-profile"))
	if err != nil {
		return err
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	guard, err := commandworker.PinWorkingDirectory(h.profileRoot, root)
	if err != nil {
		return err
	}
	defer guard.Close()
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	visited := 0
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if err := deadline.Err(); err != nil {
			return err
		}
		visited++
		if visited > 100000 {
			return errors.New("browser profile scan entry budget exceeded")
		}
		if walkErr != nil {
			return walkErr
		}
		if info == nil {
			return errors.New("browser profile entry unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("browser profile contains a link or junction")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("browser profile boundary invalid")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("browser profile contains a non-regular entry")
		}
		// Profiles are per-session; usage classification starts after that directory.
		rel = filepath.ToSlash(rel)
		if head, tail, ok := strings.Cut(rel, "/"); ok && len(head) == 32 {
			rel = tail
		}
		return visit(path, rel, info)
	})
}

func (h *LocalHost) SnapshotUsage(ctx context.Context, mode string) (int64, int64, int64) {
	profile, cache, cookies, _ := h.SnapshotUsageChecked(ctx, mode)
	return profile, cache, cookies
}
func (h *LocalHost) SnapshotUsageChecked(ctx context.Context, mode string) (profile, cache, cookies int64, err error) {
	err = h.walkProfile(ctx, mode, func(_ string, rel string, info os.FileInfo) error {
		profile += info.Size()
		if isCachePath(rel) {
			cache += info.Size()
		}
		if isCookiesPath(rel) {
			cookies += info.Size()
		}
		return nil
	})
	return
}
func (h *LocalHost) ClearData(ctx context.Context, mode string, olderThan time.Time) (int64, error) {
	var freed int64
	err := h.walkProfile(ctx, mode, func(path, rel string, info os.FileInfo) error {
		if (!isCachePath(rel) && !isCookiesPath(rel)) || !info.ModTime().Before(olderThan) {
			return nil
		}
		guard, err := commandworker.PinWorkingDirectory(h.profileRoot, filepath.Dir(path))
		if err != nil {
			return err
		}
		defer guard.Close()
		root, err := os.OpenRoot(h.profileRoot)
		if err != nil {
			return err
		}
		defer root.Close()
		relative, err := filepath.Rel(h.profileRoot, path)
		if err != nil {
			return err
		}
		if err := root.Remove(relative); err != nil {
			return err
		}
		freed += info.Size()
		return nil
	})
	return freed, err
}

func (h *LocalHost) killProc(ctx context.Context, sessionID string) error {
	h.mu.Lock()
	managed := h.procs[sessionID]
	h.mu.Unlock()
	if managed == nil {
		return nil
	}
	// Cut all owned network connections immediately even if CDP teardown fails.
	if managed.proxy != nil {
		_ = managed.proxy.Close()
	}
	var err error
	if managed.cancel != nil {
		managed.cancel()
		select {
		case <-managed.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if managed.context != nil {
			managed.context.closeConnection()
		}
	} else if managed.context != nil {
		err = managed.context.Close(ctx)
	}
	if err == nil {
		h.mu.Lock()
		if h.procs[sessionID] == managed {
			delete(h.procs, sessionID)
		}
		h.mu.Unlock()
	}
	return err
}

// Only browser/system environment crosses into the managed process.
func browserEnvironment() []string {
	allowed := map[string]bool{"SYSTEMROOT": true, "WINDIR": true, "PATH": true, "TEMP": true, "TMP": true, "LOCALAPPDATA": true, "APPDATA": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "HOME": true, "DISPLAY": true, "XAUTHORITY": true, "LANG": true}
	var env []string
	for _, pair := range os.Environ() {
		key, _, ok := strings.Cut(pair, "=")
		if ok && allowed[strings.ToUpper(key)] {
			env = append(env, pair)
		}
	}
	return env
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
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: CDP version status %d", ErrBrMode, resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if derr := dec.Decode(&doc); derr != nil || doc.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%w: bridge port %d not a CDP endpoint", ErrBrMode, port)
	}
	wsHost, wsPort, wsErr := wsHostPort(doc.WebSocketDebuggerURL)
	if wsErr != nil || wsHost != host || wsPort != port {
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
	if err != nil || u.Scheme != "ws" || u.User != nil || u.Hostname() != "127.0.0.1" || u.RawQuery != "" || u.Fragment != "" {
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

func (h *LocalHost) IsSessionRunning(sessionID, mode string) bool {
	if mode == ModeBuiltin {
		return true
	}
	h.mu.Lock()
	managed := h.procs[sessionID]
	h.mu.Unlock()
	if managed == nil {
		return false
	}
	if managed.done != nil {
		select {
		case <-managed.done:
			return false
		default:
		}
	}
	return managed.context != nil
}
func (h *LocalHost) Close() error {
	h.mu.Lock()
	ids := make([]string, 0, len(h.procs))
	for id := range h.procs {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var all error
	for _, id := range ids {
		all = errors.Join(all, h.killProc(ctx, id))
	}
	return all
}
