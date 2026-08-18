// refhost.go hosts the lifecycle of the local GPT-SoVITS api_v2 service
// so the 50-preset ref catalogue works without the user manually
// launching the model server: when 9880 is down and a start script is
// detected, Synthesize auto-spawns it (hidden window), polls /docs until
// the model finishes loading, and the engine kills the spawned tree on
// shutdown. State machine: not_configured → launching → online / offline.
package tts

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultRefHostScripts are probed in order for a launcher that can
// bring the local GPT-SoVITS api_v2 service up (first hit wins). A var
// (not const) so tests can point it at fixtures.
var DefaultRefHostScripts = []string{
	`E:\GPT-SoVITS\start-api-cpu.bat`,
}

// Ref host states surfaced through tts.voices ref_meta and
// tts.ensureRefEngine.
const (
	RefHostOnline        = "online"         // /docs answers 200
	RefHostLaunching     = "launching"      // spawned, model still loading
	RefHostNotConfigured = "not_configured" // no launcher script found
	RefHostOffline       = "offline"        // last launch attempt failed
)

// ErrRefEngineStarting marks the window where the hosted GPT-SoVITS
// service is coming up (CPU cold load can take 30–90s). The bridge maps
// it onto the retryable M95-001 family so the player waits instead of
// circuit-breaking.
var ErrRefEngineStarting = errors.New("ref engine starting")

// DefaultRefHost is the process-singleton shared by the ref engine, the
// voices probe and the ensureRefEngine bridge method. main.go owns its
// shutdown (defer Stop) and may point its launcher log at the engine
// log directory.
var DefaultRefHost = NewRefHost(DefaultRefHostScripts...)

// RefHost manages one hosted api_v2 child process tree.
type RefHost struct {
	mu        sync.Mutex
	scripts   []string
	script    string // resolved launcher script ("" → none found)
	cmd       *exec.Cmd
	logFile   *os.File
	state     string
	lastErr   string
	startedAt time.Time
}

// NewRefHost returns a host probing the given launcher scripts (falls
// back to DefaultRefHostScripts when none are given).
func NewRefHost(scripts ...string) *RefHost {
	if len(scripts) == 0 {
		scripts = DefaultRefHostScripts
	}
	return &RefHost{scripts: scripts, state: RefHostNotConfigured}
}

// SetLogDir redirects the hosted process stdout/stderr into
// ref-engine-*.log files under dir (best effort; silent no-op when dir
// cannot be used).
func (h *RefHost) SetLogDir(dir string) {
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("ref-engine-%s.log", time.Now().Format("20060102-150405")))
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.logFile != nil {
			_ = h.logFile.Close()
		}
		h.logFile = f
	}
}

// resolveScript finds the first launcher script that exists on disk.
func (h *RefHost) resolveScript() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.script != "" {
		return h.script
	}
	for _, s := range h.scripts {
		if info, err := os.Stat(s); err == nil && !info.IsDir() {
			h.script = s
			return s
		}
	}
	return ""
}

// refLiveness probes {endpoint}/docs once (any FastAPI 200 = alive). The
// budget must tolerate GPT-SoVITS saturating the CPU mid-synthesis: a 900ms
// probe times out while the model is legitimately busy and the host would
// misread a healthy server as dead.
func refLiveness(endpoint string) bool {
	if endpoint == "" {
		endpoint = DefaultRefEndpoint
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(endpoint, "/") + "/docs")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Status reports the live host state. It re-probes /docs when idle
// (cheap) so a server the user started manually shows as online, and
// keeps "launching" while a spawn is in flight.
func (h *RefHost) Status(endpoint string) (state, script string) {
	script = h.resolveScript()
	if refLiveness(endpoint) {
		h.mu.Lock()
		// An externally started server also clears our spawn bookkeeping.
		if h.state != RefHostOnline {
			h.state = RefHostOnline
			h.lastErr = ""
		}
		state = h.state
		h.mu.Unlock()
		return state, script
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch h.state {
	case RefHostLaunching:
		// still loading — keep reporting launching
		state = h.state
	case RefHostOnline:
		// we thought it was up but /docs is gone: the tree died
		h.state = RefHostOffline
		h.lastErr = "service stopped answering"
		state = h.state
	default:
		if h.script == "" && script == "" {
			h.state = RefHostNotConfigured
		} else {
			h.state = RefHostOffline
		}
		state = h.state
	}
	return state, script
}

// busyGrace bounds how long EnsureRunning re-probes a server it believed
// online before concluding the tree really died. GPT-SoVITS on CPU can pin
// all cores for tens of seconds per synthesis; without this grace window a
// single missed probe spawns a SECOND model server (2× memory, port 9880
// conflict) and destabilizes the whole machine.
const busyGrace = 15 * time.Second

// EnsureRunning brings the api_v2 service up. Fast path: /docs already
// answers. Otherwise it spawns the launcher script once (concurrent
// callers wait on the same launch) and polls /docs until ready or the
// wait budget runs out. A probe failure against a server we recently saw
// online is treated as transient CPU contention and re-probed through the
// grace window — never as a reason to spawn a second model tree.
func (h *RefHost) EnsureRunning(endpoint string, wait time.Duration) error {
	if refLiveness(endpoint) {
		h.mu.Lock()
		h.state = RefHostOnline
		h.lastErr = ""
		h.mu.Unlock()
		return nil
	}
	script := h.resolveScript()
	if script == "" {
		h.mu.Lock()
		h.state = RefHostNotConfigured
		h.lastErr = "未找到 GPT-SoVITS 启动脚本"
		h.mu.Unlock()
		return errors.New("ref host: no launcher script configured")
	}

	h.mu.Lock()
	state := h.state
	h.mu.Unlock()
	if state == RefHostOnline {
		// We saw /docs answer recently: the failed probe is most likely the
		// server being busy synthesizing, not dead. Re-probe through the
		// grace window before considering a respawn.
		if h.awaitReady(endpoint, busyGrace) == nil {
			return nil
		}
		// A concurrent caller may have just recovered or re-spawned the
		// service while we probed without the lock; verify once more so we
		// never spawn a second tree over a live server.
		if refLiveness(endpoint) {
			return nil
		}
	}

	h.mu.Lock()
	if h.state == RefHostLaunching && time.Since(h.startedAt) < 3*time.Minute {
		// Adopt the in-flight launch instead of spawning a second tree.
		h.mu.Unlock()
		return h.awaitReady(endpoint, wait)
	}
	if h.state == RefHostOnline {
		h.state = RefHostOffline
		h.lastErr = "service stopped answering"
	}
	if err := h.spawnLocked(script); err != nil {
		h.state = RefHostOffline
		h.lastErr = err.Error()
		h.mu.Unlock()
		return fmt.Errorf("ref host spawn: %w", err)
	}
	h.state = RefHostLaunching
	h.startedAt = time.Now()
	h.lastErr = ""
	h.mu.Unlock()
	return h.awaitReady(endpoint, wait)
}

// spawnLocked starts the launcher hidden; caller must hold h.mu.
func (h *RefHost) spawnLocked(script string) error {
	// A previous tree that stopped answering must not survive the respawn:
	// it would keep port 9880 bound and leak across app restarts.
	if h.cmd != nil && h.cmd.Process != nil {
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(h.cmd.Process.Pid), "/T", "/F").Run()
		h.cmd = nil
	}
	// cmd /c is required for .bat launchers; /T tree-kill on Stop.
	cmd := exec.Command("cmd", "/c", script)
	cmd.Dir = filepath.Dir(script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	if h.logFile != nil {
		cmd.Stdout = h.logFile
		cmd.Stderr = h.logFile
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	h.cmd = cmd
	return nil
}

// awaitReady polls /docs until the model answers or the budget is gone.
func (h *RefHost) awaitReady(endpoint string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		time.Sleep(1500 * time.Millisecond)
		if refLiveness(endpoint) {
			h.mu.Lock()
			h.state = RefHostOnline
			h.lastErr = ""
			h.mu.Unlock()
			return nil
		}
	}
	h.mu.Lock()
	state, lastErr := h.state, h.lastErr
	h.mu.Unlock()
	if state == RefHostOnline { // lost the race, probe flipped it
		return nil
	}
	return fmt.Errorf("%w: 语音引擎仍在启动（%s）", ErrRefEngineStarting, lastErr)
}

// LastErr returns the most recent launch failure description.
func (h *RefHost) LastErr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastErr
}

// Stop kills the hosted process tree (cmd /T kills the python child
// too) and closes the launcher log. Idempotent; safe to defer.
func (h *RefHost) Stop() {
	h.mu.Lock()
	cmd := h.cmd
	h.cmd = nil
	h.state = RefHostNotConfigured
	h.lastErr = ""
	logFile := h.logFile
	h.logFile = nil
	h.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		// Tree-kill: the .bat wraps env\python.exe, so killing the cmd
		// PID alone would leak the python server holding port 9880.
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
	}
	if logFile != nil {
		_ = logFile.Close()
	}
}
