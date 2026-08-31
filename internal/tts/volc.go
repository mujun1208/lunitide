package tts

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/voice/volcsauc"
)

const (
	volcResourceTTS20 = "seed-tts-2.0"
	volcResourceTTS10 = "seed-tts-1.0"
	volcDefaultHost   = "openspeech.bytedance.com"
	volcPlanTTSPath   = "/api/v3/plan/tts/unidirectional"
	volcMaxBody       = 8 << 20
	volcDoneCode      = 20000000
)

type volcEngine struct {
	client *http.Client
	url    string
}

// NewVolcEngine talks to Agent Plan HTTP unidirectional TTS.
func NewVolcEngine() Engine {
	return &volcEngine{
		client: &http.Client{Timeout: 45 * time.Second},
		url:    "https://" + volcDefaultHost + volcPlanTTSPath,
	}
}

func (e *volcEngine) Voices() ([]Voice, error) {
	return VolcVoices(), nil
}

func (e *volcEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	if strings.TrimSpace(in.VolcAPIKey) == "" {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 未配火山语音密钥", ErrEngineUnavailable)
	}
	speaker, fallback := volcResolveSpeaker(in.VoiceID)
	audio, err := e.postUnidirectional(context.Background(), in, speaker)
	if err != nil {
		return SynthesizeResult{}, fallback, err
	}
	if len(audio) == 0 {
		return SynthesizeResult{}, fallback, fmt.Errorf("%w: 火山未返回音频", ErrSynthesisFailed)
	}
	return SynthesizeResult{
		WavBase64:    base64.StdEncoding.EncodeToString(audio),
		DurationHint: float64(len(audio)) / 4000,
	}, fallback, nil
}

func (e *volcEngine) postUnidirectional(ctx context.Context, in SynthesizeInput, speaker string) ([]byte, error) {
	if e.client == nil {
		return nil, fmt.Errorf("%w: 火山语音客户端未装配", ErrEngineUnavailable)
	}
	url := strings.TrimSpace(e.url)
	if url == "" {
		url = volcTTSURL(in.VolcBaseURL)
	}
	body, err := json.Marshal(volcHTTPBody(in, speaker))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Resource-Id", volcResourceID(speaker))
	req.Header.Set("X-Api-Connect-Id", volcConnectID())
	req.Header.Set("X-Control-Require-Usage-Tokens-Return", "*")
	app, token := volcsauc.ParseCredential(in.VolcAPIKey)
	if app != "" {
		req.Header.Set("X-Api-App-Key", app)
		req.Header.Set("X-Api-Access-Key", token)
	} else {
		req.Header.Set("X-Api-Key", token)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法连接火山语音（需联网）: %v", ErrEngineUnavailable, err)
	}
	defer resp.Body.Close()
	logid := resp.Header.Get("X-Tt-Logid")
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, volcMaxBody))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: 火山语音鉴权失败（请用 Agent Plan 专属 API Key）logid=%s", ErrEngineUnavailable, logid)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: 火山语音 HTTP %d logid=%s", ErrSynthesisFailed, resp.StatusCode, logid)
	}
	if readErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrSynthesisFailed, readErr)
	}
	audio, parseErr := parseVolcNDJSON(raw)
	if parseErr != nil {
		return nil, fmt.Errorf("%w: %v logid=%s", ErrSynthesisFailed, parseErr, logid)
	}
	return audio, nil
}

func volcHTTPBody(in SynthesizeInput, speaker string) map[string]any {
	return map[string]any{
		"req_params": map[string]any{
			"text":    in.Text,
			"speaker": speaker,
			"audio_params": map[string]any{
				"format":        "mp3",
				"sample_rate":   24000,
				"speech_rate":   clampInt(in.Rate*10, -50, 100),
				"loudness_rate": clampInt(in.Volume-80, -50, 100),
			},
		},
	}
}

func foldTTSResourceToken(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.TrimPrefix(s, "doubao-")
	return s
}

// IsVolcTTSResourceID reports an Agent Plan X-Api-Resource-Id, including
// official aliases (seedtts-2.0, doubao-seed-tts-2.0). Speakers are not this.
func IsVolcTTSResourceID(id string) bool {
	s := foldTTSResourceToken(id)
	return s == "seed-tts" || s == "seedtts" || strings.HasPrefix(s, "seed-tts-") || strings.HasPrefix(s, "seedtts-")
}

// CanonicalTTSResourceID maps official aliases onto seed-tts-2.0 / seed-tts-1.0.
func CanonicalTTSResourceID(id string) string {
	s := foldTTSResourceToken(id)
	if strings.HasPrefix(s, "seed-tts-1") || strings.HasPrefix(s, "seedtts-1") {
		return volcResourceTTS10
	}
	if IsVolcTTSResourceID(id) {
		return volcResourceTTS20
	}
	return strings.TrimSpace(id)
}

func volcResourceID(speaker string) string {
	if IsVolcTTSResourceID(speaker) {
		return CanonicalTTSResourceID(speaker)
	}
	if strings.Contains(speaker, "_uranus_") || strings.HasPrefix(speaker, "saturn_") {
		return volcResourceTTS20
	}
	return volcResourceTTS10
}

func volcTTSURL(baseURL string) string {
	host := volcDefaultHost
	h := volcHostOf(baseURL)
	if h != "" && !strings.Contains(h, "volces.com") {
		host = h
	}
	return "https://" + host + volcPlanTTSPath
}

func volcHostOf(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "wss://")
	s = strings.TrimPrefix(s, "ws://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

func volcConnectID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

type volcLine struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
	Data    string `json:"data"`
}

func parseVolcNDJSON(raw []byte) ([]byte, error) {
	audio := make([]byte, 0, 4096)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), volcMaxBody)
	sawLine := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		sawLine = true
		var row volcLine
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("响应不是 JSON: %v", err)
		}
		if row.Code == volcDoneCode {
			break
		}
		if row.Code > 0 {
			msg := row.Message
			if msg == "" {
				msg = row.Msg
			}
			if msg == "" {
				msg = fmt.Sprintf("code %d", row.Code)
			}
			return nil, fmt.Errorf("%s", msg)
		}
		if row.Data == "" {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(row.Data)
		if err != nil {
			return nil, fmt.Errorf("音频不是 base64: %v", err)
		}
		audio = append(audio, chunk...)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sawLine && len(bytes.TrimSpace(raw)) > 0 {
		var row volcLine
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, fmt.Errorf("响应不是 JSON")
		}
		if row.Code > 0 && row.Code != volcDoneCode {
			msg := row.Message
			if msg == "" {
				msg = row.Msg
			}
			return nil, fmt.Errorf("%s", msg)
		}
		if row.Data != "" {
			chunk, err := base64.StdEncoding.DecodeString(row.Data)
			if err != nil {
				return nil, err
			}
			audio = append(audio, chunk...)
		}
	}
	return audio, nil
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
