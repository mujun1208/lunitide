// M10 wave-4 computer-control bridge tests: the configuration lifecycle
// (seed → enable → emergency stop → re-enable), the audit ledger over the
// executed tool pipeline, and the M10-CC-001/002 error family.
package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func newCcEngine(t *testing.T) (*Engine, *ccapp.Service) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m10cc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ccSvc := ccapp.New(store.AgentRuntimeRepository())
	e := NewEngine(nil, "test")
	e.SetCcControlService(ccSvc)
	return e, ccSvc
}

// fakeCcHost is the injectable control surface: a deterministic screen and
// a recorded foreground window, mirroring the User32 host contract.
type fakeCcHost struct {
	title, process string
}

func (f *fakeCcHost) Available() bool                    { return true }
func (f *fakeCcHost) ScreenSize() (int, int)             { return 1920, 1080 }
func (f *fakeCcHost) MouseMove(x, y int) error           { return nil }
func (f *fakeCcHost) MouseClick(string, int) error       { return nil }
func (f *fakeCcHost) KeyboardType(string) error          { return nil }
func (f *fakeCcHost) KeyboardShortcut([]string) error    { return nil }
func (f *fakeCcHost) MouseScroll(int) error              { return nil }
func (f *fakeCcHost) ScreenCapture() ([]byte, error)     { return nil, nil }
func (f *fakeCcHost) ActiveWindow() (string, string, error) {
	return f.title, f.process, nil
}
func (f *fakeCcHost) ObserveDialogs() ([]ccapp.DialogSnapshot, error) { return nil, nil }
func (f *fakeCcHost) ConfirmDialog(string) (ccapp.DialogSnapshot, error) {
	return ccapp.DialogSnapshot{}, nil
}

func TestCcConfigLifecycleThroughBridge(t *testing.T) {
	e, _ := newCcEngine(t)
	ctx := context.Background()

	// First read seeds and answers the disabled defaults.
	initial := e.Handle(ctx, nominationRequest("cc.getConfig", `{}`))
	if !initial.OK {
		t.Fatalf("getConfig failed: %+v", initial.Error)
	}
	var initialCfg struct {
		Enabled              bool     `json:"enabled"`
		SecurityLevel        string   `json:"securityLevel"`
		ProcessBlocklist     []string `json:"processBlocklist"`
		MaxActionsPerMinute  int      `json:"maxActionsPerMinute"`
		ConfirmTimeoutSecond int      `json:"confirmTimeoutSeconds"`
		EmergencyStopped     bool     `json:"emergencyStopped"`
	}
	if err := json.Unmarshal(mustJSON(initial.Payload), &initialCfg); err != nil {
		t.Fatal(err)
	}
	if initialCfg.Enabled || initialCfg.EmergencyStopped || initialCfg.SecurityLevel != "standard" ||
		initialCfg.MaxActionsPerMinute != 30 || initialCfg.ConfirmTimeoutSecond != 60 {
		t.Fatalf("seeded defaults invalid: %+v", initialCfg)
	}
	if len(initialCfg.ProcessBlocklist) == 0 || initialCfg.ProcessBlocklist[0] != "cmd.exe" {
		t.Fatalf("default blocklist missing shells: %v", initialCfg.ProcessBlocklist)
	}

	// Out-of-range patch answers M10-CC-001.
	bad := e.Handle(ctx, nominationRequest("cc.updateConfig", `{"maxActionsPerMinute":0}`))
	if bad.OK || bad.Error.Code != "M10-CC-001" {
		t.Fatalf("bad update = %+v, want M10-CC-001", bad.Error)
	}

	// The three-step enable flow lands on enabled.
	enabled := e.Handle(ctx, nominationRequest("cc.updateConfig", `{"enabled":true,"securityLevel":"standard"}`))
	if !enabled.OK {
		t.Fatalf("enable failed: %+v", enabled.Error)
	}

	// Emergency stop arms the latch and answers it.
	stopped := e.Handle(ctx, nominationRequest("cc.emergencyStop", `{"actor":"operator","reason":"测试急停"}`))
	if !stopped.OK {
		t.Fatalf("emergencyStop failed: %+v", stopped.Error)
	}
	var stoppedCfg struct {
		EmergencyStopped   bool   `json:"emergencyStopped"`
		EmergencyStoppedAt string `json:"emergencyStoppedAt"`
	}
	if err := json.Unmarshal(mustJSON(stopped.Payload), &stoppedCfg); err != nil {
		t.Fatal(err)
	}
	if !stoppedCfg.EmergencyStopped || stoppedCfg.EmergencyStoppedAt == "" {
		t.Fatalf("latch not armed: %+v", stoppedCfg)
	}

	// Any non-enable patch while latched answers M10-CC-002.
	latched := e.Handle(ctx, nominationRequest("cc.updateConfig", `{"maxActionsPerMinute":60}`))
	if latched.OK || latched.Error.Code != "M10-CC-002" {
		t.Fatalf("latched update = %+v, want M10-CC-002", latched.Error)
	}

	// Re-running the enable flow clears the latch.
	reenabled := e.Handle(ctx, nominationRequest("cc.updateConfig", `{"enabled":true}`))
	if !reenabled.OK {
		t.Fatalf("re-enable failed: %+v", reenabled.Error)
	}
	var reenabledCfg struct {
		Enabled          bool `json:"enabled"`
		EmergencyStopped bool `json:"emergencyStopped"`
	}
	if err := json.Unmarshal(mustJSON(reenabled.Payload), &reenabledCfg); err != nil {
		t.Fatal(err)
	}
	if !reenabledCfg.Enabled || reenabledCfg.EmergencyStopped {
		t.Fatalf("re-enable must clear the latch: %+v", reenabledCfg)
	}
}

func TestCcExecuteToolAuditedThroughBridge(t *testing.T) {
	e, ccSvc := newCcEngine(t)
	ccSvc.SetHost(&fakeCcHost{title: "Notes", process: "notepad.exe"})
	ctx := context.Background()
	if res := e.Handle(ctx, nominationRequest("cc.updateConfig", `{"enabled":true}`)); !res.OK {
		t.Fatalf("enable failed: %+v", res.Error)
	}

	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	tools.SetCcExecutor(ccSvc.ExecuteTool)
	e.SetToolRuntime(tools)

	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	out, err := tools.Execute(ctx, toolruntime.FullAccess, session, "cc.get_active_window", json.RawMessage(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "notepad.exe") {
		t.Fatalf("unexpected output: %q", out.Output)
	}

	listed := e.Handle(ctx, nominationRequest("cc.getAuditLog", `{"limit":20}`))
	if !listed.OK {
		t.Fatalf("getAuditLog failed: %+v", listed.Error)
	}
	raw := string(mustJSON(listed.Payload))
	if !strings.Contains(raw, "cc.get_active_window") || !strings.Contains(raw, "executed") {
		t.Fatalf("audit ledger missing executed entry: %s", raw)
	}

	// Blocked foreground process answers M10-CC-009 end to end.
	e.Handle(ctx, nominationRequest("cc.updateConfig", `{"processBlocklist":["notepad.exe"]}`))
	_, err = tools.Execute(ctx, toolruntime.FullAccess, session, "cc.mouse_move", json.RawMessage(`{"x":100,"y":100}`), true)
	if err == nil || !strings.Contains(err.Error(), "M10-CC-009") {
		t.Fatalf("expected M10-CC-009 process block, got %v", err)
	}
	filtered := e.Handle(ctx, nominationRequest("cc.getAuditLog", `{"limit":20,"status":"blocked"}`))
	if !filtered.OK || !strings.Contains(string(mustJSON(filtered.Payload)), "process-monitor") {
		t.Fatalf("blocked filter missing process-monitor row: %+v", filtered.Payload)
	}
}
