package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
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
	if cfg.MaxActionsPerMinute != ccapp.CcDefaultMaxActionsPerMinute {
		t.Fatalf("companion enable should seed rate cap %d, got %d", ccapp.CcDefaultMaxActionsPerMinute, cfg.MaxActionsPerMinute)
	}
}

func TestEnsureCompanionDoesNotEnableFullDiskOnFreshRoot(t *testing.T) {
	e, _ := newCcEngine(t)
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tools.Close() })
	e.SetToolRuntime(tools)
	if tools.FullDiskEnabled() {
		t.Fatal("fresh tool root must not enable fullAccess")
	}
	e.ensureCompanionRuntimeCapabilities(context.Background())
	if tools.FullDiskEnabled() {
		t.Fatal("companion must not silently enable full-disk command policy")
	}
}

func TestEnsureCompanionDoesNotEnableFullDisk(t *testing.T) {
	e, _ := newCcEngine(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "command-policy.json"), []byte(`{"commands":[],"fullAccess":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tools, err := toolruntime.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tools.Close() })
	e.SetToolRuntime(tools)
	if tools.FullDiskEnabled() {
		t.Fatal("seed must not enable fullAccess")
	}
	e.ensureCompanionRuntimeCapabilities(context.Background())
	if tools.FullDiskEnabled() {
		t.Fatal("companion must not silently enable full-disk command policy")
	}
}

func TestEnsureCompanionBumpsLegacyRateCap(t *testing.T) {
	e, ccSvc := newCcEngine(t)
	ctx := context.Background()
	enabled := true
	legacy := ccapp.CcLegacyDefaultMaxActionsPerMinute
	if _, err := ccSvc.UpdateConfig(ctx, ccapp.SettingsPatch{Enabled: &enabled, MaxActionsPerMinute: &legacy}); err != nil {
		t.Fatal(err)
	}
	e.ensureCompanionRuntimeCapabilities(ctx)
	cfg, err := ccSvc.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.MaxActionsPerMinute != ccapp.CcDefaultMaxActionsPerMinute {
		t.Fatalf("legacy 30/min must bump to %d: %+v", ccapp.CcDefaultMaxActionsPerMinute, cfg)
	}
}
