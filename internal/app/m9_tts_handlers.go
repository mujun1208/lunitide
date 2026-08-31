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
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secretlease"
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
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "engine 必须为 sapi/natural/edge/ref/volc", false)
	}
	if p.Engine == tts.EngineVolc {
		return bridge.Success(r.ID, map[string]any{"voices": tts.VolcVoices()})
	}
	if p.Engine == tts.EngineRef {
		return bridge.Success(r.ID, map[string]any{
			"voices":   tts.RefVoices(),
			"ref_meta": tts.RefPackMeta(p.RefEndpoint),
		})
	}
	if e.m9tts == nil {
		return bridge.Failure(r.ID, r.TraceID, "M95-001", "本机无可用语音合成引擎", true)
	}
	voices, err := e.m9tts.VoicesFor(p.Engine)
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
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "engine 必须为 sapi/natural/edge/ref/volc", false)
	}
	if p.Engine == tts.EngineVolc && p.VoiceID == "" {
		p.VoiceID = tts.VolcDefaultVoiceID()
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
	if p.Engine == tts.EngineVolc {
		return synthesizeVolc(e, ctx, r, p.Text, p.VoiceID, p.Rate, p.Volume)
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
	return ttsSynthResponse(r, out, err)
}

func synthesizeVolc(e *Engine, ctx context.Context, r bridge.Request, text, voiceID string, rate, volume *int) bridge.Response {
	input := tts.SynthesizeInput{
		Text: text, VoiceID: voiceID, Rate: tts.DefaultRate, Volume: tts.DefaultVolume, Engine: tts.EngineVolc,
	}
	if rate != nil {
		input.Rate = *rate
	}
	if volume != nil {
		input.Volume = *volume
	}
	p, err := e.firstReadyVolcSpeech(ctx)
	if err != nil {
		return ttsFailure(r, err)
	}
	var out tts.SynthesizeResultOut
	err = e.withProviderLease(ctx, p, secretlease.OperationChat, func(_ context.Context, secret []byte) error {
		input.VolcAPIKey = string(secret)
		input.VolcBaseURL = p.BaseURL
		var synthErr error
		out, synthErr = e.m9tts.Synthesize(input)
		return synthErr
	})
	if err != nil {
		if errors.Is(err, tts.ErrEngineUnavailable) || errors.Is(err, tts.ErrSynthesisFailed) {
			return ttsFailure(r, err)
		}
		return ttsFailure(r, fmt.Errorf("%w: 火山语音密钥不可用", tts.ErrEngineUnavailable))
	}
	return ttsSynthResponse(r, out, nil)
}

func (e *Engine) firstReadyVolcSpeech(ctx context.Context) (provider.Provider, error) {
	if e.providers == nil {
		return provider.Provider{}, fmt.Errorf("%w: 未配火山语音密钥", tts.ErrEngineUnavailable)
	}
	items, err := e.providers.List(ctx, provider.Filter{Protocol: provider.ProtocolVolcSpeech})
	if err != nil {
		return provider.Provider{}, fmt.Errorf("%w: 未配火山语音密钥", tts.ErrEngineUnavailable)
	}
	for _, item := range items {
		if item.Status != provider.StatusEnabled {
			continue
		}
		if item.CredentialState != provider.CredentialConfigured || item.CredentialRef == "" {
			continue
		}
		return item, nil
	}
	return provider.Provider{}, fmt.Errorf("%w: 未配火山语音密钥", tts.ErrEngineUnavailable)
}

func ttsSynthResponse(r bridge.Request, out tts.SynthesizeResultOut, err error) bridge.Response {
	if err != nil {
		return ttsFailure(r, err)
	}
	if out.Discarded {
		return bridge.Success(r.ID, map[string]any{
			"wav_base64": "", "duration_hint": 0, "discarded": true, "notice": "TTS_CANCELLED",
		})
	}
	payload := map[string]any{
		"wav_base64":    out.Result.WavBase64,
		"duration_hint": out.Result.DurationHint,
	}
	if out.VoiceFallback {
		payload["notice"] = "TTS_VOICE_NOT_FOUND"
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
		msg := "本机无可用语音合成引擎"
		if strings.Contains(err.Error(), "火山") {
			msg = "火山朗读不可用。请在供应商「语音模型」里配置 Agent Plan 专属 API Key"
		} else if strings.Contains(err.Error(), "云端") || strings.Contains(err.Error(), "联网") {
			msg = "无法连接微软云端语音（需联网）"
		}
		return bridge.Failure(r.ID, r.TraceID, "M95-001", msg, true)
	case errors.Is(err, tts.ErrRefEngineStarting):
		// The hosted GPT-SoVITS service is loading (retryable M95-001
		// family): the player waits and retries instead of breaking.
		return bridge.Failure(r.ID, r.TraceID, "M95-001", "语音引擎启动中，请稍候", true)
	case errors.Is(err, tts.ErrSynthesisFailed):
		msg := "该段语音合成失败"
		if strings.Contains(err.Error(), "火山") || strings.Contains(err.Error(), "seed-tts") {
			msg = "火山语音合成失败（请核对 Agent Plan 专属 API Key 与音色）"
		} else if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "Forbidden") {
			msg = "云端语音被拒绝（请检查系统时间与网络，或改用「自然语音」本机引擎）"
		} else if strings.Contains(err.Error(), "云端") {
			msg = "云端语音合成失败（请检查网络，或改用「自然语音」本机引擎）"
		} else if idx := strings.Index(err.Error(), "HTTP "); idx >= 0 {
			msg = "该段语音合成失败（" + err.Error()[idx:] + "）"
		}
		return bridge.Failure(r.ID, r.TraceID, "M95-002", msg, false)
	default:
		log.Printf("tts bridge failure: %v", err)
		return bridge.Failure(r.ID, r.TraceID, "M95-002", "该段语音合成失败", false)
	}
}

// handleTtsEnsureRefEngine triggers the auto-host launch of the local
// GPT-SoVITS api_v2 service (non-blocking) and returns the live host
// state. The Moon Companion stage and the settings page call it on
// engine/ref selection so the model loads while the user reads.
func handleTtsEnsureRefEngine(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RefEndpoint string `json:"refEndpoint"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tts.ensureRefEngine 参数无效", false)
	}
	endpoint := p.RefEndpoint
	if endpoint == "" {
		endpoint = tts.DefaultRefEndpoint
	}
	state, script := tts.DefaultRefHost.Status(endpoint)
	if state != tts.RefHostOnline {
		// Fire-and-forget: the caller polls tts.voices / retries this
		// method for progress instead of blocking the bridge round-trip
		// through a 30-90s CPU model load.
		go func() {
			if err := tts.DefaultRefHost.EnsureRunning(endpoint, 120*time.Second); err != nil {
				log.Printf("tts.ensureRefEngine launch: %v", err)
			}
		}()
		state, _ = tts.DefaultRefHost.Status(endpoint)
	}
	return bridge.Success(r.ID, map[string]any{
		"state":       state,
		"host_script": script,
		"endpoint":    endpoint,
		"last_error":  tts.DefaultRefHost.LastErr(),
	})
}
