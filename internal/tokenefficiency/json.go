// Package tokenefficiency contains deterministic, lossless prompt reductions.
// It never summarizes evidence, selects a different model, caches answers, or
// changes an output budget. Callers retain their existing ownership boundaries.
package tokenefficiency

import (
	"bytes"
	"encoding/json"
	"strings"
)

// CompactToolJSON removes only insignificant structural JSON whitespace.
// Do not unmarshal/remarshal: that can round large integers, reorder keys, lose
// duplicate keys, or change literal strings (including code and quotations).
// Text logs, partial JSON and scalar JSON remain byte-identical. Oversized
// inputs bypass the optimization; they are never cut to the limit.
func CompactToolJSON(content string) string {
	if len(content) > 1<<20 {
		return content
	}
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return content
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(content)); err != nil || compact.Len() >= len(content) {
		return content
	}
	return compact.String()
}
