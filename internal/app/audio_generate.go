package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/tts"
	"github.com/oklog/ulid/v2"
)

const audioSpeechNotice = "朗读合成，可在对话里直接点播放。这不是演唱成曲，产品没有作曲引擎。"

func (e *Engine) invokeAudioGenerate(ctx context.Context, sessionID string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		Prompt  string `json:"prompt"`
		Lyrics  string `json:"lyrics"`
		Path    string `json:"path"`
		VoiceID string `json:"voiceId"`
	}
	if json.Unmarshal(args, &a) != nil {
		return toolruntime.Result{}, fmt.Errorf("invalid audio.generate arguments")
	}
	text := strings.TrimSpace(a.Lyrics)
	if text == "" {
		text = strings.TrimSpace(a.Prompt)
	}
	if text == "" || utf8.RuneCountInString(text) > 4000 {
		return toolruntime.Result{}, fmt.Errorf("没有可朗读的文本")
	}
	if err := e.CheckCapability(ctx, "tts"); err != nil {
		return toolruntime.Result{}, fmt.Errorf("语音合成已禁用")
	}
	if e.m9tts == nil {
		return toolruntime.Result{}, fmt.Errorf("本机无可用语音合成引擎")
	}
	if e.tools == nil || sessionID == "" {
		return toolruntime.Result{}, fmt.Errorf("会话文件存储不可用")
	}
	segments := tts.SplitSpeechSegments(text, tts.MaxSegmentChars)
	if len(segments) == 0 {
		return toolruntime.Result{}, fmt.Errorf("没有可朗读的文本")
	}
	var wavs [][]byte
	var duration float64
	var fallback bool
	for _, seg := range segments {
		out, err := e.m9tts.Synthesize(tts.SynthesizeInput{
			Text:    seg,
			VoiceID: strings.TrimSpace(a.VoiceID),
			Rate:    tts.DefaultRate,
			Volume:  tts.DefaultVolume,
		})
		if err != nil {
			return toolruntime.Result{}, fmt.Errorf("语音合成失败: %w", err)
		}
		if out.Discarded {
			return toolruntime.Result{}, fmt.Errorf("语音合成已取消")
		}
		fallback = fallback || out.VoiceFallback
		raw, decErr := base64.StdEncoding.DecodeString(out.Result.WavBase64)
		if decErr != nil || len(raw) == 0 {
			return toolruntime.Result{}, fmt.Errorf("语音合成失败: 引擎未返回音频")
		}
		wavs = append(wavs, raw)
		duration += out.Result.DurationHint
	}
	data, ext, err := mergeSpeechClips(wavs)
	if err != nil {
		return toolruntime.Result{}, fmt.Errorf("语音合成失败: %w", err)
	}
	path := strings.TrimSpace(a.Path)
	if path == "" {
		path = "generated-" + ulid.Make().String() + ext
	} else if strings.ToLower(filepath.Ext(path)) != ext {
		path = strings.TrimSuffix(path, filepath.Ext(path)) + ext
	}
	written, saveErr := e.tools.SaveGeneratedAudio(sessionID, path, data)
	if saveErr != nil {
		return toolruntime.Result{}, fmt.Errorf("音频保存失败: %w", saveErr)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "kind=audio path=%s\nmode=tts-speech\ncaption=%s\nchars=%d segments=%d",
		written.Artifact.Path, audioSpeechNotice, utf8.RuneCountInString(text), len(segments))
	if duration > 0 {
		fmt.Fprintf(&b, " durationHint=%.1f", duration)
	}
	if fallback {
		b.WriteString("\nvoice=fallback")
	}
	written.Output = b.String()
	return written, nil
}

func mergeSpeechClips(clips [][]byte) ([]byte, string, error) {
	if len(clips) == 0 {
		return nil, "", fmt.Errorf("引擎未返回音频")
	}
	if toolruntimeLooksLikeWAV(clips[0]) {
		joined, err := tts.ConcatWAV(clips)
		if err != nil {
			return nil, "", err
		}
		return joined, ".wav", nil
	}
	if len(clips) == 1 && toolruntimeLooksLikeMP3(clips[0]) {
		return clips[0], ".mp3", nil
	}
	return nil, "", fmt.Errorf("引擎未返回可播放的 WAV")
}

func toolruntimeLooksLikeWAV(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

func toolruntimeLooksLikeMP3(data []byte) bool {
	if len(data) >= 3 && string(data[0:3]) == "ID3" {
		return true
	}
	return len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0
}
