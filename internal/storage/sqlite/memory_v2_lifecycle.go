package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func (s *Store) ListCanonicalMemoryHistory(ctx context.Context, subjectID, factID string, cursor int64, limit int) ([]m8core.CanonicalMemoryRecord, string, error) {
	return listCanonicalMemoryHistory(ctx, s.db, subjectID, factID, cursor, limit)
}

func (r *AgentRuntimeRepository) ListCanonicalMemoryHistory(ctx context.Context, subjectID, factID string, cursor int64, limit int) ([]m8core.CanonicalMemoryRecord, string, error) {
	return listCanonicalMemoryHistory(ctx, r.db, subjectID, factID, cursor, limit)
}

func listCanonicalMemoryHistory(ctx context.Context, db *sql.DB, subjectID, factID string, cursor int64, limit int) ([]m8core.CanonicalMemoryRecord, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if _, err := getCanonicalMemoryItem(ctx, db, subjectID, factID); err != nil {
		return nil, "", err
	}
	args := []any{factID, subjectID}
	q := `SELECT v.fact_version,h.revision,v.kind,h.scope_kind,h.scope_id,h.is_forgotten,v.created_at,b.canonical_text
		FROM memory_content_versions v
		JOIN memory_fact_heads h ON h.fact_id=v.fact_id
		LEFT JOIN memory_content_bodies b ON b.fact_id=v.fact_id AND b.fact_version=v.fact_version
		WHERE v.fact_id=? AND h.subject_id=?`
	if cursor > 0 {
		q += ` AND v.fact_version<?`
		args = append(args, cursor)
	}
	q += ` ORDER BY v.fact_version DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []m8core.CanonicalMemoryRecord
	for rows.Next() {
		var rec m8core.CanonicalMemoryRecord
		var forgotten int64
		var text sql.NullString
		if err = rows.Scan(&rec.Version, &rec.Revision, &rec.Kind, &rec.ScopeKind, &rec.ScopeID, &forgotten, &rec.UpdatedAt, &text); err != nil {
			return nil, "", err
		}
		rec.FactID = factID
		rec.Forgotten = forgotten == 1
		if text.Valid && !rec.Forgotten {
			rec.Text = text.String
		}
		out = append(out, rec)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		next = strconv.FormatInt(out[limit-1].Version, 10)
		out = out[:limit]
	}
	return out, next, nil
}

func (s *Store) CorrectCanonicalMemoryItem(ctx context.Context, subjectID, factID, text, reason, operationID, idempotencyKey string, expectedRevision int64, validFrom *time.Time) (m8core.CanonicalMemoryRecord, error) {
	return correctCanonicalMemoryItem(ctx, s.db, subjectID, factID, text, reason, operationID, idempotencyKey, expectedRevision, validFrom)
}

func (r *AgentRuntimeRepository) CorrectCanonicalMemoryItem(ctx context.Context, subjectID, factID, text, reason, operationID, idempotencyKey string, expectedRevision int64, validFrom *time.Time) (m8core.CanonicalMemoryRecord, error) {
	return correctCanonicalMemoryItem(ctx, r.db, subjectID, factID, text, reason, operationID, idempotencyKey, expectedRevision, validFrom)
}

func correctCanonicalMemoryItem(ctx context.Context, db *sql.DB, subjectID, factID, text, reason, operationID, idempotencyKey string, expectedRevision int64, validFrom *time.Time) (m8core.CanonicalMemoryRecord, error) {
	text = strings.TrimSpace(text)
	reason = strings.TrimSpace(reason)
	if subjectID == "" || factID == "" || text == "" || reason == "" || operationID == "" {
		return m8core.CanonicalMemoryRecord{}, fmt.Errorf("correct missing required fields")
	}
	if idempotencyKey == "" {
		idempotencyKey = operationID
	}
	digest := sha256.Sum256([]byte(text))
	contentDigest := hex.EncodeToString(digest[:])
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay struct {
			m8core.CanonicalMemoryRecord
			Digest string `json:"digest"`
		}
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.FactID != "" {
			if replay.Digest != "" && replay.Digest != contentDigest {
				return m8core.CanonicalMemoryRecord{}, m8core.ErrOperationReplayMismatch
			}
			return replay.CanonicalMemoryRecord, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.CanonicalMemoryRecord{}, err
	}
	var headSubject, scopeKind, scopeID string
	var currentVersion, revision, forgotten int64
	err = tx.QueryRowContext(ctx, `SELECT h.subject_id,h.scope_kind,h.scope_id,h.current_version,h.revision,h.is_forgotten
		FROM memory_fact_heads h WHERE h.fact_id=?`, factID).Scan(&headSubject, &scopeKind, &scopeID, &currentVersion, &revision, &forgotten)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrNotFound
	}
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if headSubject != subjectID || forgotten == 1 {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrNotFound
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrRevisionConflict
	}
	now := time.Now().UTC()
	from := now
	if validFrom != nil {
		from = validFrom.UTC()
	}
	newVersion := currentVersion + 1
	newKind := m8core.ClassifyMemoryKind(text)
	if _, err = tx.ExecContext(ctx, `UPDATE memory_content_versions SET valid_to=? WHERE fact_id=? AND fact_version=? AND valid_to IS NULL`,
		from.Format(time.RFC3339Nano), factID, currentVersion); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_content_versions(
		fact_id,fact_version,subject_id,scope_kind,scope_id,kind,body_ref,authority,stability,
		importance,confidence,valid_from,valid_to,observed_at,ingested_at,origin_plane,origin_id,
		extractor_kind,extractor_model,content_digest,created_at)
		VALUES(?,?,?,?,?,?, 'body', ?, ?, 0.5, 1, ?, NULL, ?, ?, 'native', ?, 'deterministic', '', ?, ?)`,
		factID, newVersion, subjectID, scopeKind, scopeID, newKind,
		m8core.MemoryAuthorityExplicit, m8core.MemoryStabilityDurable,
		from.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), operationID,
		contentDigest, now.Format(time.RFC3339Nano)); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_content_bodies(fact_id,fact_version,canonical_text,canonical_json) VALUES(?,?,?,NULL)`,
		factID, newVersion, text); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_fact_heads SET current_version=?, revision=revision+1, updated_at=? WHERE fact_id=? AND subject_id=? AND revision=?`,
		newVersion, now.Format(time.RFC3339Nano), factID, subjectID, revision); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_fact_supersessions(subject_id,scope_kind,scope_id,fact_id,old_version,new_version,effective_at,event_seq)
		VALUES(?,?,?,?,?,?,?,?)`, subjectID, scopeKind, scopeID, factID, currentVersion, newVersion, now.Format(time.RFC3339Nano), nextSeq); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_evidence_spans(id,fact_id,fact_version,source_kind,source_ref,start_byte,end_byte,quote_digest,created_at)
		VALUES(?,?,?,?,?,NULL,NULL,?,?)`,
		ulid.Make().String(), factID, newVersion, m8core.MemorySourceDirect, "operation:"+operationID, contentDigest, now.Format(time.RFC3339Nano)); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM memory_search_documents WHERE fact_id=?`, factID); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_search_documents(subject_id,scope_kind,scope_id,fact_id,fact_version,text) VALUES(?,?,?,?,?,?)`,
		subjectID, scopeKind, scopeID, factID, newVersion, text); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE fact_id=?`, factID); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	rec := m8core.CanonicalMemoryRecord{
		FactID: factID, Version: newVersion, Revision: revision + 1, Kind: newKind,
		ScopeKind: scopeKind, ScopeID: scopeID, Text: text, UpdatedAt: now.Format(time.RFC3339Nano),
	}
	payload, _ := json.Marshal(struct {
		m8core.CanonicalMemoryRecord
		Reason string `json:"reason"`
		Digest string `json:"digest"`
	}{CanonicalMemoryRecord: rec, Reason: reason, Digest: contentDigest})
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'fact',?,'corrected',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, scopeKind, scopeID, factID, string(payload), idempotencyKey,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	return rec, nil
}

func (s *Store) UndoCanonicalCapture(ctx context.Context, subjectID, undoOperationID, operationID, idempotencyKey string) (m8core.MemoryUndoResult, error) {
	return undoCanonicalCapture(ctx, s.db, subjectID, undoOperationID, operationID, idempotencyKey)
}

func (r *AgentRuntimeRepository) UndoCanonicalCapture(ctx context.Context, subjectID, undoOperationID, operationID, idempotencyKey string) (m8core.MemoryUndoResult, error) {
	return undoCanonicalCapture(ctx, r.db, subjectID, undoOperationID, operationID, idempotencyKey)
}

func undoCanonicalCapture(ctx context.Context, db *sql.DB, subjectID, undoOperationID, operationID, idempotencyKey string) (m8core.MemoryUndoResult, error) {
	if subjectID == "" || undoOperationID == "" || operationID == "" {
		return m8core.MemoryUndoResult{}, fmt.Errorf("undo missing required fields")
	}
	if idempotencyKey == "" {
		idempotencyKey = operationID
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryUndoResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.MemoryUndoResult
		if json.Unmarshal([]byte(existing), &replay) == nil {
			replay.Replay = true
			return replay, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryUndoResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT h.fact_id,h.current_version,h.revision,h.is_forgotten,h.scope_kind,h.scope_id,v.created_at
		FROM memory_content_versions v
		JOIN memory_fact_heads h ON h.fact_id=v.fact_id
		WHERE v.origin_id=? AND v.origin_plane='native' AND v.fact_version=1 AND h.subject_id=?
		ORDER BY h.fact_id`, undoOperationID, subjectID)
	if err != nil {
		return m8core.MemoryUndoResult{}, err
	}
	type originFact struct {
		FactID, ScopeKind, ScopeID, CreatedAt string
		Version, Revision, Forgotten          int64
	}
	var facts []originFact
	for rows.Next() {
		var f originFact
		if err = rows.Scan(&f.FactID, &f.Version, &f.Revision, &f.Forgotten, &f.ScopeKind, &f.ScopeID, &f.CreatedAt); err != nil {
			rows.Close()
			return m8core.MemoryUndoResult{}, err
		}
		facts = append(facts, f)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return m8core.MemoryUndoResult{}, err
	}
	var prior string
	priorErr := tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE entity_type='capture' AND entity_id=? AND event_type='undone' AND subject_id=? ORDER BY event_seq DESC LIMIT 1`,
		undoOperationID, subjectID).Scan(&prior)
	alreadyUndone := priorErr == nil
	if priorErr != nil && !errors.Is(priorErr, sql.ErrNoRows) {
		return m8core.MemoryUndoResult{}, priorErr
	}
	if len(facts) == 0 {
		if alreadyUndone {
			var replay m8core.MemoryUndoResult
			if json.Unmarshal([]byte(prior), &replay) == nil {
				replay.Replay = true
				return replay, tx.Commit()
			}
		}
		return m8core.MemoryUndoResult{}, m8core.ErrNotFound
	}
	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	ids := make([]string, 0, len(facts))
	scopeKind, scopeID := facts[0].ScopeKind, facts[0].ScopeID
	for _, f := range facts {
		created, parseErr := time.Parse(time.RFC3339Nano, f.CreatedAt)
		if parseErr != nil {
			created, _ = time.Parse(time.RFC3339, f.CreatedAt)
		}
		if f.Version != 1 || created.Before(cutoff) || (f.Forgotten == 1 && !alreadyUndone) {
			return m8core.MemoryUndoResult{}, m8core.ErrUndoConflict
		}
		if f.Forgotten == 0 {
			ids = append(ids, f.FactID)
		}
	}
	if alreadyUndone && len(ids) == 0 {
		var replay m8core.MemoryUndoResult
		if json.Unmarshal([]byte(prior), &replay) == nil {
			replay.Replay = true
			return replay, tx.Commit()
		}
	}
	for _, factID := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE memory_fact_heads SET is_forgotten=1, revision=revision+1, updated_at=? WHERE fact_id=? AND subject_id=?`,
			now.Format(time.RFC3339Nano), factID, subjectID); err != nil {
			return m8core.MemoryUndoResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_content_bodies WHERE fact_id=?`, factID); err != nil {
			return m8core.MemoryUndoResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_search_documents WHERE fact_id=?`, factID); err != nil {
			return m8core.MemoryUndoResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE fact_id=?`, factID); err != nil {
			return m8core.MemoryUndoResult{}, err
		}
	}
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryUndoResult{}, err
	}
	result := m8core.MemoryUndoResult{ForgottenFactIDs: ids}
	payload, _ := json.Marshal(result)
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'capture',?,'undone',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, scopeKind, scopeID, undoOperationID, string(payload), idempotencyKey,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return m8core.MemoryUndoResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryUndoResult{}, err
	}
	rev, err := memoryDatabaseRevision(ctx, db, subjectID)
	if err != nil {
		return m8core.MemoryUndoResult{}, err
	}
	result.DatabaseRevision = rev
	return result, nil
}
