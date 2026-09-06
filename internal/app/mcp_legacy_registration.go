package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/oklog/ulid/v2"
)

func legacyMcpRegistrationID(input mcp6.EndpointInput) string {
	input.ID = ""
	raw, _ := json.Marshal(input)
	digest := sha256.Sum256(raw)
	var id ulid.ULID
	copy(id[:], digest[:16])
	return id.String()
}

func (e *Engine) registerLegacyMcpDurably(ctx context.Context, r bridge.Request, input mcp6.EndpointInput) bridge.Response {
	if err := input.Pin.Validate(); err != nil {
		return m6McpFailure(r, err)
	}
	input.ID = legacyMcpRegistrationID(input)
	ctx, release, err := e.AcquireCapability(ctx, mcpPluginIDs(input.Command, input.Args)...)
	if err != nil {
		return m6McpFailure(r, err)
	}
	defer release()
	// Credentials may already have been approved through the settings/Host
	// journey. Reuse that exact endpoint identity instead of inventing a second
	// credential binding for the same source.
	rows, listErr := e.m7mcp.List(ctx, "")
	if listErr != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "端点读取失败", true)
	}
	for _, row := range rows {
		var args []string
		if json.Unmarshal([]byte(row.ArgsJSON), &args) != nil {
			continue
		}
		if row.Transport == input.Transport && row.Command == input.Command && row.URL == input.URL && mustJSONArgs(args) == mustJSONArgs(input.Args) {
			input.ID = chatMcpEndpointID(row.EndpointID)
			break
		}
	}
	existing, readErr := e.m7mcp.Endpoint(ctx, "mcp-"+input.ID)
	if readErr == nil {
		if !existing.Enabled || existing.State == m7flow.McpStateRevoked || existing.State == m7flow.McpStateQuarantined {
			return m6McpFailure(r, mcp6.ErrEndpointRevoked)
		}
		if existing.Security.AuthRef != input.AuthRef {
			return m6McpFailure(r, mcp6.ErrCredentialRevoked)
		}
		pin, _ := json.Marshal(input.Pin)
		_, bootstrap := input.Pin.ToolSchemaDigests["_pending"]
		if !bootstrap && existing.Security.PinJSON != string(pin) {
			return m6McpFailure(r, mcp6.ErrCapabilityDrift)
		}
		if err = e.refreshLegacyMcp(ctx, existing.EndpointID); err != nil {
			return m6McpFailure(r, err)
		}
	} else {
		if !errors.Is(readErr, sql.ErrNoRows) && !errors.Is(readErr, m7flow.ErrNotFound) && !errors.Is(readErr, m7app.ErrMcpNotFound) {
			return r.Fail("STORAGE_UNAVAILABLE", "端点读取失败", true)
		}
		checked, probeErr := e.mcp6Registry.CheckEndpoint(ctx, input)
		if probeErr != nil && (!errors.Is(probeErr, mcp6.ErrHealthCheckFailed) || checked == nil) {
			return m6McpFailure(r, probeErr)
		}
		stored, persistErr := e.m7mcp.ImportGatewayEndpoint(ctx, input, checked)
		if persistErr != nil {
			return m7McpFailure(r, persistErr, "mcp6.register")
		}
		if probeErr != nil {
			return m6McpFailure(r, probeErr)
		}
		if err = e.refreshLegacyMcp(ctx, stored.EndpointID); err != nil {
			return m6McpFailure(r, err)
		}
	}
	actual, err := e.mcp6Registry.Get(input.ID)
	if err != nil {
		return m6McpFailure(r, err)
	}
	if actual.State != mcp6.StateReady {
		return m6McpFailure(r, mcp6.ErrHealthCheckFailed)
	}
	if err = e.checkMcpCapability(ctx, actual); err != nil {
		return m6McpFailure(r, err)
	}
	if err = ctx.Err(); err != nil {
		return m6McpFailure(r, err)
	}
	return r.Ok(struct {
		EndpointID string `json:"endpointId"`
		State      string `json:"state"`
	}{actual.ID, actual.State})
}

func (e *Engine) refreshLegacyMcp(ctx context.Context, id string) error {
	health, err := e.m7mcp.Health(ctx, id)
	if err != nil {
		return err
	}
	if health.State != m7flow.McpStateReady {
		return mcp6.ErrHealthCheckFailed
	}
	return nil
}
