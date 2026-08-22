// Package ccapp implements the M10 wave-4 computer-control service: the
// single-row security configuration (enable flag, security level, process
// blocklist, rate and confirm caps, emergency-stop latch), the append-only
// operation ledger, and the three-layer interception pipeline (intent /
// risk classification, input filtering, foreground-process monitoring)
// behind the six cc.* agent tools. Critical operations require the
// allow_critical switch plus manual confirmation; emergency stop latches
// every tool call until the operator re-runs the enable flow.
package ccapp

import (
	"context"
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
	ToolKeyboardType     = "cc.keyboard_type"
	ToolKeyboardShortcut = "cc.keyboard_shortcut"
	ToolScreenCapture    = "cc.screen_capture"
	ToolGetActiveWindow  = "cc.get_active_window"

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

	LayerIntent    = "intent"
	LayerInput     = "input-filter"
	LayerProcess   = "process-monitor"
)

// Frozen caps.
const (
	// CcMaxAuditEntries bounds one getAuditLog response.
	CcMaxAuditEntries = 200
	// CcMaxTextRunes bounds keyboard_type text.
	CcMaxTextRunes = 4096
	// CcMaxShortcutKeys bounds keyboard_shortcut combos.
	CcMaxShortcutKeys = 4
	// CcMaxBlocklistEntries / CcMaxBlocklistEntryLen bound the process
	// blocklist.
	CcMaxBlocklistEntries = 128
	CcMaxBlocklistEntryLen = 128
)

// ccTools is the frozen six-tool set.
var ccTools = map[string]bool{
	ToolMouseMove: true, ToolMouseClick: true, ToolKeyboardType: true,
	ToolKeyboardShortcut: true, ToolScreenCapture: true, ToolGetActiveWindow: true,
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
	"taskmgr.exe", "mmc.exe", "eventvwr.exe",
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
type Host interface {
	Available() bool
	ScreenSize() (width, height int)
	MouseMove(x, y int) error
	MouseClick(button string, clicks int) error
	KeyboardType(text string) error
	KeyboardShortcut(keys []string) error
	ScreenCapture() (png []byte, err error)
	ActiveWindow() (title, process string, err error)
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
	uow    UnitOfWork
	clock  Clock
	host   Host
	limit  rateWindow
	emuMu  sync.Mutex
	emuAt  time.Time
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
	case ToolMouseMove, ToolGetActiveWindow:
		return RiskLow
	case ToolMouseClick, ToolKeyboardType, ToolScreenCapture:
		return RiskMedium
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
	return tool != ToolGetActiveWindow
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

	// Layer 3: foreground-process monitoring.
	if screenAffecting(tool) {
		_, process, err := s.host.ActiveWindow()
		if err == nil && blocklistHit(settings.ProcessBlocklist, process) {
			s.recordAudit(ctx, session, tool, risk, StatusBlocked, LayerProcess, map[string]any{"process": process}, ts)
			return Outcome{}, ErrCcProcessBlocked
		}
	}

	summary, capture, execErr := s.runHost(tool, args, shortcut)
	if execErr != nil {
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
		w, h := s.host.ScreenSize()
		if w > 0 && h > 0 && (a.X < 0 || a.Y < 0 || a.X > w || a.Y > h) {
			return nil, fmt.Errorf("%w: coordinates out of bounds %d,%d", ErrCcInputFiltered, a.X, a.Y)
		}
		if a.X < 0 || a.Y < 0 || a.X > 65535 || a.Y > 65535 {
			return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolMouseClick:
		var a struct {
			Button string `json:"button"`
			Clicks int    `json:"clicks"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Button == "" {
			a.Button = "left"
		}
		if a.Button != "left" && a.Button != "right" && a.Button != "middle" {
			return nil, fmt.Errorf("%w: button %q", ErrCcInputFiltered, a.Button)
		}
		if a.Clicks < 1 {
			a.Clicks = 1
		}
		if a.Clicks > 3 {
			return nil, fmt.Errorf("%w: clicks", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolKeyboardType:
		var a struct {
			Text string `json:"text"`
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
		return nil, nil
	case ToolKeyboardShortcut:
		var a struct {
			Keys []string `json:"keys"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		return normalizeShortcut(a.Keys)
	case ToolScreenCapture, ToolGetActiveWindow:
		var a struct{}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("%w: tool", ErrCcSchema)
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
		if err := s.host.MouseMove(a.X, a.Y); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("moved cursor to (%d,%d)", a.X, a.Y), nil, nil
	case ToolMouseClick:
		var a struct {
			Button string `json:"button"`
			Clicks int    `json:"clicks"`
		}
		_ = json.Unmarshal(args, &a)
		if a.Button == "" {
			a.Button = "left"
		}
		if a.Clicks < 1 {
			a.Clicks = 1
		}
		if err := s.host.MouseClick(a.Button, a.Clicks); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("clicked %s mouse %d time(s)", a.Button, a.Clicks), nil, nil
	case ToolKeyboardType:
		var a struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.host.KeyboardType(a.Text); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("typed %d character(s)", len([]rune(a.Text))), nil, nil
	case ToolKeyboardShortcut:
		if err := s.host.KeyboardShortcut(shortcut); err != nil {
			return "", nil, err
		}
		return "pressed " + strings.Join(shortcut, "+"), nil, nil
	case ToolScreenCapture:
		png, err := s.host.ScreenCapture()
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("captured screen (%d bytes png)", len(png)), png, nil
	case ToolGetActiveWindow:
		title, process, err := s.host.ActiveWindow()
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("active window: %s (process: %s)", title, process), nil, nil
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
