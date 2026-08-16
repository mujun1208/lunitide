//go:build !windows

// fallback_other.go keeps the M9.5 TTS surface cross-compilable: off
// Windows there is no SAPI, so voices and synthesis degrade to the
// M95-001 subtitle-only mode while chat and speech input stay intact.
package tts

import "fmt"

// fallbackEngine answers M95-001 for every call.
type fallbackEngine struct{}

// NewPlatformEngine returns the non-Windows M95-001 stub.
func NewPlatformEngine() Engine { return fallbackEngine{} }

func (fallbackEngine) Voices() ([]Voice, error) {
	return nil, fmt.Errorf("%w: SAPI 仅存在于 Windows", ErrEngineUnavailable)
}

func (fallbackEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	var zero SynthesizeResult
	return zero, false, fmt.Errorf("%w: SAPI 仅存在于 Windows", ErrEngineUnavailable)
}
