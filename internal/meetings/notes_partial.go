package meetings

import (
	"context"
	"encoding/json"
	"strings"
)

// InterimPublisher hands a partial document to whoever owns the write. Only the
// meetings Service implements it: the final save uses compare-and-swap on
// updatedAt, so an interim write from anywhere else would make that swap conflict
// and the finished notes would be rejected as a concurrent edit.
type InterimPublisher func(Notes)

type interimPublisherKey struct{}

func WithInterimPublisher(ctx context.Context, fn InterimPublisher) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, interimPublisherKey{}, fn)
}

// WithoutInterimPublisher silences interim publishing for one nested call. A long
// meeting is summarized segment by segment, and one segment's stream only knows
// its own slice: publishing it would replace the segments already on screen with
// less text, so the reader watches the document shrink. The segmented path
// publishes the accumulated stitch between segments instead.
func WithoutInterimPublisher(ctx context.Context) context.Context {
	return context.WithValue(ctx, interimPublisherKey{}, InterimPublisher(nil))
}

// InterimPublisherFrom returns a no-op when nothing is listening, so callers can
// publish unconditionally.
func InterimPublisherFrom(ctx context.Context) InterimPublisher {
	if fn, ok := ctx.Value(interimPublisherKey{}).(InterimPublisher); ok && fn != nil {
		return fn
	}
	return func(Notes) {}
}

// Notes used to arrive as one blocking response: the reader watched a spinner for
// the entire generation, which reads as 卡顿 even when the model is healthy. The
// stream carries finished topics long before the closing brace, so we close the
// truncated JSON ourselves and hand back whatever is already readable.
//
// ParsePartialNotes reports how many topics are complete so a caller can publish
// only when the document actually grew, instead of on every delta.
func ParsePartialNotes(raw, fallbackTitle string) (Notes, int, bool) {
	body := jsonBody(raw)
	if body == "" {
		return Notes{}, 0, false
	}
	// A truncation can land mid-number, mid-escape or inside a literal, which no
	// amount of closing braces repairs. Each retry drops the broken tail back to
	// the previous finished object and tries again.
	for attempt := 0; attempt < 4 && body != ""; attempt++ {
		var payload notesPayload
		if json.Unmarshal([]byte(closeOpenJSON(body)), &payload) == nil {
			return partialNotesFrom(payload, fallbackTitle)
		}
		cut := lastUnquotedIndex(body, '}')
		if cut < 0 {
			return Notes{}, 0, false
		}
		body = body[:cut]
	}
	return Notes{}, 0, false
}

func partialNotesFrom(payload notesPayload, fallbackTitle string) (Notes, int, bool) {
	ready := 0
	for _, topic := range payload.Topics {
		if strings.TrimSpace(topic.Heading) != "" {
			ready++
		}
	}
	if ready == 0 {
		// A title alone is not a document. Publishing it would replace the
		// spinner with an empty page, which looks more broken, not less.
		return Notes{}, 0, false
	}
	clampNotesPayload(&payload)
	summary := composeStructuredSummary(payload)
	if strings.TrimSpace(summary) == "" {
		return Notes{}, 0, false
	}
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = fallbackTitle
	}
	return Notes{Title: title, Summary: summary, Actions: decodeActions(payload.Actions)}, ready, true
}

// jsonBody finds the object in a stream prefix that may still be inside a
// Markdown fence and has no closing fence yet.
func jsonBody(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if i := strings.Index(trimmed, "```"); i >= 0 {
		rest := trimmed[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		rest = strings.TrimPrefix(rest, "JSON")
		rest = strings.TrimPrefix(rest, "\n")
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		trimmed = strings.TrimSpace(rest)
	}
	start := strings.Index(trimmed, "{")
	if start < 0 {
		return ""
	}
	return trimmed[start:]
}

// closeOpenJSON terminates an open string and every open array or object, and
// drops a dangling comma or key so the result parses.
func closeOpenJSON(s string) string {
	var stack []byte
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, c)
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	var b strings.Builder
	b.WriteString(s)
	if escaped {
		// A trailing backslash would escape the quote we are about to write.
		b.WriteByte('\\')
	}
	if inString {
		b.WriteByte('"')
	}
	out := b.String()
	if !inString {
		out = trimDanglingMember(out)
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			out += "}"
		} else {
			out += "]"
		}
	}
	return out
}

// trimDanglingMember removes a comma, colon or bare key left hanging by the cut,
// e.g. `{"a":1,` or `{"a":1,"b":`.
func trimDanglingMember(s string) string {
	for {
		trimmed := strings.TrimRight(s, " \n\r\t")
		if trimmed == "" {
			return trimmed
		}
		switch trimmed[len(trimmed)-1] {
		case ',':
			s = trimmed[:len(trimmed)-1]
			continue
		case ':':
			// Drop the colon and the key it belonged to.
			body := strings.TrimRight(trimmed[:len(trimmed)-1], " \n\r\t")
			key := lastUnquotedIndex(body, '"')
			if key < 0 {
				return body
			}
			s = body[:key]
			continue
		}
		return trimmed
	}
}

// lastUnquotedIndex finds the last occurrence of target that is not inside a
// JSON string. For '"' it returns the opening quote of the final string.
func lastUnquotedIndex(s string, target byte) int {
	found, openQuote := -1, -1
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
				if target == '"' {
					found = openQuote
				}
			}
			continue
		}
		if c == '"' {
			inString, openQuote = true, i
			continue
		}
		if c == target {
			found = i
		}
	}
	return found
}
