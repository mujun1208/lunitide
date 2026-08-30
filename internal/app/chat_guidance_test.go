package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestInjectedGuidanceLabels(t *testing.T) {
	req := gateway.Request{Messages: []gateway.Message{
		{Role: gateway.RoleSystem, Content: "[身份记忆] 你叫月汐。\n\n[内置工作流] 开箱即用\n[仓库约定] AGENTS.md：Keep tests.\n[可用技能目录]\n- search"},
		{Role: gateway.RoleUser, Content: "打开网易云"},
	}}
	got := injectedGuidanceLabels(req)
	joined := strings.Join(got, ",")
	for _, want := range []string{"工作流", "身份", "AGENTS", "技能"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("labels %v missing %s", got, want)
		}
	}
	_, digest, _ := injectedGuidanceDigest(req)
	if len(digest) != 16 {
		t.Fatalf("digest %q", digest)
	}
}

func TestInjectedGuidanceLabelsEmpty(t *testing.T) {
	req := gateway.Request{Messages: []gateway.Message{
		{Role: gateway.RoleUser, Content: "hi"},
	}}
	if labels := injectedGuidanceLabels(req); len(labels) != 0 {
		t.Fatalf("labels = %v", labels)
	}
}
