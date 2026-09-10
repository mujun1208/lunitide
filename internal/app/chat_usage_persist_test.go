package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestPersistUsageFromStreamKeepsInputAndCache(t *testing.T) {
	got := persistUsageFromStream("openai_compatible", "glm-4", llmadapter.Usage{
		InputTokens: 80, OutputTokens: 12, TotalTokens: 92,
		CachedInputTokens: 20, CacheWriteInputTokens: 5, CacheUsageReported: true,
	})
	if got.Provider != "openai_compatible" || got.Model != "glm-4" || got.OutputTokens != 12 {
		t.Fatalf("identity: %+v", got)
	}
	if got.InputTokens != 80 || got.CachedInputTokens != 20 || got.CacheWriteInputTokens != 5 || !got.CacheUsageReported {
		t.Fatalf("input/cache dropped: %+v", got)
	}
}

func TestEstimateToolSchemaTokensCountsDefinitions(t *testing.T) {
	defs := []llmadapter.ToolDefinition{{
		Name: "files.plan", Description: "Create a confined workspace file plan",
		Schema: []byte(`{"type":"object","properties":{"recipe":{"type":"string"}}}`),
	}}
	if n := estimateToolSchemaTokens("glm-4", defs); n <= 0 {
		t.Fatal("tool schemas must reserve tokens")
	}
	if estimateToolSchemaTokens("glm-4", nil) != 0 {
		t.Fatal("empty tools must not invent schema tokens")
	}
}

func TestChatStartBudgetIncludesAppendedToolSchemas(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	mode := executionModeApproval
	engineOnly := e.engineToolDefinitionsFor(mode)
	full := e.chatTurnToolDefinitions(chatTurnToolBuild{Mode: mode, Profile: toolProfileDefault})
	engineN := estimateToolSchemaTokens("glm-4", engineOnly)
	fullN := estimateToolSchemaTokens("glm-4", full)
	if fullN <= engineN {
		t.Fatalf("appended plan/mcp/skill schemas must increase reserve: engine=%d full=%d", engineN, fullN)
	}
	hasPlan := false
	for _, d := range full {
		if d.Name == "plan.run" {
			hasPlan = true
			break
		}
	}
	if !hasPlan {
		t.Fatal("default send surface must reserve plan.run")
	}
	used := int64(100)
	window := used + engineN + 2048 + 1024 + 8
	engineInfo := contextapp.ProviderInfo{ContextWindow: window, SafetyCeiling: window, ReservedOutput: 2048, ToolSchemaTokens: engineN, SafetyMargin: 1024}
	fullInfo := engineInfo
	fullInfo.ToolSchemaTokens = fullN
	if err := errIfRequestOverBudget(used, engineInfo); err != nil {
		t.Fatalf("engine-only reserve must still fit: %v", err)
	}
	if err := errIfRequestOverBudget(used, fullInfo); err == nil {
		t.Fatal("full appended schemas must trip the budget gate")
	}
}
