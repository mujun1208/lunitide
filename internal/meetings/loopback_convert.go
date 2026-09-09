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
