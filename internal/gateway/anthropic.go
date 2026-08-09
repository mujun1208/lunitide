package gateway

import (
	"context"
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
	Model     string    `json:"model"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
}
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Usage struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
}

func anthropicPayload(in Request, stream bool) anthropicRequest {
	p := anthropicRequest{Model: in.Model, MaxTokens: in.MaxTokens, Stream: stream}
	if p.MaxTokens <= 0 {
		p.MaxTokens = 1
	}
	var sys []string
	for _, m := range in.Messages {
		if m.Role == RoleSystem {
			sys = append(sys, m.Content)
		} else {
			p.Messages = append(p.Messages, m)
		}
	}
	p.System = strings.Join(sys, "\n\n")
	return p
}
func (a *Anthropic) Complete(ctx context.Context, s []byte, in Request) (Response, error) {
	return a.run(ctx, s, in, false, nil)
}
func (a *Anthropic) Stream(ctx context.Context, s []byte, in Request, emit func(Delta) error) (Response, error) {
	return a.run(ctx, s, in, true, emit)
}
func (a *Anthropic) run(ctx context.Context, secret []byte, in Request, stream bool, emit func(Delta) error) (Response, error) {
	p := anthropicPayload(in, stream)
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
			return a.readStream(resp.Body, emit)
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
		if !validUsage(x.Usage.Input, x.Usage.Output, x.Usage.Input+x.Usage.Output) {
			return Response{}, safeError("MALFORMED_RESPONSE", StageDecode, resp.StatusCode, "upstream returned invalid fields")
		}
		var text strings.Builder
		for _, c := range x.Content {
			if c.Type == "text" {
				text.WriteString(c.Text)
			}
		}
		return Response{Message: Message{Role: RoleAssistant, Content: text.String()}, Usage: normalizeUsage(x.Usage.Input, x.Usage.Output, 0)}, nil
	}
	return Response{}, safeError("RETRY_EXHAUSTED", StageConnect, 0, "upstream unavailable")
}
func (a *Anthropic) readStream(body io.ReadCloser, emit func(Delta) error) (Response, error) {
	defer body.Close()
	out := Response{Message: Message{Role: RoleAssistant}}
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
		if typ == "message_start" || typ == "message_delta" {
			u := normalizeUsage(x.Usage.Input, x.Usage.Output, 0)
			if x.Usage.Input > 0 {
				out.Usage.InputTokens = x.Usage.Input
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
	return out, nil
}
func (a *Anthropic) Discover(ctx context.Context, secret []byte) (Discovery, error) {
	return Discovery{Unsupported: true, Warning: "Anthropic does not provide a portable model-list endpoint; configure a model explicitly"}, nil
}
