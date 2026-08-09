package gateway

import (
	"context"
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
	Model         string    `json:"model"`
	Messages      []Message `json:"messages"`
	MaxTokens     int       `json:"max_tokens,omitempty"`
	Stream        bool      `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}
type openAIResponse struct {
	Choices []struct {
		Message Message `json:"message"`
		Delta   struct {
			Content string `json:"content,omitempty"`
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

func (a *OpenAI) run(ctx context.Context, secret []byte, in Request, stream bool, emit func(Delta) error) (Response, error) {
	p := openAIRequest{Model: in.Model, Messages: in.Messages, MaxTokens: in.MaxTokens, Stream: stream}
	if stream {
		p.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{true}
	}
	var last error
	maxAttempts := attempts(a.o, in, stream)
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
			resp.Body.Close()
			last = statusError(resp.StatusCode)
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
			err = strictJSON(resp.Body, &out)
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
			return Response{Message: out.Choices[0].Message, Usage: normalizeUsage(out.Usage.Prompt, out.Usage.Completion, out.Usage.Total)}, nil
		}
		return a.readStream(resp.Body, emit)
	}
	return Response{}, last
}

func (a *OpenAI) readStream(body io.ReadCloser, emit func(Delta) error) (Response, error) {
	defer body.Close()
	var out Response
	out.Message.Role = RoleAssistant
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
		if e := strictJSON(strings.NewReader(data), &chunk); e != nil {
			return out, e
		}
		if len(chunk.Choices) > 0 {
			text := chunk.Choices[0].Delta.Content
			out.Message.Content += text
			if emit != nil && text != "" {
				if e := emit(Delta{Text: text}); e != nil {
					return out, e
				}
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
	if err := strictJSON(resp.Body, &x); err != nil {
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
