package ccapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/providerapp"
)

type executionTestTx struct {
	Tx
	settings     Settings
	entries      []AuditEntry
	events       []providerapp.Audit
	failIntent   bool
	failReceipt  bool
	failSettings bool
	onIntent     func()
}

func (tx *executionTestTx) GetCcSettings() (Settings, error) { return tx.settings, nil }
func (tx *executionTestTx) PutCcSettings(s Settings) error {
	if tx.failSettings {
		return errors.New("injected settings write failure")
	}
	tx.settings = s
	return nil
}
func (tx *executionTestTx) AppendCcAudit(e AuditEntry) error {
	if tx.failReceipt {
		return errors.New("injected receipt disk failure")
	}
	tx.entries = append(tx.entries, e)
	return nil
}
func (tx *executionTestTx) PutAudit(e providerapp.Audit) error {
	var meta map[string]any
	_ = json.Unmarshal(e.Metadata, &meta)
	if meta["phase"] == "prepared" {
		if tx.failIntent {
			return errors.New("injected intent disk failure")
		}
		if tx.onIntent != nil {
			tx.onIntent()
		}
	}
	tx.events = append(tx.events, e)
	return nil
}

type executionTestUow struct {
	mu sync.Mutex
	tx *executionTestTx
}

func (u *executionTestUow) TransactCc(ctx context.Context, fn func(Tx) error) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(u.tx)
}

type executionTestHost struct {
	Host
	before       func()
	write        func()
	writes       atomic.Int32
	foreground   func() (string, string, error)
	focusProcess string
	windowList   func() ([]WindowInfo, error)
	holdKey      func(string, bool) error
}

func (h *executionTestHost) Available() bool {
	if h.before != nil {
		h.before()
	}
	return true
}
func (h *executionTestHost) ClipboardSet(string) error {
	h.writes.Add(1)
	if h.write != nil {
		h.write()
	}
	return nil
}
func (h *executionTestHost) ActiveWindow() (string, string, error) {
	if h.foreground != nil {
		return h.foreground()
	}
	return "Notes", "notepad.exe", nil
}
func (h *executionTestHost) FocusWindow(q string) (WindowInfo, error) {
	return WindowInfo{Title: q, Process: h.focusProcess}, nil
}
func (h *executionTestHost) EnsureForeground() error { return nil }
func (h *executionTestHost) HoldKey(key string, down bool) error {
	if h.holdKey != nil {
		return h.holdKey(key, down)
	}
	return nil
}
func (h *executionTestHost) KeyboardType(string) error { h.writes.Add(1); return nil }
func (h *executionTestHost) KeyboardShortcut([]string) error {
	h.writes.Add(1)
	if h.write != nil {
		h.write()
	}
	return nil
}
func (h *executionTestHost) ScreenCapture() ([]byte, error) {
	return nil, errors.New("fake skips verification capture")
}
func (h *executionTestHost) WindowCapture(string) ([]byte, int, int, error) {
	return nil, 0, 0, errors.New("fake skips verification capture")
}
func (h *executionTestHost) ListWindows() ([]WindowInfo, error) {
	if h.windowList != nil {
		return h.windowList()
	}
	return []WindowInfo{{ID: "1", Title: "Notes", Process: "notepad.exe", Foreground: true}}, nil
}
func executionTestService(t *testing.T) (*Service, *executionTestTx, *executionTestHost) {
	t.Helper()
	tx := &executionTestTx{settings: Settings{Revision: 1, Enabled: true, SecurityLevel: LevelStandard, MaxActionsPerMinute: 120, ConfirmTimeoutSecond: 30, ProcessBlocklist: []string{"powershell.exe"}}}
	h := &executionTestHost{}
	s := New(&executionTestUow{tx: tx})
	s.SetHost(h)
	return s, tx, h
}
func executeTestClipboard(s *Service, ctx context.Context) error {
	_, err := s.ExecuteTool(ctx, "test", ToolClipboard, []byte(`{"op":"set","text":"test"}`), true)
	return err
}
func awaitExecution(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not finish")
		return nil
	}
}

func TestExecutionCancelledBeforeHostWrite(t *testing.T) {
	s, _, h := executionTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.before = cancel
	if err := executeTestClipboard(s, ctx); !errors.Is(err, context.Canceled) || h.writes.Load() != 0 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
}
func TestExecutionEmergencyBeforeHostWrite(t *testing.T) {
	s, tx, h := executionTestService(t)
	h.before = func() {
		_, err := s.EmergencyStop(context.Background(), "test", "stop")
		if !errors.Is(err, ErrCcStopPending) {
			t.Errorf("stop must report active operation: %v", err)
		}
	}
	if err := executeTestClipboard(s, context.Background()); !errors.Is(err, ErrCcEmergency) || h.writes.Load() != 0 || !tx.settings.EmergencyStopped {
		t.Fatalf("writes=%d err=%v settings=%+v", h.writes.Load(), err, tx.settings)
	}
}
func TestExecutionReceiptFailureIsUnknownOutcome(t *testing.T) {
	s, tx, h := executionTestService(t)
	tx.failReceipt = true
	err := executeTestClipboard(s, context.Background())
	if !errors.Is(err, ErrCcOutcomeUnknown) || h.writes.Load() != 1 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
	if len(tx.events) != 1 {
		t.Fatalf("prepared intent missing: %+v", tx.events)
	}
}
func TestExecutionNamedBlockedWindowCannotReceiveInput(t *testing.T) {
	s, tx, h := executionTestService(t)
	h.focusProcess = "powershell.exe"
	_, err := s.ExecuteTool(context.Background(), "test", ToolKeyboardType, []byte(`{"text":"x","window":"PowerShell"}`), true)
	if !errors.Is(err, ErrCcProcessBlocked) || h.writes.Load() != 0 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
	if len(tx.entries) != 1 || tx.entries[0].Status != StatusBlocked {
		t.Fatalf("missing blocked receipt: %+v", tx.entries)
	}
}
func TestExecutionIntentFailurePreventsHostWrite(t *testing.T) {
	s, tx, h := executionTestService(t)
	tx.failIntent = true
	if err := executeTestClipboard(s, context.Background()); !errors.Is(err, ErrCcAuditUnavailable) || h.writes.Load() != 0 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
}
func TestExecutionCancellationAfterIntentGetsTerminalReceipt(t *testing.T) {
	s, tx, h := executionTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx.onIntent = cancel
	err := executeTestClipboard(s, ctx)
	if !errors.Is(err, context.Canceled) || h.writes.Load() != 0 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
	if len(tx.entries) != 1 || tx.entries[0].Status != StatusStopped {
		t.Fatalf("missing stopped receipt: %+v", tx.entries)
	}
	var meta, receipt map[string]any
	_ = json.Unmarshal(tx.events[0].Metadata, &meta)
	_ = json.Unmarshal([]byte(tx.entries[0].Detail), &receipt)
	if receipt["phase"] != "receipt" || receipt["operationId"] != meta["operationId"] || receipt["dispatched"] != false {
		t.Fatalf("receipt=%v intent=%v", receipt, meta)
	}
}
func TestExecutionCancellationAfterWriteRecordsPossibleEffect(t *testing.T) {
	s, tx, h := executionTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.write = cancel
	err := executeTestClipboard(s, ctx)
	if !errors.Is(err, context.Canceled) || h.writes.Load() != 1 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
	var receipt map[string]any
	if len(tx.entries) != 1 {
		t.Fatalf("missing receipt: %+v", tx.entries)
	}
	_ = json.Unmarshal([]byte(tx.entries[0].Detail), &receipt)
	if receipt["dispatched"] != true || receipt["outcome"] != StatusStopped {
		t.Fatalf("receipt=%v", receipt)
	}
}
func TestExecutionEmergencyDoesNotWaitForBlockedHost(t *testing.T) {
	s, _, h := executionTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.write = func() { close(entered); <-release }
	done := make(chan error, 1)
	go func() { done <- executeTestClipboard(s, context.Background()) }()
	<-entered
	stop := make(chan error, 1)
	go func() { _, err := s.EmergencyStop(context.Background(), "test", "stop"); stop <- err }()
	stopErr := awaitExecution(t, stop)
	close(release)
	if !errors.Is(stopErr, ErrCcStopPending) {
		t.Fatalf("pending stop: %v", stopErr)
	}
	if err := awaitExecution(t, done); !errors.Is(err, ErrCcEmergency) {
		t.Fatalf("execution=%v", err)
	}
	if _, err := s.EmergencyStop(context.Background(), "test", "settled"); err != nil {
		t.Fatalf("settled stop=%v", err)
	}
}
func TestExecutionQueuedCancellationDoesNotEnterHost(t *testing.T) {
	s, _, h := executionTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.write = func() { close(entered); <-release }
	done := make(chan error, 1)
	go func() { done <- executeTestClipboard(s, context.Background()) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { queued <- executeTestClipboard(s, ctx) }()
	cancel()
	queuedErr := awaitExecution(t, queued)
	close(release)
	if err := awaitExecution(t, done); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(queuedErr, context.Canceled) || h.writes.Load() != 1 {
		t.Fatalf("writes=%d queued=%v", h.writes.Load(), queuedErr)
	}
}
func TestExecutionRepeatedPressStopsAfterCancellation(t *testing.T) {
	s, _, h := executionTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.write = cancel
	_, err := s.ExecuteTool(ctx, "test", ToolPress, []byte(`{"key":"enter","count":3}`), true)
	if !errors.Is(err, context.Canceled) || h.writes.Load() != 1 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
}
func TestExecutionRechecksForegroundAfterFocus(t *testing.T) {
	s, _, h := executionTestService(t)
	h.focusProcess = "notepad.exe"
	var checks int
	h.foreground = func() (string, string, error) {
		checks++
		if checks > 1 {
			return "Shell", "powershell.exe", nil
		}
		return "Notes", "notepad.exe", nil
	}
	_, err := s.ExecuteTool(context.Background(), "test", ToolKeyboardType, []byte(`{"text":"x","window":"Notes"}`), true)
	if !errors.Is(err, ErrCcProcessBlocked) || h.writes.Load() != 0 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
}
func TestExecutionRejectsUnknownTargetProcess(t *testing.T) {
	s, _, h := executionTestService(t)
	h.windowList = func() ([]WindowInfo, error) { return nil, errors.New("access denied") }
	_, err := s.ExecuteTool(context.Background(), "test", ToolAppQuit, []byte(`{"name":"notes"}`), true)
	if !errors.Is(err, ErrCcProcessBlocked) {
		t.Fatalf("err=%v", err)
	}
}
func TestExecutionConfigChangeCancelsInFlightOperation(t *testing.T) {
	s, tx, h := executionTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.write = func() { close(entered); <-release }
	done := make(chan error, 1)
	go func() { done <- executeTestClipboard(s, context.Background()) }()
	<-entered
	enabled := false
	_, updateErr := s.UpdateConfig(context.Background(), SettingsPatch{ExpectedRevision: tx.settings.Revision, Enabled: &enabled})
	close(release)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	if err := awaitExecution(t, done); !errors.Is(err, ErrCcPermissionChanged) {
		t.Fatalf("execution=%v", err)
	}
}

func TestExecutionEmergencyPersistenceFailureKeepsLocalLatch(t *testing.T) {
	s, tx, h := executionTestService(t)
	tx.failSettings = true
	if _, err := s.EmergencyStop(context.Background(), "test", "disk full"); !errors.Is(err, ErrCcStopPersistence) {
		t.Fatalf("err=%v", err)
	}
	config, err := s.GetConfig(context.Background())
	if err != nil || !config.EmergencyStopped {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	if err := executeTestClipboard(s, context.Background()); !errors.Is(err, ErrCcEmergency) || h.writes.Load() != 0 {
		t.Fatalf("writes=%d err=%v", h.writes.Load(), err)
	}
}

func TestExecutionEmergencyDoesNotWaitForHeldKeyCleanup(t *testing.T) {
	s, _, h := executionTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.holdKey = func(_ string, down bool) error {
		if !down {
			close(entered)
			<-release
		}
		return nil
	}
	s.noteHeld("ctrl")
	stop := make(chan error, 1)
	go func() { _, err := s.EmergencyStop(context.Background(), "test", "release"); stop <- err }()
	stopErr := awaitExecution(t, stop)
	<-entered
	close(release)
	if !errors.Is(stopErr, ErrCcStopPending) {
		t.Fatalf("stop=%v", stopErr)
	}
}

func TestExecutionWaitIsCancelledWithoutWaitingForTimeout(t *testing.T) {
	s, _, _ := executionTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op, err := s.beginExecution(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.endExecution(op)
	done := make(chan error, 1)
	go func() { done <- s.waitExecution(time.Minute) }()
	cancel()
	if err := awaitExecution(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait=%v", err)
	}
}

func TestExecutionAuditRejectsOversizedDetailWithoutInvalidJSON(t *testing.T) {
	s, tx, _ := executionTestService(t)
	err := s.writeAudit(context.Background(), "test", ToolClipboard, RiskMedium, StatusExecuted, "", "cc.operation.executed", map[string]any{"text": strings.Repeat("中", 2000)}, time.Now().UTC().Format(time.RFC3339))
	if !errors.Is(err, ErrCcSchema) || len(tx.entries) != 0 {
		t.Fatalf("entries=%v err=%v", tx.entries, err)
	}
}
