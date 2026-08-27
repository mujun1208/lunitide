package omni

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/voice"
)

// Host states surfaced through omni.status / omni.ensure.
const (
	HostIdle           = "idle"
	HostDownloading    = "downloading"
	HostLaunching      = "launching"
	HostReady          = "ready"
	HostFailed         = "failed"
	HostMissingModel   = "missing_model"
	HostMissingRuntime = "missing_runtime"
)

var (
	ErrMissingModel   = errors.New("omni: MiniCPM-o 4.5 Q4 尚未下载")
	ErrMissingRuntime = errors.New("omni: 未找到 llama-omni-server")
	ErrNotReady       = errors.New("omni: 本机模型服务尚未就绪")
)

// Host owns the llama-omni-server child and the GGUF directory.
type Host struct {
	Root      string
	Listen    string
	Endpoint  string
	Finder    func() string
	HTTP      *http.Client
	installer *voice.Installer

	mu      sync.Mutex
	cmd     *exec.Cmd
	state   string
	lastErr string
}

// NewHost roots downloads and the optional runtime binary under dir.
func NewHost(root string) *Host {
	return &Host{
		Root:      root,
		Listen:    ListenAddr,
		Endpoint:  HTTPProbe,
		installer: &voice.Installer{Root: root},
		HTTP:      &http.Client{Timeout: 2 * time.Second},
		state:     HostIdle,
	}
}

// ModelDir is where the Q4 GGUF tree lands.
func (h *Host) ModelDir() string {
	return h.installer.BundleDir(BundleID)
}

// Installed reports whether the Q4 bundle is complete.
func (h *Host) Installed() bool {
	return h.installer.Installed(ModelBundle())
}

func (h *Host) baseURL() string {
	if h.Endpoint != "" {
		return strings.TrimRight(h.Endpoint, "/")
	}
	return HTTPProbe
}

func (h *Host) healthClient() *http.Client {
	if h.HTTP != nil {
		return h.HTTP
	}
	return &http.Client{Timeout: 2 * time.Second}
}

// Healthy is GET /health on loopback.
func (h *Host) Healthy() bool {
	resp, err := h.healthClient().Get(h.baseURL() + "/health")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// RuntimePath is llama-omni-server on disk, or empty.
func (h *Host) RuntimePath() string {
	if h.Finder != nil {
		return h.Finder()
	}
	return findRuntime(h.Root)
}

func findRuntime(root string) string {
	names := []string{"llama-omni-server.exe", "llama-omni-server"}
	var candidates []string
	for _, name := range names {
		candidates = append(candidates,
			filepath.Join(root, "runtime", name),
			filepath.Join(root, name),
		)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates,
			filepath.Join(local, "Programs", "Comni", "llama-omni-server.exe"),
			filepath.Join(local, "Comni", "llama-omni-server.exe"),
		)
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "Comni", "llama-omni-server.exe"))
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	for _, name := range names {
		if found, err := exec.LookPath(name); err == nil {
			return found
		}
	}
	return ""
}

// Snapshot is the settings/status payload.
func (h *Host) Snapshot() map[string]any {
	installed := h.Installed()
	runtimePath := h.RuntimePath()
	healthy := h.Healthy()
	h.mu.Lock()
	state, lastErr := h.state, h.lastErr
	h.mu.Unlock()
	if healthy {
		state, lastErr = HostReady, ""
	} else if state == HostDownloading || state == HostLaunching || state == HostFailed {
		// keep
	} else if !installed {
		state = HostMissingModel
	} else if runtimePath == "" {
		state = HostMissingRuntime
	} else {
		state = HostIdle
	}
	bundle := ModelBundle()
	return map[string]any{
		"supported":     true,
		"ready":         healthy,
		"installed":     installed,
		"runtimeFound":  runtimePath != "" || healthy,
		"hostState":     state,
		"downloadBytes": bundle.TotalBytes(),
		"title":         bundle.Title,
		"lastError":     lastErr,
	}
}

// Ensure starts llama-omni-server on loopback when the model is present.
// It returns immediately with launching; first boot can take 10–60s.
func (h *Host) Ensure() (string, error) {
	if h.Healthy() {
		h.mu.Lock()
		h.state, h.lastErr = HostReady, ""
		h.mu.Unlock()
		return HostReady, nil
	}
	if !h.Installed() {
		h.mu.Lock()
		h.state, h.lastErr = HostMissingModel, ErrMissingModel.Error()
		h.mu.Unlock()
		return HostMissingModel, ErrMissingModel
	}
	bin := h.RuntimePath()
	if bin == "" {
		h.mu.Lock()
		h.state, h.lastErr = HostMissingRuntime, ErrMissingRuntime.Error()
		h.mu.Unlock()
		return HostMissingRuntime, ErrMissingRuntime
	}
	h.mu.Lock()
	if h.state == HostLaunching && h.cmd != nil && h.cmd.Process != nil {
		h.mu.Unlock()
		return HostLaunching, nil
	}
	if err := h.spawnLocked(bin); err != nil {
		h.state, h.lastErr = HostFailed, err.Error()
		h.mu.Unlock()
		return HostFailed, err
	}
	h.state, h.lastErr = HostLaunching, ""
	h.mu.Unlock()
	return HostLaunching, nil
}

func (h *Host) spawnLocked(bin string) error {
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
		h.cmd = nil
	}
	listen := h.Listen
	if listen == "" {
		listen = ListenAddr
	}
	host, port, ok := strings.Cut(listen, ":")
	if !ok || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return fmt.Errorf("omni: listen address must be loopback, got %s", listen)
	}
	model := filepath.Join(h.ModelDir(), LLMFile)
	args := []string{
		"--host", host,
		"--port", port,
		"--model", model,
		"-ngl", "99",
		"--ctx-size", "8192",
		"--repeat-penalty", "1.05",
		"--temp", "0.7",
	}
	cmd, err := startOmniServer(bin, args)
	if err != nil {
		return err
	}
	h.cmd = cmd
	return nil
}

// WaitReady polls /health until ctx is done.
func (h *Host) WaitReady(ctxDone <-chan struct{}) error {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		if h.Healthy() {
			h.mu.Lock()
			h.state, h.lastErr = HostReady, ""
			h.mu.Unlock()
			return nil
		}
		select {
		case <-ctxDone:
			return ErrNotReady
		case <-ticker.C:
		}
	}
}

// Stop kills the hosted server.
func (h *Host) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
	}
	h.cmd = nil
	if h.state == HostLaunching || h.state == HostReady {
		h.state = HostIdle
	}
}
