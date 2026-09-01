package tts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	edgeSynthHost    = "speech.platform.bing.com"
	edgeSynthHostAlt = "api.msedgeservices.com"
	edgeSynthPath    = "/consumer/speech/synthesize/readaloud/edge/v1"
	edgeSynthPathAlt = "/tts/cognitiveservices/websocket/v1"
	edgeAudioDelim   = "Path:audio\r\n"
	edgeOutputFmt    = "audio-24khz-48kbitrate-mono-mp3"
	edgeMaxFrame     = 4 << 20
	// A parked socket is reused only while Microsoft is still likely to
	// hold it open; past that a fresh dial is cheaper than a timeout.
	edgeConnMaxIdle = 20 * time.Second
	edgeConnMaxAge  = 200 * time.Second
	// How long a reused socket may stay silent before we give up on it.
	edgeWarmFirstFrameTimeout = 6 * time.Second
)

func edgeTimestamp(now time.Time) string {
	return now.UTC().Format("Mon Jan 2 2006 15:04:05") + " GMT+0000 (Coordinated Universal Time)"
}

func edgeMUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	const hexdigits = "0123456789ABCDEF"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2], out[i*2+1] = hexdigits[v>>4], hexdigits[v&0x0f]
	}
	return string(out)
}

func (e *edgeEngine) synthesizeWS(ctx context.Context, in SynthesizeInput) (SynthesizeResult, bool, error) {
	return e.synthesizeWSChunks(ctx, in, nil)
}

func (e *edgeEngine) SynthesizeStream(ctx context.Context, in SynthesizeInput, emit func([]byte) error) (SynthesizeResult, bool, error) {
	edgeApplyStyleVoice(&in)
	res, fb, err := e.synthesizeWSChunks(ctx, in, emit)
	if err != nil {
		return res, fb, err
	}
	if strings.HasPrefix(in.VoiceID, "HKEY_") {
		fb = true
	}
	return res, fb, nil
}

func (e *edgeEngine) synthesizeWSChunks(ctx context.Context, in SynthesizeInput, emit func([]byte) error) (SynthesizeResult, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	targets := []struct{ host, path string }{
		{edgeSynthHost, edgeSynthPath},
		{edgeSynthHostAlt, edgeSynthPathAlt},
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		e.mu.Lock()
		skew := e.clockSkew
		e.mu.Unlock()
		adjusted := false
		for _, target := range targets {
			res, fb, err := e.synthesizeWSHost(ctx, target.host, target.path, in, edgeSecMSGEC(time.Now().Add(skew)), emit)
			if err == nil {
				return res, fb, nil
			}
			lastErr = err
			var he *edgeHandshakeError
			if errors.As(err, &he) && he.status == http.StatusForbidden && he.hasDate {
				e.adjustClockSkew(he.date)
				adjusted = true
				break
			}
		}
		if !adjusted {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: 云端语音合成失败", ErrSynthesisFailed)
	}
	if res, fb, err := edgeSynthesizePython(ctx, in); err == nil {
		if emit != nil && res.WavBase64 != "" {
			if raw, decErr := base64.StdEncoding.DecodeString(res.WavBase64); decErr == nil && len(raw) > 0 {
				if emitErr := emit(raw); emitErr != nil {
					return SynthesizeResult{}, fb, emitErr
				}
			}
		}
		return res, fb, nil
	}
	return SynthesizeResult{}, false, lastErr
}

// edgeConn is a synthesis socket kept warm between turns.
type edgeConn struct {
	conn       *websocket.Conn
	host       string
	path       string
	createdAt  time.Time
	usedAt     time.Time
	configured bool
}

// takeWarmConn removes the pooled connection and returns it when it still
// matches the target and is young enough to trust. Caller holds connMu.
func (e *edgeEngine) takeWarmConn(host, path string) *edgeConn {
	c := e.conn
	if c == nil {
		return nil
	}
	e.conn = nil
	now := time.Now()
	if c.host != host || c.path != path ||
		now.Sub(c.usedAt) > edgeConnMaxIdle || now.Sub(c.createdAt) > edgeConnMaxAge {
		c.conn.Close()
		return nil
	}
	return c
}

// keepConn parks a healthy connection for the next turn. Caller holds connMu.
func (e *edgeEngine) keepConn(c *edgeConn) {
	c.usedAt = time.Now()
	if e.conn != nil && e.conn != c {
		e.conn.conn.Close()
	}
	e.conn = c
}

func (e *edgeEngine) synthesizeWSHost(ctx context.Context, host, path string, in SynthesizeInput, gec string, emit func([]byte) error) (SynthesizeResult, bool, error) {
	e.connMu.Lock()
	defer e.connMu.Unlock()

	// A parked socket that died quietly would otherwise block the read
	// until the 25s request deadline, so its first frame gets a short
	// leash and a failure just falls through to a fresh dial below.
	if warm := e.takeWarmConn(host, path); warm != nil {
		res, fb, err := edgeSynthesizeTurn(ctx, warm, in, edgeWarmFirstFrameTimeout, emit)
		if err == nil {
			e.keepConn(warm)
			return res, fb, nil
		}
		warm.conn.Close()
	}

	conn, err := dialEdgeWS(ctx, host, path, gec)
	if err != nil {
		return SynthesizeResult{}, false, err
	}
	fresh := &edgeConn{conn: conn, host: host, path: path, createdAt: time.Now()}
	res, fb, err := edgeSynthesizeTurn(ctx, fresh, in, 0, emit)
	if err != nil {
		conn.Close()
		return SynthesizeResult{}, false, err
	}
	e.keepConn(fresh)
	return res, fb, nil
}

// edgeSynthesizeTurn runs one request/response turn on an open socket.
// firstFrameTimeout caps the wait for the first reply (0 = request deadline).
func edgeSynthesizeTurn(ctx context.Context, c *edgeConn, in SynthesizeInput, firstFrameTimeout time.Duration, emit func([]byte) error) (SynthesizeResult, bool, error) {
	conn := c.conn
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		_ = conn.SetWriteDeadline(deadline)
		_ = conn.SetReadDeadline(deadline)
	}
	if firstFrameTimeout > 0 {
		first := time.Now().Add(firstFrameTimeout)
		if !hasDeadline || first.Before(deadline) {
			_ = conn.SetReadDeadline(first)
		}
	}

	ts := edgeTimestamp(time.Now())
	if !c.configured {
		config := "X-Timestamp:" + ts + "\r\n" +
			"Content-Type:application/json; charset=utf-8\r\n" +
			"Path:speech.config\r\n\r\n" +
			`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"false"},"outputFormat":"` + edgeOutputFmt + `"}}}}` + "\r\n"
		if err := conn.WriteMessage(websocket.TextMessage, []byte(config)); err != nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
		}
		c.configured = true
	}

	requestID := edgeRequestID()
	ssml := "X-RequestId:" + requestID + "\r\n" +
		"Content-Type:application/ssml+xml\r\n" +
		"X-Timestamp:" + ts + "Z\r\n" +
		"Path:ssml\r\n\r\n" + edgeSSML(in)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ssml)); err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}

	var audio []byte
	pending := true
	for {
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
		}
		if pending {
			pending = false
			if hasDeadline {
				_ = conn.SetReadDeadline(deadline)
			}
		}
		switch mt {
		case websocket.CloseMessage:
			return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音连接关闭", ErrSynthesisFailed)
		case websocket.TextMessage:
			text := string(payload)
			if strings.Contains(text, "Path:turn.end") {
				if len(audio) < 64 {
					return SynthesizeResult{}, false, fmt.Errorf("%w: 云端未返回音频", ErrSynthesisFailed)
				}
				hint := float64(len(audio)) / 6000
				if hint < 0.25 {
					hint = 0.25
				}
				return SynthesizeResult{WavBase64: base64.StdEncoding.EncodeToString(audio), DurationHint: hint}, false, nil
			}
			if strings.Contains(text, "Path:turn.error") || strings.Contains(text, `"error"`) {
				return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音合成被拒绝", ErrSynthesisFailed)
			}
		case websocket.BinaryMessage:
			if ctx.Err() != nil {
				return SynthesizeResult{}, false, ctx.Err()
			}
			chunk := edgeAudioPayload(payload)
			if len(chunk) == 0 {
				continue
			}
			if len(audio)+len(chunk) > edgeMaxBody {
				return SynthesizeResult{}, false, fmt.Errorf("%w: 云端音频过大", ErrSynthesisFailed)
			}
			audio = append(audio, chunk...)
			if emit != nil {
				if err := emit(chunk); err != nil {
					return SynthesizeResult{}, false, err
				}
			}
		}
	}
}

func dialEdgeWS(ctx context.Context, host, path, gec string) (*websocket.Conn, error) {
	connID := edgeRequestID()
	rawQuery := "TrustedClientToken=" + edgeTrustedToken +
		"&ConnectionId=" + connID +
		"&Sec-MS-GEC=" + gec +
		"&Sec-MS-GEC-Version=" + edgeSecMSGECVersion()
	u := url.URL{Scheme: "wss", Host: host, Path: path, RawQuery: rawQuery}

	major := strings.SplitN(edgeChromiumFull, ".", 2)[0]
	header := http.Header{}
	header.Set("Pragma", "no-cache")
	header.Set("Cache-Control", "no-cache")
	header.Set("Origin", "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold")
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/"+major+".0.0.0 Safari/537.36 Edg/"+major+".0.0.0")
	header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	header.Set("Accept-Language", "en-US,en;q=0.9")
	header.Set("Cookie", "muid="+edgeMUID()+";")

	dialer := websocket.Dialer{
		EnableCompression: true,
		HandshakeTimeout:  10 * time.Second,
		Proxy:             http.ProxyFromEnvironment,
	}
	conn, resp, err := dialer.DialContext(ctx, u.String(), header)
	if err == nil {
		return conn, nil
	}
	if resp == nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		return nil, err
	}
	serverDate, hasDate := time.Time{}, false
	if d := resp.Header.Get("Date"); d != "" {
		if t, perr := http.ParseTime(d); perr == nil {
			serverDate, hasDate = t, true
		}
	}
	return nil, &edgeHandshakeError{
		status:  resp.StatusCode,
		date:    serverDate,
		hasDate: hasDate,
		msg:     fmt.Sprintf("websocket handshake HTTP/1.1 %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
	}
}

func edgeAudioPayload(frame []byte) []byte {
	if len(frame) < 4 {
		return nil
	}
	headerLen := int(binary.BigEndian.Uint16(frame[:2]))
	if headerLen > 2 && headerLen+2 <= len(frame) {
		headerBlock := frame[:headerLen]
		if bytes.Contains(headerBlock, []byte("Path:audio")) {
			return frame[headerLen+2:]
		}
	}
	if headerLen > 0 && 2+headerLen+2 <= len(frame) {
		header := frame[2 : 2+headerLen]
		if bytes.Contains(header, []byte("Path:audio")) {
			return frame[2+headerLen+2:]
		}
	}
	idx := bytes.Index(frame, []byte(edgeAudioDelim))
	if idx < 0 {
		return nil
	}
	return frame[idx+len(edgeAudioDelim):]
}

func edgeRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2], out[i*2+1] = hexdigits[v>>4], hexdigits[v&0x0f]
	}
	return string(out)
}

type edgeHandshakeError struct {
	status  int
	date    time.Time
	hasDate bool
	msg     string
}

func (e *edgeHandshakeError) Error() string { return e.msg }

func (e *edgeEngine) adjustClockSkew(serverDate time.Time) {
	e.mu.Lock()
	e.clockSkew = time.Until(serverDate)
	e.mu.Unlock()
}
