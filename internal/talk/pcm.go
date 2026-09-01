package talk

import (
	"encoding/base64"
	"encoding/binary"
)

// UpsamplePCM16kTo24k linearly stretches 16 kHz s16le onto 24 kHz.
// Capture is 16 kHz; OpenAI-shaped realtime pcm16 is 24 kHz.
func UpsamplePCM16kTo24k(pcm []byte) []byte {
	if len(pcm) < 2 || len(pcm)%2 != 0 {
		return pcm
	}
	inN := len(pcm) / 2
	outN := inN * 3 / 2
	if outN < 1 {
		return pcm
	}
	out := make([]byte, outN*2)
	for i := 0; i < outN; i++ {
		pos := float64(i) * 2 / 3
		left := int(pos)
		if left >= inN {
			left = inN - 1
		}
		right := left + 1
		if right >= inN {
			right = inN - 1
		}
		weight := pos - float64(left)
		a := int16(binary.LittleEndian.Uint16(pcm[left*2:]))
		b := int16(binary.LittleEndian.Uint16(pcm[right*2:]))
		sample := int16(float64(a)*(1-weight) + float64(b)*weight)
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}

func UpsamplePCM16kBase64To24k(pcmBase64 string) string {
	raw, err := base64.StdEncoding.DecodeString(pcmBase64)
	if err != nil || len(raw) < 2 {
		return pcmBase64
	}
	return base64.StdEncoding.EncodeToString(UpsamplePCM16kTo24k(raw))
}
