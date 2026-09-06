package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
)

// appendAssistantTurn keeps a full reply recoverable within the existing
// per-message limits. The first key stays compatible with previous releases;
// later parts have deterministic keys, so an uncertain partial commit replays.
func (e *Engine) appendAssistantTurn(ctx context.Context, turnID, actor, sessionID, text string, usage messageapp.AssistantUsage) (message.Message, error) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') || text == "" {
		return message.Message{}, messageapp.ErrAssistantResponseTooLarge
	}
	payload, _ := json.Marshal(map[string]string{"sessionId": sessionID})
	release, scopeErr := e.authorizeDataRequest(ctx, "chat.persist", payload)
	if scopeErr != nil {
		return message.Message{}, scopeErr
	}
	defer release()
	var last message.Message
	for part := 0; len(text) > 0; part++ {
		end, count := 0, 0
		for offset, r := range text {
			next := offset + utf8.RuneLen(r)
			if count == message.MaxRunesAssistant || next > message.MaxBytesAssistant {
				break
			}
			end = next
			count++
		}
		key := turnID
		partUsage := usage
		if part > 0 {
			key = fmt.Sprintf("%s:part:%d", turnID, part)
			partUsage = messageapp.AssistantUsage{}
		}
		saved, err := e.messages.AppendAssistant(ctx, key, actor, sessionID, text[:end], partUsage)
		if err != nil {
			return last, err
		}
		last = saved
		text = text[end:]
	}
	return last, nil
}
