package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/oklog/ulid/v2"
)

type mcpLaunchRewrite struct {
	command string
	args    []string
}

// RepairLegacyMcpPresetLaunches preserves endpoint IDs, pack references and
// enablement while repairing shipped, nonexistent npm packages and leftover
// "{{dir}}" filesystem placeholders. A user-pinned or credential-bearing
// server is never rewritten by this compatibility repair.
func (s *Store) RepairLegacyMcpPresetLaunches(ctx context.Context) error {
	npxToUvx := map[string]string{
		"@modelcontextprotocol/server-fetch": "mcp-server-fetch",
		"@modelcontextprotocol/server-time":  "mcp-server-time",
		"@microsoft/markitdown-mcp":          "markitdown-mcp",
		"mcp-server-calculator":              "mcp-server-calculator",
		"@nickclyde/duckduckgo-mcp-server":   "duckduckgo-mcp-server",
		"duckduckgo-mcp-server":              "duckduckgo-mcp-server",
	}
	npxPackage := map[string]string{
		"youtube-transcript-mcp": "@sinco-lab/mcp-youtube-transcript",
	}
	return s.AgentRuntimeRepository().TransactMcp(ctx, func(tx m7app.McpTx) error {
		rows, err := tx.ListMcpEndpoints("")
		if err != nil {
			return err
		}
		native := tx.(*agentRuntimeTx)
		for i, ep := range rows {
			next, ok := plannedMcpLaunchRewrite(ep, npxToUvx, npxPackage)
			if !ok {
				continue
			}
			if mcpLaunchAlreadyLive(rows, ep.EndpointID, next.command, next.args) {
				if err := revokeDuplicateMcpLaunch(ctx, native, ep); err != nil {
					return err
				}
				rows[i].State = m7flow.McpStateRevoked
				rows[i].Enabled = false
				continue
			}
			launch, _ := json.Marshal(next.args)
			_, err = native.tx.ExecContext(ctx, `UPDATE mcp_endpoint_settings SET command=?,args_json=?,state='degraded',capability_digest=NULL,pinned_digest=NULL,last_health_at=NULL WHERE endpoint_id=?`, next.command, string(launch), ep.EndpointID)
			if err != nil {
				return native.fail(err)
			}
			if ep.Security.Version > 0 {
				security := ep.Security
				security.LaunchArgsJSON = ""
				security.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				if err = native.PutMcpSecurity(ep.EndpointID, security.Version, security); err != nil {
					return err
				}
			}
			if _, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.preset.repair", ResourceType: "mcp_endpoint", ResourceID: ep.EndpointID, Actor: "system/compatibility", BeforeDigest: m7flow.SHA256Hex([]byte(ep.Command + ep.ArgsJSON)), AfterDigest: m7flow.SHA256Hex([]byte(next.command + string(launch))), CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
				return err
			}
			rows[i].Command = next.command
			rows[i].ArgsJSON = string(launch)
			rows[i].State = m7flow.McpStateDegraded
		}
		return nil
	})
}

func plannedMcpLaunchRewrite(ep m7flow.McpEndpointConfig, npxToUvx, npxPackage map[string]string) (mcpLaunchRewrite, bool) {
	// Revoked: permanently removed, never rewrite.
	// Credential-bearing (AuthRef/EnvRefsJSON) or pin-locked (PinJSON): user
	// or security system controls these; never auto-rewrite.
	// Quarantined with a pin: capability drift detected; the security review
	// flow owns recovery, not auto-rewrite.
	// Quarantined WITHOUT a pin: likely caused by timeout/network/env issues
	// during the initial probe — safe to rewrite so the next health check can
	// use the correct package coordinates.
	if ep.State == m7flow.McpStateRevoked || ep.Security.PinJSON != "" || ep.Security.AuthRef != "" || (ep.Security.EnvRefsJSON != "" && ep.Security.EnvRefsJSON != "{}") {
		return mcpLaunchRewrite{}, false
	}
	if ep.State == m7flow.McpStateQuarantined && ep.PinnedDigest != "" {
		return mcpLaunchRewrite{}, false
	}
	var args []string
	if json.Unmarshal([]byte(ep.ArgsJSON), &args) != nil || len(args) == 0 {
		return mcpLaunchRewrite{}, false
	}
	next := mcpLaunchRewrite{command: ep.Command, args: append([]string(nil), args...)}
	changed := false
	if ep.Command == "npx" && len(args) >= 2 && args[0] == "-y" {
		pkg := args[1]
		if uvxPkg, ok := npxToUvx[pkg]; ok && len(args) == 2 {
			next.command = "uvx"
			next.args = []string{uvxPkg}
			changed = true
		} else if remapped, ok := npxPackage[pkg]; ok && len(args) == 2 {
			next.args = []string{"-y", remapped}
			changed = true
		}
	}
	for i, a := range next.args {
		if a == "{{dir}}" {
			next.args[i] = mcp6.PrepareSandbox("filesystem")
			changed = true
		}
	}
	if !changed {
		return mcpLaunchRewrite{}, false
	}
	return next, true
}

func mcpLaunchAlreadyLive(rows []m7flow.McpEndpointConfig, skipID, command string, args []string) bool {
	want, err := json.Marshal(args)
	if err != nil {
		return false
	}
	target := string(want)
	for _, ep := range rows {
		if ep.EndpointID == skipID || ep.State == m7flow.McpStateRevoked {
			continue
		}
		if ep.Command == command && ep.ArgsJSON == target {
			return true
		}
	}
	return false
}

func revokeDuplicateMcpLaunch(ctx context.Context, native *agentRuntimeTx, ep m7flow.McpEndpointConfig) error {
	_, err := native.tx.ExecContext(ctx, `UPDATE mcp_endpoint_settings SET enabled=0,state='revoked',capability_digest=NULL,last_health_at=NULL WHERE endpoint_id=? AND state<>'revoked'`, ep.EndpointID)
	if err != nil {
		return native.fail(err)
	}
	if _, err = native.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.preset.repair", ResourceType: "mcp_endpoint", ResourceID: ep.EndpointID, Actor: "system/compatibility", BeforeDigest: m7flow.SHA256Hex([]byte(ep.Command + ep.ArgsJSON)), AfterDigest: m7flow.SHA256Hex([]byte("revoked-duplicate")), CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	return nil
}
