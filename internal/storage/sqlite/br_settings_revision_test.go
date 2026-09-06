package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/brapp"
)

func TestBrSettingsCASRejectsMissingAndStaleRevision(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBrService(t)
	for _, revision := range []int64{0, 2} {
		if _, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{ExpectedRevision: revision}); !errors.Is(err, brapp.ErrBrConflict) {
			t.Fatalf("revision %d: %v", revision, err)
		}
	}
	n := 14
	first, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{ExpectedRevision: 1, DataRetentionDays: &n})
	if err != nil || first.Revision != 2 || first.ApplyStatus != brapp.ApplyApplied {
		t.Fatalf("first: %+v %v", first, err)
	}
	n = 90
	if _, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{ExpectedRevision: 1, DataRetentionDays: &n}); !errors.Is(err, brapp.ErrBrConflict) {
		t.Fatalf("stale: %v", err)
	}
	latest, err := svc.GetSettings(ctx)
	if err != nil || latest.DataRetentionDays != 14 {
		t.Fatalf("stale overwrote settings: %+v %v", latest, err)
	}
}

func TestBrSettingsApplyFailureIsDurableAndRetryable(t *testing.T) {
	ctx := context.Background()
	svc, host := newBrService(t)
	before, err := svc.Connect(ctx, "br-apply", brapp.ModeChrome, "test")
	if err != nil {
		t.Fatal(err)
	}
	host.disconnectErr = errors.New("process access denied")
	host.onDisconnect = func() {
		// Host work must not hold SQLite's writer. This callback would time out
		// against the former transaction-wrapped host.Disconnect implementation.
		readCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		s, err := svc.GetSettings(readCtx)
		if err != nil || s.ApplyStatus != brapp.ApplyApplying {
			t.Errorf("pending intent unavailable during host call: %+v %v", s, err)
		}
	}
	edge := brapp.ModeEdge
	failed, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{ExpectedRevision: 1, Mode: &edge})
	if err != nil || failed.ApplyStatus != brapp.ApplyFailed || failed.ApplyError == "" || failed.Mode != edge {
		t.Fatalf("failed apply: %+v %v", failed, err)
	}
	live, err := svc.ListSessions(ctx)
	if err != nil || len(live) != 1 || live[0].State != brapp.StateError || live[0].WsURL != before.WsURL || live[0].ConnectedAt != before.ConnectedAt {
		t.Fatalf("false disconnect receipt: %+v %v", live, err)
	}
	if _, err := svc.Navigate(ctx, before.SessionID, "https://example.com", "test"); !errors.Is(err, brapp.ErrBrState) {
		t.Fatalf("unapplied policy navigated: %v", err)
	}
	if _, err := svc.Connect(ctx, "br-new", "", "test"); !errors.Is(err, brapp.ErrBrState) {
		t.Fatalf("unapplied policy connected: %v", err)
	}
	host.disconnectErr = nil
	done, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{ExpectedRevision: failed.Revision})
	if err != nil || done.ApplyStatus != brapp.ApplyApplied || done.ApplyError != "" {
		t.Fatalf("retry: %+v %v", done, err)
	}
	live, err = svc.ListSessions(ctx)
	if err != nil || live[0].State != brapp.StateDisconnected || live[0].WsURL != "" {
		t.Fatalf("retry did not close: %+v %v", live, err)
	}
}

func TestBrSettingsInterruptedApplyRequiresRetryAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "browser.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	err = store.AgentRuntimeRepository().TransactBr(ctx, func(tx brapp.Tx) error {
		s, err := tx.GetBrSettings()
		if err != nil {
			return err
		}
		s.ApplyStatus = brapp.ApplyApplying
		return tx.PutBrSettings(s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := brapp.New(store.AgentRuntimeRepository(), t.TempDir())
	svc.SetHost(&fakeBrHost{})
	s, err := svc.GetSettings(ctx)
	if err != nil || s.ApplyStatus != brapp.ApplyFailed || s.ApplyError == "" {
		t.Fatalf("interruption falsely applied: %+v %v", s, err)
	}
	done, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{ExpectedRevision: s.Revision})
	if err != nil || done.ApplyStatus != brapp.ApplyApplied {
		t.Fatalf("retry unavailable: %+v %v", done, err)
	}
}

func TestBrDisconnectFailureKeepsEndpointAndBlocksDuplicateConnect(t *testing.T) {
	ctx := context.Background()
	svc, host := newBrService(t)
	sess, err := svc.Connect(ctx, "br-stop-failure", brapp.ModeChrome, "test")
	if err != nil {
		t.Fatal(err)
	}
	host.disconnectErr = errors.New("stop failed")
	failed, err := svc.Disconnect(ctx, sess.SessionID, "test")
	if !errors.Is(err, brapp.ErrBrState) || failed.State != brapp.StateError || failed.WsURL == "" {
		t.Fatalf("false stop: %+v %v", failed, err)
	}
	if _, err := svc.Connect(ctx, sess.SessionID, brapp.ModeChrome, "test"); !errors.Is(err, brapp.ErrBrState) {
		t.Fatalf("duplicate process launched: %v", err)
	}
	host.disconnectErr = nil
	if _, err := svc.Disconnect(ctx, sess.SessionID, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestBrConcurrentSessionBudgetIsReleasedByDisconnect(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBrService(t)
	for _, id := range []string{"one", "two", "three", "four"} {
		if _, err := svc.Connect(ctx, id, brapp.ModeBuiltin, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Connect(ctx, "five", brapp.ModeBuiltin, "test"); !errors.Is(err, brapp.ErrBrState) {
		t.Fatalf("unbounded live sessions: %v", err)
	}
	if _, err := svc.Disconnect(ctx, "one", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Connect(ctx, "five", brapp.ModeBuiltin, "test"); err != nil {
		t.Fatalf("closed session retained resource slot: %v", err)
	}
}
