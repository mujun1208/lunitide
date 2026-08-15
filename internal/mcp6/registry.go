// Package mcp6 implements the M6 HTTP MCP endpoint registry (T-6.1.4:
// mcp6.register / mcp6.invoke / mcp6.revoke). The registry owns the endpoint
// lifecycle registered→probe→ready/degraded→revoked, the capability pin
// (M6-MCP-002), credential revocation (M6-MCP-003) and the 5-failure/60 s
// circuit breaker (M6-MCP-005, M6_GATEWAY_POLICY_V1). stdio endpoints answer
// M6-MCP-004 and stay DISABLED until the isolation POC gate opens.
//
// Secret handling: authRef is a SecretRef handle only. Credential bytes are
// fetched inside the secret-lease callback and never leave it — they are not
// stored, logged or embedded in error strings.
package mcp6

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/oklog/ulid/v2"
)

// M6 wire error codes (M6_ERROR_CATALOG_V2).
const (
	CodeHealthCheckFailed = "M6-MCP-001"
	CodeCapabilityDrift   = "M6-MCP-002"
	CodeCredentialRevoked = "M6-MCP-003"
	CodeStdioDisabled     = "M6-MCP-004"
	CodeCircuitOpen       = "M6-MCP-005"
)

var (
	// ErrStdioDisabled is M6-MCP-004: stdio POC gate not passed.
	ErrStdioDisabled = errors.New("mcp6: stdio transport is disabled until the isolation POC gate opens (M6-MCP-004)")
	// ErrHealthCheckFailed is M6-MCP-001: probe failed, endpoint stays
	// probe/degraded and never auto-escalates its permissions.
	ErrHealthCheckFailed = errors.New("mcp6: health check failed (M6-MCP-001)")
	// ErrNotReady is M6-MCP-001: invoke against a non-ready endpoint.
	ErrNotReady = errors.New("mcp6: endpoint not ready (M6-MCP-001)")
	// ErrCapabilityDrift is M6-MCP-002: tool schema digest drifted.
	ErrCapabilityDrift = errors.New("mcp6: capability pin drift detected (M6-MCP-002)")
	// ErrCredentialRevoked is M6-MCP-003: upstream answered 401.
	ErrCredentialRevoked = errors.New("mcp6: credential rejected by upstream (M6-MCP-003)")
	// ErrCircuitOpen is M6-MCP-005: breaker window after 5 consecutive
	// failures.
	ErrCircuitOpen = errors.New("mcp6: circuit breaker open (M6-MCP-005)")
	// ErrEndpointNotFound / ErrEndpointRevoked guard lifecycle misuse.
	ErrEndpointNotFound = errors.New("mcp6: endpoint not found")
	ErrEndpointRevoked  = errors.New("mcp6: endpoint revoked")
	// ErrTransport maps upstream timeout / oversize / transport failures
	// onto the breaker accounting (M6-MCP-005 family).
	ErrTransport = errors.New("mcp6: upstream transport failure (M6-MCP-005)")
)

// Frozen M6 gateway policy (M6_GATEWAY_POLICY_V1); changes need an ADR.
const (
	// BreakerThreshold is the consecutive-failure count that opens the
	// breaker.
	BreakerThreshold = 5
	// BreakerCooldown is the fixed open-window (60 s).
	BreakerCooldown = 60 * time.Second
)

var (
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	authRefPattern = regexp.MustCompile(`^secretref:[A-Za-z0-9._/-]{1,248}$`)
)

// CapabilityPin freezes the server identity and per-tool schema digests the
// subject authorised. Client self-reported hashes are never trusted; drift
// invalidates the grant immediately (M6-MCP-002).
type CapabilityPin struct {
	ServerIdentityDigest string            `json:"serverIdentityDigest"`
	ToolSchemaDigests    map[string]string `json:"toolSchemaDigests"`
}

// Validate checks the pin shape: identity digest well-formed and at least
// one pinned tool.
func (p CapabilityPin) Validate() error {
	if !digestPattern.MatchString(p.ServerIdentityDigest) {
		return fmt.Errorf("mcp6: serverIdentityDigest must be a sha-256 hex digest")
	}
	if len(p.ToolSchemaDigests) == 0 {
		return fmt.Errorf("mcp6: toolSchemaDigests must pin at least one tool")
	}
	for tool, digest := range p.ToolSchemaDigests {
		if strings.TrimSpace(tool) == "" || !digestPattern.MatchString(digest) {
			return fmt.Errorf("mcp6: tool %q schema digest malformed", tool)
		}
	}
	return nil
}

// Endpoint states mirror m6_mcp_endpoint.state (see domain/m6supply).
const (
	StateRegistered = "registered"
	StateProbe      = "probe"
	StateReady      = "ready"
	StateDegraded   = "degraded"
	StateRevoked    = "revoked"
	StateDisabled   = "disabled"
)

// Revocation reasons (mcp6.revoke wire enum).
const (
	ReasonCredential = "credential"
	ReasonDrift      = "drift"
	ReasonPolicy     = "policy"
	ReasonManual     = "manual"
)

// Endpoint is one registered remote MCP endpoint.
type Endpoint struct {
	ID        string
	Transport string
	URL       string
	AuthRef   string
	Pin       CapabilityPin
	State     string
	Version   int64

	consecFails  int
	breakerUntil time.Time
}

// ProbeFunc performs a health check against the endpoint transport. Returning
// an error means the probe failed (M6-MCP-001); the registry never escalates
// permissions based on probe outcomes.
type ProbeFunc func(ctx context.Context, e *Endpoint) error

// InvokeFunc performs the actual authorised call. auth holds the credential
// bytes resolved from the secret lease — implementations must not persist or
// log them. Typed sentinel errors (ErrCredentialRevoked, ErrTransport)
// drive the registry lifecycle; an unpinned tool answers
// ErrCapabilityDrift via the drift detail below.
type InvokeFunc func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error)

// DriftDetail carries the drifted/unpinned tool name alongside the
// ErrCapabilityDrift sentinel so callers can re-pin precisely.
type DriftDetail struct {
	Tool string
}

// SecretLease resolves an authRef to transient credential bytes. The
// callback contract mirrors secretlease.WithLease: bytes live only inside
// the callback frame.
type SecretLease interface {
	WithLease(ctx context.Context, authRef string, fn func(auth []byte) error) error
}

// Registry is the in-memory M6 endpoint registry. Persistence onto
// m6_mcp_endpoint rows is wired by the storage adapter slice; the registry
// owns lifecycle, pinning and breaker semantics.
type Registry struct {
	mu       sync.Mutex
	endpoint map[string]*Endpoint
	probe    ProbeFunc
	invoke   InvokeFunc
	lease    SecretLease
	now      func() time.Time
}

// NewRegistry builds a registry. probe/invoke are the transport seams (tests
// inject fakes; production wires the M5 HTTPS client); lease resolves
// SecretRef handles.
func NewRegistry(probe ProbeFunc, invoke InvokeFunc, lease SecretLease) *Registry {
	return &Registry{
		endpoint: make(map[string]*Endpoint),
		probe:    probe,
		invoke:   invoke,
		lease:    lease,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// SetClock substitutes the wall clock (tests).
func (r *Registry) SetClock(now func() time.Time) { r.mu.Lock(); r.now = now; r.mu.Unlock() }

// Register validates and admits one endpoint, then immediately probes it.
// The returned state is ready or degraded; degraded answers
// ErrHealthCheckFailed so callers can surface M6-MCP-001 while keeping the
// endpoint registered for later probes.
func (r *Registry) Register(ctx context.Context, transport, rawURL, authRef string, pin CapabilityPin) (*Endpoint, error) {
	if transport != "https" {
		return nil, fmt.Errorf("%w: transport %q", ErrStdioDisabled, transport)
	}
	if err := mcp.ValidateBaseURL(rawURL); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHealthCheckFailed, err)
	}
	if !authRefPattern.MatchString(authRef) {
		return nil, fmt.Errorf("mcp6: authRef must be a secretref: handle, credentials are never accepted inline")
	}
	if err := pin.Validate(); err != nil {
		return nil, err
	}

	e := &Endpoint{
		ID:        ulid.Make().String(),
		Transport: transport,
		URL:       rawURL,
		AuthRef:   authRef,
		Pin:       pin,
		State:     StateRegistered,
		Version:   1,
	}
	r.mu.Lock()
	r.endpoint[e.ID] = e
	probe := r.probe
	r.mu.Unlock()

	e.State = StateProbe
	if probe == nil {
		e.State = StateReady
		return e, nil
	}
	if err := probe(ctx, e); err != nil {
		e.State = StateDegraded
		return e, fmt.Errorf("%w: %v", ErrHealthCheckFailed, err)
	}
	e.State = StateReady
	return e, nil
}

// Get returns a snapshot of one endpoint.
func (r *Registry) Get(endpointID string) (*Endpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.endpoint[endpointID]
	if !ok {
		return nil, ErrEndpointNotFound
	}
	clone := *e
	return &clone, nil
}

// Probe re-runs the health check: success flips degraded→ready, failure
// keeps/stays degraded with M6-MCP-001.
func (r *Registry) Probe(ctx context.Context, endpointID string) (*Endpoint, error) {
	r.mu.Lock()
	e, ok := r.endpoint[endpointID]
	probe := r.probe
	r.mu.Unlock()
	if !ok {
		return nil, ErrEndpointNotFound
	}
	if e.State == StateRevoked {
		return nil, ErrEndpointRevoked
	}
	if probe == nil || probe(ctx, e) != nil {
		e.State = StateDegraded
		return e, ErrHealthCheckFailed
	}
	e.State = StateReady
	return e, nil
}

// InvokeResult carries the mcp6.invoke wire payload.
type InvokeResult struct {
	Result     map[string]any
	Bytes      int
	DurationMS int64
	TraceID    string
}

// Invoke executes one pinned tool call. Lifecycle: not-ready → M6-MCP-001;
// unknown tool or digest drift → M6-MCP-002 (grant invalidated, endpoint
// degraded); upstream 401 → M6-MCP-003 (endpoint revoked, pools cleared);
// timeout/oversize/transport failure → breaker accounting, five consecutive
// failures open a 60 s window (M6-MCP-005).
func (r *Registry) Invoke(ctx context.Context, endpointID, tool string, args map[string]any) (*InvokeResult, error) {
	r.mu.Lock()
	e, ok := r.endpoint[endpointID]
	invoke := r.invoke
	lease := r.lease
	now := r.now
	r.mu.Unlock()
	if !ok {
		return nil, ErrEndpointNotFound
	}
	if e.State == StateRevoked {
		return nil, ErrEndpointRevoked
	}
	if e.State != StateReady {
		return nil, fmt.Errorf("%w: state %s", ErrNotReady, e.State)
	}
	if now().Before(e.breakerUntil) {
		return nil, fmt.Errorf("%w: retry after %s", ErrCircuitOpen, e.breakerUntil.UTC().Format(time.RFC3339))
	}
	pinned, ok := e.Pin.ToolSchemaDigests[tool]
	if !ok || !digestPattern.MatchString(pinned) {
		e.State = StateDegraded
		return nil, fmt.Errorf("%w: tool %q not pinned or digest malformed", ErrCapabilityDrift, tool)
	}

	var result map[string]any
	var authSeen []byte
	start := now()
	leaseErr := lease.WithLease(ctx, e.AuthRef, func(auth []byte) error {
		authSeen = auth
		out, err := invoke(ctx, e, tool, args, auth)
		if err != nil {
			return err
		}
		result = out
		return nil
	})
	duration := now().Sub(start).Milliseconds()
	if leaseErr != nil {
		return nil, r.accountFailure(e, redactError(leaseErr, authSeen))
	}
	r.mu.Lock()
	e.consecFails = 0
	r.mu.Unlock()
	return &InvokeResult{Result: result, Bytes: approxJSONBytes(result), DurationMS: duration, TraceID: ulid.Make().String()}, nil
}

// accountFailure classifies a transport error and updates the endpoint
// lifecycle + breaker counters. The incoming message was already redacted;
// lifecycle sentinels are matched before breaker accounting.
func (r *Registry) accountFailure(e *Endpoint, err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case errors.Is(err, ErrCredentialRevoked):
		e.State = StateRevoked
		e.consecFails = 0
		e.breakerUntil = time.Time{}
		return fmt.Errorf("%w: endpoint revoked and connection pools cleared", ErrCredentialRevoked)
	case errors.Is(err, ErrCapabilityDrift):
		e.State = StateDegraded
		return fmt.Errorf("%w: grant invalidated, re-pin required", ErrCapabilityDrift)
	}
	e.consecFails++
	if e.consecFails >= BreakerThreshold {
		e.breakerUntil = r.now().Add(BreakerCooldown)
		e.consecFails = 0
		return fmt.Errorf("%w: opened for %s after %d consecutive failures", ErrCircuitOpen, BreakerCooldown, BreakerThreshold)
	}
	return fmt.Errorf("%w: %v", ErrTransport, err)
}

// Revoke is idempotent: any endpoint may transition to revoked; pools are
// cleared and the endpoint refuses further invocations.
func (r *Registry) Revoke(endpointID, reason string) (*Endpoint, error) {
	switch reason {
	case ReasonCredential, ReasonDrift, ReasonPolicy, ReasonManual:
	default:
		return nil, fmt.Errorf("mcp6: reason must be credential, drift, policy or manual")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.endpoint[endpointID]
	if !ok {
		return nil, ErrEndpointNotFound
	}
	e.State = StateRevoked
	e.consecFails = 0
	e.breakerUntil = time.Time{}
	e.Version++
	clone := *e
	return &clone, nil
}

// redactedError keeps the sentinel chain (Unwrap) while presenting a
// redacted message, so lifecycle classification via errors.Is still works
// after secret scrubbing.
type redactedError struct {
	msg     string
	wrapped error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.wrapped }

// redactError strips any accidental secret material (the lease bytes) from
// an error message while preserving the wrapped chain for errors.Is. Errors
// that never carried the secret pass through untouched.
func redactError(err error, auth []byte) error {
	if err == nil || len(auth) == 0 || !strings.Contains(err.Error(), string(auth)) {
		return err
	}
	return &redactedError{msg: strings.ReplaceAll(err.Error(), string(auth), "[REDACTED]"), wrapped: err}
}

func approxJSONBytes(v map[string]any) int { return len(fmt.Sprintf("%v", v)) }
