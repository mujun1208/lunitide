package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/videounderstand"
)

// TranscribeVideoAudio uses only the already installed local offline model.
// No provider key, model installation or separate recognizer is introduced.
func (e *Engine) TranscribeVideoAudio(ctx context.Context, pcm []byte) (string, error) {
	if e.voice == nil || e.voice.refiner == nil {
		return "", videounderstand.ErrLocalASRMissing
	}
	refiner := e.voice.refiner
	if err := refiner.Ready(ctx); err != nil {
		return "", errors.Join(videounderstand.ErrLocalASRMissing, err)
	}
	if err := refiner.Warm(ctx); err != nil {
		return "", err
	}
	return refiner.Transcribe(ctx, pcm)
}
