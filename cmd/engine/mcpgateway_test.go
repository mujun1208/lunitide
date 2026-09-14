package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMcpStdioSandboxDirRejectsEscape(t *testing.T) {
	root := t.TempDir()
	mcpGatewaySetStdioWorkDir(root)
	t.Cleanup(func() { mcpGatewaySetStdioWorkDir("") })

	ok, err := mcpStdioSandboxDir("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ok, root) {
		t.Fatalf("dir %q must stay under %q", ok, root)
	}
	if filepath.Base(ok) != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("unexpected base %q", ok)
	}

	for _, id := range []string{"", "..", "../x", "a/b", `a\b`, ".", "x/../y"} {
		if _, err := mcpStdioSandboxDir(id); err == nil {
			t.Fatalf("id %q must be rejected", id)
		}
	}
}

func TestMcpStdioSandboxDirRequiresRoot(t *testing.T) {
	mcpGatewaySetStdioWorkDir("")
	if _, err := mcpStdioSandboxDir("ok"); err == nil {
		t.Fatal("empty work dir must fail")
	}
}
