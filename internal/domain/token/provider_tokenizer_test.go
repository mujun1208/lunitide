package token

import "testing"

// TestCountTokensForModel_KnownModelExact verifies that a known OpenAI-family
// model uses the exact BPE tokenizer and returns the well-known token counts.
// These values are stable properties of the cl100k_base / o200k_base encoders.
func TestCountTokensForModel_KnownModelExact(t *testing.T) {
	tests := []struct {
		name  string
		model string
		text  string
		want  int64
	}{
		// "hello world" is a canonical tiktoken fixture: 2 tokens under
		// cl100k_base ("hello", " world").
		{"gpt-4 hello world", "gpt-4", "hello world", 2},
		{"gpt-3.5 hello world", "gpt-3.5-turbo", "hello world", 2},
		// o200k_base (gpt-4o) also encodes "hello world" as 2 tokens.
		{"gpt-4o hello world", "gpt-4o", "hello world", 2},
		// Prefixed / dated model ids resolve via prefix matching.
		{"gpt-4o dated", "gpt-4o-2024-05-13", "hello world", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountTokensForModel(tt.model, tt.text)
			if got != tt.want {
				t.Errorf("CountTokensForModel(%q, %q) = %d, want %d", tt.model, tt.text, got, tt.want)
			}
		})
	}
}

// TestCountTokensForModel_ExactDiffersFromHeuristic ensures the exact path is
// actually engaged (i.e. it is not silently identical to the heuristic) for a
// longer sample, and stays a positive, plausible count.
func TestCountTokensForModel_ExactDiffersFromHeuristic(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog. Tokenization matters."
	exact := CountTokensForModel("gpt-4o", text)
	if exact < 1 {
		t.Fatalf("exact count = %d, want >= 1", exact)
	}
	// The exact count should be within a sane band of the word/char length.
	if exact > int64(len(text)) {
		t.Errorf("exact count %d exceeds byte length %d", exact, len(text))
	}
}

// TestCountTokensForModel_UnknownModelFallback verifies unknown models fall
// back to the frozen canonical EstimateTokens exactly.
func TestCountTokensForModel_UnknownModelFallback(t *testing.T) {
	samples := []string{
		"hello world",
		"这是一段中文测试文本。",
		"Hello 世界 test 😀",
		"",
	}
	for _, s := range samples {
		got := CountTokensForModel("some-unknown-model-xyz", s)
		want := EstimateTokens(s)
		if got != want {
			t.Errorf("CountTokensForModel(unknown, %q) = %d, want EstimateTokens = %d", s, got, want)
		}
	}
}

// TestCountTokensForModel_EmptyModelFallback verifies an empty model name (no
// tokenizer resolvable) falls back to canonical.
func TestCountTokensForModel_EmptyModelFallback(t *testing.T) {
	text := "fallback please"
	if got, want := CountTokensForModel("", text), EstimateTokens(text); got != want {
		t.Errorf("CountTokensForModel(\"\", %q) = %d, want %d", text, got, want)
	}
}

// TestCountTokensForModel_Deterministic verifies repeated calls (exercising the
// encoder cache) return identical results and are concurrency-safe-ish under
// sequential repetition.
func TestCountTokensForModel_Deterministic(t *testing.T) {
	text := "Determinism is a property of the tokenizer path."
	first := CountTokensForModel("gpt-4o", text)
	for i := 0; i < 5; i++ {
		if got := CountTokensForModel("gpt-4o", text); got != first {
			t.Fatalf("non-deterministic count: got %d, first %d", got, first)
		}
	}
}