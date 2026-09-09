package llmadapter

// Optional counters distinguish a provider-reported zero from absent metadata.
type openAIUsage struct {
	Prompt         *int `json:"prompt_tokens"`
	Completion     int  `json:"completion_tokens"`
	Total          int  `json:"total_tokens"`
	PromptCacheHit *int `json:"prompt_cache_hit_tokens"`
	PromptDetails  *struct {
		Cached *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (w openAIUsage) normalized() Usage {
	u := normalizeUsage(usageCount(w.Prompt), w.Completion, w.Total)
	cached := w.PromptCacheHit
	if cached == nil && w.PromptDetails != nil {
		cached = w.PromptDetails.Cached
	}
	if cached != nil && *cached >= 0 && *cached <= u.InputTokens {
		u.CachedInputTokens = *cached
		u.CacheUsageReported = w.Prompt != nil
	}
	return u
}

type anthropicUsage struct {
	Input                *int `json:"input_tokens"`
	Output               *int `json:"output_tokens"`
	CacheRead            *int `json:"cache_read_input_tokens"`
	CacheCreation        *int `json:"cache_creation_input_tokens"`
	CacheCreationDetails *struct {
		Ephemeral5m int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServiceTier   string  `json:"service_tier"`
	InferenceGeo  *string `json:"inference_geo"`
	ServerToolUse *struct {
		WebSearchRequests int `json:"web_search_requests"`
		WebFetchRequests  int `json:"web_fetch_requests"`
	} `json:"server_tool_use"`
}

func usageCount(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func (w anthropicUsage) input() int {
	return usageCount(w.Input) + usageCount(w.CacheRead) + usageCount(w.CacheCreation)
}

func (w anthropicUsage) normalized() Usage {
	u := normalizeUsage(w.input(), usageCount(w.Output), 0)
	if (w.CacheRead != nil || w.CacheCreation != nil) && usageCount(w.CacheRead) >= 0 && usageCount(w.CacheCreation) >= 0 {
		u.CachedInputTokens = usageCount(w.CacheRead)
		u.CacheWriteInputTokens = usageCount(w.CacheCreation)
		// Anthropic input excludes both cache buckets. A compatible endpoint
		// omitting either bucket only supplies partial accounting.
		u.CacheUsageReported = w.Input != nil && w.CacheRead != nil && w.CacheCreation != nil
	}
	return u
}

// Streaming Anthropic counters are cumulative snapshots. Omitted fields in
// message_delta preserve the input/cache counters from message_start; repeated
// output counters replace earlier values and are never counted twice.
func (w *anthropicUsage) merge(next anthropicUsage) {
	if next.Input != nil {
		w.Input = next.Input
	}
	if next.Output != nil {
		w.Output = next.Output
	}
	if next.CacheRead != nil {
		w.CacheRead = next.CacheRead
	}
	if next.CacheCreation != nil {
		w.CacheCreation = next.CacheCreation
	}
}

func (w anthropicUsage) valid() bool {
	return validUsage(usageCount(w.Input), usageCount(w.Output), w.input()+usageCount(w.Output)) &&
		validUsage(usageCount(w.CacheRead), usageCount(w.CacheCreation), w.input())
}
