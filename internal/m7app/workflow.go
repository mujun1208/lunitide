// Package m7app implements the M7 application services. Slice 1 (T-7.1.3/
// T-7.1.4): the nine-stage workflow service — createVersion/publish,
// startStage with the active-attempt guard, captureInput with canonical
// snapshots and the M6 final-tree adaptation check.
package m7app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrServiceUnavailable: unit of work missing.
	ErrServiceUnavailable = errors.New("m7app: unit of work unavailable")
	// ErrProjectNotFound: the referenced project row does not exist.
	ErrProjectNotFound = errors.New("m7app: project not found")
	// ErrInstanceNotFound / ErrStageRunNotFound / ErrVersionNotFound.
	ErrInstanceNotFound = errors.New("m7app: workflow instance not found")
	ErrStageRunNotFound = errors.New("m7app: stage run not found")
	ErrVersionNotFound  = errors.New("m7app: workflow version not found")
	// ErrDuplicateVersion: UNIQUE(project, version) conflict (WF-005).
	ErrDuplicateVersion = errors.New("m7app: workflow version already exists")
	// ErrActiveAttempt: an active attempt already exists for the stage —
	// answer it instead of starting a second one (idempotent start).
	ErrActiveAttempt = errors.New("m7app: active stage attempt exists")
	// ErrDependencyIncomplete: upstream stages not completed yet; the run
	// stays draft until the DAG is satisfied.
	ErrDependencyIncomplete = errors.New("m7app: stage dependencies not completed")
	// ErrSnapshotChanged: the captured inputs differ from the frozen
	// snapshot (SNP-002 — mark stale, never silently reuse).
	ErrSnapshotChanged = errors.New("m7app: stage inputs changed after capture")
	// ErrM6TreeDigest: M6 adaptation — the m6.finalTree input lacks a
	// rootId/final-tree digest pair (SNP-001 semantics for M6 sources).
	ErrM6TreeDigest = errors.New("m7app: m6 final-tree input missing rootId/digest")
	// ErrAlreadyPublished: cannot mutate a published version in place
	// (WF-001 — clone a new version instead).
	ErrAlreadyPublished = errors.New("m7app: workflow version already published")
	// ErrNotPublished: instances can only bind published versions.
	ErrNotPublished = errors.New("m7app: workflow version not published")
	// ErrVersionConflict: optimistic-lock mismatch on stage runs (WF-004).
	ErrVersionConflict = errors.New("m7app: stage run version conflict")
	// ErrIllegalTransition: the requested StageRun state change is not in
	// the canonical state machine.
	ErrIllegalTransition = errors.New("m7app: illegal stage run transition")
)

// WorkflowTx is the M7 single-writer transaction (sqlite agentRuntimeTx
// satisfies it).
type WorkflowTx interface {
	GetProject(id string) (string, error) // returns project id or ""
	MaxWorkflowVersion(projectID string) (int64, error)
	PutWorkflowVersion(m7flow.WorkflowVersion) error
	GetWorkflowVersion(id string) (m7flow.WorkflowVersion, error)
	FindPublishedWorkflowVersion(projectID string) (m7flow.WorkflowVersion, error)
	PublishWorkflowVersion(id string, at time.Time) error
	PutStageDefinitions(versionID string, defs []m7flow.StageDefinition) error
	ListStageDefinitions(versionID string) ([]m7flow.StageDefinition, error)
	PutWorkflowInstance(m7flow.WorkflowInstance) error
	GetWorkflowInstance(id string) (m7flow.WorkflowInstance, error)
	FindRunningInstance(projectID string) (m7flow.WorkflowInstance, error)
	PutStageRun(m7flow.StageRun) error
	GetStageRun(id string) (m7flow.StageRun, error)
	FindActiveStageRun(instanceID, stageDefID string) (m7flow.StageRun, error)
	MaxStageAttempt(instanceID, stageDefID string) (int64, error)
	LatestStageRunState(instanceID, stageDefID string) (string, error)
	UpdateStageRunState(id string, expectedVersion int64, to string, at time.Time, completed bool) (m7flow.StageRun, error)
	PutInputSnapshot(m7flow.InputSnapshot) error
	LatestInputSnapshot(stageRunID string) (m7flow.InputSnapshot, error)
	PutArtifactVersion(m7flow.ArtifactVersion) error
	FindArtifactVersion(artifactID string, versionNo int64) (m7flow.ArtifactVersion, error)
	MaxArtifactVersion(artifactID string) (int64, error)
}

// WorkflowUnitOfWork is the M7 single-writer boundary.
type WorkflowUnitOfWork interface {
	TransactM7(ctx context.Context, fn func(WorkflowTx) error) error
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// WorkflowService implements workflow.createVersion / startStage /
// captureInput (slice 1).
type WorkflowService struct {
	uow   WorkflowUnitOfWork
	clock Clock
}

func NewWorkflowService(uow WorkflowUnitOfWork) *WorkflowService {
	return &WorkflowService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *WorkflowService) SetClock(c Clock) { s.clock = c }

// CreateVersion seeds the fixed nine stages for a project as a new draft
// version (next version number). The definition digest binds the exact
// content; publish freezes it (WF-001 afterwards).
func (s *WorkflowService) CreateVersion(ctx context.Context, projectID string) (m7flow.WorkflowVersion, error) {
	if s == nil || s.uow == nil {
		return m7flow.WorkflowVersion{}, ErrServiceUnavailable
	}
	var out m7flow.WorkflowVersion
	err := s.uow.TransactM7(ctx, func(tx WorkflowTx) error {
		if _, err := tx.GetProject(projectID); err != nil {
			return ErrProjectNotFound
		}
		next, err := tx.MaxWorkflowVersion(projectID)
		if err != nil {
			return err
		}
		defs := m7flow.FixedStages()
		if err := m7flow.ValidateFixedSet(defs); err != nil {
			return err // WF-002/WF-003 (subprocess as stage also lands here)
		}
		now := s.clock.Now().UTC()
		out = m7flow.WorkflowVersion{
			ID: ulid.Make().String(), ProjectID: projectID, Version: next + 1,
			Status: m7flow.WVDraft, DefinitionDigest: m7flow.DefinitionDigest(defs),
			CreatedAt: now,
		}
		if err := tx.PutWorkflowVersion(out); err != nil {
			return fmt.Errorf("%w: %v", ErrDuplicateVersion, err)
		}
		return tx.PutStageDefinitions(out.ID, defs)
	})
	return out, err
}

// Publish freezes a draft version (published versions are read-only;
// changes require cloning a new version — WF-001).
func (s *WorkflowService) Publish(ctx context.Context, versionID string) (m7flow.WorkflowVersion, error) {
	if s == nil || s.uow == nil {
		return m7flow.WorkflowVersion{}, ErrServiceUnavailable
	}
	var out m7flow.WorkflowVersion
	err := s.uow.TransactM7(ctx, func(tx WorkflowTx) error {
		v, err := tx.GetWorkflowVersion(versionID)
		if err != nil {
			return ErrVersionNotFound
		}
		if v.Status == m7flow.WVPublished {
			out = v
			return nil // idempotent
		}
		defs, err := tx.ListStageDefinitions(versionID)
		if err != nil {
			return err
		}
		if err := m7flow.ValidateFixedSet(defs); err != nil {
			return err
		}
		if d := m7flow.DefinitionDigest(defs); d != v.DefinitionDigest {
			return fmt.Errorf("%w: definition digest drift", ErrAlreadyPublished)
		}
		if err := tx.PublishWorkflowVersion(versionID, s.clock.Now().UTC()); err != nil {
			return err
		}
		out, err = tx.GetWorkflowVersion(versionID)
		return err
	})
	return out, err
}

// StartInstance binds a project to its published workflow version (one
// running instance per project; the existing running instance is answered
// idempotently).
func (s *WorkflowService) StartInstance(ctx context.Context, projectID string) (m7flow.WorkflowInstance, error) {
	if s == nil || s.uow == nil {
		return m7flow.WorkflowInstance{}, ErrServiceUnavailable
	}
	var out m7flow.WorkflowInstance
	err := s.uow.TransactM7(ctx, func(tx WorkflowTx) error {
		if _, err := tx.GetProject(projectID); err != nil {
			return ErrProjectNotFound
		}
		if inst, err := tx.FindRunningInstance(projectID); err == nil {
			out = inst
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		v, err := tx.FindPublishedWorkflowVersion(projectID)
		if err != nil {
			return fmt.Errorf("%w: no published version", ErrNotPublished)
		}
		out = m7flow.WorkflowInstance{
			ID: ulid.Make().String(), ProjectID: projectID,
			WorkflowVersionID: v.ID, State: m7flow.InstanceRunning, CreatedAt: s.clock.Now().UTC(),
		}
		return tx.PutWorkflowInstance(out)
	})
	return out, err
}

// StageRunResult carries the existing-or-new run plus its definition.
type StageRunResult struct {
	Run       m7flow.StageRun
	Def       m7flow.StageDefinition
	Instance  m7flow.WorkflowInstance
	NewRun    bool
	Dependent bool // true when the run stayed draft on incomplete deps
}

// StartStage opens (or answers) the single active attempt of one stage for a
// project's running instance, creating the instance on first use. A stage
// whose direct dependencies have no completed attempt stays draft
// (WF-004/WF-005 map onto ErrVersionConflict/ErrDuplicateVersion at the wire).
func (s *WorkflowService) StartStage(ctx context.Context, projectID, stageKey string) (StageRunResult, error) {
	if s == nil || s.uow == nil {
		return StageRunResult{}, ErrServiceUnavailable
	}
	var out StageRunResult
	err := s.uow.TransactM7(ctx, func(tx WorkflowTx) error {
		if _, err := tx.GetProject(projectID); err != nil {
			return ErrProjectNotFound
		}
		inst, err := tx.FindRunningInstance(projectID)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
				return err
			}
			v, verr := tx.FindPublishedWorkflowVersion(projectID)
			if verr != nil {
				return fmt.Errorf("%w: no published version", ErrNotPublished)
			}
			inst = m7flow.WorkflowInstance{
				ID: ulid.Make().String(), ProjectID: projectID,
				WorkflowVersionID: v.ID, State: m7flow.InstanceRunning, CreatedAt: s.clock.Now().UTC(),
			}
			if err := tx.PutWorkflowInstance(inst); err != nil {
				return err
			}
		}
		out.Instance = inst
		defs, err := tx.ListStageDefinitions(inst.WorkflowVersionID)
		if err != nil {
			return err
		}
		def := stageDefByKey(defs, stageKey)
		if def == nil {
			return fmt.Errorf("%w: %s", m7flow.ErrStageFixedSet, stageKey)
		}
		out.Def = *def
		// Active attempt answers idempotently.
		if run, err := tx.FindActiveStageRun(inst.ID, def.ID); err == nil {
			out.Run, out.NewRun = run, false
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		// Dependencies: every direct dep stage needs a completed attempt.
		var deps []string
		if err := json.Unmarshal([]byte(def.DependencyKeys), &deps); err != nil {
			return err
		}
		state := m7flow.RunReady
		for _, dep := range deps {
			depDef := stageDefByKey(defs, dep)
			if depDef == nil {
				return m7flow.ErrStageFixedSet
			}
			depState, err := tx.LatestStageRunState(inst.ID, depDef.ID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
					state, out.Dependent = m7flow.RunDraft, true
					break
				}
				return err
			}
			if depState != m7flow.RunCompleted {
				state, out.Dependent = m7flow.RunDraft, true
				break
			}
		}
		attempt, err := tx.MaxStageAttempt(inst.ID, def.ID)
		if err != nil {
			return err
		}
		run := m7flow.StageRun{
			ID: ulid.Make().String(), InstanceID: inst.ID, StageDefinitionID: def.ID,
			AttemptNo: attempt + 1, State: state, LockVersion: 1, CreatedAt: s.clock.Now().UTC(),
		}
		if err := tx.PutStageRun(run); err != nil {
			return err
		}
		got, err := tx.GetStageRun(run.ID)
		if err != nil {
			return err
		}
		out.Run, out.NewRun = got, true
		return nil
	})
	return out, err
}

func stageDefByKey(defs []m7flow.StageDefinition, key string) *m7flow.StageDefinition {
	for i := range defs {
		if defs[i].StageKey == key {
			return &defs[i]
		}
	}
	return nil
}

// TransitionStage applies one canonical StageRun state change under the
// optimistic lock (WF-004). Terminal states stamp completed_at.
func (s *WorkflowService) TransitionStage(ctx context.Context, stageRunID, to string) (m7flow.StageRun, error) {
	return s.TransitionStageChecked(ctx, stageRunID, to, 0)
}

// TransitionStageChecked additionally verifies the caller's expected
// lock_version before applying the transition (wire-level optimistic lock;
// expectedVersion 0 skips the pre-check for internal callers).
func (s *WorkflowService) TransitionStageChecked(ctx context.Context, stageRunID, to string, expectedVersion int64) (m7flow.StageRun, error) {
	if s == nil || s.uow == nil {
		return m7flow.StageRun{}, ErrServiceUnavailable
	}
	var out m7flow.StageRun
	err := s.uow.TransactM7(ctx, func(tx WorkflowTx) error {
		run, err := tx.GetStageRun(stageRunID)
		if err != nil {
			return ErrStageRunNotFound
		}
		if expectedVersion > 0 && run.LockVersion != expectedVersion {
			return fmt.Errorf("%w: expected lock_version %d, current %d", ErrVersionConflict, expectedVersion, run.LockVersion)
		}
		if !m7flow.LegalRunTransition(run.State, to) {
			return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, run.State, to)
		}
		now := s.clock.Now().UTC()
		out, err = tx.UpdateStageRunState(stageRunID, run.LockVersion, to, now, m7flow.IsTerminalRun(to))
		if err != nil {
			return fmt.Errorf("%w: %v", ErrVersionConflict, err)
		}
		return nil
	})
	return out, err
}

// CaptureInput freezes the canonical snapshot of a stage run's inputs.
// M6 adaptation: when inputs contain an "m6.finalTree" entry it must carry
// both rootId and a 64-hex final-tree digest, else the snapshot is invalid
// (missing/failed inputs never become usable snapshots).
func (s *WorkflowService) CaptureInput(ctx context.Context, stageRunID string, inputs map[string]any) (m7flow.InputSnapshot, error) {
	if s == nil || s.uow == nil {
		return m7flow.InputSnapshot{}, ErrServiceUnavailable
	}
	if err := validateSnapshotInputs(inputs); err != nil {
		return m7flow.InputSnapshot{}, err
	}
	canonical, digest, err := m7flow.NormalizeInputs(inputs)
	if err != nil {
		return m7flow.InputSnapshot{}, err
	}
	var out m7flow.InputSnapshot
	err = s.uow.TransactM7(ctx, func(tx WorkflowTx) error {
		run, err := tx.GetStageRun(stageRunID)
		if err != nil {
			return ErrStageRunNotFound
		}
		if m7flow.IsTerminalRun(run.State) {
			return ErrStageRunNotFound
		}
		// Recapture on the same digest is idempotent; a different digest on a
		// non-draft run is a stale transition (SNP-002).
		if latest, err := tx.LatestInputSnapshot(stageRunID); err == nil {
			if latest.Digest == digest {
				out = latest
				return nil
			}
			if run.State != m7flow.RunDraft && run.State != m7flow.RunReady {
				return ErrSnapshotChanged
			}
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		out = m7flow.InputSnapshot{
			ID: ulid.Make().String(), StageRunID: stageRunID,
			InputsJSON: canonical, Digest: digest, CapturedAt: s.clock.Now().UTC(),
		}
		return tx.PutInputSnapshot(out)
	})
	return out, err
}

// PutArtifact registers a new immutable artifact version. Superseding an
// existing artifact is a new version_no row — never an edit (M7-ART-001).
func (s *WorkflowService) PutArtifact(ctx context.Context, art m7flow.ArtifactVersion) (m7flow.ArtifactVersion, error) {
	if s == nil || s.uow == nil {
		return m7flow.ArtifactVersion{}, ErrServiceUnavailable
	}
	if art.State == "" {
		art.State = m7flow.ArtifactActive
	}
	var out m7flow.ArtifactVersion
	err := s.uow.TransactM7(ctx, func(tx WorkflowTx) error {
		if art.VersionNo == 0 {
			next, err := tx.MaxArtifactVersion(art.ArtifactID)
			if err != nil {
				return err
			}
			art.VersionNo = next + 1
		}
		if _, err := tx.FindArtifactVersion(art.ArtifactID, art.VersionNo); err == nil {
			return fmt.Errorf("%w: %s v%d", ErrDuplicateVersion, art.ArtifactID, art.VersionNo)
		}
		if art.ID == "" {
			art.ID = ulid.Make().String()
		}
		if art.CreatedAt.IsZero() {
			art.CreatedAt = s.clock.Now().UTC()
		}
		out = art
		return tx.PutArtifactVersion(art)
	})
	return out, err
}

// validateSnapshotInputs enforces the M6 final-tree adaptation: every entry
// named "m6.finalTree" must be an object with rootId and a 64-hex digest.
// The digest key accepts both the M7-native "digest" and the M6-native
// "finalDigest" (merge.finalize payload field), so a real M6 payload can
// flow into captureInput unmodified.
func validateSnapshotInputs(inputs map[string]any) error {
	v, ok := inputs["m6.finalTree"]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return ErrM6TreeDigest
	}
	rootID, _ := m["rootId"].(string)
	digest, _ := m["digest"].(string)
	if digest == "" {
		digest, _ = m["finalDigest"].(string)
	}
	if rootID == "" || len(digest) != 64 || !isLowerHex(digest) {
		return ErrM6TreeDigest
	}
	return nil
}

func isLowerHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
