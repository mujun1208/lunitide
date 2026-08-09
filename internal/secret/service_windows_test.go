//go:build windows

package secret

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/lunitide/lunitide/internal/datadir"
)

func TestDPAPIRoundTripWrongEntropyDelete(t *testing.T) {
	root, err := datadir.PrepareForTest(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	service, _ := NewDPAPIService(root)
	ref := Ref{CredentialRef: "credential-1", ProviderID: "provider-1", Origin: "HTTPS://EXAMPLE.COM/", Protocol: "openai_compatible"}
	input := []byte("secret-canary-dpapi")
	if err := service.Put(context.Background(), ref, input); err != nil {
		t.Fatal(err)
	}
	if err := service.WithSecret(context.Background(), ref, func(got []byte) error {
		if !bytes.Equal(got, input) {
			return errors.New("mismatch")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wrong := ref
	wrong.Protocol = "anthropic"
	correctPath, _ := root.FilePath(assetName(mustValid(t, ref)))
	wrongPath, _ := root.FilePath(assetName(mustValid(t, wrong)))
	if correctPath == wrongPath {
		t.Fatal("different entropy produced the same asset path")
	}
	ciphertext, err := os.ReadFile(correctPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, input) {
		t.Fatal("plaintext canary present in DPAPI asset")
	}
	if err := os.WriteFile(wrongPath, ciphertext, 0600); err != nil {
		t.Fatal(err)
	}
	Zero(ciphertext)
	if err := root.ProtectRegularFile(filepath.Base(wrongPath)); err != nil {
		t.Fatal(err)
	}
	wrongCalled := false
	wrongRevealed := false
	wrongErr := service.WithSecret(context.Background(), wrong, func(got []byte) error {
		wrongCalled = true
		wrongRevealed = bytes.Equal(got, input)
		return nil
	})
	if wrongErr == nil || errors.Is(wrongErr, os.ErrNotExist) || wrongCalled || wrongRevealed {
		t.Fatal("wrong DPAPI entropy accepted")
	}
	if err := service.Delete(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := service.WithSecret(context.Background(), ref, func([]byte) error { return nil }); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted secret readable: %v", err)
	}
}

func TestNativeBlobIsZeroedBeforeFree(t *testing.T) {
	value := []byte("native-plaintext")
	oldZero, oldFree := secureZeroNative, localFreeNative
	defer func() { secureZeroNative, localFreeNative = oldZero, oldFree }()
	zeroed := false
	secureZeroNative = func(data *byte, size uintptr) { Zero(unsafe.Slice(data, size)); zeroed = true }
	localFreeNative = func(*byte) {
		if !zeroed {
			t.Fatal("LocalFree called before secure zero")
		}
		for _, b := range value {
			if b != 0 {
				t.Fatal("native buffer was not zeroed")
			}
		}
	}
	releaseNativeBlob(dataBlob{size: uint32(len(value)), data: &value[0]})
}

func mustValid(t *testing.T, ref Ref) Ref {
	t.Helper()
	valid, err := ref.Validate()
	if err != nil {
		t.Fatal(err)
	}
	return valid
}
