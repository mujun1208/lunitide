package modelfit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func SealProtocolPrivate(plain, key []byte) ([]byte, string, error) {
	if len(key) != 32 {
		return nil, "", fmt.Errorf("protocol private key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(plain)
	return append(nonce, gcm.Seal(nil, nonce, plain, nil)...), hex.EncodeToString(sum[:]), nil
}

func OpenProtocolPrivate(blob, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("protocol private key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, fmt.Errorf("protocol private blob truncated")
	}
	nonce, cipherText := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	return gcm.Open(nil, nonce, cipherText, nil)
}

func RedactProtocolCapture(in ProtocolCapture) ProtocolCapture {
	in.ReasoningContent = ""
	return in
}
