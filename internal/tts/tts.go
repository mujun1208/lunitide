// Package tts implements the M9.5 Moon Companion text-to-speech
// runtime: free Microsoft cloud neural voices, offline Windows SAPI /
// OneCore, and local GPT-SoVITS cloning, with a single-flight queue,
// session-scoped cancellation, segment de-duplication and a graceful
// non-Windows fallback (M95-001 semantics, subtitle-only degradation).
package tts

import (
	"context"
	"errors"
)

// Voice is one speech token exposed via tts.voices. SAPI engines leave
// Group empty; the GPT-SoVITS preset catalogue fills it so the settings
// page can group the 18 role voices by style.
type Voice struct {
	VoiceID     string `json:"voice_id"`
	DisplayName string `json:"display_name"`
	Gender      string `json:"gender"`
	Lang        string `json:"lang"`
	Group       string `json:"group,omitempty"`
}

// SynthesizeInput carries one cleaned subtitle segment (<=500 chars)
// plus the companion settings that steer the selected engine.
type SynthesizeInput struct {
	Text    string
	VoiceID string
	Rate    int // SAPI rate in [-10,10]; other engines map it internally
	Volume  int // SAPI volume in [0,100]

	// Engine selects the router target ("" keeps the legacy SAPI path so
	// old payloads stay valid).
	Engine string
	// Reference-timbre engine parameters: a local GPT-SoVITS api_v2
	// compatible endpoint plus one reference audio file.
	RefEndpoint   string
	RefWavPath    string
	RefPromptText string
	// Style is an optional Edge neural speaking style (chat, affectionate…).
	Style string
	// VolcAPIKey / VolcBaseURL are filled by the bridge from the stored
	// volc_speech provider lease. The renderer never sends them.
	VolcAPIKey  string
	VolcBaseURL string
	// TryStreaming asks GPT-SoVITS for streaming_mode=true. Banned after a
	// non-RIFF or failed probe so the next segment falls back immediately.
	TryStreaming bool
}

// Engine selector values carried by tts.synthesize payloads.
const (
	EngineSapi    = "sapi"    // offline Windows SAPI desktop voices
	EngineNatural = "natural" // local OneCore natural neural voices (default)
	EngineEdge    = "edge"    // free Microsoft cloud neural voices (晓晓等)
	EngineRef     = "ref"     // zero-shot reference-timbre cloning via local service
	EngineVolc    = "volc"    // Ark Agent Plan seed-tts (openspeech)
)

// ValidEngine reports whether the payload engine field is accepted.
func ValidEngine(engine string) bool {
	switch engine {
	case "", EngineSapi, EngineNatural, EngineEdge, EngineRef, EngineVolc:
		return true
	}
	return false
}

// SynthesizeResult is the payload body for tts.synthesize.
type SynthesizeResult struct {
	WavBase64    string  `json:"wav_base64"`
	DurationHint float64 `json:"duration_hint"`
}

// Error family mapped by the bridge handlers onto the M95 code matrix.
var (
	// ErrEngineUnavailable maps to M95-001 (503): no usable SAPI engine
	// or voice on this machine (Windows N, stripped install, broken COM).
	ErrEngineUnavailable = errors.New("tts engine unavailable")
	// ErrSynthesisFailed maps to M95-002 (500): one segment failed to
	// synthesize; the caller skips the segment and keeps the subtitle.
	ErrSynthesisFailed = errors.New("tts synthesis failed")
)

// Engine is the platform boundary implemented by sapi_windows.go
// (real SAPI) and fallback_other.go (M95-001 stub off Windows).
type Engine interface {
	// Voices enumerates the installed SAPI voices.
	Voices() ([]Voice, error)
	// Synthesize renders one segment to a 16kHz mono 16-bit WAV.
	// The second return marks that the requested voice was missing and
	// the default voice was used instead (M95-004 notice semantics).
	Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error)
}

// ChunkStreamer emits audio as soon as the engine has a playable slice.
// Synthesize stays the whole-clip path for SAPI / ref / cache hits.
type ChunkStreamer interface {
	SynthesizeStream(ctx context.Context, in SynthesizeInput, emit func([]byte) error) (SynthesizeResult, bool, error)
}

// MaxSegmentChars is the per-segment character cap enforced by the
// technical design (segmentation happens in the renderer).
const MaxSegmentChars = 500

// DefaultRate and DefaultVolume mirror the frozen companion defaults.
const (
	DefaultRate   = 0
	DefaultVolume = 80
)
