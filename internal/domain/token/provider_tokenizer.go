// Provider-accurate tokenization for OpenAI-family models.
//
// This file adds a NEW code path on top of the frozen canonical estimator.
// It does NOT modify EstimateTokens (ADR-005 / M3 canonical freeze). For known
// OpenAI-family models (gpt-4o / gpt-4 / gpt-3.5 → o200k_base / cl100k_base,
// plus other models recognized by tiktoken-go) it returns an exact BPE token
// count; for unknown models, or whenever the tokenizer cannot be loaded, it
// silently falls back to the canonical EstimateTokens.
//
// Offline availability: the BPE encoding files (cl100k_base, o200k_base, …) are
// provided by github.com/pkoukk/tiktoken-go-loader via go:embed and injected
// through tiktoken.SetBpeLoader. No network access is required at runtime or in
// tests — the default tiktoken-go loader (which downloads from a blob URL) is
// never used.
package token

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

var (
	// bpeLoaderOnce ensures the offline (embedded) BPE loader is installed
	// exactly once before any encoding is loaded.
	bpeLoaderOnce sync.Once

	// encoderMu guards encoderCache.
	encoderMu sync.Mutex
	// encoderCache memoizes *tiktoken.Tiktoken per model name (including the
	// "unavailable" negative result, cached as a nil value) so repeated calls
	// are cheap and thread-safe.
	encoderCache = map[string]*tiktoken.Tiktoken{}
	// encoderKnown records whether a model has been resolved (positively or
	// negatively) to distinguish a cached nil (unavailable) from "not tried".
	encoderKnown = map[string]bool{}
)

// installOfflineBpeLoader wires tiktoken-go to the embedded BPE assets so that
// encoding loads never hit the network. Safe to call repeatedly.
func installOfflineBpeLoader() {
	bpeLoaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
	})
}

// encoderForModel returns a cached tiktoken encoder for the given model name,
// or nil if the model is unknown / the encoding could not be loaded. It never
// panics and never blocks on I/O beyond the first (embedded) load per encoding.
func encoderForModel(model string) *tiktoken.Tiktoken {
	if model == "" {
		return nil
	}

	encoderMu.Lock()
	defer encoderMu.Unlock()

	if encoderKnown[model] {
		return encoderCache[model]
	}

	installOfflineBpeLoader()

	// EncodingForModel resolves both exact model ids and known prefixes
	// (e.g. "gpt-4o-2024-05-13"). Unknown models return an error → fallback.
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil || enc == nil {
		// Some deployments namespace models as "openai/gpt-4o"; retry on the
		// trailing segment before giving up.
		if base := lastPathSegment(model); base != "" && base != model {
			enc, err = tiktoken.EncodingForModel(base)
		}
	}
	if err != nil || enc == nil {
		encoderKnown[model] = true
		encoderCache[model] = nil
		return nil
	}

	encoderKnown[model] = true
	encoderCache[model] = enc
	return enc
}

// lastPathSegment returns the substring after the final '/' in s, or "" if
// there is no '/'.
func lastPathSegment(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return ""
}

// CountTokensForModel returns a token count for text using the exact tokenizer
// for known OpenAI-family models, falling back to the frozen canonical
// EstimateTokens for unknown models or when the exact tokenizer is unavailable
// (e.g. an embedded encoding fails to load).
//
// The text is normalized via NormalizeText (NFC + LF) before counting so that
// exact counts remain deterministic and consistent with the rest of the token
// pipeline. This function never modifies canonical behavior: callers that need
// the frozen canonical count must keep calling EstimateTokens directly.
func CountTokensForModel(model, text string) int64 {
	if len(text) == 0 {
		return 0
	}
	enc := encoderForModel(model)
	if enc == nil {
		return EstimateTokens(text)
	}
	normalized := NormalizeText(text)
	// EncodeOrdinary counts content tokens without special-token handling,
	// which is what we want for raw text budgeting.
	tokens := enc.EncodeOrdinary(normalized)
	return max64(1, int64(len(tokens)))
}