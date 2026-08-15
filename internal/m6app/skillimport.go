// Legacy S8 skill import application service: the governed
// discovered -> pinned -> inspected -> scanned -> evaluated ->
// awaiting_approval -> approved pipeline with one audited CAS step per
// transition, plus the approval gate that materializes the skill entity
// chain (skill + version + dependencies) from an approved candidate.
//
// Every step runs inside the agent-runtime single-writer transaction so
// the candidate row, its audit record and (on approval) the skill chain
// commit atomically. Approval is the only step that writes the skill
// tables; rejection and revocation never touch them.
package m6app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrCandidateNotFound: the import candidate does not exist.
	ErrCandidateNotFound = errors.New("m6app: import candidate not found")
	// ErrCandidateExists: (sourceUrl, immutableCommit) already discovered.
	ErrCandidateExists = errors.New("m6app: import candidate already exists")
	// ErrSkillNotFound: the skill row does not exist.
	ErrSkillNotFound = errors.New("m6app: skill not found")
	// ErrSkillVersionExists: the (skill, semver) version already pinned.
	ErrSkillVersionExists = errors.New("m6app: skill version already pinned")
	// ErrSkillInstallNotFound: the install row does not exist.
	ErrSkillInstallNotFound = errors.New("m6app: skill install not found")
)

// SkillImportService implements the governed import pipeline.
type SkillImportService struct {
	uow   UnitOfWork
	clock Clock
}

func NewSkillImportService(uow UnitOfWork) *SkillImportService {
	return &SkillImportService{uow: uow, clock: systemClock{}}
}

func (s *SkillImportService) SetClock(c Clock) { s.clock = c }

func (s *SkillImportService) available() error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return nil
}

// DiscoverInput is the discovery payload (skill.import.discover).
type DiscoverInput struct {
	AssetType       string
	SourceURL       string
	ImmutableCommit string
	ArchiveHash     string
	License         string
	NoticeRef       string
	Publisher       string
	Signature       string
}

// Discover records a candidate at the head of the pipeline
// (skill.import.discovered audit).
func (s *SkillImportService) Discover(ctx context.Context, in DiscoverInput) (m6supply.ImportCandidate, error) {
	if err := s.available(); err != nil {
		return m6supply.ImportCandidate{}, err
	}
	if err := m6supply.ValidateImportInput(in.AssetType, in.SourceURL, in.ImmutableCommit, in.ArchiveHash, in.License, in.Publisher); err != nil {
		return m6supply.ImportCandidate{}, err
	}
	var out m6supply.ImportCandidate
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.FindM6ImportCandidate(in.SourceURL, in.ImmutableCommit); err == nil {
			return ErrCandidateExists
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.ImportCandidate{
			ID: ulid.Make().String(), AssetType: in.AssetType,
			SourceURL: in.SourceURL, ImmutableCommit: in.ImmutableCommit,
			ArchiveHash: in.ArchiveHash, License: in.License,
			NoticeRef: in.NoticeRef, Publisher: in.Publisher, Signature: in.Signature,
			State: m6supply.ImportDiscovered, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6ImportCandidate(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "skill.import.discovered",
			AggregateID: out.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				CandidateID string `json:"candidateId"`
				AssetType   string `json:"assetType"`
				SourceURL   string `json:"sourceUrl"`
				Commit      string `json:"immutableCommit"`
			}{CandidateID: out.ID, AssetType: out.AssetType, SourceURL: out.SourceURL, Commit: out.ImmutableCommit}),
		})
	})
	return out, err
}

// step advances the pipeline by exactly one governed transition. The
// state-specific use cases (Pin/Inspect/Scan/Evaluate/Approve/Reject/
// Revoke) all funnel through here so the CAS + audit pairing cannot drift.
func (s *SkillImportService) step(ctx context.Context, id string, expectedVersion int64, to string, evidence m6supply.ImportEvidence, extra func(tx Tx, cur, next m6supply.ImportCandidate) error) (m6supply.ImportCandidate, error) {
	if err := s.available(); err != nil {
		return m6supply.ImportCandidate{}, err
	}
	var out m6supply.ImportCandidate
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		cur, err := tx.GetM6ImportCandidate(id)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrCandidateNotFound
		}
		if err != nil {
			return err
		}
		if cur.State == to {
			out = cur
			return nil
		}
		if !m6supply.ImportTransitionAllowed(cur.State, to) {
			return m6supply.ErrInvalidTransition
		}
		now := s.clock.Now().UTC()
		next, err := tx.TransitionM6ImportCandidate(id, expectedVersion, to, evidence, now)
		if errors.Is(err, m6supply.ErrVersionConflict) {
			return err
		}
		if err != nil {
			return err
		}
		if extra != nil {
			if err := extra(tx, cur, next); err != nil {
				return err
			}
		}
		out = next
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: m6supply.ImportAuditAction(to),
			AggregateID: id, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				CandidateID string `json:"candidateId"`
				From        string `json:"from"`
				To          string `json:"to"`
			}{CandidateID: id, From: cur.State, To: to}),
		})
	})
	return out, err
}

// Pin freezes the immutable source coordinates (skill.import.pinned).
func (s *SkillImportService) Pin(ctx context.Context, id string, expectedVersion int64, evidence m6supply.ImportEvidence) (m6supply.ImportCandidate, error) {
	return s.step(ctx, id, expectedVersion, m6supply.ImportPinned, evidence, nil)
}

// Inspect records license/notice/signature evidence
// (skill.import.inspected).
func (s *SkillImportService) Inspect(ctx context.Context, id string, expectedVersion int64, evidence m6supply.ImportEvidence) (m6supply.ImportCandidate, error) {
	return s.step(ctx, id, expectedVersion, m6supply.ImportInspected, evidence, nil)
}

// Scan attaches the scan references (skill.import.scanned). Empty scan
// evidence is refused: a scan step without a scan record is governance
// theater.
func (s *SkillImportService) Scan(ctx context.Context, id string, expectedVersion int64, evidence m6supply.ImportEvidence) (m6supply.ImportCandidate, error) {
	if evidence.ScanRefs == "" {
		return m6supply.ImportCandidate{}, errors.New("m6app: scanRefs required at the scanned step")
	}
	return s.step(ctx, id, expectedVersion, m6supply.ImportScanned, evidence, nil)
}

// Evaluate attaches the evaluation reference (skill.import.evaluated).
func (s *SkillImportService) Evaluate(ctx context.Context, id string, expectedVersion int64, evidence m6supply.ImportEvidence) (m6supply.ImportCandidate, error) {
	if evidence.EvaluationID == "" {
		return m6supply.ImportCandidate{}, errors.New("m6app: evaluationId required at the evaluated step")
	}
	return s.step(ctx, id, expectedVersion, m6supply.ImportEvaluated, evidence, nil)
}

// Submit moves evaluated -> awaiting_approval (audited as the approval
// request; the catalog folds submit into the approve action).
func (s *SkillImportService) Submit(ctx context.Context, id string, expectedVersion int64, evidence m6supply.ImportEvidence) (m6supply.ImportCandidate, error) {
	return s.step(ctx, id, expectedVersion, m6supply.ImportAwaitingApproval, evidence, nil)
}

// Reject terminates the walk from any pre-approval state
// (skill.import.rejected).
func (s *SkillImportService) Reject(ctx context.Context, id string, expectedVersion int64, evidence m6supply.ImportEvidence) (m6supply.ImportCandidate, error) {
	return s.step(ctx, id, expectedVersion, m6supply.ImportRejected, evidence, nil)
}

// ApproveInput carries the approval record and (for skills) the parsed
// manifest that materializes the entity chain.
type ApproveInput struct {
	CandidateID     string
	ExpectedVersion int64
	Approval        string // JSON approval record
	Manifest        []byte // required for assetType == skill
}

// Approve is the install gate: awaiting_approval -> approved, audited as
// skill.import.approved, and — for skill assets — the materialization of
// the skill + version + dependency chain in the same transaction.
func (s *SkillImportService) Approve(ctx context.Context, in ApproveInput) (m6supply.ImportCandidate, error) {
	if in.Approval == "" || !jsonObject(in.Approval) {
		return m6supply.ImportCandidate{}, errors.New("m6app: approval record must be a JSON object")
	}
	return s.step(ctx, in.CandidateID, in.ExpectedVersion, m6supply.ImportApproved,
		m6supply.ImportEvidence{Approval: in.Approval},
		func(tx Tx, cur, next m6supply.ImportCandidate) error {
			if cur.AssetType != m6supply.AssetSkill {
				return nil
			}
			manifest, err := m6supply.ParseManifest(in.Manifest)
			if err != nil {
				return err
			}
			if manifest.Publisher != cur.Publisher {
				return fmt.Errorf("m6app: manifest publisher %q does not match candidate publisher %q", manifest.Publisher, cur.Publisher)
			}
			return materializeSkill(tx, cur, manifest, s.clock.Now().UTC())
		})
}

// Revoke is terminal cleanup after approval or rejection
// (skill.import.revoked).
func (s *SkillImportService) Revoke(ctx context.Context, id string, expectedVersion int64, evidence m6supply.ImportEvidence) (m6supply.ImportCandidate, error) {
	return s.step(ctx, id, expectedVersion, m6supply.ImportRevoked, evidence, nil)
}

// materializeSkill writes the skill entity chain for an approved skill
// candidate: a new skill head (or the existing one), the pinned version
// and its dependency edges. Called inside the approval transaction.
func materializeSkill(tx Tx, c m6supply.ImportCandidate, m *m6supply.Manifest, now time.Time) error {
	permissionsJSON, err := json.Marshal(m.Permissions)
	if err != nil {
		return err
	}
	sk, err := tx.FindM6SkillByName(m.Name)
	if errors.Is(err, m6supply.ErrNotFound) {
		sk = m6supply.Skill{
			ID: ulid.Make().String(), Name: m.Name, Publisher: m.Publisher,
			Status: m6supply.SkillVerified, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6Skill(sk); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := tx.FindM6SkillVersion(sk.ID, m.Version); err == nil {
		return ErrSkillVersionExists
	} else if !errors.Is(err, m6supply.ErrNotFound) {
		return err
	}
	version := m6supply.SkillVersion{
		ID: ulid.Make().String(), SkillID: sk.ID, Semver: m.Version,
		ManifestRef: fmt.Sprintf("candidate://%s/%s", c.ID, c.ImmutableCommit),
		PackageHash: c.ArchiveHash, SignatureStatus: m6supply.SignatureUnverified,
		PermissionsJSON: string(permissionsJSON), CreatedAt: now,
	}
	if c.Signature != "" {
		version.SignatureStatus = m6supply.SignatureVerified
	}
	if err := tx.PutM6SkillVersion(version); err != nil {
		return err
	}
	for _, dep := range m.Dependencies {
		d := m6supply.SkillDependency{
			ID: ulid.Make().String(), SkillVersionID: version.ID,
			DependencyType: dep.Type, Name: dep.Name,
			VersionConstraint: dep.VersionConstraint, CreatedAt: now,
		}
		if err := tx.PutM6SkillDependency(d); err != nil {
			return err
		}
	}
	return tx.SetM6SkillCurrentVersion(sk.ID, version.ID, now)
}

// InstallSkill binds an approved skill version into a workspace.
func (s *SkillImportService) InstallSkill(ctx context.Context, skillVersionID, workspaceID string) (m6supply.SkillInstall, error) {
	if err := s.available(); err != nil {
		return m6supply.SkillInstall{}, err
	}
	if len(workspaceID) < 1 || len(workspaceID) > 256 {
		return m6supply.SkillInstall{}, errors.New("m6app: workspaceId length must be 1..256")
	}
	var out m6supply.SkillInstall
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		v, err := tx.GetM6SkillVersion(skillVersionID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrSkillNotFound
		}
		if err != nil {
			return err
		}
		if existing, ferr := tx.FindM6SkillInstall(skillVersionID, workspaceID); ferr == nil {
			out = existing // idempotent: rebinding the same pair replays as success
			return nil
		} else if !errors.Is(ferr, m6supply.ErrNotFound) {
			return ferr
		}
		now := s.clock.Now().UTC()
		out = m6supply.SkillInstall{
			ID: ulid.Make().String(), SkillVersionID: v.ID, WorkspaceID: workspaceID,
			Status: m6supply.SkillInstallInstalled, InstalledAt: now,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6SkillInstall(out); err != nil {
			return err
		}
		_, err = tx.TransitionM6SkillState(v.SkillID, m6supply.SkillInstalled, now)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrSkillNotFound
		}
		return err
	})
	return out, err
}

// RecordTrigger appends one trigger decision row (session telemetry).
func (s *SkillImportService) RecordTrigger(ctx context.Context, sessionID, skillVersionID string, score float64, reason, status, resultRef string) (m6supply.SkillTrigger, error) {
	if err := s.available(); err != nil {
		return m6supply.SkillTrigger{}, err
	}
	if err := m6supply.ValidateSkillTriggerInput(score, status); err != nil {
		return m6supply.SkillTrigger{}, err
	}
	if len(sessionID) < 1 || len(sessionID) > 256 {
		return m6supply.SkillTrigger{}, errors.New("m6app: sessionId length must be 1..256")
	}
	if len(reason) < 1 || len(reason) > 2048 {
		return m6supply.SkillTrigger{}, errors.New("m6app: reason length must be 1..2048")
	}
	var out m6supply.SkillTrigger
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.GetM6SkillVersion(skillVersionID); err != nil {
			if errors.Is(err, m6supply.ErrNotFound) {
				return ErrSkillNotFound
			}
			return err
		}
		out = m6supply.SkillTrigger{
			ID: ulid.Make().String(), SessionID: sessionID,
			SkillVersionID: skillVersionID, Score: score,
			Reason: reason, Status: status, ResultRef: resultRef,
			CreatedAt: s.clock.Now().UTC(),
		}
		return tx.PutM6SkillTrigger(out)
	})
	return out, err
}
