package m8app

import (
	"strings"
	"testing"
)

func TestConversationExpertsJSONHasNoIndependentAgentLie(t *testing.T) {
	raw := string(conversationExpertsJSON)
	if strings.Contains(raw, "独立智能体") {
		t.Fatal("catalog seed still claims 独立智能体; rewrite at load is not enough")
	}
	if !strings.Contains(raw, "同事专家（同一月汐引擎，不是独立进程）") {
		t.Fatal("catalog seed must name 同事专家 and deny a separate process")
	}
}
