// Package mcp6 implements the M6 MCP endpoint registry (T-6.1.4:
// mcp6.register / mcp6.invoke / mcp6.revoke). The registry owns the endpoint
// lifecycle registered→probe→ready/degraded→revoked, the capability pin
// (M6-MCP-002), credential revocation (M6-MCP-003) and the 5-failure/60 s
// circuit breaker (M6-MCP-005, M6_GATEWAY_POLICY_V1).
//
// stdio transport (M6-MCP-004) is ENABLED since 2026-08-16: the 5A/5B/5C
// isolation evidence passed (red-team PASS, docs/evidence/stdio-5c) and the
// product owner signed the gate open. Admission stays narrow — npx/uvx/node
// whitelist with metacharacter-free args (m7flow) — and every session runs
// under the 5B Job Object spawn engine with a per-call process lifetime.
//
// Secret handling: authRef is a SecretRef handle only. Credential bytes are
// fetched inside the secret-lease callback and never leave it — they are not
// stored, logged or embedded in error strings.
package mcp6

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
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
	// ErrStdioDisabled is M6-MCP-004: the requested transport is neither
	// https nor an admitted stdio shape (whitelisted command with safe
	// args).
	ErrStdioDisabled = errors.New("mcp6: transport refused (M6-MCP-004)")
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
	// Command/Args carry the stdio launch vector (transport == "stdio");
	// URL then holds the display/fingerprint form "stdio://<command>".
	Command string
	Args    []string
	AuthRef string
	Pin     CapabilityPin
	State   string
	Version int64

	consecFails  int
	breakerUntil time.Time
	// toolSchemas caches the last describe result (description + input
	// schema per tool). It is a display/merge cache only — the pin digests
	// stay the drift authority, so a poisoned or stale cache can never
	// broaden what Invoke admits.
	toolSchemas map[string]ToolSchema
}

// ToolSchema is one tool's advertised description and JSON Schema body.
type ToolSchema struct {
	Description string
	InputSchema json.RawMessage
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
	describe DescribeFunc
	now      func() time.Time
}

// DescribeFunc fetches the endpoint's live tool catalogue (name,
// description, input schema). It runs after every successful probe purely
// to refresh the schema cache; failures never change endpoint state.
type DescribeFunc func(ctx context.Context, e *Endpoint) (map[string]ToolSchema, error)

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

// SetDescribeFunc installs the tool-catalogue refresh seam (production
// wires the M5 HTTPS client's /tools listing; tests inject fakes).
func (r *Registry) SetDescribeFunc(fn DescribeFunc) { r.mu.Lock(); r.describe = fn; r.mu.Unlock() }

// refreshToolbox re-reads the endpoint's tool catalogue into the schema
// cache. A describe failure keeps the previous cache; the snapshot callers
// fall back to the pin names, so describe is strictly best-effort.
func (r *Registry) refreshToolbox(ctx context.Context, e *Endpoint) {
	r.mu.Lock()
	describe := r.describe
	r.mu.Unlock()
	if describe == nil {
		return
	}
	schemas, err := describe(ctx, e)
	if err != nil {
		return
	}
	r.mu.Lock()
	e.toolSchemas = schemas
	r.mu.Unlock()
}

// EndpointInput is the wire-shaped registration request shared by both
// transports.
type EndpointInput struct {
	Transport string
	URL       string
	AuthRef   string
	Command   string
	Args      []string
	Pin       CapabilityPin
}

// Register validates and admits one endpoint, then immediately probes it.
// The returned state is ready or degraded; degraded answers
// ErrHealthCheckFailed so callers can surface M6-MCP-001 while keeping the
// endpoint registered for later probes.
func (r *Registry) Register(ctx context.Context, in EndpointInput) (*Endpoint, error) {
	switch in.Transport {
	case "https":
		if err := mcp.ValidateBaseURL(in.URL); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrHealthCheckFailed, err)
		}
		if !authRefPattern.MatchString(in.AuthRef) {
			return nil, fmt.Errorf("mcp6: authRef must be a secretref: handle, credentials are never accepted inline")
		}
	case "stdio":
		// M6-MCP-004 admission: whitelisted launcher, metacharacter-free
		// args, at least one arg (the server spec).
		if in.Command == "" || len(in.Args) == 0 {
			return nil, fmt.Errorf("%w: stdio needs command and args", ErrStdioDisabled)
		}
		if !m7flow.McpStdioCommandAllowed(in.Command) {
			return nil, fmt.Errorf("%w: command %q not whitelisted", ErrStdioDisabled, in.Command)
		}
		if !m7flow.McpArgsSafe(in.Args) {
			return nil, fmt.Errorf("%w: args contain metacharacters", ErrStdioDisabled)
		}
		in.URL = "stdio://" + in.Command
		in.AuthRef = "secretref:stdio/" + in.Command
	default:
		return nil, fmt.Errorf("%w: transport %q", ErrStdioDisabled, in.Transport)
	}
	if err := in.Pin.Validate(); err != nil {
		return nil, err
	}

	e := &Endpoint{
		ID:        ulid.Make().String(),
		Transport: in.Transport,
		URL:       in.URL,
		Command:   in.Command,
		Args:      append([]string(nil), in.Args...),
		AuthRef:   in.AuthRef,
		Pin:       in.Pin,
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
	r.refreshToolbox(ctx, e)
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

// ReadyTool is one pinned, callable tool on a ready endpoint. Description
// and Schema carry the last describe cache when available (zero values
// mean the catalogue has not been fetched — callers fall back to a
// pass-through schema).
type ReadyTool struct {
	EndpointID  string
	Tool        string
	Description string
	Schema      json.RawMessage
}

// ReadyToolSnapshot lists every pinned tool on every ready endpoint. The
// chat tool loop merges this snapshot into the model tool list; Invoke
// re-checks state, pinning and the breaker per call, so a stale snapshot
// can never bypass the lifecycle gates.
func (r *Registry) ReadyToolSnapshot() []ReadyTool {
	r.mu.Lock()
	defer r.mu.Unlock()
	var tools []ReadyTool
	for id, e := range r.endpoint {
		if e.State != StateReady {
			continue
		}
		for tool := range e.Pin.ToolSchemaDigests {
			entry := ReadyTool{EndpointID: id, Tool: tool}
			if cached, ok := e.toolSchemas[tool]; ok {
				entry.Description = cached.Description
				entry.Schema = append(json.RawMessage(nil), cached.InputSchema...)
			}
			tools = append(tools, entry)
		}
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].EndpointID != tools[j].EndpointID {
			return tools[i].EndpointID < tools[j].EndpointID
		}
		return tools[i].Tool < tools[j].Tool
	})
	return tools
}

// Probe re-runs the health check: success flips degraded→ready, failure
// keeps/stays degraded with M6-MCP-001. All endpoint field reads/writes
// happen under r.mu; the returned endpoint is a clone so callers never
// race later state transitions.
func (r *Registry) Probe(ctx context.Context, endpointID string) (*Endpoint, error) {
	r.mu.Lock()
	e, ok := r.endpoint[endpointID]
	probe := r.probe
	if ok && e.State == StateRevoked {
		r.mu.Unlock()
		return nil, ErrEndpointRevoked
	}
	r.mu.Unlock()
	if !ok {
		return nil, ErrEndpointNotFound
	}
	if probe == nil || probe(ctx, e) != nil {
		r.mu.Lock()
		if e.State != StateRevoked {
			e.State = StateDegraded
		}
		degraded := *e
		r.mu.Unlock()
		return &degraded, ErrHealthCheckFailed
	}
	r.mu.Lock()
	if e.State != StateRevoked {
		e.State = StateReady
	}
	ready := *e
	r.mu.Unlock()
	r.refreshToolbox(ctx, e)
	return &ready, nil
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
	if ok {
		if e.State == StateRevoked {
			r.mu.Unlock()
			return nil, ErrEndpointRevoked
		}
		if e.State != StateReady {
			state := e.State
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: state %s", ErrNotReady, state)
		}
		if now().Before(e.breakerUntil) {
			until := e.breakerUntil
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: retry after %s", ErrCircuitOpen, until.UTC().Format(time.RFC3339))
		}
		pinned, pinnedOK := e.Pin.ToolSchemaDigests[tool]
		if !pinnedOK || !digestPattern.MatchString(pinned) {
			e.State = StateDegraded
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: tool %q not pinned or digest malformed", ErrCapabilityDrift, tool)
		}
	}
	r.mu.Unlock()
	if !ok {
		return nil, ErrEndpointNotFound
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
