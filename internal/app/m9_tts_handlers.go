// M9.5 Moon Companion bridge handlers: tts.voices / tts.synthesize /
// tts.cancel. Errors follow the frozen M95 code matrix — M95-001
// (engine unavailable, 503 semantics) and M95-002 (segment synthesis
// failed, 500 semantics) are failures; M95-003 (cancel notice) and
// M95-004 (voice fallback notice) travel as 200-level payload notices.
package app

import (
	"context"
	"errors"
	"log"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/tts"
)

func handleTtsVoices(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tts.voices 参数无效", false)
	}
	if e.m9tts == nil {
		return bridge.Failure(r.ID, r.TraceID, "M95-001", "本机无可用语音合成引擎", true)
	}
	voices, err := e.m9tts.Voices()
	if err != nil {
		return ttsFailure(r, err)
	}
	if len(voices) == 0 {
		return bridge.Failure(r.ID, r.TraceID, "M95-001", "本机无可用语音合成音色", true)
	}
	return bridge.Success(r.ID, map[string]any{"voices": voices})
}

func handleTtsSynthesize(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Text    string `json:"text"`
		VoiceID string `json:"voiceId"`
		Rate    *int   `json:"rate"`
		Volume  *int   `json:"volume"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tts.synthesize 参数无效", false)
	}
	if p.Text == "" || utf8.RuneCountInString(p.Text) > tts.MaxSegmentChars {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "text 必须为 1-500 字符", false)
	}
	if e.m9tts == nil {
		return bridge.Failure(r.ID, r.TraceID, "M95-001", "本机无可用语音合成引擎", true)
	}
	rate, volume := tts.DefaultRate, tts.DefaultVolume
	if p.Rate != nil {
		rate = *p.Rate
	}
	if p.Volume != nil {
		volume = *p.Volume
	}
	out, err := e.m9tts.Synthesize(tts.SynthesizeInput{
		Text: p.Text, VoiceID: p.VoiceID, Rate: rate, Volume: volume,
	})
	if err != nil {
		return ttsFailure(r, err)
	}
	if out.Discarded {
		// The segment raced a cancel: the renderer is already muted, so
		// answer quietly instead of surfacing an error (M95-003 note).
		return bridge.Success(r.ID, map[string]any{
			"wav_base64": "", "duration_hint": 0, "discarded": true, "notice": "TTS_CANCELLED",
		})
	}
	payload := map[string]any{
		"wav_base64":    out.Result.WavBase64,
		"duration_hint": out.Result.DurationHint,
	}
	if out.VoiceFallback {
		payload["notice"] = "TTS_VOICE_NOT_FOUND" // M95-004 (200 notice)
	}
	return bridge.Success(r.ID, payload)
}

func handleTtsCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tts.cancel 参数无效", false)
	}
	if e.m9tts == nil {
		return bridge.Failure(r.ID, r.TraceID, "M95-001", "本机无可用语音合成引擎", true)
	}
	e.m9tts.Cancel() // idempotent: a no-op when idle
	return bridge.Success(r.ID, map[string]any{"notice": "TTS_CANCELLED"})
}

// ttsFailure maps the tts error family onto the M95 code matrix while
// keeping the underlying cause in the engine log only.
func ttsFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, tts.ErrEngineUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "M95-001", "本机无可用语音合成引擎", true)
	case errors.Is(err, tts.ErrSynthesisFailed):
		return bridge.Failure(r.ID, r.TraceID, "M95-002", "该段语音合成失败", false)
	default:
		log.Printf("tts bridge failure: %v", err)
		return bridge.Failure(r.ID, r.TraceID, "M95-002", "该段语音合成失败", false)
	}
}
