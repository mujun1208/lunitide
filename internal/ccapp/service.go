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
	Revision             int64    `json:"revision"`
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
	ExpectedRevision     int64
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
	execution                            executionFence
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
	lastObserve                          []UINode
	observedFrameID                      string
	observedCount                        int
	allowGUIPixels                       bool
	mutateSettle                         time.Duration
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

// SetMutateSettleForTest shortens the post-mutation wait (tests).
func (s *Service) SetMutateSettleForTest(d time.Duration) { s.mutateSettle = d }

// SetAllowGUIPixels allows raw xy when no screenshot/observe frame exists.
// Production GUI fallback turns this on only around that click and must
// defer it off. Models cannot call this. After a screenshot, xy is already
// allowed (OpenClaw path) even when the UIA tree has nodes.
func (s *Service) SetAllowGUIPixels(ok bool) {
	if s == nil {
		return
	}
	s.allowGUIPixels = ok
}

// SetAllowGUIPixelsForTest is the test alias for SetAllowGUIPixels.
func (s *Service) SetAllowGUIPixelsForTest(ok bool) { s.SetAllowGUIPixels(ok) }

// ObservedSnapshot is the last successful observe frame and node count.
func (s *Service) ObservedSnapshot() (frameID string, nodeCount int) {
	if s == nil {
		return "", 0
	}
	s.capMu.Lock()
	defer s.capMu.Unlock()
	return s.observedFrameID, s.observedCount
}

// VisionSize is the last capture image size used to map normalized GUI clicks.
func (s *Service) VisionSize() (w, h int) {
	if s == nil {
		return 0, 0
	}
	s.capMu.Lock()
	defer s.capMu.Unlock()
	return s.capVisW, s.capVisH
}

// HasObservedID reports whether observe remembered this SoM id.
func (s *Service) HasObservedID(id string) bool {
	if s == nil {
		return false
	}
	_, ok := s.lookupHit(id)
	return ok
}

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

func (s *Service) mutateSettleWait() time.Duration {
	if s.mutateSettle > 0 {
		return s.mutateSettle
	}
	if s.mutateSettle < 0 {
		return 0
	}
	return 700 * time.Millisecond
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
	if unchanged {
		if wait := s.mutateSettleWait(); wait > 0 {
			if err := s.waitExecution(wait); err != nil {
				return summary, nil, err
			}
			if recap, recapErr := s.verifyCapture(); recapErr == nil {
				png = recap
				sum = sha256.Sum256(png)
				unchanged = prev != [32]byte{} && prev == sum
			}
		}
	}
	ox, oy := s.host.ScreenOrigin()
	s.rememberCapture(png, ox, oy, true)
	id := s.CurrentFrameID()
	deskW, deskH, visW, visH := visionDimensions(png)
	if deskW == 0 {
		deskW, deskH = s.host.ScreenSize()
		visW, visH = deskW, deskH
	}
	if unchanged {
		msg := appendFrameID(fmt.Sprintf("%s; screen unchanged after wait (mutation unverified)", summary), id)
		return msg, png, fmt.Errorf("%w: screen unchanged after action", ErrCcExecFailed)
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
	op, err := s.beginExecution(ctx)
	if err != nil {
		if errors.Is(err, ErrCcEmergency) {
			auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			err = errors.Join(err, s.recordAudit(auditCtx, session, tool, classifyRisk(tool, nil), StatusStopped, "", map[string]any{"reason": "emergency-stop"}, s.clock.Now().UTC().Format(time.RFC3339)))
		}
		return Outcome{}, err
	}
	defer s.endExecution(op)
	ctx = op.ctx
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
	s.execution.mu.Lock()
	op.settings = settings
	s.execution.mu.Unlock()

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
		return Outcome{}, errors.Join(ErrCcDisabled, s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusDenied, "", map[string]any{"reason": "disabled"}, ts))
	}
	if emergency {
		return Outcome{}, errors.Join(ErrCcEmergency, s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusStopped, "", map[string]any{"reason": "emergency-stop"}, ts))
	}
	// Rate limit: every attempted action consumes one slot.
	if !s.limit.allow(now, settings.MaxActionsPerMinute) {
		return Outcome{}, errors.Join(ErrCcRateLimited, s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusDenied, "", map[string]any{"reason": "rate-limited", "cap": settings.MaxActionsPerMinute}, ts))
	}
	if s.host == nil || !s.host.Available() {
		return Outcome{}, errors.Join(ErrCcEngineUnavailable, s.recordAudit(ctx, session, auditTool, RiskMedium, StatusFailed, "", map[string]any{"reason": "engine-unavailable"}, ts))
	}

	// Layer 2: input filtering (also parses the tool arguments).
	shortcut, err := s.filterInput(execTool, execArgs)
	if err != nil {
		return Outcome{}, errors.Join(err, s.recordAudit(ctx, session, auditTool, classifyRisk(execTool, nil), StatusBlocked, LayerInput, map[string]any{"reason": err.Error()}, ts))
	}

	// Layer 1: intent / risk classification and the confirmation gate.
	risk := classifyRisk(execTool, shortcut)
	switch risk {
	case RiskCritical:
		if !settings.AllowCritical {
			return Outcome{}, errors.Join(ErrCcRiskBlocked, s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "critical not allowed"}, ts))
		}
		if !approved {
			return Outcome{}, errors.Join(ErrCcConfirmRequired, s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "confirmation required"}, ts))
		}
	case RiskHigh:
		if settings.SecurityLevel == LevelStrict {
			return Outcome{}, errors.Join(ErrCcRiskBlocked, s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "strict level"}, ts))
		}
		if !approved {
			return Outcome{}, errors.Join(ErrCcConfirmRequired, s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerIntent, map[string]any{"reason": "confirmation required"}, ts))
		}
	}

	// Layer 3: foreground-process monitoring, plus target-process checks
	// for window close / app quit (the victim may not be in the foreground).
	if screenAffecting(execTool) {
		title, process, err := s.host.ActiveWindow()
		if err != nil || strings.TrimSpace(process) == "" {
			return Outcome{}, errors.Join(ErrCcProcessBlocked, s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerProcess, map[string]any{"reason": "foreground unavailable"}, ts))
		} else {
			s.noteForeground(title, process)
			if blocklistHit(settings.ProcessBlocklist, process) {
				return Outcome{}, errors.Join(ErrCcProcessBlocked, s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerProcess, map[string]any{"process": process}, ts))
			}
		}
	}
	if err := s.rejectTargetProcess(settings, execTool, execArgs); err != nil {
		return Outcome{}, errors.Join(err, s.recordAudit(ctx, session, auditTool, risk, StatusBlocked, LayerProcess, map[string]any{"reason": err.Error()}, ts))
	}

	if err := s.checkExecution(); err != nil {
		return Outcome{}, err
	}
	operationID, err := s.prepareAudit(ctx, session, auditTool, execTool, risk, execArgs, approved)
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: %v", ErrCcAuditUnavailable, err)
	}
	var summary string
	var capture []byte
	execErr := s.checkExecution()
	if execErr == nil {
		summary, capture, execErr = s.runHost(execTool, execArgs, shortcut)
	}
	if fenceErr := s.checkExecution(); fenceErr != nil {
		execErr = fenceErr
	}
	action := "cc.operation.executed"
	if approved && (risk == RiskHigh || risk == RiskCritical) {
		action = "cc.operation.confirmed"
	}
	detail := map[string]any{"summary": clampReason(summary), "operationId": operationID, "phase": "receipt", "dispatched": op.dispatched}
	if tool == ToolComputerAct {
		detail["via"] = ToolComputerAct
		detail["mapped"] = execTool
	}
	status, layer := StatusExecuted, ""
	if execErr != nil {
		detail["reason"] = clampReason(execErr.Error())
		status = StatusFailed
		if executionFenceError(execErr) {
			status, action = StatusStopped, "cc.tool.denied"
		}
		if errors.Is(execErr, ErrCcRiskBlocked) {
			status, layer, action = StatusBlocked, LayerIntent, "cc.operation.blocked"
		}
		if errors.Is(execErr, ErrCcProcessBlocked) {
			status, layer, action = StatusBlocked, LayerProcess, "cc.operation.blocked"
		}
	}
	detail["outcome"] = status
	// A cancelled caller must not erase the terminal receipt of a prepared action.
	receiptCtx, cancelReceipt := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancelReceipt()
	if auditErr := s.writeAudit(receiptCtx, session, auditTool, risk, status, layer, action, detail, s.clock.Now().UTC().Format(time.RFC3339)); auditErr != nil {
		kind := ErrCcAuditUnavailable
		if op.dispatched {
			kind = ErrCcOutcomeUnknown
		}
		return Outcome{}, fmt.Errorf("%w (operation %s): %w", kind, operationID, errors.Join(execErr, auditErr))
	}
	if execErr != nil {
		if executionFenceError(execErr) || errors.Is(execErr, ErrCcRiskBlocked) {
			return Outcome{}, execErr
		}
		return Outcome{}, fmt.Errorf("%w: %w", ErrCcExecFailed, execErr)
	}
	out := Outcome{Tool: tool, Summary: summary, CapturePNG: capture}
	return out, nil
}

func isCompanionProcess(process string) bool {
	name := processStem(process)
	return name == "lunitide" || name == "lunitide-engine"
}

func companionWindowTitle(title string) bool {
	return strings.Contains(title, "Lunitide") || strings.Contains(title, "月伴") || strings.Contains(title, "月汐")
}

func (s *Service) refuseSelfWindowPixels() error {
	if s == nil || s.host == nil {
		return nil
	}
	title, process, err := s.host.ActiveWindow()
	if err != nil {
		return nil
	}
	if isCompanionProcess(process) || companionWindowTitle(title) {
		return fmt.Errorf("%w: 前台是 Lunitide/月伴，禁止像素动作", ErrCcExecFailed)
	}
	return nil
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
		info, err := s.controlHost().FocusWindow(window)
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
			return s.controlHost().EnsureForeground()
		}
	}
	s.lastMu.Lock()
	q := strings.TrimSpace(s.lastTitle)
	if q == "" {
		q = strings.TrimSpace(s.lastProc)
	}
	s.lastMu.Unlock()
	if q != "" {
		info, err := s.controlHost().FocusWindow(q)
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
			info, err := s.controlHost().FocusWindow(query)
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
	s.lastObserve = append([]UINode(nil), nodes...)
	s.observedFrameID = s.capFrameID
	s.observedCount = len(nodes)
	s.capMu.Unlock()
}

func (s *Service) requireObserveBeforeXY() error {
	s.capMu.Lock()
	defer s.capMu.Unlock()
	if strings.TrimSpace(s.capFrameID) != "" || strings.TrimSpace(s.observedFrameID) != "" {
		return nil
	}
	if s.allowGUIPixels {
		return nil
	}
	return fmt.Errorf("%w: 先 screenshot 或 observe 再使用坐标", ErrCcInputFiltered)
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
		return "", 0, 0, "", fmt.Errorf("%w: no observed id %q (先 observe)", ErrCcInputFiltered, query)
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
	nodes = assignNodeIDs(nodes)
	want := strings.ToLower(query)
	var hits []UINode
	bestScore := 0
	for i := range nodes {
		n := nodes[i]
		if ChromeCloseControl(n.Name, n.Y, n.W, n.H) {
			continue
		}
		got := strings.ToLower(strings.TrimSpace(n.Name))
		score := 0
		switch {
		case strings.EqualFold(n.ID, query):
			score = 120
		case got == want || namesExactAlias(query, n.Name):
			score = 100
		case strings.Contains(got, want) || strings.Contains(want, got) || namesEquivalent(query, n.Name):
			score = 50
		}
		if score > bestScore {
			bestScore = score
			hits = []UINode{n}
		} else if score == bestScore && score > 0 {
			hits = append(hits, n)
		}
	}
	if bestScore == 0 || len(hits) == 0 {
		if chromeCloseName(query) {
			return "", 0, 0, "", fmt.Errorf("%w: refusing window close unless the user asked to close", ErrCcRiskBlocked)
		}
		return "", 0, 0, "", fmt.Errorf("%w: no UI node matching %q", ErrCcInputFiltered, query)
	}
	if len(hits) > 1 && unnamedUIName(hits[0].Name) && allSameUIName(hits) {
		ids := make([]string, 0, len(hits))
		for _, n := range hits {
			if n.ID != "" {
				ids = append(ids, n.ID)
			}
		}
		return "", 0, 0, "", fmt.Errorf("%w: name %q matches %d nodes (%s); use id=", ErrCcInputFiltered, query, len(hits), strings.Join(ids, "/"))
	}
	best := pickPreferredNamedHit(query, hits)
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
	// Electron/UIA often reports the wrong hit-test name. Invoke first; if
	// that fails, click the resolved box (OpenClaw). Hit-test is advisory.
	if !unnamedUIName(invokeName) {
		if err := s.controlHost().InvokeUI(invokeName); err == nil {
			return nil
		} else if wrapped := wrapHostIntegrityError(err); errors.Is(wrapped, ErrCcRiskBlocked) || executionFenceError(wrapped) {
			return wrapped
		}
	}
	if !unnamedUIName(invokeName) {
		if win, ok := s.host.(win32Clicker); ok {
			if err := s.dispatch(true, func() error { return win.Win32Click(invokeName) }); err == nil {
				return nil
			} else if executionFenceError(err) || errors.Is(err, ErrCcRiskBlocked) {
				return err
			}
		}
	}
	return s.clickNamedPixels(sx, sy)
}

func (s *Service) clickNamedPixels(sx, sy int) error {
	if s.host != nil {
		ox, oy := s.host.ScreenOrigin()
		w, h := s.host.ScreenSize()
		if w <= 0 || h <= 0 || sx < ox || sy < oy || sx >= ox+w || sy >= oy+h {
			return fmt.Errorf("%w: resolved target outside current desktop; observe again", ErrCcInputFiltered)
		}
	}
	if err := s.refuseSelfWindowPixels(); err != nil {
		return err
	}
	if err := s.controlHost().MouseMove(sx, sy); err != nil {
		return err
	}
	return s.controlHost().MouseClick("left", 1)
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
