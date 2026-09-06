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
		want    string
	}{{"npx", []string{"-y", "@fixture/server"}, "@fixture/server@1.2.3"}, {"uvx", []string{"mcp-fixture"}, "mcp-fixture==2.4.1"}} {
		locked, err := ResolveLaunchArgs(context.Background(), tc.command, tc.args, fetch)
		if err != nil {
			t.Fatal(err)
		}
		if locked[len(locked)-1] != tc.want {
			t.Fatal(locked)
		}
		before := calls
		if _, err = ResolveLaunchArgs(context.Background(), tc.command, locked, fetch); err != nil || calls != before {
			t.Fatalf("locked version was re-resolved: %v", err)
		}
	}
	if _, err := ResolveLaunchArgs(context.Background(), "npx", []string{"https://evil.invalid/code"}, fetch); err == nil {
		t.Fatal("arbitrary source accepted")
	}
}
