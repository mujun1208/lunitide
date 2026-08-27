package meetings

import (
	"encoding/json"
	"strings"
)

func parseJSONNotes(raw string) (Notes, bool) {
	trimmed := strings.TrimSpace(raw)
	if i := strings.Index(trimmed, "```"); i >= 0 {
		rest := trimmed[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		rest = strings.TrimPrefix(rest, "JSON")
		if end := strings.Index(rest, "```"); end >= 0 {
			trimmed = strings.TrimSpace(rest[:end])
		}
	}
	if !strings.HasPrefix(trimmed, "{") {
		if start := strings.Index(trimmed, "{"); start >= 0 {
			if end := strings.LastIndex(trimmed, "}"); end > start {
				trimmed = trimmed[start : end+1]
			}
		}
	}
	var payload struct {
		Title   string      `json:"title"`
		Summary string      `json:"summary"`
		Actions json.RawMessage `json:"actions"`
	}
	if json.Unmarshal([]byte(trimmed), &payload) != nil {
		return Notes{}, false
	}
	if payload.Summary == "" && len(payload.Actions) == 0 {
		return Notes{}, false
	}
	notes := Notes{Title: strings.TrimSpace(payload.Title), Summary: strings.TrimSpace(payload.Summary)}
	notes.Actions = decodeActions(payload.Actions)
	return notes, true
}

func decodeActions(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var items []string
	if json.Unmarshal(raw, &items) == nil {
		var b strings.Builder
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if !strings.HasPrefix(item, "-") {
				item = "- " + item
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(item)
		}
		return b.String()
	}
	return strings.TrimSpace(string(raw))
}

func sectionBetween(raw string, starts, ends []string) string {
	lower := strings.ToLower(raw)
	startAt := -1
	for _, label := range starts {
		idx := indexHeading(lower, strings.ToLower(label))
		if idx >= 0 && (startAt < 0 || idx < startAt) {
			startAt = idx
		}
	}
	if startAt < 0 {
		return ""
	}
	from := skipHeadingLine(raw, startAt)
	endAt := len(raw)
	for _, label := range ends {
		idx := indexHeading(lower[from:], strings.ToLower(label))
		if idx >= 0 {
			abs := from + idx
			if abs < endAt {
				endAt = abs
			}
		}
	}
	return strings.TrimSpace(raw[from:endAt])
}

func indexHeading(lower, label string) int {
	patterns := []string{"## " + label, "# " + label, label + "\n", label + "：", label + ":"}
	best := -1
	for _, p := range patterns {
		if i := strings.Index(lower, p); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

func skipHeadingLine(raw string, at int) int {
	rest := raw[at:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		return at + i + 1
	}
	return at
}
