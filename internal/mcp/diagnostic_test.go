package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMcpDiagnosticsNeverExposeRawErrorOrStderr(t *testing.T) {
	secret := "secret-api-key-12345"
	for _, input := range []struct{ raw, code string }{
		{"Credentials not found. Please run with 'auth' argument first. " + secret, "MCP_CREDENTIAL_REQUIRED"},
		{"npm ERR! code E404 " + secret, "MCP_PACKAGE_NOT_FOUND"},
		{"Failed to download dependency " + secret, "MCP_NETWORK_FAILED"},
		{"No solution found when resolving dependencies " + secret, "MCP_DEPENDENCY_FAILED"},
	} {
		w := &stderrClassifier{}
		n, err := w.Write([]byte(strings.Repeat("a", 10000) + input.raw))
		if err != nil || n != 10000+len(input.raw) {
			t.Fatal(n, err)
		}
		classified := w.classify(errors.New(secret))
		d := ConnectionDiagnostic(classified)
		if d.Code != input.code || strings.Contains(d.Message, secret) || strings.Contains(classified.Error(), secret) || w.tail != "" {
			t.Fatalf("unsafe diagnostic %+v", d)
		}
	}
	if d := ConnectionDiagnostic(context.DeadlineExceeded); d.Code != "MCP_CONNECT_TIMEOUT" {
		t.Fatal(d)
	}
	if d := ConnectionDiagnostic(errors.New(secret)); strings.Contains(d.Message, secret) {
		t.Fatal(d)
	}
}

func TestStdioLaunchDiagnosticDistinguishesMissingRuntime(t *testing.T) {
	uv := ConnectionDiagnostic(fmt.Errorf("%w: uvx not on PATH: missing", ErrStdioLaunch))
	if uv.Code != "MCP_UV_UNAVAILABLE" || !strings.Contains(uv.Message, "安装 uv") {
		t.Fatalf("uvx miss = %+v", uv)
	}
	node := ConnectionDiagnostic(fmt.Errorf("%w: npx not on PATH: missing", ErrStdioLaunch))
	if node.Code != "MCP_RUNTIME_UNAVAILABLE" || !strings.Contains(node.Message, "Node.js") {
		t.Fatalf("npx miss = %+v", node)
	}
	spawn := ConnectionDiagnostic(fmt.Errorf("%w: spawn failed", ErrStdioLaunch))
	if spawn.Code != "MCP_CONNECT_FAILED" {
		t.Fatalf("spawn fail must not look like missing Node: %+v", spawn)
	}
}
