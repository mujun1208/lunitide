package mcp

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in, catalogue-only smoke check against HF's public official endpoint.
// The deterministic TLS fixtures remain the required offline gate.
func TestStreamableOfficialPublicCatalogue(t *testing.T) {
	if os.Getenv("LUNITIDE_TEST_PUBLIC_MCP") != "1" {
		t.Skip("public network check is opt-in")
	}
	client, err := NewClient(RemoteEndpoint{BaseURL: "https://huggingface.co/mcp"}, []string{"huggingface.co"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, tools, err := client.Discover(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.IsLegacy() || len(tools) == 0 {
		t.Fatal("official catalogue unavailable")
	}
	t.Logf("official public MCP: protocol=%s tools=%d", session.protocol, len(tools))
}
