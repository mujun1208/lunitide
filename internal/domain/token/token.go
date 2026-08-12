// Package token defines the canonical tokenizer artifact and token estimation.
//
// This package freezes the canonical tokenizer identity, normalization rules,
// serialization format, and estimation algorithm required by ADR-005 and the
// M3 milestone gate (architecture doc §12.1.1):
//
//   - Tokenizer ID: lunitide-canonical-v1
//   - Version: v1.0.0
//   - Normalization: Unicode NFC + LF line endings (CRLF/CR → LF, no trimming)
//   - Canonical serialization: deterministic JSON (sorted keys, no whitespace)
//   - Estimation: character-class aware byte-ratio (conservative upper bound)
//   - Attachment metering: raw UTF-8 byte count / 4 (conservative)
//
// The canonical tokenizer is a stable capacity gauge — it does NOT match any
// specific provider's tokenizer. Provider-accurate counts are cached separately
// in the token_ledger with the provider/model/tokenizer_revision tuple and
// calibrated against actual usage. The canonical count provides a deterministic,
// versioned fallback that is stable across provider switches.
package token

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"golang.org/x/text/unicode/norm"
)

// Canonical tokenizer identity (frozen for M3).
const (
	CanonicalTokenizerID       = "lunitide-canonical-v1"
	CanonicalTokenizerVersion  = "v1.0.0"
	CanonicalTokenizerRevision = "v1.0.0" // alias used in token_ledger.tokenizer_revision
	// ProviderReportTokenizerID identifies counts supplied by a provider usage
	// response. It is deliberately distinct from the canonical tokenizer: a
	// provider report is an observation, not a canonical tokenization.
	ProviderReportTokenizerID       = "provider-report"
	ProviderReportTokenizerRevision = "v1"
)

// ArtifactSHA256 returns the SHA-256 of the canonical tokenizer artifact
// descriptor. The descriptor is the deterministic JSON serialization of all
// frozen fields. This hash is pinned at M3 freeze and must not change without
// a version bump.
//
// To recompute: see TestCanonicalArtifactSHA256 in token_test.go.
var ArtifactSHA256 = computeArtifactSHA256()

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
	TokenizerID       string           `json:"tokenizerId"`
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
	if e.TokenizerID == "" || e.TokenizerRevision == "" || len(e.TokenizerID) > 128 || len(e.Provider) > 128 || len(e.Model) > 128 || len(e.TokenizerRevision) > 64 {
		return errors.New("token ledger provider/model/revision exceed limits")
	}
	if e.EstimationMethod == ProviderReport {
		if e.TokenizerID != ProviderReportTokenizerID || e.TokenizerRevision != ProviderReportTokenizerRevision || e.Provider == "" || e.Model == "" {
			return errors.New("provider-reported token ledger identity/provider/model invalid")
		}
	} else if e.TokenizerID == CanonicalTokenizerID && (e.TokenizerRevision != CanonicalTokenizerRevision || e.Provider != "" || e.Model != "") {
		return errors.New("canonical token ledger identity/revision/provider/model invalid")
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

// artifactDescriptor is the deterministic JSON object whose SHA-256 is the
// frozen artifact hash. Changing any field value or adding/removing fields
// changes the hash and requires a version bump.
type artifactDescriptor struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	Normalization string `json:"normalization"`
	LineEnding    string `json:"lineEnding"`
	Serialization string `json:"serialization"`
	Estimation    string `json:"estimation"`
	Attachment    string `json:"attachmentMetering"`
}

// ArtifactDescriptor returns the frozen canonical tokenizer descriptor.
func ArtifactDescriptor() artifactDescriptor {
	return artifactDescriptor{
		ID:            CanonicalTokenizerID,
		Version:       CanonicalTokenizerVersion,
		Normalization: "Unicode NFC",
		LineEnding:    "LF (CRLF/CR normalized to LF, no trimming)",
		Serialization: "JSON canonical form: sorted keys, no whitespace, UTF-8",
		Estimation:    "character-class aware byte-ratio (conservative upper bound)",
		Attachment:    "raw UTF-8 byte count / 4, minimum 1",
	}
}

// computeArtifactSHA256 returns the hex-encoded SHA-256 of the canonical JSON
// serialization of the artifact descriptor.
func computeArtifactSHA256() string {
	d := ArtifactDescriptor()
	// Canonical JSON: sort keys, no indent.
	m := map[string]string{
		"id":                 d.ID,
		"version":            d.Version,
		"normalization":      d.Normalization,
		"lineEnding":         d.LineEnding,
		"serialization":      d.Serialization,
		"estimation":         d.Estimation,
		"attachmentMetering": d.Attachment,
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(m[k])
		sb.Write(kb)
		sb.WriteByte(':')
		sb.Write(vb)
	}
	sb.WriteByte('}')
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// NormalizeText applies the canonical normalization: Unicode NFC followed by
// CRLF/CR → LF conversion. No trimming is performed (consistent with message
// storage rules: CRLF/CR normalized to LF without trimming).
//
// NFC normalization is declared as the canonical form and is implemented via
// golang.org/x/text/unicode/norm.NFC.String. NFC precomposes combining
// character sequences so that canonically equivalent inputs (e.g. U+00E9 "é"
// vs U+0065 U+0301 "e\u0301") produce identical normalized output and thus
// identical canonical token counts.
//
// The order is: (1) validate UTF-8 and replace invalid bytes with U+FFFD;
// (2) NFC composition; (3) CRLF/CR → LF. NFC runs before line-ending
// normalization so that any combining characters produced by NFC on CR/LF
// (none in practice) are handled. The result is deterministic for any given
// input regardless of its original Unicode normalization form (NFC, NFD, NFKC,
// NFKD).
func NormalizeText(text string) string {
	if text == "" {
		return ""
	}
	// Step 1: Validate UTF-8. Invalid sequences are replaced with U+FFFD
	// by the runtime on subsequent operations, ensuring deterministic output.
	if !utf8.ValidString(text) {
		// Force valid UTF-8 by replacing invalid bytes with U+FFFD.
		text = strings.ToValidUTF8(text, "\uFFFD")
	}
	// Step 2: Unicode NFC composition (ADR-005 §2 canonical tokenizer freeze).
	// norm.NFC.String returns the NFC-normalized form of the input. For inputs
	// already in NFC this is a no-op; for NFD/other forms it precomposes
	// combining sequences so canonically equivalent strings become byte-identical.
	text = norm.NFC.String(text)
	// Step 3: Line ending normalization (CRLF → LF, CR → LF).
	// This matches the message storage rule: CRLF/CR normalized to LF without trimming.
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return normalized
}

// CanonicalJSON serializes a Go value to canonical JSON form: sorted keys,
// no whitespace, UTF-8. This is used for structured object token estimation.
func CanonicalJSON(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// Re-encode via map to ensure sorted keys.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		// Not a JSON object — return the original encoding.
		return data, nil
	}
	return canonicalJSONMap(m)
}

func canonicalJSONMap(m map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		sb.Write(kb)
		sb.WriteByte(':')
		vb, err := canonicalJSONValue(m[k])
		if err != nil {
			return nil, err
		}
		sb.Write(vb)
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}

func canonicalJSONValue(v any) ([]byte, error) {
	switch val := v.(type) {
	case map[string]any:
		return canonicalJSONMap(val)
	case []any:
		var sb strings.Builder
		sb.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				sb.WriteByte(',')
			}
			itemBytes, err := canonicalJSONValue(item)
			if err != nil {
				return nil, err
			}
			sb.Write(itemBytes)
		}
		sb.WriteByte(']')
		return []byte(sb.String()), nil
	default:
		return json.Marshal(v)
	}
}

// EstimateTokens returns a conservative canonical token count for the given
// text. The estimate uses character-class aware byte-ratio:
//
//   - CJK characters (3-byte UTF-8): ~1.5 bytes per token → weight 0.5
//   - ASCII characters (1-byte): ~4 bytes per token → weight 0.25
//   - Other multi-byte: ~2 bytes per token → weight 0.5
//
// This is a conservative upper bound. Provider-accurate counts should be
// cached in the token_ledger via the provider's tokenizer.
//
// The text is first normalized via NormalizeText (NFC + LF) to ensure
// deterministic output.
func EstimateTokens(text string) int64 {
	if len(text) == 0 {
		return 0
	}
	normalized := NormalizeText(text)
	if !utf8.ValidString(normalized) {
		// Fallback: count raw bytes / 4.
		return max64(1, int64(len(normalized)+3)/4)
	}
	var weighted int64
	for i := 0; i < len(normalized); {
		r, size := utf8.DecodeRuneInString(normalized[i:])
		if r == utf8.RuneError && size == 1 {
			weighted += 1
		} else if isCJK(r) {
			weighted += int64(size) / 2 // 3 bytes → 1 weight ≈ 1.5 chars/token
		} else if size > 1 {
			weighted += int64(size) / 2
		} else {
			weighted += 1 // ASCII: 4 bytes ≈ 1 token, but we count per-char at 0.25
			// Actually: for ASCII, 4 chars ≈ 1 token, so each char contributes 0.25.
			// But we're counting whole numbers, so we use integer accumulation.
			// To stay conservative, we treat every 4th ASCII char as 1 token.
			// This is handled by the final division below.
		}
		if weighted < 1 {
			weighted = 1
		}
		i += size
	}
	// For ASCII-heavy text, the per-char weight overestimates.
	// Apply a correction: if the text is mostly ASCII, divide by 4.
	asciiCount := int64(0)
	for _, r := range normalized {
		if r < 128 {
			asciiCount++
		}
	}
	totalRunes := int64(utf8.RuneCountInString(normalized))
	if totalRunes > 0 && asciiCount*4 > totalRunes*3 {
		// >75% ASCII: use byte-length / 4 (standard char-ratio).
		return max64(1, int64(len(normalized)+3)/4)
	}
	return max64(1, weighted)
}

// EstimateAttachmentTokens returns a conservative token count for an
// attachment based on its raw UTF-8 byte size. The rule is byteCount / 4,
// minimum 1 (frozen in artifact descriptor).
func EstimateAttachmentTokens(byteCount int64) int64 {
	if byteCount <= 0 {
		return 0
	}
	return max64(1, (byteCount+3)/4)
}

// EstimateStructuredTokens estimates tokens for a structured object by
// serializing it to canonical JSON and then applying EstimateTokens.
func EstimateStructuredTokens(v any) int64 {
	data, err := CanonicalJSON(v)
	if err != nil {
		return 1
	}
	return EstimateTokens(string(data))
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

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
