package m7app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/audit"
	"github.com/oklog/ulid/v2"
	"regexp"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/lunitide/lunitide/internal/mcp6"
)

var ErrMcpSecurityConflict = errors.New("m7app: MCP security version changed")

type mcpSecurityTx interface {
	PutMcpSecurity(string, int64, m7flow.McpEndpointSecurity) error
}

var mcpRefPattern = regexp.MustCompile(`^secretref:[A-Za-z0-9._/-]{1,248}$`)
var mcpEnvPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func ValidateMcpSecretRefs(auth string, refs map[string]string) error {
	if auth != "" && !mcpRefPattern.MatchString(auth) {
		return ErrMcpSchema
	}
	if len(refs) > 32 {
		return ErrMcpSchema
	}
	for name, ref := range refs {
		if !mcpEnvPattern.MatchString(name) || !mcpRefPattern.MatchString(ref) {
			return ErrMcpSchema
		}
		// Credentials cannot override executable loading, shell startup, or
		// the host's isolation/runtime switches.
		switch name {
		case "PATH", "PATHEXT", "SYSTEMROOT", "COMSPEC", "HOME", "USERPROFILE", "TEMP", "TMP", "NODE_OPTIONS", "NODE_PATH", "PYTHONPATH", "PYTHONHOME", "BASH_ENV", "ENV":
			return ErrMcpSchema
		}
		if strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "DYLD_") || strings.HasPrefix(name, "NPM_CONFIG_") || strings.HasPrefix(name, "UV_") || strings.HasPrefix(name, "STDIOMCP_") {
			return ErrMcpSchema
		}
	}
	return nil
}

func (s *McpRuntimeService) Endpoint(ctx context.Context, id string) (m7flow.McpEndpointConfig, error) {
	var out m7flow.McpEndpointConfig
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error { var err error; out, err = tx.GetMcpEndpoint(id); return err })
	return out, err
}

// ObservePin records the first real handshake without replacing an existing
// authorization. The exact config and credential generation must still match.
func (s *McpRuntimeService) ObservePin(ctx context.Context, observed m7flow.McpEndpointConfig, pin mcp6.CapabilityPin, args []string, digest ...string) (m7flow.McpEndpointSecurity, error) {
	if err := pin.Validate(); err != nil {
		return m7flow.McpEndpointSecurity{}, err
	}
	b, _ := json.Marshal(pin)
	launch := encodeMcpLaunch(observed, args, digest)
	var out m7flow.McpEndpointSecurity
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		current, err := tx.GetMcpEndpoint(observed.EndpointID)
		if err != nil {
			return err
		}
		if canonicalMcpTarget(current) != canonicalMcpTarget(observed) || current.Security.Version != observed.Security.Version {
			return ErrMcpSecurityConflict
		}
		if current.Security.PinJSON != "" {
			if current.Security.PinJSON != string(b) || current.Security.LaunchArgsJSON != string(launch) {
				return ErrMcpDrift
			}
			out = current.Security
			return nil
		}
		secure, ok := tx.(mcpSecurityTx)
		if !ok {
			return ErrServiceUnavailable
		}
		out = current.Security
		out.PinJSON = string(b)
		out.LaunchArgsJSON = string(launch)
		out.UpdatedAt = s.clock.Now().UTC().Format(time.RFC3339)
		if err = secure.PutMcpSecurity(current.EndpointID, out.Version, out); err != nil {
			return err
		}
		out.Version++
		_, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.security.review", ResourceType: "mcp_endpoint", ResourceID: current.EndpointID, Actor: "system/first-admission", AfterDigest: digestOf(out.PinJSON), CreatedAt: out.UpdatedAt})
		return err
	})
	return out, err
}

func (s *McpRuntimeService) ReplacePin(ctx context.Context, observed m7flow.McpEndpointConfig, pin mcp6.CapabilityPin, args []string, digest ...string) (m7flow.McpEndpointSecurity, error) {
	if err := pin.Validate(); err != nil {
		return m7flow.McpEndpointSecurity{}, err
	}
	encoded, _ := json.Marshal(pin)
	launch := encodeMcpLaunch(observed, args, digest)
	var out m7flow.McpEndpointSecurity
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		current, err := tx.GetMcpEndpoint(observed.EndpointID)
		if err != nil {
			return err
		}
		if current.Security.Version != observed.Security.Version || canonicalMcpTarget(current) != canonicalMcpTarget(observed) || current.State == m7flow.McpStateRevoked {
			return ErrMcpSecurityConflict
		}
		secure, ok := tx.(mcpSecurityTx)
		if !ok {
			return ErrServiceUnavailable
		}
		out = current.Security
		out.PinJSON = string(encoded)
		out.LaunchArgsJSON = string(launch)
		out.UpdatedAt = s.clock.Now().UTC().Format(time.RFC3339)
		if err = secure.PutMcpSecurity(current.EndpointID, out.Version, out); err != nil {
			return err
		}
		out.Version++
		if err = tx.UpdateMcpEndpointState(current.EndpointID, current.State, m7flow.McpStateReady, nil, s.clock.Now()); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.security.review", ResourceType: "mcp_endpoint", ResourceID: current.EndpointID, Actor: "user", BeforeDigest: digestOf(current.Security.PinJSON), AfterDigest: digestOf(out.PinJSON), CreatedAt: out.UpdatedAt})
		return err
	})
	if err == nil && s.invalidate != nil {
		s.invalidate(observed.EndpointID)
	}
	return out, err
}

func encodeMcpLaunch(ep m7flow.McpEndpointConfig, args []string, digest []string) []byte {
	lock := mcp.LaunchLock{Args: args, SourceArgs: ep.ArgsJSON}
	if len(digest) > 0 {
		lock.Digest = digest[0]
	}
	b, _ := json.Marshal(lock)
	return b
}

// BindCredential is called only through the private Host channel after the
// secret store accepted an exact endpoint/origin binding. Empty ref revokes it.
func (s *McpRuntimeService) BindCredential(ctx context.Context, id string, expected int64, ref, env string) (m7flow.McpEndpointSecurity, error) {
	var out m7flow.McpEndpointSecurity
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		ep, err := tx.GetMcpEndpoint(id)
		if err != nil {
			return err
		}
		if ep.Security.Version != expected {
			return ErrMcpSecurityConflict
		}
		refs := map[string]string{}
		if ep.Security.EnvRefsJSON != "" && json.Unmarshal([]byte(ep.Security.EnvRefsJSON), &refs) != nil {
			return ErrMcpSchema
		}
		out = ep.Security
		if env == "" {
			if ep.Transport != "https" {
				return ErrMcpSchema
			}
			out.AuthRef = ref
		} else {
			if ep.Transport != "stdio" {
				return ErrMcpSchema
			}
			if ref == "" {
				delete(refs, env)
			} else {
				refs[env] = ref
			}
		}
		if err := ValidateMcpSecretRefs(out.AuthRef, refs); err != nil {
			return err
		}
		b, _ := json.Marshal(refs)
		out.EnvRefsJSON = string(b)
		out.UpdatedAt = s.clock.Now().UTC().Format(time.RFC3339)
		secure, ok := tx.(mcpSecurityTx)
		if !ok {
			return ErrServiceUnavailable
		}
		if err := secure.PutMcpSecurity(id, expected, out); err != nil {
			return err
		}
		out.Version++
		// A credential rotation allows a new authenticated probe; drift pins
		// remain intact. Explicit revoked settings stay revoked.
		_, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "mcp.credential.set", ResourceType: "mcp_endpoint", ResourceID: id, Actor: "host/user", AfterDigest: digestOf(fmt.Sprintf("security-version=%d", out.Version)), CreatedAt: out.UpdatedAt})
		return err
	})
	if err == nil && s.invalidate != nil {
		s.invalidate(id)
	}
	return out, err
}

func (s *McpRuntimeService) SecurityFailure(ctx context.Context, id string, expected int64, drift bool) error {
	return s.uow.TransactMcp(ctx, func(tx McpTx) error {
		ep, err := tx.GetMcpEndpoint(id)
		if err != nil {
			return err
		}
		if ep.Security.Version != expected {
			return ErrMcpSecurityConflict
		}
		to := m7flow.McpStateDegraded
		if drift {
			to = m7flow.McpStateQuarantined
		}
		if ep.State == m7flow.McpStateRevoked {
			return nil
		}
		if err := tx.UpdateMcpEndpointState(id, ep.State, to, nil, s.clock.Now()); err != nil {
			return fmt.Errorf("persist MCP security failure: %w", err)
		}
		return nil
	})
}
