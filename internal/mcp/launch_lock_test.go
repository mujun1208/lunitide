package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestMcpPackageLaunchLocksBeforeExecution(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, target string) ([]byte, error) {
		calls++
		if strings.Contains(target, "registry.npmjs.org") {
			return []byte(`{"name":"@fixture/server","version":"1.2.3"}`), nil
		}
		return []byte(`{"info":{"name":"mcp-fixture","version":"2.4.1"}}`), nil
	}
	for _, tc := range []struct {
		command string
		args    []string
		want    []string
	}{
		{"npx", []string{"-y", "@fixture/server"}, []string{"-y", "@fixture/server@1.2.3"}},
		{"uvx", []string{"mcp-fixture"}, []string{"mcp-fixture==2.4.1"}},
		{"uvx", []string{"mcp-fixture", "stdio"}, []string{"mcp-fixture==2.4.1", "stdio"}},
		{"uvx", []string{"--from", "mcp-fixture", "word_mcp_server"}, []string{"--from", "mcp-fixture==2.4.1", "word_mcp_server"}},
		{"uvx", []string{"--from", "mcp-fixture==2.4.1", "word_mcp_server"}, []string{"--from", "mcp-fixture==2.4.1", "word_mcp_server"}},
	} {
		locked, err := ResolveLaunchArgs(context.Background(), tc.command, tc.args, fetch)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(locked, " ") != strings.Join(tc.want, " ") {
			t.Fatalf("locked %v, want %v", locked, tc.want)
		}
		before := calls
		if _, err = ResolveLaunchArgs(context.Background(), tc.command, locked, fetch); err != nil || calls != before {
			t.Fatalf("locked version was re-resolved: %v", err)
		}
	}
	if _, err := ResolveLaunchArgs(context.Background(), "npx", []string{"https://evil.invalid/code"}, fetch); err == nil {
		t.Fatal("arbitrary source accepted")
	}
	for _, args := range [][]string{
		{"--from"},
		{"--from", "mcp-fixture"},
		{"--from", "mcp-fixture", "--help"},
		{"--refresh", "mcp-fixture"},
	} {
		if _, err := ResolveLaunchArgs(context.Background(), "uvx", args, fetch); err == nil {
			t.Fatalf("unsupported uvx args accepted: %v", args)
		}
	}
}
