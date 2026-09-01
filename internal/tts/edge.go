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

type edgeEngine struct {
	client    *http.Client
	voicesURL string
	synth     func(context.Context, SynthesizeInput) (SynthesizeResult, bool, error)

	mu        sync.Mutex
	voices    []Voice
	voicesAt  time.Time
	clockSkew time.Duration

	// Warm synthesis socket. A fresh TLS+WebSocket handshake costs
	// 150–300ms on every sentence, which lands as dead air before the
	// companion speaks; the connection is kept between turns instead.
	connMu sync.Mutex
	conn   *edgeConn
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
	edgeApplyStyleVoice(&in)
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
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		e.mu.Lock()
		skew := e.clockSkew
		e.mu.Unlock()
		gec := edgeSecMSGEC(time.Now().Add(skew))
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
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, edgeMaxBody))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			if d := resp.Header.Get("Date"); d != "" {
				if t, perr := http.ParseTime(d); perr == nil {
					e.adjustClockSkew(t)
					lastErr = fmt.Errorf("%w: 云端语音拒绝访问，请检查系统时间与网络", ErrEngineUnavailable)
					continue
				}
			}
			return nil, fmt.Errorf("%w: 云端语音拒绝访问，请检查系统时间与网络", ErrEngineUnavailable)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%w: 云端语音列表 HTTP %d", ErrEngineUnavailable, resp.StatusCode)
		}
		if readErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, readErr)
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
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%w: 云端语音拒绝访问，请检查系统时间与网络", ErrEngineUnavailable)
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
	api := make([]Voice, 0, len(rows))
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
		api = append(api, Voice{
			VoiceID:     row.ShortName,
			DisplayName: row.ShortName,
			Gender:      gender,
			Lang:        row.Locale,
		})
	}
	return expandEdgeMandarinVoices(api), nil
}

func edgeVoiceRank(v Voice) int {
	if preset, ok := edgePresetMeta(v.VoiceID); ok {
		return preset.Rank
	}
	if v.VoiceID == edgeDefaultVoice || strings.HasPrefix(v.VoiceID, edgeDefaultVoice+edgeStyleVoiceSep) {
		return 0
	}
	if v.Lang == "zh-CN" {
		return 30
	}
	return 100
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
	style := strings.TrimSpace(in.Style)
	if style == "" && edgeVoiceSupportsChatStyle(voice) {
		style = "chat"
	}
	expr := edgeExpressionFor(voice, style, in.Text)
	inner := `<prosody rate="` + signedPercent(ratePct) + `" pitch="` + expr.pitch + `" volume="` + strconv.Itoa(vol) + `">` +
		text.String() +
		`</prosody>`
	if expr.style != "" {
		inner = `<mstts:express-as style="` + xmlEscapeAttr(expr.style) + `" styledegree="` + expr.degree + `">` + inner + `</mstts:express-as>`
	}
	ns := `xmlns="http://www.w3.org/2001/10/synthesis"`
	if expr.style != "" {
		ns += ` xmlns:mstts="https://www.w3.org/2001/mstts"`
	}
	return `<speak version="1.0" ` + ns + ` xml:lang="` + lang + `">` +
		`<voice name="` + voice + `">` +
		inner +
		`</voice></speak>`
}

// edgeExpression is how one utterance is delivered: the persona style, how
// strongly it is applied, and the pitch. Reading a whole call with one
// fixed setting is what makes cloud TTS sound like a reader rather than
// someone talking, so each clip is tuned to its own sentence.
type edgeExpression struct {
	style  string
	degree string
	pitch  string
}

var (
	edgeCheerfulHints = []string{"太好了", "真棒", "恭喜", "哈哈", "开心", "好耶", "不错", "喜欢", "期待", "厉害", "当然"}
	edgeGentleHints   = []string{"别担心", "没关系", "慢慢来", "辛苦了", "早点休息", "好好休息", "注意身体", "抱歉", "对不起", "不好意思", "难过", "陪着你"}
)

func edgeExpressionFor(voice, style, text string) edgeExpression {
	expr := edgeExpression{style: style, degree: "1.5", pitch: "+6%"}
	switch {
	case edgeTextHasAny(text, edgeCheerfulHints) || strings.Contains(text, "！"):
		expr.degree, expr.pitch = "1.8", "+10%"
		if edgeStyleIsNeutral(style) && edgeVoiceSupportsStyle(voice, "cheerful") {
			expr.style = "cheerful"
		}
	case edgeTextHasAny(text, edgeGentleHints):
		expr.degree, expr.pitch = "1.2", "+2%"
		if edgeStyleIsNeutral(style) && edgeVoiceSupportsStyle(voice, "gentle") {
			expr.style = "gentle"
		}
	case strings.ContainsAny(text, "？?"):
		expr.degree, expr.pitch = "1.6", "+9%"
	}
	return expr
}

// edgeStyleIsNeutral marks the plain conversational personas — the only
// ones a sentence may move away from. An explicit 「轻柔耳语」/「新闻播报」
// pick belongs to the user and is never overridden.
func edgeStyleIsNeutral(style string) bool {
	return style == "" || style == "chat" || style == "assistant"
}

func edgeTextHasAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func edgeVoiceSupportsChatStyle(voice string) bool {
	switch voice {
	case "zh-CN-XiaoxiaoNeural", "zh-CN-XiaoyiNeural", "zh-CN-XiaohanNeural", "zh-CN-XiaoxuanNeural", "zh-CN-YunxiNeural":
		return true
	default:
		return false
	}
}

func xmlEscapeAttr(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
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
	h.Set("Accept-Encoding", "gzip, deflate, br")
	h.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	h.Set("Sec-CH-UA", `"Chromium";v="`+major+`", "Microsoft Edge";v="`+major+`", "Not;A Brand";v="99"`)
	h.Set("Sec-CH-UA-Mobile", "?0")
}
