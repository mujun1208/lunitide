package m8app

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// MountInput is the expert.mount command.
type MountInput struct {
	ProjectID string
	PhaseKey  string
	ExpertID  string
	Action    string
	Actor     string
}

// MountResult is the expert.mount outcome.
type MountResult struct {
	MountingID  string `json:"mountingId"`
	State       string `json:"state"`
	ActiveCount int    `json:"activeCount"`
}

// ExpertMountingView is one mounting.get matrix row projection.
type ExpertMountingView struct {
	MountingID  string `json:"mountingId"`
	ExpertID    string `json:"expertId"`
	VersionID   string `json:"versionId"`
	Semver      string `json:"semver"`
	State       string `json:"state"`
	ExpertState string `json:"expertState"`
}

// Mount enacts the mounting transitions. mount demands the expert enabled
// (M8-045) and the per-phase count <4 (M8-044; the migration trigger
// backs the insert path); the mounting pins the current version. unmount
// flips mounted->unmounted. updateVersion re-pins the mounting onto the
// expert's current version (explicit upgrade - revisions never drift an
// active mounting implicitly).
func (s *ExpertService) Mount(ctx context.Context, in MountInput) (MountResult, error) {
	if s == nil || s.uow == nil {
		return MountResult{}, ErrServiceUnavailable
	}
	if len(in.ProjectID) != 26 || len(in.ExpertID) != 26 ||
		!m8core.ValidPhaseKey(in.PhaseKey) ||
		(in.Action != "mount" && in.Action != "unmount" && in.Action != "updateVersion") {
		return MountResult{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out MountResult
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(in.ExpertID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		existing, has, err := tx.GetMounting(in.ProjectID, in.PhaseKey, in.ExpertID)
		if err != nil {
			return err
		}
		switch in.Action {
		case "mount":
			if e.State != m8core.ExpertEnabled {
				return ErrExpertNotMountable
			}
			// M8-044 app pre-check (the trigger backs the insert path only).
			counted := 0
			if has && existing.State == m8core.MountingMounted {
				counted = 1
			}
			n, err := tx.CountMountedInPhase(in.ProjectID, in.PhaseKey)
			if err != nil {
				return err
			}
			if n-counted >= m8core.MaxMountedExpertsPerPhase {
				return ErrMountLimitExceeded
			}
			if has {
				existing.State = m8core.MountingMounted
				existing.VersionID = e.CurrentVersionID
				existing.UpdatedAt = now
				if err := tx.PutMounting(existing); err != nil {
					return err
				}
				out.MountingID = existing.MountingID
			} else {
				m := m8core.ExpertMounting{
					MountingID: ulid.Make().String(), ProjectID: in.ProjectID,
					PhaseKey: in.PhaseKey, ExpertID: in.ExpertID,
					VersionID: e.CurrentVersionID, State: m8core.MountingMounted,
					MountedAt: now, UpdatedAt: now,
				}
				if err := tx.PutMounting(m); err != nil {
					return err
				}
				out.MountingID = m.MountingID
			}
			out.State = m8core.MountingMounted
		case "unmount":
			if !has {
				return ErrExpertNotFound
			}
			if existing.State == m8core.MountingMounted {
				existing.State, existing.UpdatedAt = m8core.MountingUnmounted, now
				if err := tx.PutMounting(existing); err != nil {
					return err
				}
			}
			out.MountingID = existing.MountingID
			out.State = m8core.MountingUnmounted
		case "updateVersion":
			if !has {
				return ErrExpertNotFound
			}
			if e.State == m8core.ExpertArchived {
				return ErrExpertNotMountable
			}
			existing.VersionID, existing.UpdatedAt = e.CurrentVersionID, now
			if err := tx.PutMounting(existing); err != nil {
				return err
			}
			out.MountingID = existing.MountingID
			out.State = existing.State
		}
		var cerr error
		out.ActiveCount, cerr = tx.CountMountedInPhase(in.ProjectID, in.PhaseKey)
		if cerr != nil {
			return cerr
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "expert.mount." + in.Action,
			ResourceType: "expert_mounting", ResourceID: out.MountingID,
			Actor: actorOr(in.Actor), CreatedAt: now,
		}); err != nil {
			return err
		}
		return nil
	})
	return out, err
}

// MountingGetInput is the expert.mounting.get command.
type MountingGetInput struct {
	ProjectID string
	PhaseKey  string
}

// PhaseDefaultView is one suggested default expert.
type PhaseDefaultView struct {
	ExpertID string `json:"expertId"`
	Division string `json:"division"`
}

// PhaseMatrixRow is one matrix phase row.
type PhaseMatrixRow struct {
	PhaseKey  string              `json:"phaseKey"`
	Defaults  []PhaseDefaultView  `json:"defaults"`
	Mountings []ExpertMountingDTO `json:"mountings"`
}

// ExpertMountingDTO is one mounting projection.
type ExpertMountingDTO struct {
	MountingID  string `json:"mountingId"`
	ExpertID    string `json:"expertId"`
	VersionID   string `json:"versionId"`
	Semver      string `json:"semver"`
	State       string `json:"state"`
	ExpertState string `json:"expertState"`
}

// MountingGetResult is the expert.mounting.get outcome.
type MountingGetResult struct {
	Matrix []PhaseMatrixRow `json:"matrix"`
}

// MountingGet answers the nine-phase mounting matrix (or one phase when
// phaseKey is given). Defaults are the M7 recommended division mapping
// projected onto enabled, not-yet-mounted experts - advisory suggestions
// only; nothing is written silently (the caller confirms before any
// expert.mount lands).
func (s *ExpertService) MountingGet(ctx context.Context, in MountingGetInput) (MountingGetResult, error) {
	if s == nil || s.uow == nil {
		return MountingGetResult{}, ErrServiceUnavailable
	}
	if len(in.ProjectID) != 26 {
		return MountingGetResult{}, ErrPayloadInvalid
	}
	if in.PhaseKey != "" && !m8core.ValidPhaseKey(in.PhaseKey) {
		return MountingGetResult{}, ErrPayloadInvalid
	}
	phases := m8core.PhaseKeys
	if in.PhaseKey != "" {
		phases = []string{in.PhaseKey}
	}
	var out MountingGetResult
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		catalog, err := tx.ListExperts(ExpertFilter{})
		if err != nil {
			return err
		}
		byDivision := map[string][]ExpertListItem{}
		for _, e := range catalog {
			if e.State == m8core.ExpertEnabled {
				byDivision[e.Division] = append(byDivision[e.Division], e)
			}
		}
		out.Matrix = make([]PhaseMatrixRow, 0, len(phases))
		for _, phase := range phases {
			row := PhaseMatrixRow{PhaseKey: phase, Defaults: []PhaseDefaultView{}, Mountings: []ExpertMountingDTO{}}
			mountings, err := tx.ListMountingsByProjectPhase(in.ProjectID, phase)
			if err != nil {
				return err
			}
			mounted := map[string]bool{}
			for _, m := range mountings {
				row.Mountings = append(row.Mountings, ExpertMountingDTO(m))
				if m.State == m8core.MountingMounted {
					mounted[m.ExpertID] = true
				}
			}
			for _, d := range m8core.DefaultPhaseMapping {
				if d.PhaseKey != phase {
					continue
				}
				for _, division := range d.Divisions {
					for _, cand := range byDivision[division] {
						if mounted[cand.ExpertID] {
							continue
						}
						if len(row.Defaults) >= m8core.MaxMountedExpertsPerPhase {
							break
						}
						row.Defaults = append(row.Defaults, PhaseDefaultView{
							ExpertID: cand.ExpertID, Division: cand.Division,
						})
					}
				}
			}
			out.Matrix = append(out.Matrix, row)
		}
		return nil
	})
	return out, err
}
