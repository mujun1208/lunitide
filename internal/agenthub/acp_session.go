package agenthub

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func acpAuthMethodID(init *acpRPC) string {
	if init == nil || len(init.Result) == 0 {
		return ""
	}
	var payload struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if json.Unmarshal(init.Result, &payload) != nil {
		return ""
	}
	preferred := ""
	for _, method := range payload.AuthMethods {
		id := strings.TrimSpace(method.ID)
		if id == "" {
			continue
		}
		low := strings.ToLower(id)
		if strings.Contains(low, "cursor") || strings.Contains(low, "login") || strings.Contains(low, "agent") {
			return id
		}
		if preferred == "" {
			preferred = id
		}
	}
	return preferred
}

func acpAuthenticateIfNeeded(call func(string, any) (*acpRPC, error), init *acpRPC) error {
	methodID := acpAuthMethodID(init)
	if methodID == "" {
		return nil
	}
	_, err := call("authenticate", map[string]any{"methodId": methodID})
	return err
}

func keepNativeID(returned, stored string) string {
	if id := strings.TrimSpace(returned); id != "" {
		return id
	}
	return strings.TrimSpace(stored)
}

func beginAssistantCapture(buf *strings.Builder, capture *bool) {
	*capture = true
	buf.Reset()
}

func takeAssistantCapture(buf *strings.Builder, capture *bool) string {
	*capture = false
	text := buf.String()
	buf.Reset()
	return text
}

func pushAssistantChunk(buf *strings.Builder, capture *bool, chunk string) {
	if chunk == "" || capture == nil || !*capture {
		return
	}
	next := absorbAssistantChunk(buf.String(), chunk)
	buf.Reset()
	buf.WriteString(next)
}

func absorbAssistantChunk(have, chunk string) string {
	if have == "" || chunk == "" {
		if chunk == "" {
			return have
		}
		return chunk
	}
	if strings.HasPrefix(chunk, have) || strings.HasPrefix(have, chunk) {
		if len(chunk) >= len(have) {
			return chunk
		}
		return have
	}
	return have + chunk
}

func stripCarriedTurn(store *ThreadStore, threadID, next string) string {
	text := strings.TrimSpace(next)
	if text == "" || store == nil {
		return text
	}
	msgs, err := store.ListMessages(threadID)
	if err != nil {
		return text
	}
	var prevAssistant, prevUser string
	seenCurrentUser := false
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].Role {
		case "user":
			if !seenCurrentUser {
				seenCurrentUser = true
				continue
			}
			if prevUser == "" {
				prevUser = strings.TrimSpace(msgs[i].Content)
			}
		case "assistant":
			if prevAssistant == "" {
				prevAssistant = strings.TrimSpace(msgs[i].Content)
			}
		}
		if prevAssistant != "" && prevUser != "" {
			break
		}
	}
	return stripCarriedPrefix(prevUser, prevAssistant, text)
}

func stripCarriedPrefix(prevUser, prevAssistant, text string) string {
	var prefixes []string
	if prevUser != "" && prevAssistant != "" {
		prefixes = append(prefixes,
			prevUser+"\n"+prevAssistant,
			prevUser+"\n\n"+prevAssistant,
			prevAssistant+"\n"+prevUser,
			prevAssistant+"\n\n"+prevUser,
		)
	}
	if prevAssistant != "" {
		prefixes = append(prefixes, prevAssistant)
	}
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || utf8.RuneCountInString(prefix) < 40 || len(prefix) >= len(text) || !strings.HasPrefix(text, prefix) {
			continue
		}
		rest := strings.TrimSpace(text[len(prefix):])
		if rest != "" {
			return rest
		}
	}
	return text
}

func acpOpenNativeSession(call func(string, any) (*acpRPC, error), cwd, nativeID string) (string, error) {
	params := map[string]any{"cwd": cwd, "mcpServers": []any{}}
	var resp *acpRPC
	var err error
	usedLoad := false
	if nativeID != "" {
		load := map[string]any{"cwd": cwd, "mcpServers": []any{}, "sessionId": nativeID}
		resp, err = call("session/load", load)
		if err != nil {
			resp, err = call("session/new", params)
		} else {
			usedLoad = true
		}
	} else {
		resp, err = call("session/new", params)
	}
	if err != nil {
		return "", err
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if resp != nil {
		_ = json.Unmarshal(resp.Result, &created)
	}
	if usedLoad {
		return keepNativeID(created.SessionID, nativeID), nil
	}
	if created.SessionID == "" {
		return "", fmt.Errorf("session/new 未返回 sessionId")
	}
	return created.SessionID, nil
}
