package llmadapter

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

func TestEfficiencyReviewConcurrentSharedHistoryAndWireNameCollisions(t *testing.T) {
	raw := "{\n  \"serial\": 9007199254740993, \"code\": \"a  b\\n\\t\", \"same\":1,\"same\":2\n}"
	request := Request{Model: "current", MaxTokens: 8192, DisableReasoning: true, Images: []Image{{MIME: "image/png", Data: []byte{0, 1, 2}}}, Tools: []ToolDefinition{
		{Name: "a_b", Description: "Existing literal", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "a.b", Description: "Existing dotted", Schema: json.RawMessage(`{"type":"object"}`)},
	}, Messages: []Message{
		{Role: RoleSystem, Content: "Preserve exact approved instructions\n  including spacing"},
		{Role: RoleUser, Content: raw},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "a_b", Arguments: json.RawMessage(`{"literal":"  retain  "}`)}}},
		{Role: RoleTool, ToolCallID: "call-1", Content: raw},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-2", Name: "a.b", Arguments: json.RawMessage(`{}`)}}},
		{Role: RoleTool, ToolCallID: "call-2", Content: raw},
	}}
	before, _ := json.Marshal(request)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := prepareEfficientRequest(request, Options{})
			if out.Messages[1].Content != raw || out.MaxTokens != 8192 || !out.DisableReasoning || !reflect.DeepEqual(out.Images, request.Images) {
				t.Error("protected user/budget/image fields changed")
			}
			for _, max := range []int{openAIToolNameMax, anthropicToolNameMax} {
				names := buildWireNames(out.Tools, max)
				for _, m := range out.Messages {
					for _, call := range m.ToolCalls {
						if names.original(names.wire(call.Name)) != call.Name {
							t.Error("tool routing changed")
						}
					}
				}
			}
			if out.Messages[3].ToolCallID != "call-1" || out.Messages[5].ToolCallID != "call-2" || len(out.Messages) != 6 {
				t.Error("temporal tool evidence collapsed")
			}
		}()
	}
	wg.Wait()
	after, _ := json.Marshal(request)
	if string(before) != string(after) {
		t.Fatal("shared caller history changed")
	}
	if got := prepareEfficientRequest(request, Options{DisableTokenEfficiency: true}); !reflect.DeepEqual(got, request) {
		t.Fatal("rollback changed request")
	}
}

func TestEfficiencyReviewMissingCallIdentityCannotAuthorizeResultReformatting(t *testing.T) {
	raw := "{\n  \"file-content\": \"retain original formatting\"\n}"
	request := Request{Messages: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "weather.get", Arguments: json.RawMessage(`{}`)}}},
		{Role: RoleTool, Content: raw},
	}}
	if got := prepareEfficientRequest(request, Options{}); got.Messages[1].Content != raw {
		t.Fatal("result without a call ID was treated as a verified weather envelope")
	}
}
