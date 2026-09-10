package llmadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const anthropicVersion = "2023-06-01"

type Anthropic struct {
	c Connector
	o Options
}

func NewAnthropic(c Connector, o Options) *Anthropic { return &Anthropic{c: c, o: defaults(o)} }

type anthropicRequest struct {
	Model     string                 `json:"model"`
	System    []anthropicSystemBlock `json:"system,omitempty"`
	Messages  []anthropicMessage     `json:"messages"`
	MaxTokens int                    `json:"max_tokens"`
	Stream    bool                   `json:"stream,omitempty"`
	Tools     []anthropicTool        `json:"tools,omitempty"`
}
type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}
type anthropicCacheControl struct {
	Type string `json:"type"`
}

// ephemeralCache is the single cache_control flavor Anthropic supports.
func ephemeralCache() *anthropicCacheControl { return &anthropicCacheControl{Type: "ephemeral"} }

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessage struct {
	Role    Role `json:"role"`
	Content any  `json:"content"`
}
type anthropicBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	Thinking     string                 `json:"thinking,omitempty"`
	Source       *anthropicImageSource  `json:"source,omitempty"`
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Input        json.RawMessage        `json:"input,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
	Content      string                 `json:"content,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}
type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}
type anthropicResponse struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Role         Role    `json:"role"`
	Model        string  `json:"model"`
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
	Index        int     `json:"index"`
	ContentBlock struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		Thinking  string          `json:"thinking"`
		Signature string          `json:"signature"`
	} `json:"content_block"`
	Content []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Signature string          `json:"signature"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
	} `json:"content"`
	Delta struct {
		Type         string  `json:"type"`
		Text         string  `json:"text"`
		Thinking     string  `json:"thinking"`
		Signature    string  `json:"signature"`
		PartialJSON  string  `json:"partial_json"`
		StopReason   *string `json:"stop_reason"`
		StopSequence *string `json:"stop_sequence"`
	} `json:"delta"`
	Usage   anthropicUsage     `json:"usage"`
	Message *anthropicResponse `json:"message"`
}

// anthropicPayload builds the wire request. Three ephemeral cache
// breakpoints are placed on the stable prefix (P0-2 prompt caching):
// tools tail, system tail, and the last-but-one message tail. Anthropic
// allows at most four; the prefix stays byte-identical across the
// multi-step tool loop and across turns (history is append-only), so
// subsequent requests re-hit the cache and prefill cost drops.
func anthropicPayload(in Request, stream bool, wn *wireNames) anthropicRequest {
	p := anthropicRequest{Model: in.Model, MaxTokens: in.MaxTokens, Stream: stream}
	for _, t := range in.Tools {
		p.Tools = append(p.Tools, anthropicTool{Name: wn.wire(t.Name), Description: t.Description, InputSchema: t.Schema})
	}
	if n := len(p.Tools); n > 0 {
		p.Tools[n-1].CacheControl = ephemeralCache()
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = 1
	}
	var sys []string
	lastUser := -1
	toolResultGroup := -1
	for _, m := range in.Messages {
		if m.Role == RoleSystem {
			sys = append(sys, m.Content)
		} else {
			if m.Role != RoleTool {
				toolResultGroup = -1
			}
			role := m.Role
			var content any = m.Content
			if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
				blocks := []anthropicBlock{}
				if m.Content != "" {
					blocks = append(blocks, anthropicBlock{Type: "text", Text: m.Content})
				}
				for _, tc := range m.ToolCalls {
					blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: tc.ID, Name: wn.wire(tc.Name), Input: tc.Arguments})
				}
				content = blocks
			}
			if m.Role == RoleTool {
				role = RoleUser
				result := anthropicBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}
				// Parallel tool results belong in one user message directly after
				// their assistant tool_use turn. Keep normal user turns separate
				// so attachments still target their original user message.
				if toolResultGroup >= 0 {
					blocks := p.Messages[toolResultGroup].Content.([]anthropicBlock)
					p.Messages[toolResultGroup].Content = append(blocks, result)
					continue
				}
				toolResultGroup = len(p.Messages)
				content = []anthropicBlock{result}
			}
			p.Messages = append(p.Messages, anthropicMessage{Role: role, Content: content})
			if m.Role == RoleUser {
				lastUser = len(p.Messages) - 1
			}
		}
	}
	if lastUser >= 0 && len(in.Images) > 0 {
		blocks := make([]anthropicBlock, 0, len(in.Images)+1)
		for _, image := range in.Images {
			blocks = append(blocks, anthropicBlock{Type: "image", Source: &anthropicImageSource{Type: "base64", MediaType: image.MIME, Data: base64.StdEncoding.EncodeToString(image.Data)}})
		}
		blocks = append(blocks, anthropicBlock{Type: "text", Text: p.Messages[lastUser].Content.(string)})
		p.Messages[lastUser].Content = blocks
	}
	for _, s := range sys {
		p.System = append(p.System, anthropicSystemBlock{Type: "text", Text: s})
	}
	if n := len(p.System); n > 0 {
		// 严格分离静态区与动态区缓存：
		// 第一个 system block 通常为静态规则，加上 cache_control 提高命中率。
		p.System[0].CacheControl = ephemeralCache()
		if n > 1 {
			// 最后一个 system block 包含动态状态，加 cache_control 作为历史前缀断点。
			p.System[n-1].CacheControl = ephemeralCache()
		}
	}
	// History-prefix breakpoint: everything except the final message is
	// stable across the six-step tool loop and across turns, so marking
	// the second-to-last message tail turns each follow-up into an
	// incremental cache write/read.
	if n := len(p.Messages); n >= 2 {
		markAnthropicCacheBreakpoint(&p.Messages[n-2])
	}
	return p
}

// markAnthropicCacheBreakpoint stamps an ephemeral cache_control on the
// trailing block of one message. String content is lifted into a single
// text block first; cache_control is only expressible on content blocks.
func markAnthropicCacheBreakpoint(m *anthropicMessage) {
	switch c := m.Content.(type) {
	case string:
		m.Content = []anthropicBlock{{Type: "text", Text: c, CacheControl: ephemeralCache()}}
	case []anthropicBlock:
		c[len(c)-1].CacheControl = ephemeralCache()
	}
}
func (a *Anthropic) Complete(ctx context.Context, s []byte, in Request) (Response, error) {
	return a.run(ctx, s, in, false, nil)
}
func (a *Anthropic) Stream(ctx context.Context, s []byte, in Request, emit func(Delta) error) (Response, error) {
	return a.run(ctx, s, in, true, emit)
}
func (a *Anthropic) run(ctx context.Context, secret []byte, in Request, stream bool, emit func(Delta) error) (Response, error) {
	in = attachEfficientRequest(in, a.o)
	wn := buildWireNames(in.Tools, anthropicToolNameMax)
	p := anthropicPayload(in, stream, wn)
	maxAttempts := attempts(a.o, in, stream)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		body, e := marshalBounded(p, a.o.MaxRequestBytes)
		if e != nil {
			return Response{}, e
		}
		req, e := a.c.NewRequest(ctx, http.MethodPost, "messages", body)
		if e != nil {
			return Response{}, classify(e)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("anthropic-version", anthropicVersion)
		if in.IdempotencyKey != "" && a.o.IdempotencyHeader != "" {
			req.Header.Set(a.o.IdempotencyHeader, in.IdempotencyKey)
		}
		resp, e := doWithSecret(a.c, req, "x-api-key", "", secret)
		if e != nil {
			if attempt < maxAttempts && retryableBeforeConnect(e) {
				if x := waitRetry(ctx, retryDelay(nil, attempt, a.o.RetryBase)); x != nil {
					return Response{}, x
				}
				continue
			}
			return Response{}, uncertain(e)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			status := resp.StatusCode
			resp.Body.Close()
			if attempt < maxAttempts && in.IdempotencyKey != "" && a.o.IdempotencyHeader != "" && retryableStatus(status) {
				if x := waitRetry(ctx, retryDelay(resp, attempt, a.o.RetryBase)); x != nil {
					return Response{}, x
				}
				continue
			}
			return Response{}, statusError(status)
		}
		if stream {
			return a.readStream(resp.Body, emit, wn)
		}
		var x anthropicResponse
		e = strictJSON(resp.Body, &x)
		resp.Body.Close()
		if e != nil {
			return Response{}, e
		}
		if len(x.Content) == 0 {
			return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, resp.StatusCode, "upstream success omitted content")
		}
		if !x.Usage.valid() {
			return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, resp.StatusCode, "upstream returned invalid fields")
		}
		var text, reasoning strings.Builder
		var calls []ToolCall
		for _, c := range x.Content {
			if c.Type == "text" {
				text.WriteString(c.Text)
			}
			if c.Type == "thinking" {
				reasoning.WriteString(c.Thinking)
			}
			if c.Type == "tool_use" && c.ID != "" && c.Name != "" && json.Valid(c.Input) {
				calls = append(calls, ToolCall{ID: c.ID, Name: wn.original(c.Name), Arguments: c.Input})
			}
		}
		return Response{Message: Message{Role: RoleAssistant, Content: text.String(), ReasoningContent: reasoning.String(), ToolCalls: calls}, Usage: x.Usage.normalized(), Reasoning: reasoning.String(), FinishReason: normalizeFinishReason(x.StopReason)}, nil
	}
	return Response{}, safeError("RETRY_EXHAUSTED", StageConnect, 0, "upstream unavailable")
}
func (a *Anthropic) readStream(body io.ReadCloser, emit func(Delta) error, wn *wireNames) (Response, error) {
	defer body.Close()
	out := Response{Message: Message{Role: RoleAssistant}}
	type partialCall struct {
		id, name string
		args     strings.Builder
	}
	partials := map[int]*partialCall{}
	var usage anthropicUsage
	completed := false
	for {
		ev, eof, e := a.c.ReadSSE(body)
		if e != nil {
			return out, classifyStreamError(e)
		}
		if eof {
			break
		}
		typ, data := sseData(ev)
		if err := streamEventError(typ, data); err != nil {
			return out, err
		}
		if typ == "message_stop" {
			completed = true
			break
		}
		var x anthropicResponse
		if e := strictJSON(strings.NewReader(data), &x); e != nil {
			return out, e
		}
		if typ == "content_block_delta" && x.Delta.Type == "text_delta" {
			out.Message.Content += x.Delta.Text
			if emit != nil {
				if e := emit(Delta{Text: x.Delta.Text}); e != nil {
					return out, e
				}
			}
		}
		if typ == "content_block_delta" && x.Delta.Type == "thinking_delta" && x.Delta.Thinking != "" {
			out.Reasoning += x.Delta.Thinking
			if emit != nil {
				if e := emit(Delta{Reasoning: x.Delta.Thinking}); e != nil {
					return out, e
				}
			}
		}
		if typ == "content_block_start" && x.ContentBlock.Type == "tool_use" {
			if x.ContentBlock.ID == "" || x.ContentBlock.Name == "" {
				return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "tool_use omitted identity")
			}
			p := &partialCall{id: x.ContentBlock.ID, name: x.ContentBlock.Name}
			if len(x.ContentBlock.Input) > 0 && string(x.ContentBlock.Input) != "{}" {
				p.args.Write(x.ContentBlock.Input)
			}
			partials[x.Index] = p
		}
		if typ == "content_block_delta" && x.Delta.Type == "input_json_delta" {
			p := partials[x.Index]
			if p == nil {
				return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "tool fragment omitted start")
			}
			p.args.WriteString(x.Delta.PartialJSON)
		}
		if typ == "content_block_stop" {
			if p := partials[x.Index]; p != nil {
				raw := json.RawMessage(p.args.String())
				if len(raw) == 0 {
					raw = json.RawMessage(`{}`)
				}
				if !json.Valid(raw) {
					return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "tool arguments are invalid JSON")
				}
				call := ToolCall{ID: p.id, Name: wn.original(p.name), Arguments: raw}
				out.Message.ToolCalls = append(out.Message.ToolCalls, call)
				if emit != nil {
					if e := emit(Delta{ToolCall: &call}); e != nil {
						return out, e
					}
				}
				delete(partials, x.Index)
			}
		}
		if typ == "message_start" || typ == "message_delta" {
			next := x.Usage
			if typ == "message_start" && x.Message != nil {
				next = x.Message.Usage
				out.recordFinishReason(x.Message.StopReason)
			}
			if typ == "message_delta" {
				out.recordFinishReason(x.Delta.StopReason)
			}
			// Older compatible endpoints send zero placeholders on deltas.
			// They cannot erase already reported positive input/cache counts.
			if typ == "message_delta" && usageCount(next.Input) == 0 && usageCount(usage.Input) > 0 {
				next.Input = nil
			}
			usage.merge(next)
			if !usage.valid() {
				return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "upstream returned invalid usage")
			}
			u := usage.normalized()
			out.Usage = u
			if emit != nil && (u.TotalTokens > 0 || u.CacheUsageReported) {
				if e := emit(Delta{Usage: &u}); e != nil {
					return out, e
				}
			}
		}
	}
	if !completed {
		return out, safeError("STREAM_INCOMPLETE", StageStream, 0, "upstream stream ended before completion")
	}
	if len(partials) != 0 {
		return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "unterminated tool_use block")
	}
	out.Message.ReasoningContent = out.Reasoning
	return out, nil
}
func (a *Anthropic) Discover(ctx context.Context, secret []byte) (Discovery, error) {
	return Discovery{Unsupported: true, Warning: "Anthropic does not provide a portable model-list endpoint; configure a model explicitly"}, nil
}
