package omni

import (
	"errors"
	"fmt"
	"net/http"
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
	ErrMissingRuntime = errors.New("omni: 本机推理进程未能展开")
	ErrNotReady       = errors.New("omni: 本机模型服务尚未就绪")
)

// Host owns the llama-omni-server child and the GGUF directory.
type Host struct {
	Root     string
	Listen   string
	Endpoint string
	Finder   func() string
	Present  func() bool
	HTTP     *http.Client
	// Payload is a zip or directory of llama-omni-server. Empty discovers
	// omni/llama-omni-runtime.zip next to the engine.
	Payload   string
	installer *voice.Installer

	mu        sync.Mutex
	extractMu sync.Mutex
	cmd       *exec.Cmd
	state     string
	lastErr   string
}

// NewHost roots downloads and the bundled runtime binary under dir.
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

// Installed reports whether the Q4 bundle is on disk at the expected sizes.
// Status/start use presence, not a full re-hash: hashing Q4 on every probe
// exceeds the bridge deadline.
func (h *Host) Installed() bool {
	if h.Present != nil {
		return h.Present()
	}
	return h.installer.Present(ModelBundle())
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

// RuntimePath is llama-omni-server on disk, or empty. Does not extract.
func (h *Host) RuntimePath() string {
	if h.Finder != nil {
		return h.Finder()
	}
	return findRuntime(h.Root)
}

// EnsureRuntime copies the bundled payload into root/runtime when missing.
func (h *Host) EnsureRuntime() error {
	if h.Finder != nil {
		return nil
	}
	h.extractMu.Lock()
	defer h.extractMu.Unlock()
	return EnsureBundledRuntime(h.Root, h.Payload)
}

// Snapshot is the settings/status payload.
func (h *Host) Snapshot() map[string]any {
	installed := h.Installed()
	runtimePath := h.RuntimePath()
	// A black-holed :19080 (or a hung PATH LookPath, now removed) used to
	// stall omni.status so settings sat on 「正在检测 MiniCPM-o 4.5…」.
	// Probe loopback only when a binary or our child might actually answer.
	probeHealth := runtimePath != ""
	if !probeHealth {
		h.mu.Lock()
		probeHealth = h.cmd != nil && h.cmd.Process != nil
		h.mu.Unlock()
	}
	healthy := false
	if probeHealth {
		healthy = h.Healthy()
	}
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
	if err := h.EnsureRuntime(); err != nil {
		h.mu.Lock()
		h.state, h.lastErr = HostMissingRuntime, err.Error()
		h.mu.Unlock()
		return HostMissingRuntime, err
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
