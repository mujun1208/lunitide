package m8app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/oklog/ulid/v2"
)

var (
	ErrExpertManualOnly = errors.New("m8app: operation requires own manually created expert")
	ErrExpertInUse      = errors.New("m8app: expert is mounted in a project or session")
)

func (s *ExpertService) ownManual(e m8core.ExpertCatalog) error {
	if e.SubjectID != s.subject {
		return ErrExpertNotFound
	}
	if e.CreationOrigin != m8core.ExpertOriginManual || e.Source != m8core.ExpertSourceLocal || e.CatalogItemID != "" || e.OriginBundleID != "" {
		return ErrExpertManualOnly
	}
	return nil
}

type ExpertTrial struct {
	ExpertID   string            `json:"expertId"`
	VersionID  string            `json:"versionId"`
	Name       string            `json:"name"`
	State      string            `json:"state"`
	SixSection m8core.SixSection `json:"sixSection"`
}

// PrepareTrial takes an immutable snapshot without toggling, mounting, or publishing.
func (s *ExpertService) PrepareTrial(ctx context.Context, id, expected string) (ExpertTrial, error) {
	var out ExpertTrial
	if s == nil || s.uow == nil {
		return out, ErrServiceUnavailable
	}
	if len(id) != 26 || len(expected) != 26 {
		return out, ErrPayloadInvalid
	}
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(id)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		if err := s.ownManual(e); err != nil {
			return err
		}
		if e.DeletedAt != "" {
			return ErrExpertNotFound
		}
		if e.State == m8core.ExpertArchived {
			return ErrExpertStateInvalid
		}
		if e.CurrentVersionID != expected {
			return ErrExpertVersionConflict
		}
		v, err := tx.GetVersion(expected)
		if err != nil {
			return err
		}
		if v.ExpertID != id {
			return ErrExpertNotFound
		}
		body, err := s.loadBody(v.PersonaRef, v.SixSectionDigest)
		if err != nil {
			return err
		}
		out = ExpertTrial{ExpertID: id, VersionID: expected, Name: e.Name, State: e.State}
		return json.Unmarshal(body, &out.SixSection)
	})
	return out, err
}

type ExpertDeleteInput struct {
	ExpertID          string `json:"expertId"`
	ExpectedVersionID string `json:"expectedVersionId"`
	ConfirmToken      string `json:"confirmToken"`
}

func ExpertDeleteToken(id, version string) string {
	return m8core.DigestOf("expert.delete|" + id + "|" + version)
}

// Delete leaves the WORM versions and audit evidence intact. Reference checks
// and the tombstone share the writer transaction, including session mounts.
func (s *ExpertService) Delete(ctx context.Context, in ExpertDeleteInput) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	if len(in.ExpertID) != 26 || len(in.ExpectedVersionID) != 26 || in.ConfirmToken != ExpertDeleteToken(in.ExpertID, in.ExpectedVersionID) {
		return ErrPayloadInvalid
	}
	return s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(in.ExpertID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		if err := s.ownManual(e); err != nil {
			return err
		}
		if e.CurrentVersionID != in.ExpectedVersionID {
			return ErrExpertVersionConflict
		}
		if e.DeletedAt != "" {
			return nil
		}
		mounts, err := tx.ListMountingsByExpert(e.ExpertID)
		if err != nil {
			return err
		}
		for _, m := range mounts {
			if m.State == m8core.MountingMounted {
				return ErrExpertInUse
			}
		}
		mounted, err := tx.HasExpertSessionMounts(e.ExpertID)
		if err != nil {
			return err
		}
		if mounted {
			return ErrExpertInUse
		}
		now := s.clock.Now().UTC().Format(time.RFC3339Nano)
		e.State, e.DeletedAt, e.UpdatedAt = m8core.ExpertArchived, now, now
		if err := tx.PutExpert(e); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "expert.delete", ResourceType: "expert", ResourceID: e.ExpertID, Actor: s.subject, CreatedAt: now})
		return err
	})
}
