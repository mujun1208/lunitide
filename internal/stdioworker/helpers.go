package stdioworker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ErrQuotaPolicy: quotas outside the frozen policy ceilings (M6-SBX-003).
var ErrQuotaPolicy = errors.New("stdioworker: quota outside frozen policy ceiling")

// digestBytes hex-hashes a byte slice (policy/spec digest helper).
func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
