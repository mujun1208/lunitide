package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNodeScriptContentLockWithoutExecution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.js")
	if err := os.WriteFile(path, []byte("// fixture v1"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := NodeScriptDigest(context.Background(), []string{"server.js"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("// fixture v2"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := NodeScriptDigest(context.Background(), []string{"server.js"}, dir)
	if err != nil || second == first {
		t.Fatalf("content change not detected: %v", err)
	}
	if _, err = NodeScriptDigest(context.Background(), []string{"--eval", "code"}, dir); err == nil {
		t.Fatal("unreviewable inline code accepted")
	}
}
