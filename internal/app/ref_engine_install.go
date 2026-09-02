// ref_engine_install.go is the on-demand download of the local GPT-SoVITS
// engine, the TTS counterpart of VoiceService's ASR runtime install. The
// desktop package ships nothing large; when the user first picks the 本地
// voice path the engine pack is pulled into %LOCALAPPDATA%\Lunitide\gpt-sovits
// and verified by digest, after which refHostScriptCandidates discovers the
// launcher and the 50 preset voices work offline.
//
// It mirrors VoiceService.begin/run/snapshot exactly: a bridge call returns
// immediately with a progress snapshot and starts the transfer once, because
// a multi-GB download cannot be held open inside one bridge round-trip.
package app

import (
	"context"
	"log"
	"sync"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/tts"
	"github.com/lunitide/lunitide/internal/voice"
)

// RefEngineInstall downloads and verifies the local GPT-SoVITS engine pack.
type RefEngineInstall struct {
	root      string
	installer *voice.Installer
	// installBundle is the test seam: nil means the real installer, a stub
	// lets a test hold a transfer in flight and prove idempotency without a
	// real (multi-GB, unreachable) download.
	installBundle func(context.Context, voice.Bundle, func(voice.Progress)) error
	// launcherPresent reports whether a usable launcher already exists on
	// disk (portable copy, prior install, or the legacy dev layout), so a
	// machine that already has the engine reports ready without a download.
	launcherPresent func() bool

	mu         sync.Mutex
	progress   voice.Progress
	state      string
	lastErr    string
	installing bool
}

// NewRefEngineInstall roots the installer at the per-user data directory so a
// verified extraction lands exactly where the launcher probes.
func NewRefEngineInstall() *RefEngineInstall {
	root := tts.RefEngineDataRoot()
	return &RefEngineInstall{
		root:            root,
		installer:       &voice.Installer{Root: root},
		launcherPresent: tts.RefEngineLauncherPresent,
		state:           "idle",
	}
}

// SetRefEngineInstall wires the on-demand engine download.
func (e *Engine) SetRefEngineInstall(s *RefEngineInstall) { e.refInstall = s }

// begin loads the manifest and launches the download unless one is already
// running, the pack is already installed, or a launcher is already on disk.
func (s *RefEngineInstall) begin() {
	if s == nil || s.root == "" {
		return
	}
	if s.launcherPresent != nil && s.launcherPresent() {
		s.setState("ready", "")
		return
	}
	manifest, err := tts.LoadRefEnginePackManifest(s.root)
	if err != nil {
		s.setState("not_configured", "")
		return
	}
	bundle := tts.RefEngineBundle(manifest)

	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return
	}
	if s.installer.Installed(bundle) {
		s.state, s.lastErr = "ready", ""
		s.mu.Unlock()
		return
	}
	s.installing, s.state, s.lastErr, s.progress = true, "downloading", "", voice.Progress{}
	s.mu.Unlock()

	go s.run(bundle)
}

func (s *RefEngineInstall) run(bundle voice.Bundle) {
	defer func() {
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
	}()
	install := s.installBundle
	if install == nil {
		install = s.installer.Install
	}
	err := install(context.Background(), bundle, func(p voice.Progress) {
		s.mu.Lock()
		s.progress = p
		s.mu.Unlock()
	})
	if err != nil {
		log.Printf("tts.installRefEngine: %v", err)
		s.setState("failed", err.Error())
		return
	}
	s.setState("ready", "")
}

func (s *RefEngineInstall) setState(state, lastErr string) {
	s.mu.Lock()
	s.state, s.lastErr = state, lastErr
	s.mu.Unlock()
}

func (s *RefEngineInstall) snapshot() map[string]any {
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

// handleTtsInstallRefEngine starts the engine download if needed and returns
// where the current one has got to. Idempotent: repeated calls are how the UI
// polls progress.
func handleTtsInstallRefEngine(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.refInstall == nil || e.refInstall.root == "" {
		return r.Ok(map[string]any{"state": "not_configured", "percent": 0, "doneBytes": 0, "totalBytes": 0})
	}
	e.refInstall.begin()
	return r.Ok(e.refInstall.snapshot())
}
