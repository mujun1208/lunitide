package agenthub

import (
	"encoding/json"
	"strconv"
	"strings"
)

func ParseLine(agent, line string) (AgentEvent, bool) {
	_ = agent
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return AgentEvent{}, false
	}
	var raw map[string]any
	if json.Unmarshal([]byte(line), &raw) != nil {
		return AgentEvent{Type: "message", Title: "输出", Detail: line}, true
	}
	typ := firstString(raw, "type", "event", "kind")
	switch typ {
	case "item.started", "thread.started", "started", "session.created":
		typ = "started"
	case "item.completed", "step", "agent.message":
		typ = "step"
	case "file_write", "file.write", "fs.write", "file":
		typ = "file_write"
	case "token_count", "usage", "token.usage":
		typ = "usage"
	case "error", "failed", "item.failed":
		typ = "failed"
	case "turn.completed", "finished", "done", "result":
		typ = "finished"
	default:
		if typ == "" || !knownEvent(typ) {
			typ = "message"
		}
	}
	ev := AgentEvent{
		Type:   typ,
		Title:  firstString(raw, "title", "name", "item"),
		Detail: firstString(raw, "detail", "message", "text", "content", "delta"),
		Path:   firstString(raw, "path", "file", "filename", "output"),
		Tokens: extractTokens(raw),
	}
	if ev.Title == "" {
		ev.Title = typ
	}
	return ev, true
}

func knownEvent(typ string) bool {
	switch typ {
	case "started", "step", "message", "file_write", "usage", "failed", "finished":
		return true
	}
	return false
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if text := stringFromAny(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return firstString(typed, "text", "message", "content", "path", "title")
	case []any:
		for _, item := range typed {
			if text := stringFromAny(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func extractTokens(raw map[string]any) int64 {
	for _, key := range []string{"tokens", "total_tokens", "token_count"} {
		if n, ok := asNonNeg(raw[key]); ok {
			return n
		}
	}
	if usage, ok := raw["usage"].(map[string]any); ok {
		if n, ok := asNonNeg(usage["total_tokens"]); ok {
			return n
		}
		in, _ := asNonNeg(usage["input_tokens"])
		out, _ := asNonNeg(usage["output_tokens"])
		return in + out
	}
	return 0
}

func asNonNeg(v any) (int64, bool) {
	switch typed := v.(type) {
	case float64:
		if typed < 0 {
			return 0, true
		}
		return int64(typed), true
	case int:
		if typed < 0 {
			return 0, true
		}
		return int64(typed), true
	case json.Number:
		n, err := typed.Int64()
		if err != nil || n < 0 {
			return 0, err == nil
		}
		return n, true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}
