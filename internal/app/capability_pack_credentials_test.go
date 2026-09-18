package app

import (
	"context"
	"strings"
	"testing"
)

func TestCapabilityPackRejectsRemovedCredentialPresets(t *testing.T) {
	e, _ := packFixture(t)
	ctx := context.Background()
	executor := enginePackExecutor{e: e}
	_, err := executor.Describe(ctx, "mcp", "huggingface")
	if err == nil || !strings.Contains(err.Error(), "unknown MCP preset") {
		t.Fatalf("removed credential preset must not be pack-installable: %v", err)
	}
}
