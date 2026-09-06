// mcp_bridge.go admits settings-plane MCP endpoints (mcp.add) into the
// chat-facing mcp6 registry so registered servers actually appear as
// model tools. The two planes used to be disconnected: settings wrote
// SQLite, chat read an empty in-memory registry.
package app

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp6"
)

func chatMcpEndpointID(settingsID string) string {
	id := strings.TrimPrefix(settingsID, "mcp-")
	if len(id) == 26 {
		return id
	}
	return settingsID
}

func mustJSONArgs(args []string) string {
	if len(args) == 0 {
		return "[]"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// admitSettingsMcp copies one settings-plane endpoint into the chat
// gateway. Failures stay logged: mcp.add itself already succeeded, and a
// later health probe / restart hydrate can recover.
func (e *Engine) admitSettingsMcp(ctx context.Context, ep m7flow.McpEndpointConfig) error {
	if e == nil || e.mcp6Registry == nil || ep.EndpointID == "" {
		return nil
	}
	if !ep.Enabled || ep.State == m7flow.McpStateRevoked || ep.State == m7flow.McpStateQuarantined {
		e.dropSettingsMcp(ep.EndpointID)
		return nil
	}
	input, err := settingsMcpInput(ep)
	if err != nil {
		return err
	}
	if err := e.checkMcpPlugin(ctx, input.Command, input.Args); err != nil {
		return err
	}
	if e.m7mcp != nil && e.mcp6Registry.SecurityEnabled() {
		_, err = (settingsGatewayProber{e}).Probe(ctx, ep)
		return err
	}
	_, err = e.mcp6Registry.Register(ctx, input)
	return err
}

func (e *Engine) dropSettingsMcp(endpointID string) {
	if e == nil || e.mcp6Registry == nil || endpointID == "" {
		return
	}
	if _, err := e.mcp6Registry.Revoke(chatMcpEndpointID(endpointID), mcp6.ReasonManual); err != nil {
		log.Printf("mcp gateway drop %s: %v", endpointID, err)
	}
}

// HydrateMcpGatewayFromSettings loads enabled settings-plane endpoints into
// the chat registry after a restart. Runs in the background so npx probes
// never block engine listen.
func (e *Engine) HydrateMcpGatewayFromSettings(ctx context.Context) {
	if e == nil || e.m7mcp == nil || e.mcp6Registry == nil {
		return
	}
	eps, err := e.m7mcp.List(ctx, "")
	if err != nil {
		log.Printf("mcp gateway hydrate: %v", err)
		return
	}
	for _, ep := range eps {
		if !ep.Enabled || ep.State == m7flow.McpStateRevoked || ep.State == m7flow.McpStateQuarantined {
			continue
		}
		if _, err := e.m7mcp.Health(ctx, ep.EndpointID); err != nil {
			log.Printf("mcp gateway hydrate %s: %v", ep.EndpointID, err)
		}
	}
}

func mcpEndpointHasPackage(eps []m7flow.McpEndpointConfig, pkg string) bool {
	for _, ep := range eps {
		if strings.Contains(ep.ArgsJSON, pkg) {
			return true
		}
	}
	return false
}

// SeedRecommendedMcpKit registers the free no-arg kit (memory + sequential
// thinking) when missing, then enables it. Runs in the background so npx
// describe never blocks listen. Distinct stdio args are different
// fingerprints — two npx servers no longer collapse into one row.
func (e *Engine) SeedRecommendedMcpKit(ctx context.Context) {
	if e == nil || e.m7mcp == nil {
		return
	}
	eps, err := e.m7mcp.List(ctx, "")
	if err != nil {
		log.Printf("mcp kit seed list: %v", err)
		return
	}
	for _, id := range []string{"memory", "sequentialthinking"} {
		p, ok := mcp6.PresetByID(id)
		if !ok || p.NeedsArgs || len(p.Args) == 0 {
			continue
		}
		pkg := p.Args[len(p.Args)-1]
		if mcpEndpointHasPackage(eps, pkg) {
			continue
		}
		res, err := e.m7mcp.Add(ctx, m7app.McpAddInput{
			Origin: m7flow.McpOriginManual, Transport: m7flow.McpTransportStdio,
			Command: p.Command, Args: p.Args, RiskConfirmed: true,
			Actor: "system", IdempotencyKey: "seed-" + id,
		})
		if err != nil {
			log.Printf("mcp kit seed %s: %v", id, err)
			continue
		}
		ep, err := e.m7mcp.Toggle(ctx, res.EndpointID, true, "system")
		if err != nil {
			log.Printf("mcp kit enable %s: %v", id, err)
			continue
		}
		eps = append(eps, ep)
	}
}
