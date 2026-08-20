// edge.go is the free Microsoft Edge cloud neural TTS engine (the
// service Edge Read Aloud uses): no API key, HTTPS + WebSocket to
// speech.platform.bing.com. This is what EngineEdge was documented as
// in cmd/engine; it used to alias local OneCore instead.
package tts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	edgeTrustedToken   = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"
	edgeChromiumFull   = "143.0.3650.75"
	edgeDefaultVoice   = "zh-CN-XiaoxiaoNeural"
	edgeVoiceListURL   = "https://speech.platform.bing.com/consumer/speech/synthesize/readaloud/voices/list"
	edgeWinEpochSec    = 11644473600
	edgeGECPeriodTicks = 3_000_000_000 // 300s in 100ns ticks
	edgeMaxBody        = 8 << 20
)

// EdgeDefaultVoiceID is the first-listen Chinese neural voice.
func EdgeDefaultVoiceID() string { return edgeDefaultVoice }

func edgeVoiceToLocal(voiceID string) string {
	_ = voiceID
	return ""
}

type edgeEngine struct {
	client    *http.Client
	voicesURL string
	synth     func(context.Context, SynthesizeInput) (SynthesizeResult, bool, error)

	mu        sync.Mutex
	voices    []Voice
	voicesAt  time.Time
	clockSkew time.Duration
}

// NewEdgeEngine talks to Microsoft's free Read Aloud endpoint.
func NewEdgeEngine() Engine {
	e := &edgeEngine{
		client:    &http.Client{Timeout: 25 * time.Second},
		voicesURL: edgeVoiceListURL,
	}
	e.synth = e.synthesizeWS
	return e
}

func (e *edgeEngine) Voices() ([]Voice, error) {
	e.mu.Lock()
	if len(e.voices) > 0 && time.Since(e.voicesAt) < time.Hour {
		out := append([]Voice(nil), e.voices...)
		e.mu.Unlock()
		return out, nil
	}
	e.mu.Unlock()
	voices, err := e.fetchVoices(context.Background())
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.voices, e.voicesAt = voices, time.Now()
	e.mu.Unlock()
	return append([]Voice(nil), voices...), nil
}

func (e *edgeEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	voice := strings.TrimSpace(in.VoiceID)
	if voice == "" || strings.HasPrefix(voice, "HKEY_") || strings.HasPrefix(voice, "refpack:") {
		voice = edgeDefaultVoice
	}
	in.VoiceID = voice
	if e.synth == nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音引擎未装配", ErrEngineUnavailable)
	}
	res, _, err := e.synth(context.Background(), in)
	if err != nil {
		return SynthesizeResult{}, false, err
	}
	fallback := strings.HasPrefix(in.VoiceID, "HKEY_")
	return res, fallback, nil
}

func (e *edgeEngine) fetchVoices(ctx context.Context) ([]Voice, error) {
	if e.client == nil {
		return nil, fmt.Errorf("%w: 云端语音客户端未装配", ErrEngineUnavailable)
	}
	gec := edgeSecMSGEC(time.Now().Add(e.clockSkew))
	url := e.voicesURL + "?trustedclienttoken=" + edgeTrustedToken +
		"&Sec-MS-GEC=" + gec + "&Sec-MS-GEC-Version=" + edgeSecMSGECVersion()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	edgeSetHeaders(req.Header)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法连接微软云端语音（需联网）: %v", ErrEngineUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		if d := resp.Header.Get("Date"); d != "" {
			if t, perr := http.ParseTime(d); perr == nil {
				e.mu.Lock()
				e.clockSkew = t.Sub(time.Now())
				e.mu.Unlock()
			}
		}
		return nil, fmt.Errorf("%w: 云端语音拒绝访问，请检查系统时间与网络", ErrEngineUnavailable)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: 云端语音列表 HTTP %d", ErrEngineUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, edgeMaxBody))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	voices, err := parseEdgeVoices(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	if len(voices) == 0 {
		return nil, fmt.Errorf("%w: 云端未返回可用音色", ErrEngineUnavailable)
	}
	return voices, nil
}

type edgeVoiceRow struct {
	ShortName    string `json:"ShortName"`
	FriendlyName string `json:"FriendlyName"`
	Gender       string `json:"Gender"`
	Locale       string `json:"Locale"`
}

func parseEdgeVoices(raw []byte) ([]Voice, error) {
	var rows []edgeVoiceRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	out := make([]Voice, 0, len(rows))
	for _, row := range rows {
		if row.ShortName == "" || !strings.Contains(row.ShortName, "Neural") {
			continue
		}
		gender := "neutral"
		switch strings.ToLower(row.Gender) {
		case "female":
			gender = "female"
		case "male":
			gender = "male"
		}
		name := row.FriendlyName
		if rest, ok := strings.CutPrefix(name, "Microsoft "); ok {
			if i := strings.Index(rest, " Online"); i > 0 {
				name = rest[:i]
			}
		}
		if name == "" {
			name = row.ShortName
		}
		out = append(out, Voice{
			VoiceID:     row.ShortName,
			DisplayName: name,
			Gender:      gender,
			Lang:        row.Locale,
			Group:       edgeVoiceGroup(row.Locale, gender),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return edgeVoiceRank(out[i]) < edgeVoiceRank(out[j])
	})
	return out, nil
}

func edgeVoiceGroup(locale, gender string) string {
	if locale == "zh-CN" {
		if gender == "male" {
			return "云端中文 · 男声"
		}
		return "云端中文 · 女声"
	}
	if strings.HasPrefix(locale, "zh-") {
		return "云端中文 · 方言"
	}
	return "云端外语"
}

func edgeVoiceRank(v Voice) int {
	if v.VoiceID == edgeDefaultVoice {
		return 0
	}
	if v.Lang == "zh-CN" {
		return 1
	}
	if strings.HasPrefix(v.Lang, "zh-") {
		return 2
	}
	return 3
}

func edgeSSML(in SynthesizeInput) string {
	voice := in.VoiceID
	if voice == "" {
		voice = edgeDefaultVoice
	}
	var text bytes.Buffer
	_ = xml.EscapeText(&text, []byte(edgeSanitizeText(in.Text)))
	ratePct := in.Rate * 10
	if ratePct < -50 {
		ratePct = -50
	}
	if ratePct > 100 {
		ratePct = 100
	}
	vol := in.Volume
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	lang := "zh-CN"
	if i := strings.Index(voice, "-"); i > 0 {
		if j := strings.Index(voice[i+1:], "-"); j > 0 {
			lang = voice[:i+1+j]
		}
	}
	return `<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="` + lang + `">` +
		`<voice name="` + voice + `">` +
		`<prosody rate="` + signedPercent(ratePct) + `" volume="` + strconv.Itoa(vol) + `">` +
		text.String() +
		`</prosody></voice></speak>`
}

func signedPercent(n int) string {
	if n >= 0 {
		return "+" + strconv.Itoa(n) + "%"
	}
	return strconv.Itoa(n) + "%"
}

// edgeSanitizeText strips control characters the Edge TTS service rejects.
func edgeSanitizeText(text string) string {
	if text == "" {
		return text
	}
	runes := []rune(text)
	for i, r := range runes {
		if (r >= 0 && r <= 8) || (r >= 11 && r <= 12) || (r >= 14 && r <= 31) {
			runes[i] = ' '
		}
	}
	return string(runes)
}

// edgeSecMSGEC is the time-based token Edge Read Aloud requires
// (SHA-256 of Windows-filetime ticks snapped to 300s + the public client token).
func edgeSecMSGEC(now time.Time) string {
	ticks := (now.UTC().Unix() + edgeWinEpochSec) * 10_000_000
	ticks -= ticks % edgeGECPeriodTicks
	sum := sha256.Sum256([]byte(strconv.FormatInt(ticks, 10) + edgeTrustedToken))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func edgeSecMSGECVersion() string { return "1-" + edgeChromiumFull }

func edgeSetHeaders(h http.Header) {
	major := strings.SplitN(edgeChromiumFull, ".", 2)[0]
	h.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/"+major+".0.0.0 Safari/537.36 Edg/"+major+".0.0.0")
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
}
