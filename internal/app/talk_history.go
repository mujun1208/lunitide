package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/talk"
)

func talkTranscriptKey(session *talkSession, event talk.ServerEvent) (string, error) {
	item := event.ItemID
	if item == "" {
		item = event.ResponseID
	}
	if item == "" {
		item = event.EventID
	}
	if item == "" || len(item) > 512 || event.ContentIndex < 0 || !event.Final || (event.Role != "user" && event.Role != "assistant") {
		return "", errors.New("final transcript has no stable identity")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", session.sessionID, session.talkID, event.Role, item, event.ContentIndex)))
	return "talk-final:" + hex.EncodeToString(sum[:]), nil
}

// Persist only provider final transcripts. The independent deadline means a
// received final can commit even if the socket or page closes in the meantime.
// The same key retries an ambiguous commit without adding another message.
func (e *Engine) persistTalkTranscript(session *talkSession, event talk.ServerEvent, parents ...context.Context) (message.Message, error) {
	key, err := talkTranscriptKey(session, event)
	if err != nil {
		return message.Message{}, err
	}
	if !messageServiceAvailable(e.messages) {
		return message.Message{}, errors.New("message storage unavailable")
	}
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(event.Transcript, "\r\n", "\n"), "\r", "\n"))
	if text == "" || !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
		return message.Message{}, errors.New("invalid final transcript")
	}
	parent := context.Background()
	if len(parents) > 0 && parents[0] != nil {
		parent = parents[0]
	}
	scoped, release, err := e.AcquireCapability(parent, "llm", "stt", "tts", "session")
	if err != nil {
		return message.Message{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(scoped, 5*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"sessionId": session.sessionID})
	releaseData, scopeErr := e.authorizeDataRequest(ctx, "talk.persist", payload)
	if scopeErr != nil {
		return message.Message{}, scopeErr
	}
	defer releaseData()
	// Recheck the current session/project before a write. A stale talk socket
	// must not keep writing after its project was archived or session removed.
	projectID, ok, err := projectIDForSession(e, ctx, session.sessionID)
	if err != nil {
		return message.Message{}, err
	}
	if !ok || !projectServiceAvailable(e.projects) {
		return message.Message{}, errors.New("session scope unavailable")
	}
	project, err := e.projects.Get(ctx, projectID)
	if err != nil {
		return message.Message{}, err
	}
	if !project.CanEditMutableFields() {
		return message.Message{}, errors.New("session project is read only")
	}
	// This is a persisted part-key protocol, independent of the typed input
	// limit. Changing it would invalidate a pre-upgrade final's retry receipt.
	limitRunes, limitBytes := 2048, 8192
	if event.Role == "assistant" {
		limitRunes, limitBytes = message.MaxRunesAssistant, message.MaxBytesAssistant
	}
	parts := splitTalkTranscript(text, limitRunes, limitBytes)
	digest := ""
	if len(parts) > 1 {
		sum := sha256.Sum256([]byte(text))
		digest = hex.EncodeToString(sum[:])
	}
	var first message.Message
	for part, content := range parts {
		partKey := key
		if part > 0 {
			partKey = fmt.Sprintf("%s:part:%d", key, part)
		}
		var saved message.Message
		for attempt := 0; attempt < 2; attempt++ {
			if event.Role == "assistant" {
				usage := messageapp.AssistantUsage{TranscriptDigest: digest}
				if part == 0 {
					usage.Provider = session.providerProtocol
					usage.Model = session.modelID
				}
				saved, err = e.messages.AppendAssistant(ctx, partKey, "talk", session.sessionID, content, usage)
			} else {
				request := struct {
					SessionID        string `json:"sessionId"`
					Text             string `json:"text"`
					TranscriptDigest string `json:"transcriptDigest,omitempty"`
				}{session.sessionID, content, digest}
				saved, err = e.messages.Append(ctx, partKey, "talk", request, message.Message{SessionID: session.sessionID, Text: content})
			}
			if err == nil || ctx.Err() != nil || errors.Is(err, messageapp.ErrIdempotencyConflict) {
				break
			}
		}
		if err != nil {
			return first, err
		}
		if part == 0 {
			first = saved
		}
	}
	return first, nil
}

func splitTalkTranscript(text string, maxRunes, maxBytes int) []string {
	var parts []string
	for text != "" {
		end, count := 0, 0
		for offset, r := range text {
			next := offset + utf8.RuneLen(r)
			if count == maxRunes || next > maxBytes {
				break
			}
			end = next
			count++
		}
		parts = append(parts, text[:end])
		text = text[end:]
	}
	return parts
}
