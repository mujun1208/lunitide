package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
)

var errBoom = errors.New("boom")

// fakeCcHost records host calls without touching the real desktop.
type fakeCcHost struct {
	available bool
	w, h      int
	process   string
	title     string
	moveErr   error
	moves     [][2]int
	clicks    []string
	typed     []string
	shortcuts [][]string
	captures  int
	png       []byte
	dialogs   []ccapp.DialogSnapshot
	confirmed []string
	confirmErr error
}

func newFakeCcHost() *fakeCcHost {
	return &fakeCcHost{available: true, w: 1920, h: 1080,
		title: "Untitled - Notepad", process: "notepad.exe"}
}

func (f *fakeCcHost) Available() bool { return f.available }
func (f *fakeCcHost) ScreenSize() (int, int) {
	return f.w, f.h
}
func (f *fakeCcHost) MouseMove(x, y int) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	f.moves = append(f.moves, [2]int{x, y})
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
func (f *fakeCcHost) ScreenCapture() ([]byte, error) {
	f.captures++
	if len(f.png) > 0 {
		return f.png, nil
	}
	return []byte{0x89, 0x50, 0x4E, 0x47}, nil
}
func (f *fakeCcHost) ActiveWindow() (string, string, error) {
	return f.title, f.process, nil
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
