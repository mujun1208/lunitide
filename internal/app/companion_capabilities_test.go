package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestEnsureCompanionDoesNotClearEmergencyStop(t *testing.T) {
	e, ccSvc := newCcEngine(t)
	ctx := context.Background()
	enabled := true
	if _, err := ccSvc.UpdateConfig(ctx, ccapp.SettingsPatch{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	stopped, err := ccSvc.EmergencyStop(ctx, "operator", "companion must not clear")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.EmergencyStopped {
		t.Fatal("expected emergency stop")
	}
	e.ensureCompanionRuntimeCapabilities(ctx)
	cfg, err := ccSvc.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.EmergencyStopped || cfg.EmergencyStoppedAt != stopped.EmergencyStoppedAt {
		t.Fatalf("companion must not call UpdateConfig while stopped: %+v", cfg)
	}
}

func TestEnsureCompanionEnablesWhenIdle(t *testing.T) {
	e, ccSvc := newCcEngine(t)
	ctx := context.Background()
	before, err := ccSvc.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Enabled || before.EmergencyStopped {
		t.Fatalf("unexpected seed: %+v", before)
	}
	e.ensureCompanionRuntimeCapabilities(ctx)
	cfg, err := ccSvc.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.EmergencyStopped {
		t.Fatalf("companion should enable idle computer control: %+v", cfg)
	}
}
