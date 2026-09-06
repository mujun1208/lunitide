package m8app

import (
	"context"
	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/oklog/ulid/v2"
	"reflect"
	"time"
)

type ExpertEquipment struct {
	ExpertID  string   `json:"expertId"`
	VersionID string   `json:"versionId"`
	SkillKeys []string `json:"skillKeys"`
	Known     bool     `json:"known"`
}

func (s *ExpertService) Equipment(ctx context.Context, id, version string) (ExpertEquipment, error) {
	out := ExpertEquipment{ExpertID: id}
	if s == nil || s.uow == nil {
		return out, ErrServiceUnavailable
	}
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(id)
		if err != nil {
			return err
		}
		if version == "" {
			version = e.CurrentVersionID
		}
		v, err := tx.GetVersion(version)
		if err != nil {
			return err
		}
		if v.ExpertID != id {
			return ErrExpertNotFound
		}
		out.VersionID = version
		out.SkillKeys, out.Known, err = tx.GetExpertEquipmentSnapshot(version)
		return err
	})
	return out, err
}
func (s *ExpertService) ReplaceBoundSkillsVersioned(ctx context.Context, id, expected string, keys []string) (ExpertEquipment, error) {
	if s == nil || s.uow == nil {
		return ExpertEquipment{}, ErrServiceUnavailable
	}
	if len(id) != 26 || len(expected) != 26 {
		return ExpertEquipment{}, ErrPayloadInvalid
	}
	keys = s.applySkillFloor(ctx, id, keys)
	var out ExpertEquipment
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		var err error
		out, err = UpdateExpertEquipmentTx(tx, id, expected, keys, s.clock.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
	return out, err
}

// UpdateExpertEquipmentTx also serves catalog seeding/merging: every real
// equipment change appends a version while preserving mounted old versions.
func UpdateExpertEquipmentTx(tx ExpertTx, id, expected string, keys []string, now string) (ExpertEquipment, error) {
	if keys == nil {
		keys = []string{}
	}
	out := ExpertEquipment{ExpertID: id, SkillKeys: keys, Known: true}
	e, err := tx.GetExpert(id)
	if err != nil {
		return out, err
	}
	if expected != "" && e.CurrentVersionID != expected {
		return out, ErrExpertVersionConflict
	}
	if e.State == m8core.ExpertArchived {
		return out, ErrExpertStateInvalid
	}
	current, err := tx.ListExpertSkillKeys(id)
	if err != nil {
		return out, err
	}
	out.VersionID = e.CurrentVersionID
	if reflect.DeepEqual(current, keys) {
		return out, nil
	}
	v, err := tx.GetVersion(e.CurrentVersionID)
	if err != nil {
		return out, err
	}
	next := m8core.BumpPatch(v.Semver)
	for i := 0; i < 1000; i++ {
		if _, has, err := tx.GetVersionBySemver(id, next); err != nil {
			return out, err
		} else if !has {
			break
		}
		next = m8core.BumpPatch(next)
	}
	if err = tx.ReplaceExpertSkillKeys(id, keys); err != nil {
		return out, mapSkillBindError(err)
	}
	v.VersionID, v.Semver, v.ChangeNote, v.CreatedAt = ulid.Make().String(), next, "equipment update", now
	if err = tx.PutVersion(v); err != nil {
		return out, err
	}
	if err = tx.PutExpertEquipmentSnapshot(v.VersionID, keys); err != nil {
		return out, err
	}
	e.CurrentVersionID, e.UpdatedAt = v.VersionID, now
	if err = tx.PutExpert(e); err != nil {
		return out, err
	}
	_, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "expert.update", ResourceType: "expert", ResourceID: id, Actor: "local-user", AfterDigest: v.SixSectionDigest, CreatedAt: now})
	out.VersionID = v.VersionID
	return out, err
}
