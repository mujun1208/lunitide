package llmadapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestOpenAICompatibleCacheUsageCompleteAndStream(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		cached      int
		reported    bool
	}{
		{"deepseek", `,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20`, 80, true},
		{"openai", `,"prompt_tokens_details":{"cached_tokens":60,"audio_tokens":0}`, 60, true},
		{"reported-zero", `,"prompt_cache_hit_tokens":0`, 0, true},
		{"missing", "", 0, false},
		{"empty-details", `,"prompt_tokens_details":{}`, 0, false},
		{"invalid-counter", `,"prompt_cache_hit_tokens":101`, 0, false},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				usage := fmt.Sprintf(`{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107%s}`, tc.extra)
				body := `{"id":"real-response","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"done"}}],"usage":` + usage + `}`
				if stream {
					body = "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n" + "data: {\"choices\":[],\"usage\":" + usage + "}\n\n" + "data: {\"choices\":[],\"usage\":" + usage + "}\n\ndata: [DONE]\n\n"
				}
				f := &fakeConnector{responses: []*http.Response{response(200, body)}}
				a := NewOpenAI(f, Options{})
				var out Response
				var err error
				var frames []Usage
				if stream {
					out, err = a.Stream(context.Background(), nil, Request{Model: "model"}, func(d Delta) error {
						if d.Usage != nil {
							frames = append(frames, *d.Usage)
						}
						return nil
					})
				} else {
					out, err = a.Complete(context.Background(), nil, Request{Model: "model"})
				}
				if err != nil {
					t.Fatal(err)
				}
				want := Usage{InputTokens: 100, OutputTokens: 7, TotalTokens: 107, CachedInputTokens: tc.cached, CacheUsageReported: tc.reported}
				if out.Usage != want || out.Message.Content != "done" {
					t.Fatalf("actual: %+v want: %+v", out, want)
				}
				if stream && (len(frames) != 2 || frames[0] != want || frames[1] != want) {
					t.Fatalf("usage snapshots changed or doubled: %+v", frames)
				}
			})
		}
	}
}

func TestAnthropicRealWireCacheUsageDoesNotDoubleCountCreationDetails(t *testing.T) {
	body := `{"id":"msg_real","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":0},"server_tool_use":null,"service_tier":"standard","inference_geo":"not_available"}}`
	f := &fakeConnector{responses: []*http.Response{response(200, body)}}
	out, err := NewAnthropic(f, Options{}).Complete(context.Background(), nil, Request{Model: "claude"})
	want := Usage{InputTokens: 115, OutputTokens: 2, TotalTokens: 117, CachedInputTokens: 100, CacheWriteInputTokens: 5, CacheUsageReported: true}
	if err != nil || out.Usage != want {
		t.Fatalf("real complete: %+v %v", out, err)
	}
}

func TestAnthropicRealStreamRetainsNestedCacheAndCumulativeOutput(t *testing.T) {
	body := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_real","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":1,"cache_read_input_tokens":40,"cache_creation_input_tokens":5}}}` + "\n\n" +
		"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}` + "\n\n" +
		"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":null,"stop_sequence":null},"usage":{"output_tokens":2}}` + "\n\n" +
		"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":null,"stop_sequence":null},"usage":{"output_tokens":2}}` + "\n\n" +
		"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}` + "\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	f := &fakeConnector{responses: []*http.Response{response(200, body)}}
	var frames []Usage
	out, err := NewAnthropic(f, Options{}).Stream(context.Background(), nil, Request{Model: "claude"}, func(d Delta) error {
		if d.Usage != nil {
			frames = append(frames, *d.Usage)
		}
		return nil
	})
	want := Usage{InputTokens: 48, OutputTokens: 3, TotalTokens: 51, CachedInputTokens: 40, CacheWriteInputTokens: 5, CacheUsageReported: true}
	if err != nil || out.Usage != want || out.Message.Content != "done" {
		t.Fatalf("real stream: %+v %v", out, err)
	}
	if len(frames) != 4 || frames[3] != want || frames[1] != frames[2] {
		t.Fatalf("cumulative output or nested input lost: %+v", frames)
	}
	for _, u := range frames {
		if u.InputTokens != 48 || u.CachedInputTokens != 40 || u.CacheWriteInputTokens != 5 || !u.CacheUsageReported {
			t.Fatalf("cache information erased by delta: %+v", u)
		}
	}
}

func TestAnthropicMissingCacheMetadataDoesNotReportZeroHits(t *testing.T) {
	for _, extra := range []string{"", `,"cache_read_input_tokens":0,"cache_creation_input_tokens":0`} {
		f := &fakeConnector{responses: []*http.Response{response(200, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":2`+extra+`}}`)}}
		out, err := NewAnthropic(f, Options{}).Complete(context.Background(), nil, Request{Model: "claude"})
		if err != nil || out.Usage.CacheUsageReported != (extra != "") {
			t.Fatalf("absent vs explicit zero: %+v %v", out, err)
		}
	}
}

func TestOpenAIUsageSurvivesCancelledTextFromSameFrame(t *testing.T) {
	body := "data: " + `{"choices":[{"delta":{"content":"partial"}}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_cache_hit_tokens":80}}` + "\n\ndata: [DONE]\n\n"
	f := &fakeConnector{responses: []*http.Response{response(200, body)}}
	out, err := NewOpenAI(f, Options{}).Stream(context.Background(), nil, Request{Model: "model"}, func(d Delta) error {
		if d.Text != "" {
			return context.Canceled
		}
		return nil
	})
	want := Usage{InputTokens: 100, OutputTokens: 7, TotalTokens: 107, CachedInputTokens: 80, CacheUsageReported: true}
	if !errors.Is(err, context.Canceled) || out.Usage != want || out.Message.Content != "partial" {
		t.Fatalf("received usage lost on cancel: %+v %v", out, err)
	}
}

func TestAnthropicPartialCacheMetadataKeepsKnownCountsWithoutClaimingComplete(t *testing.T) {
	for _, tc := range []struct {
		extra       string
		read, write int
	}{
		{`,"cache_read_input_tokens":7`, 7, 0},
		{`,"cache_creation_input_tokens":7`, 0, 7},
	} {
		f := &fakeConnector{responses: []*http.Response{response(200, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":2`+tc.extra+`}}`)}}
		out, err := NewAnthropic(f, Options{}).Complete(context.Background(), nil, Request{Model: "claude"})
		if err != nil || out.Usage.CacheUsageReported || out.Usage.CachedInputTokens != tc.read || out.Usage.CacheWriteInputTokens != tc.write {
			t.Fatalf("partial accounting was discarded or claimed complete: %+v %v", out.Usage, err)
		}
	}
}

func TestCacheZeroWithoutReportedInputDoesNotClaimComplete(t *testing.T) {
	for _, stream := range []bool{false, true} {
		body := `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"completion_tokens":2,"prompt_cache_hit_tokens":0}}`
		if stream {
			body = "data: " + `{"choices":[{"delta":{"content":"ok"}}],"usage":{"completion_tokens":2,"prompt_cache_hit_tokens":0}}` + "\n\ndata: [DONE]\n\n"
		}
		f := &fakeConnector{responses: []*http.Response{response(200, body)}}
		a := NewOpenAI(f, Options{})
		var out Response
		var err error
		if stream {
			out, err = a.Stream(context.Background(), nil, Request{}, nil)
		} else {
			out, err = a.Complete(context.Background(), nil, Request{})
		}
		if err != nil || out.Message.Content != "ok" || out.Usage.CacheUsageReported {
			t.Fatalf("absent input invented cache completeness: %+v %v", out, err)
		}
	}
	f := &fakeConnector{responses: []*http.Response{response(200, `{"content":[{"type":"text","text":"ok"}],"usage":{"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`)}}
	if out, err := NewAnthropic(f, Options{}).Complete(context.Background(), nil, Request{}); err != nil || out.Usage.CacheUsageReported {
		t.Fatalf("Anthropic absent input: %+v %v", out, err)
	}
}
