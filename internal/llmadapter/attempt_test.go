package llmadapter

import (
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/modelfit"
)

func TestAttemptErrorMatrix(t *testing.T) {
	cases := []struct {
		name           string
		in             AttemptClassInput
		retryable      bool
		executeTools   bool
		usageIntegrity string
	}{
		{"param_400", AttemptClassInput{HTTPStatus: 400, Kind: "param"}, false, false, ""},
		{"rate_429", AttemptClassInput{HTTPStatus: 429, Kind: "rate"}, true, false, ""},
		{"auth_401", AttemptClassInput{HTTPStatus: 401, Kind: "auth"}, false, false, ""},
		{"cancel_before_send", AttemptClassInput{Kind: "cancel_before_send"}, false, false, ""},
		{"disconnect", AttemptClassInput{Kind: "disconnect"}, true, false, "unknown"},
		{"truncated_sse", AttemptClassInput{Kind: "truncated_sse"}, false, false, "unknown"},
		{"missing_usage", AttemptClassInput{Kind: "missing_usage", HTTPStatus: 200}, false, false, "missing"},
		{"tool_args_fragment", AttemptClassInput{Kind: "tool_args_fragment", Arguments: json.RawMessage(`{"path":`)}, false, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyAttempt(tc.in)
			if got.Retryable != tc.retryable {
				t.Fatalf("retryable=%v want %v: %+v", got.Retryable, tc.retryable, got)
			}
			if got.ExecuteTools != tc.executeTools {
				t.Fatalf("executeTools=%v want %v: %+v", got.ExecuteTools, tc.executeTools, got)
			}
			if tc.usageIntegrity != "" && got.UsageIntegrity != tc.usageIntegrity {
				t.Fatalf("usageIntegrity=%q want %q", got.UsageIntegrity, tc.usageIntegrity)
			}
			if got.SideEffect != "none" {
				t.Fatalf("attempt class must not invent extra side effects: %+v", got)
			}
			if tc.in.Kind == "cancel_before_send" && got.HTTPSends != 0 {
				t.Fatalf("cancel before send must not HTTP: %+v", got)
			}
			if tc.in.Kind == "tool_args_fragment" && modelfit.CanExecuteToolCalls([]modelfit.ProtocolToolCall{{
				ID: "c1", Name: "workspace.write", Arguments: tc.in.Arguments,
			}}) {
				t.Fatal("fragment arguments must not enter the tool executor")
			}
		})
	}
}

func TestCompiledToolCatalogStable(t *testing.T) {
	a := CatalogTool{Name: "workspace.read", Version: "1", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}
	b := CatalogTool{Name: "web.search", Version: "1", Schema: json.RawMessage(`{"type":"object"}`)}
	attach := CatalogTool{Name: "user.attach", Version: "1", Schema: json.RawMessage(`{"type":"object"}`), Attachment: true}

	first := CompileToolCatalog("task-1", "glm-5", []CatalogTool{b, a, attach}, "")
	second := CompileToolCatalog("task-1", "glm-5", []CatalogTool{attach, a, b}, "")
	if first.Digest == "" || first.Digest != second.Digest {
		t.Fatalf("order must not change digest: %s vs %s", first.Digest, second.Digest)
	}

	bumped := CompileToolCatalog("task-1", "glm-5", []CatalogTool{
		{Name: a.Name, Version: "2", Schema: a.Schema}, b, attach,
	}, "")
	if bumped.Digest == first.Digest {
		t.Fatal("version change must change digest")
	}

	otherTask := CompileToolCatalog("task-2", "glm-5", []CatalogTool{b, a, attach}, "")
	if otherTask.Digest == first.Digest {
		t.Fatal("catalog must compile per task")
	}
	otherModel := CompileToolCatalog("task-1", "deepseek-v4", []CatalogTool{b, a, attach}, "")
	if otherModel.Digest == first.Digest {
		t.Fatal("catalog must compile per model")
	}

	sneak := CompileToolCatalog("task-1", "glm-5", []CatalogTool{b, a, attach}, `call invented.tool now`)
	if sneak.Digest != first.Digest {
		t.Fatal("model text must not register unknown tools")
	}
	for _, tool := range sneak.Tools {
		if tool.Name == "invented.tool" {
			t.Fatal("unknown tool registered from model text")
		}
		if tool.Attachment && tool.Trust != "untrusted" {
			t.Fatalf("attachment must stay untrusted: %+v", tool)
		}
	}
}
