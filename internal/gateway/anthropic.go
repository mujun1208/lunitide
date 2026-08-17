package gateway

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
	Index        int `json:"index"`
	ContentBlock struct {
		Type      string          `json:"type"`
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
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	Usage struct {
		Input         int `json:"input_tokens"`
		Output        int `json:"output_tokens"`
		CacheRead     int `json:"cache_read_input_tokens"`
		CacheCreation int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// anthropicBilledInput folds cache accounting into the metered input count:
// Anthropic reports uncached tokens in input_tokens while cached reads and
// writes arrive separately, so all three must sum for truthful totals.
func (x anthropicResponse) anthropicBilledInput() int {
	return x.Usage.Input + x.Usage.CacheRead + x.Usage.CacheCreation
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
	for _, m := range in.Messages {
		if m.Role == RoleSystem {
			sys = append(sys, m.Content)
		} else {
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
				content = []anthropicBlock{{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}}
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
		p.System = append(p.System, anthropicSystemBlock{Text: s})
	}
	if n := len(p.System); n > 0 {
		p.System[n-1].CacheControl = ephemeralCache()
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
		if !validUsage(x.anthropicBilledInput(), x.Usage.Output, x.anthropicBilledInput()+x.Usage.Output) {
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
		return Response{Message: Message{Role: RoleAssistant, Content: text.String(), ToolCalls: calls}, Usage: normalizeUsage(x.anthropicBilledInput(), x.Usage.Output, 0), Reasoning: reasoning.String()}, nil
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
	for {
		ev, eof, e := a.c.ReadSSE(body)
		if e != nil {
			return out, classify(e)
		}
		if eof {
			break
		}
		typ, data := sseData(ev)
		if typ == "message_stop" {
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
			billed := x.anthropicBilledInput()
			u := normalizeUsage(billed, x.Usage.Output, 0)
			if billed > 0 {
				out.Usage.InputTokens = billed
			}
			if x.Usage.Output > 0 {
				out.Usage.OutputTokens = x.Usage.Output
			}
			out.Usage.TotalTokens = out.Usage.InputTokens + out.Usage.OutputTokens
			if emit != nil && u.TotalTokens > 0 {
				if e := emit(Delta{Usage: &u}); e != nil {
					return out, e
				}
			}
		}
	}
	if len(partials) != 0 {
		return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "unterminated tool_use block")
	}
	return out, nil
}
func (a *Anthropic) Discover(ctx context.Context, secret []byte) (Discovery, error) {
	return Discovery{Unsupported: true, Warning: "Anthropic does not provide a portable model-list endpoint; configure a model explicitly"}, nil
}
