package app

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func injectedGuidanceDigest(req llmadapter.Request) (systemBytes int, sha8 string, toolCount int) {
	h := sha256.New()
	for _, m := range req.Messages {
		if m.Role != llmadapter.RoleSystem {
			continue
		}
		systemBytes += len(m.Content)
		_, _ = h.Write([]byte(m.Content))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return systemBytes, hex.EncodeToString(sum[:8]), len(req.Tools)
}

func logInjectedGuidance(sessionID string, companion bool, req llmadapter.Request) {
	n, digest, tools := injectedGuidanceDigest(req)
	log.Printf("chat.guidance session=%s companion=%v system_bytes=%d sha256_8=%s tools=%d", sessionID, companion, n, digest, tools)
}

func injectedGuidanceLabels(req llmadapter.Request) []string {
	var labels []string
	add := func(label string) {
		for _, existing := range labels {
			if existing == label {
				return
			}
		}
		if len(labels) >= 8 {
			return
		}
		labels = append(labels, label)
	}
	for _, m := range req.Messages {
		if m.Role != llmadapter.RoleSystem {
			continue
		}
		c := m.Content
		if strings.Contains(c, "[内置工作流]") {
			add("工作流")
		}
		if strings.Contains(c, "[身份记忆]") {
			add("身份")
		}
		if strings.Contains(c, "[仓库约定]") || strings.Contains(c, "AGENTS.md") {
			add("AGENTS")
		}
		if strings.Contains(c, "[可用技能目录]") {
			add("技能")
		}
	}
	return labels
}

func emitInjectedGuidance(send func(bridge.Event) error, req llmadapter.Request) {
	labels := injectedGuidanceLabels(req)
	if len(labels) == 0 || send == nil {
		return
	}
	_, digest, _ := injectedGuidanceDigest(req)
	_ = send(bridge.Event{Type: bridge.EventGuidance, Guidance: &bridge.GuidanceEvent{Labels: labels, Digest: digest}})
}
