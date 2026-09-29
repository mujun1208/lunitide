package llmadapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// OpenAIResponses speaks the OpenAI Responses API (POST {base}/responses).
// It is the wire used by OpenAI, Volcengine Ark (standard, Agent Plan) and
// Bailian compatible-mode. Text and vision chat with function tools only;
// embeddings and media generation stay on the chat-completions adapter.
type OpenAIResponses struct {
	c Connector
	o Options
}

func NewOpenAIResponses(c Connector, o Options) *OpenAIResponses {
	return &OpenAIResponses{c: c, o: defaults(o)}
}

// responsesMinOutputTokens is the smallest max_output_tokens OpenAI accepts.
const responsesMinOutputTokens = 16

type responsesRequest struct {
	Model           string              `json:"model"`
	Input           []any               `json:"input"`
	MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
	Stream          bool                `json:"stream,omitempty"`
	Store           *bool               `json:"store,omitempty"`
	Tools           []responsesTool     `json:"tools,omitempty"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
	// Thinking is Ark's Responses-side switch ({"type":"enabled"|"disabled"}).
	// Only emitted when a profile compiled it; OpenAI rejects unknown keys.
	Thinking *openAIThinking `json:"thinking,omitempty"`
}
type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}
type responsesMessageItem struct {
	Role    Role `json:"role"`
	Content any  `json:"content"`
}
type responsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}
type responsesFunctionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type responsesFunctionOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type responsesUsage struct {
	Input        *int `json:"input_tokens"`
	Output       int  `json:"output_tokens"`
	Total        int  `json:"total_tokens"`
	InputDetails *struct {
		Cached *int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

func (w responsesUsage) normalized() Usage {
	u := normalizeUsage(usageCount(w.Input), w.Output, w.Total)
	if w.InputDetails != nil && w.InputDetails.Cached != nil && *w.InputDetails.Cached >= 0 && *w.InputDetails.Cached <= u.InputTokens {
		u.CachedInputTokens = *w.InputDetails.Cached
		u.CacheUsageReported = w.Input != nil
	}
	return u
}

func (w responsesUsage) valid() bool {
	return validUsage(usageCount(w.Input), w.Output, w.Total)
}

type responsesOutputItem struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Role    Role   `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responsesObject struct {
	ID                string                `json:"id"`
	Status            string                `json:"status"`
	Output            []responsesOutputItem `json:"output"`
	Usage             *responsesUsage       `json:"usage"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

// responsesStreamEvent is the union of the SSE frames we consume. Unknown
// event types are ignored so newer servers do not break older clients.
type responsesStreamEvent struct {
	Type        string               `json:"type"`
	OutputIndex *int                 `json:"output_index"`
	Delta       string               `json:"delta"`
	Arguments   string               `json:"arguments"`
	Item        *responsesOutputItem `json:"item"`
	Response    *responsesObject     `json:"response"`
}

func (a *OpenAIResponses) Complete(ctx context.Context, secret []byte, in Request) (Response, error) {
	return a.run(ctx, secret, in, false, nil)
}
func (a *OpenAIResponses) Stream(ctx context.Context, secret []byte, in Request, emit func(Delta) error) (Response, error) {
	return a.run(ctx, secret, in, true, emit)
}

func (a *OpenAIResponses) TestConnection(ctx context.Context, secret []byte, in Request) error {
	store := false
	p := responsesRequest{Model: in.Model, Input: responsesInput(in, nil), MaxOutputTokens: responsesMaxOutput(in.MaxTokens), Store: &store}
	body, err := marshalBounded(p, a.o.MaxRequestBytes)
	if err != nil {
		return err
	}
	req, err := a.c.NewRequest(ctx, http.MethodPost, "responses", body)
	if err != nil {
		return classify(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
	if err != nil {
		return uncertain(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusErrorReason(resp.StatusCode, boundedReason(resp.Body))
	}
	return nil
}

// Discover lists GET {base}/models like the chat-completions adapter; the
// Responses API shares the model catalog route.
func (a *OpenAIResponses) Discover(ctx context.Context, secret []byte) (Discovery, error) {
	return (&OpenAI{c: a.c, o: a.o}).Discover(ctx, secret)
}

func responsesMaxOutput(requested int) int {
	if requested <= 0 {
		return 0
	}
	if requested < responsesMinOutputTokens {
		return responsesMinOutputTokens
	}
	return requested
}

func buildResponsesRequest(in Request, wn *wireNames, stream bool) responsesRequest {
	maxTokens := in.MaxTokens
	if in.Effective != nil && in.Effective.MaxTokens > 0 {
		maxTokens = int(in.Effective.MaxTokens)
	}
	store := false
	p := responsesRequest{Model: in.Model, Input: responsesInput(in, wn), MaxOutputTokens: responsesMaxOutput(maxTokens), Stream: stream, Store: &store}
	// Thinking is Ark's chat-completions switch. OpenAI Responses rejects
	// unknown keys, so deep mode maps to reasoning.effort only.
	if in.Effective != nil {
		if eff := in.Effective.Effort; eff != "" && eff != "omitted" {
			p.Reasoning = &responsesReasoning{Effort: eff}
		}
	}
	for _, t := range in.Tools {
		p.Tools = append(p.Tools, responsesTool{Type: "function", Name: wn.wire(t.Name), Description: t.Description, Parameters: t.Schema})
	}
	return p
}

// responsesInput flattens chat history into Responses input items. System
// prompts stay ordered as system messages; assistant tool calls become
// function_call items and tool results become function_call_output items.
// Reasoning text is never replayed: providers require their own opaque
// encrypted reasoning items, which this ledger does not store.
func responsesInput(in Request, wn *wireNames) []any {
	out := make([]any, 0, len(in.Messages))
	lastUser := -1
	for _, m := range in.Messages {
		switch m.Role {
		case RoleTool:
			out = append(out, responsesFunctionOutputItem{Type: "function_call_output", CallID: m.ToolCallID, Output: m.Content})
		case RoleAssistant:
			if m.Content != "" || len(m.ToolCalls) == 0 {
				out = append(out, responsesMessageItem{Role: RoleAssistant, Content: m.Content})
			}
			for _, tc := range m.ToolCalls {
				name := tc.Name
				if wn != nil {
					name = wn.wire(tc.Name)
				}
				args := string(tc.Arguments)
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				out = append(out, responsesFunctionCallItem{Type: "function_call", CallID: tc.ID, Name: name, Arguments: args})
			}
		default:
			out = append(out, responsesMessageItem{Role: m.Role, Content: m.Content})
			if m.Role == RoleUser {
				lastUser = len(out) - 1
			}
		}
	}
	if lastUser < 0 || len(in.Images) == 0 {
		return out
	}
	user := out[lastUser].(responsesMessageItem)
	parts := []responsesContentPart{{Type: "input_text", Text: user.Content.(string)}}
	for _, image := range in.Images {
		mime := image.MIME
		if mime == "" {
			mime = "image/png"
		}
		parts = append(parts, responsesContentPart{Type: "input_image", ImageURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image.Data)})
	}
	user.Content = parts
	out[lastUser] = user
	return out
}

func (a *OpenAIResponses) run(ctx context.Context, secret []byte, in Request, stream bool, emit func(Delta) error) (Response, error) {
	in = attachEfficientRequest(in, a.o)
	wn := buildWireNames(in.Tools, openAIToolNameMax)
	p := buildResponsesRequest(in, wn, stream)
	var last error
	maxAttempts := attempts(a.o, in, stream)
	sanitized := false
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		body, err := marshalBoundedBytes(p, a.o.MaxRequestBytes)
		if err != nil {
			return Response{}, err
		}
		req, err := a.c.NewRequest(ctx, http.MethodPost, "responses", bytes.NewReader(body))
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
			// Same one-shot schema sanitation as chat/completions: compatible
			// gateways reject non-core JSON Schema keywords with 400.
			if resp.StatusCode == http.StatusBadRequest && len(p.Tools) > 0 && !sanitized {
				sanitized = true
				for i := range p.Tools {
					p.Tools[i].Parameters = sanitizeToolSchema(p.Tools[i].Parameters)
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
			var out responsesObject
			err = compatibleJSON(resp.Body, &out)
			resp.Body.Close()
			if err != nil {
				return Response{}, err
			}
			return responsesFinal(out, wn, resp.StatusCode)
		}
		return a.readStream(resp.Body, emit, wn, in.DisableReasoning)
	}
	return Response{}, last
}

// responsesFinal folds a complete response object into the neutral Response.
func responsesFinal(out responsesObject, wn *wireNames, status int) (Response, error) {
	if out.Status == "failed" {
		return Response{}, safeError("UPSTREAM_FAILED", StageDecode, status, "upstream reported a failed response")
	}
	if out.Usage != nil && !out.Usage.valid() {
		return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, status, "upstream returned invalid usage")
	}
	r := Response{Message: Message{Role: RoleAssistant}}
	if out.Usage != nil {
		r.Usage = out.Usage.normalized()
	}
	sawAny := false
	for _, item := range out.Output {
		switch item.Type {
		case "message":
			sawAny = true
			for _, part := range item.Content {
				if part.Type == "output_text" || part.Type == "text" {
					r.Message.Content += part.Text
				}
			}
		case "reasoning":
			sawAny = true
			for _, part := range item.Summary {
				r.Reasoning += part.Text
			}
			for _, part := range item.Content {
				if part.Type == "reasoning_text" {
					r.Reasoning += part.Text
				}
			}
		case "function_call":
			sawAny = true
			if item.CallID == "" || item.Name == "" || !json.Valid([]byte(item.Arguments)) {
				return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, status, "invalid tool call")
			}
			r.Message.ToolCalls = append(r.Message.ToolCalls, ToolCall{ID: item.CallID, Name: wn.original(item.Name), Arguments: json.RawMessage(item.Arguments)})
		}
	}
	if !sawAny && out.Status != "incomplete" {
		return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, status, "upstream success omitted output")
	}
	r.Message.ReasoningContent = r.Reasoning
	r.FinishReason = responsesFinishReason(out, len(r.Message.ToolCalls) > 0)
	return r, nil
}

func responsesFinishReason(out responsesObject, toolCalls bool) FinishReason {
	if out.Status == "incomplete" {
		reason := ""
		if out.IncompleteDetails != nil {
			reason = out.IncompleteDetails.Reason
		}
		switch reason {
		case "max_output_tokens", "max_tokens":
			return FinishReasonLength
		case "content_filter":
			return FinishReasonContentFilter
		}
		return FinishReasonOther
	}
	if toolCalls {
		return FinishReasonToolCalls
	}
	if out.Status == "completed" || out.Status == "" {
		return FinishReasonStop
	}
	return FinishReasonOther
}

func (a *OpenAIResponses) readStream(body io.ReadCloser, emit func(Delta) error, wn *wireNames, muteReasoning bool) (Response, error) {
	defer body.Close()
	out := Response{Message: Message{Role: RoleAssistant}}
	type partial struct{ id, name, args string }
	calls := map[int]*partial{}
	order := []int{}
	var final *responsesObject
	completed := false
	for {
		event, eof, err := a.c.ReadSSE(body)
		if err != nil {
			return out, classifyStreamError(err)
		}
		if eof {
			break
		}
		typ, data := sseData(event)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			completed = true
			break
		}
		if err := streamEventError(typ, data); err != nil {
			return out, err
		}
		var ev responsesStreamEvent
		if e := compatibleJSON(strings.NewReader(data), &ev); e != nil {
			return out, e
		}
		if ev.Type == "" {
			ev.Type = typ
		}
		switch ev.Type {
		case "response.output_text.delta":
			out.Message.Content += ev.Delta
			if emit != nil && ev.Delta != "" {
				if e := emit(Delta{Text: ev.Delta}); e != nil {
					return out, e
				}
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning.delta":
			out.Reasoning += ev.Delta
			if emit != nil && ev.Delta != "" && !muteReasoning {
				if e := emit(Delta{Reasoning: ev.Delta}); e != nil {
					return out, e
				}
			}
		case "response.output_item.added", "response.output_item.done":
			if ev.Item == nil || ev.Item.Type != "function_call" || ev.OutputIndex == nil {
				continue
			}
			p := calls[*ev.OutputIndex]
			if p == nil {
				p = &partial{}
				calls[*ev.OutputIndex] = p
				order = append(order, *ev.OutputIndex)
			}
			if ev.Item.CallID != "" {
				p.id = ev.Item.CallID
			}
			if ev.Item.Name != "" {
				p.name = ev.Item.Name
			}
			if ev.Type == "response.output_item.done" && ev.Item.Arguments != "" {
				p.args = ev.Item.Arguments
			}
		case "response.function_call_arguments.delta":
			if ev.OutputIndex == nil {
				continue
			}
			p := calls[*ev.OutputIndex]
			if p == nil {
				p = &partial{}
				calls[*ev.OutputIndex] = p
				order = append(order, *ev.OutputIndex)
			}
			p.args += ev.Delta
		case "response.function_call_arguments.done":
			if ev.OutputIndex == nil {
				continue
			}
			if p := calls[*ev.OutputIndex]; p != nil && ev.Arguments != "" {
				p.args = ev.Arguments
			}
		case "response.completed", "response.incomplete", "response.failed":
			completed = true
			if ev.Response != nil {
				final = ev.Response
			}
		}
		if final != nil {
			break
		}
	}
	if !completed {
		return out, safeError("STREAM_INCOMPLETE", StageStream, 0, "upstream stream ended before completion")
	}
	if final != nil {
		if final.Status == "failed" {
			return out, safeError("UPSTREAM_FAILED", StageStream, 0, "upstream reported a failed response")
		}
		if final.Usage != nil {
			if !final.Usage.valid() {
				return out, safeError("MALFORMED_RESPONSE", StageDecode, 0, "upstream returned invalid usage")
			}
			u := final.Usage.normalized()
			out.Usage = u
			if emit != nil && (u.TotalTokens > 0 || u.CacheUsageReported) {
				if e := emit(Delta{Usage: &u}); e != nil {
					return out, e
				}
			}
		}
		// The terminal object is authoritative for tool calls a compatible
		// server streamed without item events.
		for i, item := range final.Output {
			if item.Type != "function_call" {
				continue
			}
			p := calls[i]
			if p == nil {
				p = &partial{}
				calls[i] = p
				order = append(order, i)
			}
			if p.id == "" {
				p.id = item.CallID
			}
			if p.name == "" {
				p.name = item.Name
			}
			if p.args == "" {
				p.args = item.Arguments
			}
		}
	}
	for _, idx := range order {
		p := calls[idx]
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
	out.Message.ReasoningContent = out.Reasoning
	if final != nil {
		out.FinishReason = responsesFinishReason(*final, len(out.Message.ToolCalls) > 0)
	} else if len(out.Message.ToolCalls) > 0 {
		out.FinishReason = FinishReasonToolCalls
	} else {
		out.FinishReason = FinishReasonStop
	}
	return out, nil
}
