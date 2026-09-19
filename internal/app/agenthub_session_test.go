package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/agenthub"
)

func TestHubProjectionTextUsesLastHarnessTurn(t *testing.T) {
	text := hubProjectionText(agenthub.ThreadDetail{
		Thread: agenthub.ThreadRecord{HarnessID: "loopback"},
		Messages: []agenthub.ThreadMessage{
			{Role: "user", Content: "写一份纪要"},
			{Role: "assistant", Content: "已完成"},
		},
	})
	if !strings.Contains(text, "loopback") || !strings.Contains(text, "写一份纪要") || !strings.Contains(text, "已完成") {
		t.Fatalf("projection=%q", text)
	}
}

func TestHubProjectionTextEmptyWithoutMessages(t *testing.T) {
	if hubProjectionText(agenthub.ThreadDetail{}) != "" {
		t.Fatal("empty detail must not invent a card")
	}
}
