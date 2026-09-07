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

	"github.com/lunitide/lunitide/internal/voice"
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

type utteranceBit struct {
	Text      string `json:"text"`
	Definite  bool   `json:"definite"`
	StartTime *int64 `json:"start_time"`
	EndTime   *int64 `json:"end_time"`
}

type resultBit struct {
	Text       string         `json:"text"`
	Utterances []utteranceBit `json:"utterances"`
}

func pickResultText(text string, utterances []utteranceBit) (string, bool) {
	out := strings.TrimSpace(text)
	final := false
	var parts []string
	for _, u := range utterances {
		if part := strings.TrimSpace(u.Text); part != "" {
			parts = append(parts, part)
			// full results include earlier finished sentences. Only the last
			// nonempty utterance describes whether the current tail has ended.
			final = u.Definite
		}
	}
	if out == "" {
		out = strings.Join(parts, "")
	}
	return out, final
}

func resultFromJSON(raw []byte) (resultBit, bool) {
	var wrap struct {
		PayloadMsg json.RawMessage `json:"payload_msg"`
		Result     json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return resultBit{}, false
	}
	if nested := bytes.TrimSpace(wrap.PayloadMsg); len(nested) > 0 && !bytes.Equal(nested, bytes.TrimSpace(raw)) {
		if item, ok := resultFromJSON(nested); ok {
			return item, true
		}
	}
	result := bytes.TrimSpace(wrap.Result)
	if len(result) == 0 || bytes.Equal(result, []byte("null")) {
		return resultBit{}, false
	}
	if result[0] == '[' {
		var items []resultBit
		if json.Unmarshal(result, &items) != nil {
			return resultBit{}, false
		}
		for i := len(items) - 1; i >= 0; i-- {
			if text, _ := pickResultText(items[i].Text, items[i].Utterances); text != "" {
				return items[i], true
			}
		}
		return resultBit{}, false
	}
	var item resultBit
	if json.Unmarshal(result, &item) != nil {
		return resultBit{}, false
	}
	text, _ := pickResultText(item.Text, item.Utterances)
	return item, text != ""
}

// TranscriptFromJSON maps a SAUC result body onto text + endpoint.
// Official payloads may wrap the body in payload_msg, and result may be
// either an object or a list.
func TranscriptFromJSON(raw []byte) (text string, final bool, ok bool) {
	item, ok := resultFromJSON(raw)
	if !ok {
		return "", false, false
	}
	text, final = pickResultText(item.Text, item.Utterances)
	return text, final, true
}

// Keep a bounded recent window in append replies. The renderer identifies
// segments by audio time, so it never needs the entire meeting on every 100ms
// audio reply. Text-only older providers retain their compatible fallback.
func snapshotFromJSON(raw []byte) (voice.Transcript, bool) {
	item, ok := resultFromJSON(raw)
	if !ok {
		return voice.Transcript{}, false
	}
	text, final := pickResultText(item.Text, item.Utterances)
	tr := voice.Transcript{Text: text, Final: final}
	start := max(0, len(item.Utterances)-64)
	for _, u := range item.Utterances[start:] {
		if strings.TrimSpace(u.Text) == "" {
			continue
		}
		if u.StartTime == nil || u.EndTime == nil || *u.StartTime < 0 || *u.EndTime < *u.StartTime {
			tr.Utterances = nil
			break
		}
		tr.Utterances = append(tr.Utterances, voice.Utterance{Text: u.Text, StartMs: *u.StartTime, EndMs: *u.EndTime, Final: u.Definite})
	}
	if len(tr.Utterances) > 0 {
		var parts []string
		for _, u := range tr.Utterances {
			parts = append(parts, u.Text)
		}
		tr.Text = strings.Join(parts, "")
	}
	return tr, true
}
