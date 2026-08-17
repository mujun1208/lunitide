// Package mcapp implements the M10 wave-3 MCP-market service: catalog
// browse over the insert-only 0060 cache, the 8-rule connector-config
// validation chain, single-use confirmation tokens binding lifecycle
// operations (install/uninstall/update), an in-process rate limiter
// (11 market operations per minute) and per-endpoint usage statistics.
// Secrets never round-trip: env entries are Secret Lease references only.
package mcapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// Service-level errors mapped by the Bridge handlers onto M10-MC codes.
var (
	// ErrMcSchema: config failed the 8-rule validation chain (M10-MC-001).
	ErrMcSchema = errors.New("mcapp: connector config invalid")
	// ErrMcConfirm: token missing/expired/consumed/mismatched (M10-MC-002).
	ErrMcConfirm = errors.New("mcapp: confirmation token rejected")
	// ErrMcSource: market signature/digest verification failed (M10-MC-003).
	ErrMcSource = errors.New("mcapp: market source untrusted")
	// ErrMcNotFound: item or endpoint missing / revoked (M10-MC-004).
	ErrMcNotFound = errors.New("mcapp: market resource not found")
	// ErrMcQuota: endpoint cap or fingerprint duplicate (M10-MC-005).
	ErrMcQuota = errors.New("mcapp: endpoint quota exceeded")
	// ErrMcRateLimited: >11 market operations per minute (M10-MC-006).
	ErrMcRateLimited = errors.New("mcapp: market rate limited")
	// ErrMcRegistry: catalog registry unreachable and cache empty (M10-MC-007).
	ErrMcRegistry = errors.New("mcapp: market registry unreachable")
)

// Frozen caps.
const (
	// McRateLimitPerMinute bounds the combined confirm/install/uninstall/
	///update operations (design: 11 per minute).
	McRateLimitPerMinute = 11
	// McConfirmTTL is the confirmation-token lifetime.
	McConfirmTTL = 5 * time.Minute
	// McMaxArgs / McMaxEnv bound the config surface.
	McMaxArgs = 32
	McMaxEnv  = 32
)

// Validation rule identifiers (frozen wire contract).
const (
	RuleTransport    = "MC-VR-01"
	RuleStdioCommand = "MC-VR-02"
	RuleStdioArgs    = "MC-VR-03"
	RuleHTTPSURL     = "MC-VR-04"
	RuleSSRF         = "MC-VR-05"
	RuleEnvSecrets   = "MC-VR-06"
	RuleLengths      = "MC-VR-07"
	RuleQuota        = "MC-VR-08"
)

// Confirm-method identifiers bound into each token.
const (
	ConfirmMethodInstall   = "mc.connector.install"
	ConfirmMethodUninstall = "mc.connector.uninstall"
	ConfirmMethodUpdate    = "mc.connector.update"
)

// Tx is the wave-3 single-writer transaction (satisfied by the shared
// agent-runtime tx alongside the slice-8 surface).
type Tx interface {
	ListMcMarket(query, transportHint, afterID string, limit int) ([]m7flow.McpMarketItem, error)
	GetMcpMarketItem(id string) (m7flow.McpMarketItem, error)
	FindMcpMarketItemByName(name string) (m7flow.McpMarketItem, error)
	PutMcpMarketItem(m7flow.McpMarketItem) error
	GetMcpEndpoint(id string) (m7flow.McpEndpointConfig, error)
	FindMcpEndpointByFingerprint(transport, command, urlRef string) (m7flow.McpEndpointConfig, error)
	ListMcpEndpoints(transport string) ([]m7flow.McpEndpointConfig, error)
	CountMcpEndpoints() (int, error)
	PutMcpEndpoint(m7flow.McpEndpointConfig) error
	UpdateMcpEndpointTarget(id, urlRef, argsJSON string) error
	SetMcpEndpointEnabled(id string, enabled bool) error
	UpdateMcpEndpointState(id, from, to string, capabilityDigest *string, checkedAt time.Time) error
	AppendAuditEvent(e audit.Event) (audit.Event, error)
	PutConfirmToken(ConfirmTokenRow) error
	GetConfirmToken(tokenHash string) (ConfirmTokenRow, error)
	ConsumeConfirmToken(tokenHash, consumedAt string) error
	UpsertEndpointUsage(endpointID string, delta UsageDelta, at time.Time) error
	GetEndpointUsage(endpointID string) (EndpointUsage, error)
	ListEndpointUsage() ([]EndpointUsage, error)
}

// UnitOfWork is the wave-3 single-writer boundary.
type UnitOfWork interface {
	TransactMc(ctx context.Context, fn func(Tx) error) error
}

// ConfirmTokenRow is one persisted confirmation token (hash only - the
// raw token never touches storage).
type ConfirmTokenRow struct {
	TokenHash  string
	Method     string
	Target     string
	Digest     string
	IssuedAt   string
	ExpiresAt  string
	ConsumedAt string
}

// UsageDelta is the per-operation usage increment.
type UsageDelta struct {
	Installs   int
	Updates    int
	Uninstalls int
}

// EndpointUsage is one aggregated usage row joined with endpoint state.
type EndpointUsage struct {
	EndpointID string `json:"endpointId"`
	Installs   int    `json:"installs"`
	Updates    int    `json:"updates"`
	Uninstalls int    `json:"uninstalls"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
	Transport  string `json:"transport"`
	State      string `json:"state"`
	Origin     string `json:"origin"`
	Enabled    bool   `json:"enabled"`
}

// ConfigInput is the normalized connector config subject to validation.
type ConfigInput struct {
	Transport     string            `json:"transport"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	URL           string            `json:"url"`
	EnvSecretRefs map[string]string `json:"envSecretRefs"`
}

// ValidationCheck is one rule outcome in the validation chain.
type ValidationCheck struct {
	Rule   string `json:"rule"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

// ValidationResult carries the full chain outcome.
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Checks []ValidationCheck `json:"checks"`
}

// rateWindow is the in-process sliding-window limiter shared by the four
// lifecycle operations.
type rateWindow struct {
	mu     sync.Mutex
	stamps []time.Time
}

func (w *rateWindow) allow(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := now.Add(-time.Minute)
	kept := w.stamps[:0]
	for _, ts := range w.stamps {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	w.stamps = kept
	if len(w.stamps) >= McRateLimitPerMinute {
		return false
	}
	w.stamps = append(w.stamps, now)
	return true
}

// Service implements the mc.* market surface.
type Service struct {
	uow      UnitOfWork
	clock    m7app.Clock
	prober   m7app.McpProber
	verifier func(m7flow.McpMarketItem) bool
	registry func(ctx context.Context) ([]m7flow.McpMarketItem, error)
	limiter  rateWindow
}

// New returns a Service over the given unit of work.
func New(uow UnitOfWork) *Service {
	return &Service{
		uow: uow, clock: systemClock{}, prober: m7app.LocalMcpProber{},
		verifier: defaultVerifier,
		registry: func(context.Context) ([]m7flow.McpMarketItem, error) {
			return nil, ErrMcRegistry
		},
	}
}

// SetClock substitutes the clock (tests).
func (s *Service) SetClock(c m7app.Clock) { s.clock = c }

// SetProber substitutes the transport prober (tests).
func (s *Service) SetProber(p m7app.McpProber) { s.prober = p }

// SetVerifier substitutes the catalog signature verifier (tests).
func (s *Service) SetVerifier(fn func(m7flow.McpMarketItem) bool) { s.verifier = fn }

// SetRegistry substitutes the market registry fetcher (tests).
func (s *Service) SetRegistry(fn func(context.Context) ([]m7flow.McpMarketItem, error)) { s.registry = fn }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func defaultVerifier(item m7flow.McpMarketItem) bool {
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(item.ID+"|"+item.CatalogDigest)))
	return item.Signature == want
}

func actorOr(actor string) string {
	if actor == "" {
		return "renderer"
	}
	return actor
}

// ── mc.market.list ──────────────────────────────────────────────────────────

// MarketItemDTO is one catalog card.
type MarketItemDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Publisher     string `json:"publisher"`
	Description   string `json:"description"`
	TransportHint string `json:"transportHint"`
	CatalogDigest string `json:"catalogDigest"`
	FetchedAt     string `json:"fetchedAt"`
}

// MarketList browses the catalog with a transport filter; a registry
// outage degrades to the read-only cache (fresh=false).
func (s *Service) MarketList(ctx context.Context, query, transportHint, cursor string, limit int) ([]MarketItemDTO, bool, string, error) {
	if transportHint != "" && transportHint != m7flow.McpTransportStdio && transportHint != m7flow.McpTransportHTTPS {
		return nil, false, "", fmt.Errorf("%w: transportHint %q", ErrMcSchema, transportHint)
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	fresh := true
	items, err := s.registry(ctx)
	if err != nil {
		fresh = false
	}
	if len(items) > 0 {
		_ = s.uow.TransactMc(ctx, func(tx Tx) error {
			for _, it := range items {
				if _, err := tx.GetMcpMarketItem(it.ID); err == nil {
					continue
				}
				_ = tx.PutMcpMarketItem(it)
			}
			return nil
		})
	}
	var cached []m7flow.McpMarketItem
	lerr := s.uow.TransactMc(ctx, func(tx Tx) error {
		list, err := tx.ListMcMarket(strings.ToLower(query), transportHint, cursor, limit)
		cached = list
		return err
	})
	if lerr != nil {
		return nil, false, "", lerr
	}
	if !fresh && len(cached) == 0 {
		return nil, false, "", ErrMcRegistry
	}
	out := make([]MarketItemDTO, 0, len(cached))
	for _, it := range cached {
		out = append(out, MarketItemDTO{
			ID: it.ID, Name: it.Name, Publisher: it.Publisher, Description: it.Description,
			TransportHint: it.TransportHint, CatalogDigest: it.CatalogDigest, FetchedAt: it.FetchedAt,
		})
	}
	next := ""
	if len(out) == limit {
		next = out[len(out)-1].ID
	}
	return out, fresh, next, nil
}

// ── mc.market.detail ────────────────────────────────────────────────────────

// MarketDetail answers one catalog item plus the server-side validation
// chain pre-check over its install config.
func (s *Service) MarketDetail(ctx context.Context, itemID string) (m7flow.McpMarketItem, ConfigInput, ValidationResult, error) {
	var item m7flow.McpMarketItem
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		it, err := tx.GetMcpMarketItem(itemID)
		item = it
		return err
	})
	if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return m7flow.McpMarketItem{}, ConfigInput{}, ValidationResult{}, fmt.Errorf("%w: item %s", ErrMcNotFound, itemID)
	}
	if err != nil {
		return m7flow.McpMarketItem{}, ConfigInput{}, ValidationResult{}, err
	}
	cfg, perr := parseInstallConfig(item.InstallConfigJSON)
	if perr != nil {
		return m7flow.McpMarketItem{}, ConfigInput{}, ValidationResult{}, fmt.Errorf("%w: install config: %v", ErrMcSchema, perr)
	}
	res := s.validateRules(ctx, cfg)
	return item, cfg, res, nil
}

// parseInstallConfig strictly decodes one market install config document.
func parseInstallConfig(raw string) (ConfigInput, error) {
	var cfg ConfigInput
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return ConfigInput{}, err
	}
	return cfg, nil
}

// ── mc.config.validate ──────────────────────────────────────────────────────

// ValidateConfig runs the full 8-rule chain (quota + fingerprint included)
// without writing anything.
func (s *Service) ValidateConfig(ctx context.Context, cfg ConfigInput) (ValidationResult, error) {
	if s == nil || s.uow == nil {
		return ValidationResult{}, ErrMcNotFound
	}
	res := s.validateRules(ctx, cfg)
	quota := s.checkQuota(ctx, cfg, "")
	res.Checks = append(res.Checks, quota)
	res.Valid = res.Valid && quota.Passed
	return res, nil
}

// validateRules runs the pure rules R1-R7.
func (s *Service) validateRules(_ context.Context, cfg ConfigInput) ValidationResult {
	res := ValidationResult{Valid: true, Checks: make([]ValidationCheck, 0, 8)}
	fail := func(rule, format string, args ...any) {
		res.Checks = append(res.Checks, ValidationCheck{Rule: rule, Passed: false, Reason: fmt.Sprintf(format, args...)})
		res.Valid = false
	}
	pass := func(rule string) {
		res.Checks = append(res.Checks, ValidationCheck{Rule: rule, Passed: true})
	}

	// R1 transport: explicit value must be legal and consistent with the
	// command/url evidence; both present is ambiguous.
	transport := cfg.Transport
	if transport != "" && transport != m7flow.McpTransportStdio && transport != m7flow.McpTransportHTTPS {
		fail(RuleTransport, "transport %q must be stdio or https", transport)
	} else if cfg.Command != "" && cfg.URL != "" {
		fail(RuleTransport, "command and url are mutually exclusive")
	} else if cfg.Command == "" && cfg.URL == "" {
		fail(RuleTransport, "command or url required")
	} else {
		if transport == "" {
			if cfg.Command != "" {
				transport = m7flow.McpTransportStdio
			} else {
				transport = m7flow.McpTransportHTTPS
			}
		}
		if transport == m7flow.McpTransportStdio && cfg.URL != "" {
			fail(RuleTransport, "stdio transport cannot carry a url")
		}
		if transport == m7flow.McpTransportHTTPS && cfg.Command != "" {
			fail(RuleTransport, "https transport cannot carry a command")
		}
		if res.Valid {
			pass(RuleTransport)
		}
	}

	// R2/R3 stdio surface.
	if transport == m7flow.McpTransportStdio || (transport == "" && cfg.Command != "") {
		if cfg.Command == "" {
			fail(RuleStdioCommand, "stdio needs a command")
		} else if !m7flow.McpStdioCommandAllowed(cfg.Command) {
			fail(RuleStdioCommand, "command %q not whitelisted", cfg.Command)
		} else {
			pass(RuleStdioCommand)
		}
		if len(cfg.Args) == 0 {
			fail(RuleStdioArgs, "stdio needs at least one arg")
		} else if len(cfg.Args) > McMaxArgs {
			fail(RuleStdioArgs, "too many args (%d > %d)", len(cfg.Args), McMaxArgs)
		} else if !m7flow.McpArgsSafe(cfg.Args) {
			fail(RuleStdioArgs, "args contain shell metacharacters")
		} else {
			pass(RuleStdioArgs)
		}
	}

	// R4/R5 https surface.
	if transport == m7flow.McpTransportHTTPS || (transport == "" && cfg.URL != "") {
		if !strings.HasPrefix(cfg.URL, "https://") || len(cfg.URL) > 2048 {
			fail(RuleHTTPSURL, "https url required (max 2048 chars)")
		} else if _, err := url.Parse(cfg.URL); err != nil {
			fail(RuleHTTPSURL, "url unparseable: %v", err)
		} else {
			pass(RuleHTTPSURL)
		}
		if ok, reason := ssrfCheck(cfg.URL); !ok {
			fail(RuleSSRF, "%s", reason)
		} else {
			pass(RuleSSRF)
		}
	}

	// R6 env secret refs.
	envOK := true
	if len(cfg.EnvSecretRefs) > McMaxEnv {
		fail(RuleEnvSecrets, "too many env refs (%d > %d)", len(cfg.EnvSecretRefs), McMaxEnv)
		envOK = false
	} else {
		for k, v := range cfg.EnvSecretRefs {
			if k == "" || v == "" {
				fail(RuleEnvSecrets, "env ref %q empty", k)
				envOK = false
				break
			}
			if strings.Contains(strings.ToLower(k), "key") && strings.Contains(v, "sk-") {
				fail(RuleEnvSecrets, "env ref %q carries a plaintext credential", k)
				envOK = false
				break
			}
		}
	}
	if envOK {
		pass(RuleEnvSecrets)
	}

	// R7 length bounds.
	if len(cfg.Command) > 512 {
		fail(RuleLengths, "command too long (%d > 512)", len(cfg.Command))
	} else {
		tooLong := false
		for _, a := range cfg.Args {
			if len(a) > 512 {
				fail(RuleLengths, "arg too long (%d > 512)", len(a))
				tooLong = true
				break
			}
		}
		if !tooLong {
			pass(RuleLengths)
		}
	}
	return res
}

// checkQuota is R8: endpoint cap + fingerprint duplicate (self excluded).
func (s *Service) checkQuota(ctx context.Context, cfg ConfigInput, selfID string) ValidationCheck {
	transport := cfg.Transport
	if transport == "" {
		if cfg.Command != "" {
			transport = m7flow.McpTransportStdio
		} else {
			transport = m7flow.McpTransportHTTPS
		}
	}
	check := ValidationCheck{Rule: RuleQuota, Passed: true}
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		if n, err := tx.CountMcpEndpoints(); err != nil {
			return err
		} else if n >= m7app.McpMaxEndpoints {
			check.Passed = false
			check.Reason = fmt.Sprintf("endpoint cap reached (%d)", m7app.McpMaxEndpoints)
			return nil
		}
		if existing, err := tx.FindMcpEndpointByFingerprint(transport, cfg.Command, cfg.URL); err == nil {
			if existing.EndpointID != selfID {
				check.Passed = false
				check.Reason = "transport target already registered: " + existing.EndpointID
			}
		}
		return nil
	})
	if err != nil {
		check.Passed = false
		check.Reason = "quota check unavailable"
	}
	return check
}

// ssrfCheck enforces the egress policy on one https url: allowed port,
// and every resolved address outside loopback/private/link-local/CGNAT
// ranges (hostname resolution failures fail closed).
func ssrfCheck(rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, "url unparseable"
	}
	host := u.Hostname()
	if host == "" {
		return false, "url host missing"
	}
	if port := u.Port(); port != "" {
		allowed := false
		for _, p := range []string{"80", "443", "8080", "8443"} {
			if port == p {
				allowed = true
				break
			}
		}
		if !allowed {
			return false, "port " + port + " not allowed"
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if m7flow.ToolSSRFReject(ip.String()) {
			return false, "address " + host + " is a forbidden network target"
		}
		return true, ""
	}
	// IPv6 literals keep their brackets out of Hostname(); resolve the name
	// and require every address to pass the IP policy.
	resolver := net.DefaultResolver
	addrs, err := resolver.LookupIPAddr(context.Background(), host)
	if err != nil || len(addrs) == 0 {
		return false, "host " + host + " does not resolve"
	}
	for _, a := range addrs {
		if m7flow.ToolSSRFReject(a.IP.String()) {
			return false, "host " + host + " resolves to forbidden address " + a.IP.String()
		}
	}
	return true, ""
}

// ── mc.confirm.token ────────────────────────────────────────────────────────

// IssueConfirmToken mints one single-use token bound to method+target.
func (s *Service) IssueConfirmToken(ctx context.Context, method, target, digest string) (string, string, error) {
	if s == nil || s.uow == nil {
		return "", "", ErrMcNotFound
	}
	if method != ConfirmMethodInstall && method != ConfirmMethodUninstall && method != ConfirmMethodUpdate {
		return "", "", fmt.Errorf("%w: method %q", ErrMcSchema, method)
	}
	if len(target) < 1 || len(target) > 256 {
		return "", "", fmt.Errorf("%w: target length", ErrMcSchema)
	}
	now := s.clock.Now().UTC()
	if !s.limiter.allow(now) {
		return "", "", ErrMcRateLimited
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	row := ConfirmTokenRow{
		TokenHash:  hex.EncodeToString(sum[:]),
		Method:     method,
		Target:     target,
		Digest:     digest,
		IssuedAt:   now.Format(time.RFC3339),
		ExpiresAt:  now.Add(McConfirmTTL).Format(time.RFC3339),
	}
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		if err := tx.PutConfirmToken(row); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "mc.confirm.issued",
			ResourceType: "mc_confirm_token", ResourceID: row.TokenHash[:16],
			Actor: "renderer", CreatedAt: row.IssuedAt,
		})
		return err
	})
	if err != nil {
		return "", "", err
	}
	return token, row.ExpiresAt, nil
}

// consumeConfirm validates and burns one token inside the caller's
// transaction (single-use gate: exactly one row may flip consumed_at).
func (s *Service) consumeConfirm(tx Tx, method, target, token string, now time.Time) error {
	if token == "" {
		return fmt.Errorf("%w: token missing", ErrMcConfirm)
	}
	sum := sha256.Sum256([]byte(token))
	row, err := tx.GetConfirmToken(hex.EncodeToString(sum[:]))
	if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: unknown token", ErrMcConfirm)
	}
	if err != nil {
		return err
	}
	if row.Method != method || row.Target != target {
		return fmt.Errorf("%w: token bound to %s/%s", ErrMcConfirm, row.Method, row.Target)
	}
	if err := tx.ConsumeConfirmToken(row.TokenHash, now.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("%w: token expired or already used", ErrMcConfirm)
	}
	return nil
}

// installTarget derives the canonical confirm-target for an install:
// the market item id, or the fingerprint digest for manual configs.
func installTarget(in InstallInput) string {
	if in.MarketItemID != "" {
		return in.MarketItemID
	}
	return "fp:" + fingerprintDigest(in.Transport, in.Command, in.URL)
}

func fingerprintDigest(transport, command, urlRef string) string {
	sum := sha256.Sum256([]byte(transport + "|" + command + "|" + urlRef))
	return hex.EncodeToString(sum[:])
}

// ── mc.connector.install ────────────────────────────────────────────────────

// InstallInput is the mc.connector.install command.
type InstallInput struct {
	Origin        string
	Transport     string
	Command       string
	Args          []string
	URL           string
	EnvSecretRefs map[string]string
	MarketItemID  string
	ConfirmToken  string
	Actor         string
}

// InstallResult answers the endpoint identity and post-probe state.
type InstallResult struct {
	EndpointID       string
	State            string
	CapabilityDigest string
}

// Install runs the full chain: validation (R1-R8), confirmation token,
// source trust, endpoint creation with audit + usage, then probe.
func (s *Service) Install(ctx context.Context, in InstallInput) (InstallResult, ValidationResult, error) {
	if s == nil || s.uow == nil {
		return InstallResult{}, ValidationResult{}, ErrMcNotFound
	}
	if in.Origin != m7flow.McpOriginMarket && in.Origin != m7flow.McpOriginManual {
		return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: origin %q", ErrMcSchema, in.Origin)
	}
	now := s.clock.Now().UTC()
	if !s.limiter.allow(now) {
		return InstallResult{}, ValidationResult{}, ErrMcRateLimited
	}

	cfg := ConfigInput{Transport: in.Transport, Command: in.Command, Args: in.Args, URL: in.URL, EnvSecretRefs: in.EnvSecretRefs}
	if in.Origin == m7flow.McpOriginMarket {
		if in.MarketItemID == "" {
			return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: marketItemId required", ErrMcSchema)
		}
		var item m7flow.McpMarketItem
		err := s.uow.TransactMc(ctx, func(tx Tx) error {
			it, err := tx.GetMcpMarketItem(in.MarketItemID)
			item = it
			return err
		})
		if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: item %s", ErrMcNotFound, in.MarketItemID)
		}
		if err != nil {
			return InstallResult{}, ValidationResult{}, err
		}
		if !s.verifier(item) {
			return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: signature/digest failed", ErrMcSource)
		}
		itemCfg, perr := parseInstallConfig(item.InstallConfigJSON)
		if perr != nil {
			return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: install config: %v", ErrMcSchema, perr)
		}
		cfg = itemCfg
	}

	res := s.validateRules(ctx, cfg)
	quota := s.checkQuota(ctx, cfg, "")
	res.Checks = append(res.Checks, quota)
	res.Valid = res.Valid && quota.Passed
	if !res.Valid {
		return InstallResult{}, res, ErrMcSchema
	}

	transport := cfg.Transport
	if transport == "" {
		if cfg.Command != "" {
			transport = m7flow.McpTransportStdio
		} else {
			transport = m7flow.McpTransportHTTPS
		}
	}
	trust := m7flow.McpTrustVerified
	if in.Origin == m7flow.McpOriginMarket {
		trust = m7flow.McpTrustSigned
	}
	argsJSON, _ := json.Marshal(cfg.Args)
	ts := now.Format(time.RFC3339)
	var out InstallResult
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		// confirmation token burned inside the same transaction
		if err := s.consumeConfirm(tx, ConfirmMethodInstall, installTarget(in), in.ConfirmToken, now); err != nil {
			return err
		}
		// idempotent re-install answers the original endpoint
		if existing, err := tx.FindMcpEndpointByFingerprint(transport, cfg.Command, cfg.URL); err == nil {
			out = InstallResult{EndpointID: existing.EndpointID, State: existing.State, CapabilityDigest: existing.CapabilityDigest}
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		ep := m7flow.McpEndpointConfig{
			EndpointID: "mcp-" + ulid.Make().String(),
			Transport:  transport, Command: cfg.Command, ArgsJSON: string(argsJSON), URL: cfg.URL,
			Origin: in.Origin, SourceTrust: trust, Enabled: false,
			State: m7flow.McpStateProbe, CreatedAt: ts,
		}
		if err := tx.PutMcpEndpoint(ep); err != nil {
			return err
		}
		if err := tx.UpsertEndpointUsage(ep.EndpointID, UsageDelta{Installs: 1}, now); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "mc.connector.installed",
			ResourceType: "mcp_endpoint", ResourceID: ep.EndpointID,
			Actor: actorOr(in.Actor), AfterDigest: fingerprintDigest(transport, cfg.Command, cfg.URL),
			CreatedAt: ts,
		}); err != nil {
			return err
		}
		out = InstallResult{EndpointID: ep.EndpointID, State: ep.State}
		return nil
	})
	if err != nil {
		return InstallResult{}, res, err
	}
	// probe outside the write tx: failure parks the endpoint in probe
	// state for mcp.health to retry (M7-MCP-004 semantics).
	if out.State == m7flow.McpStateProbe {
		s.reprobe(ctx, out.EndpointID, m7flow.McpStateProbe)
		ep, err := s.getEndpoint(ctx, out.EndpointID)
		if err == nil {
			out.State = ep.State
			out.CapabilityDigest = ep.CapabilityDigest
		}
	}
	return out, res, nil
}

// ── mc.connector.uninstall ──────────────────────────────────────────────────

// Uninstall revokes one endpoint (terminal state) after token
// confirmation; usage statistics survive.
func (s *Service) Uninstall(ctx context.Context, endpointID, confirmToken, actor string) (string, error) {
	if s == nil || s.uow == nil {
		return "", ErrMcNotFound
	}
	now := s.clock.Now().UTC()
	if !s.limiter.allow(now) {
		return "", ErrMcRateLimited
	}
	ts := now.Format(time.RFC3339)
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		ep, err := tx.GetMcpEndpoint(endpointID)
		if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrMcNotFound, endpointID)
		}
		if err != nil {
			return err
		}
		if ep.State == m7flow.McpStateRevoked {
			return nil // idempotent
		}
		if err := s.consumeConfirm(tx, ConfirmMethodUninstall, endpointID, confirmToken, now); err != nil {
			return err
		}
		if err := tx.SetMcpEndpointEnabled(endpointID, false); err != nil {
			return err
		}
		if err := tx.UpdateMcpEndpointState(endpointID, ep.State, m7flow.McpStateRevoked, nil, now); err != nil {
			return err
		}
		if err := tx.UpsertEndpointUsage(endpointID, UsageDelta{Uninstalls: 1}, now); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "mc.connector.uninstalled",
			ResourceType: "mcp_endpoint", ResourceID: endpointID,
			Actor: actorOr(actor), CreatedAt: ts,
		})
		return err
	})
	if err != nil {
		return "", err
	}
	return m7flow.McpStateRevoked, nil
}

// ── mc.connector.update ─────────────────────────────────────────────────────

// UpdateInput swaps the https url or stdio args of one endpoint after
// token confirmation; digests clear so the next probe re-pins.
type UpdateInput struct {
	EndpointID   string
	URL          string
	Args         []string
	ConfirmToken string
	Actor        string
}

// Update re-validates the new target (R1-R7 + R8 with self excluded).
func (s *Service) Update(ctx context.Context, in UpdateInput) (InstallResult, ValidationResult, error) {
	if s == nil || s.uow == nil {
		return InstallResult{}, ValidationResult{}, ErrMcNotFound
	}
	now := s.clock.Now().UTC()
	if !s.limiter.allow(now) {
		return InstallResult{}, ValidationResult{}, ErrMcRateLimited
	}
	var ep m7flow.McpEndpointConfig
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		e, err := tx.GetMcpEndpoint(in.EndpointID)
		ep = e
		return err
	})
	if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: %s", ErrMcNotFound, in.EndpointID)
	}
	if err != nil {
		return InstallResult{}, ValidationResult{}, err
	}
	if ep.State == m7flow.McpStateRevoked {
		return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: revoked", ErrMcNotFound)
	}
	if ep.Transport == m7flow.McpTransportStdio && len(in.Args) == 0 {
		return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: stdio update needs args", ErrMcSchema)
	}
	if ep.Transport == m7flow.McpTransportHTTPS && in.URL == "" {
		return InstallResult{}, ValidationResult{}, fmt.Errorf("%w: https update needs url", ErrMcSchema)
	}
	cfg := ConfigInput{Transport: ep.Transport, Command: ep.Command, Args: in.Args, URL: in.URL}
	if ep.Transport == m7flow.McpTransportStdio {
		cfg.URL = ""
	} else {
		cfg.Command = ""
		cfg.Args = nil
	}
	res := s.validateRules(ctx, cfg)
	quota := s.checkQuota(ctx, cfg, in.EndpointID)
	res.Checks = append(res.Checks, quota)
	res.Valid = res.Valid && quota.Passed
	if !res.Valid {
		return InstallResult{}, res, ErrMcSchema
	}
	argsJSON, _ := json.Marshal(in.Args)
	targetURL, targetArgs := "", ""
	if ep.Transport == m7flow.McpTransportHTTPS {
		targetURL = in.URL
	} else {
		targetArgs = string(argsJSON)
	}
	ts := now.Format(time.RFC3339)
	err = s.uow.TransactMc(ctx, func(tx Tx) error {
		if err := s.consumeConfirm(tx, ConfirmMethodUpdate, in.EndpointID, in.ConfirmToken, now); err != nil {
			return err
		}
		if err := tx.UpdateMcpEndpointTarget(in.EndpointID, targetURL, targetArgs); err != nil {
			return err
		}
		if err := tx.UpsertEndpointUsage(in.EndpointID, UsageDelta{Updates: 1}, now); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "mc.connector.updated",
			ResourceType: "mcp_endpoint", ResourceID: in.EndpointID,
			Actor: actorOr(in.Actor),
			BeforeDigest: fingerprintDigest(ep.Transport, ep.Command, ep.URL),
			AfterDigest:  fingerprintDigest(ep.Transport, ep.Command, pickNonEmpty(targetURL, ep.URL)),
			CreatedAt:    ts,
		})
		return err
	})
	if err != nil {
		return InstallResult{}, res, err
	}
	// re-probe: drive to ready/degraded from the current state
	s.reprobe(ctx, in.EndpointID, ep.State)
	updated, gerr := s.getEndpoint(ctx, in.EndpointID)
	if gerr != nil {
		return InstallResult{EndpointID: in.EndpointID, State: ep.State}, res, nil
	}
	return InstallResult{EndpointID: in.EndpointID, State: updated.State, CapabilityDigest: updated.CapabilityDigest}, res, nil
}

func pickNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// reprobe runs one probe and drives the state machine like m7 Health.
func (s *Service) reprobe(ctx context.Context, endpointID, fromState string) {
	ep, err := s.getEndpoint(ctx, endpointID)
	if err != nil {
		return
	}
	digest, perr := s.prober.Probe(ctx, ep)
	now := s.clock.Now().UTC()
	if perr != nil {
		_ = s.uow.TransactMc(ctx, func(tx Tx) error {
			return tx.UpdateMcpEndpointState(endpointID, ep.State, m7flow.McpStateDegraded, nil, now)
		})
		return
	}
	pin := ep.PinnedDigest
	if pin == "" {
		pin = digest
	}
	_ = s.uow.TransactMc(ctx, func(tx Tx) error {
		return tx.UpdateMcpEndpointState(endpointID, ep.State, m7flow.McpStateReady, &pin, now)
	})
}

func (s *Service) getEndpoint(ctx context.Context, endpointID string) (m7flow.McpEndpointConfig, error) {
	var ep m7flow.McpEndpointConfig
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		e, err := tx.GetMcpEndpoint(endpointID)
		ep = e
		return err
	})
	return ep, err
}

// ── mc.connector.usage ──────────────────────────────────────────────────────

// Usage answers aggregated lifecycle statistics for one endpoint or all.
func (s *Service) Usage(ctx context.Context, endpointID string) ([]EndpointUsage, error) {
	if s == nil || s.uow == nil {
		return nil, ErrMcNotFound
	}
	if endpointID != "" {
		u, err := func() (EndpointUsage, error) {
			var u EndpointUsage
			err := s.uow.TransactMc(ctx, func(tx Tx) error {
				row, err := tx.GetEndpointUsage(endpointID)
				u = row
				return err
			})
			return u, err
		}()
		if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrMcNotFound, endpointID)
		}
		if err != nil {
			return nil, err
		}
		return []EndpointUsage{u}, nil
	}
	var out []EndpointUsage
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		list, err := tx.ListEndpointUsage()
		out = list
		return err
	})
	return out, err
}

// ── mc.tombstone.check ──────────────────────────────────────────────────────

// TombstoneReport is the outcome of the catalog revocation sweep.
type TombstoneReport struct {
	Fresh   bool             `json:"fresh"`
	Revoked []RevokedSuspect `json:"revoked"`
	Drifted []DriftedItem    `json:"drifted"`
}

// RevokedSuspect links a cached-but-delisted catalog item to installed
// endpoints matching its install fingerprint.
type RevokedSuspect struct {
	MarketItemID string `json:"marketItemId"`
	Name         string `json:"name"`
	EndpointIDs  []string `json:"endpointIds"`
}

// DriftedItem is a catalog item whose digest changed upstream.
type DriftedItem struct {
	MarketItemID   string `json:"marketItemId"`
	Name           string `json:"name"`
	CachedDigest   string `json:"cachedDigest"`
	RegistryDigest string `json:"registryDigest"`
}

// TombstoneCheck compares the live registry against the insert-only cache:
// items missing upstream are revocation suspects; digest changes are
// drift. Suspects are matched to installed market endpoints by
// fingerprint so the UI can offer uninstall.
func (s *Service) TombstoneCheck(ctx context.Context) (TombstoneReport, error) {
	if s == nil || s.uow == nil {
		return TombstoneReport{}, ErrMcNotFound
	}
	report := TombstoneReport{Fresh: true, Revoked: []RevokedSuspect{}, Drifted: []DriftedItem{}}
	regItems, err := s.registry(ctx)
	if err != nil {
		report.Fresh = false
		return report, nil
	}
	regByID := make(map[string]m7flow.McpMarketItem, len(regItems))
	for _, it := range regItems {
		regByID[it.ID] = it
	}
	var cached []m7flow.McpMarketItem
	var endpoints []m7flow.McpEndpointConfig
	uerr := s.uow.TransactMc(ctx, func(tx Tx) error {
		list, err := tx.ListMcMarket("", "", "", 1000)
		cached = list
		if err != nil {
			return err
		}
		endpoints, err = tx.ListMcpEndpoints("")
		return err
	})
	if uerr != nil {
		return report, uerr
	}
	for _, it := range cached {
		reg, ok := regByID[it.ID]
		if !ok {
			suspect := RevokedSuspect{MarketItemID: it.ID, Name: it.Name, EndpointIDs: []string{}}
			cfg, perr := parseInstallConfig(it.InstallConfigJSON)
			if perr == nil {
				transport := cfg.Transport
				if transport == "" {
					if cfg.Command != "" {
						transport = m7flow.McpTransportStdio
					} else {
						transport = m7flow.McpTransportHTTPS
					}
				}
				for _, ep := range endpoints {
					if ep.Origin != m7flow.McpOriginMarket {
						continue
					}
					if ep.Transport != transport {
						continue
					}
					if (transport == m7flow.McpTransportStdio && ep.Command == cfg.Command) ||
						(transport == m7flow.McpTransportHTTPS && ep.URL == cfg.URL) {
						suspect.EndpointIDs = append(suspect.EndpointIDs, ep.EndpointID)
					}
				}
			}
			report.Revoked = append(report.Revoked, suspect)
			continue
		}
		if reg.CatalogDigest != it.CatalogDigest {
			report.Drifted = append(report.Drifted, DriftedItem{
				MarketItemID: it.ID, Name: it.Name,
				CachedDigest: it.CatalogDigest, RegistryDigest: reg.CatalogDigest,
			})
		}
	}
	return report, nil
}
