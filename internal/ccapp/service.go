// Package ccapp implements the M10 wave-4 computer-control service: the
// single-row security configuration (enable flag, security level, process
// blocklist, rate and confirm caps, emergency-stop latch), the append-only
// operation ledger, and the three-layer interception pipeline (intent /
// risk classification, input filtering, foreground-process monitoring)
// behind the cc.* agent tools. Critical operations require the
// allow_critical switch plus manual confirmation; emergency stop latches
// every tool call until the operator re-runs the enable flow.
package ccapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/providerapp"
)

// Service-level errors mapped by the Bridge handlers and the tool runtime
// onto the M10-CC wire codes.
var (
	// ErrCcSchema: payload/arguments failed validation (M10-CC-001).
	ErrCcSchema = errors.New("ccapp: computer-control payload invalid")
	// ErrCcState: illegal state transition, e.g. enabling while the
	// emergency latch is armed without a fresh enable flow (M10-CC-002).
	ErrCcState = errors.New("ccapp: computer-control state invalid")
	// ErrCcNotFound: audit entry or resource missing (M10-CC-003).
	ErrCcNotFound = errors.New("ccapp: computer-control resource not found")
	// ErrCcRiskBlocked: operation rejected by the risk policy
	// (strict level, critical without allow_critical) (M10-CC-004).
	ErrCcRiskBlocked = errors.New("ccapp: operation blocked by risk policy")
	// ErrCcEmergency: emergency stop latch is armed (M10-CC-005).
	ErrCcEmergency = errors.New("ccapp: emergency stop active")
	// ErrCcRateLimited: actions exceeded maxActionsPerMinute (M10-CC-006).
	ErrCcRateLimited = errors.New("ccapp: computer-control rate limited")
	// ErrCcConfirmRequired: high/critical operation lacks the manual
	// confirmation (M10-CC-007).
	ErrCcConfirmRequired = errors.New("ccapp: operation requires manual confirmation")
	// ErrCcInputFiltered: arguments rejected by the input filter
	// (coordinates, text, key vocabulary, forbidden combos) (M10-CC-008).
	ErrCcInputFiltered = errors.New("ccapp: input rejected by filter")
	// ErrCcProcessBlocked: foreground process is on the blocklist (M10-CC-009).
	ErrCcProcessBlocked = errors.New("ccapp: target process blocked")
	// ErrCcEngineUnavailable: control engine missing (non-Windows host)
	// (M10-CC-010).
	ErrCcEngineUnavailable = errors.New("ccapp: control engine unavailable")
	// ErrCcExecFailed: the engine call itself failed (M10-CC-011).
	ErrCcExecFailed = errors.New("ccapp: operation failed")
	// ErrCcDisabled: computer control is not enabled (M10-CC-012).
	ErrCcDisabled = errors.New("ccapp: computer control disabled")
)

// Frozen enum values (wire contract).
const (
	ToolMouseMove        = "cc.mouse_move"
	ToolMouseClick       = "cc.mouse_click"
	ToolMouseDrag        = "cc.mouse_drag"
	ToolKeyboardType     = "cc.keyboard_type"
	ToolKeyboardShortcut = "cc.keyboard_shortcut"
	ToolScreenCapture    = "cc.screen_capture"
	ToolGetActiveWindow  = "cc.get_active_window"
	ToolWindowList       = "cc.window_list"
	ToolWindowFocus      = "cc.window_focus"
	ToolObserveDialog    = "cc.observe_dialog"
	ToolConfirmDialog    = "cc.confirm_dialog"
	ToolObserveUI        = "cc.observe_ui"
	ToolWait             = "cc.wait"
	ToolClipboard        = "cc.clipboard"
	ToolWindowAction     = "cc.window_action"
	ToolAppList          = "cc.app_list"
	ToolAppQuit          = "cc.app_quit"
	ToolPaste            = "cc.paste"
	ToolPress            = "cc.press"
	ToolMenuClick        = "cc.menu_click"
	ToolSetValue         = "cc.set_value"

	RiskLow      = "low"
	RiskMedium   = "medium"
	RiskHigh     = "high"
	RiskCritical = "critical"

	LevelStandard = "standard"
	LevelStrict   = "strict"

	StatusExecuted = "executed"
	StatusBlocked  = "blocked"
	StatusDenied   = "denied"
	StatusFailed   = "failed"
	StatusStopped  = "stopped"

	LayerIntent  = "intent"
	LayerInput   = "input-filter"
	LayerProcess = "process-monitor"
)

// Frozen caps.
const (
	// CcMaxAuditEntries bounds one getAuditLog response.
	CcMaxAuditEntries = 200
	// CcMaxTextRunes bounds keyboard_type text.
	CcMaxTextRunes = 4096
	// CcMaxShortcutKeys bounds keyboard_shortcut combos.
	CcMaxShortcutKeys = 4
	// CcMaxClipboardRunes bounds clipboard get/set text.
	CcMaxClipboardRunes = 8192
	// CcMaxListedWindows bounds cc.window_list.
	CcMaxListedWindows = 64
	// CcMaxObserveDialogs bounds cc.observe_dialog rows.
	CcMaxObserveDialogs = 16
	// CcMaxDialogNodes bounds clickable nodes on one observed dialog.
	CcMaxDialogNodes = 24
	// CcMaxBlocklistEntries / CcMaxBlocklistEntryLen bound the process
	// blocklist.
	CcMaxBlocklistEntries  = 128
	CcMaxBlocklistEntryLen = 128
)

// ccTools is the computer-control agent tool set.
var ccTools = map[string]bool{
	ToolMouseMove: true, ToolMouseClick: true, ToolMouseDrag: true,
	ToolKeyboardType: true, ToolKeyboardShortcut: true,
	ToolScreenCapture: true, ToolGetActiveWindow: true,
	ToolWindowList: true, ToolWindowFocus: true,
	ToolObserveDialog: true, ToolConfirmDialog: true, ToolObserveUI: true,
	ToolWait: true, ToolClipboard: true,
	ToolWindowAction: true, ToolAppList: true, ToolAppQuit: true,
	ToolPaste: true, ToolPress: true, ToolMenuClick: true, ToolSetValue: true,
}

// IsCcTool reports whether name belongs to the frozen cc tool set.
func IsCcTool(name string) bool { return ccTools[name] }

// Code maps one service error onto its M10-CC wire code ("M10-CC-001" ..
// "M10-CC-012"). Unknown errors answer the generic execution code.
func Code(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrCcSchema):
		return "M10-CC-001"
	case errors.Is(err, ErrCcState):
		return "M10-CC-002"
	case errors.Is(err, ErrCcNotFound):
		return "M10-CC-003"
	case errors.Is(err, ErrCcRiskBlocked):
		return "M10-CC-004"
	case errors.Is(err, ErrCcEmergency):
		return "M10-CC-005"
	case errors.Is(err, ErrCcRateLimited):
		return "M10-CC-006"
	case errors.Is(err, ErrCcConfirmRequired):
		return "M10-CC-007"
	case errors.Is(err, ErrCcInputFiltered):
		return "M10-CC-008"
	case errors.Is(err, ErrCcProcessBlocked):
		return "M10-CC-009"
	case errors.Is(err, ErrCcEngineUnavailable):
		return "M10-CC-010"
	case errors.Is(err, ErrCcExecFailed):
		return "M10-CC-011"
	case errors.Is(err, ErrCcDisabled):
		return "M10-CC-012"
	}
	return "M10-CC-011"
}

// DefaultProcessBlocklist seeds the first-read configuration: shells and
// system consoles where synthetic input could escalate privileges.
var DefaultProcessBlocklist = []string{
	"cmd.exe", "powershell.exe", "pwsh.exe", "regedit.exe",
	"taskmgr.exe", "mmc.exe", "eventvwr.exe", "consent.exe",
}

// forbiddenCombos are always rejected at the input-filter layer no matter
// the confirmation state (system-reserved sequences).
var forbiddenCombos = map[string]bool{
	"alt+ctrl+delete": true,
}

// criticalCombos carry system-level impact (close window, run dialog,
// lock, task manager, desktop flip); they ride the critical risk class.
var criticalCombos = map[string]bool{
	"alt+f4":         true,
	"alt+shift+esc":  false, // placeholder keeps map literal tidy
	"ctrl+shift+esc": true,
	"win+d":          true,
	"win+l":          true,
	"win+r":          true,
	"win+tab":        true,
}

// keyVocabulary is the portable key-name set accepted by
// keyboard_shortcut (lowercase; "del" normalizes to "delete").
var keyVocabulary = map[string]bool{
	"ctrl": true, "shift": true, "alt": true, "win": true,
	"enter": true, "esc": true, "space": true, "tab": true,
	"backspace": true, "delete": true, "home": true, "end": true,
	"pageup": true, "pagedown": true, "up": true, "down": true,
	"left": true, "right": true, "printscreen": true, "capslock": true,
	"a": true, "b": true, "c": true, "d": true, "e": true, "f": true,
	"g": true, "h": true, "i": true, "j": true, "k": true, "l": true,
	"m": true, "n": true, "o": true, "p": true, "q": true, "r": true,
	"s": true, "t": true, "u": true, "v": true, "w": true, "x": true,
	"y": true, "z": true,
	"0": true, "1": true, "2": true, "3": true, "4": true,
	"5": true, "6": true, "7": true, "8": true, "9": true,
	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true,
	"f6": true, "f7": true, "f8": true, "f9": true, "f10": true,
	"f11": true, "f12": true, "f13": true, "f14": true, "f15": true,
	"f16": true, "f17": true, "f18": true, "f19": true, "f20": true,
	"f21": true, "f22": true, "f23": true, "f24": true,
	"media_play": true, "media_pause": true, "media_next": true, "media_prev": true, "media_stop": true,
}

// Settings is the single-row security configuration.
type Settings struct {
	Enabled              bool     `json:"enabled"`
	SecurityLevel        string   `json:"securityLevel"`
	AllowCritical        bool     `json:"allowCritical"`
	ProcessBlocklist     []string `json:"processBlocklist"`
	MaxActionsPerMinute  int      `json:"maxActionsPerMinute"`
	ConfirmTimeoutSecond int      `json:"confirmTimeoutSeconds"`
	EmergencyStopped     bool     `json:"emergencyStopped"`
	EmergencyStoppedAt   string   `json:"emergencyStoppedAt,omitempty"`
	UpdatedAt            string   `json:"updatedAt"`
}

// AuditEntry is one append-only ledger row.
type AuditEntry struct {
	EntryID   string `json:"entryId"`
	SessionID string `json:"sessionId"`
	Tool      string `json:"tool"`
	Action    string `json:"action"`
	RiskLevel string `json:"riskLevel"`
	Status    string `json:"status"`
	Layer     string `json:"layer,omitempty"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"createdAt"`
}

// SettingsPatch carries optional updateConfig fields.
type SettingsPatch struct {
	Enabled              *bool
	SecurityLevel        *string
	AllowCritical        *bool
	ProcessBlocklist     *[]string
	MaxActionsPerMinute  *int
	ConfirmTimeoutSecond *int
	Actor                string
}

// Host abstracts the OS control surface (User32/SendInput on Windows).
// The service never touches syscalls directly; tests inject fakes.
// MouseMove/MouseDrag take screen pixels (SetCursorPos space), not the
// 0..65535 SendInput range.
type Host interface {
	Available() bool
	ScreenSize() (width, height int)
	ScreenOrigin() (x, y int)
	CursorPosition() (x, y int, err error)
	MouseMove(x, y int) error
	MouseClick(button string, clicks int) error
	MouseDrag(x1, y1, x2, y2 int) error
	KeyboardType(text string) error
	KeyboardShortcut(keys []string) error
	MouseScroll(notches int) error
	MouseScrollH(notches int) error
	EnsureForeground() error
	ScreenCapture() (png []byte, err error)
	WindowCapture(query string) (png []byte, originX, originY int, err error)
	ActiveWindow() (title, process string, err error)
	ListWindows() ([]WindowInfo, error)
	FocusWindow(query string) (WindowInfo, error)
	ObserveDialogs() ([]DialogSnapshot, error)
	ConfirmDialog(button string) (DialogSnapshot, error)
	ObserveUI(maxNodes int) ([]UINode, error)
	ClipboardGet() (string, error)
	ClipboardSet(text string) error
	WindowAction(query, op string, x, y, w, h int) (WindowInfo, error)
	QuitApp(query string) (closed int, sample WindowInfo, err error)
	MenuClick(path string) error
	SetValue(target, value string) error
	// InvokeUI activates a named foreground accessibility node via
	// IAccessible::accDoDefaultAction (DoDefaultAction / Invoke).
	InvokeUI(target string) error
}

// Clock is the injectable time source.
type Clock interface{ Now() time.Time }

// Tx is the wave-4 computer-control transaction (satisfied by the shared
// agent-runtime tx).
type Tx interface {
	GetCcSettings() (Settings, error)
	PutCcSettings(Settings) error
	AppendCcAudit(AuditEntry) error
	ListCcAudit(limit int, status, sessionID string) ([]AuditEntry, error)
	PutAudit(providerapp.Audit) error
}

// UnitOfWork is the wave-4 single-writer boundary.
type UnitOfWork interface {
	TransactCc(ctx context.Context, fn func(Tx) error) error
}

// Outcome is one executed tool call.
type Outcome struct {
	Tool    string
	Summary string
	// CapturePNG carries the screen_capture artifact bytes; the tool
	// runtime persists them into the session workspace.
	CapturePNG []byte
}

// rateWindow is the sliding-window limiter shared with brapp's shape.
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

// Service implements the cc.* surface.
type Service struct {
	uow                                  UnitOfWork
	clock                                Clock
	host                                 Host
	limit                                rateWindow
	emuMu                                sync.Mutex
	emuAt                                time.Time
	capMu                                sync.Mutex
	capVisW, capVisH, capDeskW, capDeskH int
	capOriginX, capOriginY               int
	capHash                              [32]byte
	obsHits                              map[string]uiHit
	lastMu                               sync.Mutex
	lastTitle, lastProc                  string
}

// New returns a Service over the given unit of work with the platform
// host (unavailable on non-Windows builds).
func New(uow UnitOfWork) *Service {
	return &Service{uow: uow, clock: systemClock{}, host: PlatformHost()}
}

// SetClock substitutes the clock (tests).
func (s *Service) SetClock(c Clock) { s.clock = c }

// SetHost substitutes the control host (tests).
func (s *Service) SetHost(h Host) { s.host = h }

func (s *Service) rememberCapture(png []byte, originX, originY int) {
	imgW, imgH, visW, visH := visionDimensions(png)
	s.capMu.Lock()
	deskW, deskH := imgW, imgH
	if originX == s.capOriginX && originY == s.capOriginY &&
		s.capDeskW > 0 && s.capDeskH > 0 &&
		imgW == s.capVisW && imgH == s.capVisH &&
		(s.capDeskW != imgW || s.capDeskH != imgH) {
		// Annotated / downscaled vision frame of the current desktop capture:
		// keep the original desktop size so later clicks still map correctly.
		deskW, deskH = s.capDeskW, s.capDeskH
		visW, visH = imgW, imgH
	}
	s.capDeskW, s.capDeskH, s.capVisW, s.capVisH = deskW, deskH, visW, visH
	s.capOriginX, s.capOriginY = originX, originY
	if len(png) > 0 {
		s.capHash = sha256.Sum256(png)
	}
	s.capMu.Unlock()
}

func (s *Service) mapPoint(x, y int) (int, int) {
	s.capMu.Lock()
	vw, vh, dw, dh := s.capVisW, s.capVisH, s.capDeskW, s.capDeskH
	s.capMu.Unlock()
	return MapCapturePoint(x, y, vw, vh, dw, dh)
}

// toScreen maps a point in the last vision image onto SetCursorPos screen
// pixels. Before the first capture, the point is treated as virtual-desktop
// relative (origin = ScreenOrigin).
func (s *Service) toScreen(x, y int) (int, int) {
	dx, dy := s.mapPoint(x, y)
	s.capMu.Lock()
	ox, oy := s.capOriginX, s.capOriginY
	hasCap := s.capDeskW > 0 || s.capVisW > 0
	s.capMu.Unlock()
	if !hasCap {
		ox, oy = s.host.ScreenOrigin()
	}
	return ox + dx, oy + dy
}

func (s *Service) rejectOutOfBounds(x, y int) error {
	s.capMu.Lock()
	vw, vh := s.capVisW, s.capVisH
	s.capMu.Unlock()
	if vw > 0 && vh > 0 {
		if x < 0 || y < 0 || x > vw || y > vh {
			return fmt.Errorf("%w: coordinates out of bounds %d,%d (image %dx%d)", ErrCcInputFiltered, x, y, vw, vh)
		}
		return nil
	}
	w, h := s.host.ScreenSize()
	if w > 0 && h > 0 && (x < 0 || y < 0 || x > w || y > h) {
		return fmt.Errorf("%w: coordinates out of bounds %d,%d", ErrCcInputFiltered, x, y)
	}
	return nil
}

func (s *Service) captureDesktop() ([]byte, error) {
	png, err := s.host.ScreenCapture()
	if err != nil {
		return nil, err
	}
	ox, oy := s.host.ScreenOrigin()
	s.rememberCapture(png, ox, oy)
	return png, nil
}

// CaptureDesktopPNG grabs the virtual desktop without running the cc tool
// pipeline. People chat uses this for a local screenshot (no browser share UI).
func (s *Service) CaptureDesktopPNG() ([]byte, error) {
	return s.host.ScreenCapture()
}

// verifyAfter takes a fresh desktop screenshot so the model can see the
// result of an input action (OpenClaw observe→act→verify). Unchanged pixels
// stay out of the vision payload.
func (s *Service) verifyAfter(summary string) (string, []byte, error) {
	png, err := s.host.ScreenCapture()
	if err != nil {
		return summary + " (verify capture failed)", nil, nil
	}
	sum := sha256.Sum256(png)
	s.capMu.Lock()
	prev := s.capHash
	s.capMu.Unlock()
	if prev != [32]byte{} && prev == sum {
		return summary + "; screen unchanged since previous frame", nil, nil
	}
	ox, oy := s.host.ScreenOrigin()
	s.rememberCapture(png, ox, oy)
	deskW, deskH, visW, visH := visionDimensions(png)
	if deskW == 0 {
		deskW, deskH = s.host.ScreenSize()
		visW, visH = deskW, deskH
	}
	return fmt.Sprintf("%s; screen updated %dx%d (use image %dx%d)", summary, deskW, deskH, visW, visH), png, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func actorOr(actor string) string {
	if actor == "" {
		return "renderer"
	}
	return actor
}

// ── settings ────────────────────────────────────────────────────────────────

// GetConfig answers the singleton, seeding the default row on first read.
func (s *Service) GetConfig(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		var e error
		out, e = tx.GetCcSettings()
		return e
	})
	return out, err
}

func defaultSettings(now string) Settings {
	return Settings{
		Enabled:              false,
		SecurityLevel:        LevelStandard,
		AllowCritical:        false,
		ProcessBlocklist:     append([]string(nil), DefaultProcessBlocklist...),
		MaxActionsPerMinute:  30,
		ConfirmTimeoutSecond: 60,
		UpdatedAt:            now,
	}
}

// ValidateSettings checks one settings document (shared by update and
// seeding).
func ValidateSettings(v Settings) error {
	if v.SecurityLevel != LevelStandard && v.SecurityLevel != LevelStrict {
		return fmt.Errorf("%w: securityLevel", ErrCcSchema)
	}
	if v.MaxActionsPerMinute < 1 || v.MaxActionsPerMinute > 120 {
		return fmt.Errorf("%w: maxActionsPerMinute", ErrCcSchema)
	}
	if v.ConfirmTimeoutSecond < 10 || v.ConfirmTimeoutSecond > 600 {
		return fmt.Errorf("%w: confirmTimeoutSeconds", ErrCcSchema)
	}
	if len(v.ProcessBlocklist) > CcMaxBlocklistEntries {
		return fmt.Errorf("%w: processBlocklist size", ErrCcSchema)
	}
	seen := map[string]bool{}
	for _, item := range v.ProcessBlocklist {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" || len(name) > CcMaxBlocklistEntryLen || strings.ContainsAny(name, "/\\") || seen[name] {
			return fmt.Errorf("%w: processBlocklist entry", ErrCcSchema)
		}
		seen[name] = true
	}
	return nil
}

// UpdateConfig applies one patch. Re-enabling clears the emergency latch
// (the operator just re-ran the enable flow); any other patch with the
// latch armed is refused so the stop stays visible until acknowledged.
func (s *Service) UpdateConfig(ctx context.Context, patch SettingsPatch) (Settings, error) {
	var out Settings
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		cur, err := tx.GetCcSettings()
		if err != nil {
			return err
		}
		next := cur
		if patch.Enabled != nil {
			next.Enabled = *patch.Enabled
		}
		if patch.SecurityLevel != nil {
			next.SecurityLevel = *patch.SecurityLevel
		}
		if patch.AllowCritical != nil {
			next.AllowCritical = *patch.AllowCritical
		}
		if patch.ProcessBlocklist != nil {
			next.ProcessBlocklist = *patch.ProcessBlocklist
		}
		if patch.MaxActionsPerMinute != nil {
			next.MaxActionsPerMinute = *patch.MaxActionsPerMinute
		}
		if patch.ConfirmTimeoutSecond != nil {
			next.ConfirmTimeoutSecond = *patch.ConfirmTimeoutSecond
		}
		if err := ValidateSettings(next); err != nil {
			return err
		}
		if cur.EmergencyStopped {
			if patch.Enabled == nil || !*patch.Enabled {
				return fmt.Errorf("%w: 紧急停止已激活，需重新走启用流程", ErrCcState)
			}
			next.EmergencyStopped = false
			next.EmergencyStoppedAt = ""
		}
		if patch.Enabled != nil && !*patch.Enabled {
			next.EmergencyStopped = false
			next.EmergencyStoppedAt = ""
		}
		ts := s.clock.Now().UTC().Format(time.RFC3339)
		next.UpdatedAt = ts
		if err := tx.PutCcSettings(next); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{
			"enabled": next.Enabled, "securityLevel": next.SecurityLevel,
			"allowCritical": next.AllowCritical,
		})
		if err := tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "cc.config.updated",
			AggregateID: "cc-security-config", Actor: actorOr(patch.Actor),
			Metadata: meta, CreatedAt: s.clock.Now().UTC(),
		}); err != nil {
			return err
		}
		out = next
		return nil
	})
	return out, err
}

// ── audit log ───────────────────────────────────────────────────────────────

// GetAuditLog answers the newest entries (bounded by CcMaxAuditEntries)
// with optional status/session filters.
func (s *Service) GetAuditLog(ctx context.Context, limit int, status, sessionID string) ([]AuditEntry, error) {
	if limit <= 0 || limit > CcMaxAuditEntries {
		limit = 50
	}
	if status != "" && status != StatusExecuted && status != StatusBlocked &&
		status != StatusDenied && status != StatusFailed && status != StatusStopped {
		return nil, fmt.Errorf("%w: status filter", ErrCcSchema)
	}
	var out []AuditEntry
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		var e error
		out, e = tx.ListCcAudit(limit, status, sessionID)
		return e
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []AuditEntry{}
	}
	return out, nil
}

// ── emergency stop ──────────────────────────────────────────────────────────

// EmergencyStop arms the latch: every later tool call fails closed with
// M10-CC-005 until the operator re-runs the enable flow.
func (s *Service) EmergencyStop(ctx context.Context, actor, reason string) (Settings, error) {
	var out Settings
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		cur, err := tx.GetCcSettings()
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		ts := now.Format(time.RFC3339)
		cur.EmergencyStopped = true
		cur.EmergencyStoppedAt = ts
		cur.UpdatedAt = ts
		if err := tx.PutCcSettings(cur); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"reason": clampReason(reason)})
		if err := tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "cc.emergency.stopped",
			AggregateID: "cc-security-config", Actor: actorOr(actor),
			Metadata: meta, CreatedAt: now,
		}); err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, err
}

func clampReason(reason string) string {
	if len(reason) > 512 {
		reason = reason[:512]
	}
	return reason
}

// ── tool execution pipeline ─────────────────────────────────────────────────

// classifyRisk answers the four-level risk class for one tool call
// (layer 1: intent recognition).
func classifyRisk(tool string, normalizedShortcut []string) string {
	switch tool {
	case ToolMouseMove, ToolGetActiveWindow, ToolObserveDialog, ToolWindowList, ToolObserveUI, ToolWait, ToolAppList:
		return RiskLow
	case ToolMouseClick, ToolMouseDrag, ToolKeyboardType, ToolScreenCapture, ToolConfirmDialog, ToolWindowFocus:
		return RiskMedium
	case ToolClipboard, ToolPaste, ToolPress, ToolSetValue:
		return RiskMedium
	case ToolWindowAction:
		if len(normalizedShortcut) > 0 && destructiveWindowOp(normalizedShortcut[0]) {
			return RiskHigh
		}
		return RiskMedium
	case ToolAppQuit, ToolMenuClick:
		return RiskHigh
	case ToolKeyboardShortcut:
		if isCriticalCombo(normalizedShortcut) {
			return RiskCritical
		}
		return RiskHigh
	}
	return RiskCritical
}

// normalizeShortcut validates and normalizes key names: lowercase, del →
// delete, order-insensitive combo signature.
func normalizeShortcut(keys []string) ([]string, error) {
	if len(keys) < 1 || len(keys) > CcMaxShortcutKeys {
		return nil, fmt.Errorf("%w: combo size", ErrCcInputFiltered)
	}
	hasNonModifier := false
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, raw := range keys {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "del" {
			key = "delete"
		}
		if !keyVocabulary[key] {
			return nil, fmt.Errorf("%w: key %q", ErrCcInputFiltered, raw)
		}
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate key %q", ErrCcInputFiltered, key)
		}
		seen[key] = true
		if key != "ctrl" && key != "shift" && key != "alt" && key != "win" {
			hasNonModifier = true
		}
		out = append(out, key)
	}
	if !hasNonModifier {
		return nil, fmt.Errorf("%w: modifier-only combo", ErrCcInputFiltered)
	}
	sort.Strings(out)
	if forbiddenCombos[strings.Join(out, "+")] {
		return nil, fmt.Errorf("%w: forbidden combo", ErrCcInputFiltered)
	}
	return out, nil
}

func isCriticalCombo(normalized []string) bool {
	return criticalCombos[strings.Join(normalized, "+")]
}

// screenAffecting reports whether a tool acts on the shared desktop and
// therefore rides the foreground-process gate (layer 3).
func screenAffecting(tool string) bool {
	switch tool {
	case ToolGetActiveWindow, ToolObserveDialog, ToolObserveUI, ToolWindowList, ToolWait, ToolAppList:
		return false
	case ToolClipboard:
		return false
	}
	return true
}

func blocklistHit(blocklist []string, process string) bool {
	name := strings.ToLower(strings.TrimSpace(process))
	for _, item := range blocklist {
		if strings.EqualFold(strings.TrimSpace(item), name) {
			return true
		}
	}
	return false
}

// ExecuteTool runs the full interception pipeline for one cc.* tool call.
// approved marks the chat-side manual confirmation (high/critical gate).
func (s *Service) ExecuteTool(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (Outcome, error) {
	if !ccTools[tool] {
		return Outcome{}, fmt.Errorf("%w: tool %q", ErrCcSchema, tool)
	}
	if len(session) < 1 || len(session) > 64 {
		return Outcome{}, fmt.Errorf("%w: sessionId", ErrCcSchema)
	}
	var settings Settings
	var emergency bool
	if err := s.uow.TransactCc(ctx, func(tx Tx) error {
		var e error
		settings, e = tx.GetCcSettings()
		return e
	}); err != nil {
		return Outcome{}, err
	}
	emergency = settings.EmergencyStopped

	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)

	// Gate 0: enabled / emergency latch.
	if !settings.Enabled {
		s.recordAudit(ctx, session, tool, classifyRisk(tool, nil), StatusDenied, "", map[string]any{"reason": "disabled"}, ts)
		return Outcome{}, ErrCcDisabled
	}
	if emergency {
		s.recordAudit(ctx, session, tool, classifyRisk(tool, nil), StatusStopped, "", map[string]any{"reason": "emergency-stop"}, ts)
		return Outcome{}, ErrCcEmergency
	}
	// Rate limit: every attempted action consumes one slot.
	if !s.limit.allow(now, settings.MaxActionsPerMinute) {
		s.recordAudit(ctx, session, tool, classifyRisk(tool, nil), StatusDenied, "", map[string]any{"reason": "rate-limited", "cap": settings.MaxActionsPerMinute}, ts)
		return Outcome{}, ErrCcRateLimited
	}
	if s.host == nil || !s.host.Available() {
		s.recordAudit(ctx, session, tool, RiskMedium, StatusFailed, "", map[string]any{"reason": "engine-unavailable"}, ts)
		return Outcome{}, ErrCcEngineUnavailable
	}

	// Layer 2: input filtering (also parses the tool arguments).
	shortcut, err := s.filterInput(tool, args)
	if err != nil {
		s.recordAudit(ctx, session, tool, classifyRisk(tool, nil), StatusBlocked, LayerInput, map[string]any{"reason": err.Error()}, ts)
		return Outcome{}, err
	}

	// Layer 1: intent / risk classification and the confirmation gate.
	risk := classifyRisk(tool, shortcut)
	switch risk {
	case RiskCritical:
		if !settings.AllowCritical {
			s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "critical not allowed"}, ts)
			return Outcome{}, ErrCcRiskBlocked
		}
		if !approved {
			s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "confirmation required"}, ts)
			return Outcome{}, ErrCcConfirmRequired
		}
	case RiskHigh:
		if settings.SecurityLevel == LevelStrict {
			s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "strict level"}, ts)
			return Outcome{}, ErrCcRiskBlocked
		}
		if !approved {
			s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "confirmation required"}, ts)
			return Outcome{}, ErrCcConfirmRequired
		}
	}

	// Layer 3: foreground-process monitoring, plus target-process checks
	// for window close / app quit (the victim may not be in the foreground).
	if screenAffecting(tool) {
		title, process, err := s.host.ActiveWindow()
		if err == nil {
			s.noteForeground(title, process)
			if blocklistHit(settings.ProcessBlocklist, process) {
				s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerProcess, map[string]any{"process": process}, ts)
				return Outcome{}, ErrCcProcessBlocked
			}
		}
	}
	if err := s.rejectTargetProcess(settings, tool, args); err != nil {
		s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerProcess, map[string]any{"reason": err.Error()}, ts)
		return Outcome{}, err
	}

	summary, capture, execErr := s.runHost(tool, args, shortcut)
	if execErr != nil {
		if errors.Is(execErr, ErrCcRiskBlocked) {
			s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": execErr.Error()}, ts)
			return Outcome{}, execErr
		}
		s.recordAudit(ctx, session, tool, risk, StatusFailed, "", map[string]any{"reason": execErr.Error()}, ts)
		return Outcome{}, fmt.Errorf("%w: %v", ErrCcExecFailed, execErr)
	}
	action := "cc.operation.executed"
	if approved && (risk == RiskHigh || risk == RiskCritical) {
		action = "cc.operation.confirmed"
	}
	s.writeAudit(ctx, session, tool, risk, StatusExecuted, "", action, map[string]any{"summary": clampReason(summary)}, ts)
	out := Outcome{Tool: tool, Summary: summary, CapturePNG: capture}
	return out, nil
}

// filterInput validates the per-tool argument shapes (layer 2).
func (s *Service) filterInput(tool string, args json.RawMessage) ([]string, error) {
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	switch tool {
	case ToolMouseMove:
		var a struct {
			X int `json:"x"`
			Y int `json:"y"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.X < 0 || a.Y < 0 || a.X > 65535 || a.Y > 65535 {
			return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
		}
		if err := s.rejectOutOfBounds(a.X, a.Y); err != nil {
			return nil, err
		}
		return nil, nil
	case ToolMouseClick:
		var a struct {
			Button     string `json:"button"`
			Clicks     int    `json:"clicks"`
			X          *int   `json:"x"`
			Y          *int   `json:"y"`
			Scroll     int    `json:"scroll"`
			ScrollAxis string `json:"scrollAxis"`
			Name       string `json:"name"`
			ID         string `json:"id"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Scroll < -12 || a.Scroll > 12 {
			return nil, fmt.Errorf("%w: scroll", ErrCcInputFiltered)
		}
		if a.ScrollAxis != "" && a.ScrollAxis != "vertical" && a.ScrollAxis != "horizontal" {
			return nil, fmt.Errorf("%w: scrollAxis", ErrCcInputFiltered)
		}
		if a.Button == "" {
			a.Button = "left"
		}
		if a.Scroll == 0 && a.Button != "left" && a.Button != "right" && a.Button != "middle" {
			return nil, fmt.Errorf("%w: button %q", ErrCcInputFiltered, a.Button)
		}
		if a.Clicks < 1 {
			a.Clicks = 1
		}
		if a.Clicks > 3 {
			return nil, fmt.Errorf("%w: clicks", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Name) > 80 {
			return nil, fmt.Errorf("%w: name", ErrCcInputFiltered)
		}
		if a.ID != "" && (len(a.ID) > 8 || !validNodeID(a.ID)) {
			return nil, fmt.Errorf("%w: id", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.ID) > 8 {
			return nil, fmt.Errorf("%w: id", ErrCcInputFiltered)
		}
		if (a.X == nil) != (a.Y == nil) {
			return nil, fmt.Errorf("%w: x and y must be paired", ErrCcInputFiltered)
		}
		if a.X != nil {
			if *a.X < 0 || *a.Y < 0 || *a.X > 65535 || *a.Y > 65535 {
				return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
			}
			if err := s.rejectOutOfBounds(*a.X, *a.Y); err != nil {
				return nil, err
			}
		}
		return nil, nil
	case ToolMouseDrag:
		var a struct {
			X1 int `json:"x1"`
			Y1 int `json:"y1"`
			X2 int `json:"x2"`
			Y2 int `json:"y2"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		for _, n := range []int{a.X1, a.Y1, a.X2, a.Y2} {
			if n < 0 || n > 65535 {
				return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
			}
		}
		if err := s.rejectOutOfBounds(a.X1, a.Y1); err != nil {
			return nil, err
		}
		if err := s.rejectOutOfBounds(a.X2, a.Y2); err != nil {
			return nil, err
		}
		return nil, nil
	case ToolKeyboardType:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if !utf8.ValidString(a.Text) {
			return nil, fmt.Errorf("%w: text encoding", ErrCcInputFiltered)
		}
		runes := []rune(a.Text)
		if len(runes) < 1 || len(runes) > CcMaxTextRunes {
			return nil, fmt.Errorf("%w: text length", ErrCcInputFiltered)
		}
		for _, r := range runes {
			if r == '\t' || r == '\n' || r == '\r' {
				continue
			}
			if unicode.IsControl(r) {
				return nil, fmt.Errorf("%w: control character", ErrCcInputFiltered)
			}
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolKeyboardShortcut:
		var a struct {
			Keys   []string `json:"keys"`
			Window string   `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return normalizeShortcut(a.Keys)
	case ToolScreenCapture:
		var a struct {
			Target string `json:"target"`
			Title  string `json:"title"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Target != "" && a.Target != "desktop" && a.Target != "foreground" && a.Target != "window" {
			return nil, fmt.Errorf("%w: target", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Title) > 200 {
			return nil, fmt.Errorf("%w: title", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolGetActiveWindow, ToolWindowList:
		var a struct{}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		return nil, nil
	case ToolWindowFocus:
		var a struct {
			Title   string `json:"title"`
			Process string `json:"process"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		q := windowFocusQuery(a.Title, a.Process)
		if q == "" || utf8.RuneCountInString(q) > 200 {
			return nil, fmt.Errorf("%w: title or process", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolObserveDialog:
		var a struct {
			WaitMs int `json:"waitMs"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.WaitMs < 0 || a.WaitMs > 5000 {
			return nil, fmt.Errorf("%w: waitMs", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolConfirmDialog:
		var a struct {
			Button string `json:"button"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if len([]rune(a.Button)) > 32 {
			return nil, fmt.Errorf("%w: button", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolObserveUI:
		var a struct {
			MaxNodes int `json:"maxNodes"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.MaxNodes < 0 || a.MaxNodes > 80 {
			return nil, fmt.Errorf("%w: maxNodes", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolWait:
		var a struct {
			Ms    *int   `json:"ms"`
			Until string `json:"until"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Ms != nil && (*a.Ms < 0 || *a.Ms > 8000) {
			return nil, fmt.Errorf("%w: ms", ErrCcInputFiltered)
		}
		if a.Until != "" && a.Until != "timeout" && a.Until != "change" {
			return nil, fmt.Errorf("%w: until", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolClipboard:
		var a struct {
			Op   string `json:"op"`
			Text string `json:"text"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Op != "get" && a.Op != "set" {
			return nil, fmt.Errorf("%w: op", ErrCcInputFiltered)
		}
		if a.Op == "set" {
			if !utf8.ValidString(a.Text) || utf8.RuneCountInString(a.Text) < 1 || utf8.RuneCountInString(a.Text) > CcMaxClipboardRunes {
				return nil, fmt.Errorf("%w: text", ErrCcInputFiltered)
			}
		}
		return nil, nil
	case ToolWindowAction:
		var a struct {
			Title string `json:"title"`
			Op    string `json:"op"`
			X     int    `json:"x"`
			Y     int    `json:"y"`
			W     int    `json:"w"`
			H     int    `json:"h"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if utf8.RuneCountInString(a.Title) > 200 {
			return nil, fmt.Errorf("%w: title", ErrCcInputFiltered)
		}
		op := strings.ToLower(strings.TrimSpace(a.Op))
		switch op {
		case "close", "minimize", "maximize", "restore", "hide":
		case "move":
			if a.X < -32768 || a.Y < -32768 || a.X > 65535 || a.Y > 65535 {
				return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
			}
		case "resize":
			if a.W < 1 || a.H < 1 || a.W > 65535 || a.H > 65535 {
				return nil, fmt.Errorf("%w: size", ErrCcInputFiltered)
			}
		default:
			return nil, fmt.Errorf("%w: op", ErrCcInputFiltered)
		}
		return []string{op}, nil
	case ToolAppList:
		var a struct{}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		return nil, nil
	case ToolAppQuit:
		var a struct {
			Title string `json:"title"`
			Name  string `json:"name"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if strings.TrimSpace(a.Title) == "" && strings.TrimSpace(a.Name) == "" {
			return nil, fmt.Errorf("%w: title or name required", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Title) > 200 || utf8.RuneCountInString(a.Name) > 200 {
			return nil, fmt.Errorf("%w: query", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolPaste:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Text != "" && (!utf8.ValidString(a.Text) || utf8.RuneCountInString(a.Text) > 8192) {
			return nil, fmt.Errorf("%w: text", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolPress:
		var a struct {
			Key    string `json:"key"`
			Count  int    `json:"count"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		key := strings.ToLower(strings.TrimSpace(a.Key))
		if key == "del" {
			key = "delete"
		}
		if !keyVocabulary[key] || key == "ctrl" || key == "shift" || key == "alt" || key == "win" {
			return nil, fmt.Errorf("%w: key", ErrCcInputFiltered)
		}
		if a.Count < 0 || a.Count > 8 {
			return nil, fmt.Errorf("%w: count", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolMenuClick:
		var a struct {
			Path   string `json:"path"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		segs := SplitMenuPath(a.Path)
		if len(segs) < 1 || len(segs) > 6 {
			return nil, fmt.Errorf("%w: path", ErrCcInputFiltered)
		}
		for _, seg := range segs {
			if utf8.RuneCountInString(seg) > 80 {
				return nil, fmt.Errorf("%w: path segment", ErrCcInputFiltered)
			}
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolSetValue:
		var a struct {
			Target string `json:"target"`
			Value  string `json:"value"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if strings.TrimSpace(a.Target) == "" || utf8.RuneCountInString(a.Target) > 80 {
			return nil, fmt.Errorf("%w: target", ErrCcInputFiltered)
		}
		if !utf8.ValidString(a.Value) || utf8.RuneCountInString(a.Value) > 4096 {
			return nil, fmt.Errorf("%w: value", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("%w: tool", ErrCcSchema)
}

func (s *Service) rejectTargetProcess(settings Settings, tool string, args json.RawMessage) error {
	if tool != ToolWindowAction && tool != ToolAppQuit {
		return nil
	}
	if s.host == nil {
		return nil
	}
	var a struct {
		Title string `json:"title"`
		Name  string `json:"name"`
		Op    string `json:"op"`
	}
	_ = json.Unmarshal(args, &a)
	query := strings.TrimSpace(a.Title)
	if query == "" {
		query = strings.TrimSpace(a.Name)
	}
	if query == "" {
		query = "foreground"
	}
	wins, err := s.host.ListWindows()
	if err != nil || len(wins) == 0 {
		return nil
	}
	var targets []WindowInfo
	if tool == ToolAppQuit {
		targets = MatchWindows(wins, query)
	} else if info, ok := MatchWindow(wins, query); ok {
		targets = []WindowInfo{info}
	}
	op := strings.ToLower(strings.TrimSpace(a.Op))
	checkProtected := tool == ToolAppQuit || destructiveWindowOp(op)
	for _, w := range targets {
		if blocklistHit(settings.ProcessBlocklist, w.Process) {
			return ErrCcProcessBlocked
		}
		if checkProtected && ProtectedDesktopProcess(w.Process) {
			return fmt.Errorf("%w: protected process %s", ErrCcRiskBlocked, w.Process)
		}
	}
	return nil
}

func isCompanionProcess(process string) bool {
	name := processStem(process)
	return name == "lunitide" || name == "lunitide-engine"
}

func (s *Service) noteForeground(title, process string) {
	if isCompanionProcess(process) {
		return
	}
	if strings.TrimSpace(title) == "" && strings.TrimSpace(process) == "" {
		return
	}
	s.lastMu.Lock()
	s.lastTitle, s.lastProc = title, process
	s.lastMu.Unlock()
}

func (s *Service) focusIfNamed(window string) error {
	window = strings.TrimSpace(window)
	if window != "" {
		info, err := s.host.FocusWindow(window)
		if err != nil {
			return err
		}
		s.noteForeground(info.Title, info.Process)
		return nil
	}
	title, process, err := s.host.ActiveWindow()
	if err == nil {
		s.noteForeground(title, process)
		if !isCompanionProcess(process) {
			return s.host.EnsureForeground()
		}
	}
	s.lastMu.Lock()
	q := strings.TrimSpace(s.lastTitle)
	if q == "" {
		q = strings.TrimSpace(s.lastProc)
	}
	s.lastMu.Unlock()
	if q != "" {
		info, err := s.host.FocusWindow(q)
		if err == nil {
			s.noteForeground(info.Title, info.Process)
			return nil
		}
	}
	if wins, err := s.host.ListWindows(); err == nil {
		for _, w := range wins {
			if isCompanionProcess(w.Process) || strings.TrimSpace(w.Title) == "" {
				continue
			}
			query := w.ID
			if query == "" {
				query = w.Title
			}
			info, err := s.host.FocusWindow(query)
			if err == nil {
				s.noteForeground(info.Title, info.Process)
				return nil
			}
		}
	}
	return s.host.EnsureForeground()
}

func (s *Service) captureSpace() (ox, oy, visW, visH, deskW, deskH int, space string) {
	s.capMu.Lock()
	visW, visH, deskW, deskH = s.capVisW, s.capVisH, s.capDeskW, s.capDeskH
	ox, oy = s.capOriginX, s.capOriginY
	s.capMu.Unlock()
	if deskW > 0 && deskH > 0 {
		return ox, oy, visW, visH, deskW, deskH, "image"
	}
	if s.host != nil {
		ox, oy = s.host.ScreenOrigin()
		deskW, deskH = s.host.ScreenSize()
	}
	if deskW <= 0 || deskH <= 0 {
		return ox, oy, 0, 0, 0, 0, "screen"
	}
	return ox, oy, deskW, deskH, deskW, deskH, "screen"
}

func (s *Service) mapWindows(wins []WindowInfo) (mapped []WindowInfo, space string) {
	if len(wins) > CcMaxListedWindows {
		wins = wins[:CcMaxListedWindows]
	}
	ox, oy, vw, vh, dw, dh, space := s.captureSpace()
	out := make([]WindowInfo, len(wins))
	for i, w := range wins {
		w.X, w.Y, w.W, w.H = ProjectRect(w.X, w.Y, w.W, w.H, ox, oy, vw, vh, dw, dh)
		out[i] = w
	}
	return out, space
}

func (s *Service) mapDialogs(snaps []DialogSnapshot) []DialogSnapshot {
	if len(snaps) > CcMaxObserveDialogs {
		snaps = snaps[:CcMaxObserveDialogs]
	}
	ox, oy, vw, vh, dw, dh, _ := s.captureSpace()
	out := make([]DialogSnapshot, len(snaps))
	for i, d := range snaps {
		d.X, d.Y, d.W, d.H = ProjectRect(d.X, d.Y, d.W, d.H, ox, oy, vw, vh, dw, dh)
		if len(d.Nodes) > CcMaxDialogNodes {
			d.Nodes = d.Nodes[:CcMaxDialogNodes]
		}
		nodes := make([]UINode, len(d.Nodes))
		for j, n := range d.Nodes {
			n.X, n.Y, n.W, n.H = ProjectRect(n.X, n.Y, n.W, n.H, ox, oy, vw, vh, dw, dh)
			nodes[j] = n
		}
		d.Nodes = nodes
		out[i] = d
	}
	return out
}

func clampClipboard(text string) string {
	if utf8.RuneCountInString(text) <= CcMaxClipboardRunes {
		return text
	}
	return string([]rune(text)[:CcMaxClipboardRunes])
}

func (s *Service) captureSummary(png []byte, kind string) string {
	deskW, deskH, visW, visH := visionDimensions(png)
	if deskW == 0 {
		deskW, deskH = s.host.ScreenSize()
		visW, visH = deskW, deskH
	}
	return fmt.Sprintf("captured %s %dx%d; use image coordinates %dx%d for cc.mouse_move/cc.mouse_click/cc.mouse_drag", kind, deskW, deskH, visW, visH)
}

func (s *Service) rememberHits(nodes []UINode) {
	hits := make(map[string]uiHit, len(nodes))
	for _, n := range nodes {
		if n.ID == "" {
			continue
		}
		hits[strings.ToUpper(n.ID)] = uiHit{ID: n.ID, Name: n.Name, SX: n.X + n.W/2, SY: n.Y + n.H/2}
	}
	s.capMu.Lock()
	s.obsHits = hits
	s.capMu.Unlock()
}

func (s *Service) lookupHit(query string) (uiHit, bool) {
	query = strings.ToUpper(strings.TrimSpace(query))
	s.capMu.Lock()
	defer s.capMu.Unlock()
	hit, ok := s.obsHits[query]
	return hit, ok
}

func validNodeID(id string) bool {
	id = strings.ToUpper(strings.TrimSpace(id))
	if len(id) < 2 || len(id) > 8 {
		return false
	}
	if id[0] < 'A' || id[0] > 'Z' {
		return false
	}
	for _, c := range id[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func nodeNames(nodes []UINode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if strings.TrimSpace(n.Name) != "" {
			out = append(out, n.Name)
		}
	}
	return out
}

func (s *Service) sensitiveForeground(buttons []string) string {
	if s.host == nil {
		return ""
	}
	title, process, err := s.host.ActiveWindow()
	if err != nil {
		return ""
	}
	return SensitiveSurfaceReason(title, process, "", buttons)
}

func (s *Service) resolveNamedTarget(query string) (invokeName string, sx, sy int, hit string, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, 0, "", fmt.Errorf("%w: empty UI target", ErrCcInputFiltered)
	}
	if reason := s.sensitiveForeground([]string{query}); reason != "" {
		return "", 0, 0, "", fmt.Errorf("%w: %s", ErrCcRiskBlocked, reason)
	}
	if validNodeID(query) {
		if h, ok := s.lookupHit(query); ok {
			name := strings.TrimSpace(h.Name)
			if name == "" {
				return "", 0, 0, "", fmt.Errorf("%w: no UI node matching %q", ErrCcInputFiltered, query)
			}
			return name, h.SX, h.SY, h.Name, nil
		}
	}
	if h, ok := s.lookupHit(query); ok && strings.TrimSpace(h.Name) != "" {
		return h.Name, h.SX, h.SY, h.Name, nil
	}
	nodes, err := s.host.ObserveUI(80)
	if err != nil {
		return "", 0, 0, "", err
	}
	if reason := s.sensitiveForeground(nodeNames(nodes)); reason != "" {
		return "", 0, 0, "", fmt.Errorf("%w: %s", ErrCcRiskBlocked, reason)
	}
	want := strings.ToLower(query)
	var best *UINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		got := strings.ToLower(strings.TrimSpace(n.Name))
		score := 0
		switch {
		case strings.EqualFold(n.ID, query):
			score = 120
		case got == want:
			score = 100
		case strings.Contains(got, want) || strings.Contains(want, got):
			score = 50
		}
		if score > bestScore {
			bestScore = score
			best = n
		}
	}
	if best == nil || bestScore == 0 {
		return "", 0, 0, "", fmt.Errorf("%w: no UI node matching %q", ErrCcInputFiltered, query)
	}
	name := strings.TrimSpace(best.Name)
	if name == "" {
		return "", 0, 0, "", fmt.Errorf("%w: no UI node matching %q", ErrCcInputFiltered, query)
	}
	return name, best.X + best.W/2, best.Y + best.H/2, name, nil
}

func (s *Service) mapUINodes(nodes []UINode) []UINode {
	ox, oy, vw, vh, dw, dh, _ := s.captureSpace()
	out := make([]UINode, 0, len(nodes))
	for _, n := range nodes {
		n.X, n.Y, n.W, n.H = ProjectRect(n.X, n.Y, n.W, n.H, ox, oy, vw, vh, dw, dh)
		out = append(out, n)
	}
	return out
}

// runHost dispatches one validated call onto the OS host.
func (s *Service) runHost(tool string, args json.RawMessage, shortcut []string) (string, []byte, error) {
	switch tool {
	case ToolMouseMove:
		var a struct {
			X int `json:"x"`
			Y int `json:"y"`
		}
		_ = json.Unmarshal(args, &a)
		sx, sy := s.toScreen(a.X, a.Y)
		if err := s.host.MouseMove(sx, sy); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("moved cursor to screen (%d,%d)", sx, sy), nil, nil
	case ToolMouseClick:
		var a struct {
			Button     string `json:"button"`
			Clicks     int    `json:"clicks"`
			X          *int   `json:"x"`
			Y          *int   `json:"y"`
			Scroll     int    `json:"scroll"`
			ScrollAxis string `json:"scrollAxis"`
			Name       string `json:"name"`
			ID         string `json:"id"`
		}
		_ = json.Unmarshal(args, &a)
		if a.Button == "" {
			a.Button = "left"
		}
		if a.Clicks < 1 {
			a.Clicks = 1
		}
		if name := strings.TrimSpace(a.Name); name != "" || strings.TrimSpace(a.ID) != "" {
			query := strings.TrimSpace(a.ID)
			if query == "" {
				query = name
			}
			invokeName, sx, sy, hit, err := s.resolveNamedTarget(query)
			if err != nil {
				return "", nil, err
			}
			if err := s.host.InvokeUI(invokeName); err == nil {
				return s.verifyAfter(fmt.Sprintf("invoked %q via accessibility", hit))
			}
			if err := s.host.MouseMove(sx, sy); err != nil {
				return "", nil, err
			}
			time.Sleep(25 * time.Millisecond)
			if err := s.host.MouseClick(a.Button, a.Clicks); err != nil {
				return "", nil, err
			}
			return s.verifyAfter(fmt.Sprintf("clicked %s %q at screen (%d,%d)", a.Button, hit, sx, sy))
		}
		if a.X != nil && a.Y != nil {
			sx, sy := s.toScreen(*a.X, *a.Y)
			if err := s.host.MouseMove(sx, sy); err != nil {
				return "", nil, err
			}
			time.Sleep(25 * time.Millisecond)
		}
		if a.Scroll != 0 {
			var err error
			if a.ScrollAxis == "horizontal" {
				err = s.host.MouseScrollH(a.Scroll)
			} else {
				err = s.host.MouseScroll(a.Scroll)
			}
			if err != nil {
				return "", nil, err
			}
			axis := a.ScrollAxis
			if axis == "" {
				axis = "vertical"
			}
			return s.verifyAfter(fmt.Sprintf("scrolled %s %d notch(es)", axis, a.Scroll))
		}
		if err := s.host.MouseClick(a.Button, a.Clicks); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("clicked %s mouse %d time(s)", a.Button, a.Clicks))
	case ToolMouseDrag:
		var a struct {
			X1 int `json:"x1"`
			Y1 int `json:"y1"`
			X2 int `json:"x2"`
			Y2 int `json:"y2"`
		}
		_ = json.Unmarshal(args, &a)
		sx1, sy1 := s.toScreen(a.X1, a.Y1)
		sx2, sy2 := s.toScreen(a.X2, a.Y2)
		if err := s.host.MouseDrag(sx1, sy1, sx2, sy2); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("dragged from (%d,%d) to (%d,%d)", sx1, sy1, sx2, sy2))
	case ToolKeyboardType:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		time.Sleep(40 * time.Millisecond)
		if err := s.host.KeyboardType(a.Text); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("typed %d character(s)", len([]rune(a.Text))))
	case ToolKeyboardShortcut:
		var a struct {
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		time.Sleep(40 * time.Millisecond)
		if err := s.host.KeyboardShortcut(shortcut); err != nil {
			return "", nil, err
		}
		return s.verifyAfter("pressed " + strings.Join(shortcut, "+"))
	case ToolScreenCapture:
		var a struct {
			Target string `json:"target"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal(args, &a)
		target := strings.TrimSpace(a.Target)
		if target == "" {
			target = "desktop"
		}
		if target == "desktop" && strings.TrimSpace(a.Title) == "" {
			png, err := s.captureDesktop()
			if err != nil {
				return "", nil, err
			}
			return s.captureSummary(png, "desktop"), png, nil
		}
		query := strings.TrimSpace(a.Title)
		if target == "foreground" {
			query = "foreground"
		}
		png, ox, oy, err := s.host.WindowCapture(query)
		if err != nil {
			return "", nil, err
		}
		s.rememberCapture(png, ox, oy)
		kind := "window"
		if target == "foreground" {
			kind = "foreground window"
		}
		return s.captureSummary(png, kind), png, nil
	case ToolGetActiveWindow:
		title, process, err := s.host.ActiveWindow()
		if err != nil {
			return "", nil, err
		}
		s.noteForeground(title, process)
		cx, cy, _ := s.host.CursorPosition()
		s.capMu.Lock()
		ox, oy, vw, vh, dw, dh := s.capOriginX, s.capOriginY, s.capVisW, s.capVisH, s.capDeskW, s.capDeskH
		hasCap := dw > 0 || vw > 0
		s.capMu.Unlock()
		if hasCap {
			ix, iy := MapScreenToVision(cx, cy, ox, oy, vw, vh, dw, dh)
			return fmt.Sprintf("active window: %s (process: %s); cursor screen (%d,%d) image (%d,%d)", title, process, cx, cy, ix, iy), nil, nil
		}
		return fmt.Sprintf("active window: %s (process: %s); cursor screen (%d,%d)", title, process, cx, cy), nil, nil
	case ToolWindowList:
		wins, err := s.host.ListWindows()
		if err != nil {
			return "", nil, err
		}
		if wins == nil {
			wins = []WindowInfo{}
		}
		mapped, space := s.mapWindows(wins)
		raw, err := json.Marshal(map[string]any{"count": len(mapped), "space": space, "windows": mapped})
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, nil
	case ToolWindowFocus:
		var a struct {
			Title   string `json:"title"`
			Process string `json:"process"`
		}
		_ = json.Unmarshal(args, &a)
		info, err := s.host.FocusWindow(windowFocusQuery(a.Title, a.Process))
		if err != nil {
			return "", nil, err
		}
		s.noteForeground(info.Title, info.Process)
		return s.verifyAfter(fmt.Sprintf("focused window %q (process: %s, id: %s)", info.Title, info.Process, info.ID))
	case ToolObserveDialog:
		var a struct {
			WaitMs int `json:"waitMs"`
		}
		_ = json.Unmarshal(args, &a)
		if a.WaitMs > 0 {
			time.Sleep(time.Duration(a.WaitMs) * time.Millisecond)
		}
		snaps, err := s.host.ObserveDialogs()
		if err != nil {
			return "", nil, err
		}
		if snaps == nil {
			snaps = []DialogSnapshot{}
		}
		mapped := s.mapDialogs(snaps)
		_, _, _, _, _, _, space := s.captureSpace()
		raw, err := json.Marshal(map[string]any{"count": len(mapped), "space": space, "dialogs": mapped})
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, nil
	case ToolConfirmDialog:
		var a struct {
			Button string `json:"button"`
		}
		_ = json.Unmarshal(args, &a)
		snap, err := s.host.ConfirmDialog(strings.TrimSpace(a.Button))
		if err != nil {
			return "", nil, err
		}
		caption := ConfirmButtonName(snap.Buttons, a.Button)
		if caption == "" {
			caption = "confirm"
		}
		return s.verifyAfter(formatDialogSummary("clicked "+caption+" on", snap))
	case ToolObserveUI:
		var a struct {
			MaxNodes int `json:"maxNodes"`
		}
		_ = json.Unmarshal(args, &a)
		if a.MaxNodes <= 0 {
			a.MaxNodes = 40
		}
		png, err := s.captureDesktop()
		if err != nil {
			return "", nil, err
		}
		if reason := s.sensitiveForeground(nil); reason != "" {
			raw, err := json.Marshal(map[string]any{"count": 0, "nodes": []UINode{}, "refused": reason, "space": "image"})
			if err != nil {
				return "", nil, err
			}
			return string(raw), png, nil
		}
		nodes, err := s.host.ObserveUI(a.MaxNodes)
		if err != nil {
			return "", nil, err
		}
		if len(nodes) > a.MaxNodes {
			nodes = nodes[:a.MaxNodes]
		}
		if reason := s.sensitiveForeground(nodeNames(nodes)); reason != "" {
			raw, err := json.Marshal(map[string]any{"count": 0, "nodes": []UINode{}, "refused": reason, "space": "image"})
			if err != nil {
				return "", nil, err
			}
			return string(raw), png, nil
		}
		nodes = assignNodeIDs(nodes)
		s.rememberHits(nodes)
		mapped := s.mapUINodes(nodes)
		s.capMu.Lock()
		vw, vh := s.capVisW, s.capVisH
		s.capMu.Unlock()
		if annotated, aerr := AnnotateCapture(png, mapped, vw, vh); aerr == nil && len(annotated) > 0 {
			png = annotated
			ox, oy := s.host.ScreenOrigin()
			s.rememberCapture(png, ox, oy)
		}
		raw, err := json.Marshal(map[string]any{"count": len(mapped), "space": "image", "nodes": mapped})
		if err != nil {
			return "", nil, err
		}
		return string(raw), png, nil
	case ToolWait:
		var a struct {
			Ms    *int   `json:"ms"`
			Until string `json:"until"`
		}
		_ = json.Unmarshal(args, &a)
		ms := 400
		if a.Ms != nil {
			ms = *a.Ms
		}
		if a.Until == "change" {
			before, err := s.host.ScreenCapture()
			if err != nil {
				return "", nil, err
			}
			beforeHash := sha256.Sum256(before)
			deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
			for {
				remain := time.Until(deadline)
				if remain <= 0 {
					return fmt.Sprintf("waited %dms; screen unchanged", ms), nil, nil
				}
				slice := 200 * time.Millisecond
				if remain < slice {
					slice = remain
				}
				time.Sleep(slice)
				cur, err := s.host.ScreenCapture()
				if err != nil {
					continue
				}
				if sha256.Sum256(cur) != beforeHash {
					ox, oy := s.host.ScreenOrigin()
					s.rememberCapture(cur, ox, oy)
					return s.captureSummary(cur, "desktop after wait"), cur, nil
				}
			}
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return fmt.Sprintf("waited %dms", ms), nil, nil
	case ToolClipboard:
		var a struct {
			Op   string `json:"op"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(args, &a)
		if a.Op == "get" {
			text, err := s.host.ClipboardGet()
			if err != nil {
				return "", nil, err
			}
			text = clampClipboard(text)
			raw, err := json.Marshal(map[string]any{"text": text, "runes": utf8.RuneCountInString(text)})
			if err != nil {
				return "", nil, err
			}
			return string(raw), nil, nil
		}
		if err := s.host.ClipboardSet(a.Text); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("clipboard set (%d character(s))", utf8.RuneCountInString(a.Text)), nil, nil
	case ToolWindowAction:
		var a struct {
			Title string `json:"title"`
			Op    string `json:"op"`
			X     int    `json:"x"`
			Y     int    `json:"y"`
			W     int    `json:"w"`
			H     int    `json:"h"`
		}
		_ = json.Unmarshal(args, &a)
		query := strings.TrimSpace(a.Title)
		if query == "" {
			query = "foreground"
		}
		info, err := s.host.WindowAction(query, strings.ToLower(strings.TrimSpace(a.Op)), a.X, a.Y, a.W, a.H)
		if err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("window %s %q (process: %s, id: %s)", a.Op, info.Title, info.Process, info.ID))
	case ToolAppList:
		wins, err := s.host.ListWindows()
		if err != nil {
			return "", nil, err
		}
		type appRow struct {
			Name       string `json:"name"`
			Windows    int    `json:"windows"`
			Foreground bool   `json:"foreground,omitempty"`
		}
		seen := map[string]int{}
		order := make([]string, 0)
		fg := map[string]bool{}
		for _, w := range wins {
			name := strings.TrimSpace(w.Process)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; !ok {
				order = append(order, name)
			}
			seen[key]++
			if w.Foreground {
				fg[key] = true
			}
		}
		apps := make([]appRow, 0, len(order))
		for _, name := range order {
			key := strings.ToLower(name)
			apps = append(apps, appRow{Name: name, Windows: seen[key], Foreground: fg[key]})
		}
		raw, err := json.Marshal(map[string]any{"count": len(apps), "apps": apps})
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, nil
	case ToolAppQuit:
		var a struct {
			Title string `json:"title"`
			Name  string `json:"name"`
		}
		_ = json.Unmarshal(args, &a)
		query := strings.TrimSpace(a.Title)
		if query == "" {
			query = strings.TrimSpace(a.Name)
		}
		closed, info, err := s.host.QuitApp(query)
		if err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("quit %d window(s) matching %q (sample: %s / %s)", closed, query, info.Title, info.Process))
	case ToolPaste:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(a.Text) != "" {
			if err := s.host.ClipboardSet(a.Text); err != nil {
				return "", nil, err
			}
		}
		if err := s.host.KeyboardShortcut([]string{"ctrl", "v"}); err != nil {
			return "", nil, err
		}
		n := utf8.RuneCountInString(a.Text)
		if n == 0 {
			return s.verifyAfter("pasted clipboard")
		}
		return s.verifyAfter(fmt.Sprintf("pasted %d character(s)", n))
	case ToolPress:
		var a struct {
			Key    string `json:"key"`
			Count  int    `json:"count"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		key := strings.ToLower(strings.TrimSpace(a.Key))
		if key == "del" {
			key = "delete"
		}
		if a.Count < 1 {
			a.Count = 1
		}
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		for i := 0; i < a.Count; i++ {
			if err := s.host.KeyboardShortcut([]string{key}); err != nil {
				return "", nil, err
			}
			if i+1 < a.Count {
				time.Sleep(30 * time.Millisecond)
			}
		}
		return s.verifyAfter(fmt.Sprintf("pressed %s x%d", key, a.Count))
	case ToolMenuClick:
		var a struct {
			Path   string `json:"path"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		if err := s.host.MenuClick(strings.TrimSpace(a.Path)); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("clicked menu %q", strings.TrimSpace(a.Path)))
	case ToolSetValue:
		var a struct {
			Target string `json:"target"`
			Value  string `json:"value"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		target := strings.TrimSpace(a.Target)
		if hit, ok := s.lookupHit(target); ok && hit.Name != "" {
			target = hit.Name
		}
		if err := s.host.SetValue(target, a.Value); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("set value on %q (%d character(s))", target, utf8.RuneCountInString(a.Value)))
	}
	return "", nil, fmt.Errorf("unknown tool %q", tool)
}

// recordAudit writes one ledger row plus the mirror audit_events action
// derived from the ledger status.
func (s *Service) recordAudit(ctx context.Context, session, tool, risk, status, layer string, detail map[string]any, ts string) {
	action := "cc.operation.executed"
	switch status {
	case StatusBlocked:
		action = "cc.operation.blocked"
	case StatusDenied, StatusStopped:
		action = "cc.tool.denied"
	}
	s.writeAudit(ctx, session, tool, risk, status, layer, action, detail, ts)
}

// writeAudit persists the ledger row and the audit_events mirror on one
// transaction. Audit writes are best-effort after a rejection: a ledger
// failure never masks the original error.
func (s *Service) writeAudit(ctx context.Context, session, tool, risk, status, layer, action string, detail map[string]any, ts string) {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil || len(raw) < 2 {
		raw = []byte("{}")
	}
	if len(raw) > 4096 {
		raw = raw[:4096]
	}
	now, _ := time.Parse(time.RFC3339, ts)
	_ = s.uow.TransactCc(ctx, func(tx Tx) error {
		if err := tx.AppendCcAudit(AuditEntry{
			EntryID: ulid.Make().String(), SessionID: session, Tool: tool,
			Action: action, RiskLevel: risk, Status: status, Layer: layer,
			Detail: string(raw), CreatedAt: ts,
		}); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{
			"tool": tool, "risk": risk, "status": status,
		})
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: action,
			AggregateID: session, Actor: "agent-runtime",
			Metadata: meta, CreatedAt: now,
		})
	})
}
