package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/ccapp"
)

var errBoom = errors.New("boom")

// fakeCcHost records host calls without touching the real desktop.
type fakeCcHost struct {
	available   bool
	w, h        int
	process     string
	title       string
	moveErr     error
	moves       [][2]int
	clicks      []string
	typed       []string
	shortcuts   [][]string
	captures    int
	png         []byte
	originX     int
	originY     int
	cursorX     int
	cursorY     int
	drags       [][4]int
	windows     []ccapp.WindowInfo
	uiNodes     []ccapp.UINode
	clip        string
	focused     []string
	dialogs     []ccapp.DialogSnapshot
	confirmed   []string
	confirmErr  error
	flipCapture bool
	winX, winY  int
	actions     []string
	quits       []string
	menus       []string
	values      [][2]string
	invokes     []string
	invokeFail  map[string]error
}

func newFakeCcHost() *fakeCcHost {
	return &fakeCcHost{available: true, w: 1920, h: 1080,
		title: "Untitled - Notepad", process: "notepad.exe"}
}

func (f *fakeCcHost) Available() bool { return f.available }
func (f *fakeCcHost) ScreenSize() (int, int) {
	return f.w, f.h
}
func (f *fakeCcHost) ScreenOrigin() (int, int) { return f.originX, f.originY }
func (f *fakeCcHost) CursorPosition() (int, int, error) {
	return f.cursorX, f.cursorY, nil
}
func (f *fakeCcHost) MouseMove(x, y int) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	f.moves = append(f.moves, [2]int{x, y})
	f.cursorX, f.cursorY = x, y
	return nil
}
func (f *fakeCcHost) MouseClick(button string, clicks int) error {
	f.clicks = append(f.clicks, button)
	return nil
}
func (f *fakeCcHost) KeyboardType(text string) error {
	f.typed = append(f.typed, text)
	return nil
}
func (f *fakeCcHost) KeyboardShortcut(keys []string) error {
	f.shortcuts = append(f.shortcuts, keys)
	return nil
}
func (f *fakeCcHost) MouseScroll(notches int) error {
	return nil
}
func (f *fakeCcHost) MouseScrollH(int) error { return nil }
func (f *fakeCcHost) MouseDrag(x1, y1, x2, y2 int) error {
	f.drags = append(f.drags, [4]int{x1, y1, x2, y2})
	return nil
}
func (f *fakeCcHost) EnsureForeground() error { return nil }
func (f *fakeCcHost) ScreenCapture() ([]byte, error) {
	f.captures++
	if len(f.png) > 0 {
		out := append([]byte(nil), f.png...)
		if f.flipCapture && len(out) > 8 {
			out[8] ^= byte(f.captures)
		}
		return out, nil
	}
	return []byte{0x89, 0x50, 0x4E, 0x47}, nil
}
func (f *fakeCcHost) WindowCapture(query string) ([]byte, int, int, error) {
	raw, err := f.ScreenCapture()
	return raw, f.winX, f.winY, err
}
func (f *fakeCcHost) ActiveWindow() (string, string, error) {
	return f.title, f.process, nil
}
func (f *fakeCcHost) ListWindows() ([]ccapp.WindowInfo, error) {
	if f.windows == nil {
		return []ccapp.WindowInfo{{
			ID: "0x1", Title: f.title, Process: f.process, Foreground: true,
			W: f.w, H: f.h,
		}}, nil
	}
	return append([]ccapp.WindowInfo(nil), f.windows...), nil
}
func (f *fakeCcHost) FocusWindow(query string) (ccapp.WindowInfo, error) {
	f.focused = append(f.focused, query)
	wins, _ := f.ListWindows()
	if info, ok := ccapp.MatchWindow(wins, query); ok {
		f.title, f.process = info.Title, info.Process
		info.Foreground = true
		for i := range f.windows {
			f.windows[i].Foreground = f.windows[i].ID != "" && f.windows[i].ID == info.ID
			if !f.windows[i].Foreground && info.ID == "" {
				f.windows[i].Foreground = f.windows[i].Title == info.Title && f.windows[i].Process == info.Process
			}
		}
		return info, nil
	}
	if len(wins) == 0 {
		return ccapp.WindowInfo{}, errors.New("no window")
	}
	return wins[0], nil
}
func (f *fakeCcHost) ObserveUI(maxNodes int) ([]ccapp.UINode, error) {
	return append([]ccapp.UINode(nil), f.uiNodes...), nil
}
func (f *fakeCcHost) ClipboardGet() (string, error) { return f.clip, nil }
func (f *fakeCcHost) ClipboardSet(text string) error {
	f.clip = text
	return nil
}
func (f *fakeCcHost) WindowAction(query, op string, x, y, w, h int) (ccapp.WindowInfo, error) {
	f.actions = append(f.actions, op+":"+query)
	wins, _ := f.ListWindows()
	if len(wins) == 0 {
		return ccapp.WindowInfo{}, errors.New("no window")
	}
	if info, ok := ccapp.MatchWindow(wins, query); ok {
		return info, nil
	}
	return wins[0], nil
}
func (f *fakeCcHost) QuitApp(query string) (int, ccapp.WindowInfo, error) {
	f.quits = append(f.quits, query)
	wins, _ := f.ListWindows()
	hits := ccapp.MatchWindows(wins, query)
	if len(hits) == 0 {
		if info, ok := ccapp.MatchWindow(wins, query); ok {
			hits = []ccapp.WindowInfo{info}
		}
	}
	if len(hits) == 0 && len(wins) > 0 {
		hits = []ccapp.WindowInfo{wins[0]}
	}
	if len(hits) == 0 {
		return 0, ccapp.WindowInfo{}, errors.New("no window")
	}
	return len(hits), hits[0], nil
}
func (f *fakeCcHost) MenuClick(path string) error {
	f.menus = append(f.menus, path)
	return nil
}
func (f *fakeCcHost) SetValue(target, value string) error {
	f.values = append(f.values, [2]string{target, value})
	return nil
}
func (f *fakeCcHost) InvokeUI(target string) error {
	f.invokes = append(f.invokes, target)
	if f.invokeFail != nil {
		if err := f.invokeFail[target]; err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeCcHost) ObserveDialogs() ([]ccapp.DialogSnapshot, error) {
	return append([]ccapp.DialogSnapshot(nil), f.dialogs...), nil
}

func (f *fakeCcHost) ConfirmDialog(button string) (ccapp.DialogSnapshot, error) {
	if f.confirmErr != nil {
		return ccapp.DialogSnapshot{}, f.confirmErr
	}
	for _, d := range f.dialogs {
		if !d.Confirmable {
			continue
		}
		if ccapp.ConfirmButtonName(d.Buttons, button) == "" {
			continue
		}
		f.confirmed = append(f.confirmed, button)
		return d, nil
	}
	for _, d := range f.dialogs {
		if d.Refused != "" {
			return d, fmt.Errorf("%w: %s", ccapp.ErrCcRiskBlocked, d.Refused)
		}
	}
	return ccapp.DialogSnapshot{}, errors.New("no confirmable dialog")
}

func newCcService(t *testing.T) (*ccapp.Service, *fakeCcHost, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cc-control.db")
	store, err := OpenTemplated(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	host := newFakeCcHost()
	svc := ccapp.New(store.AgentRuntimeRepository())
	svc.SetHost(host)
	return svc, host, path
}

func enableCc(t *testing.T, svc *ccapp.Service, mutate func(*ccapp.SettingsPatch)) ccapp.Settings {
	t.Helper()
	ctx := context.Background()
	patch := ccapp.SettingsPatch{}
	enable := true
	patch.Enabled = &enable
	if mutate != nil {
		mutate(&patch)
	}
	out, err := svc.UpdateConfig(ctx, patch)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// M10-CC-012: every tool call fails closed while disabled.
func TestCcDisabledGate(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()

	cfg, err := svc.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.EmergencyStopped || cfg.SecurityLevel != ccapp.LevelStandard {
		t.Fatalf("unexpected seed: %+v", cfg)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
		[]byte(`{"x":10,"y":10}`), false); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	// denial lands in the ledger with status=denied
	entries, err := svc.GetAuditLog(ctx, 10, ccapp.StatusDenied, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 denied entry, got %d", len(entries))
	}
}

// M10-CC-001: schema failures for tools, session ids and settings patches.
func TestCcSchemaErrors(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)

	if _, err := svc.ExecuteTool(ctx, "s1", "cc.unknown", []byte(`{}`), false); !errors.Is(err, ccapp.ErrCcSchema) {
		t.Fatalf("expected ErrCcSchema for unknown tool, got %v", err)
	}
	if _, err := svc.ExecuteTool(ctx, "", ccapp.ToolMouseMove, []byte(`{}`), false); !errors.Is(err, ccapp.ErrCcSchema) {
		t.Fatalf("expected ErrCcSchema for empty session, got %v", err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove, []byte(`{"x":"NaN"}`), false); !errors.Is(err, ccapp.ErrCcSchema) {
		t.Fatalf("expected ErrCcSchema for bad args, got %v", err)
	}
	badLevel := "yolo"
	if _, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{SecurityLevel: &badLevel}); !errors.Is(err, ccapp.ErrCcSchema) {
		t.Fatalf("expected ErrCcSchema for bad level, got %v", err)
	}
	badCap := 999
	if _, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{MaxActionsPerMinute: &badCap}); !errors.Is(err, ccapp.ErrCcSchema) {
		t.Fatalf("expected ErrCcSchema for bad cap, got %v", err)
	}
	badList := []string{"a/b"}
	if _, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{ProcessBlocklist: &badList}); !errors.Is(err, ccapp.ErrCcSchema) {
		t.Fatalf("expected ErrCcSchema for bad blocklist, got %v", err)
	}
	if _, err := svc.GetAuditLog(ctx, 10, "bogus", ""); !errors.Is(err, ccapp.ErrCcSchema) {
		t.Fatalf("expected ErrCcSchema for bad status filter, got %v", err)
	}
}

// M10-CC-005 / M10-CC-002: emergency latch blocks tools and config edits
// until the enable flow re-runs.
func TestCcEmergencyStop(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)

	stopped, err := svc.EmergencyStop(ctx, "operator", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.EmergencyStopped || stopped.EmergencyStoppedAt == "" {
		t.Fatalf("unexpected latch state: %+v", stopped)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
		[]byte(`{"x":1,"y":1}`), false); !errors.Is(err, ccapp.ErrCcEmergency) {
		t.Fatalf("expected ErrCcEmergency, got %v", err)
	}
	// non-enable patch with the latch armed is refused (M10-CC-002)
	level := ccapp.LevelStrict
	if _, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{SecurityLevel: &level}); !errors.Is(err, ccapp.ErrCcState) {
		t.Fatalf("expected ErrCcState, got %v", err)
	}
	// stopped entries use status=stopped
	entries, err := svc.GetAuditLog(ctx, 10, ccapp.StatusStopped, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 stopped entry, got %d", len(entries))
	}
	// re-running the enable flow clears the latch
	recovered := enableCc(t, svc, nil)
	if recovered.EmergencyStopped {
		t.Fatalf("latch should be cleared: %+v", recovered)
	}
}

// M10-CC-004 / M10-CC-007: risk classification with the confirmation gate.
func TestCcRiskAndConfirmation(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()

	// keyboard_shortcut is high risk: unapproved fails, approved executes
	enableCc(t, svc, nil)
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardShortcut,
		[]byte(`{"keys":["ctrl","s"]}`), false); !errors.Is(err, ccapp.ErrCcConfirmRequired) {
		t.Fatalf("expected ErrCcConfirmRequired, got %v", err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardShortcut,
		[]byte(`{"keys":["ctrl","s"]}`), true); err != nil {
		t.Fatalf("approved shortcut should execute: %v", err)
	}

	// strict level blocks high risk outright (M10-CC-004)
	strict := ccapp.LevelStrict
	if _, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{SecurityLevel: &strict}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardShortcut,
		[]byte(`{"keys":["ctrl","s"]}`), true); !errors.Is(err, ccapp.ErrCcRiskBlocked) {
		t.Fatalf("expected ErrCcRiskBlocked under strict, got %v", err)
	}

	// critical combos need allow_critical plus approval
	standard := ccapp.LevelStandard
	if _, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{SecurityLevel: &standard}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardShortcut,
		[]byte(`{"keys":["alt","f4"]}`), true); !errors.Is(err, ccapp.ErrCcRiskBlocked) {
		t.Fatalf("expected ErrCcRiskBlocked for critical without allow, got %v", err)
	}
	allow := true
	if _, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{AllowCritical: &allow}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardShortcut,
		[]byte(`{"keys":["alt","f4"]}`), false); !errors.Is(err, ccapp.ErrCcConfirmRequired) {
		t.Fatalf("expected ErrCcConfirmRequired for critical, got %v", err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardShortcut,
		[]byte(`{"keys":["alt","f4"]}`), true); err != nil {
		t.Fatalf("approved critical should execute: %v", err)
	}
}

// M10-CC-008: input filter rejects out-of-range coordinates, unknown keys
// and forbidden combos.
func TestCcInputFilter(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()
	allow := true
	enableCc(t, svc, func(p *ccapp.SettingsPatch) { p.AllowCritical = &allow })

	cases := []struct {
		tool string
		args string
	}{
		{ccapp.ToolMouseMove, `{"x":-5,"y":10}`},
		{ccapp.ToolMouseClick, `{"button":"both","clicks":1}`},
		{ccapp.ToolMouseClick, `{"button":"left","clicks":9}`},
		{ccapp.ToolKeyboardType, `{"text":""}`},
		{ccapp.ToolKeyboardType, "{\"text\":\"\\u0001\"}"},
		{ccapp.ToolKeyboardShortcut, `{"keys":["ctrl","meta"]}`},
		{ccapp.ToolKeyboardShortcut, `{"keys":["ctrl"]}`},
		{ccapp.ToolKeyboardShortcut, `{"keys":["alt","ctrl","delete"]}`},
		{ccapp.ToolWait, `{"ms":8001}`},
		{ccapp.ToolMouseDrag, `{"x1":-1,"y1":0,"x2":1,"y2":1}`},
	}
	for _, tc := range cases {
		if _, err := svc.ExecuteTool(ctx, "s1", tc.tool, []byte(tc.args), true); !errors.Is(err, ccapp.ErrCcInputFiltered) {
			t.Fatalf("%s %s: expected ErrCcInputFiltered, got %v", tc.tool, tc.args, err)
		}
	}
	blocked, err := svc.GetAuditLog(ctx, 50, ccapp.StatusBlocked, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != len(cases) {
		t.Fatalf("expected %d blocked entries, got %d", len(cases), len(blocked))
	}
	for _, e := range blocked {
		if e.Layer != ccapp.LayerInput {
			t.Fatalf("expected input-filter layer, got %q", e.Layer)
		}
	}
}

// M10-CC-009: foreground blocklist processes are protected.
func TestCcProcessBlocked(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)

	host.process = "powershell.exe"
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
		[]byte(`{"x":5,"y":5}`), true); !errors.Is(err, ccapp.ErrCcProcessBlocked) {
		t.Fatalf("expected ErrCcProcessBlocked, got %v", err)
	}
	entries, err := svc.GetAuditLog(ctx, 10, ccapp.StatusBlocked, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Layer != ccapp.LayerProcess {
		t.Fatalf("unexpected process-monitor entry: %+v", entries)
	}

	// get_active_window never touches the desktop: not gated
	host.process = "cmd.exe"
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolGetActiveWindow, []byte(`{}`), false); err != nil {
		t.Fatalf("get_active_window should bypass the process gate: %v", err)
	}
}

// M10-CC-010: an unavailable host fails closed.
func TestCcEngineUnavailable(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.available = false

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
		[]byte(`{"x":1,"y":1}`), false); !errors.Is(err, ccapp.ErrCcEngineUnavailable) {
		t.Fatalf("expected ErrCcEngineUnavailable, got %v", err)
	}
}

// M10-CC-011: engine failures map onto the exec-failed code.
func TestCcExecFailed(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.moveErr = errBoom

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
		[]byte(`{"x":1,"y":1}`), false); err == nil || !strings.Contains(err.Error(), "operation failed") {
		t.Fatalf("expected exec failure, got %v", err)
	}
	failed, err := svc.GetAuditLog(ctx, 10, ccapp.StatusFailed, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed entry, got %d", len(failed))
	}
}

// M10-CC-006: the sliding window caps attempts per minute.
func TestCcRateLimited(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()
	cap := 2
	enableCc(t, svc, func(p *ccapp.SettingsPatch) { p.MaxActionsPerMinute = &cap })

	for i := 0; i < 2; i++ {
		if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
			[]byte(`{"x":1,"y":1}`), false); err != nil {
			t.Fatalf("call %d should pass: %v", i, err)
		}
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
		[]byte(`{"x":1,"y":1}`), false); !errors.Is(err, ccapp.ErrCcRateLimited) {
		t.Fatalf("expected ErrCcRateLimited, got %v", err)
	}
	denied, err := svc.GetAuditLog(ctx, 10, ccapp.StatusDenied, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(denied) != 1 {
		t.Fatalf("expected 1 rate-denied entry, got %d", len(denied))
	}
}

// M10-CC-003: the ledger is append-only (trigger aborts UPDATE/DELETE).
func TestCcAuditLedgerAppendOnly(t *testing.T) {
	svc, _, path := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)

	if _, err := svc.ExecuteTool(ctx, "append-only-1", ccapp.ToolMouseMove,
		[]byte(`{"x":1,"y":1}`), false); err != nil {
		t.Fatal(err)
	}
	entries, err := svc.GetAuditLog(ctx, 10, ccapp.StatusExecuted, "append-only-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RiskLevel != ccapp.RiskLow {
		t.Fatalf("unexpected entry: %+v", entries)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(`UPDATE cc_audit_log SET status='failed'`); err == nil ||
		!strings.Contains(err.Error(), "M10-CC-003") {
		t.Fatalf("expected M10-CC-003 on update, got %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM cc_audit_log`); err == nil ||
		!strings.Contains(err.Error(), "M10-CC-003") {
		t.Fatalf("expected M10-CC-003 on delete, got %v", err)
	}
}

// Screen capture flows the PNG artifact through the outcome.
func TestCcScreenCaptureAndWindow(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)

	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CapturePNG) == 0 || host.captures != 1 {
		t.Fatalf("unexpected capture: %d bytes, %d calls", len(out.CapturePNG), host.captures)
	}

	win, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolGetActiveWindow, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(win.Summary, "notepad.exe") {
		t.Fatalf("unexpected window summary: %s", win.Summary)
	}

	// audit mirrors the confirmed action for approved medium-risk calls
	entries, err := svc.GetAuditLog(ctx, 20, "", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if !strings.Contains(out.Summary, "desktop") {
		t.Fatalf("capture should name the desktop: %s", out.Summary)
	}
}

func TestCcMouseMoveMapsVisionPixelsToDesktop(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 2400, 1600)

	cap, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true)
	if err != nil {
		t.Fatal(err)
	}
	var deskW, deskH, visW, visH int
	if _, err := fmt.Sscanf(cap.Summary, "captured desktop %dx%d; use image coordinates %dx%d", &deskW, &deskH, &visW, &visH); err != nil || deskW != 2400 || deskH != 1600 || visW <= 0 || visH <= 0 {
		t.Fatalf("capture summary %q", cap.Summary)
	}
	vx, vy := visW/2, visH/2
	wantX, wantY := ccapp.MapCapturePoint(vx, vy, visW, visH, deskW, deskH)
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove,
		[]byte(fmt.Sprintf(`{"x":%d,"y":%d}`, vx, vy)), false); err != nil {
		t.Fatal(err)
	}
	if len(host.moves) != 1 || host.moves[0] != [2]int{wantX, wantY} {
		t.Fatalf("move = %v want [%d %d] (vision %d,%d of %dx%d → desktop %dx%d)",
			host.moves, wantX, wantY, vx, vy, visW, visH, deskW, deskH)
	}
}

func TestCcMouseDragMapsVisionPixelsToDesktop(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.originX, host.originY = -1920, 0
	host.png = encodeTestPNG(t, 2400, 1600)

	cap, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true)
	if err != nil {
		t.Fatal(err)
	}
	var deskW, deskH, visW, visH int
	if _, err := fmt.Sscanf(cap.Summary, "captured desktop %dx%d; use image coordinates %dx%d", &deskW, &deskH, &visW, &visH); err != nil || visW <= 0 || visH <= 0 {
		t.Fatalf("capture summary %q", cap.Summary)
	}
	x1, y1 := visW/4, visH/4
	x2, y2 := visW/2, visH/2
	mx1, my1 := ccapp.MapCapturePoint(x1, y1, visW, visH, deskW, deskH)
	mx2, my2 := ccapp.MapCapturePoint(x2, y2, visW, visH, deskW, deskH)
	want := [4]int{host.originX + mx1, host.originY + my1, host.originX + mx2, host.originY + my2}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseDrag,
		[]byte(fmt.Sprintf(`{"x1":%d,"y1":%d,"x2":%d,"y2":%d}`, x1, y1, x2, y2)), true); err != nil {
		t.Fatal(err)
	}
	if len(host.drags) != 1 || host.drags[0] != want {
		t.Fatalf("drag = %v want %v", host.drags, want)
	}
}

func encodeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 80, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCcObserveAndConfirmDialog(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)

	observed, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolObserveDialog, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(observed.Summary, `"count":0`) {
		t.Fatalf("empty observe = %s", observed.Summary)
	}

	host.dialogs = []ccapp.DialogSnapshot{{
		Title: "要保存更改吗？", Process: "notepad.exe", Class: "#32770",
		Buttons: []string{"确定", "取消"}, Confirmable: true,
	}}
	clicked, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolConfirmDialog, []byte(`{"button":"ok"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clicked.Summary, "确定") || len(host.confirmed) != 1 {
		t.Fatalf("confirm outcome %q confirmed=%v", clicked.Summary, host.confirmed)
	}

	host.dialogs = []ccapp.DialogSnapshot{{
		Title: "用户账户控制", Process: "consent.exe",
		Buttons: []string{"是", "否"}, Confirmable: false, Refused: "uac dialog",
	}}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolConfirmDialog, []byte(`{}`), true); !errors.Is(err, ccapp.ErrCcRiskBlocked) {
		t.Fatalf("UAC confirm should be blocked, got %v", err)
	}

	entries, err := svc.GetAuditLog(ctx, 20, "", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected observe+confirm+blocked UAC in ledger, got %d", len(entries))
	}
	foundObserve, foundConfirm, foundUAC := false, false, false
	for _, e := range entries {
		switch {
		case e.Tool == ccapp.ToolObserveDialog && e.Status == ccapp.StatusExecuted:
			foundObserve = true
		case e.Tool == ccapp.ToolConfirmDialog && e.Status == ccapp.StatusExecuted:
			foundConfirm = true
		case e.Tool == ccapp.ToolConfirmDialog && e.Status == ccapp.StatusBlocked:
			foundUAC = true
		}
	}
	if !foundObserve || !foundConfirm || !foundUAC {
		t.Fatalf("ledger missing dialog rows: %+v", entries)
	}
}

func TestCcOpenClawParityTools(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 320, 200)
	host.uiNodes = []ccapp.UINode{{Role: "button", Name: "保存", X: 40, Y: 80, W: 60, H: 24}}
	host.clip = "hello"

	listed, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowList, []byte(`{}`), false)
	if err != nil || !strings.Contains(listed.Summary, "notepad.exe") {
		t.Fatalf("window list: %v %s", err, listed.Summary)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowFocus, []byte(`{"title":"Notepad"}`), true); err != nil {
		t.Fatal(err)
	}
	if len(host.focused) != 1 {
		t.Fatalf("focus calls = %v", host.focused)
	}

	ui, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolObserveUI, []byte(`{}`), false)
	if err != nil || !strings.Contains(ui.Summary, `"role":"button"`) {
		t.Fatalf("observe ui: %v %s", err, ui.Summary)
	}

	clip, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolClipboard, []byte(`{"op":"get"}`), true)
	if err != nil || !strings.Contains(clip.Summary, "hello") {
		t.Fatalf("clipboard get: %v %s", err, clip.Summary)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolClipboard, []byte(`{"op":"set","text":"月汐"}`), true); err != nil {
		t.Fatal(err)
	}
	if host.clip != "月汐" {
		t.Fatalf("clipboard set = %q", host.clip)
	}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseDrag, []byte(`{"x1":10,"y1":10,"x2":40,"y2":40}`), true); err != nil {
		t.Fatal(err)
	}
	if len(host.drags) != 1 {
		t.Fatalf("drags = %v", host.drags)
	}

	waited, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWait, []byte(`{"ms":1,"until":"timeout"}`), false)
	if err != nil || !strings.Contains(waited.Summary, "waited") {
		t.Fatalf("wait: %v %s", err, waited.Summary)
	}

	named, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseClick, []byte(`{"name":"保存"}`), true)
	if err != nil || !strings.Contains(named.Summary, "保存") {
		t.Fatalf("named click: %v %s", err, named.Summary)
	}
	if len(host.invokes) != 1 || host.invokes[0] != "保存" {
		t.Fatalf("named click should invoke accessibility, invokes=%v", host.invokes)
	}
	if len(host.moves) != 0 || len(host.clicks) != 0 {
		t.Fatalf("named invoke should not center-click, moves=%v clicks=%v", host.moves, host.clicks)
	}
}

func TestCcNamedClickFallsBackToCenterWhenInvokeFails(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 320, 200)
	host.uiNodes = []ccapp.UINode{{Role: "button", Name: "保存", X: 40, Y: 80, W: 60, H: 24}}
	host.invokeFail = map[string]error{"保存": errors.New("invoke unavailable")}

	named, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseClick, []byte(`{"name":"保存"}`), true)
	if err != nil || !strings.Contains(named.Summary, "保存") {
		t.Fatalf("named click fallback: %v %s", err, named.Summary)
	}
	if len(host.invokes) != 1 || host.invokes[0] != "保存" {
		t.Fatalf("should try invoke first, invokes=%v", host.invokes)
	}
	if len(host.moves) != 1 || host.moves[0] != [2]int{70, 92} {
		t.Fatalf("fallback should center-click, moves=%v", host.moves)
	}
	if len(host.clicks) != 1 || host.clicks[0] != "left" {
		t.Fatalf("fallback clicks=%v", host.clicks)
	}
}

func TestCcNamedClickByObserveIDInvokesAccessibilityName(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 320, 200)
	host.uiNodes = []ccapp.UINode{{Role: "button", Name: "OK", X: 10, Y: 10, W: 40, H: 20}}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolObserveUI, []byte(`{}`), false); err != nil {
		t.Fatal(err)
	}
	named, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseClick, []byte(`{"id":"B1"}`), true)
	if err != nil || !strings.Contains(named.Summary, "invoked") {
		t.Fatalf("id click: %v %s", err, named.Summary)
	}
	if len(host.invokes) != 1 || host.invokes[0] != "OK" {
		t.Fatalf("id click should invoke node name, invokes=%v", host.invokes)
	}
	if len(host.moves) != 0 {
		t.Fatalf("id invoke should not mouse-move, moves=%v", host.moves)
	}
}

func TestCcEmergencyStopBlocksNewTools(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	if _, err := svc.EmergencyStop(ctx, "tester", "halt"); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{ccapp.ToolMouseDrag, ccapp.ToolWindowList, ccapp.ToolObserveUI, ccapp.ToolClipboard, ccapp.ToolWait} {
		args := []byte(`{}`)
		switch tool {
		case ccapp.ToolMouseDrag:
			args = []byte(`{"x1":1,"y1":1,"x2":2,"y2":2}`)
		case ccapp.ToolClipboard:
			args = []byte(`{"op":"get"}`)
		case ccapp.ToolWait:
			args = []byte(`{"ms":1}`)
		}
		if _, err := svc.ExecuteTool(ctx, "s1", tool, args, true); !errors.Is(err, ccapp.ErrCcEmergency) {
			t.Fatalf("%s expected emergency, got %v", tool, err)
		}
	}
}

func TestCcClickMapsImagePixelsThroughCaptureOrigin(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.originX, host.originY = -1920, 0
	host.png = encodeTestPNG(t, 400, 300)

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseClick, []byte(`{"x":40,"y":80,"button":"left"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(host.moves) != 1 || host.moves[0] != [2]int{-1880, 80} {
		t.Fatalf("click move = %v want [-1880 80]", host.moves)
	}
	if len(host.clicks) != 1 || host.clicks[0] != "left" {
		t.Fatalf("clicks = %v", host.clicks)
	}
	if !strings.Contains(out.Summary, "clicked") {
		t.Fatalf("click summary %q", out.Summary)
	}
}

func TestCcWindowCaptureRemapsClicksIntoWindow(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 400, 300)
	host.winX, host.winY = 100, 50

	cap, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{"target":"window","title":"Notepad"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.Summary, "window") {
		t.Fatalf("window capture summary %q", cap.Summary)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove, []byte(`{"x":10,"y":20}`), false); err != nil {
		t.Fatal(err)
	}
	if len(host.moves) != 1 || host.moves[0] != [2]int{110, 70} {
		t.Fatalf("window-space move = %v want [110 70]", host.moves)
	}
}

func TestCcObserveUIProjectsBoundsIntoImageSpace(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.originX, host.originY = -1920, 0
	host.png = encodeTestPNG(t, 400, 300)
	host.uiNodes = []ccapp.UINode{{Role: "button", Name: "保存", X: -1880, Y: 80, W: 60, H: 24}}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	ui, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolObserveUI, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ui.Summary, `"x":40`) || !strings.Contains(ui.Summary, `"y":80`) {
		t.Fatalf("ui nodes should be in image pixels, got %s", ui.Summary)
	}
}

func TestCcClickVerifyReturnsNewFrameWhenPixelsChange(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 320, 200)
	host.flipCapture = true

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	before := host.captures
	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseClick, []byte(`{"button":"left"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if host.captures != before+1 {
		t.Fatalf("verify capture skipped, captures=%d", host.captures)
	}
	if len(out.CapturePNG) == 0 || !strings.Contains(out.Summary, "screen updated") {
		t.Fatalf("expected verify screenshot, summary=%q png=%d", out.Summary, len(out.CapturePNG))
	}
}

func TestCcWaitUntilChangeReturnsNewScreenshot(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 320, 200)
	host.flipCapture = true

	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWait, []byte(`{"ms":800,"until":"change"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CapturePNG) == 0 || !strings.Contains(out.Summary, "desktop after wait") {
		t.Fatalf("wait-until-change = %q png=%d", out.Summary, len(out.CapturePNG))
	}
}

func TestCcWaitMsZeroIsImmediate(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWait, []byte(`{"ms":0}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Summary != "waited 0ms" {
		t.Fatalf("wait 0 = %q", out.Summary)
	}
}

func TestCcTypeFocusesNamedWindow(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 64, 64)

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardType, []byte(`{"text":"hello","window":"Notepad"}`), true); err != nil {
		t.Fatal(err)
	}
	if len(host.focused) != 1 || host.focused[0] != "Notepad" {
		t.Fatalf("focus = %v", host.focused)
	}
	if len(host.typed) != 1 || host.typed[0] != "hello" {
		t.Fatalf("typed = %v", host.typed)
	}
}

func TestCcClickRejectsCoordinatesOutsideLastImage(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 400, 300)

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMouseMove, []byte(`{"x":500,"y":10}`), false); !errors.Is(err, ccapp.ErrCcInputFiltered) {
		t.Fatalf("expected out-of-bounds filter, got %v", err)
	}
}

func TestCcDesktopOpsTools(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 64, 64)
	host.windows = []ccapp.WindowInfo{
		{ID: "0x10", Title: "Untitled - Notepad", Process: "notepad.exe", Foreground: true, W: 400, H: 300},
		{ID: "0x11", Title: "Notes", Process: "notepad.exe", W: 200, H: 100},
	}

	listed, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolAppList, []byte(`{}`), false)
	if err != nil || !strings.Contains(listed.Summary, "notepad.exe") {
		t.Fatalf("app list: %v %s", err, listed.Summary)
	}

	restored, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowAction, []byte(`{"op":"restore","title":"Notepad"}`), false)
	if err != nil || !strings.Contains(restored.Summary, "restore") {
		t.Fatalf("restore: %v %s", err, restored.Summary)
	}
	if len(host.actions) != 1 || host.actions[0] != "restore:Notepad" {
		t.Fatalf("actions = %v", host.actions)
	}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowAction, []byte(`{"op":"close","title":"Notepad"}`), false); !errors.Is(err, ccapp.ErrCcConfirmRequired) {
		t.Fatalf("close without confirm: %v", err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowAction, []byte(`{"op":"close","title":"Notepad"}`), true); err != nil {
		t.Fatal(err)
	}

	quit, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolAppQuit, []byte(`{"name":"notepad.exe"}`), true)
	if err != nil || !strings.Contains(quit.Summary, "quit") {
		t.Fatalf("quit: %v %s", err, quit.Summary)
	}
	if len(host.quits) != 1 {
		t.Fatalf("quits = %v", host.quits)
	}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolPaste, []byte(`{"text":"月汐"}`), false); err != nil {
		t.Fatal(err)
	}
	if host.clip != "月汐" {
		t.Fatalf("paste clip = %q", host.clip)
	}
	if len(host.shortcuts) == 0 {
		t.Fatal("paste should send ctrl+v")
	}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolPress, []byte(`{"key":"enter","count":2}`), false); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolMenuClick, []byte(`{"path":"File > Save"}`), true); err != nil {
		t.Fatal(err)
	}
	if len(host.menus) != 1 || host.menus[0] != "File > Save" {
		t.Fatalf("menus = %v", host.menus)
	}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolSetValue, []byte(`{"target":"Name","value":"Ada"}`), false); err != nil {
		t.Fatal(err)
	}
	if len(host.values) != 1 || host.values[0] != [2]string{"Name", "Ada"} {
		t.Fatalf("values = %v", host.values)
	}
}

func TestCcQuitProtectedProcessBlocked(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.windows = []ccapp.WindowInfo{{
		ID: "0x1", Title: "File Explorer", Process: "explorer.exe", Foreground: true, W: 800, H: 600,
	}}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolAppQuit, []byte(`{"name":"explorer.exe"}`), true); !errors.Is(err, ccapp.ErrCcRiskBlocked) {
		t.Fatalf("expected protected block, got %v", err)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowAction, []byte(`{"op":"close","title":"File Explorer"}`), true); !errors.Is(err, ccapp.ErrCcRiskBlocked) {
		t.Fatalf("expected close protected block, got %v", err)
	}
}

func TestCcWindowListMapsBoundsIntoImageSpace(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.originX, host.originY = -1920, 0
	host.png = encodeTestPNG(t, 400, 300)
	host.windows = []ccapp.WindowInfo{{
		ID: "0x1", Title: "Notes", Process: "notepad.exe", Foreground: true,
		X: -1880, Y: 80, W: 200, H: 100,
	}}

	before, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowList, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Count   int                `json:"count"`
		Space   string             `json:"space"`
		Windows []ccapp.WindowInfo `json:"windows"`
	}
	if err := json.Unmarshal([]byte(before.Summary), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Space != "screen" || listed.Count != 1 || listed.Windows[0].X != 40 {
		t.Fatalf("pre-capture list = %s", before.Summary)
	}

	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	after, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowList, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(after.Summary), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Space != "image" || listed.Windows[0].X != 40 || listed.Windows[0].Y != 80 || listed.Windows[0].W != 200 || listed.Windows[0].H != 100 {
		t.Fatalf("post-capture list = %s", after.Summary)
	}
}

func TestCcWindowListCapsCount(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.windows = make([]ccapp.WindowInfo, ccapp.CcMaxListedWindows+8)
	for i := range host.windows {
		host.windows[i] = ccapp.WindowInfo{
			ID: fmt.Sprintf("0x%x", i+1), Title: fmt.Sprintf("W%d", i), Process: "app.exe", W: 10, H: 10,
		}
	}
	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowList, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out.Summary), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Count != ccapp.CcMaxListedWindows {
		t.Fatalf("count = %d want %d", listed.Count, ccapp.CcMaxListedWindows)
	}
}

func TestCcClipboardGetTruncatesAndSetRejectsOversize(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.clip = strings.Repeat("月", ccapp.CcMaxClipboardRunes+64)
	got, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolClipboard, []byte(`{"op":"get"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Text  string `json:"text"`
		Runes int    `json:"runes"`
	}
	if err := json.Unmarshal([]byte(got.Summary), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Runes != ccapp.CcMaxClipboardRunes || utf8.RuneCountInString(payload.Text) != ccapp.CcMaxClipboardRunes {
		t.Fatalf("get cap = runes %d text %d", payload.Runes, utf8.RuneCountInString(payload.Text))
	}
	over := `{"op":"set","text":"` + strings.Repeat("a", ccapp.CcMaxClipboardRunes+1) + `"}`
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolClipboard, []byte(over), true); !errors.Is(err, ccapp.ErrCcInputFiltered) {
		t.Fatalf("oversize set: %v", err)
	}
}

func TestCcObserveDialogMapsButtonBounds(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.originX, host.originY = -1920, 0
	host.png = encodeTestPNG(t, 400, 300)
	host.dialogs = []ccapp.DialogSnapshot{{
		Title: "要保存更改吗？", Process: "notepad.exe", Class: "#32770",
		X: -1880, Y: 80, W: 200, H: 100,
		Buttons:     []string{"确定", "取消"},
		Confirmable: true,
		Nodes: []ccapp.UINode{
			{Role: "button", Name: "确定", X: -1860, Y: 140, W: 60, H: 24},
		},
	}}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolScreenCapture, []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolObserveDialog, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Count   int `json:"count"`
		Space   string
		Dialogs []ccapp.DialogSnapshot `json:"dialogs"`
	}
	if err := json.Unmarshal([]byte(out.Summary), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 1 || payload.Dialogs[0].X != 40 || payload.Dialogs[0].Y != 80 {
		t.Fatalf("dialog bounds = %s", out.Summary)
	}
	if len(payload.Dialogs[0].Nodes) != 1 || payload.Dialogs[0].Nodes[0].Name != "确定" || payload.Dialogs[0].Nodes[0].X != 60 {
		t.Fatalf("dialog nodes = %s", out.Summary)
	}
}

func TestCcObserveUIRefusesUAC(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 64, 64)
	host.title, host.process = "用户账户控制", "consent.exe"
	host.uiNodes = []ccapp.UINode{{Role: "button", Name: "是", X: 10, Y: 10, W: 40, H: 20}}
	out, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolObserveUI, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Summary, `"refused":"uac dialog"`) || !strings.Contains(out.Summary, `"count":0`) {
		t.Fatalf("UAC observe = %s", out.Summary)
	}
}

func TestCcFocusThenTypeHitsTargetWindow(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.png = encodeTestPNG(t, 64, 64)
	host.title, host.process = "Lunitide", "lunitide.exe"
	host.windows = []ccapp.WindowInfo{
		{ID: "0xA", Title: "Lunitide", Process: "lunitide.exe", Foreground: true, W: 400, H: 300},
		{ID: "0xB", Title: "Untitled - Notepad", Process: "notepad.exe", W: 800, H: 600},
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowFocus, []byte(`{"title":"Notepad"}`), true); err != nil {
		t.Fatal(err)
	}
	if host.process != "notepad.exe" {
		t.Fatalf("focus should switch fake foreground, process=%s", host.process)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolKeyboardType, []byte(`{"text":"hello"}`), true); err != nil {
		t.Fatal(err)
	}
	if len(host.focused) < 1 || host.focused[0] != "Notepad" {
		t.Fatalf("focus calls = %v", host.focused)
	}
	if len(host.typed) != 1 || host.typed[0] != "hello" {
		t.Fatalf("typed = %v", host.typed)
	}
}

func TestCcWindowFocusByProcess(t *testing.T) {
	svc, host, _ := newCcService(t)
	ctx := context.Background()
	enableCc(t, svc, nil)
	host.windows = []ccapp.WindowInfo{
		{ID: "0xA", Title: "Lunitide", Process: "lunitide.exe", Foreground: true, W: 400, H: 300},
		{ID: "0xB", Title: "Untitled - Notepad", Process: "notepad.exe", W: 800, H: 600},
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowFocus, []byte(`{"process":"notepad"}`), true); err != nil {
		t.Fatal(err)
	}
	if len(host.focused) != 1 || host.focused[0] != "notepad" {
		t.Fatalf("focus calls = %v", host.focused)
	}
	if host.process != "notepad.exe" {
		t.Fatalf("foreground process = %q", host.process)
	}
	if _, err := svc.ExecuteTool(ctx, "s1", ccapp.ToolWindowFocus, []byte(`{}`), true); !errors.Is(err, ccapp.ErrCcInputFiltered) {
		t.Fatalf("empty focus query: %v", err)
	}
}
