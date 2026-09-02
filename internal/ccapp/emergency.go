package ccapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

const holdKeyAutoRelease = 8 * time.Second

// EmergencyStop arms the latch: every later tool call fails closed with
// M10-CC-005 until the operator re-runs the enable flow.
func (s *Service) EmergencyStop(ctx context.Context, actor, reason string) (Settings, error) {
	var out Settings
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		cur, err := tx.GetCcSettings()
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		ts := now.Format(time.RFC3339)
		cur.EmergencyStopped = true
		cur.EmergencyStoppedAt = ts
		cur.ArmedUntil = ""
		cur.UpdatedAt = ts
		if err := tx.PutCcSettings(cur); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"reason": clampReason(reason)})
		if err := tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "cc.emergency.stopped",
			AggregateID: "cc-security-config", Actor: actorOr(actor),
			Metadata: meta, CreatedAt: now,
		}); err != nil {
			return err
		}
		out = cur
		return nil
	})
	s.releaseHeldKeys()
	return out, err
}

func clampReason(reason string) string {
	if len(reason) > 512 {
		reason = reason[:512]
	}
	return reason
}

func isHoldModifier(key string) bool {
	return key == "ctrl" || key == "shift" || key == "alt" || key == "win"
}

func normalizeModifiers(mods []string) ([]string, error) {
	if len(mods) == 0 {
		return nil, nil
	}
	if len(mods) > CcMaxClickModifiers {
		return nil, fmt.Errorf("%w: modifiers", ErrCcInputFiltered)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(mods))
	for _, raw := range mods {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "control" {
			key = "ctrl"
		}
		if !isHoldModifier(key) {
			return nil, fmt.Errorf("%w: modifier %q", ErrCcInputFiltered, raw)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out, nil
}

func (s *Service) withModifiers(mods []string, fn func() error) error {
	var held []string
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = s.host.HoldKey(held[i], false)
		}
	}()
	for _, m := range mods {
		if err := s.host.HoldKey(m, true); err != nil {
			return err
		}
		held = append(held, m)
	}
	return fn()
}

func (s *Service) noteHeld(key string) {
	s.holdMu.Lock()
	defer s.holdMu.Unlock()
	for _, k := range s.heldKeys {
		if k == key {
			s.armHoldReleaseLocked()
			return
		}
	}
	s.heldKeys = append(s.heldKeys, key)
	s.armHoldReleaseLocked()
}

func (s *Service) clearHeld(key string) {
	s.holdMu.Lock()
	defer s.holdMu.Unlock()
	kept := s.heldKeys[:0]
	for _, k := range s.heldKeys {
		if k != key {
			kept = append(kept, k)
		}
	}
	s.heldKeys = kept
	if len(s.heldKeys) == 0 && s.holdTimer != nil {
		s.holdTimer.Stop()
		s.holdTimer = nil
	}
}

func (s *Service) armHoldReleaseLocked() {
	if s.holdTimer != nil {
		s.holdTimer.Stop()
	}
	s.holdTimer = time.AfterFunc(holdKeyAutoRelease, s.releaseHeldKeys)
}

func (s *Service) releaseHeldKeys() {
	s.holdMu.Lock()
	keys := append([]string(nil), s.heldKeys...)
	s.heldKeys = nil
	if s.holdTimer != nil {
		s.holdTimer.Stop()
		s.holdTimer = nil
	}
	host := s.host
	s.holdMu.Unlock()
	if host == nil {
		return
	}
	for i := len(keys) - 1; i >= 0; i-- {
		_ = host.HoldKey(keys[i], false)
	}
}
