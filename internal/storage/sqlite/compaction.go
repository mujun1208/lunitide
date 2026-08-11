package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/domain/compaction"
)

// CreateCheckpoint inserts a new compaction checkpoint.
func (s *Store) CreateCheckpoint(ctx context.Context, cp compaction.Checkpoint) (compaction.Checkpoint, error) {
	if cp.ID == "" {
		var err error
		cp.ID, err = s.newULID(time.Now())
		if err != nil {
			return cp, err
		}
	}
	cp.CreatedAt = time.Now().UTC()
	if cp.Status == "" {
		cp.Status = compaction.StatusPending
	}
	if cp.SummarySchemaVersion == "" {
		cp.SummarySchemaVersion = compaction.SummarySchemaVersion
	}
	if cp.SummaryJSON == "" {
		cp.SummaryJSON = "{}"
	}
	if err := cp.Validate(); err != nil {
		return cp, err
	}
	var prevCheckpointID, prevCheckpointDigest, failureCode, completedAt any
	if cp.PrevCheckpointID != nil {
		prevCheckpointID = *cp.PrevCheckpointID
	}
	if cp.PrevCheckpointDigest != nil {
		prevCheckpointDigest = *cp.PrevCheckpointDigest
	}
	if cp.FailureCode != nil {
		failureCode = *cp.FailureCode
	}
	if cp.CompletedAt != nil {
		completedAt = formatTime(*cp.CompletedAt)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO compaction_checkpoints(id, session_id, version, source_start_id, source_end_id,
		 source_start_seq, source_end_seq, source_digest, prev_checkpoint_id, prev_checkpoint_digest,
		 summary_schema_version, trigger, trigger_reason, status, provider, model,
		 summary_json, human_summary, failure_code, created_at, completed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		cp.ID, cp.SessionID, cp.Version, cp.SourceStartID, cp.SourceEndID,
		cp.SourceStartSeq, cp.SourceEndSeq, cp.SourceDigest, prevCheckpointID, prevCheckpointDigest,
		cp.SummarySchemaVersion, cp.Trigger, cp.TriggerReason, cp.Status, cp.Provider, cp.Model,
		cp.SummaryJSON, cp.HumanSummary, failureCode, formatTime(cp.CreatedAt), completedAt)
	return cp, mapWriteError(err)
}

// GetCheckpoint returns a checkpoint by ID.
func (s *Store) GetCheckpoint(ctx context.Context, id string) (*compaction.Checkpoint, error) {
	var cp compaction.Checkpoint
	var created, completed sql.NullString
	var prevCheckpointID, prevCheckpointDigest, failureCode sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, version, source_start_id, source_end_id,
		 source_start_seq, source_end_seq, source_digest, prev_checkpoint_id, prev_checkpoint_digest,
		 summary_schema_version, trigger, trigger_reason, status, provider, model,
		 summary_json, human_summary, failure_code, created_at, completed_at
		 FROM compaction_checkpoints WHERE id=?`, id).Scan(
		&cp.ID, &cp.SessionID, &cp.Version, &cp.SourceStartID, &cp.SourceEndID,
		&cp.SourceStartSeq, &cp.SourceEndSeq, &cp.SourceDigest, &prevCheckpointID, &prevCheckpointDigest,
		&cp.SummarySchemaVersion, &cp.Trigger, &cp.TriggerReason, &cp.Status, &cp.Provider, &cp.Model,
		&cp.SummaryJSON, &cp.HumanSummary, &failureCode, &created, &completed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cp.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return nil, err
	}
	if prevCheckpointID.Valid {
		cp.PrevCheckpointID = &prevCheckpointID.String
	}
	if prevCheckpointDigest.Valid {
		cp.PrevCheckpointDigest = &prevCheckpointDigest.String
	}
	if failureCode.Valid {
		cp.FailureCode = &failureCode.String
	}
	if completed.Valid {
		t, err := time.Parse(time.RFC3339Nano, completed.String)
		if err != nil {
			return nil, err
		}
		cp.CompletedAt = &t
	}
	return &cp, nil
}

// ListCheckpointsBySession returns checkpoints for a session ordered by version descending.
func (s *Store) ListCheckpointsBySession(ctx context.Context, sessionID string, limit int) ([]compaction.Checkpoint, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, version, source_start_id, source_end_id,
		 source_start_seq, source_end_seq, source_digest, prev_checkpoint_id, prev_checkpoint_digest,
		 summary_schema_version, trigger, trigger_reason, status, provider, model,
		 summary_json, human_summary, failure_code, created_at, completed_at
		 FROM compaction_checkpoints WHERE session_id=? ORDER BY version DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []compaction.Checkpoint
	for rows.Next() {
		var cp compaction.Checkpoint
		var created, completed sql.NullString
		var prevCheckpointID, prevCheckpointDigest, failureCode sql.NullString
		if err = rows.Scan(
			&cp.ID, &cp.SessionID, &cp.Version, &cp.SourceStartID, &cp.SourceEndID,
			&cp.SourceStartSeq, &cp.SourceEndSeq, &cp.SourceDigest, &prevCheckpointID, &prevCheckpointDigest,
			&cp.SummarySchemaVersion, &cp.Trigger, &cp.TriggerReason, &cp.Status, &cp.Provider, &cp.Model,
			&cp.SummaryJSON, &cp.HumanSummary, &failureCode, &created, &completed); err != nil {
			return nil, err
		}
		cp.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
		if err != nil {
			return nil, err
		}
		if prevCheckpointID.Valid {
			cp.PrevCheckpointID = &prevCheckpointID.String
		}
		if prevCheckpointDigest.Valid {
			cp.PrevCheckpointDigest = &prevCheckpointDigest.String
		}
		if failureCode.Valid {
			cp.FailureCode = &failureCode.String
		}
		if completed.Valid {
			t, err := time.Parse(time.RFC3339Nano, completed.String)
			if err != nil {
				return nil, err
			}
			cp.CompletedAt = &t
		}
		result = append(result, cp)
	}
	return result, rows.Err()
}

// GetLatestCheckpoint returns the checkpoint with the highest version for a session.
func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*compaction.Checkpoint, error) {
	var cp compaction.Checkpoint
	var created, completed sql.NullString
	var prevCheckpointID, prevCheckpointDigest, failureCode sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, version, source_start_id, source_end_id,
		 source_start_seq, source_end_seq, source_digest, prev_checkpoint_id, prev_checkpoint_digest,
		 summary_schema_version, trigger, trigger_reason, status, provider, model,
		 summary_json, human_summary, failure_code, created_at, completed_at
		 FROM compaction_checkpoints WHERE session_id=? ORDER BY version DESC LIMIT 1`, sessionID).Scan(
		&cp.ID, &cp.SessionID, &cp.Version, &cp.SourceStartID, &cp.SourceEndID,
		&cp.SourceStartSeq, &cp.SourceEndSeq, &cp.SourceDigest, &prevCheckpointID, &prevCheckpointDigest,
		&cp.SummarySchemaVersion, &cp.Trigger, &cp.TriggerReason, &cp.Status, &cp.Provider, &cp.Model,
		&cp.SummaryJSON, &cp.HumanSummary, &failureCode, &created, &completed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cp.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return nil, err
	}
	if prevCheckpointID.Valid {
		cp.PrevCheckpointID = &prevCheckpointID.String
	}
	if prevCheckpointDigest.Valid {
		cp.PrevCheckpointDigest = &prevCheckpointDigest.String
	}
	if failureCode.Valid {
		cp.FailureCode = &failureCode.String
	}
	if completed.Valid {
		t, err := time.Parse(time.RFC3339Nano, completed.String)
		if err != nil {
			return nil, err
		}
		cp.CompletedAt = &t
	}
	return &cp, nil
}

// UpdateCheckpointStatus atomically updates the status and optionally the summary
// of a checkpoint using CAS (compare-and-swap) on expectedStatus. If the current
// status does not match expectedStatus, 0 rows are affected and the method
// returns compactionapp.ErrConcurrentModification (ADR-005 §4.2).
func (s *Store) UpdateCheckpointStatus(ctx context.Context, id string, expectedStatus, status compaction.Status, summaryJSON, humanSummary string, failureCode *string) error {
	var completedAt any
	if status.IsTerminal() {
		completedAt = formatTime(time.Now().UTC())
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE compaction_checkpoints SET status=?, summary_json=?, human_summary=?, failure_code=?, completed_at=? WHERE id=? AND status=?`,
		status, summaryJSON, humanSummary, failureCode, completedAt, id, expectedStatus)
	if err != nil {
		return mapWriteError(err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return compactionapp.ErrConcurrentModification
	}
	return nil
}

// ListCheckpointsByStatus returns checkpoints matching the given status,
// ordered by created_at ascending. Used by restart recovery to find orphaned
// pending/running checkpoints left over from a previous process crash
// (ADR-005 §5: "automatic high/low-watermark compaction and restart recovery").
func (s *Store) ListCheckpointsByStatus(ctx context.Context, status compaction.Status, limit int) ([]compaction.Checkpoint, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, version, source_start_id, source_end_id,
		 source_start_seq, source_end_seq, source_digest, prev_checkpoint_id, prev_checkpoint_digest,
		 summary_schema_version, trigger, trigger_reason, status, provider, model,
		 summary_json, human_summary, failure_code, created_at, completed_at
		 FROM compaction_checkpoints WHERE status=? ORDER BY created_at ASC LIMIT ?`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []compaction.Checkpoint
	for rows.Next() {
		var cp compaction.Checkpoint
		var created, completed sql.NullString
		var prevCheckpointID, prevCheckpointDigest, failureCode sql.NullString
		if err = rows.Scan(
			&cp.ID, &cp.SessionID, &cp.Version, &cp.SourceStartID, &cp.SourceEndID,
			&cp.SourceStartSeq, &cp.SourceEndSeq, &cp.SourceDigest, &prevCheckpointID, &prevCheckpointDigest,
			&cp.SummarySchemaVersion, &cp.Trigger, &cp.TriggerReason, &cp.Status, &cp.Provider, &cp.Model,
			&cp.SummaryJSON, &cp.HumanSummary, &failureCode, &created, &completed); err != nil {
			return nil, err
		}
		cp.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
		if err != nil {
			return nil, err
		}
		if prevCheckpointID.Valid {
			cp.PrevCheckpointID = &prevCheckpointID.String
		}
		if prevCheckpointDigest.Valid {
			cp.PrevCheckpointDigest = &prevCheckpointDigest.String
		}
		if failureCode.Valid {
			cp.FailureCode = &failureCode.String
		}
		if completed.Valid {
			t, err := time.Parse(time.RFC3339Nano, completed.String)
			if err != nil {
				return nil, err
			}
			cp.CompletedAt = &t
		}
		result = append(result, cp)
	}
	return result, rows.Err()
}

// CountCheckpointsBySession returns the number of checkpoints for a session.
func (s *Store) CountCheckpointsBySession(ctx context.Context, sessionID string) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM compaction_checkpoints WHERE session_id=?`, sessionID).Scan(&count)
	return count, err
}

// MarkPreviousSucceededAsSuperseded marks all succeeded checkpoints for the
// session (except the one with currentCheckpointID) as superseded. This ensures
// only one active succeeded checkpoint exists per session after a commit
// (ADR-005 §4.2). Returns the number of checkpoints superseded.
func (s *Store) MarkPreviousSucceededAsSuperseded(ctx context.Context, sessionID, currentCheckpointID string) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE compaction_checkpoints SET status='superseded', completed_at=?
		 WHERE session_id=? AND status='succeeded' AND id != ?`,
		formatTime(time.Now().UTC()), sessionID, currentCheckpointID)
	if err != nil {
		return 0, mapWriteError(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// GetLatestSucceededCheckpoint returns the latest succeeded checkpoint for the
// session (highest version among succeeded), or nil if none. Used for low-
// watermark verification and rolling summary loading.
func (s *Store) GetLatestSucceededCheckpoint(ctx context.Context, sessionID string) (*compaction.Checkpoint, error) {
	var cp compaction.Checkpoint
	var created, completed sql.NullString
	var prevCheckpointID, prevCheckpointDigest, failureCode sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, version, source_start_id, source_end_id,
		 source_start_seq, source_end_seq, source_digest, prev_checkpoint_id, prev_checkpoint_digest,
		 summary_schema_version, trigger, trigger_reason, status, provider, model,
		 summary_json, human_summary, failure_code, created_at, completed_at
		 FROM compaction_checkpoints WHERE session_id=? AND status='succeeded'
		 ORDER BY version DESC LIMIT 1`, sessionID).Scan(
		&cp.ID, &cp.SessionID, &cp.Version, &cp.SourceStartID, &cp.SourceEndID,
		&cp.SourceStartSeq, &cp.SourceEndSeq, &cp.SourceDigest, &prevCheckpointID, &prevCheckpointDigest,
		&cp.SummarySchemaVersion, &cp.Trigger, &cp.TriggerReason, &cp.Status, &cp.Provider, &cp.Model,
		&cp.SummaryJSON, &cp.HumanSummary, &failureCode, &created, &completed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cp.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return nil, err
	}
	if prevCheckpointID.Valid {
		cp.PrevCheckpointID = &prevCheckpointID.String
	}
	if prevCheckpointDigest.Valid {
		cp.PrevCheckpointDigest = &prevCheckpointDigest.String
	}
	if failureCode.Valid {
		cp.FailureCode = &failureCode.String
	}
	if completed.Valid {
		t, err := time.Parse(time.RFC3339Nano, completed.String)
		if err != nil {
			return nil, err
		}
		cp.CompletedAt = &t
	}
	return &cp, nil
}