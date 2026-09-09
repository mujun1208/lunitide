package llmadapter

// FinishReason is a bounded provider-neutral classification, never upstream
// free text. Empty means the provider did not report a reason.
type FinishReason string

const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonContentFilter FinishReason = "content_filter"
	FinishReasonToolCalls     FinishReason = "tool_calls"
	FinishReasonOther         FinishReason = "other"
)

func normalizeFinishReason(raw *string) FinishReason {
	if raw == nil || *raw == "" {
		return ""
	}
	switch *raw {
	case "stop", "end_turn", "stop_sequence":
		return FinishReasonStop
	case "length", "max_tokens", "model_context_window_exceeded":
		return FinishReasonLength
	case "content_filter", "refusal":
		return FinishReasonContentFilter
	case "tool_calls", "function_call", "tool_use":
		return FinishReasonToolCalls
	default:
		return FinishReasonOther
	}
}

func (r *Response) recordFinishReason(raw *string) {
	// Usage-only/empty frames cannot erase a terminal reason. Once explicitly
	// incomplete, a later conflicting stop must not turn partial text into success.
	if r.FinishReason == FinishReasonLength || r.FinishReason == FinishReasonContentFilter {
		return
	}
	if reason := normalizeFinishReason(raw); reason != "" {
		r.FinishReason = reason
	}
}
