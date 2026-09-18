package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func (s *Store) BuildMemoryGeneration(ctx context.Context, subjectID, scopeKind, scopeID, parentID, operationID, idempotencyKey string) (m8core.MemoryGeneration, error) {
	return buildMemoryGeneration(ctx, s.db, subjectID, scopeKind, scopeID, parentID, operationID, idempotencyKey)
}

func (r *AgentRuntimeRepository) BuildMemoryGeneration(ctx context.Context, subjectID, scopeKind, scopeID, parentID, operationID, idempotencyKey string) (m8core.MemoryGeneration, error) {
	return buildMemoryGeneration(ctx, r.db, subjectID, scopeKind, scopeID, parentID, operationID, idempotencyKey)
}

func buildMemoryGeneration(ctx context.Context, db *sql.DB, subjectID, scopeKind, scopeID, parentID, operationID, idempotencyKey string) (m8core.MemoryGeneration, error) {
	if subjectID == "" {
		return m8core.MemoryGeneration{}, fmt.Errorf("generation missing subject")
	}
	if scopeKind == "" {
		scopeKind = "user"
	}
	if scopeKind == "user" {
		scopeID = subjectID
	}
	if idempotencyKey == "" {
		idempotencyKey = operationID
	}
	if idempotencyKey == "" {
		idempotencyKey = ulid.Make().String()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryGeneration{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.MemoryGeneration
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.GenerationID != "" {
			return replay, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, err
	}
	var cutoff int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0) FROM memory_event_log WHERE subject_id=?`, subjectID).Scan(&cutoff); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := ulid.Make().String()
	parent := sql.NullString{String: parentID, Valid: parentID != ""}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_generations(
		generation_id,subject_id,scope_kind,scope_id,parent_generation_id,source_cutoff_seq,state,builder_version,stats_json,activated_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?, 'ready', 'index-v1', '{}', NULL, ?, ?)`,
		id, subjectID, scopeKind, scopeID, parent, cutoff, now, now); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT fact_id,current_version FROM memory_fact_heads
		WHERE subject_id=? AND scope_kind=? AND scope_id=? AND is_forgotten=0`, subjectID, scopeKind, scopeID)
	if err != nil {
		return m8core.MemoryGeneration{}, err
	}
	var members int64
	for rows.Next() {
		var factID string
		var version int64
		if err = rows.Scan(&factID, &version); err != nil {
			rows.Close()
			return m8core.MemoryGeneration{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_generation_members(generation_id,fact_id,fact_version) VALUES(?,?,?)`, id, factID, version); err != nil {
			rows.Close()
			return m8core.MemoryGeneration{}, err
		}
		members++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	gen := m8core.MemoryGeneration{
		GenerationID:       id,
		ParentGenerationID: parentID,
		SubjectID:          subjectID,
		ScopeKind:          scopeKind,
		ScopeID:            scopeID,
		State:              "ready",
		SourceCutoffSeq:    cutoff,
		BuilderVersion:     "index-v1",
		MemberCount:        members,
		Revision:           1,
		CreatedAt:          now,
		ReadyAt:            now,
	}
	payload, _ := json.Marshal(gen)
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'generation',?,'built',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, scopeKind, scopeID, id, string(payload), idempotencyKey, now, now); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	return gen, nil
}

func (s *Store) ListMemoryGenerations(ctx context.Context, subjectID, scopeKind, scopeID, cursor string, limit int) ([]m8core.MemoryGeneration, string, error) {
	return listMemoryGenerations(ctx, s.db, subjectID, scopeKind, scopeID, cursor, limit)
}

func (r *AgentRuntimeRepository) ListMemoryGenerations(ctx context.Context, subjectID, scopeKind, scopeID, cursor string, limit int) ([]m8core.MemoryGeneration, string, error) {
	return listMemoryGenerations(ctx, r.db, subjectID, scopeKind, scopeID, cursor, limit)
}

func listMemoryGenerations(ctx context.Context, db *sql.DB, subjectID, scopeKind, scopeID, cursor string, limit int) ([]m8core.MemoryGeneration, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if scopeKind == "user" {
		scopeID = subjectID
	}
	args := []any{subjectID, scopeKind, scopeID}
	q := `SELECT g.generation_id,COALESCE(g.parent_generation_id,''),g.scope_kind,g.scope_id,g.state,g.source_cutoff_seq,g.builder_version,g.created_at,COALESCE(g.activated_at,''),
		(SELECT COUNT(*) FROM memory_generation_members m WHERE m.generation_id=g.generation_id),
		COALESCE((SELECT revision FROM memory_generation_heads h WHERE h.subject_id=g.subject_id AND h.scope_kind=g.scope_kind AND h.scope_id=g.scope_id),1)
		FROM memory_generations g
		WHERE g.subject_id=? AND g.scope_kind=? AND g.scope_id=?`
	if cursor != "" {
		q += ` AND (g.created_at < (SELECT created_at FROM memory_generations WHERE generation_id=?) OR (g.created_at = (SELECT created_at FROM memory_generations WHERE generation_id=?) AND g.generation_id > ?))`
		args = append(args, cursor, cursor, cursor)
	}
	q += ` ORDER BY g.created_at DESC, g.generation_id ASC LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []m8core.MemoryGeneration
	for rows.Next() {
		var g m8core.MemoryGeneration
		if err = rows.Scan(&g.GenerationID, &g.ParentGenerationID, &g.ScopeKind, &g.ScopeID, &g.State, &g.SourceCutoffSeq, &g.BuilderVersion, &g.CreatedAt, &g.ActivatedAt, &g.MemberCount, &g.Revision); err != nil {
			return nil, "", err
		}
		g.SubjectID = subjectID
		if g.State == "ready" || g.State == "active" || g.State == "archived" {
			g.ReadyAt = g.CreatedAt
		}
		out = append(out, g)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		next = out[limit-1].GenerationID
		out = out[:limit]
	}
	return out, next, nil
}

func (s *Store) GetMemoryGeneration(ctx context.Context, subjectID, generationID string) (m8core.MemoryGeneration, error) {
	return getMemoryGeneration(ctx, s.db, subjectID, generationID)
}

func (r *AgentRuntimeRepository) GetMemoryGeneration(ctx context.Context, subjectID, generationID string) (m8core.MemoryGeneration, error) {
	return getMemoryGeneration(ctx, r.db, subjectID, generationID)
}

func getMemoryGeneration(ctx context.Context, db *sql.DB, subjectID, generationID string) (m8core.MemoryGeneration, error) {
	var g m8core.MemoryGeneration
	err := db.QueryRowContext(ctx, `SELECT generation_id,subject_id,scope_kind,scope_id,COALESCE(parent_generation_id,''),state,source_cutoff_seq,builder_version,created_at,COALESCE(activated_at,''),
		(SELECT COUNT(*) FROM memory_generation_members m WHERE m.generation_id=memory_generations.generation_id),
		COALESCE((SELECT revision FROM memory_generation_heads h WHERE h.subject_id=memory_generations.subject_id AND h.scope_kind=memory_generations.scope_kind AND h.scope_id=memory_generations.scope_id),1)
		FROM memory_generations WHERE generation_id=?`, generationID).
		Scan(&g.GenerationID, &g.SubjectID, &g.ScopeKind, &g.ScopeID, &g.ParentGenerationID, &g.State, &g.SourceCutoffSeq, &g.BuilderVersion, &g.CreatedAt, &g.ActivatedAt, &g.MemberCount, &g.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationNotFound
	}
	if err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if g.SubjectID != subjectID {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationNotFound
	}
	if g.State == "ready" || g.State == "active" || g.State == "archived" {
		g.ReadyAt = g.CreatedAt
	}
	if g.Revision < 1 {
		g.Revision = 1
	}
	return g, nil
}

func (s *Store) PreviewMemoryGeneration(ctx context.Context, subjectID, generationID, cursor string, limit int) (m8core.MemoryGeneration, []m8core.MemoryGenerationChange, string, error) {
	return previewMemoryGeneration(ctx, s.db, subjectID, generationID, cursor, limit)
}

func (r *AgentRuntimeRepository) PreviewMemoryGeneration(ctx context.Context, subjectID, generationID, cursor string, limit int) (m8core.MemoryGeneration, []m8core.MemoryGenerationChange, string, error) {
	return previewMemoryGeneration(ctx, r.db, subjectID, generationID, cursor, limit)
}

func previewMemoryGeneration(ctx context.Context, db *sql.DB, subjectID, generationID, cursor string, limit int) (m8core.MemoryGeneration, []m8core.MemoryGenerationChange, string, error) {
	gen, err := getMemoryGeneration(ctx, db, subjectID, generationID)
	if err != nil {
		return m8core.MemoryGeneration{}, nil, "", err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	args := []any{generationID}
	q := `SELECT m.fact_id,m.fact_version,COALESCE(b.canonical_text,'')
		FROM memory_generation_members m
		LEFT JOIN memory_content_bodies b ON b.fact_id=m.fact_id AND b.fact_version=m.fact_version
		WHERE m.generation_id=?`
	if cursor != "" {
		q += ` AND m.fact_id > ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY m.fact_id ASC LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return gen, nil, "", err
	}
	defer rows.Close()
	var changes []m8core.MemoryGenerationChange
	for rows.Next() {
		var factID, text string
		var version int64
		if err = rows.Scan(&factID, &version, &text); err != nil {
			return gen, nil, "", err
		}
		to := version
		chg := m8core.MemoryGenerationChange{Change: "add", FactID: factID, ToVersion: &to, AfterText: clipGenerationText(text), ReasonCodes: []string{"index"}}
		changes = append(changes, chg)
	}
	if err = rows.Err(); err != nil {
		return gen, nil, "", err
	}
	next := ""
	if len(changes) > limit {
		next = changes[limit-1].FactID
		changes = changes[:limit]
	}
	return gen, changes, next, nil
}

func (s *Store) ActivateMemoryGeneration(ctx context.Context, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, error) {
	return activateMemoryGeneration(ctx, s.db, subjectID, generationID, operationID, idempotencyKey, expectedRevision)
}

func (r *AgentRuntimeRepository) ActivateMemoryGeneration(ctx context.Context, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, error) {
	return activateMemoryGeneration(ctx, r.db, subjectID, generationID, operationID, idempotencyKey, expectedRevision)
}

func activateMemoryGeneration(ctx context.Context, db *sql.DB, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, error) {
	if expectedRevision < 1 || generationID == "" || operationID == "" || idempotencyKey == "" {
		return m8core.MemoryGeneration{}, fmt.Errorf("activate missing required fields")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryGeneration{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.MemoryGeneration
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.GenerationID != "" {
			if replay.GenerationID != generationID {
				return m8core.MemoryGeneration{}, m8core.ErrOperationReplayMismatch
			}
			return replay, tx.Commit()
		}
		return m8core.MemoryGeneration{}, m8core.ErrOperationReplayMismatch
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, err
	}
	var gen m8core.MemoryGeneration
	err = tx.QueryRowContext(ctx, `SELECT generation_id,subject_id,scope_kind,scope_id,COALESCE(parent_generation_id,''),state,source_cutoff_seq,builder_version,created_at
		FROM memory_generations WHERE generation_id=?`, generationID).
		Scan(&gen.GenerationID, &gen.SubjectID, &gen.ScopeKind, &gen.ScopeID, &gen.ParentGenerationID, &gen.State, &gen.SourceCutoffSeq, &gen.BuilderVersion, &gen.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationNotFound
	}
	if err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if gen.SubjectID != subjectID {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationNotFound
	}
	if gen.State != "ready" && gen.State != "archived" {
		if gen.State == "active" {
			return m8core.MemoryGeneration{}, m8core.ErrGenerationActive
		}
		return m8core.MemoryGeneration{}, m8core.ErrGenerationNotReady
	}
	var headRev sql.NullInt64
	var activeID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT revision,active_generation_id FROM memory_generation_heads WHERE subject_id=? AND scope_kind=? AND scope_id=?`,
		subjectID, gen.ScopeKind, gen.ScopeID).Scan(&headRev, &activeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, err
	}
	currentRev := int64(1)
	if headRev.Valid {
		currentRev = headRev.Int64
	}
	if currentRev != expectedRevision {
		return m8core.MemoryGeneration{}, m8core.ErrRevisionConflict
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if activeID.Valid && activeID.String != "" && activeID.String != generationID {
		if _, err = tx.ExecContext(ctx, `UPDATE memory_generations SET state='archived', updated_at=? WHERE generation_id=? AND state='active'`, now, activeID.String); err != nil {
			return m8core.MemoryGeneration{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_generations SET state='active', activated_at=?, updated_at=? WHERE generation_id=?`, now, now, generationID); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if headRev.Valid {
		res, err := tx.ExecContext(ctx, `UPDATE memory_generation_heads SET active_generation_id=?, revision=revision+1, updated_at=? WHERE subject_id=? AND scope_kind=? AND scope_id=? AND revision=?`,
			generationID, now, subjectID, gen.ScopeKind, gen.ScopeID, expectedRevision)
		if err != nil {
			return m8core.MemoryGeneration{}, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return m8core.MemoryGeneration{}, m8core.ErrRevisionConflict
		}
		gen.Revision = expectedRevision + 1
	} else {
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_generation_heads(subject_id,scope_kind,scope_id,active_generation_id,revision,updated_at) VALUES(?,?,?,?,1,?)`,
			subjectID, gen.ScopeKind, gen.ScopeID, generationID, now); err != nil {
			return m8core.MemoryGeneration{}, err
		}
		gen.Revision = 1
	}
	gen.State = "active"
	gen.ActivatedAt = now
	gen.ReadyAt = gen.CreatedAt
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_generation_members WHERE generation_id=?`, generationID).Scan(&gen.MemberCount); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	payload, _ := json.Marshal(gen)
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'generation',?,'activated',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, gen.ScopeKind, gen.ScopeID, generationID, string(payload), idempotencyKey, now, now); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	return gen, nil
}

func (s *Store) DiscardMemoryGeneration(ctx context.Context, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, error) {
	return discardMemoryGeneration(ctx, s.db, subjectID, generationID, operationID, idempotencyKey, expectedRevision)
}

func (r *AgentRuntimeRepository) DiscardMemoryGeneration(ctx context.Context, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, error) {
	return discardMemoryGeneration(ctx, r.db, subjectID, generationID, operationID, idempotencyKey, expectedRevision)
}

func discardMemoryGeneration(ctx context.Context, db *sql.DB, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, error) {
	if expectedRevision < 1 || generationID == "" || operationID == "" || idempotencyKey == "" {
		return m8core.MemoryGeneration{}, fmt.Errorf("discard missing required fields")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryGeneration{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.MemoryGeneration
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.GenerationID != "" {
			if replay.GenerationID != generationID {
				return m8core.MemoryGeneration{}, m8core.ErrOperationReplayMismatch
			}
			return replay, tx.Commit()
		}
		return m8core.MemoryGeneration{}, m8core.ErrOperationReplayMismatch
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, err
	}
	var gen m8core.MemoryGeneration
	err = tx.QueryRowContext(ctx, `SELECT generation_id,subject_id,scope_kind,scope_id,COALESCE(parent_generation_id,''),state,source_cutoff_seq,builder_version,created_at
		FROM memory_generations WHERE generation_id=?`, generationID).
		Scan(&gen.GenerationID, &gen.SubjectID, &gen.ScopeKind, &gen.ScopeID, &gen.ParentGenerationID, &gen.State, &gen.SourceCutoffSeq, &gen.BuilderVersion, &gen.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationNotFound
	}
	if err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if gen.SubjectID != subjectID {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationNotFound
	}
	if gen.State == "active" || gen.State == "archived" {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationActive
	}
	if gen.State != "building" && gen.State != "ready" && gen.State != "failed" {
		return m8core.MemoryGeneration{}, m8core.ErrGenerationStateInvalid
	}
	var headRev sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM memory_generation_heads WHERE subject_id=? AND scope_kind=? AND scope_id=?`,
		subjectID, gen.ScopeKind, gen.ScopeID).Scan(&headRev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryGeneration{}, err
	}
	currentRev := int64(1)
	if headRev.Valid {
		currentRev = headRev.Int64
	}
	if currentRev != expectedRevision {
		return m8core.MemoryGeneration{}, m8core.ErrRevisionConflict
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE memory_consolidation_jobs SET state='cancelled', fence=fence+1, updated_at=? WHERE generation_id=? AND state IN ('queued','running','deferred')`, now, generationID); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_generations SET state='discarded', updated_at=? WHERE generation_id=?`, now, generationID); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	gen.State = "discarded"
	gen.Revision = expectedRevision
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_generation_members WHERE generation_id=?`, generationID).Scan(&gen.MemberCount); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	payload, _ := json.Marshal(gen)
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'generation',?,'discarded',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, gen.ScopeKind, gen.ScopeID, generationID, string(payload), idempotencyKey, now, now); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryGeneration{}, err
	}
	return gen, nil
}

func clipGenerationText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 8192 {
		return s[:8192]
	}
	return s
}
