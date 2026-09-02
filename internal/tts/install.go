// install.go turns the local GPT-SoVITS engine into an on-demand download,
// matching how the sherpa ASR runtime is fetched: the installer is small,
// nothing ships in the desktop package, and the multi-GB engine + models are
// pulled into %LOCALAPPDATA%\Lunitide\gpt-sovits on first use and verified by
// digest before anything under the final name is trusted.
//
// The engine pack itself is not in this repository (it is a Python + PyTorch
// + weights tree, far too large and explicitly rejected by the release
// layout gate), so its source is supplied by a manifest rather than hardcoded
// here: an operator hosts one immutable tar.bz2, records its URL + SHA-256 +
// size, and drops that into an env override or a pack.manifest.json. The
// download/verify/extract mechanics are the battle-tested voice.Installer.
package tts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/voice"
)

// RefEngineBundleID is the install directory name under the data root. It is
// the same "gpt-sovits" segment refHostScriptCandidates probes, so a verified
// extraction is immediately discoverable by the launcher.
const RefEngineBundleID = "gpt-sovits"

// refEngineManifestFile is the per-user manifest that names the hosted pack
// when no env override is set. It lives inside the install directory so a
// pack and the record of where it came from travel together.
const refEngineManifestFile = "pack.manifest.json"

// ErrRefEnginePackNotConfigured means no manifest (env or file) points at a
// downloadable engine pack, so there is nothing to install yet.
var ErrRefEnginePackNotConfigured = errors.New("ref engine pack not configured")

// RefEnginePackManifest describes one immutable engine pack to fetch. The
// archive must expand to a tree whose top level contains start-api-cpu.bat
// (after StripComponents), i.e. exactly the layout the launcher probes.
type RefEnginePackManifest struct {
	URL             string `json:"url"`
	SHA256          string `json:"sha256"`
	Bytes           int64  `json:"bytes"`
	StripComponents int    `json:"stripComponents"`
	Title           string `json:"title"`
	Detail          string `json:"detail"`
}

// RefEngineDataRoot is the per-user directory that holds the installed engine
// (…\Lunitide). Empty when the platform gives us no per-user location, which
// the caller treats as "download unavailable".
func RefEngineDataRoot() string {
	if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
		return filepath.Join(base, "Lunitide")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".lunitide")
	}
	return ""
}

// LoadRefEnginePackManifest resolves the pack source. An explicit
// LUNITIDE_REF_ENGINE_PACK_URL (with _SHA256/_BYTES/_STRIP) wins so CI and
// power users can point at any host without editing files; otherwise the
// manifest JSON inside the install directory is read. Returns
// ErrRefEnginePackNotConfigured when neither names a URL.
func LoadRefEnginePackManifest(root string) (RefEnginePackManifest, error) {
	if url := strings.TrimSpace(os.Getenv("LUNITIDE_REF_ENGINE_PACK_URL")); url != "" {
		m := RefEnginePackManifest{
			URL:             url,
			SHA256:          strings.ToLower(strings.TrimSpace(os.Getenv("LUNITIDE_REF_ENGINE_PACK_SHA256"))),
			Bytes:           parseInt64(os.Getenv("LUNITIDE_REF_ENGINE_PACK_BYTES")),
			StripComponents: int(parseInt64(os.Getenv("LUNITIDE_REF_ENGINE_PACK_STRIP"))),
		}
		return m, nil
	}
	if root == "" {
		return RefEnginePackManifest{}, ErrRefEnginePackNotConfigured
	}
	raw, err := os.ReadFile(filepath.Join(root, RefEngineBundleID, refEngineManifestFile))
	if err != nil {
		return RefEnginePackManifest{}, ErrRefEnginePackNotConfigured
	}
	var m RefEnginePackManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return RefEnginePackManifest{}, err
	}
	m.URL = strings.TrimSpace(m.URL)
	m.SHA256 = strings.ToLower(strings.TrimSpace(m.SHA256))
	if m.URL == "" {
		return RefEnginePackManifest{}, ErrRefEnginePackNotConfigured
	}
	return m, nil
}

// RefEngineBundle turns a manifest into the single-archive bundle the
// installer fetches. StripComponents defaults to 1 because a well-formed
// release wraps its tree in one top directory.
func RefEngineBundle(m RefEnginePackManifest) voice.Bundle {
	strip := m.StripComponents
	if strip <= 0 {
		strip = 1
	}
	title := m.Title
	if title == "" {
		title = "本地语音引擎（GPT-SoVITS）"
	}
	detail := m.Detail
	if detail == "" {
		detail = "本地克隆音色引擎与模型，安装后 50 种音色可离线试听。"
	}
	return voice.Bundle{
		ID:     RefEngineBundleID,
		Kind:   voice.BundleRuntime,
		Title:  title,
		Detail: detail,
		Downloads: []voice.Download{{
			Path:            "", // extract into the bundle directory root
			URLs:            []string{m.URL},
			SHA256:          m.SHA256,
			Bytes:           m.Bytes,
			Archive:         voice.ArchiveTarBz2,
			StripComponents: strip,
		}},
	}
}

func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}
