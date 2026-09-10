package token

import "strings"

type Estimate struct {
	Count              int64
	Method             string
	Confidence         string
	TokenizerRevision  string
}

func EstimateForDeployment(model, text string) Estimate {
	count := CountTokensForModel(model, text)
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "deepseek"):
		return Estimate{Count: count, Method: "family_calibrated", Confidence: "medium", TokenizerRevision: "deepseek-offline-v1"}
	case strings.Contains(m, "glm") || strings.Contains(m, "zhipu"):
		return Estimate{Count: count, Method: "family_calibrated", Confidence: "medium", TokenizerRevision: "glm-offline-v1"}
	case encoderForModel(model) != nil:
		return Estimate{Count: count, Method: "exact", Confidence: "high", TokenizerRevision: "tiktoken-offline"}
	default:
		return Estimate{Count: count, Method: "canonical_fallback", Confidence: "low", TokenizerRevision: CanonicalTokenizerRevision}
	}
}
