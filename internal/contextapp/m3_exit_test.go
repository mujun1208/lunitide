package contextapp

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestM3ExitWindowMatrixWithMillionTokenLogicalHistory(t *testing.T) {
	const messageTokens int64 = 1000
	messages := make([]Message, 1001)
	for i := range messages {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		messages[i] = Message{
			ID: fmt.Sprintf("m-%04d", i+1), Role: role,
			Content:  strings.Repeat("context ", 500),
			Sequence: int64(i + 1), TokenCount: messageTokens,
		}
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	if int64(len(messages))*messageTokens < 1_000_000 {
		t.Fatal("fixture does not reach one million logical tokens")
	}
	for _, window := range []int64{32 * 1024, 128 * 1024, 200 * 1024} {
		t.Run(fmt.Sprintf("window-%d", window), func(t *testing.T) {
			result, err := AssembleEnvelope(context.Background(), &mockReader{messages: messages}, "million-token-session", ContextEnvelope{
				Provider: ProviderInfo{
					ContextWindow: window, SafetyCeiling: window,
					ReservedOutput: 4096, SafetyMargin: 512,
				},
				MaxMessages: len(messages), RecentUserReserve: 1024,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Trace.UsedTokens > result.Trace.EffectiveBudget {
				t.Fatalf("used %d exceeds effective budget %d", result.Trace.UsedTokens, result.Trace.EffectiveBudget)
			}
			if len(result.Messages) == 0 || result.Messages[len(result.Messages)-1].ID != "m-1001" {
				t.Fatal("latest user turn was not preserved")
			}
		})
	}
}
