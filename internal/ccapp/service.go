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
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/providerapp"
)

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
	ArmedUntil           string   `json:"armedUntil,omitempty"`
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
	ArmMinutes           *int
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
	// HoldKey presses (down=true) or releases one portable key without
	// pairing the other edge. Click modifiers and hold_key use this;
	// callers must release, and the service auto-releases sticky holds.
	HoldKey(key string, down bool) error
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
	capMu                                sync.Mutex
	capVisW, capVisH, capDeskW, capDeskH int
	capOriginX, capOriginY               int
	capGeom                              DisplayGeometry
	capHash                              [32]byte
	capFrameID                           string
	capWide                              bool
	obsHits                              map[string]uiHit
	lastMu                               sync.Mutex
	lastTitle, lastProc                  string
	holdMu                               sync.Mutex
	heldKeys                             []string
	holdTimer                            *time.Timer
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

func (s *Service) rememberCapture(png []byte, originX, originY int, wide bool) {
	imgW, imgH, visW, visH := visionDimensions(png)
	geom := geometryForCapture(s.host, originX, originY, imgW, imgH, wide)
	s.capMu.Lock()
	deskW, deskH := imgW, imgH
	if wide && s.capDeskW > imgW && s.capDeskH > imgH {
		// Annotated / downscaled vision frame: keep the last full desktop
		// size so layer-4 clicks do not map through a 1280 thumbnail.
		deskW, deskH = s.capDeskW, s.capDeskH
		visW, visH = imgW, imgH
	} else if visW <= 0 || visH <= 0 {
		visW, visH = imgW, imgH
	}
	if imgW > 0 && imgH > 0 && (imgW < deskW || imgH < deskH) {
		visW, visH = imgW, imgH
	}
	s.capDeskW, s.capDeskH, s.capVisW, s.capVisH = deskW, deskH, visW, visH
	s.capOriginX, s.capOriginY = originX, originY
	s.capGeom = geom
	s.capWide = wide
	if len(png) > 0 {
		s.capHash = sha256.Sum256(png)
		s.capFrameID = FrameIDFromCapture(s.capHash, geom)
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
	s.rememberCapture(png, ox, oy, true)
	return png, nil
}

// CaptureDesktopPNG grabs the virtual desktop without running the cc tool
// pipeline. Agent computer-control still uses the full framebuffer.
func (s *Service) CaptureDesktopPNG() ([]byte, error) {
	return s.host.ScreenCapture()
}

// regionCaptureHost is implemented by the Windows host so people chat can
// freeze the desktop and let the operator drag a WeChat-style crop.
type regionCaptureHost interface {
	RegionCapture() ([]byte, error)
}

// CaptureRegionPNG runs an interactive region snip when the host supports it,
// otherwise it returns a full-desktop PNG (test fakes, non-Windows).
func (s *Service) CaptureRegionPNG() ([]byte, error) {
	if s == nil || s.host == nil {
		return nil, ErrCcEngineUnavailable
	}
	if r, ok := s.host.(regionCaptureHost); ok {
		return r.RegionCapture()
	}
	return s.host.ScreenCapture()
}

// verifyAfter takes a fresh desktop screenshot so the model can see the
// result of an input action (OpenClaw observe→act→verify). Unchanged pixels
// still attach the current frame and keep the same frameId — the previous
// screenshot remains valid; do not drop the image or the model will click
// the pre-action frame.
func (s *Service) verifyCapture() ([]byte, error) {
	if s == nil || s.host == nil {
		return nil, ErrCcEngineUnavailable
	}
	s.capMu.Lock()
	wide := s.capWide
	s.capMu.Unlock()
	if !wide {
		png, _, _, err := s.host.WindowCapture("foreground")
		if err == nil && len(png) > 0 {
			return png, nil
		}
	}
	return s.host.ScreenCapture()
}

func (s *Service) verifyAfter(summary string) (string, []byte, error) {
	png, err := s.verifyCapture()
	if err != nil {
		return summary, nil, fmt.Errorf("%w: verify capture failed: %v", ErrCcExecFailed, err)
	}
	sum := sha256.Sum256(png)
	s.capMu.Lock()
	prev := s.capHash
	s.capMu.Unlock()
	unchanged := prev != [32]byte{} && prev == sum
	ox, oy := s.host.ScreenOrigin()
	s.rememberCapture(png, ox, oy, true)
	id := s.CurrentFrameID()
	deskW, deskH, visW, visH := visionDimensions(png)
	if deskW == 0 {
		deskW, deskH = s.host.ScreenSize()
		visW, visH = deskW, deskH
	}
	if unchanged {
		return appendFrameID(fmt.Sprintf("%s; screen unchanged since previous frame (still current; use this image, not the pre-action frame)", summary), id), png, nil
	}
	return appendFrameID(fmt.Sprintf("%s; screen updated %dx%d (use image %dx%d)", summary, deskW, deskH, visW, visH), id), png, nil
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

// ── audit log ───────────────────────────────────────────────────────────────

// ── emergency stop ──────────────────────────────────────────────────────────

// ── tool execution pipeline ─────────────────────────────────────────────────

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
		if e != nil {
			return e
		}
		settings, e = s.expireArm(tx, settings)
		return e
	}); err != nil {
		return Outcome{}, err
	}
	emergency = settings.EmergencyStopped

	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)

	// Expand computer.act onto a cc.* name for risk/input/host. The
	// ledger records the inbound tool name (computer.act stays
	// computer.act; inner cc.* is in detail.mapped).
	execTool, execArgs := tool, args
	if tool == ToolComputerAct {
		mappedTool, mappedArgs, mapErr := MapComputerAct(args)
		if mapErr != nil {
			return Outcome{}, mapErr
		}
		execTool, execArgs = mappedTool, mappedArgs
	}
	auditTool := tool

	// Gate 0: enabled / emergency latch.
	if !settings.Enabled {
		s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusDenied, "", map[string]any{"reason": "disabled"}, ts)
		return Outcome{}, ErrCcDisabled
	}
	if emergency {
		s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusStopped, "", map[string]any{"reason": "emergency-stop"}, ts)
		return Outcome{}, ErrCcEmergency
	}
	// Rate limit: every attempted action consumes one slot.
	if !s.limit.allow(now, settings.MaxActionsPerMinute) {
		s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusDenied, "", map[string]any{"reason": "rate-limited", "cap": settings.MaxActionsPerMinute}, ts)
		return Outcome{}, ErrCcRateLimited
	}
	if s.host == nil || !s.host.Available() {
		s.recordAudit(ctx, session, auditTool, RiskMedium, StatusFailed, "", map[string]any{"reason": "engine-unavailable"}, ts)
		return Outcome{}, ErrCcEngineUnavailable
	}

	// Layer 2: input filtering (also parses the tool arguments).
	shortcut, err := s.filterInput(execTool, execArgs)
	if err != nil {
		s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusBlocked, LayerInput, map[string]any{"reason": err.Error()}, ts)
		return Outcome{}, err
	}

	// Layer 1: intent / risk classification and the confirmation gate.
	risk := classifyRisk(execTool, shortcut)
	switch risk {
	case RiskCritical:
		if !settings.AllowCritical {
			s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "critical not allowed"}, ts)
			return Outcome{}, ErrCcRiskBlocked
		}
		if !approved {
			s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "confirmation required"}, ts)
			return Outcome{}, ErrCcConfirmRequired
		}
	case RiskHigh:
		if settings.SecurityLevel == LevelStrict {
			s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "strict level"}, ts)
			return Outcome{}, ErrCcRiskBlocked
		}
		if !approved {
			s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "confirmation required"}, ts)
			return Outcome{}, ErrCcConfirmRequired
		}
	}

	// Layer 3: foreground-process monitoring, plus target-process checks
	// for window close / app quit (the victim may not be in the foreground).
	if screenAffecting(execTool) {
		title, process, err := s.host.ActiveWindow()
		if err == nil {
			s.noteForeground(title, process)
			if blocklistHit(settings.ProcessBlocklist, process) {
				s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerProcess, map[string]any{"process": process}, ts)
				return Outcome{}, ErrCcProcessBlocked
			}
		}
	}
	if err := s.rejectTargetProcess(settings, execTool, execArgs); err != nil {
		s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerProcess, map[string]any{"reason": err.Error()}, ts)
		return Outcome{}, err
	}

	summary, capture, execErr := s.runHost(execTool, execArgs, shortcut)
	if execErr != nil {
		if errors.Is(execErr, ErrCcRiskBlocked) {
			s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": execErr.Error()}, ts)
			return Outcome{}, execErr
		}
		s.recordAudit(ctx, session, auditTool, risk, StatusFailed, "", map[string]any{"reason": execErr.Error()}, ts)
		return Outcome{}, fmt.Errorf("%w: %v", ErrCcExecFailed, execErr)
	}
	action := "cc.operation.executed"
	if approved && (risk == RiskHigh || risk == RiskCritical) {
		action = "cc.operation.confirmed"
	}
	detail := map[string]any{"summary": clampReason(summary)}
	if tool == ToolComputerAct {
		detail["via"] = ToolComputerAct
		detail["mapped"] = execTool
	}
	s.writeAudit(ctx, session, auditTool, risk, StatusExecuted, "", action, detail, ts)
	out := Outcome{Tool: tool, Summary: summary, CapturePNG: capture}
	return out, nil
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
	return s.restoreNonCompanionForeground()
}

// restoreNonCompanionForeground keeps input on the last user app. If the
// companion stole focus, re-asserting the current foreground would type
// into Lunitide itself.
func (s *Service) restoreNonCompanionForeground() error {
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
	return nil
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
	id := s.CurrentFrameID()
	summary := appendFrameID(fmt.Sprintf("captured %s %dx%d; use image coordinates %dx%d for cc.mouse_move/cc.mouse_click/cc.mouse_drag", kind, deskW, deskH, visW, visH), id)
	n := hostScreenCount(s.host)
	if n < 1 {
		n = 1
	}
	s.capMu.Lock()
	idx := s.capGeom.ScreenIndex
	s.capMu.Unlock()
	return summary + fmt.Sprintf("; screens=%d; screenIndex=%d", n, idx)
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

func (s *Service) observeHideReason(buttons []string) string {
	if s.host == nil {
		return ""
	}
	title, process, err := s.host.ActiveWindow()
	if err != nil {
		return ""
	}
	return SensitiveObserveHide(title, process, "", buttons)
}

func (s *Service) filePickerHandoff(buttons []string) string {
	if s.host == nil {
		return ""
	}
	title, process, err := s.host.ActiveWindow()
	if err != nil {
		return ""
	}
	return FilePickerHandoff(title, process, "", buttons)
}

func (s *Service) resolveNamedTarget(query string) (invokeName string, sx, sy int, hit string, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, 0, "", fmt.Errorf("%w: empty UI target", ErrCcInputFiltered)
	}
	if reason := s.sensitiveForeground([]string{query}); reason != "" {
		if reason == "uac dialog" || reason == "elevation dialog" {
			return "", 0, 0, "", fmt.Errorf("%w: %s — %s", ErrCcRiskBlocked, reason, UACUserPrompt)
		}
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
		if reason == "uac dialog" || reason == "elevation dialog" {
			return "", 0, 0, "", fmt.Errorf("%w: %s — %s", ErrCcRiskBlocked, reason, UACUserPrompt)
		}
		return "", 0, 0, "", fmt.Errorf("%w: %s", ErrCcRiskBlocked, reason)
	}
	want := strings.ToLower(query)
	var best *UINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		if ChromeCloseControl(n.Name, n.Y, n.W, n.H) {
			continue
		}
		got := strings.ToLower(strings.TrimSpace(n.Name))
		score := 0
		switch {
		case strings.EqualFold(n.ID, query):
			score = 120
		case got == want || namesEquivalent(query, n.Name):
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
		if chromeCloseName(query) {
			return "", 0, 0, "", fmt.Errorf("%w: refusing window close unless the user asked to close", ErrCcRiskBlocked)
		}
		return "", 0, 0, "", fmt.Errorf("%w: no UI node matching %q", ErrCcInputFiltered, query)
	}
	name := strings.TrimSpace(best.Name)
	if name == "" {
		return "", 0, 0, "", fmt.Errorf("%w: no UI node matching %q", ErrCcInputFiltered, query)
	}
	return name, best.X + best.W/2, best.Y + best.H/2, name, nil
}

type clickHitter interface {
	HitTest(sx, sy int) (string, error)
}

type win32Clicker interface {
	Win32Click(target string) error
}

func clickNameMatch(want, got string) bool {
	return namesEquivalent(want, got)
}

func (s *Service) verifyClickHit(want string, sx, sy int) error {
	hitter, ok := s.host.(clickHitter)
	if !ok {
		return nil
	}
	got, err := hitter.HitTest(sx, sy)
	if err != nil {
		return fmt.Errorf("%w: click hit-test failed: %v", ErrCcExecFailed, err)
	}
	if !clickNameMatch(want, got) {
		return fmt.Errorf("%w: click landed on %q, wanted %q", ErrCcExecFailed, got, want)
	}
	return nil
}

func (s *Service) clickNamedLadder(invokeName string, sx, sy int, hit string) error {
	if err := s.host.InvokeUI(invokeName); err == nil {
		return s.verifyClickHit(hit, sx, sy)
	}
	if win, ok := s.host.(win32Clicker); ok {
		if err := win.Win32Click(invokeName); err == nil {
			return s.verifyClickHit(hit, sx, sy)
		}
	}
	// Layer 4: native desktop pixels (sx/sy are SetCursorPos space, not the
	// 1280 vision thumbnail) + hit-test. Missing hit-test host = fail closed.
	if sx > 0 && sy > 0 {
		if _, ok := s.host.(clickHitter); ok {
			if err := s.host.MouseMove(sx, sy); err != nil {
				return err
			}
			if err := s.host.MouseClick("left", 1); err != nil {
				return err
			}
			return s.verifyClickHit(hit, sx, sy)
		}
	}
	return fmt.Errorf("%w: control %q is not invokable via accessibility", ErrCcExecFailed, hit)
}

func (s *Service) verifyPixelClick(sx, sy int) error {
	hitter, ok := s.host.(clickHitter)
	if !ok {
		return nil
	}
	if _, err := hitter.HitTest(sx, sy); err != nil {
		return fmt.Errorf("%w: click hit-test failed: %v", ErrCcExecFailed, err)
	}
	return nil
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
