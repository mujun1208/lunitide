package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/ccapp"
)

// ensureCompanionRuntimeCapabilities may raise a legacy rate cap when the
// operator has already enabled computer control. It never turns CC on.
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
	if !cfg.Enabled {
		return
	}
	needRate := cfg.MaxActionsPerMinute == ccapp.CcLegacyDefaultMaxActionsPerMinute
	if !needRate {
		return
	}
	cap := ccapp.CcDefaultMaxActionsPerMinute
	_, _ = e.ccctrl.UpdateConfig(ctx, ccapp.SettingsPatch{Actor: "companion", MaxActionsPerMinute: &cap})
}
