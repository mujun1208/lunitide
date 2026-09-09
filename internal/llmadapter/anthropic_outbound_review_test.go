package llmadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func TestAnthropicOutboundSystemAndParallelResultContract(t *testing.T) {
	// Inspect the actual HTTP body: a payload-only test previously blessed
	// system blocks that omitted the API's required type discriminator.
	for _, stream := range []bool{false, true} {
		name := "complete"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			in := Request{
				Model: "claude", MaxTokens: 32,
				Tools: []ToolDefinition{
					{Name: "workspace.read", Description: "Read a file", Schema: json.RawMessage(`{"type":"object"}`)},
					{Name: "workspace.search", Description: "Search files", Schema: json.RawMessage(`{"type":"object"}`)},
				},
				Messages: []Message{
					{Role: RoleSystem, Content: "静态规则\n保留原文。"},
					{Role: RoleSystem, Content: "本轮上下文"},
					{Role: RoleUser, Content: "读取并查询"},
					{Role: RoleAssistant, Content: "检查两个来源", ToolCalls: []ToolCall{
						{ID: "call_read", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"a.json"}`)},
						{ID: "call_search", Name: "workspace.search", Arguments: json.RawMessage(`{"query":"标题"}`)},
					}},
					{Role: RoleTool, ToolCallID: "call_read", Content: "{ \"exact\" : 9007199254740993 }"},
					{Role: RoleTool, ToolCallID: "call_search", Content: "a.json\n标题"},
					{Role: RoleUser, Content: "也参考这张图"},
				},
				Images: []Image{{MIME: "image/png", Data: []byte("attachment fixture")}},
			}
			before, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			body := `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`
			if stream {
				body = "event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_stop\ndata: {}\n\n"
			}
			f := &fakeConnector{responses: []*http.Response{response(200, body)}}
			a := NewAnthropic(f, Options{})
			if stream {
				_, err = a.Stream(context.Background(), nil, in, func(Delta) error { return nil })
			} else {
				_, err = a.Complete(context.Background(), nil, in)
			}
			if err != nil || len(f.requests) != 1 {
				t.Fatalf("request failed: %v, requests=%d", err, len(f.requests))
			}
			wire, err := io.ReadAll(f.requests[0].Body)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				System []struct {
					Type         string                 `json:"type"`
					Text         string                 `json:"text"`
					CacheControl *struct{ Type string } `json:"cache_control"`
				} `json:"system"`
				Messages []struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(wire, &got); err != nil {
				t.Fatal(err)
			}
			if len(got.System) != 2 || len(got.Messages) != 4 {
				t.Fatalf("system/messages length = %d/%d, want 2/4", len(got.System), len(got.Messages))
			}
			for i, s := range got.System {
				if s.Type != "text" || s.Text != in.Messages[i].Content || s.CacheControl == nil || s.CacheControl.Type != "ephemeral" {
					t.Fatalf("invalid system text block %d: %+v", i, s)
				}
			}
			var uses, results, imageTurn []anthropicBlock
			for i, dst := range map[int]*[]anthropicBlock{1: &uses, 2: &results, 3: &imageTurn} {
				if err := json.Unmarshal(got.Messages[i].Content, dst); err != nil {
					t.Fatal(err)
				}
			}
			if got.Messages[1].Role != "assistant" || len(uses) != 3 || uses[1].Type != "tool_use" || uses[2].Type != "tool_use" {
				t.Fatalf("assistant parallel calls were damaged: %+v", uses)
			}
			if got.Messages[2].Role != "user" || len(results) != 2 {
				t.Fatalf("parallel results must share one user message: %+v", results)
			}
			for i, result := range results {
				original := in.Messages[i+4]
				if result.Type != "tool_result" || result.ToolUseID != original.ToolCallID || result.Content != original.Content {
					t.Fatalf("tool result %d lost identity or content: %+v", i, result)
				}
				if (result.CacheControl != nil) != (i == len(results)-1) {
					t.Fatalf("cache must mark only the result group tail: %+v", results)
				}
			}
			if got.Messages[3].Role != "user" || len(imageTurn) != 2 || imageTurn[0].Type != "image" || imageTurn[0].Source == nil || imageTurn[0].Source.Data != base64.StdEncoding.EncodeToString(in.Images[0].Data) || imageTurn[1].Text != in.Messages[6].Content {
				t.Fatalf("attachment no longer belongs to its user turn: %+v", imageTurn)
			}
			if imageTurn[0].CacheControl != nil || imageTurn[1].CacheControl != nil || countBreakpoints(string(wire)) != 4 {
				t.Fatalf("history cache moved to fresh input or exceeded four breakpoints: %s", wire)
			}
			after, _ := json.Marshal(in)
			if string(before) != string(after) {
				t.Fatal("outbound preparation mutated shared history or attachments")
			}
		})
	}
}

func TestAnthropicParallelResultsPreserveSeparateToolRounds(t *testing.T) {
	in := Request{Messages: []Message{
		{Role: RoleUser, Content: "first"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "first_a", Name: "read", Arguments: json.RawMessage(`{}`)},
			{ID: "first_b", Name: "read", Arguments: json.RawMessage(`{}`)},
		}},
		{Role: RoleTool, ToolCallID: "first_a", Content: "one"},
		{Role: RoleTool, ToolCallID: "first_b", Content: "two"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "second_a", Name: "read", Arguments: json.RawMessage(`{}`)},
			{ID: "second_b", Name: "read", Arguments: json.RawMessage(`{}`)},
		}},
		{Role: RoleTool, ToolCallID: "second_a", Content: "three"},
		{Role: RoleTool, ToolCallID: "second_b", Content: "four"},
	}}
	p := anthropicPayload(in, false, nil)
	if len(p.Messages) != 5 {
		t.Fatalf("two tool rounds should produce five messages, got %d", len(p.Messages))
	}
	for index, want := range map[int][]string{2: {"first_a", "first_b"}, 4: {"second_a", "second_b"}} {
		blocks, ok := p.Messages[index].Content.([]anthropicBlock)
		if !ok || p.Messages[index].Role != RoleUser {
			t.Fatalf("tool round missing at %d: %+v", index, p.Messages[index])
		}
		var ids []string
		for _, block := range blocks {
			ids = append(ids, block.ToolUseID)
			if block.Type != "tool_result" || block.CacheControl != nil {
				t.Fatalf("wrong tool result/cache in round at %d: %+v", index, block)
			}
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("tool rounds were reordered or merged: got %v, want %v", ids, want)
		}
	}
	// The complete previous assistant turn is stable, not a partial result
	// group from the current round. Its last tool_use is the cache boundary.
	prior := p.Messages[3].Content.([]anthropicBlock)
	if len(prior) != 2 || prior[0].CacheControl != nil || prior[1].CacheControl == nil {
		t.Fatalf("cache is not on the prior complete assistant turn: %+v", prior)
	}
}
