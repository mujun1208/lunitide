package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// GetMemoryV2Settings returns the R3 row, inserting the first-read default
// when neither a v2 row nor a legacy settings row exists.
func (s *Store) GetMemoryV2Settings(ctx context.Context, subjectID string) (m8core.MemoryV2Settings, error) {
	return getMemoryV2Settings(ctx, s.db, subjectID)
}

func getMemoryV2Settings(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, subjectID string) (m8core.MemoryV2Settings, error) {
	out, err := scanMemoryV2Settings(ctx, q, subjectID)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryV2Settings{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	legacy, legacyErr := getMemorySettings(ctx, q, subjectID)
	mode, last := "auto", "auto"
	var migratedEnabled any
	var migratedMode any
	if legacyErr == nil && legacy.CreatedAt != "" {
		mode = legacy.CaptureMode
		if !legacy.MemoryEnabled {
			mode = "off"
		}
		last = legacy.CaptureMode
		if last != "auto" && last != "manual" {
			last = "auto"
		}
		migratedEnabled = boolInt(legacy.MemoryEnabled)
		migratedMode = legacy.CaptureMode
	}
	_, err = q.ExecContext(ctx, `INSERT INTO memory_v2_settings(
		subject_id,revision,capture_mode,last_non_off_capture_mode,
		personal_memory_enabled,project_memory_enabled,
		migrated_memory_enabled,migrated_capture_mode,
		memory_v2_write,memory_v2_read,memory_v2_auto_capture,memory_v2_hybrid_recall,memory_v2_consolidation,
		created_at,updated_at)
		VALUES(?,1,?,?,1,1,?,?,0,0,0,0,0,?,?)`, subjectID, mode, last, migratedEnabled, migratedMode, now, now)
	if err != nil {
		out, scanErr := scanMemoryV2Settings(ctx, q, subjectID)
		if scanErr == nil {
			return out, nil
		}
		return m8core.MemoryV2Settings{}, err
	}
	return scanMemoryV2Settings(ctx, q, subjectID)
}

func scanMemoryV2Settings(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, subjectID string) (m8core.MemoryV2Settings, error) {
	var mode, last, created, updated string
	var revision, personal, project int64
	err := q.QueryRowContext(ctx, `SELECT revision,capture_mode,last_non_off_capture_mode,personal_memory_enabled,project_memory_enabled,created_at,updated_at
		FROM memory_v2_settings WHERE subject_id=?`, subjectID).Scan(&revision, &mode, &last, &personal, &project, &created, &updated)
	if err != nil {
		return m8core.MemoryV2Settings{}, err
	}
	return m8core.MemoryV2Settings{
		SubjectID:             subjectID,
		Revision:              revision,
		CaptureMode:           mode,
		LastNonOffCaptureMode: last,
		PersonalMemoryEnabled: personal == 1,
		ProjectMemoryEnabled:  project == 1,
	}, nil
}

// CompareAndSwapMemoryV2Settings updates user-visible R3 columns only.
func (s *Store) CompareAndSwapMemoryV2Settings(ctx context.Context, next m8core.MemoryV2Settings, expectedRevision int64) (m8core.MemoryV2Settings, error) {
	if next.CaptureMode != "auto" && next.CaptureMode != "manual" && next.CaptureMode != "off" {
		return m8core.MemoryV2Settings{}, errors.New("invalid capture mode")
	}
	last := next.LastNonOffCaptureMode
	if next.CaptureMode == "auto" || next.CaptureMode == "manual" {
		last = next.CaptureMode
	}
	if last != "auto" && last != "manual" {
		last = "auto"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryV2Settings{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE memory_v2_settings SET
		capture_mode=?, last_non_off_capture_mode=?,
		personal_memory_enabled=?, project_memory_enabled=?,
		revision=revision+1, updated_at=?
		WHERE subject_id=? AND revision=?`,
		next.CaptureMode, last, boolInt(next.PersonalMemoryEnabled), boolInt(next.ProjectMemoryEnabled),
		now, next.SubjectID, expectedRevision)
	if err != nil {
		return m8core.MemoryV2Settings{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return m8core.MemoryV2Settings{}, m8core.ErrSettingsConflict
	}
	legacyMode := last
	enabled := 0
	if next.CaptureMode != "off" {
		enabled = 1
		legacyMode = next.CaptureMode
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_settings(subject_id,memory_enabled,auto_nominate,growth_days,created_at,updated_at,capture_mode)
		VALUES(?,?,0,14,?,?,?)
		ON CONFLICT(subject_id) DO UPDATE SET memory_enabled=excluded.memory_enabled, capture_mode=excluded.capture_mode, updated_at=excluded.updated_at`,
		next.SubjectID, enabled, now, now, legacyMode); err != nil {
		return m8core.MemoryV2Settings{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryV2Settings{}, err
	}
	return getMemoryV2Settings(ctx, s.db, next.SubjectID)
}

func (s *Store) CreateCanonicalMemoryItem(ctx context.Context, item m8core.CanonicalMemoryWrite) (m8core.CanonicalMemoryResult, error) {
	return insertCanonicalMemoryItem(ctx, s.db, item)
}

func (r *AgentRuntimeRepository) CreateCanonicalMemoryItem(ctx context.Context, item m8core.CanonicalMemoryWrite) (m8core.CanonicalMemoryResult, error) {
	return insertCanonicalMemoryItem(ctx, r.db, item)
}

func insertCanonicalMemoryItem(ctx context.Context, db *sql.DB, item m8core.CanonicalMemoryWrite) (m8core.CanonicalMemoryResult, error) {
	item.Text = strings.TrimSpace(item.Text)
	if item.SubjectID == "" || item.Text == "" || item.OperationID == "" {
		return m8core.CanonicalMemoryResult{}, fmt.Errorf("canonical item missing required fields")
	}
	if item.ScopeKind == "" {
		item.ScopeKind = "user"
	}
	if item.ScopeKind == "user" {
		item.ScopeID = item.SubjectID
	}
	if item.Kind == "" {
		item.Kind = m8core.ClassifyMemoryKind(item.Text)
	}
	if item.IdempotencyKey == "" {
		item.IdempotencyKey = item.OperationID
	}
	digest := sha256.Sum256([]byte(item.Text))
	contentDigest := hex.EncodeToString(digest[:])
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, item.IdempotencyKey).Scan(&existing)
	if err == nil {
		var replay struct {
			m8core.CanonicalMemoryResult
			Digest string `json:"digest"`
		}
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.FactID != "" {
			if replay.Digest != "" && replay.Digest != contentDigest {
				return m8core.CanonicalMemoryResult{}, m8core.ErrOperationReplayMismatch
			}
			replay.Replay = true
			return replay.CanonicalMemoryResult, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.CanonicalMemoryResult{}, err
	}
	now := time.Now().UTC()
	factID := ulid.Make().String()
	evidenceID := ulid.Make().String()
	eventID := ulid.Make().String()
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_content_versions(
		fact_id,fact_version,subject_id,scope_kind,scope_id,kind,body_ref,authority,stability,
		importance,confidence,valid_from,valid_to,observed_at,ingested_at,origin_plane,origin_id,
		extractor_kind,extractor_model,content_digest,created_at)
		VALUES(?,1,?,?,?,?, 'body', ?, ?, 0.5, 1, NULL, NULL, ?, ?, 'native', ?, 'deterministic', '', ?, ?)`,
		factID, item.SubjectID, item.ScopeKind, item.ScopeID, item.Kind,
		m8core.MemoryAuthorityExplicit, m8core.MemoryStabilityDurable,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), item.OperationID,
		contentDigest, now.Format(time.RFC3339Nano)); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_content_bodies(fact_id,fact_version,canonical_text,canonical_json) VALUES(?,1,?,NULL)`,
		factID, item.Text); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_fact_heads(fact_id,subject_id,scope_kind,scope_id,current_version,revision,is_forgotten,updated_at)
		VALUES(?,?,?,?,1,1,0,?)`, factID, item.SubjectID, item.ScopeKind, item.ScopeID, now.Format(time.RFC3339Nano)); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	sourceKind, sourceRef, startByte, endByte, quoteDigest, err := canonicalEvidence(item, contentDigest)
	if err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_evidence_spans(id,fact_id,fact_version,source_kind,source_ref,start_byte,end_byte,quote_digest,created_at)
		VALUES(?,?,1,?,?,?,?,?,?)`,
		evidenceID, factID, sourceKind, sourceRef, startByte, endByte, quoteDigest, now.Format(time.RFC3339Nano)); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_search_documents(subject_id,scope_kind,scope_id,fact_id,fact_version,text) VALUES(?,?,?,?,1,?)`,
		item.SubjectID, item.ScopeKind, item.ScopeID, factID, item.Text); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	result := m8core.CanonicalMemoryResult{FactID: factID, Version: 1}
	payload, _ := json.Marshal(struct {
		m8core.CanonicalMemoryResult
		Digest string `json:"digest"`
	}{CanonicalMemoryResult: result, Digest: contentDigest})
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'fact',?,'created',?,?,?,?)`,
		nextSeq, eventID, item.SubjectID, item.ScopeKind, item.ScopeID, factID, string(payload), item.IdempotencyKey,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.CanonicalMemoryResult{}, err
	}
	return result, nil
}

func canonicalEvidence(item m8core.CanonicalMemoryWrite, contentDigest string) (sourceKind, sourceRef string, startByte, endByte any, quoteDigest string, err error) {
	quoteDigest = strings.TrimSpace(item.QuoteDigest)
	if quoteDigest == "" {
		quoteDigest = contentDigest
	}
	switch item.SourceKind {
	case "", m8core.MemorySourceDirect:
		return m8core.MemorySourceDirect, "operation:" + item.OperationID, nil, nil, quoteDigest, nil
	case m8core.MemorySourceUserMessage:
		if strings.TrimSpace(item.SourceRef) == "" || item.StartByte == nil || item.EndByte == nil {
			return "", "", nil, nil, "", fmt.Errorf("user_message evidence requires source ref and span")
		}
		return m8core.MemorySourceUserMessage, item.SourceRef, *item.StartByte, *item.EndByte, quoteDigest, nil
	default:
		return "", "", nil, nil, "", fmt.Errorf("unsupported evidence source")
	}
}

func syncMemoryV2FromLegacy(ctx context.Context, tx *sql.Tx, settings m8core.MemorySettings) error {
	effective := settings.CaptureMode
	if !settings.MemoryEnabled {
		effective = "off"
	}
	last := settings.CaptureMode
	if last != "auto" && last != "manual" {
		last = "auto"
	}
	res, err := tx.ExecContext(ctx, `UPDATE memory_v2_settings SET
		capture_mode=?,
		last_non_off_capture_mode=CASE WHEN ? IN ('auto','manual') THEN ? ELSE last_non_off_capture_mode END,
		revision=revision+1,
		updated_at=?
		WHERE subject_id=?`,
		effective, settings.CaptureMode, settings.CaptureMode, settings.UpdatedAt, settings.SubjectID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_v2_settings(
		subject_id,revision,capture_mode,last_non_off_capture_mode,
		personal_memory_enabled,project_memory_enabled,
		migrated_memory_enabled,migrated_capture_mode,
		memory_v2_write,memory_v2_read,memory_v2_auto_capture,memory_v2_hybrid_recall,memory_v2_consolidation,
		created_at,updated_at)
		VALUES(?,1,?,?,1,1,NULL,NULL,0,0,0,0,0,?,?)`,
		settings.SubjectID, effective, last, settings.UpdatedAt, settings.UpdatedAt)
	return err
}

func (s *Store) ListCanonicalMemoryItems(ctx context.Context, subjectID, scopeKind, scopeID string, limit int) ([]string, error) {
	return listCanonicalMemoryItems(ctx, s.db, subjectID, scopeKind, scopeID, limit)
}

func (r *AgentRuntimeRepository) ListCanonicalMemoryItems(ctx context.Context, subjectID, scopeKind, scopeID string, limit int) ([]string, error) {
	return listCanonicalMemoryItems(ctx, r.db, subjectID, scopeKind, scopeID, limit)
}

func listCanonicalMemoryItems(ctx context.Context, db *sql.DB, subjectID, scopeKind, scopeID string, limit int) ([]string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `SELECT b.canonical_text
		FROM memory_fact_heads h
		JOIN memory_content_bodies b ON b.fact_id=h.fact_id AND b.fact_version=h.current_version
		WHERE h.subject_id=? AND h.scope_kind=? AND h.scope_id=? AND h.is_forgotten=0
		ORDER BY h.updated_at DESC, h.fact_id DESC
		LIMIT ?`, subjectID, scopeKind, scopeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var text string
		if err = rows.Scan(&text); err != nil {
			return nil, err
		}
		out = append(out, text)
	}
	return out, rows.Err()
}

func (s *Store) ForgetCanonicalMemoryItem(ctx context.Context, subjectID, factID, operationID string, expectedRevision int64) error {
	return forgetCanonicalMemoryItem(ctx, s.db, subjectID, factID, operationID, expectedRevision)
}

func (r *AgentRuntimeRepository) ForgetCanonicalMemoryItem(ctx context.Context, subjectID, factID, operationID string, expectedRevision int64) error {
	return forgetCanonicalMemoryItem(ctx, r.db, subjectID, factID, operationID, expectedRevision)
}

func forgetCanonicalMemoryItem(ctx context.Context, db *sql.DB, subjectID, factID, operationID string, expectedRevision int64) error {
	if subjectID == "" || factID == "" {
		return fmt.Errorf("forget missing identity")
	}
	if operationID == "" {
		operationID = "forget:" + factID
	}
	idem := "memory.item.forget:" + operationID
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idem).Scan(&existing)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var headSubject, scopeKind, scopeID string
	var revision, forgotten int64
	err = tx.QueryRowContext(ctx, `SELECT subject_id,scope_kind,scope_id,revision,is_forgotten FROM memory_fact_heads WHERE fact_id=?`, factID).Scan(
		&headSubject, &scopeKind, &scopeID, &revision, &forgotten)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.ErrNotFound
	}
	if err != nil {
		return err
	}
	if headSubject != subjectID {
		return m8core.ErrNotFound
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return m8core.ErrRevisionConflict
	}
	now := time.Now().UTC()
	if forgotten == 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE memory_fact_heads SET is_forgotten=1, revision=revision+1, updated_at=? WHERE fact_id=? AND subject_id=?`,
			now.Format(time.RFC3339Nano), factID, subjectID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_content_bodies WHERE fact_id=?`, factID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_search_documents WHERE fact_id=?`, factID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE fact_id=?`, factID); err != nil {
			return err
		}
	}
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return err
	}
	payload, _ := json.Marshal(m8core.CanonicalMemoryResult{FactID: factID})
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'fact',?,'forgotten',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, scopeKind, scopeID, factID, string(payload), idem,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetCanonicalMemoryItem(ctx context.Context, subjectID, factID string) (m8core.CanonicalMemoryRecord, error) {
	return getCanonicalMemoryItem(ctx, s.db, subjectID, factID)
}

func (r *AgentRuntimeRepository) GetCanonicalMemoryItem(ctx context.Context, subjectID, factID string) (m8core.CanonicalMemoryRecord, error) {
	return getCanonicalMemoryItem(ctx, r.db, subjectID, factID)
}

func getCanonicalMemoryItem(ctx context.Context, db *sql.DB, subjectID, factID string) (m8core.CanonicalMemoryRecord, error) {
	var rec m8core.CanonicalMemoryRecord
	var forgotten int64
	var text sql.NullString
	err := db.QueryRowContext(ctx, `SELECT h.fact_id,h.current_version,h.revision,v.kind,h.scope_kind,h.scope_id,b.canonical_text,h.is_forgotten,h.updated_at
		FROM memory_fact_heads h
		JOIN memory_content_versions v ON v.fact_id=h.fact_id AND v.fact_version=h.current_version
		LEFT JOIN memory_content_bodies b ON b.fact_id=h.fact_id AND b.fact_version=h.current_version
		WHERE h.fact_id=? AND h.subject_id=?`, factID, subjectID).Scan(
		&rec.FactID, &rec.Version, &rec.Revision, &rec.Kind, &rec.ScopeKind, &rec.ScopeID, &text, &forgotten, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrNotFound
	}
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	rec.Forgotten = forgotten == 1
	if text.Valid && !rec.Forgotten {
		rec.Text = text.String
	}
	return rec, nil
}

func (s *Store) ListCanonicalMemoryRecords(ctx context.Context, subjectID, scopeKind, scopeID, kind string, limit int) ([]m8core.CanonicalMemoryRecord, error) {
	return listCanonicalMemoryRecords(ctx, s.db, subjectID, scopeKind, scopeID, kind, limit)
}

func (r *AgentRuntimeRepository) ListCanonicalMemoryRecords(ctx context.Context, subjectID, scopeKind, scopeID, kind string, limit int) ([]m8core.CanonicalMemoryRecord, error) {
	return listCanonicalMemoryRecords(ctx, r.db, subjectID, scopeKind, scopeID, kind, limit)
}

func listCanonicalMemoryRecords(ctx context.Context, db *sql.DB, subjectID, scopeKind, scopeID, kind string, limit int) ([]m8core.CanonicalMemoryRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	args := []any{subjectID, scopeKind, scopeID}
	q := `SELECT h.fact_id,h.current_version,h.revision,v.kind,h.scope_kind,h.scope_id,b.canonical_text,h.updated_at
		FROM memory_fact_heads h
		JOIN memory_content_versions v ON v.fact_id=h.fact_id AND v.fact_version=h.current_version
		JOIN memory_content_bodies b ON b.fact_id=h.fact_id AND b.fact_version=h.current_version
		WHERE h.subject_id=? AND h.scope_kind=? AND h.scope_id=? AND h.is_forgotten=0`
	if kind != "" {
		q += ` AND v.kind=?`
		args = append(args, kind)
	}
	q += ` ORDER BY h.updated_at DESC, h.fact_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []m8core.CanonicalMemoryRecord
	for rows.Next() {
		var rec m8core.CanonicalMemoryRecord
		if err = rows.Scan(&rec.FactID, &rec.Version, &rec.Revision, &rec.Kind, &rec.ScopeKind, &rec.ScopeID, &rec.Text, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) MemoryDatabaseRevision(ctx context.Context, subjectID string) (int64, error) {
	return memoryDatabaseRevision(ctx, s.db, subjectID)
}

func (r *AgentRuntimeRepository) MemoryDatabaseRevision(ctx context.Context, subjectID string) (int64, error) {
	return memoryDatabaseRevision(ctx, r.db, subjectID)
}

func memoryDatabaseRevision(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, subjectID string) (int64, error) {
	var rev int64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0) FROM memory_event_log WHERE subject_id=?`, subjectID).Scan(&rev)
	return rev, err
}

func (r *AgentRuntimeRepository) GetUserMessageText(ctx context.Context, messageID string) (string, error) {
	var text string
	err := r.db.QueryRowContext(ctx, `SELECT p.text
		FROM messages m
		JOIN message_parts p ON p.message_id=m.id AND p.ordinal=1 AND p.type='text'
		WHERE m.id=? AND m.role='user'`, messageID).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", m8core.ErrNotFound
	}
	return text, err
}

func (s *Store) CanonicalEvidenceSpan(ctx context.Context, factID string) (sourceKind, sourceRef string, startByte, endByte sql.NullInt64, quoteDigest string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT source_kind,source_ref,start_byte,end_byte,quote_digest
		FROM memory_evidence_spans WHERE fact_id=? ORDER BY created_at LIMIT 1`, factID).
		Scan(&sourceKind, &sourceRef, &startByte, &endByte, &quoteDigest)
	return
}
