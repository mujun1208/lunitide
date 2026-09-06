package m7app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/oklog/ulid/v2"
)

// ImportGatewayEndpoint persists an already checked descriptor before it can
// enter the callable registry. Both old and new Bridge APIs share this store.
func (s *McpRuntimeService) ImportGatewayEndpoint(ctx context.Context, input mcp6.EndpointInput, checked *mcp6.Endpoint) (m7flow.McpEndpointConfig, error) {
	if s == nil || s.uow == nil {
		return m7flow.McpEndpointConfig{}, ErrServiceUnavailable
	}
	if checked == nil || input.ID != checked.ID || (checked.State != mcp6.StateReady && checked.State != mcp6.StateDegraded) {
		return m7flow.McpEndpointConfig{}, ErrMcpSchema
	}
	if _, err := ulid.ParseStrict(input.ID); err != nil {
		return m7flow.McpEndpointConfig{}, ErrMcpSchema
	}
	if err := checked.Pin.Validate(); err != nil {
		return m7flow.McpEndpointConfig{}, err
	}
	if err := ValidateMcpSecretRefs(input.AuthRef, input.EnvSecretRefs); err != nil {
		return m7flow.McpEndpointConfig{}, err
	}
	args, _ := json.Marshal(input.Args)
	if input.Args == nil {
		args = []byte("[]")
	}
	refs, _ := json.Marshal(input.EnvSecretRefs)
	if input.EnvSecretRefs == nil {
		refs = []byte("{}")
	}
	pin, _ := json.Marshal(checked.Pin)
	now := s.clock.Now().UTC().Format(time.RFC3339)
	ep := m7flow.McpEndpointConfig{EndpointID: "mcp-" + input.ID, Transport: input.Transport, Command: input.Command, ArgsJSON: string(args), URL: input.URL, Origin: m7flow.McpOriginManual, SourceTrust: m7flow.McpTrustVerified, Enabled: true, State: checked.State, CreatedAt: now, LastHealthAt: now}
	ep.Security = m7flow.McpEndpointSecurity{AuthRef: input.AuthRef, EnvRefsJSON: string(refs), PinJSON: string(pin), LaunchArgsJSON: string(encodeMcpLaunch(ep, checked.Args, []string{checked.LaunchDigest})), UpdatedAt: now}
	if _, pending := checked.Pin.ToolSchemaDigests["_pending"]; pending && len(checked.Pin.ToolSchemaDigests) == 1 && checked.State != mcp6.StateReady {
		ep.Security.PinJSON = ""
	}
	if ep.State == m7flow.McpStateReady {
		digest, err := (LocalMcpProber{}).Probe(ctx, ep)
		if err != nil {
			return m7flow.McpEndpointConfig{}, err
		}
		ep.PinnedDigest = digest
		ep.CapabilityDigest = digest
	}
	var out m7flow.McpEndpointConfig
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		current, err := tx.GetMcpEndpoint(ep.EndpointID)
		if err == nil {
			if !current.Enabled || current.State == m7flow.McpStateRevoked || current.State == m7flow.McpStateQuarantined || canonicalMcpTarget(current) != canonicalMcpTarget(ep) || current.Security.AuthRef != ep.Security.AuthRef || current.Security.PinJSON != ep.Security.PinJSON || current.Security.LaunchArgsJSON != ep.Security.LaunchArgsJSON {
				return ErrMcpSecurityConflict
			}
			out = current
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		if _, err := tx.FindMcpEndpointByFingerprint(ep.Transport, ep.Command, ep.URL, ep.ArgsJSON); err == nil {
			return ErrMcpSecurityConflict
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		count, err := tx.CountMcpEndpoints()
		if err != nil {
			return err
		}
		if count >= McpMaxEndpoints {
			return ErrMcpQuota
		}
		secure, ok := tx.(mcpSecurityTx)
		if !ok {
			return ErrServiceUnavailable
		}
		if err = tx.PutMcpEndpoint(ep); err != nil {
			return err
		}
		if err = secure.PutMcpSecurity(ep.EndpointID, 0, ep.Security); err != nil {
			return err
		}
		if _, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.add", ResourceType: "mcp_endpoint", ResourceID: ep.EndpointID, Actor: "user/mcp6.register", AfterDigest: digestOf(ep.Security.PinJSON), CreatedAt: now}); err != nil {
			return err
		}
		ep.Security.Version = 1
		out = ep
		return nil
	})
	return out, err
}

// RevokeGatewayEndpoint commits the terminal grant before retiring cached IO.
func (s *McpRuntimeService) RevokeGatewayEndpoint(ctx context.Context, id string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		ep, err := tx.GetMcpEndpoint(id)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
			return ErrMcpNotFound
		}
		if err != nil {
			return err
		}
		if ep.State == m7flow.McpStateRevoked {
			return nil
		}
		if err = tx.SetMcpEndpointEnabled(id, false); err != nil {
			return err
		}
		if err = tx.UpdateMcpEndpointState(id, ep.State, m7flow.McpStateRevoked, nil, s.clock.Now()); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.toggle", ResourceType: "mcp_endpoint", ResourceID: id, Actor: "user/mcp6.revoke", AfterDigest: digestOf(m7flow.McpStateRevoked), CreatedAt: s.clock.Now().UTC().Format(time.RFC3339)})
		return err
	})
	if err == nil && s.invalidate != nil {
		s.invalidate(id)
	}
	return err
}
