// M7 slice 8 application service (T-7.8.x): the MCP settings plane
// (mcp.add/list/toggle/health/market.search). The settings plane owns
// endpoint lifecycle (probe -> ready <-> degraded -> revoked plus the
// quarantined fail-closed side state); invoke itself stays on mcp6.invoke
// per the wire contract. Credentials never round-trip: dsn refs are Secret
// Lease references only, and no method answers secret material.
package m7app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

var (
	// ErrMcpSchema: config failed the mcpServers schema validation
	// (M7-MCP-001, 422).
	ErrMcpSchema = errors.New("m7app: mcp config schema invalid")
	// ErrMcpSource: source unconfirmed / signature-digest failure
	// (M7-MCP-002, 403).
	ErrMcpSource = errors.New("m7app: mcp source unconfirmed")
	// ErrMcpDrift: capability pin drift - endpoint quarantined
	// (M7-MCP-003, 409).
	ErrMcpDrift = errors.New("m7app: mcp capability drift")
	// ErrMcpProbe: transport/session establishment failure (M7-MCP-004,
	// 502).
	ErrMcpProbe = errors.New("m7app: mcp probe failed")
	// ErrMcpRegistry: market registry unreachable (M7-MCP-005, 503 with
	// degraded cache).
	ErrMcpRegistry = errors.New("m7app: mcp registry unreachable")
	// ErrMcpNotFound: endpointId missing or revoked (M7-MCP-006, 404).
	ErrMcpNotFound = errors.New("m7app: mcp endpoint not found")
	// ErrMcpQuota: endpoint count / concurrency cap (429).
	ErrMcpQuota = errors.New("m7app: mcp quota exceeded")
	// ErrMcpTimeout: probe/invoke deadline (504 family).
	ErrMcpTimeout = errors.New("m7app: mcp operation timeout")
)

// Frozen caps.
const (
	McpMaxEndpoints    = 64
	McpMaxProbeTimeout = 30000
)

// McpTx is the slice-8 single-writer transaction.
type McpTx interface {
	PutMcpEndpoint(m7flow.McpEndpointConfig) error
	GetMcpEndpoint(id string) (m7flow.McpEndpointConfig, error)
	FindMcpEndpointByFingerprint(transport, command, urlRef string) (m7flow.McpEndpointConfig, error)
	ListMcpEndpoints(transport string) ([]m7flow.McpEndpointConfig, error)
	CountMcpEndpoints() (int, error)
	UpdateMcpEndpointState(id, from, to string, capabilityDigest *string, checkedAt time.Time) error
	SetMcpEndpointEnabled(id string, enabled bool) error
	PutMcpMarketItem(m7flow.McpMarketItem) error
	GetMcpMarketItem(id string) (m7flow.McpMarketItem, error)
	FindMcpMarketItemByName(name string) (m7flow.McpMarketItem, error)
	ListMcpMarketItems(query string, afterID string, limit int) ([]m7flow.McpMarketItem, error)
	AppendAuditEvent(e audit.Event) (audit.Event, error)
}

// McpUnitOfWork is the slice-8 single-writer boundary.
type McpUnitOfWork interface {
	TransactMcp(ctx context.Context, fn func(McpTx) error) error
}

// McpProber establishes one transport handshake and answers the capability
// digest observed at handshake time (serverIdentity + tool schemas).
type McpProber interface {
	Probe(ctx context.Context, endpoint m7flow.McpEndpointConfig) (capabilityDigest string, err error)
}

// LocalMcpProber is the deterministic default: in-process endpoints report
// a stable digest of their canonical descriptor.
type LocalMcpProber struct{}

func (LocalMcpProber) Probe(_ context.Context, ep m7flow.McpEndpointConfig) (string, error) {
	return m7flow.SHA256Hex([]byte(ep.EndpointID + "|" + ep.Transport + "|" + canonicalMcpTarget(ep))), nil
}

// McpRuntimeService implements the five settings-plane methods.
type McpRuntimeService struct {
	uow     McpUnitOfWork
	clock   Clock
	prober  McpProber
	verifier func(item m7flow.McpMarketItem) bool
	registry func(ctx context.Context) ([]m7flow.McpMarketItem, error)
}

func NewMcpRuntimeService(uow McpUnitOfWork) *McpRuntimeService {
	return &McpRuntimeService{
		uow: uow, clock: systemClock{}, prober: LocalMcpProber{},
		verifier: defaultCatalogVerifier,
		registry: func(context.Context) ([]m7flow.McpMarketItem, error) {
			return nil, ErrMcpRegistry
		},
	}
}

func (s *McpRuntimeService) SetClock(c Clock) { s.clock = c }

// SetProber substitutes the transport prober (tests).
func (s *McpRuntimeService) SetProber(p McpProber) { s.prober = p }

// SetVerifier substitutes the catalog signature verifier (tests).
func (s *McpRuntimeService) SetVerifier(fn func(m7flow.McpMarketItem) bool) { s.verifier = fn }

// SetRegistry substitutes the market registry fetcher (tests).
func (s *McpRuntimeService) SetRegistry(fn func(context.Context) ([]m7flow.McpMarketItem, error)) { s.registry = fn }

// ── mcp.add ─────────────────────────────────────────────────────────────────

// McpAddInput is the mcp.add command.
type McpAddInput struct {
	Origin           string
	Transport        string
	Command          string
	Args             []string
	URL              string
	EnvSecretRefs    map[string]string
	MarketItemID     string
	RiskConfirmed    bool
	Actor            string
	IdempotencyKey   string
}

// McpAddResult answers the endpoint identity and post-probe state.
type McpAddResult struct {
	EndpointID       string
	State            string
	CapabilityDigest string
}

// Add validates the mcpServers-shaped config (M7-MCP-001), enforces source
// trust (M7-MCP-002), probes the transport (M7-MCP-004 on failure) and is
// idempotent per fingerprint + requestId: re-adding the same endpoint
// answers the original endpointId.
func (s *McpRuntimeService) Add(ctx context.Context, in McpAddInput) (McpAddResult, error) {
	if s == nil || s.uow == nil {
		return McpAddResult{}, ErrServiceUnavailable
	}
	if in.Origin != m7flow.McpOriginMarket && in.Origin != m7flow.McpOriginManual {
		return McpAddResult{}, fmt.Errorf("%w: origin %q", ErrMcpSchema, in.Origin)
	}
	transport := in.Transport
	if transport == "" && in.URL == "" && in.Command == "" {
		return McpAddResult{}, fmt.Errorf("%w: transport missing", ErrMcpSchema)
	}
	if transport == "" {
		if in.Command != "" {
			transport = m7flow.McpTransportStdio
		} else {
			transport = m7flow.McpTransportHTTPS
		}
	}
	// schema validation per transport (scenario 46)
	switch transport {
	case m7flow.McpTransportStdio:
		if in.Command == "" || len(in.Args) == 0 {
			return McpAddResult{}, fmt.Errorf("%w: stdio needs command+args", ErrMcpSchema)
		}
		if !m7flow.McpStdioCommandAllowed(in.Command) {
			return McpAddResult{}, fmt.Errorf("%w: command %q not whitelisted", ErrMcpSchema, in.Command)
		}
		if !m7flow.McpArgsSafe(in.Args) {
			return McpAddResult{}, fmt.Errorf("%w: args contain metacharacters", ErrMcpSchema)
		}
	case m7flow.McpTransportHTTPS:
		if in.URL == "" || !strings.HasPrefix(in.URL, "https://") {
			return McpAddResult{}, fmt.Errorf("%w: https url required", ErrMcpSchema)
		}
	default:
		return McpAddResult{}, fmt.Errorf("%w: transport %q", ErrMcpSchema, transport)
	}
	for k, v := range in.EnvSecretRefs {
		if k == "" || v == "" || strings.Contains(strings.ToLower(k), "key") && strings.Contains(v, "sk-") {
			return McpAddResult{}, fmt.Errorf("%w: env carries plaintext credential", ErrMcpSchema)
		}
	}
	// source trust (M7-MCP-002)
	trust := m7flow.McpTrustUnknown
	if in.Origin == m7flow.McpOriginMarket {
		var item m7flow.McpMarketItem
		if err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
			it, err := tx.GetMcpMarketItem(in.MarketItemID)
			item = it
			return err
		}); err != nil {
			return McpAddResult{}, fmt.Errorf("%w: market item %s", ErrMcpSource, in.MarketItemID)
		}
		if !s.verifier(item) {
			return McpAddResult{}, fmt.Errorf("%w: signature/digest failed", ErrMcpSource)
		}
		trust = m7flow.McpTrustSigned
	} else {
		if !in.RiskConfirmed {
			return McpAddResult{}, fmt.Errorf("%w: manual add needs risk confirmation", ErrMcpSource)
		}
		trust = m7flow.McpTrustVerified
	}
	argsJSON, _ := json.Marshal(in.Args)
	var out McpAddResult
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		// idempotent re-add answers the original endpoint
		if existing, err := tx.FindMcpEndpointByFingerprint(transport, in.Command, in.URL); err == nil {
			out = McpAddResult{EndpointID: existing.EndpointID, State: existing.State, CapabilityDigest: existing.CapabilityDigest}
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		n, err := tx.CountMcpEndpoints()
		if err != nil {
			return err
		}
		if n >= McpMaxEndpoints {
			return ErrMcpQuota
		}
		now := s.clock.Now().UTC()
		ep := m7flow.McpEndpointConfig{
			EndpointID:  "mcp-" + ulid.Make().String(),
			Transport:   transport,
			Command:     in.Command,
			ArgsJSON:    string(argsJSON),
			URL:         in.URL,
			Origin:      in.Origin,
			SourceTrust: trust,
			Enabled:     false,
			State:       m7flow.McpStateProbe,
			CreatedAt:   now.Format(time.RFC3339),
		}
		if trust == m7flow.McpTrustUnknown {
			ep.State = m7flow.McpStateQuarantined
		}
		if err := tx.PutMcpEndpoint(ep); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "mcp.add", ResourceType: "mcp_endpoint",
			ResourceID: ep.EndpointID, Actor: actorOr(in.Actor),
			AfterDigest: digestOf(ep.State), CreatedAt: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		out = McpAddResult{EndpointID: ep.EndpointID, State: ep.State}
		return nil
	})
	if err != nil {
		return McpAddResult{}, err
	}
	// probe outside the write tx: failure parks degraded (M7-MCP-004)
	if out.State == m7flow.McpStateProbe {
		hm, herr := s.Health(ctx, out.EndpointID)
		if herr != nil {
			return McpAddResult{EndpointID: out.EndpointID, State: m7flow.McpStateProbe}, nil
		}
		out.State = hm.State
		out.CapabilityDigest = hm.CapabilityDigest
	}
	return out, nil
}

// ── mcp.list / mcp.toggle ──────────────────────────────────────────────────

// List answers endpoints optionally filtered by transport.
func (s *McpRuntimeService) List(ctx context.Context, transport string) ([]m7flow.McpEndpointConfig, error) {
	if transport != "" && transport != m7flow.McpTransportStdio && transport != m7flow.McpTransportHTTPS {
		return nil, fmt.Errorf("%w: transport %q", ErrMcpSchema, transport)
	}
	var out []m7flow.McpEndpointConfig
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		list, err := tx.ListMcpEndpoints(transport)
		out = list
		return err
	})
	return out, err
}

// Toggle flips enabled; repeated toggles are last-write-wins, all audited.
func (s *McpRuntimeService) Toggle(ctx context.Context, endpointID string, enabled bool, actor string) (m7flow.McpEndpointConfig, error) {
	var out m7flow.McpEndpointConfig
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		ep, err := tx.GetMcpEndpoint(endpointID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrMcpNotFound, endpointID)
		}
		if err != nil {
			return err
		}
		if ep.State == m7flow.McpStateRevoked {
			return fmt.Errorf("%w: revoked", ErrMcpNotFound)
		}
		if err := tx.SetMcpEndpointEnabled(endpointID, enabled); err != nil {
			return err
		}
		ep.Enabled = enabled
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "mcp.toggle", ResourceType: "mcp_endpoint",
			ResourceID: endpointID, Actor: actorOr(actor),
			AfterDigest: digestOf(fmt.Sprintf("enabled=%v", enabled)),
			CreatedAt:   s.clock.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return err
		}
		out = ep
		return nil
	})
	if err != nil {
		return m7flow.McpEndpointConfig{}, err
	}
	return out, nil
}

// ── mcp.health ──────────────────────────────────────────────────────────────

// HealthResult answers the probe outcome.
type HealthResult struct {
	State            string
	LatencyMS        int64
	DriftDetected    bool
	CapabilityDigest string
	CheckedAt        string
}

// Health probes one endpoint and drives the state machine: success ->
// ready (pin recorded), failure -> degraded; a capability digest that
// differs from the pin quarantines fail-closed (M7-MCP-003).
func (s *McpRuntimeService) Health(ctx context.Context, endpointID string) (HealthResult, error) {
	var ep m7flow.McpEndpointConfig
	err := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		e, err := tx.GetMcpEndpoint(endpointID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrMcpNotFound, endpointID)
		}
		ep = e
		return err
	})
	if err != nil {
		return HealthResult{}, err
	}
	if ep.State == m7flow.McpStateRevoked {
		return HealthResult{}, fmt.Errorf("%w: revoked", ErrMcpNotFound)
	}
	start := s.clock.Now().UTC()
	digest, perr := s.prober.Probe(ctx, ep)
	latency := s.clock.Now().UTC().Sub(start).Milliseconds()
	now := s.clock.Now().UTC()
	result := HealthResult{LatencyMS: latency, CheckedAt: now.Format(time.RFC3339), CapabilityDigest: digest}
	if perr != nil {
		result.State = m7flow.McpStateDegraded
		_ = s.uow.TransactMcp(ctx, func(tx McpTx) error {
			return tx.UpdateMcpEndpointState(ep.EndpointID, ep.State, m7flow.McpStateDegraded, nil, now)
		})
		return result, nil
	}
	// drift: pinned digest recorded at first ready; mismatch quarantines
	if ep.PinnedDigest != "" && ep.PinnedDigest != digest {
		result.State = m7flow.McpStateQuarantined
		result.DriftDetected = true
		_ = s.uow.TransactMcp(ctx, func(tx McpTx) error {
			if err := tx.UpdateMcpEndpointState(ep.EndpointID, ep.State, m7flow.McpStateQuarantined, &digest, now); err != nil {
				return err
			}
			_, aerr := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "mcp.quarantine", ResourceType: "mcp_endpoint",
				ResourceID: ep.EndpointID, Actor: "system",
				BeforeDigest: ep.PinnedDigest, AfterDigest: digest,
				CreatedAt: now.Format(time.RFC3339),
			})
			return aerr
		})
		return result, nil
	}
	result.State = m7flow.McpStateReady
	pin := digest
	if ep.PinnedDigest != "" {
		pin = ep.PinnedDigest
	}
	_ = s.uow.TransactMcp(ctx, func(tx McpTx) error {
		return tx.UpdateMcpEndpointState(ep.EndpointID, ep.State, m7flow.McpStateReady, &pin, now)
	})
	return result, nil
}

// ── mcp.market.search ───────────────────────────────────────────────────────

// MarketSearch answers catalog items by query; a registry outage degrades
// to the read-only cache (M7-MCP-005).
func (s *McpRuntimeService) MarketSearch(ctx context.Context, query, cursor string, limit int) ([]m7flow.McpMarketItem, bool, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	fresh := true
	items, err := s.registry(ctx)
	if err != nil {
		fresh = false // degraded: serve cache only
	}
	if len(items) > 0 {
		_ = s.uow.TransactMcp(ctx, func(tx McpTx) error {
			for _, it := range items {
				if _, err := tx.GetMcpMarketItem(it.ID); err == nil {
					continue
				}
				_ = tx.PutMcpMarketItem(it)
			}
			return nil
		})
	}
	var out []m7flow.McpMarketItem
	lerr := s.uow.TransactMcp(ctx, func(tx McpTx) error {
		list, err := tx.ListMcpMarketItems(strings.ToLower(query), cursor, limit)
		out = list
		return err
	})
	if lerr != nil {
		return nil, false, lerr
	}
	if !fresh && len(out) == 0 {
		return nil, false, ErrMcpRegistry
	}
	return out, fresh, nil
}

// defaultCatalogVerifier checks the market item signature over
// catalog_digest (HMAC-style digest binding; the production registry
// substitutes an asymmetric verifier).
func defaultCatalogVerifier(item m7flow.McpMarketItem) bool {
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(item.ID+"|"+item.CatalogDigest)))
	return item.Signature == want
}

func canonicalMcpTarget(ep m7flow.McpEndpointConfig) string {
	if ep.Transport == m7flow.McpTransportStdio {
		return ep.Command + "|" + ep.ArgsJSON
	}
	return ep.URL
}