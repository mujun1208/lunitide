// edge.go implements the "edge" Moon Companion engine: free natural
// neural voices (晓晓/云希/… ) through the same read-aloud service the
// Edge browser uses. The wire protocol is the documented edge-tts
// handshake: a DRM token (Sec-MS-GEC) over the trusted client token,
// one speech.config frame, one SSML frame, then binary Path:audio
// frames until Path:turn.end. Output is requested as
// riff-24khz-16bit-mono-pcm so the WAV flows through the existing
// blob/base64 playback pipeline unchanged.
package tts

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

const (
	edgeTrustedToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"
	edgeGECVersion   = "1-130.0.2849.68"
	edgeOrigin       = "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"
	edgeUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.2849.68"
	edgeOutputFormat = "riff-24khz-16bit-mono-pcm"
	edgeBytesPerSec  = 24000 * 2
	edgeDefaultVoice = "zh-CN-XiaoxiaoNeural"
	edgeTimeout      = 20 * time.Second
	edgeMaxAudio     = 8 << 20 // 8 MiB: ~87s of 24kHz 16-bit mono
)

// edgeVoices is the curated catalogue served by tts.voices(engine=edge).
var edgeVoices = []Voice{
	{VoiceID: "zh-CN-XiaoxiaoNeural", DisplayName: "晓晓 · 温柔女声（自然）", Gender: "female", Lang: "zh-CN"},
	{VoiceID: "zh-CN-XiaoyiNeural", DisplayName: "晓伊 · 活泼女声", Gender: "female", Lang: "zh-CN"},
	{VoiceID: "zh-CN-XiaohanNeural", DisplayName: "晓涵 · 沉静女声", Gender: "female", Lang: "zh-CN"},
	{VoiceID: "zh-CN-XiaomengNeural", DisplayName: "晓梦 · 甜美女声", Gender: "female", Lang: "zh-CN"},
	{VoiceID: "zh-CN-YunxiNeural", DisplayName: "云希 · 阳光男声", Gender: "male", Lang: "zh-CN"},
	{VoiceID: "zh-CN-YunjianNeural", DisplayName: "云健 · 沉稳男声", Gender: "male", Lang: "zh-CN"},
	{VoiceID: "zh-CN-YunyangNeural", DisplayName: "云扬 · 新闻男声", Gender: "male", Lang: "zh-CN"},
	{VoiceID: "zh-CN-YunxiaNeural", DisplayName: "云夏 · 少年男声", Gender: "male", Lang: "zh-CN"},
	{VoiceID: "zh-CN-liaoning-XiaobeiNeural", DisplayName: "晓北 · 东北话女声", Gender: "female", Lang: "zh-CN-liaoning"},
	{VoiceID: "zh-CN-shaanxi-XiaoniNeural", DisplayName: "晓妮 · 陕西话女声", Gender: "female", Lang: "zh-CN-shaanxi"},
	{VoiceID: "zh-HK-HiuMaanNeural", DisplayName: "曉曼 · 粤语女声", Gender: "female", Lang: "zh-HK"},
	{VoiceID: "zh-TW-HsiaoChenNeural", DisplayName: "曉臻 · 台湾女声", Gender: "female", Lang: "zh-TW"},
	{VoiceID: "en-US-AriaNeural", DisplayName: "Aria · English F", Gender: "female", Lang: "en-US"},
	{VoiceID: "en-US-GuyNeural", DisplayName: "Guy · English M", Gender: "male", Lang: "en-US"},
	{VoiceID: "en-US-JennyNeural", DisplayName: "Jenny · English F", Gender: "female", Lang: "en-US"},
	{VoiceID: "ja-JP-NanamiNeural", DisplayName: "七海 · 日本語 F", Gender: "female", Lang: "ja-JP"},
}

// EdgeVoices returns the natural-voice catalogue (copy for the bridge).
func EdgeVoices() []Voice {
	out := make([]Voice, len(edgeVoices))
	copy(out, edgeVoices)
	return out
}

// edgeEngine talks to the read-aloud service; stateless per call.
type edgeEngine struct{}

func (edgeEngine) Voices() ([]Voice, error) { return EdgeVoices(), nil }

func (edgeEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	voice := in.VoiceID
	if !edgeVoiceKnown(voice) {
		voice = edgeDefaultVoice
	}
	wav, err := edgeSynthesize(in.Text, voice, in.Rate, in.Volume)
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: edge 语音服务不可达（%v）", ErrSynthesisFailed, err)
	}
	return SynthesizeResult{
		WavBase64:    base64.StdEncoding.EncodeToString(wav),
		DurationHint: float64(len(wav)) / float64(edgeBytesPerSec),
	}, false, nil
}

func edgeVoiceKnown(voiceID string) bool {
	for _, v := range edgeVoices {
		if v.VoiceID == voiceID {
			return true
		}
	}
	return false
}

// edgeGEC derives the Sec-MS-GEC DRM token: SHA-256 over the trusted
// client token plus the current Windows epoch tick count floored to a
// five-minute boundary.
func edgeGEC(now time.Time) string {
	ticks := uint64(now.Unix()+11644473600) * 10_000_000
	ticks -= ticks % 3_000_000_000
	sum := sha256.Sum256([]byte(edgeTrustedToken + strconv.FormatUint(ticks, 10)))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// edgeTimestamp renders the X-Timestamp header the service expects.
func edgeTimestamp(now time.Time) string {
	return now.UTC().Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)")
}

func edgeSSML(text, voice string, rate, volume int) string {
	pct := strconv.Itoa(rate * 10)
	if pct != "0" && rate > 0 {
		pct = "+" + pct
	}
	return "<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='zh-CN' " +
		"xmlns:mstts='https://www.w3.org/2001/mstts'><voice name='" + voice + "'><prosody rate='" +
		pct + "%' volume='" + strconv.Itoa(volume) + "%'>" + edgeEscapeXML(text) +
		"</prosody></voice></speak>"
}

func edgeEscapeXML(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(text)
}

func edgeSynthesize(text, voice string, rate, volume int) ([]byte, error) {
	wsURL := "wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1" +
		"?TrustedClientToken=" + edgeTrustedToken +
		"&Sec-MS-GEC=" + edgeGEC(time.Now()) +
		"&Sec-MS-GEC-Version=" + edgeGECVersion
	cfg, err := websocket.NewConfig(wsURL, edgeOrigin)
	if err != nil {
		return nil, err
	}
	cfg.Header = http.Header{
		"User-Agent":          []string{edgeUserAgent},
		"Sec-MS-GEC":          []string{edgeGEC(time.Now())},
		"Sec-MS-GEC-Version":  []string{edgeGECVersion},
		"Origin":              []string{edgeOrigin},
		"Accept-Encoding":     []string{"gzip, deflate, br"},
		"Accept-Language":     []string{"zh-CN,zh;q=0.9,en;q=0.8"},
	}
	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		return nil, err
	}
	defer ws.Close()

	now := time.Now()
	requestID := edgeGEC(now)[:32] // 32 hex chars, no dashes — any entropy works
	speechConfig := "X-Timestamp:" + edgeTimestamp(now) +
		"\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n" +
		`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"true"},"outputFormat":"` + edgeOutputFormat + `"}}}}`
	if err := websocket.Message.Send(ws, speechConfig); err != nil {
		return nil, err
	}
	ssmlFrame := "X-RequestId:" + requestID +
		"\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:" + edgeTimestamp(now) + "Z\r\nPath:ssml\r\n\r\n" +
		edgeSSML(text, voice, rate, volume)
	if err := websocket.Message.Send(ws, ssmlFrame); err != nil {
		return nil, err
	}

	type outcome struct {
		wav []byte
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		var audio []byte
		for {
			var payload interface{}
			if err := websocket.Message.Receive(ws, &payload); err != nil {
				done <- outcome{nil, err}
				return
			}
			switch data := payload.(type) {
			case []byte:
				// Binary frames: 2-byte big-endian header length, header
				// text, then the RIFF payload.
				if len(data) < 2 {
					continue
				}
				headerLen := int(data[0])<<8 | int(data[1])
				if len(data) < 2+headerLen {
					continue
				}
				if strings.Contains(string(data[2:2+headerLen]), "Path:audio") {
					audio = append(audio, data[2+headerLen:]...)
					if len(audio) > edgeMaxAudio {
						done <- outcome{nil, fmt.Errorf("音频超出大小上限")}
						return
					}
				}
			case string:
				if strings.Contains(data, "Path:turn.end") {
					done <- outcome{audio, nil}
					return
				}
			}
		}
	}()
	select {
	case out := <-done:
		if out.err != nil {
			return nil, out.err
		}
		if len(out.wav) < 44 { // a valid riff payload always beats 44 bytes
			return nil, fmt.Errorf("服务未返回音频（音色或网络异常）")
		}
		return out.wav, nil
	case <-time.After(edgeTimeout):
		return nil, fmt.Errorf("合成超时")
	}
}
