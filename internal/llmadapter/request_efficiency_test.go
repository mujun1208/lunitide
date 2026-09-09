package llmadapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/token"
)

func efficiencyFixture() Request {
	return Request{Model: "configured-model", MaxTokens: 32768, DisableReasoning: false, IdempotencyKey: "unchanged", Images: []Image{{MIME: "image/png", Data: []byte{0, 1, 2}}},
		Tools: []ToolDefinition{{Name: "weather.get", Description: "keep all fields", Schema: []byte(`{"type":"object"}`)}, {Name: "office.inspect", Schema: []byte(`{"type":"object","additionalProperties":false}`)}},
		Messages: []Message{
			{Role: RoleSystem, Content: "Do not drop safety rules.\n Preserved whitespace."},
			{Role: RoleUser, Content: "Keep 001234, 1.2300e-09 and all source files."},
			{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{ID: "weather-1", Name: "weather.get", Arguments: []byte("{\n  \"place\": \"合肥\"\n}")}}},
			{Role: RoleTool, ToolCallID: "weather-1", Content: "{\n  \"source\": \"原始来源\",\n  \"validAt\": \"2026-09-07\",\n  \"value\": 1.2300e-09\n}"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "read-1", Name: "workspace.read", Arguments: []byte(`{"path":"original.json"}`)}}},
			{Role: RoleTool, ToolCallID: "read-1", Content: "{\n  \"literalFile\": 9007199254740993\n}"},
			{Role: RoleUser, Content: "Continue with the unresolved requirement."},
		},
	}
}

func TestRequestEfficiencyProtectsTaskAndCaller(t *testing.T) {
	in := efficiencyFixture()
	original, _ := json.Marshal(in)
	out := prepareEfficientRequest(in, Options{})
	if out.Model != in.Model || out.MaxTokens != in.MaxTokens || out.DisableReasoning != in.DisableReasoning || out.IdempotencyKey != in.IdempotencyKey || !reflect.DeepEqual(out.Images, in.Images) {
		t.Fatal("request policy or images changed")
	}
	if len(out.Messages) != len(in.Messages) || out.Messages[3].ToolCallID != "weather-1" {
		t.Fatal("history / call pair lost")
	}
	for _, i := range []int{0, 1, 4, 5, 6} {
		if !reflect.DeepEqual(in.Messages[i], out.Messages[i]) {
			t.Fatalf("protected message %d changed", i)
		}
	}
	if out.Messages[3].Content != `{"source":"原始来源","validAt":"2026-09-07","value":1.2300e-09}` {
		t.Fatal(out.Messages[3].Content)
	}
	if string(out.Messages[2].ToolCalls[0].Arguments) != `{"place":"合肥"}` {
		t.Fatal("arguments not compacted")
	}
	if out.Tools[0].Name != "office.inspect" || !reflect.DeepEqual(out.Tools[0], in.Tools[1]) {
		t.Fatal("schema changed / catalog unstable")
	}
	after, _ := json.Marshal(in)
	if string(original) != string(after) {
		t.Fatal("caller or durable history mutated")
	}
	if !reflect.DeepEqual(out, prepareEfficientRequest(out, Options{})) {
		t.Fatal("repeated prepare changed prefix")
	}
	if !reflect.DeepEqual(in, prepareEfficientRequest(in, Options{DisableTokenEfficiency: true})) {
		t.Fatal("bypass changed request")
	}
}

func TestRequestEfficiencyAmbiguousAndUnknownToolsStayIntact(t *testing.T) {
	in := Request{Tools: []ToolDefinition{{Name: "same", Description: "one"}, {Name: "same", Description: "two"}}, Messages: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c", Name: "weather.get"}, {ID: "c", Name: "unknown"}}},
		{Role: RoleTool, ToolCallID: "c", Content: "{\n \"ok\": true\n}"},
		{Role: RoleTool, ToolCallID: "unknown", Content: "[\n 1, 2\n]"},
	}}
	if !reflect.DeepEqual(in, prepareEfficientRequest(in, Options{})) {
		t.Fatal("unproven result shape or duplicate tools changed")
	}
}

func TestRequestEfficiencyIsWiredToBothProvidersAndModes(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, streaming := range []bool{false, true} {
			t.Run(protocol+map[bool]string{true: "/stream", false: "/complete"}[streaming], func(t *testing.T) {
				payload := `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
				if protocol == "anthropic" {
					payload = `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`
				}
				if streaming {
					if protocol == "anthropic" {
						payload = "event: message_stop\ndata: {}\n\n"
					} else {
						payload = "data: [DONE]\n\n"
					}
				}
				f := &fakeConnector{responses: []*http.Response{response(200, payload)}}
				var adapter Adapter = NewOpenAI(f, Options{})
				if protocol == "anthropic" {
					adapter = NewAnthropic(f, Options{})
				}
				var err error
				if streaming {
					_, err = adapter.Stream(context.Background(), nil, efficiencyFixture(), nil)
				} else {
					_, err = adapter.Complete(context.Background(), nil, efficiencyFixture())
				}
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(f.requests[0].Body)
				var wire map[string]json.RawMessage
				if err = json.Unmarshal(body, &wire); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(wire["tools"]), "office") {
					t.Fatal("tool definitions lost")
				}
				if strings.Index(string(wire["tools"]), "office") > strings.Index(string(wire["tools"]), "weather") {
					t.Fatal("stable order not on wire")
				}
				if strings.Contains(string(wire["messages"]), `\n  \"source\"`) {
					t.Fatal("eligible tool JSON was not compacted on wire")
				}
				if !strings.Contains(string(wire["messages"]), `\n  \"literalFile\"`) {
					t.Fatal("raw file formatting changed on wire")
				}
			})
		}
	}
}

func TestRequestEfficiencyFixtureReportsEstimateWithoutQualityLoss(t *testing.T) {
	type row struct {
		ID    string `json:"id"`
		Code  string `json:"code"`
		Value string `json:"value"`
	}
	rows := make([]row, 40)
	for i := range rows {
		rows[i] = row{ID: "001234567890123456789", Code: "  if (ready) {\n    save();\n  }", Value: "1.2300e-09"}
	}
	pretty, _ := json.MarshalIndent(map[string]any{"rows": rows, "source": "fixture", "version": "v1"}, "", "    ")
	in := Request{Messages: []Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c", Name: "office.inspect", Arguments: []byte(`{}`)}}}, {Role: RoleTool, ToolCallID: "c", Content: string(pretty)}}}
	optimized := prepareEfficientRequest(in, Options{}).Messages[1].Content
	var before, after any
	_ = json.Unmarshal(pretty, &before)
	_ = json.Unmarshal([]byte(optimized), &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("source values changed")
	}
	if len(optimized) >= len(pretty) {
		t.Fatal("no structural reduction")
	}
	t.Logf("synthetic structured-result fixture: bytes %d -> %d; canonical estimated tokens %d -> %d (not billed usage or real-task quality score)", len(pretty), len(optimized), token.CountTokensForModel("", string(pretty)), token.CountTokensForModel("", optimized))
}
