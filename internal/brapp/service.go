// Package brapp implements the M10 wave-3 browser multi-mode service:
// the 5-mode connection settings (builtin/chrome/edge/extension/ask),
// the CDP connection state machine
// (disconnected→connecting→connected/error), navigation under the URL
// policy (allowlist + private-network blocking + port allow-set),
// per-session data usage snapshots with retention clears, and the
// ask/allow/deny permission approval queue. Audit coverage uses the six
// frozen wave-3 browser actions from migration 0076.
package brapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// Service-level errors mapped by the Bridge handlers onto M10-BR codes.
var (
	// ErrBrSchema: settings/payload failed validation (M10-BR-001).
	ErrBrSchema = errors.New("brapp: browser payload invalid")
	// ErrBrState: illegal CDP state transition or decided permission (M10-BR-002).
	ErrBrState = errors.New("brapp: browser session state invalid")
	// ErrBrNotFound: session or permission missing (M10-BR-003).
	ErrBrNotFound = errors.New("brapp: browser resource not found")
	// ErrBrURLPolicy: navigation rejected by allowlist/私网/端口 policy (M10-BR-004).
	ErrBrURLPolicy = errors.New("brapp: navigation url rejected")
	// ErrBrMode: requested mode unavailable (path missing, port closed, ask pending) (M10-BR-005).
	ErrBrMode = errors.New("brapp: browser mode unavailable")
	// ErrBrRateLimited: lifecycle >11/min or navigate >30/min (M10-BR-006).
	ErrBrRateLimited = errors.New("brapp: browser operation rate limited")
)

// Frozen enum values (wire contract).
const (
	ModeBuiltin   = "builtin"
	ModeChrome    = "chrome"
	ModeEdge      = "edge"
	ModeExtension = "extension"
	ModeAsk       = "ask"

	StateDisconnected = "disconnected"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateError        = "error"

	PermGeolocation   = "geolocation"
	PermCamera        = "camera"
	PermMicrophone    = "microphone"
	PermNotifications = "notifications"
	PermClipboardRead = "clipboard-read"
	PermDownloads     = "downloads"

	PolicyAsk   = "ask"
	PolicyAllow = "allow"
	PolicyDeny  = "deny"

	PermStatePending = "pending"
	PermStateGranted = "granted"
	PermStateDenied  = "denied"
)

// Frozen caps.
const (
	// BrLifecycleRatePerMinute bounds connect/disconnect/clear (design).
	BrLifecycleRatePerMinute = 11
	// BrNavigateRatePerMinute bounds br.navigate.
	BrNavigateRatePerMinute = 30
	// BrMaxAllowlistEntries / BrMaxAllowlistEntryLen bound the allowlist.
	BrMaxAllowlistEntries  = 256
	BrMaxAllowlistEntryLen = 256
	// BrMaxPathLen bounds the executable path fields.
	BrMaxPathLen = 1024
	// BrConnectTimeout bounds the CDP dial/launch handshake.
	BrConnectTimeout = 8 * time.Second
)

// Settings is the single-row browser configuration.
type Settings struct {
	Mode                string   `json:"mode"`
	ChromePath          string   `json:"chromePath"`
	EdgePath            string   `json:"edgePath"`
	ExtensionPort       int      `json:"extensionPort"`
	Allowlist           []string `json:"allowlist"`
	DataRetentionDays   int      `json:"dataRetentionDays"`
	BlockPrivateNetwork bool     `json:"blockPrivateNetworks"`
	UpdatedAt           string   `json:"updatedAt"`
}

// Session is one CDP connection tracked by the state machine.
type Session struct {
	SessionID   string `json:"sessionId"`
	Mode        string `json:"mode"`
	State       string `json:"state"`
	WsURL       string `json:"wsUrl,omitempty"`
	Detail      string `json:"detail,omitempty"`
	ConnectedAt string `json:"connectedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt"`
}

// DataUsage is one per-session storage snapshot.
type DataUsage struct {
	SessionID    string `json:"sessionId"`
	ProfileBytes int64  `json:"profileBytes"`
	CacheBytes   int64  `json:"cacheBytes"`
	CookiesBytes int64  `json:"cookiesBytes"`
	ComputedAt   string `json:"computedAt"`
	UpdatedAt    string `json:"updatedAt"`
}

// Permission is one approval-queue row.
type Permission struct {
	PermissionID string `json:"permissionId"`
	Origin       string `json:"origin"`
	Permission   string `json:"permission"`
	Policy       string `json:"policy"`
	State        string `json:"state"`
	SessionID    string `json:"sessionId,omitempty"`
	CreatedAt    string `json:"createdAt"`
	DecidedAt    string `json:"decidedAt,omitempty"`
}

// Tx is the wave-3 browser transaction (satisfied by the shared
// agent-runtime tx).
type Tx interface {
	GetBrSettings() (Settings, error)
	PutBrSettings(Settings) error
	GetBrSession(id string) (Session, error)
	PutBrSession(Session) error
	ListBrSessions() ([]Session, error)
	PutBrDataUsage(DataUsage) error
	ListBrDataUsage() ([]DataUsage, error)
	DeleteBrDataUsage(sessionID string) error
	PutBrPermission(Permission) error
	GetBrPermission(id string) (Permission, error)
	FindBrPermission(origin, permission string) (Permission, error)
	ListBrPermissions(state string) ([]Permission, error)
	UpdateBrPermissionState(id, from, to, decidedAt string) error
	ApplyBrPermissionPolicy(id, policy, state, decidedAt string) error
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// UnitOfWork is the wave-3 single-writer boundary.
type UnitOfWork interface {
	TransactBr(ctx context.Context, fn func(Tx) error) error
}

// PathProbe is one executable availability report.
type PathProbe struct {
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
}

// PortProbe is the extension-bridge port report.
type PortProbe struct {
	Available bool `json:"available"`
	Port      int  `json:"port"`
}

// DetectReport answers br.mode.detect.
type DetectReport struct {
	Builtin   bool      `json:"builtin"`
	Chrome    PathProbe `json:"chrome"`
	Edge      PathProbe `json:"edge"`
	Extension PortProbe `json:"extension"`
}

// Host abstracts the browser runtime surface (detection, CDP connect,
// navigation, storage snapshot/clear). The LocalHost default probes the
// filesystem, launches chrome/edge with a debugging port and talks the
// DevTools HTTP endpoints.
type Host interface {
	Detect(ctx context.Context, s Settings) (DetectReport, error)
	Connect(ctx context.Context, sessionID, mode string, s Settings) (string, error)
	Disconnect(ctx context.Context, sessionID, mode string) error
	Navigate(ctx context.Context, sess Session, rawURL string) error
	SnapshotUsage(ctx context.Context, mode string) (profile, cache, cookies int64)
	ClearData(ctx context.Context, mode string, olderThan time.Time) (int64, error)
}

// rateWindow is the shared sliding-window limiter shape.
type rateWindow struct {
	mu     sync.Mutex
	stamps []time.Time
}

func (w *rateWindow) allow(now time.Time, cap int) bool {
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
	if len(w.stamps) >= cap {
		return false
	}
	w.stamps = append(w.stamps, now)
	return true
}

// HostResolver answers the DNS lookup used by the private-network
// navigation gate; tests substitute this to avoid real network calls.
type HostResolver func(ctx context.Context, host string) ([]net.IPAddr, error)

// defaultResolver performs the real DNS lookup.
func defaultResolver(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// Service implements the br.* surface.
type Service struct {
	uow       UnitOfWork
	clock     m7app.Clock
	host      Host
	resolve   HostResolver
	lifeLimit rateWindow
	navLimit  rateWindow
}

// New returns a Service over the given unit of work with the LocalHost.
func New(uow UnitOfWork, profileRoot string) *Service {
	return &Service{uow: uow, clock: systemClock{}, host: NewLocalHost(profileRoot), resolve: defaultResolver}
}

// SetClock substitutes the clock (tests).
func (s *Service) SetClock(c m7app.Clock) { s.clock = c }

// SetHost substitutes the browser host (tests).
func (s *Service) SetHost(h Host) { s.host = h }

// SetResolver substitutes the DNS resolver used by the navigation
// private-network gate (tests).
func (s *Service) SetResolver(r HostResolver) { s.resolve = r }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func actorOr(actor string) string {
	if actor == "" {
		return "renderer"
	}
	return actor
}

func isNotFound(err error) bool {
	return errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}

func clampDetail(detail string) string {
	if len(detail) > 512 {
		return detail[:512]
	}
	return detail
}

// ── settings ────────────────────────────────────────────────────────────────

// ValidateSettings checks one settings document (shared by update and
// the handler pre-check).
func ValidateSettings(s Settings) error {
	switch s.Mode {
	case ModeBuiltin, ModeChrome, ModeEdge, ModeExtension, ModeAsk:
	default:
		return fmt.Errorf("%w: mode %q", ErrBrSchema, s.Mode)
	}
	if len(s.ChromePath) > BrMaxPathLen || len(s.EdgePath) > BrMaxPathLen {
		return fmt.Errorf("%w: path length", ErrBrSchema)
	}
	if s.ExtensionPort < 1024 || s.ExtensionPort > 65535 {
		return fmt.Errorf("%w: extensionPort %d", ErrBrSchema, s.ExtensionPort)
	}
	if s.DataRetentionDays < 0 || s.DataRetentionDays > 365 {
		return fmt.Errorf("%w: dataRetentionDays %d", ErrBrSchema, s.DataRetentionDays)
	}
	if len(s.Allowlist) > BrMaxAllowlistEntries {
		return fmt.Errorf("%w: allowlist entries %d", ErrBrSchema, len(s.Allowlist))
	}
	for _, entry := range s.Allowlist {
		if len(entry) < 1 || len(entry) > BrMaxAllowlistEntryLen {
			return fmt.Errorf("%w: allowlist entry length", ErrBrSchema)
		}
		u, err := url.Parse(entry)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%w: allowlist entry %q must be http(s)://host[:port]", ErrBrSchema, entry)
		}
	}
	return nil
}

// GetSettings answers the singleton (seeded on first read).
func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	if s == nil || s.uow == nil {
		return Settings{}, ErrBrNotFound
	}
	var out Settings
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		row, err := tx.GetBrSettings()
		out = row
		return err
	})
	return out, err
}

// SettingsPatch is the optional-field update command.
type SettingsPatch struct {
	Mode                *string
	ChromePath          *string
	EdgePath            *string
	ExtensionPort       *int
	Allowlist           *[]string
	DataRetentionDays   *int
	BlockPrivateNetwork *bool
	Actor               string
}

// UpdateSettings applies one patch; a mode change force-disconnects all
// live sessions (audited browser.disconnected).
func (s *Service) UpdateSettings(ctx context.Context, patch SettingsPatch) (Settings, error) {
	if s == nil || s.uow == nil {
		return Settings{}, ErrBrNotFound
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var out Settings
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		cur, err := tx.GetBrSettings()
		if err != nil {
			return err
		}
		next := cur
		if patch.Mode != nil {
			next.Mode = *patch.Mode
		}
		if patch.ChromePath != nil {
			next.ChromePath = *patch.ChromePath
		}
		if patch.EdgePath != nil {
			next.EdgePath = *patch.EdgePath
		}
		if patch.ExtensionPort != nil {
			next.ExtensionPort = *patch.ExtensionPort
		}
		if patch.Allowlist != nil {
			list := make([]string, len(*patch.Allowlist))
			copy(list, *patch.Allowlist)
			next.Allowlist = list
		}
		if patch.DataRetentionDays != nil {
			next.DataRetentionDays = *patch.DataRetentionDays
		}
		if patch.BlockPrivateNetwork != nil {
			next.BlockPrivateNetwork = *patch.BlockPrivateNetwork
		}
		if err := ValidateSettings(next); err != nil {
			return err
		}
		next.UpdatedAt = ts
		if err := tx.PutBrSettings(next); err != nil {
			return err
		}
		out = next
		// mode switch invalidates live CDP sessions
		if patch.Mode != nil && *patch.Mode != cur.Mode {
			live, err := tx.ListBrSessions()
			if err != nil {
				return err
			}
			for _, sess := range live {
				if sess.State == StateDisconnected {
					continue
				}
				_ = s.host.Disconnect(ctx, sess.SessionID, sess.Mode)
				sess.State = StateDisconnected
				sess.WsURL = ""
				sess.Detail = ""
				sess.ConnectedAt = ""
				sess.UpdatedAt = ts
				if err := tx.PutBrSession(sess); err != nil {
					return err
				}
				if _, err := tx.AppendAuditEvent(audit.Event{
					ID: ulid.Make().String(), Action: "browser.disconnected",
					ResourceType: "br_session", ResourceID: sess.SessionID,
					Actor: actorOr(patch.Actor), CorrelationID: "mode-switch",
					CreatedAt: ts,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return out, nil
}

// ── mode detection ──────────────────────────────────────────────────────────

// DetectModes probes the local browser landscape.
func (s *Service) DetectModes(ctx context.Context) (DetectReport, error) {
	if s == nil || s.uow == nil {
		return DetectReport{}, ErrBrNotFound
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return DetectReport{}, err
	}
	return s.host.Detect(ctx, settings)
}

// ── session lifecycle ───────────────────────────────────────────────────────

// Connect drives disconnected|error → connecting → connected|error.
// An already-connected id is idempotent; connecting is a busy rejection.
func (s *Service) Connect(ctx context.Context, sessionID, mode, actor string) (Session, error) {
	if s == nil || s.uow == nil {
		return Session{}, ErrBrNotFound
	}
	if sessionID != "" && (len(sessionID) < 1 || len(sessionID) > 64) {
		return Session{}, fmt.Errorf("%w: sessionId length", ErrBrSchema)
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return Session{}, err
	}
	if mode == "" {
		mode = settings.Mode
	}
	switch mode {
	case ModeBuiltin, ModeChrome, ModeEdge, ModeExtension:
	case ModeAsk:
		return Session{}, fmt.Errorf("%w: ask 模式需先在设置中选择浏览器", ErrBrMode)
	default:
		return Session{}, fmt.Errorf("%w: mode %q", ErrBrSchema, mode)
	}
	now := s.clock.Now().UTC()
	if !s.lifeLimit.allow(now, BrLifecycleRatePerMinute) {
		return Session{}, ErrBrRateLimited
	}
	if sessionID == "" {
		sessionID = "br-" + ulid.Make().String()
	}
	ts := now.Format(time.RFC3339)

	// idempotency gate
	var existing Session
	err = s.uow.TransactBr(ctx, func(tx Tx) error {
		row, err := tx.GetBrSession(sessionID)
		existing = row
		return err
	})
	if err == nil {
		switch existing.State {
		case StateConnected:
			return existing, nil
		case StateConnecting:
			return Session{}, fmt.Errorf("%w: session connecting", ErrBrState)
		}
	} else if !isNotFound(err) {
		return Session{}, err
	}

	// phase 1: connecting
	if err := s.uow.TransactBr(ctx, func(tx Tx) error {
		return tx.PutBrSession(Session{
			SessionID: sessionID, Mode: mode, State: StateConnecting, UpdatedAt: ts,
		})
	}); err != nil {
		return Session{}, err
	}

	// phase 2: host handshake outside the write tx
	wsURL, cerr := s.host.Connect(ctx, sessionID, mode, settings)
	if cerr != nil {
		detail := clampDetail(cerr.Error())
		_ = s.uow.TransactBr(ctx, func(tx Tx) error {
			return tx.PutBrSession(Session{
				SessionID: sessionID, Mode: mode, State: StateError, Detail: detail, UpdatedAt: ts,
			})
		})
		if errors.Is(cerr, ErrBrMode) || errors.Is(cerr, ErrBrSchema) {
			return Session{}, cerr
		}
		return Session{}, fmt.Errorf("%w: %s", ErrBrMode, detail)
	}

	var out Session
	aerr := s.uow.TransactBr(ctx, func(tx Tx) error {
		out = Session{
			SessionID: sessionID, Mode: mode, State: StateConnected,
			WsURL: wsURL, ConnectedAt: ts, UpdatedAt: ts,
		}
		if err := tx.PutBrSession(out); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "browser.connected",
			ResourceType: "br_session", ResourceID: sessionID,
			Actor: actorOr(actor), CreatedAt: ts,
		})
		return err
	})
	if aerr != nil {
		return Session{}, aerr
	}
	return out, nil
}

// ListSessions answers all tracked sessions (newest first).
func (s *Service) ListSessions(ctx context.Context) ([]Session, error) {
	if s == nil || s.uow == nil {
		return nil, ErrBrNotFound
	}
	var out []Session
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		list, err := tx.ListBrSessions()
		out = list
		return err
	})
	return out, err
}

// Disconnect drives any live state → disconnected (idempotent) and
// audits browser.disconnected.
func (s *Service) Disconnect(ctx context.Context, sessionID, actor string) (Session, error) {
	if s == nil || s.uow == nil {
		return Session{}, ErrBrNotFound
	}
	if len(sessionID) < 1 || len(sessionID) > 64 {
		return Session{}, fmt.Errorf("%w: sessionId length", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	if !s.lifeLimit.allow(now, BrLifecycleRatePerMinute) {
		return Session{}, ErrBrRateLimited
	}
	ts := now.Format(time.RFC3339)
	var out Session
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		sess, err := tx.GetBrSession(sessionID)
		if isNotFound(err) {
			return fmt.Errorf("%w: %s", ErrBrNotFound, sessionID)
		}
		if err != nil {
			return err
		}
		if sess.State == StateDisconnected {
			out = sess
			return nil
		}
		_ = s.host.Disconnect(ctx, sessionID, sess.Mode)
		sess.State = StateDisconnected
		sess.WsURL = ""
		sess.Detail = ""
		sess.ConnectedAt = ""
		sess.UpdatedAt = ts
		if err := tx.PutBrSession(sess); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "browser.disconnected",
			ResourceType: "br_session", ResourceID: sessionID,
			Actor: actorOr(actor), CreatedAt: ts,
		}); err != nil {
			return err
		}
		out = sess
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	return out, nil
}

// ── navigation ──────────────────────────────────────────────────────────────

// NavigateResult answers one navigation.
type NavigateResult struct {
	SessionID   string `json:"sessionId"`
	URL         string `json:"url"`
	NavigatedAt string `json:"navigatedAt"`
}

// Navigate sends one URL to a connected session after the URL policy
// gate (allowlist prefix match, port allow-set, private-network block).
func (s *Service) Navigate(ctx context.Context, sessionID, rawURL, actor string) (NavigateResult, error) {
	if s == nil || s.uow == nil {
		return NavigateResult{}, ErrBrNotFound
	}
	if len(sessionID) < 1 || len(sessionID) > 64 {
		return NavigateResult{}, fmt.Errorf("%w: sessionId length", ErrBrSchema)
	}
	if len(rawURL) < 1 || len(rawURL) > 2048 {
		return NavigateResult{}, fmt.Errorf("%w: url length", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	if !s.navLimit.allow(now, BrNavigateRatePerMinute) {
		return NavigateResult{}, ErrBrRateLimited
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return NavigateResult{}, err
	}
	if reason := s.checkNavigateURL(ctx, rawURL, settings); reason != "" {
		return NavigateResult{}, fmt.Errorf("%w: %s", ErrBrURLPolicy, reason)
	}
	var sess Session
	err = s.uow.TransactBr(ctx, func(tx Tx) error {
		row, err := tx.GetBrSession(sessionID)
		sess = row
		return err
	})
	if isNotFound(err) {
		return NavigateResult{}, fmt.Errorf("%w: %s", ErrBrNotFound, sessionID)
	}
	if err != nil {
		return NavigateResult{}, err
	}
	if sess.State != StateConnected {
		return NavigateResult{}, fmt.Errorf("%w: session state %s", ErrBrState, sess.State)
	}
	if nerr := s.host.Navigate(ctx, sess, rawURL); nerr != nil {
		return NavigateResult{}, nerr
	}
	ts := now.Format(time.RFC3339)
	_ = s.uow.TransactBr(ctx, func(tx Tx) error {
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "browser.navigated",
			ResourceType: "br_session", ResourceID: sessionID,
			Actor: actorOr(actor), CorrelationID: rawURL, CreatedAt: ts,
		})
		return err
	})
	return NavigateResult{SessionID: sessionID, URL: rawURL, NavigatedAt: ts}, nil
}

// checkNavigateURL applies the URL policy with the service resolver.
func (s *Service) checkNavigateURL(ctx context.Context, rawURL string, settings Settings) string {
	resolve := s.resolve
	if resolve == nil {
		resolve = defaultResolver
	}
	return checkNavigateURL(ctx, rawURL, settings, resolve)
}

// CheckNavigateURL enforces the navigation URL policy; empty string
// means accepted, otherwise the rejection reason.
func CheckNavigateURL(rawURL string, s Settings) string {
	return checkNavigateURL(context.Background(), rawURL, s, defaultResolver)
}

// checkNavigateURL is the resolver-injectable core.
func checkNavigateURL(ctx context.Context, rawURL string, s Settings, resolve HostResolver) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "url unparseable"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "scheme must be http/https"
	}
	host := u.Hostname()
	if host == "" {
		return "url host missing"
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
			return "port " + port + " not allowed"
		}
	}
	if len(s.Allowlist) > 0 {
		origin := u.Scheme + "://" + u.Host
		matched := false
		for _, entry := range s.Allowlist {
			e := strings.TrimSuffix(entry, "/")
			if origin == e || strings.HasPrefix(origin, e+"/") || strings.HasPrefix(origin+"/", e) {
				matched = true
				break
			}
		}
		if !matched {
			return "origin not in navigation allowlist"
		}
	}
	if s.BlockPrivateNetwork {
		// localhost is always loopback regardless of resolver answers.
		if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
			return "address " + host + " is a forbidden network target"
		}
		if ip := net.ParseIP(host); ip != nil {
			if m7flow.ToolSSRFReject(ip.String()) {
				return "address " + host + " is a forbidden network target"
			}
			return ""
		}
		addrs, rerr := resolve(ctx, host)
		if rerr != nil || len(addrs) == 0 {
			return "host " + host + " does not resolve"
		}
		for _, a := range addrs {
			if m7flow.ToolSSRFReject(a.IP.String()) {
				return "host " + host + " resolves to forbidden address " + a.IP.String()
			}
		}
	}
	return ""
}

// ── data management ─────────────────────────────────────────────────────────

// DataUsage snapshots (one or all) sessions and persists the result.
func (s *Service) DataUsage(ctx context.Context, sessionID string) ([]DataUsage, error) {
	if s == nil || s.uow == nil {
		return nil, ErrBrNotFound
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var targets []Session
	if sessionID != "" {
		var sess Session
		err := s.uow.TransactBr(ctx, func(tx Tx) error {
			row, err := tx.GetBrSession(sessionID)
			sess = row
			return err
		})
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrBrNotFound, sessionID)
		}
		if err != nil {
			return nil, err
		}
		targets = append(targets, sess)
	} else {
		err := s.uow.TransactBr(ctx, func(tx Tx) error {
			list, err := tx.ListBrSessions()
			targets = list
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]DataUsage, 0, len(targets))
	for _, sess := range targets {
		profile, cache, cookies := s.host.SnapshotUsage(ctx, sess.Mode)
		row := DataUsage{
			SessionID: sess.SessionID, ProfileBytes: profile, CacheBytes: cache,
			CookiesBytes: cookies, ComputedAt: ts, UpdatedAt: ts,
		}
		err := s.uow.TransactBr(ctx, func(tx Tx) error {
			return tx.PutBrDataUsage(row)
		})
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// ClearDataResult answers one retention sweep.
type ClearDataResult struct {
	ClearedSessions []string `json:"clearedSessions"`
	FreedBytes      int64    `json:"freedBytes"`
}

// ClearData removes cached data older than the retention window for
// one or all sessions and audits browser.data.cleared.
func (s *Service) ClearData(ctx context.Context, sessionID, actor string) (ClearDataResult, error) {
	if s == nil || s.uow == nil {
		return ClearDataResult{}, ErrBrNotFound
	}
	now := s.clock.Now().UTC()
	if !s.lifeLimit.allow(now, BrLifecycleRatePerMinute) {
		return ClearDataResult{}, ErrBrRateLimited
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return ClearDataResult{}, err
	}
	cutoff := now.AddDate(0, 0, -settings.DataRetentionDays)
	var targets []Session
	if sessionID != "" {
		var sess Session
		err := s.uow.TransactBr(ctx, func(tx Tx) error {
			row, err := tx.GetBrSession(sessionID)
			sess = row
			return err
		})
		if isNotFound(err) {
			return ClearDataResult{}, fmt.Errorf("%w: %s", ErrBrNotFound, sessionID)
		}
		if err != nil {
			return ClearDataResult{}, err
		}
		targets = append(targets, sess)
	} else {
		err := s.uow.TransactBr(ctx, func(tx Tx) error {
			list, err := tx.ListBrSessions()
			targets = list
			return err
		})
		if err != nil {
			return ClearDataResult{}, err
		}
	}
	result := ClearDataResult{ClearedSessions: []string{}}
	for _, sess := range targets {
		freed, cerr := s.host.ClearData(ctx, sess.Mode, cutoff)
		if cerr != nil {
			return ClearDataResult{}, cerr
		}
		result.FreedBytes += freed
		result.ClearedSessions = append(result.ClearedSessions, sess.SessionID)
		_ = s.uow.TransactBr(ctx, func(tx Tx) error {
			return tx.DeleteBrDataUsage(sess.SessionID)
		})
	}
	ts := now.Format(time.RFC3339)
	target := sessionID
	if target == "" {
		target = "all"
	}
	_ = s.uow.TransactBr(ctx, func(tx Tx) error {
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "browser.data.cleared",
			ResourceType: "br_session", ResourceID: target,
			Actor: actorOr(actor), AfterDigest: strconv.FormatInt(result.FreedBytes, 10),
			CreatedAt: ts,
		})
		return err
	})
	return result, nil
}

// ── permission approval ─────────────────────────────────────────────────────

// RequestPermission enqueues one ask; allow/deny policies resolve
// immediately at creation.
func (s *Service) RequestPermission(ctx context.Context, origin, permission, sessionID, actor string) (Permission, error) {
	if s == nil || s.uow == nil {
		return Permission{}, ErrBrNotFound
	}
	switch permission {
	case PermGeolocation, PermCamera, PermMicrophone, PermNotifications, PermClipboardRead, PermDownloads:
	default:
		return Permission{}, fmt.Errorf("%w: permission %q", ErrBrSchema, permission)
	}
	if len(origin) < 1 || len(origin) > 512 {
		return Permission{}, fmt.Errorf("%w: origin length", ErrBrSchema)
	}
	if u, err := url.Parse(origin); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Permission{}, fmt.Errorf("%w: origin must be http(s)://host[:port]", ErrBrSchema)
	}
	if sessionID != "" && len(sessionID) > 64 {
		return Permission{}, fmt.Errorf("%w: sessionId length", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var out Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		policy := PolicyAsk
		if latest, err := tx.FindBrPermission(origin, permission); err == nil {
			policy = latest.Policy
		} else if !isNotFound(err) {
			return err
		}
		row := Permission{
			PermissionID: "brp-" + ulid.Make().String(),
			Origin:       origin, Permission: permission, Policy: policy,
			State: PermStatePending, SessionID: sessionID, CreatedAt: ts,
		}
		switch policy {
		case PolicyAllow:
			row.State = PermStateGranted
			row.DecidedAt = ts
		case PolicyDeny:
			row.State = PermStateDenied
			row.DecidedAt = ts
		}
		if err := tx.PutBrPermission(row); err != nil {
			return err
		}
		out = row
		if row.State != PermStatePending {
			action := "browser.permission.denied"
			if row.State == PermStateGranted {
				action = "browser.permission.granted"
			}
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: action,
				ResourceType: "br_permission", ResourceID: row.PermissionID,
				Actor: actorOr(actor), CorrelationID: "policy:" + policy, CreatedAt: ts,
			})
			return err
		}
		return nil
	})
	if err != nil {
		return Permission{}, err
	}
	return out, nil
}

// ListPermissions answers the queue (state filter optional).
func (s *Service) ListPermissions(ctx context.Context, state string) ([]Permission, error) {
	if s == nil || s.uow == nil {
		return nil, ErrBrNotFound
	}
	if state != "" && state != PermStatePending && state != PermStateGranted && state != PermStateDenied {
		return nil, fmt.Errorf("%w: state %q", ErrBrSchema, state)
	}
	var out []Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		list, err := tx.ListBrPermissions(state)
		out = list
		return err
	})
	return out, err
}

// DecidePermission resolves one pending ask (grant/deny) and audits
// browser.permission.granted / browser.permission.denied.
func (s *Service) DecidePermission(ctx context.Context, permissionID, decision, actor string) (Permission, error) {
	if s == nil || s.uow == nil {
		return Permission{}, ErrBrNotFound
	}
	if decision != "grant" && decision != "deny" {
		return Permission{}, fmt.Errorf("%w: decision %q", ErrBrSchema, decision)
	}
	if len(permissionID) < 1 || len(permissionID) > 64 {
		return Permission{}, fmt.Errorf("%w: permissionId length", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var out Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		row, err := tx.GetBrPermission(permissionID)
		if isNotFound(err) {
			return fmt.Errorf("%w: %s", ErrBrNotFound, permissionID)
		}
		if err != nil {
			return err
		}
		if row.State != PermStatePending {
			return fmt.Errorf("%w: permission already %s", ErrBrState, row.State)
		}
		next := PermStateDenied
		action := "browser.permission.denied"
		if decision == "grant" {
			next = PermStateGranted
			action = "browser.permission.granted"
		}
		if err := tx.UpdateBrPermissionState(permissionID, PermStatePending, next, ts); err != nil {
			return err
		}
		row.State = next
		row.DecidedAt = ts
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: action,
			ResourceType: "br_permission", ResourceID: permissionID,
			Actor: actorOr(actor), CreatedAt: ts,
		}); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return Permission{}, err
	}
	return out, nil
}

// SetPermissionPolicy upserts the ask/allow/deny policy for one
// origin+permission pair; a pending row resolves immediately when the
// new policy is not ask.
func (s *Service) SetPermissionPolicy(ctx context.Context, origin, permission, policy, actor string) (Permission, error) {
	if s == nil || s.uow == nil {
		return Permission{}, ErrBrNotFound
	}
	switch policy {
	case PolicyAsk, PolicyAllow, PolicyDeny:
	default:
		return Permission{}, fmt.Errorf("%w: policy %q", ErrBrSchema, policy)
	}
	switch permission {
	case PermGeolocation, PermCamera, PermMicrophone, PermNotifications, PermClipboardRead, PermDownloads:
	default:
		return Permission{}, fmt.Errorf("%w: permission %q", ErrBrSchema, permission)
	}
	if len(origin) < 1 || len(origin) > 512 {
		return Permission{}, fmt.Errorf("%w: origin length", ErrBrSchema)
	}
	if u, err := url.Parse(origin); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Permission{}, fmt.Errorf("%w: origin must be http(s)://host[:port]", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var out Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		latest, err := tx.FindBrPermission(origin, permission)
		if err == nil && latest.State == PermStatePending {
			state := PermStatePending
			decided := ""
			switch policy {
			case PolicyAllow:
				state = PermStateGranted
				decided = ts
			case PolicyDeny:
				state = PermStateDenied
				decided = ts
			}
			if err := tx.ApplyBrPermissionPolicy(latest.PermissionID, policy, state, decided); err != nil {
				return err
			}
			latest.Policy = policy
			latest.State = state
			latest.DecidedAt = decided
			out = latest
			return nil
		}
		if err != nil && !isNotFound(err) {
			return err
		}
		row := Permission{
			PermissionID: "brp-" + ulid.Make().String(),
			Origin:       origin, Permission: permission, Policy: policy,
			State: PermStatePending, CreatedAt: ts,
		}
		switch policy {
		case PolicyAllow:
			row.State = PermStateGranted
			row.DecidedAt = ts
		case PolicyDeny:
			row.State = PermStateDenied
			row.DecidedAt = ts
		}
		if err := tx.PutBrPermission(row); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return Permission{}, err
	}
	return out, nil
}

// ── local host ──────────────────────────────────────────────────────────────

// LocalHost probes the filesystem for chrome/edge, dials the extension
// bridge port, launches chrome/edge with a CDP debugging port under a
// dedicated profile directory, and drives navigation through the
// DevTools HTTP endpoints. Builtin mode represents the WebView2 host
// (always available, no CDP navigation channel).
type LocalHost struct {
	mu          sync.Mutex
	procs       map[string]*exec.Cmd
	profileRoot string
}

// NewLocalHost returns the default host rooted at profileRoot.
func NewLocalHost(profileRoot string) *LocalHost {
	return &LocalHost{procs: make(map[string]*exec.Cmd), profileRoot: profileRoot}
}

func chromeCandidates() []string {
	var out []string
	if p := osGetenv("ProgramFiles"); p != "" {
		out = append(out, filepath.Join(p, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if p := osGetenv("ProgramFiles(x86)"); p != "" {
		out = append(out, filepath.Join(p, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if p := osGetenv("LOCALAPPDATA"); p != "" {
		out = append(out, filepath.Join(p, "Google", "Chrome", "Application", "chrome.exe"))
	}
	return append(out, "/usr/bin/google-chrome", "/usr/bin/chromium-browser", "/usr/bin/chromium")
}

func edgeCandidates() []string {
	var out []string
	if p := osGetenv("ProgramFiles(x86)"); p != "" {
		out = append(out, filepath.Join(p, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	if p := osGetenv("ProgramFiles"); p != "" {
		out = append(out, filepath.Join(p, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	return append(out, "/usr/bin/microsoft-edge", "/usr/bin/microsoft-edge-stable")
}

// Detect reports local browser availability.
func (h *LocalHost) Detect(ctx context.Context, s Settings) (DetectReport, error) {
	report := DetectReport{Builtin: true, Extension: PortProbe{Port: s.ExtensionPort}}
	report.Chrome = probePath(s.ChromePath, chromeCandidates())
	report.Edge = probePath(s.EdgePath, edgeCandidates())
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.ExtensionPort)))
	if err == nil {
		_ = conn.Close()
		report.Extension.Available = true
	}
	return report, nil
}

func probePath(configured string, candidates []string) PathProbe {
	if configured != "" {
		if fileExists(configured) {
			return PathProbe{Available: true, Path: configured}
		}
		return PathProbe{Available: false, Path: configured}
	}
	for _, c := range candidates {
		if fileExists(c) {
			return PathProbe{Available: true, Path: c}
		}
	}
	return PathProbe{}
}

// Connect performs the mode handshake and answers the CDP ws url.
func (h *LocalHost) Connect(ctx context.Context, sessionID, mode string, s Settings) (string, error) {
	switch mode {
	case ModeBuiltin:
		return "", nil
	case ModeExtension:
		return cdpVersionWS(ctx, "127.0.0.1", s.ExtensionPort)
	case ModeChrome, ModeEdge:
		path := ""
		if mode == ModeChrome {
			path = firstExisting(s.ChromePath, chromeCandidates())
		} else {
			path = firstExisting(s.EdgePath, edgeCandidates())
		}
		if path == "" {
			return "", fmt.Errorf("%w: %s executable not found", ErrBrMode, mode)
		}
		port, err := freePort()
		if err != nil {
			return "", err
		}
		profileDir := filepath.Join(h.profileRoot, mode+"-profile")
		cmd := exec.Command(path,
			"--remote-debugging-port="+strconv.Itoa(port),
			"--user-data-dir="+profileDir,
			"--no-first-run", "--no-default-browser-check",
			"--headless=new", "about:blank")
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("%w: launch failed: %v", ErrBrMode, err)
		}
		h.mu.Lock()
		h.procs[sessionID] = cmd
		h.mu.Unlock()
		ws, werr := cdpPollVersionWS(ctx, "127.0.0.1", port, BrConnectTimeout)
		if werr != nil {
			h.killProc(sessionID)
			return "", fmt.Errorf("%w: CDP handshake failed: %v", ErrBrMode, werr)
		}
		return ws, nil
	case ModeAsk:
		return "", fmt.Errorf("%w: ask 模式等待用户选择", ErrBrMode)
	default:
		return "", fmt.Errorf("%w: mode %q", ErrBrSchema, mode)
	}
}

// Disconnect terminates a spawned browser process (chrome/edge).
func (h *LocalHost) Disconnect(_ context.Context, sessionID, mode string) error {
	if mode == ModeChrome || mode == ModeEdge {
		h.killProc(sessionID)
	}
	return nil
}

// Navigate opens one URL through the DevTools HTTP new-tab endpoint.
func (h *LocalHost) Navigate(ctx context.Context, sess Session, rawURL string) error {
	if sess.Mode == ModeBuiltin {
		return fmt.Errorf("%w: builtin 会话不支持 CDP 导航（使用 browser.act）", ErrBrMode)
	}
	host, port, err := wsHostPort(sess.WsURL)
	if err != nil {
		return fmt.Errorf("%w: session ws url missing", ErrBrMode)
	}
	endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/json/new?" + url.QueryEscape(rawURL)
	req, err := NewHTTPRequestContext(ctx, "PUT", endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBrMode, err)
	}
	resp, err := DefaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: navigate failed: %v", ErrBrMode, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: navigate status %d", ErrBrMode, resp.StatusCode)
	}
	return nil
}

// SnapshotUsage walks the mode profile directory (chrome/edge).
func (h *LocalHost) SnapshotUsage(_ context.Context, mode string) (int64, int64, int64) {
	if mode != ModeChrome && mode != ModeEdge {
		return 0, 0, 0
	}
	root := filepath.Join(h.profileRoot, mode+"-profile")
	var profile, cache, cookies int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		size := info.Size()
		profile += size
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isCachePath(rel) {
			cache += size
		}
		if isCookiesPath(rel) {
			cookies += size
		}
		return nil
	})
	return profile, cache, cookies
}

// ClearData removes cache/cookie artifacts older than the cutoff.
func (h *LocalHost) ClearData(_ context.Context, mode string, olderThan time.Time) (int64, error) {
	if mode != ModeChrome && mode != ModeEdge {
		return 0, nil
	}
	root := filepath.Join(h.profileRoot, mode+"-profile")
	var freed int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isCachePath(rel) && !isCookiesPath(rel) {
			return nil
		}
		if !info.ModTime().Before(olderThan) {
			return nil
		}
		if rmErr := removeFile(path); rmErr == nil {
			freed += info.Size()
		}
		return nil
	})
	return freed, nil
}

func (h *LocalHost) killProc(sessionID string) {
	h.mu.Lock()
	cmd, ok := h.procs[sessionID]
	if ok {
		delete(h.procs, sessionID)
	}
	h.mu.Unlock()
	if ok {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

func isCachePath(rel string) bool {
	for _, prefix := range []string{"Cache/", "Code Cache/", "GPUCache/", "Service Worker/CacheStorage/", "Media Cache/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return rel == "Cache" || rel == "Code Cache"
}

func isCookiesPath(rel string) bool {
	return rel == "Cookies" || rel == "Network/Cookies" || strings.HasPrefix(rel, "Network/Cookies")
}

// cdpVersionWS fetches /json/version once and extracts the browser ws url.
func cdpVersionWS(ctx context.Context, host string, port int) (string, error) {
	endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/json/version"
	req, err := NewHTTPRequestContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := DefaultHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: bridge port %d unreachable", ErrBrMode, port)
	}
	defer resp.Body.Close()
	var doc struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	dec := json.NewDecoder(resp.Body)
	if derr := dec.Decode(&doc); derr != nil || doc.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%w: bridge port %d not a CDP endpoint", ErrBrMode, port)
	}
	if !strings.HasPrefix(doc.WebSocketDebuggerURL, "ws://") {
		return "", fmt.Errorf("%w: unexpected ws url", ErrBrMode)
	}
	return doc.WebSocketDebuggerURL, nil
}

// cdpPollVersionWS retries cdpVersionWS until the deadline (launch ramp).
func cdpPollVersionWS(ctx context.Context, host string, port int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		ws, err := cdpVersionWS(ctx, host, port)
		if err == nil {
			return ws, nil
		}
		if time.Now().After(deadline) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// wsHostPort extracts host/port from a ws:// debugger url.
func wsHostPort(wsURL string) (string, int, error) {
	u, err := url.Parse(wsURL)
	if err != nil || u.Host == "" {
		return "", 0, fmt.Errorf("ws url invalid")
	}
	port := 80
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return "", 0, err
		}
	}
	return u.Hostname(), port, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
