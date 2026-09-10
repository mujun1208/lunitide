package llmadapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOpenAISendsObtainedReasoningAndDoesNotInventIt(t *testing.T) {
	withHistory := Request{Messages: []Message{
		{Role: RoleUser, Content: "q"},
		{Role: RoleAssistant, Content: "answer", ReasoningContent: "private chain"},
	}}
	body, err := json.Marshal(openAIRequest{Model: "m", Messages: openAIMessages(withHistory, nil, false)})
	if err != nil || !strings.Contains(string(body), `"reasoning_content":"private chain"`) {
		t.Fatalf("obtained reasoning omitted: %s err=%v", body, err)
	}
	plain := Request{Messages: []Message{{Role: RoleAssistant, Content: "answer"}}}
	plainBody, err := json.Marshal(openAIRequest{Model: "m", Messages: openAIMessages(plain, nil, false)})
	if err != nil || strings.Contains(string(plainBody), "reasoning_content") || strings.Contains(string(plainBody), "private") {
		t.Fatalf("invented reasoning: %s err=%v", plainBody, err)
	}
}

func TestOpenAIReasoningContentStaysSeparateFromAnswer(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(200, `{"choices":[{"message":{"role":"assistant","content":"answer","reasoning_content":"private chain"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)}}
	out, err := NewOpenAI(f, Options{}).Complete(context.Background(), nil, Request{})
	if err != nil || out.Message.Content != "answer" || out.Reasoning != "private chain" || out.Message.ReasoningContent != "private chain" {
		t.Fatalf("out=%+v err=%v", out, err)
	}

	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think \"}}],\"usage\":{}}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}],\"usage\":{}}\n\ndata: [DONE]\n\n"
	f = &fakeConnector{responses: []*http.Response{response(200, stream)}}
	var answer, reasoning string
	out, err = NewOpenAI(f, Options{}).Stream(context.Background(), nil, Request{}, func(d Delta) error { answer += d.Text; reasoning += d.Reasoning; return nil })
	if err != nil || answer != "answer" || reasoning != "think " || out.Message.Content != "answer" || out.Reasoning != "think " || out.Message.ReasoningContent != "think " {
		t.Fatalf("out=%+v answer=%q reasoning=%q err=%v", out, answer, reasoning, err)
	}
}

func TestAnthropicOfficialThinkingDeltaStaysSeparateAndRequestUnchanged(t *testing.T) {
	stream := "event: content_block_delta\ndata: {\"index\":0,\"content\":[],\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"consider\"},\"usage\":{}}\n\nevent: content_block_delta\ndata: {\"index\":1,\"content\":[],\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"},\"usage\":{}}\n\nevent: message_stop\ndata: {\"content\":[],\"delta\":{},\"usage\":{}}\n\n"
	f := &fakeConnector{responses: []*http.Response{response(200, stream)}}
	var answer, reasoning string
	out, err := NewAnthropic(f, Options{}).Stream(context.Background(), nil, Request{}, func(d Delta) error { answer += d.Text; reasoning += d.Reasoning; return nil })
	if err != nil || answer != "answer" || reasoning != "consider" || out.Message.Content != "answer" || out.Reasoning != "consider" {
		t.Fatalf("out=%+v answer=%q reasoning=%q err=%v", out, answer, reasoning, err)
	}
	b, _ := io.ReadAll(f.requests[0].Body)
	if strings.Contains(string(b), `"thinking"`) {
		t.Fatalf("thinking request parameter enabled implicitly: %s", b)
	}
}
