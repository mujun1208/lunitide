package ipc

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestSaveLoadGatewayNonce(t *testing.T) {
	path := filepath.Join(t.TempDir(), GatewayNonceFile)
	nonce := bytes.Repeat([]byte{0xab}, sessionSecretSize)
	if err := SaveGatewayNonce(path, nonce); err != nil {
		t.Fatal(err)
	}
	got, err := LoadGatewayNonce(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, nonce) {
		t.Fatalf("got %x", got)
	}
	if _, err := LoadGatewayNonce(path + ".missing"); err == nil {
		t.Fatal("missing nonce must fail")
	}
}

func TestSaveLoadEnginePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), GatewayEnginePIDFile)
	if err := SaveEnginePID(path, 4242); err != nil {
		t.Fatal(err)
	}
	got, err := LoadEnginePID(path)
	if err != nil || got != 4242 {
		t.Fatalf("got %d %v", got, err)
	}
	if err := SaveEnginePID(path, 0); err == nil {
		t.Fatal("pid 0 must fail")
	}
	if _, err := LoadEnginePID(path + ".missing"); err == nil {
		t.Fatal("missing pid must fail")
	}
}
