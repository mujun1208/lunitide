// Package tts implements the M9.5 Moon Companion local text-to-speech
// runtime: offline Windows SAPI synthesis with a single-flight queue,
// session-scoped cancellation, segment de-duplication and a graceful
// non-Windows fallback (M95-001 semantics, subtitle-only degradation).
package tts

import "errors"

// Voice is one SAPI speech token exposed via tts.voices.
type Voice struct {
	VoiceID     string `json:"voice_id"`
	DisplayName string `json:"display_name"`
	Gender      string `json:"gender"`
	Lang        string `json:"lang"`
}

// SynthesizeInput carries one cleaned subtitle segment (<=500 chars)
// plus the companion settings that steer SAPI (voice/rate/volume).
type SynthesizeInput struct {
	Text    string
	VoiceID string
	Rate    int // SAPI rate in [-10,10]
	Volume  int // SAPI volume in [0,100]
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

// MaxSegmentChars is the per-segment character cap enforced by the
// technical design (segmentation happens in the renderer).
const MaxSegmentChars = 500

// DefaultRate and DefaultVolume mirror the frozen companion defaults.
const (
	DefaultRate   = 0
	DefaultVolume = 80
)
