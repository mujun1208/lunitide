package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/lunitide/lunitide/internal/mcp6"
)

func TestMcpProductionGatewayAuthenticatedProbeCallAndDrift(t *testing.T) {
	ctx := context.Background()
	toolCalls, requests := 0, 0
	schema := `{"type":"object"}`
	reject := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("missing auth in real request")
			w.WriteHeader(401)
			return
		}
		if reject {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/tools" {
			_, _ = w.Write([]byte(`[{"name":"lookup","description":"fixture","inputSchema":` + schema + `}]`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/tools/") {
			toolCalls++
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	previous := mcpSecureClientFor
	t.Cleanup(func() { mcpSecureClientFor = previous })
	mcpSecureClientFor = func(e *mcp6.Endpoint) (*mcp.Client, error) {
		u, _ := url.Parse(e.URL)
		c, err := mcp.NewClient(mcp.RemoteEndpoint{BaseURL: e.URL}, []string{u.Host})
		if err == nil {
			c.SetTLSConfig(server.Client().Transport.(*http.Transport).TLSClientConfig)
		}
		return c, err
	}
	r := mcp6.NewRegistry(nil, nil, nil)
	r.SetSecurityAdapters(func(ctx context.Context, _ *mcp6.Endpoint, fn func(mcp6.Credentials) error) error {
		return fn(mcp6.Credentials{Context: ctx, Bearer: []byte("fixture-token")})
	}, mcpSecureDescribe, mcpSecureInvoke)
	in := mcp6.EndpointInput{ID: "fixture", Transport: "https", URL: server.URL, AuthRef: "secretref:fixture/token", Pin: mcp6.BootstrapPin("fixture")}
	ep, err := r.Register(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Invoke(ctx, ep.ID, "lookup", map[string]any{"q": "fixture"}); err != nil {
		t.Fatal(err)
	}
	if toolCalls != 1 || requests != 3 {
		t.Fatalf("probe/list/call=%d/%d", toolCalls, requests)
	}
	schema = `{"type":"object","required":["write"]}`
	if _, err = r.Invoke(ctx, ep.ID, "lookup", nil); !errors.Is(err, mcp6.ErrCapabilityDrift) {
		t.Fatal(err)
	}
	if toolCalls != 1 {
		t.Fatal("schema drift reached tools/call")
	}
	in.Pin = mcp6.BootstrapPin("explicit fixture approval")
	if _, err = r.Register(ctx, in); err != nil {
		t.Fatal(err)
	}
	reject = true
	if _, err = r.Invoke(ctx, ep.ID, "lookup", nil); !errors.Is(err, mcp6.ErrCredentialRevoked) {
		t.Fatalf("typed 401=%v", err)
	}
	if toolCalls != 1 {
		t.Fatal("401 retried or invoked")
	}
}
