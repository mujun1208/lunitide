package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeConnector struct {
	mu        sync.Mutex
	responses []*http.Response
	requests  []*http.Request
	authSeen  []bool
}

func (f *fakeConnector) NewRequest(ctx context.Context, method, p string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, "https://upstream.test/v1/"+p, body)
}
func (f *fakeConnector) Do(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authSeen = append(f.authSeen, r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "")
	clone := r.Clone(r.Context())
	clone.Header.Del("Authorization")
	clone.Header.Del("x-api-key")
	f.requests = append(f.requests, clone)
	x := f.responses[0]
	f.responses = f.responses[1:]
	return x, nil
}
func (f *fakeConnector) ReadSSE(r io.Reader) ([]byte, bool, error) {
	var b strings.Builder
	one := []byte{0}
	lineEmpty := true
	for {
		n, err := r.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				if lineEmpty {
					return []byte(b.String()), false, nil
				}
				b.WriteByte('\n')
				lineEmpty = true
			} else {
				b.WriteByte(one[0])
				lineEmpty = false
			}
		}
		if err != nil {
			return []byte(b.String()), true, nil
		}
	}
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestVersionPath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"https://x.test", "v1"}, {"https://x.test/root", "v1"}, {"https://x.test/v1", ""}, {"https://x.test/root/v1/", ""}} {
		if got := versionPath(tc.in); got != tc.want {
			t.Errorf("versionPath(%q)=%q", tc.in, got)
		}
	}
}

func TestOpenAIContractAndDiscovery(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(200, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), response(200, `{"data":[{"id":"z"},{"id":"a"},{"id":"a"}]}`)}}
	a := NewOpenAI(f, Options{})
	secret := []byte("CANARY-key")
	out, err := a.Complete(context.Background(), secret, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil || out.Message.Content != "ok" || out.Usage.TotalTokens != 3 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if got := f.requests[0].URL.Path; got != "/v1/chat/completions" {
		t.Fatalf("path=%q", got)
	}
	if !f.authSeen[0] || f.requests[0].Header.Get("Authorization") != "" {
		t.Fatal("bearer missing")
	}
	d, err := a.Discover(context.Background(), secret)
	if err != nil || len(d.Models) != 2 || d.Models[0].ID != "a" {
		t.Fatalf("discovery=%+v err=%v", d, err)
	}
}

func TestOpenAIConnectionTestAcceptsAny2xxBodyButPreservesAuthFailures(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(200, `not a chat completion`), response(401, `ignored secret body`)}}
	a := NewOpenAI(f, Options{})
	request := Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "ping"}}, MaxTokens: 1}
	if err := a.TestConnection(context.Background(), []byte("CANARY-key"), request); err != nil {
		t.Fatalf("2xx connectivity response rejected: %v", err)
	}
	err := a.TestConnection(context.Background(), []byte("CANARY-key"), request)
	ge := classify(err)
	if ge.HTTPStatus != http.StatusUnauthorized || ge.Code != "HTTP_401" || len(f.requests) != 2 || !f.authSeen[0] || !f.authSeen[1] {
		t.Fatalf("auth failure not preserved: error=%+v requests=%d auth=%v", ge, len(f.requests), f.authSeen)
	}
}

func TestAnthropicContractSystemAndStream(t *testing.T) {
	stream := "event: message_start\ndata: {\"content\":[],\"delta\":{\"type\":\"\",\"text\":\"\"},\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}\n\nevent: content_block_delta\ndata: {\"content\":[],\"delta\":{\"type\":\"text_delta\",\"text\":\"yo\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\nevent: message_delta\ndata: {\"content\":[],\"delta\":{\"type\":\"\",\"text\":\"\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"content\":[],\"delta\":{\"type\":\"\",\"text\":\"\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\n"
	f := &fakeConnector{responses: []*http.Response{response(200, stream)}}
	a := NewAnthropic(f, Options{})
	var deltas string
	out, err := a.Stream(context.Background(), []byte("key"), Request{Model: "claude", Messages: []Message{{Role: RoleSystem, Content: "rules"}, {Role: RoleUser, Content: "hi"}}}, func(d Delta) error { deltas += d.Text; return nil })
	if err != nil || out.Message.Content != "yo" || deltas != "yo" || out.Usage.TotalTokens != 3 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	r := f.requests[0]
	if r.URL.Path != "/v1/messages" || !f.authSeen[0] || r.Header.Get("x-api-key") != "" || r.Header.Get("anthropic-version") != anthropicVersion {
		t.Fatal("anthropic contract headers/path")
	}
	b, _ := io.ReadAll(r.Body)
	if strings.Contains(string(b), `"role":"system"`) || !strings.Contains(string(b), `"system":"rules"`) {
		t.Fatalf("system not lifted: %s", b)
	}
	d, _ := a.Discover(context.Background(), nil)
	if !d.Unsupported || d.Warning == "" || len(d.Models) != 0 {
		t.Fatalf("unsupported=%+v", d)
	}
}

func TestStrictMalformedStatusesRetryAndNoStreamRetry(t *testing.T) {
	for _, status := range []int{401, 403, 404, 408, 413} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := &fakeConnector{responses: []*http.Response{response(status, "secret body")}}
			_, err := NewOpenAI(f, Options{}).Complete(context.Background(), []byte("CANARY"), Request{})
			ge := classify(err)
			if ge.HTTPStatus != status || strings.Contains(err.Error(), "CANARY") {
				t.Fatalf("err=%v", err)
			}
		})
	}
	f := &fakeConnector{responses: []*http.Response{response(500, ""), response(429, ""), response(200, `{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`)}}
	_, err := NewOpenAI(f, Options{RetryBase: time.Millisecond}).Complete(context.Background(), nil, Request{})
	if err == nil || len(f.requests) != 1 {
		t.Fatalf("retry calls=%d err=%v", len(f.requests), err)
	}
	f = &fakeConnector{responses: []*http.Response{response(200, `{"choices":[],"usage":{},"unknown":1}`)}}
	_, err = NewOpenAI(f, Options{}).Complete(context.Background(), nil, Request{})
	if err == nil {
		t.Fatal("missing choices accepted")
	}
	f = &fakeConnector{responses: []*http.Response{response(200, `{"id":"deepseek-chat","object":"chat.completion","created":1730000000,"model":"deepseek-chat","system_fingerprint":"fp_x","choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"reasoning","tool_calls":[]},"logprobs":null,"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"prompt_cache_hit_tokens":1,"prompt_cache_miss_tokens":1}}`)}}
	out, err := NewOpenAI(f, Options{}).Complete(context.Background(), nil, Request{})
	if err != nil || out.Message.Content != "ok" || out.Usage.TotalTokens != 3 {
		t.Fatalf("DeepSeek extension response rejected: out=%+v err=%v", out, err)
	}
	for _, body := range []string{`{"choices":[`, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{}} {}`} {
		f = &fakeConnector{responses: []*http.Response{response(200, body)}}
		if _, err = NewOpenAI(f, Options{}).Complete(context.Background(), nil, Request{}); err == nil {
			t.Fatalf("malformed/trailing JSON accepted: %q", body)
		}
	}
	for _, body := range []string{`{"choices":[{"message":{"role":"user","content":"no"}}],"usage":{}}`, `{"choices":[{"message":{"role":"assistant","content":"no"}}],"usage":{"prompt_tokens":-1}}`} {
		f = &fakeConnector{responses: []*http.Response{response(200, body)}}
		if _, err = NewOpenAI(f, Options{}).Complete(context.Background(), nil, Request{}); err == nil {
			t.Fatalf("invalid semantic fields accepted: %q", body)
		}
	}
	f = &fakeConnector{responses: []*http.Response{response(200, "data: {bad}\n\n"), response(200, "data: [DONE]\n\n")}}
	_, err = NewOpenAI(f, Options{}).Stream(context.Background(), nil, Request{}, nil)
	if err == nil || len(f.requests) != 1 {
		t.Fatalf("stream retried calls=%d err=%v", len(f.requests), err)
	}
}

func TestStrictJSONRejectsUnknownWhileCompatibleJSONAllowsIt(t *testing.T) {
	type payload struct {
		Known string `json:"known"`
	}
	var got payload
	if err := strictJSON(strings.NewReader(`{"known":"ok","deepseek_extension":true}`), &got); err == nil {
		t.Fatal("strict JSON accepted an arbitrary unknown field")
	}
	if err := compatibleJSON(strings.NewReader(`{"known":"ok","deepseek_extension":true}`), &got); err != nil || got.Known != "ok" {
		t.Fatalf("compatible JSON rejected additive field: got=%+v err=%v", got, err)
	}
	for _, body := range []string{`{"known":`, `{"known":"ok"} {}`} {
		if err := compatibleJSON(strings.NewReader(body), &got); err == nil {
			t.Fatalf("compatible JSON accepted malformed/trailing input: %q", body)
		}
	}
}

func TestAnthropicResponseRemainsStrict(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(200, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1},"unknown":true}`)}}
	if _, err := NewAnthropic(f, Options{}).Complete(context.Background(), nil, Request{}); err == nil {
		t.Fatal("Anthropic accepted an arbitrary unknown field")
	}
}

func TestCancelAndRequestBudget(t *testing.T) {
	f := &fakeConnector{}
	a := NewOpenAI(f, Options{MaxRequestBytes: 8})
	_, err := a.Complete(context.Background(), nil, Request{Model: strings.Repeat("x", 100)})
	if err == nil || len(f.requests) != 0 {
		t.Fatal("oversize request sent")
	}
	ge := classify(err)
	if ge.Code != "REQUEST_TOO_LARGE" || ge.Stage != StageDecode || ge.HTTPStatus != 0 {
		t.Fatalf("oversize request classification=%+v", ge)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := waitRetry(ctx, time.Hour); e == nil {
		t.Fatal("cancel ignored")
	}
	_, _ = url.Parse("https://unused")
}

func TestVisionPayloadContracts(t *testing.T) {
	in := Request{Model: "vision", Messages: []Message{{Role: RoleUser, Content: "inspect"}}, Images: []Image{{MIME: "image/png", Data: []byte{1, 2, 3}}}}
	openBody, err := json.Marshal(openAIRequest{Model: in.Model, Messages: openAIMessages(in)})
	if err != nil || !strings.Contains(string(openBody), `"type":"image_url"`) || !strings.Contains(string(openBody), `data:image/png;base64,AQID`) {
		t.Fatalf("OpenAI vision payload=%s err=%v", openBody, err)
	}
	anthropicBody, err := json.Marshal(anthropicPayload(in, false))
	if err != nil || !strings.Contains(string(anthropicBody), `"type":"image"`) || !strings.Contains(string(anthropicBody), `"media_type":"image/png"`) || !strings.Contains(string(anthropicBody), `"data":"AQID"`) {
		t.Fatalf("Anthropic vision payload=%s err=%v", anthropicBody, err)
	}
	plain, _ := json.Marshal(openAIRequest{Model: "plain", Messages: openAIMessages(Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})})
	if !strings.Contains(string(plain), `"content":"hi"`) {
		t.Fatalf("plain payload compatibility lost: %s", plain)
	}
}

func TestAnthropicStreamingToolUseFragments(t *testing.T) {
	body := strings.Join([]string{
		"event: content_block_start\ndata: {\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"workspace.write\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"a.txt\\\",\"}}\n\n",
		"event: content_block_delta\ndata: {\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"content\\\":\\\"ok\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"index\":1}\n\n", "event: message_stop\ndata: {}\n\n"}, "")
	f := &fakeConnector{responses: []*http.Response{response(200, body)}}
	var emitted *ToolCall
	out, err := NewAnthropic(f, Options{}).Stream(context.Background(), nil, Request{MaxTokens: 1}, func(d Delta) error {
		if d.ToolCall != nil {
			emitted = d.ToolCall
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Message.ToolCalls) != 1 || emitted == nil || string(out.Message.ToolCalls[0].Arguments) != `{"path":"a.txt","content":"ok"}` {
		t.Fatalf("out=%+v emitted=%+v", out, emitted)
	}
}
