package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/ccapp"
)

// Entering an attended voice conversation authorizes its enabled capabilities.
// The grant belongs to this session, is audited, and never changes global
// settings or clears an emergency stop. Inbound colleague messages cannot grant it.
func (e *Engine) authorizeCompanionSession(ctx context.Context, session string) error {
	if e == nil || e.tools == nil || session == "" || unattended(ctx) || !e.tools.FullDiskEnabled() {
		return nil
	}
	if e.ccctrl != nil {
		cfg, err := e.ccctrl.GetConfig(ctx)
		if err != nil || cfg.EmergencyStopped {
			return nil
		}
	}
	return e.tools.ConfirmFullDiskSession(ctx, session)
}

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
	_, _ = e.ccctrl.UpdateConfig(ctx, ccapp.SettingsPatch{ExpectedRevision: cfg.Revision, Actor: "companion", MaxActionsPerMinute: &cap})
}

// companionCcEnabled reports whether the operator has computer control turned
// on (and not emergency-latched). When true, the standing enable acts as the
// approval for launch-shaped desktop tools (desktop.open / media.play) so a
// voice turn does not stall on an approval tap nobody can press. cc.* /
// computer.act / desktop.type stay gated regardless — those move the mouse or
// type into arbitrary windows and still deserve a real confirmation.
func (e *Engine) companionCcEnabled(ctx context.Context) bool {
	if e == nil || e.ccctrl == nil {
		return false
	}
	cfg, err := e.ccctrl.GetConfig(ctx)
	if err != nil || cfg.EmergencyStopped || !cfg.Enabled {
		return false
	}
	return true
}
