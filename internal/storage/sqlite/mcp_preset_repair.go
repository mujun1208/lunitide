package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/oklog/ulid/v2"
)

// RepairLegacyMcpPresetLaunches preserves endpoint IDs, pack references and
// enablement while repairing shipped, nonexistent npm packages. A user-pinned
// or credential-bearing server is never rewritten by this compatibility repair.
func (s *Store) RepairLegacyMcpPresetLaunches(ctx context.Context) error {
	replacements := map[string]string{
		"@modelcontextprotocol/server-fetch": "mcp-server-fetch",
		"@modelcontextprotocol/server-time":  "mcp-server-time",
		"@microsoft/markitdown-mcp":          "markitdown-mcp",
		"mcp-server-calculator":              "mcp-server-calculator",
	}
	return s.AgentRuntimeRepository().TransactMcp(ctx, func(tx m7app.McpTx) error {
		rows, err := tx.ListMcpEndpoints("")
		if err != nil {
			return err
		}
		native := tx.(*agentRuntimeTx)
		for _, ep := range rows {
			if ep.State == m7flow.McpStateRevoked || ep.Command != "npx" || ep.Security.PinJSON != "" || ep.Security.AuthRef != "" || (ep.Security.EnvRefsJSON != "" && ep.Security.EnvRefsJSON != "{}") {
				continue
			}
			var args []string
			if json.Unmarshal([]byte(ep.ArgsJSON), &args) != nil || len(args) != 2 || args[0] != "-y" {
				continue
			}
			pkg, ok := replacements[args[1]]
			if !ok {
				continue
			}
			launch, _ := json.Marshal([]string{pkg})
			_, err = native.tx.ExecContext(ctx, `UPDATE mcp_endpoint_settings SET command='uvx',args_json=?,state='degraded',capability_digest=NULL,pinned_digest=NULL,last_health_at=NULL WHERE endpoint_id=?`, string(launch), ep.EndpointID)
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
			if _, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.preset.repair", ResourceType: "mcp_endpoint", ResourceID: ep.EndpointID, Actor: "system/compatibility", BeforeDigest: m7flow.SHA256Hex([]byte(ep.Command + ep.ArgsJSON)), AfterDigest: m7flow.SHA256Hex([]byte("uvx" + string(launch))), CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
				return err
			}
		}
		return nil
	})
}
