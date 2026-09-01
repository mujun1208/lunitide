package talk

import (
	"encoding/binary"
	"testing"
)

func TestUpsamplePCM16kTo24k(t *testing.T) {
	in := make([]byte, 4)
	binary.LittleEndian.PutUint16(in[0:], 0)
	binary.LittleEndian.PutUint16(in[2:], 30000)
	out := UpsamplePCM16kTo24k(in)
	if len(out) != 6 {
		t.Fatalf("len=%d", len(out))
	}
	if binary.LittleEndian.Uint16(out[0:]) != 0 {
		t.Fatalf("first=%d", binary.LittleEndian.Uint16(out[0:]))
	}
	if binary.LittleEndian.Uint16(out[4:]) != 30000 {
		t.Fatalf("last=%d", binary.LittleEndian.Uint16(out[4:]))
	}
}
