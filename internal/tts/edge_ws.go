package tts

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	edgeSynthHost  = "speech.platform.bing.com"
	edgeSynthPath  = "/consumer/speech/synthesize/readaloud/edge/v1"
	edgeAudioDelim = "Path:audio\r\n"
	edgeOutputFmt  = "riff-24khz-16bit-mono-pcm"
	edgeGUIDMagic  = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	edgeMaxFrame   = 4 << 20
)

func (e *edgeEngine) synthesizeWS(ctx context.Context, in SynthesizeInput) (SynthesizeResult, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	e.mu.Lock()
	skew := e.clockSkew
	e.mu.Unlock()
	conn, err := dialEdgeWS(ctx, edgeSecMSGEC(time.Now().Add(skew)))
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 无法连接微软云端语音（需联网）: %v", ErrEngineUnavailable, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	requestID := edgeRequestID()
	config := "X-Timestamp:" + time.Now().UTC().Format(time.RFC3339) + "\r\n" +
		"Content-Type:application/json; charset=utf-8\r\n" +
		"Path:speech.config\r\n\r\n" +
		`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"false"},"outputFormat":"` + edgeOutputFmt + `"}}}}`
	if err := wsWriteText(conn, config); err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}
	ssml := "X-RequestId:" + requestID + "\r\n" +
		"Content-Type:application/ssml+xml\r\n" +
		"Path:ssml\r\n\r\n" + edgeSSML(in)
	if err := wsWriteText(conn, ssml); err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}

	var audio []byte
	for {
		opcode, payload, err := wsRead(conn)
		if err != nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
		}
		switch opcode {
		case 0x8:
			return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音连接关闭", ErrSynthesisFailed)
		case 0x1:
			if strings.Contains(string(payload), "Path:turn.end") {
				if len(audio) < 44 {
					return SynthesizeResult{}, false, fmt.Errorf("%w: 云端未返回音频", ErrSynthesisFailed)
				}
				hint := float64(len(audio)-44) / (24000 * 2)
				return SynthesizeResult{WavBase64: base64.StdEncoding.EncodeToString(audio), DurationHint: hint}, false, nil
			}
		case 0x2:
			chunk := edgeAudioPayload(payload)
			if len(chunk) == 0 {
				continue
			}
			if len(audio)+len(chunk) > edgeMaxBody {
				return SynthesizeResult{}, false, fmt.Errorf("%w: 云端音频过大", ErrSynthesisFailed)
			}
			audio = append(audio, chunk...)
		}
	}
}

func dialEdgeWS(ctx context.Context, gec string) (net.Conn, error) {
	connID := edgeRequestID()
	query := url.Values{}
	query.Set("TrustedClientToken", edgeTrustedToken)
	query.Set("Sec-MS-GEC", gec)
	query.Set("Sec-MS-GEC-Version", edgeSecMSGECVersion())
	query.Set("ConnectionId", connID)
	d := net.Dialer{Timeout: 10 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(edgeSynthHost, "443"))
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(raw, &tls.Config{ServerName: edgeSynthHost, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		tlsConn.Close()
		return nil, err
	}
	secKey := base64.StdEncoding.EncodeToString(key)
	major := strings.SplitN(edgeChromiumFull, ".", 2)[0]
	req := "GET " + edgeSynthPath + "?" + query.Encode() + " HTTP/1.1\r\n" +
		"Host: " + edgeSynthHost + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + secKey + "\r\n" +
		"Origin: chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold\r\n" +
		"User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + major + ".0.0.0 Safari/537.36 Edg/" + major + ".0.0.0\r\n" +
		"Pragma: no-cache\r\n" +
		"Cache-Control: no-cache\r\n\r\n"
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		tlsConn.Close()
		return nil, err
	}
	br := bufio.NewReader(tlsConn)
	status, err := br.ReadString('\n')
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		tlsConn.Close()
		return nil, fmt.Errorf("websocket handshake %s", strings.TrimSpace(status))
	}
	var accept string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			tlsConn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "sec-websocket-accept:") {
			accept = strings.TrimSpace(line[len("sec-websocket-accept:"):])
		}
	}
	want := wsAccept(secKey)
	if !strings.EqualFold(accept, want) {
		tlsConn.Close()
		return nil, fmt.Errorf("websocket accept mismatch")
	}
	return &wsBufferedConn{Conn: tlsConn, r: br}, nil
}

type wsBufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *wsBufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func wsAccept(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key+edgeGUIDMagic)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func wsWriteText(conn net.Conn, text string) error {
	return wsWrite(conn, 0x1, []byte(text))
}

func wsWrite(conn net.Conn, opcode byte, payload []byte) error {
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	n := len(payload)
	var hdr []byte
	hdr = append(hdr, 0x80|opcode)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 65536:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr = append(hdr, 0x80|127)
		hdr = append(hdr, ext[:]...)
	}
	hdr = append(hdr, mask...)
	masked := make([]byte, n)
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	_, err := conn.Write(append(hdr, masked...))
	return err
}

func wsRead(conn net.Conn) (byte, []byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	h := make([]byte, 2)
	if _, err := io.ReadFull(conn, h); err != nil {
		return 0, nil, err
	}
	opcode := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	n := int(h[1] & 0x7f)
	if n == 126 {
		ext := make([]byte, 2)
		if _, err := io.ReadFull(conn, ext); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(ext))
	} else if n == 127 {
		ext := make([]byte, 8)
		if _, err := io.ReadFull(conn, ext); err != nil {
			return 0, nil, err
		}
		n64 := binary.BigEndian.Uint64(ext)
		if n64 > edgeMaxFrame {
			return 0, nil, fmt.Errorf("frame too large")
		}
		n = int(n64)
	}
	if n > edgeMaxFrame {
		return 0, nil, fmt.Errorf("frame too large")
	}
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(conn, mask); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			return 0, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func edgeAudioPayload(frame []byte) []byte {
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
