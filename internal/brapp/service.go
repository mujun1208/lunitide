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
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
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
