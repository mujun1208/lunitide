package compactionapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ProtectedFact is an exact identifier, value, or quotation extracted from
// source messages that must be preserved verbatim in a compaction summary.
type ProtectedFact struct {
	Value string
	Kind  string // "ulid", "path", "code", "quote"
}

// ErrProtectedFactsViolation is returned when the summary does not preserve
// all protected facts extracted from the source message range.
var ErrProtectedFactsViolation = errors.New("protected facts not preserved in summary")

// ulidPattern matches canonical 26-character ULIDs (Crockford base32, first char 0-7).
var ulidPattern = regexp.MustCompile(`\b[0-7][0-9A-HJKMNP-TV-Z]{25}\b`)

// pathPattern matches common file paths (Windows and POSIX).
var pathPattern = regexp.MustCompile(`(?:[a-zA-Z]:[\\/]+|[\\/])[\w.\\/-]+`)

// codePattern matches content inside backticks.
var codePattern = regexp.MustCompile("`([^`]+)`")

// quotePattern matches content inside double quotes (at least 4 chars to avoid noise).
var quotePattern = regexp.MustCompile(`"([^"]{4,})"`)

// maxProtectedFacts bounds the number of extracted facts to prevent runaway
// extraction on adversarial inputs.
const maxProtectedFacts = 200

// ExtractProtectedFacts extracts exact identifiers, values, and quotations
// from source messages that must be preserved verbatim in the summary.
// Extraction is deterministic and ordered by first appearance.
func ExtractProtectedFacts(messages []SummaryMessage) []ProtectedFact {
	seen := make(map[string]bool)
	var facts []ProtectedFact
	add := func(value, kind string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || len(facts) >= maxProtectedFacts {
			return
		}
		seen[value] = true
		facts = append(facts, ProtectedFact{Value: value, Kind: kind})
	}
	for _, msg := range messages {
		text := msg.Content
		for _, m := range ulidPattern.FindAllString(text, -1) {
			add(m, "ulid")
		}
		for _, m := range pathPattern.FindAllString(text, -1) {
			add(m, "path")
		}
		for _, m := range codePattern.FindAllStringSubmatch(text, -1) {
			add(m[1], "code")
		}
		for _, m := range quotePattern.FindAllStringSubmatch(text, -1) {
			add(m[1], "quote")
		}
	}
	return facts
}

// ValidateProtectedFacts checks that all protected facts appear as substrings
// in at least one string value within the summary JSON. It returns
// ErrProtectedFactsViolation listing the missing facts when validation fails.
func ValidateProtectedFacts(summaryJSON string, facts []ProtectedFact) error {
	if len(facts) == 0 {
		return nil
	}
	// Extract all string values from the summary JSON.
	summaryStrings, err := extractStringValues(summaryJSON)
	if err != nil {
		return fmt.Errorf("protected facts validation: summary json parse: %w", err)
	}
	joined := strings.Join(summaryStrings, "\n")
	var missing []string
	for _, f := range facts {
		if !strings.Contains(joined, f.Value) {
			missing = append(missing, f.Value)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: %d missing (first: %s)", ErrProtectedFactsViolation, len(missing), truncate(missing[0], 64))
	}
	return nil
}

// extractStringValues recursively extracts all string values from a JSON document.
func extractStringValues(raw string) ([]string, error) {
	var v any
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var result []string
	walkStrings(v, &result)
	return result, nil
}

func walkStrings(v any, out *[]string) {
	switch t := v.(type) {
	case string:
		*out = append(*out, t)
	case []any:
		for _, e := range t {
			walkStrings(e, out)
		}
	case map[string]any:
		for _, e := range t {
			walkStrings(e, out)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
