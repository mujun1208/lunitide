// onnx_engine_install.go is the on-demand download of the offline ONNX voice:
// the sherpa-onnx offline-tts runtime plus the Kokoro multi-lang model. It is
// the install-and-use counterpart of the GPT-SoVITS RefEngineInstall — same
// begin/run/snapshot shape and the same digest-pinned voice.Installer — but it
// fetches two bundles in sequence and reports one continuous progress bar so
// the settings row moves smoothly from 0 to 100 across both archives.
package app

import (
	"context"
	"log"
	"sync"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/tts"
	"github.com/lunitide/lunitide/internal/voice"
)

// OnnxEngineInstall downloads and verifies the offline ONNX voice bundles.
type OnnxEngineInstall struct {
	root      string
	installer *voice.Installer
	// installBundle is the test seam: nil uses the real installer; a stub
	// lets a test drive begin/run without a real multi-hundred-MB download.
	installBundle func(context.Context, voice.Bundle, func(voice.Progress)) error

	mu         sync.Mutex
	progress   voice.Progress
	state      string
	lastErr    string
	installing bool
}

// NewOnnxEngineInstall roots the installer at the per-user data directory so a
// verified extraction lands exactly where the ONNX engine probes.
func NewOnnxEngineInstall() *OnnxEngineInstall {
	root := tts.RefEngineDataRoot()
	return &OnnxEngineInstall{
		root:      root,
		installer: &voice.Installer{Root: root},
		state:     "idle",
	}
}

// SetOnnxEngineInstall wires the on-demand ONNX voice download.
func (e *Engine) SetOnnxEngineInstall(s *OnnxEngineInstall) { e.onnxInstall = s }

// refresh recomputes readiness without ever starting a download. It is the
// probe path (settings open, companion entry, progress polling): the 350 MB
// model must never be pulled just because something asked whether it is ready.
func (s *OnnxEngineInstall) refresh() {
	if s == nil || s.root == "" {
		return
	}
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	installed := true
	for _, b := range tts.OnnxBundles() {
		if !s.installer.Installed(b) {
			installed = false
			break
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.installing {
		return
	}
	if installed {
		s.state, s.lastErr = "ready", ""
		return
	}
	// Leave a terminal failure visible; otherwise settle on idle so the row
	// offers a download rather than implying one is under way.
	if s.state != "failed" {
		s.state = "idle"
	}
}

// begin launches the download unless one is already running or both bundles
// are already installed. For archive bundles Installed() only compares a
// receipt digest, so this is cheap enough to run on every settings poll.
func (s *OnnxEngineInstall) begin() {
	if s == nil || s.root == "" {
		return
	}
	bundles := tts.OnnxBundles()

	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	allInstalled := true
	for _, b := range bundles {
		if !s.installer.Installed(b) {
			allInstalled = false
			break
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.installing {
		return
	}
	if allInstalled {
		s.state, s.lastErr = "ready", ""
		return
	}
	s.installing, s.state, s.lastErr, s.progress = true, "downloading", "", voice.Progress{}
	go s.run(bundles)
}

func (s *OnnxEngineInstall) run(bundles []voice.Bundle) {
	defer func() {
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
	}()
	install := s.installBundle
	if install == nil {
		install = s.installer.Install
	}
	var grandTotal int64
	for _, b := range bundles {
		grandTotal += b.TotalBytes()
	}
	var base int64
	for _, b := range bundles {
		bundle := b
		err := install(context.Background(), bundle, func(p voice.Progress) {
			s.mu.Lock()
			s.progress = voice.Progress{BundleID: p.BundleID, File: p.File, Done: base + p.Done, Total: grandTotal}
			s.mu.Unlock()
		})
		if err != nil {
			log.Printf("tts.installOnnxEngine: %v", err)
			s.setState("failed", err.Error())
			return
		}
		base += bundle.TotalBytes()
	}
	s.setState("ready", "")
}

func (s *OnnxEngineInstall) setState(state, lastErr string) {
	s.mu.Lock()
	s.state, s.lastErr = state, lastErr
	s.mu.Unlock()
}

func (s *OnnxEngineInstall) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{
		"state":      s.state,
		"percent":    s.progress.Percent(),
		"doneBytes":  s.progress.Done,
		"totalBytes": s.progress.Total,
	}
	if s.progress.File != "" {
		out["file"] = s.progress.File
	}
	if s.lastErr != "" {
		out["lastError"] = truncate(s.lastErr, 512)
	}
	return out
}

// handleTtsInstallOnnxEngine reports install progress and, only when the
// payload sets start=true, launches the download. The default (start omitted)
// is a read-only probe so a settings-open or companion-entry readiness check
// never pulls the 350 MB model; the UI polls it and reads state=="ready" once
// both bundles are verified on disk.
func handleTtsInstallOnnxEngine(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Start bool `json:"start"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "tts.installOnnxEngine 参数无效", false)
	}
	if e.onnxInstall == nil || e.onnxInstall.root == "" {
		return r.Ok(map[string]any{"state": "not_configured", "percent": 0, "doneBytes": 0, "totalBytes": 0})
	}
	if p.Start {
		e.onnxInstall.begin()
	} else {
		e.onnxInstall.refresh()
	}
	return r.Ok(e.onnxInstall.snapshot())
}
