package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

type OpenAI struct {
	c Connector
	o Options
}

func NewOpenAI(c Connector, o Options) *OpenAI { return &OpenAI{c: c, o: defaults(o)} }

type openAIRequest struct {
	Model         string          `json:"model"`
	Messages      []openAIMessage `json:"messages"`
	MaxTokens     int             `json:"max_tokens,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Tools []openAITool `json:"tools,omitempty"`
	// Disable-reasoning hints accepted by Volcengine/Qwen-compatible
	// OpenAI endpoints. Unknown fields are ignored by strict OpenAI;
	// a 400 strips them and retries once.
	Thinking       *openAIThinking `json:"thinking,omitempty"`
	EnableThinking *bool           `json:"enable_thinking,omitempty"`
}
type openAIThinking struct {
	Type string `json:"type"`
}
type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}
type openAIMessage struct {
	Role       Role             `json:"role"`
	Content    any              `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}
type openAIToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}
type openAIContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Role             Role             `json:"role"`
			Content          string           `json:"content"`
			ReasoningContent string           `json:"reasoning_content,omitempty"`
			ToolCalls        []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Content          string           `json:"content,omitempty"`
			ReasoningContent string           `json:"reasoning_content,omitempty"`
			ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
		} `json:"delta,omitempty"`
	} `json:"choices"`
	Usage struct {
		Prompt     int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
		Total      int `json:"total_tokens"`
	} `json:"usage"`
}

func (a *OpenAI) Complete(ctx context.Context, secret []byte, in Request) (Response, error) {
	return a.run(ctx, secret, in, false, nil)
}
func (a *OpenAI) Stream(ctx context.Context, secret []byte, in Request, emit func(Delta) error) (Response, error) {
	return a.run(ctx, secret, in, true, emit)
}

func (a *OpenAI) TestConnection(ctx context.Context, secret []byte, in Request) error {
	p := openAIRequest{Model: in.Model, Messages: openAIMessages(in, nil), MaxTokens: in.MaxTokens}
	body, err := marshalBounded(p, a.o.MaxRequestBytes)
	if err != nil {
		return err
	}
	req, err := a.c.NewRequest(ctx, http.MethodPost, "chat/completions", body)
	if err != nil {
		return classify(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
	if err != nil {
		return uncertain(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError(resp.StatusCode)
	}
	return nil
}

func (a *OpenAI) run(ctx context.Context, secret []byte, in Request, stream bool, emit func(Delta) error) (Response, error) {
	wn := buildWireNames(in.Tools, openAIToolNameMax)
	p := openAIRequest{Model: in.Model, Messages: openAIMessages(in, wn), MaxTokens: in.MaxTokens, Stream: stream}
	if in.DisableReasoning {
		disabled := false
		p.EnableThinking = &disabled
		p.Thinking = &openAIThinking{Type: "disabled"}
	}
	for _, t := range in.Tools {
		x := openAITool{Type: "function"}
		x.Function.Name, x.Function.Description, x.Function.Parameters = wn.wire(t.Name), t.Description, t.Schema
		p.Tools = append(p.Tools, x)
	}
	if stream {
		p.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{true}
	}
	var last error
	maxAttempts := attempts(a.o, in, stream)
	sanitized := false
	strippedThinking := false
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		body, err := marshalBounded(p, a.o.MaxRequestBytes)
		if err != nil {
			return Response{}, err
		}
		req, err := a.c.NewRequest(ctx, http.MethodPost, "chat/completions", body)
		if err != nil {
			return Response{}, classify(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if in.IdempotencyKey != "" && a.o.IdempotencyHeader != "" {
			req.Header.Set(a.o.IdempotencyHeader, in.IdempotencyKey)
		}
		resp, err := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
		if err != nil {
			last = uncertain(err)
			if attempt < maxAttempts && retryableBeforeConnect(err) {
				if e := waitRetry(ctx, retryDelay(nil, attempt, a.o.RetryBase)); e != nil {
					return Response{}, e
				}
				continue
			}
			return Response{}, last
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			reason := boundedReason(resp.Body)
			resp.Body.Close()
			// Some OpenAI-compatible providers reject non-core JSON
			// Schema keywords (additionalProperties, minimum, maxItems,
			// ...). On a 400 with tools attached, retry exactly once with
			// sanitized schemas before giving up; the chat layer then
			// falls back to plain dialogue with this reason surfaced.
			if resp.StatusCode == http.StatusBadRequest && in.DisableReasoning && !strippedThinking && (p.Thinking != nil || p.EnableThinking != nil) {
				strippedThinking = true
				p.Thinking = nil
				p.EnableThinking = nil
				attempt--
				continue
			}
			if resp.StatusCode == http.StatusBadRequest && len(p.Tools) > 0 && !sanitized {
				sanitized = true
				for i := range p.Tools {
					p.Tools[i].Function.Parameters = sanitizeToolSchema(p.Tools[i].Function.Parameters)
				}
				attempt--
				continue
			}
			last = statusErrorReason(resp.StatusCode, reason)
			if attempt < maxAttempts && in.IdempotencyKey != "" && a.o.IdempotencyHeader != "" && retryableStatus(resp.StatusCode) {
				if e := waitRetry(ctx, retryDelay(resp, attempt, a.o.RetryBase)); e != nil {
					return Response{}, e
				}
				continue
			}
			return Response{}, last
		}
		if !stream {
			var out openAIResponse
			err = compatibleJSON(resp.Body, &out)
			resp.Body.Close()
			if err != nil {
				return Response{}, err
			}
			if len(out.Choices) == 0 {
				return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, resp.StatusCode, "upstream success omitted choices")
			}
			if !validUsage(out.Usage.Prompt, out.Usage.Completion, out.Usage.Total) || out.Choices[0].Message.Role != RoleAssistant {
				return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, resp.StatusCode, "upstream returned invalid fields")
			}
			m := Message{Role: out.Choices[0].Message.Role, Content: out.Choices[0].Message.Content}
			for _, tc := range out.Choices[0].Message.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, ToolCall{ID: tc.ID, Name: wn.original(tc.Function.Name), Arguments: json.RawMessage(tc.Function.Arguments)})
			}
			return Response{Message: m, Usage: normalizeUsage(out.Usage.Prompt, out.Usage.Completion, out.Usage.Total), Reasoning: out.Choices[0].Message.ReasoningContent}, nil
		}
		return a.readStream(resp.Body, emit, wn)
	}
	return Response{}, last
}

func openAIMessages(in Request, wn *wireNames) []openAIMessage {
	out := make([]openAIMessage, 0, len(in.Messages))
	lastUser := -1
	for _, m := range in.Messages {
		x := openAIMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			c := openAIToolCall{ID: tc.ID, Type: "function"}
			c.Function.Name, c.Function.Arguments = wn.wire(tc.Name), string(tc.Arguments)
			x.ToolCalls = append(x.ToolCalls, c)
		}
		out = append(out, x)
		if m.Role == RoleUser {
			lastUser = len(out) - 1
		}
	}
	if lastUser < 0 || len(in.Images) == 0 {
		return out
	}
	parts := []openAIContentPart{{Type: "text", Text: in.Messages[lastUser].Content}}
	for _, image := range in.Images {
		imageURL := &struct {
			URL string `json:"url"`
		}{URL: "data:" + image.MIME + ";base64," + base64.StdEncoding.EncodeToString(image.Data)}
		parts = append(parts, openAIContentPart{Type: "image_url", ImageURL: imageURL})
	}
	out[lastUser].Content = parts
	return out
}

func (a *OpenAI) readStream(body io.ReadCloser, emit func(Delta) error, wn *wireNames) (Response, error) {
	defer body.Close()
	var out Response
	out.Message.Role = RoleAssistant
	type partial struct{ id, name, args string }
	calls := map[int]*partial{}
	for {
		event, eof, err := a.c.ReadSSE(body)
		if err != nil {
			return out, classify(err)
		}
		if eof {
			break
		}
		_, data := sseData(event)
		if data == "[DONE]" {
			break
		}
		var chunk openAIResponse
		if e := compatibleJSON(strings.NewReader(data), &chunk); e != nil {
			return out, e
		}
		if len(chunk.Choices) > 0 {
			reasoning := chunk.Choices[0].Delta.ReasoningContent
			out.Reasoning += reasoning
			if emit != nil && reasoning != "" {
				if e := emit(Delta{Reasoning: reasoning}); e != nil {
					return out, e
				}
			}
			text := chunk.Choices[0].Delta.Content
			out.Message.Content += text
			if emit != nil && text != "" {
				if e := emit(Delta{Text: text}); e != nil {
					return out, e
				}
			}
			for _, tc := range chunk.Choices[0].Delta.ToolCalls {
				p := calls[tc.Index]
				if p == nil {
					p = &partial{}
					calls[tc.Index] = p
				}
				if tc.ID != "" {
					p.id = tc.ID
				}
				p.name += tc.Function.Name
				p.args += tc.Function.Arguments
			}
		}
		u := normalizeUsage(chunk.Usage.Prompt, chunk.Usage.Completion, chunk.Usage.Total)
		if u.TotalTokens > 0 {
			out.Usage = u
			if emit != nil {
				if e := emit(Delta{Usage: &u}); e != nil {
					return out, e
				}
			}
		}
	}
	for i := 0; i < len(calls); i++ {
		p := calls[i]
		if p == nil || p.id == "" || p.name == "" || !json.Valid([]byte(p.args)) {
			return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "invalid tool call")
		}
		tc := ToolCall{ID: p.id, Name: wn.original(p.name), Arguments: json.RawMessage(p.args)}
		out.Message.ToolCalls = append(out.Message.ToolCalls, tc)
		if emit != nil {
			if e := emit(Delta{ToolCall: &tc}); e != nil {
				return out, e
			}
		}
	}
	return out, nil
}

func (a *OpenAI) Discover(ctx context.Context, secret []byte) (Discovery, error) {
	req, err := a.c.NewRequest(ctx, http.MethodGet, "models", nil)
	if err != nil {
		return Discovery{}, classify(err)
	}
	resp, err := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
	if err != nil {
		return Discovery{}, classify(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Discovery{}, statusError(resp.StatusCode)
	}
	var x struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := compatibleJSON(resp.Body, &x); err != nil {
		return Discovery{}, err
	}
	set := map[string]bool{}
	for _, m := range x.Data {
		if strings.TrimSpace(m.ID) != "" && len(m.ID) <= 512 {
			set[m.ID] = true
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > a.o.MaxModels {
		ids = ids[:a.o.MaxModels]
	}
	d := Discovery{}
	for _, id := range ids {
		d.Models = append(d.Models, Model{ID: id})
	}
	return d, nil
}
