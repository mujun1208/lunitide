package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestCcConfigRevisionPreventsStaleWholeListAndEmergencyReenable(t *testing.T) {
	svc, _, _ := newCcService(t)
	ctx := context.Background()
	first, err := svc.GetConfig(ctx)
	if err != nil || first.Revision != 1 {
		t.Fatalf("seed %+v: %v", first, err)
	}
	list := []string{"cmd.exe", "regedit.exe"}
	next, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{ExpectedRevision: first.Revision, ProcessBlocklist: &list})
	if err != nil || next.Revision != 2 {
		t.Fatalf("update %+v: %v", next, err)
	}
	stale := []string{"cmd.exe", "taskmgr.exe"}
	if _, err = svc.UpdateConfig(ctx, ccapp.SettingsPatch{ExpectedRevision: first.Revision, ProcessBlocklist: &stale}); !errors.Is(err, ccapp.ErrCcConflict) {
		t.Fatalf("stale overwrite=%v", err)
	}
	if _, err = svc.UpdateConfig(ctx, ccapp.SettingsPatch{ProcessBlocklist: &stale}); !errors.Is(err, ccapp.ErrCcConflict) {
		t.Fatalf("missing version=%v", err)
	}
	got, err := svc.GetConfig(ctx)
	if err != nil || got.Revision != 2 || len(got.ProcessBlocklist) != 2 || got.ProcessBlocklist[1] != "regedit.exe" {
		t.Fatalf("saved truth changed %+v: %v", got, err)
	}
	stopped, err := svc.EmergencyStop(ctx, "test", "test stop")
	if err != nil || stopped.Revision != 3 {
		t.Fatalf("stop %+v: %v", stopped, err)
	}
	on := true
	if _, err = svc.UpdateConfig(ctx, ccapp.SettingsPatch{ExpectedRevision: next.Revision, Enabled: &on}); !errors.Is(err, ccapp.ErrCcConflict) {
		t.Fatalf("stale re-enable bypassed emergency=%v", err)
	}
	reenabled, err := svc.UpdateConfig(ctx, ccapp.SettingsPatch{ExpectedRevision: stopped.Revision, Enabled: &on})
	if err != nil || reenabled.Revision != 4 || reenabled.EmergencyStopped {
		t.Fatalf("fresh enable %+v: %v", reenabled, err)
	}
}

func TestCcConfigAuditFailureRollsBackRevisionAndSavedPolicy(t *testing.T) {
	s, svc, _ := ccRecoveryFixture(t, 0)
	ctx := context.Background()
	before, err := svc.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER cc_config_fail BEFORE INSERT ON audit_events WHEN NEW.action='cc.config.updated' BEGIN SELECT RAISE(ABORT,'injected audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	list := []string{"cmd.exe"}
	if _, err = svc.UpdateConfig(ctx, ccapp.SettingsPatch{ExpectedRevision: before.Revision, ProcessBlocklist: &list}); err == nil {
		t.Fatal("audit failure reported success")
	}
	got, err := svc.GetConfig(ctx)
	if err != nil || got.Revision != before.Revision || len(got.ProcessBlocklist) != len(before.ProcessBlocklist) {
		t.Fatalf("partial commit %+v: %v", got, err)
	}
}
