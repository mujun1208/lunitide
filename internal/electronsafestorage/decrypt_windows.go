//go:build windows

package electronsafestorage

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type dataBlob struct {
	size uint32
	data *byte
}

var (
	crypt32          = windows.NewLazySystemDLL("crypt32.dll")
	cryptUnprotect   = crypt32.NewProc("CryptUnprotectData")
	advapi32         = windows.NewLazySystemDLL("advapi32.dll")
	rtlSecureZero    = advapi32.NewProc("SystemFunction040")
	secureZeroNative = func(data *byte, size uintptr) {
		_, _, _ = rtlSecureZero.Call(uintptr(unsafe.Pointer(data)), size)
	}
	localFreeNative = func(data *byte) { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(data))) }
)

func withAPIKeyPlatform(encoded, encryptedKey, expectedOrigin, expectedProtocol string, use func([]byte) error) error {
	decodedLen := base64.StdEncoding.DecodedLen(len(encoded))
	if decodedLen == 0 || decodedLen > maxPlaintext+4096 {
		return ErrInvalidInput
	}
	ciphertext := make([]byte, decodedLen)
	n, err := base64.StdEncoding.Strict().Decode(ciphertext, []byte(encoded))
	if err != nil || n == 0 {
		zero(ciphertext)
		return ErrInvalidInput
	}
	ciphertext = ciphertext[:n]
	defer zero(ciphertext)

	plaintext, err := decryptElectronValue(ciphertext, encryptedKey)
	if err != nil {
		return ErrUnavailable
	}
	defer zero(plaintext)
	if len(plaintext) == 0 || len(plaintext) > maxPlaintext {
		return ErrInvalidInput
	}

	apiKey, origin, protocol, err := decodeEnvelope(plaintext)
	if err != nil {
		return ErrInvalidInput
	}
	defer zero(apiKey)
	if origin != expectedOrigin || protocol != expectedProtocol {
		return ErrBindingMismatch
	}
	return use(apiKey)
}

func decryptElectronValue(ciphertext []byte, encodedKey string) ([]byte, error) {
	if !bytes.HasPrefix(ciphertext, []byte("v10")) && !bytes.HasPrefix(ciphertext, []byte("v11")) {
		return unprotectCurrentUser(ciphertext)
	}
	if encodedKey == "" || len(ciphertext) < 31 {
		return nil, errors.New("missing Chromium key")
	}
	wrapped, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || !bytes.HasPrefix(wrapped, []byte("DPAPI")) || len(wrapped) <= 5 {
		zero(wrapped)
		return nil, errors.New("invalid Chromium key")
	}
	defer zero(wrapped)
	key, err := unprotectCurrentUser(wrapped[5:])
	if err != nil {
		return nil, err
	}
	defer zero(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, ciphertext[3:15], ciphertext[15:], nil)
}

func unprotectCurrentUser(ciphertext []byte) ([]byte, error) {
	in := dataBlob{size: uint32(len(ciphertext)), data: &ciphertext[0]}
	var out dataBlob
	// Electron safeStorage on Windows calls CurrentUser DPAPI with no optional
	// entropy. CRYPTPROTECT_UI_FORBIDDEN prevents an unexpected UI prompt.
	r, _, _ := cryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, syscall.GetLastError()
	}
	if out.data == nil || out.size == 0 || out.size > maxPlaintext {
		releaseNative(out)
		return nil, errors.New("invalid DPAPI output")
	}
	defer releaseNative(out)
	plaintext := make([]byte, out.size)
	copy(plaintext, unsafe.Slice(out.data, out.size))
	return plaintext, nil
}

func releaseNative(out dataBlob) {
	if out.data == nil {
		return
	}
	secureZeroNative(out.data, uintptr(out.size))
	localFreeNative(out.data)
}
