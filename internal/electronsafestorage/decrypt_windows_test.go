//go:build windows

package electronsafestorage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var testCryptProtect = windows.NewLazySystemDLL("crypt32.dll").NewProc("CryptProtectData")

func protectLegacy(t *testing.T, plaintext []byte) string {
	t.Helper()
	in := dataBlob{size: uint32(len(plaintext)), data: &plaintext[0]}
	var out dataBlob
	// NULL optional entropy matches Electron safeStorage on Windows.
	r, _, _ := testCryptProtect.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		t.Fatal(syscall.GetLastError())
	}
	defer releaseNative(out)
	ciphertext := append([]byte(nil), unsafe.Slice(out.data, out.size)...)
	defer zero(ciphertext)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func envelope(key, origin, protocol string) []byte {
	return []byte(`{"version":1,"apiKey":"` + key + `","origin":"` + origin + `","protocol":"` + protocol + `"}`)
}

func TestWindowsDPAPIRoundTripAndWipesCallbackCredential(t *testing.T) {
	plaintext := envelope("api-key-leak-canary", "https://example.test", "openai")
	encoded := protectLegacy(t, plaintext)
	if strings.Contains(encoded, "api-key-leak-canary") {
		t.Fatal("plaintext canary present in encoded ciphertext")
	}
	var callbackView []byte
	err := WithAPIKey(encoded, "https://example.test", "openai", func(key []byte) error {
		if string(key) != "api-key-leak-canary" {
			t.Fatalf("wrong key: %q", key)
		}
		callbackView = key
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(callbackView, make([]byte, len(callbackView))) {
		t.Fatal("callback credential was not wiped")
	}
	zero(plaintext)
}

func TestWindowsRejectsTamperAndBindingMismatch(t *testing.T) {
	encoded := protectLegacy(t, envelope("canary", "https://example.test", "anthropic"))
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	raw[len(raw)/2] ^= 0x80
	tampered := base64.StdEncoding.EncodeToString(raw)
	zero(raw)
	called := false
	if err := WithAPIKey(tampered, "https://example.test", "anthropic", func([]byte) error { called = true; return nil }); err == nil || called {
		t.Fatal("tampered credential accepted")
	}
	for name, binding := range map[string][2]string{
		"origin":   {"https://other.test", "anthropic"},
		"protocol": {"https://example.test", "openai"},
	} {
		t.Run(name, func(t *testing.T) {
			called = false
			err := WithAPIKey(encoded, binding[0], binding[1], func([]byte) error { called = true; return nil })
			if !errors.Is(err, ErrBindingMismatch) || called {
				t.Fatalf("binding mismatch accepted: %v", err)
			}
		})
	}
}

func TestWindowsRejectsMalformedAndOversizedInputs(t *testing.T) {
	validOrigin := "https://example.test"
	cases := map[string]string{
		"empty":           "",
		"bad-base64":      "not base64!",
		"oversized-b64":   strings.Repeat("A", maxEncodedBlob+1),
		"bad-json":        protectLegacy(t, []byte(`{"version":1`)),
		"unknown-field":   protectLegacy(t, []byte(`{"version":1,"apiKey":"x","origin":"https://example.test","protocol":"anthropic","extra":true}`)),
		"duplicate-field": protectLegacy(t, []byte(`{"version":1,"version":1,"apiKey":"x","origin":"https://example.test","protocol":"anthropic"}`)),
		"wrong-version":   protectLegacy(t, []byte(`{"version":2,"apiKey":"x","origin":"https://example.test","protocol":"anthropic"}`)),
		"empty-key":       protectLegacy(t, envelope("", validOrigin, "anthropic")),
		"oversized-key":   protectLegacy(t, envelope(strings.Repeat("k", maxAPIKey+1), validOrigin, "anthropic")),
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			called := false
			if err := WithAPIKey(encoded, validOrigin, "anthropic", func([]byte) error { called = true; return nil }); err == nil || called {
				t.Fatal("malformed input accepted")
			}
		})
	}
}

func TestWindowsRequiresExactCanonicalExpectedBinding(t *testing.T) {
	encoded := protectLegacy(t, envelope("canary", "https://example.test", "openai"))
	for _, origin := range []string{"HTTPS://EXAMPLE.TEST/", "https://example.test/v1", "https://example.test:443"} {
		if err := WithAPIKey(encoded, origin, "openai", func([]byte) error { return nil }); err == nil {
			t.Fatalf("noncanonical origin accepted: %q", origin)
		}
	}
}
