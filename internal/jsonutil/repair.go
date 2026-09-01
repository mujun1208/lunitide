package jsonutil

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

// Repair extracts a JSON value from model output that is often wrapped in
// markdown fences, prefixed with prose, or carrying a trailing comma.
// If nothing salvageable remains, the original bytes are returned.
func Repair(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return raw
	}
	s = stripFence(s)
	s = extractJSON(s)
	s = stripTrailingCommas(s)
	if json.Valid([]byte(s)) {
		return []byte(s)
	}
	return raw
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		lang := strings.TrimSpace(s[:i])
		if lang == "" || strings.EqualFold(lang, "json") || strings.EqualFold(lang, "jsonc") {
			s = s[i+1:]
		}
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	obj := strings.IndexByte(s, '{')
	arr := strings.IndexByte(s, '[')
	var (
		start int
		endCh byte
	)
	switch {
	case obj >= 0 && (arr < 0 || obj < arr):
		start, endCh = obj, '}'
	case arr >= 0:
		start, endCh = arr, ']'
	default:
		return s
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 && c == endCh {
				return strings.TrimSpace(s[start : i+1])
			}
		}
	}
	return strings.TrimSpace(s[start:])
}

func stripTrailingCommas(s string) string {
	var b bytes.Buffer
	b.Grow(len(s))
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && unicode.IsSpace(rune(s[j])) {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
