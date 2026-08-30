package app

import (
	"context"
	"strings"
	"testing"
)

func TestInvokePluginCreateRefusesMcpAndAgentPack(t *testing.T) {
	e := NewEngine(nil, "test")
	_, err := e.invokePluginCreateTool(context.Background(), "sess", []byte(`{"pluginId":"x","name":"x","kind":"mcp"}`))
	if err == nil || !strings.Contains(err.Error(), "mcp.install") {
		t.Fatalf("mcp: %v", err)
	}
	_, err = e.invokePluginCreateTool(context.Background(), "sess", []byte(`{"pluginId":"x","name":"x","kind":"agent-pack"}`))
	if err == nil || !strings.Contains(err.Error(), "skill.create") {
		t.Fatalf("agent-pack: %v", err)
	}
}
