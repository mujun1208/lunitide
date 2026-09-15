package modelfit

import (
	"encoding/json"
	"fmt"
)

type ModelIntent struct {
	Mode           string `json:"mode"`
	StrictTools    bool   `json:"strictTools"`
	OutputTokenCap int64  `json:"outputTokenCap"`
}

type EffectiveParameters struct {
	ThinkingType  string `json:"thinkingType"`
	Effort        string `json:"effort"`
	ClearThinking *bool  `json:"clearThinking"`
	StrictTools   bool   `json:"strictTools"`
	MaxTokens     int64  `json:"maxTokens"`
}

func CompileParameters(p ModelProfile, in ModelIntent) (EffectiveParameters, error) {
	mode, ok := p.Modes[in.Mode]
	if !ok {
		return EffectiveParameters{}, fmt.Errorf("unknown mode %q", in.Mode)
	}
	maxTokens := p.MaxOutputTokens
	if in.OutputTokenCap > 0 && (maxTokens <= 0 || in.OutputTokenCap < maxTokens) {
		maxTokens = in.OutputTokenCap
	}
	return EffectiveParameters{
		ThinkingType:  mode.ThinkingType,
		Effort:        mode.Effort,
		ClearThinking: mode.ClearThinking,
		StrictTools:   p.StrictTools || in.StrictTools,
		MaxTokens:     maxTokens,
	}, nil
}

// EncodePrepared writes vendor thinking fields. GLM uses clear_thinking;
// a non-nil false must appear in JSON (not dropped by omitempty).
func EncodePrepared(p ModelProfile, eff EffectiveParameters) ([]byte, error) {
	out := map[string]any{}
	if p.Protocol == "openai_compatible" || p.Protocol == "" {
		if eff.ThinkingType != "" && eff.ThinkingType != "omitted" {
			out["thinking"] = map[string]string{"type": eff.ThinkingType}
		}
		if eff.Effort != "" && eff.Effort != "omitted" {
			out["reasoning_effort"] = eff.Effort
		}
	}
	if eff.ClearThinking != nil {
		out["clear_thinking"] = *eff.ClearThinking
	}
	if eff.MaxTokens > 0 {
		out["max_tokens"] = eff.MaxTokens
	}
	return json.Marshal(out)
}
