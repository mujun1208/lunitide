package app

import (
	"context"
	"strings"
	"testing"
)

func TestCapabilityPackWaitsForRemoteCredentialThenResumes(t *testing.T) {
	e, _ := packFixture(t)
	ctx := context.Background()
	executor := enginePackExecutor{e: e}
	resource, err := executor.Describe(ctx, "mcp", "huggingface")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = executor.Ensure(ctx, resource); err == nil || !strings.Contains(err.Error(), "配置本机凭据") {
		t.Fatalf("credential setup guidance missing: %v", err)
	}
	endpoint, err := e.m7mcp.Endpoint(ctx, resource.TargetID)
	if err != nil || endpoint.Enabled {
		t.Fatalf("credentialless endpoint should remain configured and disabled: %+v %v", endpoint, err)
	}
	if _, err = e.m7mcp.BindCredential(ctx, endpoint.EndpointID, endpoint.Security.Version, "secretref:fixture", ""); err != nil {
		t.Fatal(err)
	}
	got, err := executor.Ensure(ctx, resource)
	if err != nil || got != endpoint.EndpointID {
		t.Fatalf("resume should use configured endpoint: %s %v", got, err)
	}
}
