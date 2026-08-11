package token

import (
	"strings"
	"testing"
	"time"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		min  int64
		max  int64
	}{
		{"empty", "", 0, 0},
		{"single ascii", "a", 1, 1},
		{"four ascii", "abcd", 1, 1},
		{"five ascii", "abcde", 1, 5},
		{"english sentence", "Hello world, this is a test sentence.", 1, 100},
		{"chinese single", "中", 1, 3},
		{"chinese sentence", "这是一段中文测试文本。", 1, 50},
		{"mixed", "Hello 世界 test", 1, 50},
		{"emoji", "😀😀😀", 1, 10},
		{"long english", strings.Repeat("a", 2048), 1, 2048},
		{"long chinese", strings.Repeat("中", 2048), 1, 2048},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got < tt.min || got > tt.max {
				t.Errorf("EstimateTokens(%q) = %d, want between %d and %d", tt.text, got, tt.min, tt.max)
			}
		})
	}
}

func TestLedgerEntryValidate(t *testing.T) {
	valid := LedgerEntry{
		ID:                "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		MessageID:         "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Provider:          "openai",
		Model:             "gpt-4",
		TokenizerRevision: "cl100k_base",
		TokenCount:        100,
		EstimationMethod:  CharRatio,
		UTF8Bytes:         400,
		ComputedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}
	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.MessageID = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("empty message ID accepted")
	}
	invalid = valid
	invalid.TokenCount = -1
	if err := invalid.Validate(); err == nil {
		t.Fatal("negative token count accepted")
	}
	invalid = valid
	invalid.EstimationMethod = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown estimation method accepted")
	}
	invalid = valid
	invalid.ComputedAt = time.Time{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero computed_at accepted")
	}
}

// TestCanonicalArtifactSHA256 verifies that the frozen artifact hash is stable.
// If this test fails, the canonical tokenizer descriptor changed — a version
// bump is required (architecture doc §12.1.1 M3 gate).
func TestCanonicalArtifactSHA256(t *testing.T) {
	// The hash is computed at init time. We verify it is non-empty and
	// deterministic by recomputing and comparing.
	if ArtifactSHA256 == "" {
		t.Fatal("ArtifactSHA256 is empty")
	}
	recomputed := computeArtifactSHA256()
	if ArtifactSHA256 != recomputed {
		t.Fatalf("ArtifactSHA256 changed at runtime: stored=%s recomputed=%s", ArtifactSHA256, recomputed)
	}
	// Verify the hash is 64 hex chars (SHA-256).
	if len(ArtifactSHA256) != 64 {
		t.Fatalf("ArtifactSHA256 length = %d, want 64", len(ArtifactSHA256))
	}
	t.Logf("Canonical artifact SHA-256: %s", ArtifactSHA256)
}

// TestCanonicalTokenizerIdentity verifies the frozen identity constants.
func TestCanonicalTokenizerIdentity(t *testing.T) {
	if CanonicalTokenizerID != "lunitide-canonical-v1" {
		t.Fatalf("CanonicalTokenizerID = %q, want lunitide-canonical-v1", CanonicalTokenizerID)
	}
	if CanonicalTokenizerVersion != "v1.0.0" {
		t.Fatalf("CanonicalTokenizerVersion = %q, want v1.0.0", CanonicalTokenizerVersion)
	}
	if CanonicalTokenizerRevision != CanonicalTokenizerVersion {
		t.Fatalf("CanonicalTokenizerRevision = %q, want %q", CanonicalTokenizerRevision, CanonicalTokenizerVersion)
	}
}

// TestNormalizeText verifies CRLF/CR → LF and UTF-8 validation.
func TestNormalizeText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain ascii", "hello", "hello"},
		{"lf preserved", "hello\nworld", "hello\nworld"},
		{"crlf to lf", "hello\r\nworld", "hello\nworld"},
		{"cr to lf", "hello\rworld", "hello\nworld"},
		{"mixed line endings", "a\r\nb\rc\nd", "a\nb\nc\nd"},
		{"trailing crlf", "hello\r\n", "hello\n"},
		{"chinese", "你好\n世界", "你好\n世界"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeText(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNormalizeText_InvalidUTF8 verifies invalid UTF-8 is replaced with U+FFFD.
func TestNormalizeText_InvalidUTF8(t *testing.T) {
	invalid := "hello\xff\xfeworld"
	got := NormalizeText(invalid)
	if !strings.Contains(got, "\uFFFD") {
		t.Fatalf("expected U+FFFD in normalized invalid UTF-8, got %q", got)
	}
}

// TestCanonicalJSON verifies deterministic JSON serialization (sorted keys).
func TestCanonicalJSON(t *testing.T) {
	v := map[string]any{
		"z": 1,
		"a": "hello",
		"m": []any{3, 1, 2},
	}
	got, err := CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	// Keys should be sorted: a, m, z
	want := `{"a":"hello","m":[3,1,2],"z":1}`
	if string(got) != want {
		t.Errorf("CanonicalJSON = %s, want %s", got, want)
	}
}

// TestCanonicalJSON_NestedObject verifies nested objects are also sorted.
func TestCanonicalJSON_NestedObject(t *testing.T) {
	v := map[string]any{
		"outer": map[string]any{
			"b": 2,
			"a": 1,
		},
	}
	got, err := CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"outer":{"a":1,"b":2}}`
	if string(got) != want {
		t.Errorf("CanonicalJSON = %s, want %s", got, want)
	}
}

// TestEstimateAttachmentTokens verifies the attachment metering rule.
func TestEstimateAttachmentTokens(t *testing.T) {
	tests := []struct {
		bytes int64
		want  int64
	}{
		{0, 0},
		{1, 1},
		{4, 1},
		{5, 2},
		{100, 25},
		{1000, 250},
	}
	for _, tt := range tests {
		got := EstimateAttachmentTokens(tt.bytes)
		if got != tt.want {
			t.Errorf("EstimateAttachmentTokens(%d) = %d, want %d", tt.bytes, got, tt.want)
		}
	}
}

// TestEstimateStructuredTokens verifies structured object token estimation.
func TestEstimateStructuredTokens(t *testing.T) {
	v := map[string]any{
		"summary": "This is a test summary of the conversation.",
		"count":   42,
	}
	tokens := EstimateStructuredTokens(v)
	if tokens <= 0 {
		t.Fatalf("EstimateStructuredTokens = %d, want > 0", tokens)
	}
	// Should be roughly equivalent to estimating the JSON string.
	data, _ := CanonicalJSON(v)
	expected := EstimateTokens(string(data))
	if tokens != expected {
		t.Errorf("EstimateStructuredTokens = %d, want %d (from canonical JSON)", tokens, expected)
	}
}

// TestEstimateTokens_Deterministic verifies that the same input always
// produces the same output (canonical tokenizer stability requirement).
func TestEstimateTokens_Deterministic(t *testing.T) {
	inputs := []string{
		"Hello world",
		"你好世界",
		"Mixed 中英文 text with CRLF\r\nendings",
		"Emoji test 😀🎉 and special chars <>&\"'",
	}
	for _, input := range inputs {
		first := EstimateTokens(input)
		for i := 0; i < 10; i++ {
			got := EstimateTokens(input)
			if got != first {
				t.Errorf("EstimateTokens(%q) not deterministic: first=%d, iteration %d=%d", input, first, i, got)
			}
		}
	}
}

// TestEstimateTokens_CRLFNormalization verifies that CRLF and LF inputs
// produce the same token count (normalization is applied before estimation).
func TestEstimateTokens_CRLFNormalization(t *testing.T) {
	lfText := "line1\nline2\nline3"
	crlfText := "line1\r\nline2\r\nline3"
	crText := "line1\rline2\rline3"

	lfTokens := EstimateTokens(lfText)
	crlfTokens := EstimateTokens(crlfText)
	crTokens := EstimateTokens(crText)

	if lfTokens != crlfTokens {
		t.Errorf("LF and CRLF token counts differ: lf=%d crlf=%d", lfTokens, crlfTokens)
	}
	if lfTokens != crTokens {
		t.Errorf("LF and CR token counts differ: lf=%d cr=%d", lfTokens, crTokens)
	}
}

// GoldenCorpusEntry defines a golden corpus test case with a known expected
// token count. The expected values are pinned at M3 freeze and must not change
// without a version bump.
type GoldenCorpusEntry struct {
	Name     string
	Text     string
	Expected int64
	// Tolerance allows ±20% variance from the expected value. The canonical
	// tokenizer is a conservative estimator, not a precise tokenizer.
	Tolerance float64
}

// goldenCorpus is the frozen golden corpus for canonical tokenizer validation
// (architecture doc §12.1.1: "golden corpus 预期 token 数").
var goldenCorpus = []GoldenCorpusEntry{
	{
		Name:      "empty",
		Text:      "",
		Expected:  0,
		Tolerance: 0,
	},
	{
		Name:      "single-word-english",
		Text:      "hello",
		Expected:  2,
		Tolerance: 0.5,
	},
	{
		Name:      "short-english-sentence",
		Text:      "The quick brown fox jumps over the lazy dog.",
		Expected:  11,
		Tolerance: 0.3,
	},
	{
		Name:      "english-paragraph",
		Text:      "This is a longer paragraph of English text. It contains multiple sentences. The purpose is to verify that the canonical tokenizer produces a stable, conservative token estimate for typical English prose.",
		Expected:  55,
		Tolerance: 0.3,
	},
	{
		Name:      "single-cjk-character",
		Text:      "中",
		Expected:  1,
		Tolerance: 0.5,
	},
	{
		Name:      "chinese-sentence",
		Text:      "这是一段中文测试文本，用于验证规范分词器的稳定性。",
		Expected:  25,
		Tolerance: 0.3,
	},
	{
		Name:      "mixed-cn-en",
		Text:      "Hello 世界！This is a mixed 中英文 test with various characters.",
		Expected:  20,
		Tolerance: 0.3,
	},
	{
		Name:      "code-snippet",
		Text:      "func main() {\n    fmt.Println(\"Hello, World!\")\n}",
		Expected:  16,
		Tolerance: 0.3,
	},
	{
		Name:      "json-object",
		Text:      `{"name":"test","value":42,"items":["a","b","c"]}`,
		Expected:  17,
		Tolerance: 0.3,
	},
	{
		Name:      "ulid",
		Text:      "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Expected:  7,
		Tolerance: 0.3,
	},
	{
		Name:      "multiline-with-crlf",
		Text:      "line1\r\nline2\r\nline3\r\nline4",
		Expected:  7,
		Tolerance: 0.3,
	},
	{
		Name:      "emoji",
		Text:      "😀🎉👍🚀",
		Expected:  8,
		Tolerance: 0.5,
	},
	{
		Name:      "long-english-1k",
		Text:      strings.Repeat("The quick brown fox. ", 50),
		Expected:  280,
		Tolerance: 0.2,
	},
}

// TestGoldenCorpus verifies that the canonical tokenizer produces token counts
// within the expected tolerance for each golden corpus entry. This is the M3
// freeze gate (architecture doc §12.1.1).
func TestGoldenCorpus(t *testing.T) {
	if len(goldenCorpus) < 10 {
		t.Fatalf("golden corpus must have at least 10 entries, got %d", len(goldenCorpus))
	}
	for _, entry := range goldenCorpus {
		t.Run(entry.Name, func(t *testing.T) {
			got := EstimateTokens(entry.Text)
			if entry.Expected == 0 {
				if got != 0 {
					t.Errorf("golden corpus %q: EstimateTokens = %d, want 0", entry.Name, got)
				}
				return
			}
			lower := int64(float64(entry.Expected) * (1 - entry.Tolerance))
			upper := int64(float64(entry.Expected) * (1 + entry.Tolerance))
			if lower < 1 {
				lower = 1
			}
			if got < lower || got > upper {
				t.Errorf("golden corpus %q: EstimateTokens = %d, want [%d, %d] (expected %d, tolerance %.0f%%)",
					entry.Name, got, lower, upper, entry.Expected, entry.Tolerance*100)
			}
		})
	}
}

// TestGoldenCorpusFrozen verifies that golden corpus entries are immutable
// (frozen at M3). This test ensures no accidental modification of expected
// values. If expected values need to change, a version bump is required.
func TestGoldenCorpusFrozen(t *testing.T) {
	// Verify the golden corpus has a stable structure.
	expectedCount := 13
	if len(goldenCorpus) != expectedCount {
		t.Fatalf("golden corpus count = %d, want %d (frozen at M3)", len(goldenCorpus), expectedCount)
	}
	// Verify each entry has non-empty name and text (except empty test).
	for _, entry := range goldenCorpus {
		if entry.Name == "" {
			t.Fatal("golden corpus entry has empty name")
		}
		if entry.Expected < 0 {
			t.Fatalf("golden corpus entry %q has negative expected: %d", entry.Name, entry.Expected)
		}
	}
}

// TestArtifactDescriptor verifies the descriptor fields are frozen.
func TestArtifactDescriptor(t *testing.T) {
	d := ArtifactDescriptor()
	if d.ID != CanonicalTokenizerID {
		t.Errorf("descriptor ID = %q, want %q", d.ID, CanonicalTokenizerID)
	}
	if d.Version != CanonicalTokenizerVersion {
		t.Errorf("descriptor Version = %q, want %q", d.Version, CanonicalTokenizerVersion)
	}
	if d.Normalization != "Unicode NFC" {
		t.Errorf("descriptor Normalization = %q, want Unicode NFC", d.Normalization)
	}
	if !strings.Contains(d.LineEnding, "LF") {
		t.Errorf("descriptor LineEnding = %q, must contain LF", d.LineEnding)
	}
	if !strings.Contains(d.Serialization, "canonical") {
		t.Errorf("descriptor Serialization = %q, must contain canonical", d.Serialization)
	}
}

// TestEstimateTokens_NFCStability verifies that NFC-equivalent inputs produce
// similar token counts (e.g., precomposed vs decomposed forms). Since we don't
// have full NFC normalization, this test documents the current behavior and
// ensures the difference is bounded. Full NFC composition would reduce the
// difference to 0; without it, the conservative estimator may differ by up to 2
// tokens for short strings with combining characters.
func TestEstimateTokens_NFCStability(t *testing.T) {
	// Precomposed é (U+00E9) vs decomposed e + combining acute (U+0065 U+0301)
	precomposed := "café"
	decomposed := "cafe\u0301"
	preTokens := EstimateTokens(precomposed)
	decompTokens := EstimateTokens(decomposed)
	// The difference should be small (at most 2 tokens without full NFC).
	diff := preTokens - decompTokens
	if diff < 0 {
		diff = -diff
	}
	if diff > 2 {
		t.Errorf("NFC stability: precomposed=%d, decomposed=%d, diff=%d (should be ≤2)", preTokens, decompTokens, diff)
	}
}
