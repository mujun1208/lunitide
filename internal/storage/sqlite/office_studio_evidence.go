package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

func (s *Store) AddOfficeValidation(ctx context.Context, v officestudio.Validation) (officestudio.Validation, error) {
	if v.ID == "" {
		v.ID = ulid.Make().String()
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	v.Evidence = officeJSON(v.Evidence)
	if !officestudio.ValidID(v.ID) || !officestudio.ValidID(v.VersionID) || !officestudio.ValidDigest(v.SHA256) || len(v.Validator) == 0 || len(v.Validator) > 128 || len(v.Checks) > 1000 || !officestudio.ValidJSON(v.Evidence, 1<<20) {
		return v, officestudio.ErrInvalid
	}
	seen := map[string]bool{}
	for _, check := range v.Checks {
		if check.ID == "" || len(check.ID) > 128 || len(check.Label) > 512 || len(check.Detail) > 16000 || seen[check.ID] {
			return v, officestudio.ErrInvalid
		}
		seen[check.ID] = true
		switch check.Status {
		case "passed", "failed", "skipped", "unsupported", "pending":
		default:
			return v, officestudio.ErrInvalid
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if err = lockOfficeStorage(ctx, tx); err != nil {
		return v, err
	}
	if err = officeAuthorize(ctx, tx, "office-version", v.VersionID); err != nil {
		return v, err
	}
	version, err := scanOfficeVersion(tx.QueryRowContext(ctx, `SELECT `+officeVersionColumns+officeVersionFrom+` WHERE v.id=?`, v.VersionID))
	if err != nil {
		return v, err
	}
	if version.SHA256 != v.SHA256 {
		return v, officestudio.ErrConflict
	}
	// Once a required dimension is declared, later reports cannot silently drop it.
	rows, err := tx.QueryContext(ctx, `SELECT checks_json FROM office_validation_runs WHERE version_id=?`, v.VersionID)
	if err != nil {
		return v, err
	}
	required := map[string]officestudio.Check{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return v, err
		}
		var previous []officestudio.Check
		if err = json.Unmarshal([]byte(raw), &previous); err != nil {
			rows.Close()
			return v, err
		}
		for _, c := range previous {
			if c.Required {
				required[c.ID] = c
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	for i := range v.Checks {
		if _, exists := required[v.Checks[i].ID]; exists {
			v.Checks[i].Required = true
		}
	}
	for id, c := range required {
		if !seen[id] {
			c.Status = "pending"
			c.Detail = "This required check is missing from this validation run."
			v.Checks = append(v.Checks, c)
		}
	}
	v.Quality = officestudio.QualityFor(v.Checks)
	checks, err := json.Marshal(v.Checks)
	if err != nil {
		return v, err
	}
	if len(checks) > 1<<20 {
		return v, officestudio.ErrInvalid
	}
	var oldRaw, oldEvidence, oldSHA, oldValidator string
	err = tx.QueryRowContext(ctx, `SELECT checks_json,evidence_json,sha256,validator FROM office_validation_runs WHERE id=?`, v.ID).Scan(&oldRaw, &oldEvidence, &oldSHA, &oldValidator)
	if err == nil {
		if oldRaw != string(checks) || oldEvidence != string(v.Evidence) || oldSHA != v.SHA256 || oldValidator != v.Validator {
			return v, officestudio.ErrConflict
		}
		return v, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_validation_runs(id,version_id,sha256,validator,quality,checks_json,evidence_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, v.ID, v.VersionID, v.SHA256, v.Validator, v.Quality, string(checks), string(v.Evidence), officeRFC(v.CreatedAt))
	if err != nil {
		return v, err
	}
	var blobEvidence struct {
		PDFRef string `json:"pdfRef"`
	}
	if err = json.Unmarshal(v.Evidence, &blobEvidence); err != nil && v.BlobLeaseID != "" {
		return v, officestudio.ErrInvalid
	}
	if v.BlobLeaseID != "" && blobEvidence.PDFRef == "" {
		return v, officestudio.ErrInvalid
	}
	if blobEvidence.PDFRef != "" {
		if err = bindOfficeBlob(ctx, tx, version.TaskID, blobEvidence.PDFRef, 0, v.BlobLeaseID, "validation", v.ID, time.Now().UTC()); err != nil {
			return v, err
		}
	}
	// A delayed report cannot replace a newer report or clear upstream staleness.
	var latestID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM office_validation_runs WHERE version_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, v.VersionID).Scan(&latestID); err != nil {
		return v, err
	}
	if latestID == v.ID && version.Quality != "stale" {
		_, err = tx.ExecContext(ctx, `UPDATE office_version_metadata SET quality=? WHERE version_id=?`, v.Quality, v.VersionID)
		if err != nil {
			return v, err
		}
	}
	if err = appendOfficeEvent(ctx, tx, version.TaskID, v.VersionID, "version.validated", map[string]any{"validationId": v.ID, "quality": v.Quality, "sha256": v.SHA256}, v.CreatedAt); err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (s *Store) ListOfficeValidations(ctx context.Context, versionID string) ([]officestudio.Validation, error) {
	if err := officeAuthorize(ctx, s.db, "office-version", versionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,version_id,sha256,validator,quality,checks_json,evidence_json,created_at FROM office_validation_runs WHERE version_id=? ORDER BY created_at DESC,id DESC LIMIT 200`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Validation{}
	for rows.Next() {
		var v officestudio.Validation
		var checks, evidence, created string
		if err = rows.Scan(&v.ID, &v.VersionID, &v.SHA256, &v.Validator, &v.Quality, &checks, &evidence, &created); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(checks), &v.Checks); err != nil {
			return nil, err
		}
		v.Evidence = json.RawMessage(evidence)
		v.CreatedAt, err = parseRFC(created)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) AppendOfficeStepReceipt(ctx context.Context, r officestudio.StepReceipt) error {
	if r.ID == "" {
		r.ID = ulid.Make().String()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	r.Result = officeJSON(r.Result)
	if !officestudio.ValidID(r.ID) || !officestudio.ValidID(r.TaskID) || !officestudio.ValidID(r.RunID) || !officestudio.ValidDigest(r.InputDigest) || !officeKey(r.StepKey) || !officeKey(r.IdempotencyKey) || !officestudio.ValidJSON(r.Result, 1<<20) {
		return officestudio.ErrInvalid
	}
	switch r.State {
	case "started", "succeeded", "failed", "cancelled", "unknown":
	default:
		return officestudio.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", r.TaskID); err != nil {
		return err
	}
	var taskRun string
	if err = tx.QueryRowContext(ctx, `SELECT run_id FROM office_tasks WHERE id=?`, r.TaskID).Scan(&taskRun); err != nil {
		return officeError(err)
	}
	if taskRun != "" && taskRun != r.RunID {
		return officestudio.ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT input_digest,state,result_json FROM office_step_receipts WHERE task_id=? AND run_id=? AND step_key=? AND idempotency_key=?`, r.TaskID, r.RunID, r.StepKey, r.IdempotencyKey)
	if err != nil {
		return err
	}
	duplicate := false
	for rows.Next() {
		var digest, state, result string
		if err = rows.Scan(&digest, &state, &result); err != nil {
			rows.Close()
			return err
		}
		if digest != r.InputDigest {
			rows.Close()
			return officestudio.ErrConflict
		}
		if state == r.State {
			if result != string(r.Result) {
				rows.Close()
				return officestudio.ErrConflict
			}
			duplicate = true
		}
		if state != "started" && state != "unknown" && state != r.State {
			rows.Close()
			return officestudio.ErrConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if duplicate {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_step_receipts(id,task_id,run_id,step_key,idempotency_key,input_digest,state,result_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.ID, r.TaskID, r.RunID, r.StepKey, r.IdempotencyKey, r.InputDigest, r.State, string(r.Result), officeRFC(r.CreatedAt))
	if err != nil {
		return err
	}
	if err = appendOfficeEvent(ctx, tx, r.TaskID, "", "step."+r.State, map[string]any{"stepKey": r.StepKey, "runId": r.RunID, "receiptId": r.ID}, r.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListOfficeStepReceipts(ctx context.Context, taskID, runID string) ([]officestudio.StepReceipt, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,run_id,step_key,idempotency_key,input_digest,state,result_json,created_at FROM office_step_receipts WHERE task_id=? AND (?='' OR run_id=?) ORDER BY created_at,id LIMIT 1000`, taskID, runID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.StepReceipt{}
	for rows.Next() {
		var r officestudio.StepReceipt
		var result, created string
		if err = rows.Scan(&r.ID, &r.TaskID, &r.RunID, &r.StepKey, &r.IdempotencyKey, &r.InputDigest, &r.State, &result, &created); err != nil {
			return nil, err
		}
		r.Result = json.RawMessage(result)
		r.CreatedAt, err = parseRFC(created)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddOfficeEvidenceEdge(ctx context.Context, e officestudio.EvidenceEdge) error {
	if e.ID == "" {
		e.ID = ulid.Make().String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	e.Metric = officeJSON(e.Metric)
	if !officestudio.ValidID(e.ID) || !officestudio.ValidID(e.TaskID) || !officestudio.ValidID(e.SourceVersionID) || !officestudio.ValidID(e.TargetVersionID) || e.SourceVersionID == e.TargetVersionID || len(e.SourceNode) > 512 || len(e.TargetNode) > 512 || !officestudio.ValidJSON(e.Metric, 65536) {
		return officestudio.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", e.TaskID); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM artifact_versions v JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id WHERE b.task_id=? AND v.id IN (?,?)`, e.TaskID, e.SourceVersionID, e.TargetVersionID).Scan(&count); err != nil {
		return err
	}
	if count != 2 {
		return officestudio.ErrScope
	}
	if err = tx.QueryRowContext(ctx, `WITH RECURSIVE reach(id) AS (SELECT target_version_id FROM office_evidence_edges WHERE source_version_id=? UNION SELECT e.target_version_id FROM office_evidence_edges e JOIN reach r ON e.source_version_id=r.id) SELECT count(*) FROM reach WHERE id=?`, e.TargetVersionID, e.SourceVersionID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return officestudio.ErrInvalid
	}
	var oldMetric string
	err = tx.QueryRowContext(ctx, `SELECT metric_json FROM office_evidence_edges WHERE source_version_id=? AND target_version_id=? AND source_node=? AND target_node=?`, e.SourceVersionID, e.TargetVersionID, e.SourceNode, e.TargetNode).Scan(&oldMetric)
	if err == nil {
		if oldMetric != string(e.Metric) {
			return officestudio.ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_evidence_edges(id,task_id,source_version_id,target_version_id,source_node,target_node,metric_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, e.ID, e.TaskID, e.SourceVersionID, e.TargetVersionID, e.SourceNode, e.TargetNode, string(e.Metric), officeRFC(e.CreatedAt))
	if err != nil {
		return err
	}
	if err = appendOfficeEvent(ctx, tx, e.TaskID, e.TargetVersionID, "evidence.linked", map[string]any{"sourceVersionId": e.SourceVersionID, "edgeId": e.ID}, e.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListOfficeEvidenceEdges(ctx context.Context, taskID string) ([]officestudio.EvidenceEdge, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,source_version_id,target_version_id,source_node,target_node,metric_json,created_at FROM office_evidence_edges WHERE task_id=? ORDER BY created_at,id LIMIT 1000`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.EvidenceEdge{}
	for rows.Next() {
		var e officestudio.EvidenceEdge
		var metric, created string
		if err = rows.Scan(&e.ID, &e.TaskID, &e.SourceVersionID, &e.TargetVersionID, &e.SourceNode, &e.TargetNode, &metric, &created); err != nil {
			return nil, err
		}
		e.Metric = json.RawMessage(metric)
		e.CreatedAt, err = parseRFC(created)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) MarkOfficeDependentsStale(ctx context.Context, sourceVersionID string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-version", sourceVersionID); err != nil {
		return 0, err
	}
	count, err := markOfficeDependentsStale(ctx, tx, sourceVersionID)
	if err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

// Publication calls this in its own transaction with the actual replaced
// head, which can differ from the base when restoring an older version.
func markOfficeDependentsStale(ctx context.Context, tx *sql.Tx, sourceVersionID string) (int64, error) {
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE reach(id) AS (SELECT target_version_id FROM office_evidence_edges WHERE source_version_id=? UNION SELECT e.target_version_id FROM office_evidence_edges e JOIN reach r ON e.source_version_id=r.id) SELECT v.id,b.task_id FROM reach r JOIN artifact_versions v ON v.id=r.id JOIN office_version_metadata m ON m.version_id=v.id JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id WHERE m.quality<>'stale'`, sourceVersionID)
	if err != nil {
		return 0, err
	}
	type target struct{ id, task string }
	targets := []target{}
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.id, &t.task); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, t := range targets {
		if _, err = tx.ExecContext(ctx, `UPDATE office_version_metadata SET quality='stale' WHERE version_id=?`, t.id); err != nil {
			return 0, err
		}
		if err = appendOfficeEvent(ctx, tx, t.task, t.id, "version.stale", map[string]any{"sourceVersionId": sourceVersionID}, time.Now().UTC()); err != nil {
			return 0, err
		}
	}
	return int64(len(targets)), nil
}
