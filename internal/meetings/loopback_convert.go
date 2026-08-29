package meetings

import (
	"encoding/binary"
	"math"
)

type pcmFormat struct {
	channels   int
	rate       int
	bits       int
	blockAlign int
	float      bool
}

func convertTo16kMonoS16(raw []byte, format pcmFormat) []byte {
	if format.channels < 1 || format.rate < 1 || format.blockAlign < 1 || len(raw) < format.blockAlign {
		return nil
	}
	frames := len(raw) / format.blockAlign
	mono := make([]float64, frames)
	for i := 0; i < frames; i++ {
		off := i * format.blockAlign
		var sum float64
		for c := 0; c < format.channels; c++ {
			sum += sampleAt(raw, off, c, format)
		}
		mono[i] = sum / float64(format.channels)
	}
	return resampleToS16(mono, format.rate, audioSampleRate)
}

func sampleAt(raw []byte, frameOff, channel int, format pcmFormat) float64 {
	width := format.bits / 8
	if width < 1 {
		width = format.blockAlign / format.channels
	}
	off := frameOff + channel*width
	if off+width > len(raw) {
		return 0
	}
	if format.float && width >= 4 {
		bits := binary.LittleEndian.Uint32(raw[off:])
		return float64(math.Float32frombits(bits))
	}
	switch width {
	case 2:
		v := int16(binary.LittleEndian.Uint16(raw[off:]))
		return float64(v) / 32768
	case 3:
		v := int32(raw[off]) | int32(raw[off+1])<<8 | int32(raw[off+2])<<16
		if v&0x800000 != 0 {
			v |= ^0xFFFFFF
		}
		return float64(v) / 8388608
	case 4:
		v := int32(binary.LittleEndian.Uint32(raw[off:]))
		return float64(v) / 2147483648
	default:
		return 0
	}
}

func resampleToS16(in []float64, fromRate, toRate int) []byte {
	if len(in) == 0 || fromRate < 1 || toRate < 1 {
		return nil
	}
	if fromRate == toRate {
		return floatsToS16(in)
	}
	outLen := int(float64(len(in)) * float64(toRate) / float64(fromRate))
	if outLen < 1 {
		return nil
	}
	out := make([]float64, outLen)
	ratio := float64(fromRate) / float64(toRate)
	last := len(in) - 1
	for i := 0; i < outLen; i++ {
		pos := float64(i) * ratio
		left := int(pos)
		if left >= last {
			out[i] = in[last]
			continue
		}
		frac := pos - float64(left)
		out[i] = in[left]*(1-frac) + in[left+1]*frac
	}
	return floatsToS16(out)
}

func floatsToS16(in []float64) []byte {
	out := make([]byte, len(in)*2)
	for i, sample := range in {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		var v int16
		if sample < 0 {
			v = int16(sample * 32768)
		} else {
			v = int16(sample * 32767)
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}
