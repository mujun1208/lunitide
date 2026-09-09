package m6app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/skillarchive"
	"github.com/oklog/ulid/v2"
)

var ErrImportChanged = errors.New("skill import archive changed")
var ErrImportScan = errors.New("skill import static scan rejected")
var ErrImportRuntimeMissing = errors.New("approved import runtime skill is missing")

type SkillSource interface {
	Load(context.Context, string, string) (skillarchive.Package, error)
}
type importSkillWriter interface {
	PutImportedSkill(skill.Skill) error
	DisableImportedSkill(string, time.Time) error
}
type importSkillReader interface{ ImportedSkillExists(string) (bool, error) }

func (s *SkillImportService) SetSource(source SkillSource) { s.source = source }
func (s *SkillImportService) HasSource() bool              { return s != nil && s.source != nil }
func (s *SkillImportService) SetPackageRoot(root string)   { s.packageRoot = root }

func (s *SkillImportService) resolveDiscovery(ctx context.Context, in DiscoverInput) (DiscoverInput, error) {
	if in.AssetType != m6supply.AssetSkill {
		return in, fmt.Errorf("%w: 此导入入口当前支持 SKILL.md 技能", skillarchive.ErrInvalid)
	}
	p, err := s.source.Load(ctx, in.SourceURL, in.ImmutableCommit)
	if err != nil {
		return in, err
	}
	if in.ArchiveHash != "" && in.ArchiveHash != p.ArchiveHash {
		return in, ErrImportChanged
	}
	// Provenance is derived from downloaded bytes, never from renderer claims.
	return DiscoverInput{AssetType: m6supply.AssetSkill, SourceURL: p.Source.URL, ImmutableCommit: p.Source.Commit, ArchiveHash: p.ArchiveHash, License: p.License, Publisher: p.Source.Publisher, sourceAttestation: p.Attestation()}, nil
}

func (s *SkillImportService) candidate(ctx context.Context, id string) (m6supply.ImportCandidate, error) {
	if err := s.available(); err != nil {
		return m6supply.ImportCandidate{}, err
	}
	var c m6supply.ImportCandidate
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		var err error
		c, err = tx.GetM6ImportCandidate(id)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrCandidateNotFound
		}
		if err != nil {
			return err
		}
		return nil
	})
	return c, err
}

func (s *SkillImportService) reload(ctx context.Context, c m6supply.ImportCandidate) (skillarchive.Package, error) {
	if !s.HasSource() {
		return skillarchive.Package{}, ErrServiceUnavailable
	}
	p, err := s.source.Load(ctx, c.SourceURL, c.ImmutableCommit)
	if err != nil {
		return p, err
	}
	if p.ArchiveHash != c.ArchiveHash || !p.MatchesAttestation(c.SourceAttestation) {
		return p, ErrImportChanged
	}
	return p, nil
}

// InspectSource and SubmitSource commit each whole UI step atomically. Fetching
// and parsing happen before acquiring the write transaction; CAS catches edits.
func (s *SkillImportService) InspectSource(ctx context.Context, id string, expected int64) (m6supply.ImportCandidate, error) {
	c, err := s.candidate(ctx, id)
	if err != nil {
		return c, err
	}
	replay, err := sourceStepVersion(c, expected, m6supply.ImportDiscovered, m6supply.ImportInspected, 2)
	if err != nil {
		return c, err
	}
	if replay {
		return s.committedSourceResult(ctx, c)
	}
	if _, err = s.reload(ctx, c); err != nil {
		return c, err
	}
	return s.advanceSource(ctx, c, []string{m6supply.ImportPinned, m6supply.ImportInspected}, m6supply.ImportEvidence{}, nil)
}

func (s *SkillImportService) SubmitSource(ctx context.Context, id string, expected int64) (m6supply.ImportCandidate, error) {
	c, err := s.candidate(ctx, id)
	if err != nil {
		return c, err
	}
	replay, err := sourceStepVersion(c, expected, m6supply.ImportInspected, m6supply.ImportAwaitingApproval, 3)
	if err != nil {
		return c, err
	}
	if replay {
		return s.committedSourceResult(ctx, c)
	}
	p, err := s.reload(ctx, c)
	if err != nil {
		return c, err
	}
	evidence, err := scanSource(p)
	if err != nil {
		if auditErr := s.recordSourceRejection(ctx, c); auditErr != nil {
			return c, auditErr
		}
		return c, err
	}
	return s.advanceSource(ctx, c, []string{m6supply.ImportScanned, m6supply.ImportEvaluated, m6supply.ImportAwaitingApproval}, evidence, nil)
}

func scanSource(p skillarchive.Package) (m6supply.ImportEvidence, error) {
	if err := m8core.ScanInjection(p.Name + "\n" + p.Description + "\n" + p.Prompt); err != nil {
		return m6supply.ImportEvidence{}, fmt.Errorf("%w: 正文含权限绕过或指令覆盖标记", ErrImportScan)
	}
	return sourceScanEvidence(p.ArchiveHash), nil
}

// This is the deterministic representation of an already completed scan, used
// to verify stored receipts on reads. Only scanSource authorizes a new scan step.
func sourceScanEvidence(archiveHash string) m6supply.ImportEvidence {
	scan, _ := json.Marshal([]map[string]string{{"scanner": "m8core.ScanInjection/v1", "archiveHash": archiveHash, "scope": "SKILL.md name, description and body"}})
	report, _ := json.Marshal(map[string]any{"verdict": "no_known_markers", "scanner": "m8core.ScanInjection/v1", "archiveHash": archiveHash, "codeExecuted": false, "coverage": "static markers only; no security or effectiveness guarantee"})
	evalHash := fmt.Sprintf("%x", sha256.Sum256(append(scan, report...)))
	return m6supply.ImportEvidence{ScanRefs: string(scan), InjectionScan: string(report), EvaluationID: "static-validation:" + evalHash}
}

func (s *SkillImportService) recordSourceRejection(ctx context.Context, c m6supply.ImportCandidate) error {
	return s.uow.TransactM6(ctx, func(tx Tx) error {
		cur, err := tx.GetM6ImportCandidate(c.ID)
		if err != nil {
			return err
		}
		if cur.Version != c.Version {
			return m6supply.ErrVersionConflict
		}
		metadata, err := json.Marshal(map[string]any{"verdict": "blocked", "scanner": "m8core.ScanInjection/v1", "archiveHash": c.ArchiveHash, "state": cur.State})
		if err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{ID: ulid.Make().String(), Action: "skill.import.scanned", AggregateID: c.ID, Actor: delegationActor, CreatedAt: s.clock.Now().UTC(), Metadata: metadata})
	})
}

func (s *SkillImportService) approveResolved(ctx context.Context, in ApproveInput) (m6supply.ImportCandidate, error) {
	var approval map[string]any
	if len(in.Approval) > 4096 || json.Unmarshal([]byte(in.Approval), &approval) != nil || len(approval) == 0 {
		return m6supply.ImportCandidate{}, fmt.Errorf("%w: 审批记录无效", skillarchive.ErrInvalid)
	}
	canonicalApproval, _ := json.Marshal(approval)
	in.Approval = string(canonicalApproval)
	c, err := s.candidate(ctx, in.CandidateID)
	if err != nil {
		return c, err
	}
	replay, err := sourceStepVersion(c, in.ExpectedVersion, m6supply.ImportAwaitingApproval, m6supply.ImportApproved, 1)
	if err != nil {
		return c, err
	}
	if replay {
		var previous map[string]any
		if json.Unmarshal([]byte(c.Approval), &previous) != nil {
			return c, ErrImportChanged
		}
		encoded, _ := json.Marshal(previous)
		if string(encoded) != in.Approval {
			return c, ErrImportChanged
		}
		return s.committedSourceResult(ctx, c)
	}
	p, err := s.reload(ctx, c)
	if err != nil {
		return c, err
	}
	evidence, err := scanSource(p)
	if err != nil {
		return c, err
	}
	if evidence.ScanRefs != c.ScanRefs || evidence.InjectionScan != c.InjectionScan || evidence.EvaluationID != c.EvaluationID {
		return c, ErrImportChanged
	}
	if len(p.Files) == 0 {
		return c, fmt.Errorf("%w: 技能包资源为空，未批准导入", skillarchive.ErrInvalid)
	}
	packageDigest, err := skillapp.StoreLocalPackage(s.packageRoot, p.Files)
	if err != nil {
		return c, fmt.Errorf("保存完整技能资源失败，未批准导入: %w", err)
	}
	manifest, err := json.Marshal(map[string]any{"prompt": p.Prompt, "triggers": []string{p.Name}, "importCandidateId": c.ID, "sourceUrl": c.SourceURL, "commit": c.ImmutableCommit, "archiveHash": c.ArchiveHash, "license": c.License, "importScope": "complete_package", "localPackageDigest": packageDigest, "fileCount": len(p.Files), "skippedFiles": p.SkippedFiles})
	if err != nil || len(manifest) > 65536 {
		return c, fmt.Errorf("%w: 技能正文超过运行时限制", skillarchive.ErrInvalid)
	}
	return s.advanceSource(ctx, c, []string{m6supply.ImportApproved}, m6supply.ImportEvidence{Approval: in.Approval}, func(tx Tx, next m6supply.ImportCandidate) error {
		writer, ok := tx.(importSkillWriter)
		if !ok {
			return ErrServiceUnavailable
		}
		sk := skill.Skill{ID: c.ID, Name: p.Name, DisplayName: p.Name, Description: p.Description, Version: p.Version, Status: skill.SkillStatusDraft, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "builtin://import/" + c.ID, ManifestJSON: string(manifest), CreatedAt: next.UpdatedAt, UpdatedAt: next.UpdatedAt}
		if err := sk.Validate(); err != nil {
			return err
		}
		return writer.PutImportedSkill(sk)
	})
}

func sourceStepVersion(c m6supply.ImportCandidate, expected int64, from, to string, delta int64) (bool, error) {
	if expected >= 1 && c.State == to && c.Version-expected == delta {
		return true, nil
	}
	if c.Version != expected {
		return false, m6supply.ErrVersionConflict
	}
	if c.State != from {
		return false, m6supply.ErrInvalidTransition
	}
	return false, nil
}

// Committed results must be readable after a lost ACK even while offline. This
// is a read of the immutable candidate and scan receipt, not a new approval.
func validateCommittedSource(tx Tx, c m6supply.ImportCandidate) error {
	var att struct {
		SourceURL   string `json:"sourceUrl"`
		Commit      string `json:"commit"`
		ArchiveHash string `json:"archiveHash"`
		PromptHash  string `json:"promptHash"`
	}
	if json.Unmarshal([]byte(c.SourceAttestation), &att) != nil || att.SourceURL != c.SourceURL || att.Commit != c.ImmutableCommit || att.ArchiveHash != c.ArchiveHash || len(att.PromptHash) != 64 {
		return ErrImportChanged
	}
	if _, err := skillarchive.ParseSource(c.SourceURL, c.ImmutableCommit); err != nil {
		return ErrImportChanged
	}
	if c.State == m6supply.ImportAwaitingApproval || c.State == m6supply.ImportApproved {
		evidence := sourceScanEvidence(c.ArchiveHash)
		if evidence.ScanRefs != c.ScanRefs || evidence.InjectionScan != c.InjectionScan || evidence.EvaluationID != c.EvaluationID {
			return ErrImportChanged
		}
	}
	if c.State == m6supply.ImportApproved {
		reader, ok := tx.(importSkillReader)
		if !ok {
			return ErrServiceUnavailable
		}
		exists, err := reader.ImportedSkillExists(c.ID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrImportRuntimeMissing
		}
	}
	return nil
}

func (s *SkillImportService) committedSourceResult(ctx context.Context, c m6supply.ImportCandidate) (m6supply.ImportCandidate, error) {
	var out m6supply.ImportCandidate
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		cur, err := tx.GetM6ImportCandidate(c.ID)
		if err != nil {
			return err
		}
		if cur.Version != c.Version || cur.State != c.State {
			return m6supply.ErrVersionConflict
		}
		if cur.SourceAttestation != c.SourceAttestation || cur.ArchiveHash != c.ArchiveHash || cur.Approval != c.Approval {
			return ErrImportChanged
		}
		if err = validateCommittedSource(tx, cur); err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, err
}

func (s *SkillImportService) resumeSourceDiscovery(ctx context.Context, in DiscoverInput) (m6supply.ImportCandidate, bool, error) {
	if in.AssetType != m6supply.AssetSkill {
		return m6supply.ImportCandidate{}, false, fmt.Errorf("%w: 此导入入口当前支持 SKILL.md 技能", skillarchive.ErrInvalid)
	}
	src, err := skillarchive.ParseSource(in.SourceURL, in.ImmutableCommit)
	if err != nil {
		return m6supply.ImportCandidate{}, false, err
	}
	var out m6supply.ImportCandidate
	found := false
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		cur, err := tx.FindM6ImportCandidate(src.URL, src.Commit)
		if errors.Is(err, m6supply.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if in.ArchiveHash != "" && in.ArchiveHash != cur.ArchiveHash {
			return ErrImportChanged
		}
		switch cur.State {
		case m6supply.ImportDiscovered, m6supply.ImportInspected, m6supply.ImportAwaitingApproval, m6supply.ImportApproved:
		default:
			return ErrCandidateExists
		}
		if err = validateCommittedSource(tx, cur); err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, found, err
}

func (s *SkillImportService) advanceSource(ctx context.Context, c m6supply.ImportCandidate, states []string, evidence m6supply.ImportEvidence, extra func(Tx, m6supply.ImportCandidate) error) (m6supply.ImportCandidate, error) {
	var out m6supply.ImportCandidate
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		cur, err := tx.GetM6ImportCandidate(c.ID)
		if err != nil {
			return err
		}
		if cur.Version != c.Version || cur.State != c.State {
			return m6supply.ErrVersionConflict
		}
		for _, to := range states {
			if !m6supply.ImportTransitionAllowed(cur.State, to) {
				return m6supply.ErrInvalidTransition
			}
			cur, err = tx.TransitionM6ImportCandidate(c.ID, cur.Version, to, evidence, s.clock.Now().UTC())
			if err != nil {
				return err
			}
			if err = tx.PutAudit(providerapp.Audit{ID: ulid.Make().String(), Action: m6supply.ImportAuditAction(to), AggregateID: c.ID, Actor: delegationActor, CreatedAt: cur.UpdatedAt, Metadata: []byte(fmt.Sprintf(`{"state":%q,"version":%d,"archiveHash":%q}`, to, cur.Version, c.ArchiveHash))}); err != nil {
				return err
			}
		}
		if extra != nil {
			if err := extra(tx, cur); err != nil {
				return err
			}
		}
		out = cur
		return nil
	})
	return out, err
}
