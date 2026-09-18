package tts

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode"
)

// ConcatWAV joins PCM WAV clips that share the same format into one file.
func ConcatWAV(parts [][]byte) ([]byte, error) {
	if len(parts) == 0 {
		return nil, errors.New("no audio")
	}
	var format []byte
	var pcm []byte
	for i, part := range parts {
		fmtChunk, data, err := parseWAV(part)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			format = fmtChunk
		} else if !sameWAVFormat(format, fmtChunk) {
			return nil, errors.New("wav format mismatch")
		}
		pcm = append(pcm, data...)
	}
	return writeWAV(format, pcm)
}

func parseWAV(b []byte) ([]byte, []byte, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, nil, errors.New("not a wav clip")
	}
	i := 12
	var format []byte
	for i+8 <= len(b) {
		id := string(b[i : i+4])
		size := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		i += 8
		if size < 0 || i+size > len(b) {
			return nil, nil, errors.New("truncated wav")
		}
		chunk := b[i : i+size]
		i += size
		if size%2 == 1 && i < len(b) {
			i++
		}
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, nil, errors.New("invalid wav format")
			}
			format = append([]byte(nil), chunk...)
		case "data":
			if len(format) < 16 {
				return nil, nil, errors.New("wav missing format")
			}
			return format, chunk, nil
		}
	}
	return nil, nil, errors.New("wav missing data")
}

func sameWAVFormat(a, b []byte) bool {
	if len(a) < 16 || len(b) < 16 {
		return false
	}
	return a[0] == b[0] && a[1] == b[1] &&
		a[2] == b[2] && a[3] == b[3] &&
		a[4] == b[4] && a[5] == b[5] && a[6] == b[6] && a[7] == b[7] &&
		a[14] == b[14] && a[15] == b[15]
}

func writeWAV(format, pcm []byte) ([]byte, error) {
	if len(format) < 16 {
		return nil, errors.New("invalid wav format")
	}
	if len(pcm)%2 == 1 {
		pcm = append(pcm, 0)
	}
	fmtSize := len(format)
	if fmtSize%2 == 1 {
		format = append(format, 0)
		fmtSize++
	}
	riffSize := 4 + (8 + fmtSize) + (8 + len(pcm))
	out := make([]byte, 0, 12+8+fmtSize+8+len(pcm))
	out = append(out, 'R', 'I', 'F', 'F')
	out = binary.LittleEndian.AppendUint32(out, uint32(riffSize))
	out = append(out, 'W', 'A', 'V', 'E')
	out = append(out, 'f', 'm', 't', ' ')
	out = binary.LittleEndian.AppendUint32(out, uint32(len(format)))
	out = append(out, format...)
	out = append(out, 'd', 'a', 't', 'a')
	out = binary.LittleEndian.AppendUint32(out, uint32(len(pcm)))
	out = append(out, pcm...)
	return out, nil
}

// PCM16MonoWAV builds a 16-bit little-endian mono WAV at sampleRate.
func PCM16MonoWAV(sampleRate int, pcm []byte) []byte {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if len(pcm)%2 == 1 {
		pcm = append([]byte(nil), pcm...)
		pcm = append(pcm, 0)
	}
	format := make([]byte, 16)
	binary.LittleEndian.PutUint16(format[0:2], 1)
	binary.LittleEndian.PutUint16(format[2:4], 1)
	binary.LittleEndian.PutUint32(format[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(format[8:12], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(format[12:14], 2)
	binary.LittleEndian.PutUint16(format[14:16], 16)
	out, _ := writeWAV(format, pcm)
	return out
}

// SplitSpeechSegments breaks long lyrics into TTS-sized clips.
func SplitSpeechSegments(text string, max int) []string {
	if max < 1 {
		max = MaxSegmentChars
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if len(runes) <= max {
		return []string{text}
	}
	var out []string
	var buf []rune
	flushCut := func(cut int) {
		if cut < 1 {
			cut = len(buf)
		}
		if cut > len(buf) {
			cut = len(buf)
		}
		piece := strings.TrimSpace(string(buf[:cut]))
		if piece != "" {
			out = append(out, piece)
		}
		rest := append([]rune(nil), buf[cut:]...)
		buf = rest
	}
	for _, r := range runes {
		buf = append(buf, r)
		if len(buf) < max {
			continue
		}
		cut := -1
		start := len(buf) / 3
		if start < 1 {
			start = 1
		}
		for i := len(buf) - 1; i >= start; i-- {
			if isSpeechBreak(buf[i]) {
				cut = i + 1
				break
			}
		}
		if cut < 1 {
			cut = max
			if cut > len(buf) {
				cut = len(buf)
			}
		}
		flushCut(cut)
	}
	if piece := strings.TrimSpace(string(buf)); piece != "" {
		out = append(out, piece)
	}
	return out
}

func isSpeechBreak(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '!', '?', ';', '\n', '，', ',', '、':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}
