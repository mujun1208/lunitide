// onnx_install.go declares the two on-demand downloads that make the offline
// ONNX voice install-and-use: the sherpa-onnx offline-tts runtime (~21 MB)
// and the Kokoro multi-lang model (~333 MB). Neither ships in the desktop
// package; both land under %LOCALAPPDATA%\Lunitide and are verified by the
// same digest-pinned voice.Installer the ASR runtime uses.
//
// The default sources are the upstream k2-fsa GitHub release assets, pinned
// by SHA-256 so a mirror can only serve the exact bytes named or fail. The
// digests and sizes were recorded from the published assets at wiring time;
// an operator can repoint either download with the env overrides below
// without editing this file.
package tts

import (
	"os"
	"strings"

	"github.com/lunitide/lunitide/internal/voice"
)

const (
	// Pinned sherpa-onnx 1.13.6 win-x64 shared MT-MinSizeRel build *with*
	// TTS (the ASR bundle is the -no-tts twin). Static MSVC runtime, so no
	// VC++ redistributable is needed; onnxruntime DLLs sit beside the exe.
	onnxRuntimeURLDefault = "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.6/sherpa-onnx-v1.13.6-win-x64-shared-MT-MinSizeRel.tar.bz2"
	onnxRuntimeSHA256     = "57a481556035de027fde407088a72f1cbadf363361f345c11e405dab637658c8"
	onnxRuntimeBytes      = 21210111

	// Pinned Kokoro multi-lang v1.0 pack: model.onnx, voices.bin (53
	// speakers), tokens, us-en/zh lexicons, zh rule FSTs and espeak-ng-data.
	onnxModelURLDefault = "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/kokoro-multi-lang-v1_0.tar.bz2"
	onnxModelSHA256     = "c133d26353d776da730870dac7da07dbfc9a5e3bc80cc5e8e83ab6e823be7046"
	onnxModelBytes      = 349418188
)

// envOr returns the trimmed env value when set, else the fallback.
func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// envBytes returns a parsed env byte count when set and valid, else the
// pinned fallback. Keeping the pinned size as the default matters: it sizes
// the progress bar before the first byte and lets the installer reject a
// server whose Content-Length disagrees with the archive we pinned.
func envBytes(key string, fallback int64) int64 {
	if n := parseInt64(os.Getenv(key)); n > 0 {
		return n
	}
	return fallback
}

// OnnxRuntimeBundle is the sherpa-onnx offline-tts runtime download.
func OnnxRuntimeBundle() voice.Bundle {
	return voice.Bundle{
		ID:     OnnxRuntimeBundleID,
		Kind:   voice.BundleRuntime,
		Title:  "本地语音引擎（Kokoro）",
		Detail: "sherpa-onnx 离线合成引擎（Windows x64，约 21 MB）",
		Downloads: []voice.Download{{
			Path:            "",
			URLs:            []string{envOr("LUNITIDE_ONNX_TTS_RUNTIME_URL", onnxRuntimeURLDefault)},
			SHA256:          strings.ToLower(envOr("LUNITIDE_ONNX_TTS_RUNTIME_SHA256", onnxRuntimeSHA256)),
			Bytes:           envBytes("LUNITIDE_ONNX_TTS_RUNTIME_BYTES", onnxRuntimeBytes),
			Archive:         voice.ArchiveTarBz2,
			StripComponents: 1,
		}},
	}
}

// OnnxModelBundle is the Kokoro multi-lang model download.
func OnnxModelBundle() voice.Bundle {
	return voice.Bundle{
		ID:     OnnxModelBundleID,
		Kind:   voice.BundleModel,
		Title:  "本地中文语音模型（Kokoro 多语）",
		Detail: "Kokoro multi-lang v1.0，含 8 个中文音色，约 333 MB",
		Downloads: []voice.Download{{
			Path:            "",
			URLs:            []string{envOr("LUNITIDE_ONNX_TTS_MODEL_URL", onnxModelURLDefault)},
			SHA256:          strings.ToLower(envOr("LUNITIDE_ONNX_TTS_MODEL_SHA256", onnxModelSHA256)),
			Bytes:           envBytes("LUNITIDE_ONNX_TTS_MODEL_BYTES", onnxModelBytes),
			Archive:         voice.ArchiveTarBz2,
			StripComponents: 1,
		}},
	}
}

// OnnxBundles lists the downloads in install order: the small runtime first,
// then the large model, so an interrupted install keeps the cheap part.
func OnnxBundles() []voice.Bundle {
	return []voice.Bundle{OnnxRuntimeBundle(), OnnxModelBundle()}
}
