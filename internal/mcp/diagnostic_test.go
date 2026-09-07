package mcp

import (
	"context"
	"errors"
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
