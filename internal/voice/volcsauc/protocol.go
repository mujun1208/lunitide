// Package volcsauc is the Volcengine seed-asr 2.0 streaming recognizer.
//
// It is a separate ear from sherpa: same voice.Backend / voice.Session
// contract, different wire. The companion stage picks it by backend name
// rather than by mixing protocols inside the local ONNX path.
package volcsauc

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	protocolVersion byte = 0x1
	headerSizeUnits byte = 0x1 // 4-byte header

	msgFullClient  byte = 0x1
	msgAudioOnly   byte = 0x2
	msgFullServer  byte = 0x9
	msgErrorServer byte = 0xf

	flagPosSeq     byte = 0x1
	flagNegWithSeq byte = 0x3

	serialRaw  byte = 0x0
	serialJSON byte = 0x1

	compressGzip byte = 0x1
)

// Frame is one decoded SAUC v3 packet.
type Frame struct {
	Type     byte
	Flags    byte
	Sequence int32
	HasSeq   bool
	JSON     []byte
	Error    int
	Raw      []byte
}

func gzipBytes(p []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(p)
	_ = w.Close()
	return buf.Bytes()
}

func gunzipBytes(p []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(p))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func pack(msgType, flags, serial, compress byte, seq int32, payload []byte) []byte {
	out := []byte{
		(protocolVersion << 4) | headerSizeUnits,
		(msgType << 4) | flags,
		(serial << 4) | compress,
		0,
	}
	if flags&0x01 != 0 {
		var seqBuf [4]byte
		binary.BigEndian.PutUint32(seqBuf[:], uint32(seq))
		out = append(out, seqBuf[:]...)
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload)))
	out = append(out, size[:]...)
	return append(out, payload...)
}

// EncodeFullClient is the first packet: JSON config, gzipped.
func EncodeFullClient(seq int32, body []byte) []byte {
	return pack(msgFullClient, flagPosSeq, serialJSON, compressGzip, seq, gzipBytes(body))
}

// EncodeAudio is one PCM packet. last marks the negative-sequence end frame.
func EncodeAudio(seq int32, pcm []byte, last bool) []byte {
	flags := flagPosSeq
	if last {
		flags = flagNegWithSeq
		if seq > 0 {
			seq = -seq
		}
	}
	return pack(msgAudioOnly, flags, serialRaw, compressGzip, seq, gzipBytes(pcm))
}

// DecodeFrame parses one binary WebSocket payload.
func DecodeFrame(raw []byte) (Frame, error) {
	if len(raw) < 4 {
		return Frame{}, fmt.Errorf("sauc: header too short")
	}
	headerSize := int(raw[0]&0x0f) * 4
	if headerSize < 4 || len(raw) < headerSize {
		return Frame{}, fmt.Errorf("sauc: truncated header")
	}
	frame := Frame{
		Type:  raw[1] >> 4,
		Flags: raw[1] & 0x0f,
	}
	serial := raw[2] >> 4
	compress := raw[2] & 0x0f
	payload := raw[headerSize:]
	if frame.Flags&0x01 != 0 {
		if len(payload) < 4 {
			return Frame{}, fmt.Errorf("sauc: missing sequence")
		}
		frame.Sequence = int32(binary.BigEndian.Uint32(payload[:4]))
		frame.HasSeq = true
		payload = payload[4:]
	}
	switch frame.Type {
	case msgErrorServer:
		if len(payload) < 8 {
			return Frame{}, fmt.Errorf("sauc: truncated error")
		}
		frame.Error = int(binary.BigEndian.Uint32(payload[:4]))
		size := int(binary.BigEndian.Uint32(payload[4:8]))
		payload = payload[8:]
		if size > 0 && len(payload) > size {
			payload = payload[:size]
		}
	default:
		if len(payload) < 4 {
			return Frame{}, fmt.Errorf("sauc: missing payload size")
		}
		size := int(binary.BigEndian.Uint32(payload[:4]))
		payload = payload[4:]
		if size >= 0 && len(payload) > size {
			payload = payload[:size]
		}
	}
	if compress == compressGzip && len(payload) > 0 {
		plain, err := gunzipBytes(payload)
		if err != nil {
			return Frame{}, fmt.Errorf("sauc: gunzip: %w", err)
		}
		payload = plain
	}
	if serial == serialJSON {
		frame.JSON = payload
	} else {
		frame.Raw = payload
	}
	return frame, nil
}

type serverResult struct {
	Result *struct {
		Text       string `json:"text"`
		Utterances []struct {
			Text     string `json:"text"`
			Definite bool   `json:"definite"`
		} `json:"utterances"`
	} `json:"result"`
}

// TranscriptFromJSON maps a SAUC result body onto text + endpoint.
func TranscriptFromJSON(raw []byte) (text string, final bool, ok bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false, false
	}
	var body serverResult
	if json.Unmarshal(raw, &body) != nil || body.Result == nil {
		return "", false, false
	}
	text = strings.TrimSpace(body.Result.Text)
	for _, u := range body.Result.Utterances {
		if strings.TrimSpace(u.Text) != "" {
			if text == "" {
				text = strings.TrimSpace(u.Text)
			}
			if u.Definite {
				final = true
			}
		}
	}
	if text == "" {
		return "", false, false
	}
	return text, final, true
}
