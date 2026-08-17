// M9.5 Moon Companion bridge handlers: tts.voices / tts.synthesize /
// tts.cancel / tts.refAudios. Errors follow the frozen M95 code matrix —
// M95-001 (engine unavailable, 503 semantics) and M95-002 (segment
// synthesis failed, 500 semantics) are failures; M95-003 (cancel
// notice) and M95-004 (voice fallback notice) travel as 200-level
// payload notices.
package app

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/tts"
)

func handleTtsVoices(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Engine      string `json:"engine"`
		RefEndpoint string `json:"refEndpoint"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tts.voices 参数无效", false)
	}
	if !tts.ValidEngine(p.Engine) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "engine 必须为 sapi/natural/edge/ref", false)
	}
	if p.Engine == tts.EngineRef {
		return bridge.Success(r.ID, map[string]any{
			"voices":   tts.RefVoices(),
			"ref_meta": tts.RefPackMeta(p.RefEndpoint),
		})
	}
	// natural / legacy edge / sapi: one local catalogue — OneCore natural
	// voices first, classic desktop voices after.
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
		Text          string `json:"text"`
		VoiceID       string `json:"voiceId"`
		Rate          *int   `json:"rate"`
		Volume        *int   `json:"volume"`
		Engine        string `json:"engine"`
		RefEndpoint   string `json:"refEndpoint"`
		RefWavPath    string `json:"refWavPath"`
		RefPromptText string `json:"refPromptText"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tts.synthesize 参数无效", false)
	}
	if p.Text == "" || utf8.RuneCountInString(p.Text) > tts.MaxSegmentChars {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "text 必须为 1-500 字符", false)
	}
	if !tts.ValidEngine(p.Engine) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "engine 必须为 sapi/natural/edge/ref", false)
	}
	if p.Engine == tts.EngineRef {
		if p.VoiceID == "" {
			p.VoiceID = tts.RefDefaultVoiceID()
		}
		if p.RefEndpoint != "" && !strings.HasPrefix(p.RefEndpoint, "http://") && !strings.HasPrefix(p.RefEndpoint, "https://") {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "参考音色服务地址必须以 http(s):// 开头", false)
		}
		if !tts.IsRefPresetVoiceID(p.VoiceID) {
			// Custom reference audio keeps the strict payload checks;
			// preset refpack: voices resolve against the built-in pack.
			if strings.TrimSpace(p.RefWavPath) == "" {
				return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "必须选择参考音频文件", false)
			}
			if _, err := os.Stat(p.RefWavPath); err != nil {
				return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "参考音频文件不存在，请在设置中重新选择", false)
			}
		}
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
	input := tts.SynthesizeInput{
		Text: p.Text, VoiceID: p.VoiceID, Rate: rate, Volume: volume,
		Engine: p.Engine, RefEndpoint: p.RefEndpoint,
		RefWavPath: p.RefWavPath, RefPromptText: p.RefPromptText,
	}
	out, err := e.m9tts.Synthesize(input)
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

// handleTtsRefAudios browses a local directory for reference audio so
// the settings page can pick a timbre from collections such as
// E:\AI电影漫剧\800+音色合集. A missing directory is a 200 with
// exists=false (the picker shows it, not an error toast).
func handleTtsRefAudios(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Dir string `json:"dir"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Dir) == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "dir 不能为空", false)
	}
	clean, entries, err := tts.ListRefAudioEntries(p.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return bridge.Success(r.ID, map[string]any{"dir": clean, "exists": false, "entries": []tts.RefAudioEntry{}})
		}
		log.Printf("tts.refAudios failure: %v", err)
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "目录无法读取（请检查路径与权限）", false)
	}
	return bridge.Success(r.ID, map[string]any{"dir": clean, "exists": true, "entries": entries})
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
