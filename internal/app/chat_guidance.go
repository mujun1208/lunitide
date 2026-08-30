package app

import (
	"crypto/sha256"
	"encoding/hex"
	"log"

	"github.com/lunitide/lunitide/internal/gateway"
)

func injectedGuidanceDigest(req gateway.Request) (systemBytes int, sha8 string, toolCount int) {
	h := sha256.New()
	for _, m := range req.Messages {
		if m.Role != gateway.RoleSystem {
			continue
		}
		systemBytes += len(m.Content)
		_, _ = h.Write([]byte(m.Content))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return systemBytes, hex.EncodeToString(sum[:8]), len(req.Tools)
}

func logInjectedGuidance(sessionID string, companion bool, req gateway.Request) {
	n, digest, tools := injectedGuidanceDigest(req)
	log.Printf("chat.guidance session=%s companion=%v system_bytes=%d sha256_8=%s tools=%d", sessionID, companion, n, digest, tools)
}
