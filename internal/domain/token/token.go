package token

import (
	"errors"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

// EstimationMethod names the token-counting approach.
type EstimationMethod string

const (
	CharRatio      EstimationMethod = "char-ratio"
	TikToken       EstimationMethod = "tiktoken"
	ProviderReport EstimationMethod = "provider-reported"
	Manual         EstimationMethod = "manual"
)

// LedgerEntry records a token-count estimate for one message with a given
// provider/model/tokenizer tuple. Counts are caches, not message truth.
type LedgerEntry struct {
	ID                string           `json:"id"`
	MessageID         string           `json:"messageId"`
	Provider          string           `json:"provider"`
	Model             string           `json:"model"`
	TokenizerRevision string           `json:"tokenizerRevision"`
	TokenCount        int64            `json:"tokenCount"`
	EstimationMethod  EstimationMethod `json:"estimationMethod"`
	UTF8Bytes         int64            `json:"utf8Bytes"`
	ComputedAt        time.Time        `json:"computedAt"`
}

// Validate checks invariants for a ledger entry.
func (e LedgerEntry) Validate() error {
	if !canonicalULID(e.ID) || !canonicalULID(e.MessageID) {
		return errors.New("token ledger entry id or message_id is not a canonical ULID")
	}
	if len(e.Provider) > 128 || len(e.Model) > 128 || len(e.TokenizerRevision) > 64 {
		return errors.New("token ledger provider/model/revision exceed limits")
	}
	if e.TokenCount < 0 || e.UTF8Bytes < 0 {
		return errors.New("token ledger count/bytes must be non-negative")
	}
	switch e.EstimationMethod {
	case CharRatio, TikToken, ProviderReport, Manual:
	default:
		return errors.New("token ledger estimation method invalid")
	}
	if e.ComputedAt.IsZero() || e.ComputedAt.Location() != time.UTC {
		return errors.New("token ledger computed_at must be UTC")
	}
	return nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

// EstimateTokens returns a conservative token count for the given UTF-8 text.
// The estimate is (byteLen + 3) / 4 — roughly 4 bytes per token for English,
// which is conservative for CJK and other scripts. This is a fallback when no
// tokenizer is available.
func EstimateTokens(text string) int64 {
	if len(text) == 0 {
		return 0
	}
	if !utf8.ValidString(text) {
		// Fallback: count bytes
		return max(1, int64(len(text)+3)/4)
	}
	// For valid UTF-8, use a character-aware estimate:
	// CJK characters (3-byte UTF-8) are ~1.5-2 chars per token, so ~1.5 bytes per token.
	// ASCII is ~4 chars per token, so ~4 bytes per token.
	// We use a weighted byte count: count CJK bytes at 0.5 weight, others at 0.25.
	var weighted int64
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			weighted += 1
		} else if isCJK(r) {
			weighted += int64(size) / 2
		} else {
			weighted += int64(size) / 4
		}
		if weighted < 1 {
			weighted = 1
		}
		i += size
	}
	return max(1, weighted)
}

// isCJK returns true for common CJK unified ideographs and extensions.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Unified Ideographs Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Unified Ideographs Extension B
		(r >= 0x2A700 && r <= 0x2B73F) || // CJK Unified Ideographs Extension C
		(r >= 0x2B740 && r <= 0x2B81F) || // CJK Unified Ideographs Extension D
		(r >= 0x2B820 && r <= 0x2CEAF) || // CJK Unified Ideographs Extension E
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0x2F800 && r <= 0x2FA1F) // CJK Compatibility Ideographs Supplement
}

// max returns the larger of two int64 values.
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}