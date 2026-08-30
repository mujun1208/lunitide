package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/ccapp"
)

// ensureCompanionRuntimeCapabilities opts the voice companion into computer
// control (enable + rate cap) so desktop.open / computer.act can run.
// Full-disk command policy stays a user setting — never silently fullAccess.
func (e *Engine) ensureCompanionRuntimeCapabilities(ctx context.Context) {
	if e == nil {
		return
	}
	if e.ccctrl == nil {
		return
	}
	cfg, err := e.ccctrl.GetConfig(ctx)
	if err != nil {
		return
	}
	if cfg.EmergencyStopped {
		return
	}
	needEnable := !cfg.Enabled
	needRate := cfg.MaxActionsPerMinute == ccapp.CcLegacyDefaultMaxActionsPerMinute
	if !needEnable && !needRate {
		return
	}
	patch := ccapp.SettingsPatch{Actor: "companion"}
	if needEnable {
		enabled := true
		level := ccapp.LevelStandard
		patch.Enabled = &enabled
		patch.SecurityLevel = &level
	}
	if needRate {
		cap := ccapp.CcDefaultMaxActionsPerMinute
		patch.MaxActionsPerMinute = &cap
	}
	_, _ = e.ccctrl.UpdateConfig(ctx, patch)
}
