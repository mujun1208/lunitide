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

func (s *Service) expireArm(tx Tx, cur Settings) (Settings, error) {
	if !cur.Enabled || !armExpired(s.clock.Now(), cur.ArmedUntil) {
		return cur, nil
	}
	cur.Enabled = false
	cur.ArmedUntil = ""
	cur.UpdatedAt = s.clock.Now().UTC().Format(time.RFC3339)
	if err := tx.PutCcSettings(cur); err != nil {
		return cur, err
	}
	return cur, nil
}

// GetConfig answers the singleton, seeding the default row on first read.
func (s *Service) GetConfig(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		cur, e := tx.GetCcSettings()
		if e != nil {
			return e
		}
		out, e = s.expireArm(tx, cur)
		return e
	})
	return out, err
}

// ValidateSettings checks one settings document (shared by update and
// seeding).
func ValidateSettings(v Settings) error {
	if v.SecurityLevel != LevelStandard && v.SecurityLevel != LevelStrict {
		return fmt.Errorf("%w: securityLevel", ErrCcSchema)
	}
	if v.MaxActionsPerMinute < 1 || v.MaxActionsPerMinute > 120 {
		return fmt.Errorf("%w: maxActionsPerMinute", ErrCcSchema)
	}
	if v.ConfirmTimeoutSecond < 10 || v.ConfirmTimeoutSecond > 600 {
		return fmt.Errorf("%w: confirmTimeoutSeconds", ErrCcSchema)
	}
	if len(v.ProcessBlocklist) > CcMaxBlocklistEntries {
		return fmt.Errorf("%w: processBlocklist size", ErrCcSchema)
	}
	seen := map[string]bool{}
	for _, item := range v.ProcessBlocklist {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" || len(name) > CcMaxBlocklistEntryLen || strings.ContainsAny(name, "/\\") || seen[name] {
			return fmt.Errorf("%w: processBlocklist entry", ErrCcSchema)
		}
		seen[name] = true
	}
	return nil
}

// UpdateConfig applies one patch. Re-enabling clears the emergency latch
// (the operator just re-ran the enable flow); any other patch with the
// latch armed is refused so the stop stays visible until acknowledged.
func (s *Service) UpdateConfig(ctx context.Context, patch SettingsPatch) (Settings, error) {
	var out Settings
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		cur, err := tx.GetCcSettings()
		if err != nil {
			return err
		}
		next := cur
		if patch.Enabled != nil {
			next.Enabled = *patch.Enabled
		}
		if patch.SecurityLevel != nil {
			next.SecurityLevel = *patch.SecurityLevel
		}
		if patch.AllowCritical != nil {
			next.AllowCritical = *patch.AllowCritical
		}
		if patch.ProcessBlocklist != nil {
			next.ProcessBlocklist = *patch.ProcessBlocklist
		}
		if patch.MaxActionsPerMinute != nil {
			next.MaxActionsPerMinute = *patch.MaxActionsPerMinute
		}
		if patch.ConfirmTimeoutSecond != nil {
			next.ConfirmTimeoutSecond = *patch.ConfirmTimeoutSecond
		}
		if patch.ArmMinutes != nil && (*patch.ArmMinutes < 0 || *patch.ArmMinutes > CcMaxArmMinutes) {
			return fmt.Errorf("%w: armMinutes", ErrCcSchema)
		}
		next.ArmedUntil = nextArmedUntil(s.clock.Now(), patch, cur)
		if err := ValidateSettings(next); err != nil {
			return err
		}
		if cur.EmergencyStopped {
			if patch.Enabled == nil || !*patch.Enabled {
				return fmt.Errorf("%w: 紧急停止已激活，需重新走启用流程", ErrCcState)
			}
			next.EmergencyStopped = false
			next.EmergencyStoppedAt = ""
		}
		if patch.Enabled != nil && !*patch.Enabled {
			next.EmergencyStopped = false
			next.EmergencyStoppedAt = ""
		}
		ts := s.clock.Now().UTC().Format(time.RFC3339)
		next.UpdatedAt = ts
		if err := tx.PutCcSettings(next); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{
			"enabled": next.Enabled, "securityLevel": next.SecurityLevel,
			"allowCritical": next.AllowCritical,
		})
		if err := tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "cc.config.updated",
			AggregateID: "cc-security-config", Actor: actorOr(patch.Actor),
			Metadata: meta, CreatedAt: s.clock.Now().UTC(),
		}); err != nil {
			return err
		}
		out = next
		return nil
	})
	return out, err
}
