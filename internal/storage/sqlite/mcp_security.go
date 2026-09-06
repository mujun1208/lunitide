package sqlite

import (
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

func (t *agentRuntimeTx) PutMcpSecurity(id string, expected int64, s m7flow.McpEndpointSecurity) error {
	if s.EnvRefsJSON == "" {
		s.EnvRefsJSON = "{}"
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO mcp_endpoint_security(endpoint_id,updated_at) VALUES(?,?) ON CONFLICT(endpoint_id) DO NOTHING`, id, s.UpdatedAt); err != nil {
		return t.fail(err)
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE mcp_endpoint_security SET auth_ref=?,env_refs_json=?,pin_json=?,launch_args_json=?,version=version+1,updated_at=? WHERE endpoint_id=? AND version=?`, s.AuthRef, s.EnvRefsJSON, s.PinJSON, s.LaunchArgsJSON, s.UpdatedAt, id, expected)
	if err != nil {
		return t.fail(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return t.fail(err)
	}
	if n != 1 {
		return m7app.ErrMcpSecurityConflict
	}
	return nil
}
