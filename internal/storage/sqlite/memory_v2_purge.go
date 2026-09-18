package sqlite

import (
	"context"
	"crypto/rand"
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

func (s *Store) PutMemoryReview(ctx context.Context, subjectID string, review m8core.MemoryReviewWrite) error {
	return putMemoryReview(ctx, s.db, subjectID, review)
}

func (r *AgentRuntimeRepository) PutMemoryReview(ctx context.Context, subjectID string, review m8core.MemoryReviewWrite) error {
	return putMemoryReview(ctx, r.db, subjectID, review)
}

func putMemoryReview(ctx context.Context, db *sql.DB, subjectID string, review m8core.MemoryReviewWrite) error {
	if review.CandidateID == "" || review.Kind == "" {
		return fmt.Errorf("review missing identity")
	}
	if review.ScopeKind == "" {
		review.ScopeKind = "user"
	}
	if review.ScopeKind == "user" {
		review.ScopeID = subjectID
	}
	if review.Novelty == "" {
		review.Novelty = "unknown"
	}
	codes, _ := json.Marshal(review.ReasonCodes)
	if len(review.ReasonCodes) == 0 {
		codes = []byte(`["quiet_review"]`)
	}
	extractor, _ := json.Marshal(map[string]string{"text": strings.TrimSpace(review.Text)})
	var conflict any
	if review.ConflictFactID != "" {
		conflict = review.ConflictFactID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(ctx, `INSERT INTO memory_candidate_assessments(
		candidate_id,kind,decision,reason_codes_json,scope_kind,scope_id,novelty,conflict_fact_id,extractor_json,schema_version,created_at)
		VALUES(?,?,'review',?,?,?,?,?,?,1,?)`,
		review.CandidateID, review.Kind, string(codes), review.ScopeKind, review.ScopeID, review.Novelty, conflict, string(extractor), now)
	if err != nil {
		return err
	}
	var nextSeq int64
	if err = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'review',?,'opened','{}',?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, review.ScopeKind, review.ScopeID, review.CandidateID, review.CandidateID+":opened", now, now)
	return err
}

func (s *Store) ListMemoryReviews(ctx context.Context, subjectID, scopeKind, scopeID string, limit int) ([]m8core.MemoryReviewRecord, error) {
	return listMemoryReviews(ctx, s.db, subjectID, scopeKind, scopeID, limit)
}

func (r *AgentRuntimeRepository) ListMemoryReviews(ctx context.Context, subjectID, scopeKind, scopeID string, limit int) ([]m8core.MemoryReviewRecord, error) {
	return listMemoryReviews(ctx, r.db, subjectID, scopeKind, scopeID, limit)
}

func listMemoryReviews(ctx context.Context, db *sql.DB, subjectID, scopeKind, scopeID string, limit int) ([]m8core.MemoryReviewRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if scopeKind == "user" {
		scopeID = subjectID
	}
	rows, err := db.QueryContext(ctx, `SELECT candidate_id,kind,novelty,reason_codes_json,conflict_fact_id,extractor_json,scope_kind,scope_id,created_at
		FROM memory_candidate_assessments
		WHERE decision='review' AND scope_kind=? AND scope_id=?
		ORDER BY created_at DESC, candidate_id DESC LIMIT ?`, scopeKind, scopeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []m8core.MemoryReviewRecord
	for rows.Next() {
		var rec m8core.MemoryReviewRecord
		var conflict sql.NullString
		var reasons, extractor string
		if err = rows.Scan(&rec.ReviewID, &rec.Kind, &rec.Novelty, &reasons, &conflict, &extractor, &rec.ScopeKind, &rec.ScopeID, &rec.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(reasons), &rec.ReasonCodes)
		var extracted struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(extractor), &extracted)
		rec.Text = extracted.Text
		if conflict.Valid {
			rec.ConflictFactID = conflict.String
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) ResolveMemoryReview(ctx context.Context, subjectID, reviewID, decision, text, operationID, idempotencyKey string, expectedRevision int64) (m8core.CanonicalMemoryRecord, error) {
	return resolveMemoryReview(ctx, s.db, subjectID, reviewID, decision, text, operationID, idempotencyKey, expectedRevision)
}

func (r *AgentRuntimeRepository) ResolveMemoryReview(ctx context.Context, subjectID, reviewID, decision, text, operationID, idempotencyKey string, expectedRevision int64) (m8core.CanonicalMemoryRecord, error) {
	return resolveMemoryReview(ctx, r.db, subjectID, reviewID, decision, text, operationID, idempotencyKey, expectedRevision)
}

func resolveMemoryReview(ctx context.Context, db *sql.DB, subjectID, reviewID, decision, text, operationID, idempotencyKey string, expectedRevision int64) (m8core.CanonicalMemoryRecord, error) {
	if idempotencyKey == "" {
		idempotencyKey = operationID
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.CanonicalMemoryRecord
		_ = json.Unmarshal([]byte(existing), &replay)
		return replay, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return m8core.CanonicalMemoryRecord{}, err
	}
	var rec m8core.MemoryReviewRecord
	var conflict sql.NullString
	var extractor, currentDecision string
	err = tx.QueryRowContext(ctx, `SELECT candidate_id,kind,decision,novelty,conflict_fact_id,extractor_json,scope_kind,scope_id,created_at
		FROM memory_candidate_assessments WHERE candidate_id=?`, reviewID).Scan(
		&rec.ReviewID, &rec.Kind, &currentDecision, &rec.Novelty, &conflict, &extractor, &rec.ScopeKind, &rec.ScopeID, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrNotFound
	}
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if currentDecision != "review" {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrNotFound
	}
	if rec.ScopeKind == "user" && rec.ScopeID != subjectID {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrNotFound
	}
	var dbRev int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0) FROM memory_event_log WHERE subject_id=?`, subjectID).Scan(&dbRev); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if expectedRevision > 0 && dbRev != expectedRevision {
		return m8core.CanonicalMemoryRecord{}, m8core.ErrRevisionConflict
	}
	var extracted struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal([]byte(extractor), &extracted)
	body := strings.TrimSpace(text)
	if decision == "accept" {
		body = strings.TrimSpace(extracted.Text)
	}
	mapped := "drop"
	if decision == "accept" || decision == "correct" {
		mapped = "auto_accept"
		if body == "" {
			return m8core.CanonicalMemoryRecord{}, m8core.ErrNotFound
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_candidate_assessments SET decision=? WHERE candidate_id=? AND decision='review'`, mapped, reviewID); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	now := time.Now().UTC()
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	if decision == "reject" {
		payload, _ := json.Marshal(map[string]any{"reviewId": reviewID, "decision": decision})
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
			event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
			VALUES(?,?,?,?,?,'review',?,'resolved',?,?,?,?)`,
			nextSeq, ulid.Make().String(), subjectID, rec.ScopeKind, rec.ScopeID, reviewID, string(payload), idempotencyKey,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return m8core.CanonicalMemoryRecord{}, err
		}
		return m8core.CanonicalMemoryRecord{}, tx.Commit()
	}
	if err = tx.Commit(); err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	created, err := insertCanonicalMemoryItem(ctx, db, m8core.CanonicalMemoryWrite{
		SubjectID:      subjectID,
		ScopeKind:      rec.ScopeKind,
		ScopeID:        rec.ScopeID,
		Kind:           rec.Kind,
		Text:           body,
		OperationID:    operationID,
		IdempotencyKey: operationID + ":review",
		SourceKind:     m8core.MemorySourceDirect,
	})
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	item, err := getCanonicalMemoryItem(ctx, db, subjectID, created.FactID)
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, err
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO memory_fact_candidate_links(candidate_id,fact_id,fact_version,relation,created_at) VALUES(?,?,?,'accepted',?)`,
		reviewID, item.FactID, item.Version, now.Format(time.RFC3339Nano))
	payload, _ := json.Marshal(item)
	_, _ = db.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		SELECT COALESCE(MAX(event_seq),0)+1,?,?,?,?,?,'review',?,'resolved',?,?,?,? FROM memory_event_log`,
		ulid.Make().String(), subjectID, rec.ScopeKind, rec.ScopeID, reviewID, string(payload), idempotencyKey,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	_ = conflict
	return item, nil
}

func (s *Store) PrepareMemoryPurgeGrant(ctx context.Context, subjectID, scopeKind, scopeID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryPurgePrepareResult, error) {
	return prepareMemoryPurgeGrant(ctx, s.db, subjectID, scopeKind, scopeID, operationID, idempotencyKey, expectedRevision)
}

func (r *AgentRuntimeRepository) PrepareMemoryPurgeGrant(ctx context.Context, subjectID, scopeKind, scopeID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryPurgePrepareResult, error) {
	return prepareMemoryPurgeGrant(ctx, r.db, subjectID, scopeKind, scopeID, operationID, idempotencyKey, expectedRevision)
}

func prepareMemoryPurgeGrant(ctx context.Context, db *sql.DB, subjectID, scopeKind, scopeID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryPurgePrepareResult, error) {
	if scopeKind == "user" {
		scopeID = subjectID
	}
	if idempotencyKey == "" {
		idempotencyKey = operationID
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.MemoryPurgePrepareResult
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.ConfirmationToken != "" {
			replay.Replay = true
			return replay, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	var dbRev int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0) FROM memory_event_log WHERE subject_id=?`, subjectID).Scan(&dbRev); err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	if dbRev != expectedRevision {
		return m8core.MemoryPurgePrepareResult{}, m8core.ErrRevisionConflict
	}
	counts, snapshot, err := memoryPurgeSnapshot(ctx, tx, subjectID, scopeKind, scopeID)
	if err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	expires := now.Add(5 * time.Minute)
	var grantScope any
	if scopeKind == "project" {
		grantScope = scopeID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_purge_grants(grant_digest,subject_id,scope_kind,scope_id,snapshot_digest,expected_revision,expires_at,consumed_at)
		VALUES(?,?,?,?,?,?,?,NULL)`, digest, subjectID, scopeKind, grantScope, snapshot, expectedRevision, expires.Format(time.RFC3339Nano)); err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	result := m8core.MemoryPurgePrepareResult{
		Counts: counts, SnapshotDigest: snapshot, ConfirmationToken: token,
		ExpiresAt: expires.Format(time.RFC3339), OperationID: operationID, DatabaseRevision: dbRev,
	}
	payload, _ := json.Marshal(result)
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'purge',?,'prepared',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, scopeKind, scopeID, operationID, string(payload), idempotencyKey,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryPurgePrepareResult{}, err
	}
	return result, nil
}

func (s *Store) ConsumeMemoryPurgeGrant(ctx context.Context, subjectID, token, snapshotDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryOpsCounts, string, error) {
	return consumeMemoryPurgeGrant(ctx, s.db, subjectID, token, snapshotDigest, operationID, idempotencyKey, expectedRevision)
}

func (r *AgentRuntimeRepository) ConsumeMemoryPurgeGrant(ctx context.Context, subjectID, token, snapshotDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryOpsCounts, string, error) {
	return consumeMemoryPurgeGrant(ctx, r.db, subjectID, token, snapshotDigest, operationID, idempotencyKey, expectedRevision)
}

type purgeConsumePayload struct {
	FactsTombstoned int64  `json:"factsTombstoned"`
	Candidates      int64  `json:"candidates"`
	GrowthRows      int64  `json:"growthRows"`
	Flags           int64  `json:"flags"`
	Traces          int64  `json:"traces"`
	Memories        int64  `json:"memories"`
	ScopeKind       string `json:"scopeKind"`
}

func consumeMemoryPurgeGrant(ctx context.Context, db *sql.DB, subjectID, token, snapshotDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryOpsCounts, string, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(token))
	if err != nil || len(raw) != 32 {
		return m8core.MemoryOpsCounts{}, "", m8core.ErrPurgeGrantInvalid
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if idempotencyKey == "" {
		idempotencyKey = operationID
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay purgeConsumePayload
		if json.Unmarshal([]byte(existing), &replay) == nil {
			return m8core.MemoryOpsCounts{
				FactsTombstoned: replay.FactsTombstoned, Candidates: replay.Candidates,
				GrowthRows: replay.GrowthRows, Flags: replay.Flags, Traces: replay.Traces, Memories: replay.Memories,
			}, replay.ScopeKind, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryOpsCounts{}, "", err
	}
	var grantSubject, scopeKind string
	var scopeID sql.NullString
	var storedSnapshot string
	var storedRev int64
	var expiresAt string
	var consumed sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT subject_id,scope_kind,scope_id,snapshot_digest,expected_revision,expires_at,consumed_at
		FROM memory_purge_grants WHERE grant_digest=?`, digest).Scan(
		&grantSubject, &scopeKind, &scopeID, &storedSnapshot, &storedRev, &expiresAt, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryOpsCounts{}, "", m8core.ErrPurgeGrantInvalid
	}
	if err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	if grantSubject != subjectID || consumed.Valid || storedSnapshot != snapshotDigest || storedRev != expectedRevision {
		return m8core.MemoryOpsCounts{}, "", m8core.ErrPurgeGrantInvalid
	}
	exp, parseErr := time.Parse(time.RFC3339Nano, expiresAt)
	if parseErr != nil {
		exp, _ = time.Parse(time.RFC3339, expiresAt)
	}
	if time.Now().UTC().After(exp) {
		return m8core.MemoryOpsCounts{}, "", m8core.ErrPurgeGrantInvalid
	}
	resolvedScope := subjectID
	if scopeKind == "project" {
		if !scopeID.Valid {
			return m8core.MemoryOpsCounts{}, "", m8core.ErrPurgeGrantInvalid
		}
		resolvedScope = scopeID.String
	}
	counts, liveSnapshot, err := memoryPurgeSnapshot(ctx, tx, subjectID, scopeKind, resolvedScope)
	if err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	if liveSnapshot != storedSnapshot {
		return m8core.MemoryOpsCounts{}, "", m8core.ErrPurgeGrantInvalid
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := tx.QueryContext(ctx, `SELECT fact_id FROM memory_fact_heads WHERE subject_id=? AND scope_kind=? AND scope_id=? AND is_forgotten=0`,
		subjectID, scopeKind, resolvedScope)
	if err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	var factIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return m8core.MemoryOpsCounts{}, "", err
		}
		factIDs = append(factIDs, id)
	}
	rows.Close()
	for _, factID := range factIDs {
		if _, err = tx.ExecContext(ctx, `UPDATE memory_fact_heads SET is_forgotten=1, revision=revision+1, updated_at=? WHERE fact_id=?`, now, factID); err != nil {
			return m8core.MemoryOpsCounts{}, "", err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_content_bodies WHERE fact_id=?`, factID); err != nil {
			return m8core.MemoryOpsCounts{}, "", err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_search_documents WHERE fact_id=?`, factID); err != nil {
			return m8core.MemoryOpsCounts{}, "", err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE fact_id=?`, factID); err != nil {
			return m8core.MemoryOpsCounts{}, "", err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_candidate_assessments SET decision='drop' WHERE decision='review' AND scope_kind=? AND scope_id=?`,
		scopeKind, resolvedScope); err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_purge_grants SET consumed_at=? WHERE grant_digest=? AND consumed_at IS NULL`, now, digest); err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	out := m8core.MemoryOpsCounts{FactsTombstoned: counts.Facts, Candidates: counts.Candidates}
	payload, _ := json.Marshal(purgeConsumePayload{
		FactsTombstoned: out.FactsTombstoned, Candidates: out.Candidates,
		GrowthRows: out.GrowthRows, Flags: out.Flags, Traces: out.Traces, Memories: out.Memories,
		ScopeKind: scopeKind,
	})
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?,?,?,'purge',?,'consumed',?,?,?,?)`,
		nextSeq, ulid.Make().String(), subjectID, scopeKind, resolvedScope, operationID, string(payload), idempotencyKey, now, now); err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryOpsCounts{}, "", err
	}
	return out, scopeKind, nil
}

func memoryPurgeSnapshot(ctx context.Context, tx *sql.Tx, subjectID, scopeKind, scopeID string) (m8core.MemoryPurgeCounts, string, error) {
	var counts m8core.MemoryPurgeCounts
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_fact_heads WHERE subject_id=? AND scope_kind=? AND scope_id=? AND is_forgotten=0`,
		subjectID, scopeKind, scopeID).Scan(&counts.Facts); err != nil {
		return counts, "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_candidate_assessments WHERE decision='review' AND scope_kind=? AND scope_id=?`,
		scopeKind, scopeID).Scan(&counts.Candidates); err != nil {
		return counts, "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_search_documents WHERE subject_id=? AND scope_kind=? AND scope_id=?`,
		subjectID, scopeKind, scopeID).Scan(&counts.SearchDocuments); err != nil {
		return counts, "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings e
		JOIN memory_fact_heads h ON h.fact_id=e.fact_id
		WHERE h.subject_id=? AND h.scope_kind=? AND h.scope_id=?`, subjectID, scopeKind, scopeID).Scan(&counts.Embeddings); err != nil {
		return counts, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT fact_id FROM memory_fact_heads WHERE subject_id=? AND scope_kind=? AND scope_id=? AND is_forgotten=0 ORDER BY fact_id`,
		subjectID, scopeKind, scopeID)
	if err != nil {
		return counts, "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return counts, "", err
		}
		b.WriteString(id)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String() + strconv.FormatInt(counts.Facts, 10) + ":" + strconv.FormatInt(counts.Candidates, 10)))
	return counts, hex.EncodeToString(sum[:]), rows.Err()
}
