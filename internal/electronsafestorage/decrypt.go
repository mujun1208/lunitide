// Package electronsafestorage contains the deliberately isolated foundation
// for reading legacy Electron safeStorage credentials during migration.
package electronsafestorage

import (
	"errors"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

const (
	maxEncodedBlob = 96 << 10
	maxPlaintext   = 64 << 10
	maxAPIKey      = 60 << 10
)

var (
	ErrInvalidInput    = errors.New("invalid legacy Electron credential")
	ErrBindingMismatch = errors.New("legacy Electron credential binding mismatch")
	ErrUnavailable     = errors.New("legacy Electron credential unavailable")
)

// WithAPIKey decrypts and validates one legacy encryptedApiKey, invokes use
// synchronously, and wipes the callback slice immediately after use returns.
// Callers must not retain the slice. This package does not persist or log any
// input, ciphertext, plaintext, or credential.
func WithAPIKey(encryptedAPIKey, expectedOrigin, expectedProtocol string, use func([]byte) error) error {
	return WithAPIKeyAndEncryptedKey(encryptedAPIKey, "", expectedOrigin, expectedProtocol, use)
}

// WithAPIKeyAndEncryptedKey supports Chromium v10/v11 values whose AES key is
// DPAPI-protected in the same pinned Electron Local State file.
func WithAPIKeyAndEncryptedKey(encryptedAPIKey, encryptedKey, expectedOrigin, expectedProtocol string, use func([]byte) error) error {
	if use == nil || len(encryptedAPIKey) == 0 || len(encryptedAPIKey) > maxEncodedBlob {
		return ErrInvalidInput
	}
	origin, err := provider.NormalizeOrigin(expectedOrigin)
	if err != nil || origin != expectedOrigin {
		return ErrInvalidInput
	}
	// Electron persisted the historical values "openai" and "anthropic" in
	// the encrypted envelope. Keep this allowlist separate from normalized Go
	// provider protocols so binding validation remains exact.
	if expectedProtocol != "openai" && expectedProtocol != string(provider.ProtocolAnthropic) {
		return ErrInvalidInput
	}
	return withAPIKeyPlatform(encryptedAPIKey, encryptedKey, origin, expectedProtocol, use)
}

func zero(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
