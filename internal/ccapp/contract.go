package ccapp

import (
	"errors"
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
	// ErrCaptureCanceled: the operator dismissed an interactive region snip
	// (Esc / right-click / 取消) before confirming a crop.
	ErrCaptureCanceled = errors.New("ccapp: region capture canceled")
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
	// CcMaxClickModifiers bounds ctrl/shift/alt/win held around a click.
	CcMaxClickModifiers = 3
	// CcMaxClipboardRunes bounds clipboard get/set text.
	CcMaxClipboardRunes = 8192
	// CcMaxListedWindows bounds cc.window_list.
	CcMaxListedWindows = 64
	// CcMaxObserveDialogs bounds cc.observe_dialog rows.
	CcMaxObserveDialogs = 16
	// CcMaxDialogNodes bounds clickable nodes on one observed dialog.
	CcMaxDialogNodes = 24
	// CcMaxObserveUINodes is the hard cap for cc.observe_ui (default 80).
	CcMaxObserveUINodes = 120
	// CcDefaultObserveUINodes is used when maxNodes is omitted.
	CcDefaultObserveUINodes = 80
	// CcDefaultMaxActionsPerMinute is the seeded rate cap. 30 was too
	// tight for see→act→verify (screenshot + click + verify).
	CcDefaultMaxActionsPerMinute = 60
	// CcLegacyDefaultMaxActionsPerMinute is the pre-alignment seed.
	// Companion bumps existing rows that are still sitting on it.
	CcLegacyDefaultMaxActionsPerMinute = 30
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
	ToolComputerAct: true,
}

// IsCcTool reports whether name belongs to the frozen cc tool set.
func IsCcTool(name string) bool { return ccTools[name] }

// ccMachineChangingTools are the cc tools that act on the machine rather than
// just look at it: synthetic input, clipboard, and window/app lifecycle. The
// risk ladder in this package only pauses for high/critical, so this set is
// what the caller's approval gate needs in order to stop a medium-risk click
// or keystroke from landing unannounced.
//
// Observation is deliberately absent. see→act→verify screenshots before and
// after every action, so gating a capture would put a prompt in front of
// looking. cc.mouse_move and cc.window_focus are also left out: neither can
// alter state on its own, and whatever they set up for is itself gated.
var ccMachineChangingTools = map[string]bool{
	ToolMouseClick:    true,
	ToolMouseDrag:     true,
	ToolKeyboardType:  true,
	ToolPaste:         true,
	ToolPress:         true,
	ToolMenuClick:     true,
	ToolSetValue:      true,
	ToolConfirmDialog: true,
	ToolClipboard:     true,
	ToolWindowAction:  true,
	ToolAppQuit:       true,
	// Already gated by the caller for its own reasons; listed so the set
	// stays the single answer to "does this touch the machine".
	ToolKeyboardShortcut: true,
}

// ToolChangesMachine reports whether a cc tool acts on the machine. For
// computer.act, resolve the payload with MapComputerAct first and ask about
// the mapped tool: the wrapper is observation or input depending on action.
func ToolChangesMachine(name string) bool { return ccMachineChangingTools[name] }

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
