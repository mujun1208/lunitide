package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

type PhaseFileReader interface {
	ReadFile(context.Context, string) ([]byte, error)
}
type phaseEvidenceFiles struct{ attachments, templates PhaseFileReader }

// SetProjectEvidenceFiles is wired once before serving traffic. File names are
// interpreted by the same confined storage used for ingestion.
func (s *Store) SetProjectEvidenceFiles(attachments, templates PhaseFileReader) {
	s.projectEvidenceFiles = phaseEvidenceFiles{attachments, templates}
}

type phaseEvidenceQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func phaseGateError(reason string) error {
	return fmt.Errorf("%w: %s", projectapp.ErrInvalidTransition, reason)
}

func (t *txAdapter) CompleteProjectPhase(ctx context.Context, id string, version int64, phase int) (project.Project, error) {
	p, err := t.getProject(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Version != version {
		return p, projectapp.ErrProjectVersionConflict
	}
	next, ok := project.PhaseCompletionTarget(p, phase)
	if !ok {
		return p, phaseGateError("phase is not eligible for document completion")
	}
	var stageID, status string
	var stageVersion int64
	err = t.q.QueryRowContext(ctx, `SELECT id,status,version FROM stages WHERE project_id=? AND phase=?`, id, phase).Scan(&stageID, &status, &stageVersion)
	if err == sql.ErrNoRows {
		return p, phaseGateError("stage does not exist")
	}
	if err != nil {
		return p, err
	}
	if status == "completed" || status == "paused" || status == "cancelled" || status == "blocked" || status == "rejected" || status == "stale" {
		return p, phaseGateError("stage is not eligible")
	}
	var completed int
	if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM stages WHERE project_id=? AND phase<? AND status='completed'`, id, phase).Scan(&completed); err != nil {
		return p, err
	}
	if completed != phase-1 {
		return p, phaseGateError("previous stages are incomplete")
	}
	rows, err := t.q.QueryContext(ctx, deliverableSelect+` WHERE project_id=? AND phase=?`, id, phase)
	if err != nil {
		return p, err
	}
	docs := map[string]deliverable.ProjectDeliverable{}
	for rows.Next() {
		d, e := scanProjectDeliverable(rows)
		if e != nil {
			rows.Close()
			return p, e
		}
		docs[d.DocumentType] = d
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	evidence := make([]map[string]any, 0)
	if phase == project.ReleasePhase(p.Type) {
		receipt, err := t.releasePhaseEvidence(ctx, p)
		if err != nil {
			return p, err
		}
		evidence = append(evidence, receipt)
	}
	for _, key := range project.RequiredPhaseDocuments(p.Type, phase) {
		d, exists := docs[key]
		if !exists || (d.Status != deliverable.StatusApproved && d.Status != deliverable.StatusImmutable) {
			return p, phaseGateError("required deliverable is not approved: " + key)
		}
		receipt, e := t.phaseEvidence(ctx, d)
		if e != nil {
			return p, e
		}
		evidence = append(evidence, receipt)
	}
	now := time.Now().UTC()
	for _, key := range project.RequiredPhaseDocuments(p.Type, phase) {
		d := docs[key]
		if d.Status == deliverable.StatusImmutable {
			continue
		}
		res, e := t.q.ExecContext(ctx, `UPDATE project_deliverables SET status='immutable',gate_confirmations=3,version=version+1,updated_at=? WHERE id=? AND version=? AND status='approved'`, formatTime(now), d.ID, d.Version)
		if e != nil {
			return p, e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return p, e
		}
		if n != 1 {
			return p, deliverable.ErrVersionConflict
		}
	}
	res, err := t.q.ExecContext(ctx, `UPDATE stages SET status='completed',version=version+1,updated_at=? WHERE id=? AND version=?`, formatTime(now), stageID, stageVersion)
	if err != nil {
		return p, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return p, err
	}
	if n != 1 {
		return p, phaseGateError("stage changed")
	}
	p, err = t.UpdateProject(ctx, id, version, func(p *project.Project) error { p.Status = next; return nil })
	if err != nil {
		return p, err
	}
	meta, _ := json.Marshal(map[string]any{"projectId": id, "phase": phase, "version": stageVersion + 1, "evidence": evidence})
	err = t.PutAudit(ctx, providerapp.Audit{ID: ulid.Make().String(), Action: "stage.updated", AggregateID: stageID, Actor: "desktop-host", Metadata: meta, CreatedAt: now})
	return p, err
}

func (t *txAdapter) phaseEvidence(ctx context.Context, d deliverable.ProjectDeliverable) (map[string]any, error) {
	return t.s.verifyProjectEvidence(ctx, t.q, d, true)
}
func (s *Store) verifyProjectEvidence(ctx context.Context, q phaseEvidenceQuery, d deliverable.ProjectDeliverable, requireApproval bool) (map[string]any, error) {
	receipt := map[string]any{"deliverableId": d.ID, "deliverableVersion": d.Version, "documentType": d.DocumentType}
	if d.AttachmentID != "" {
		var owner, digest, path string
		var phase int
		var version int64
		err := q.QueryRowContext(ctx, `SELECT project_id,phase,digest,file_path,version FROM project_attachments WHERE id=?`, d.AttachmentID).Scan(&owner, &phase, &digest, &path, &version)
		if err == sql.ErrNoRows {
			return nil, phaseGateError("attachment missing")
		}
		if err != nil {
			return nil, err
		}
		decoded, e := hex.DecodeString(digest)
		if owner != d.ProjectID || phase != d.Phase || path == "" || e != nil || len(decoded) != 32 {
			return nil, phaseGateError("attachment evidence does not match project and phase")
		}
		actual, bytes, e := readPhaseDigest(ctx, s.projectEvidenceFiles.attachments, path)
		if e != nil {
			return nil, e
		}
		if digest != actual || (requireApproval && d.Digest != actual) {
			return nil, phaseGateError("attachment content changed after ingestion or approval")
		}
		receipt["attachmentId"], receipt["attachmentVersion"], receipt["digest"] = d.AttachmentID, version, digest
		receipt["bytes"] = bytes
		return receipt, nil
	}
	if d.TemplateID != "" {
		var status, path, owner string
		var version int64
		err := q.QueryRowContext(ctx, `SELECT status,file_path,version,COALESCE(org_id,'') FROM asset_templates WHERE id=?`, d.TemplateID).Scan(&status, &path, &version, &owner)
		if err == sql.ErrNoRows {
			return nil, phaseGateError("template missing")
		}
		if err != nil {
			return nil, err
		}
		if status != "enabled" || path == "" {
			return nil, phaseGateError("template evidence is unavailable")
		}
		var projectScope string
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(org_id,'') FROM projects WHERE id=?`, d.ProjectID).Scan(&projectScope); err != nil {
			return nil, err
		}
		if owner != projectScope {
			return nil, phaseGateError("template belongs to another data scope")
		}
		actual, bytes, e := readPhaseDigest(ctx, s.projectEvidenceFiles.templates, path)
		if e != nil {
			return nil, e
		}
		if requireApproval && d.Digest != actual {
			return nil, phaseGateError("template content changed after approval")
		}
		receipt["templateId"], receipt["templateVersion"] = d.TemplateID, version
		receipt["digest"], receipt["bytes"] = actual, bytes
		return receipt, nil
	}
	return nil, phaseGateError("deliverable has no evidence: " + d.DocumentType)
}

func readPhaseDigest(ctx context.Context, files PhaseFileReader, path string) (string, int, error) {
	if files == nil {
		return "", 0, phaseGateError("evidence file storage unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	content, err := files.ReadFile(ctx, path)
	if err != nil {
		return "", 0, phaseGateError("evidence file unavailable")
	}
	if len(content) == 0 || len(content) > 10<<20 {
		return "", 0, phaseGateError("evidence file empty or exceeds limit")
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), len(content), nil
}
