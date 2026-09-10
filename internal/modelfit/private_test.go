package modelfit

import (
	"bytes"
	"testing"
)

func TestSealProtocolPrivateRoundTripAndDigest(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	blob, digest, err := SealProtocolPrivate([]byte("need file"), key)
	if err != nil || digest == "" || bytes.Contains(blob, []byte("need file")) {
		t.Fatalf("seal failed or leaked plaintext: %q %v", blob, err)
	}
	plain, err := OpenProtocolPrivate(blob, key)
	if err != nil || string(plain) != "need file" {
		t.Fatalf("open: %q %v", plain, err)
	}
	if _, err := OpenProtocolPrivate(blob, bytes.Repeat([]byte{1}, 32)); err == nil {
		t.Fatal("wrong key must not decrypt")
	}
}
