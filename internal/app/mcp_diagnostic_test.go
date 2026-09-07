package app

import (
	"context"
	"errors"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp"
	"strings"
	"testing"
)

type diagnosticFixtureProber struct{ err error }

func (p *diagnosticFixtureProber) Probe(context.Context, m7flow.McpEndpointConfig) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return strings.Repeat("a", 64), nil
}
func TestMcpHealthAndListExposeControlledFailureAndClearOnRecovery(t *testing.T) {
	e, _, svc := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	added, err := svc.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "stdio", Command: "uvx", Args: []string{"fixture"}, RiskConfirmed: true, ConfigureOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	p := &diagnosticFixtureProber{err: &mcp.DiagnosticError{Code: "MCP_DEPENDENCY_FAILED", Cause: errors.New("secret-token")}}
	svc.SetProber(p)
	response := e.Handle(ctx, m7Request(bridge.MethodMcpHealth, `{"endpointId":"`+added.EndpointID+`"}`, ""))
	var health struct{ State, DiagnosticCode, DiagnosticMessage string }
	m7Decode(t, response, &health)
	if health.State != "degraded" || health.DiagnosticCode != "MCP_DEPENDENCY_FAILED" || strings.Contains(health.DiagnosticMessage, "secret-token") {
		t.Fatalf("bad diagnostic: %+v", health)
	}
	var list struct{ Endpoints []m7McpEndpointDTO }
	m7Decode(t, e.Handle(ctx, m7Request(bridge.MethodMcpList, `{}`, "")), &list)
	if len(list.Endpoints) != 1 || list.Endpoints[0].DiagnosticCode != health.DiagnosticCode {
		t.Fatalf("list lost diagnostic: %+v", list)
	}
	p.err = nil
	res, err := svc.Health(ctx, added.EndpointID)
	if err != nil || res.State != "ready" || svc.LastDiagnostic(added.EndpointID).Code != "" {
		t.Fatalf("stale failure: %+v %v", res, err)
	}
}
func TestLegacyGoogleDriveRequiresOAuthBindingBeforeLaunching(t *testing.T) {
	ep := m7flow.McpEndpointConfig{EndpointID: "mcp-01ARZ3NDEKTSV4RRFFQ69G5FAV", Transport: "stdio", Command: "npx", ArgsJSON: `["-y","@modelcontextprotocol/server-gdrive","C:/old/workspace"]`}
	_, err := (settingsGatewayProber{}).Probe(context.Background(), ep)
	if !errors.Is(err, mcp.ErrCredentialRequired) {
		t.Fatalf("legacy folder was accepted as credentials: %v", err)
	}
}

func TestMcpToggleFailureKeepsActionableCause(t *testing.T) {
	request := m7Request(bridge.MethodMcpToggle, `{}`, "")
	err := mcpAdmissionError(&mcp.DiagnosticError{Code: "MCP_DEPENDENCY_FAILED", Cause: errors.New("private")})
	result := m7McpFailure(request, err, "mcp.toggle")
	if result.OK || result.Error == nil || !strings.Contains(result.Error.Message, "Python") || strings.Contains(result.Error.Message, "private") {
		t.Fatalf("lost toggle diagnosis: %+v", result)
	}
}
