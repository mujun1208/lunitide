package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

const memoryFabricSchema = "lunitide.memory.fabric_v2"

type fabricArchive struct {
	Schema  string          `json:"schema"`
	Records []fabricRecord  `json:"records"`
	Manifest json.RawMessage `json:"manifest"`
}

type fabricRecord struct {
	Kind      string `json:"kind"`
	FactID    string `json:"factId"`
	Text      string `json:"text"`
	ScopeKind string `json:"scopeKind"`
	Forgotten bool   `json:"forgotten"`
}

func (s *Store) SealMemoryArchive(ctx context.Context, subjectID, scopeKind, scopeID string, raw []byte) (string, string, error) {
	return sealMemoryArchive(ctx, s.db, subjectID, scopeKind, scopeID, raw)
}

func (r *AgentRuntimeRepository) SealMemoryArchive(ctx context.Context, subjectID, scopeKind, scopeID string, raw []byte) (string, string, error) {
	return sealMemoryArchive(ctx, r.db, subjectID, scopeKind, scopeID, raw)
}

func sealMemoryArchive(ctx context.Context, db *sql.DB, subjectID, scopeKind, scopeID string, raw []byte) (string, string, error) {
	if subjectID == "" || len(raw) == 0 {
		return "", "", fmt.Errorf("archive missing bytes")
	}
	if int64(len(raw)) > m8core.MemoryArchiveMaxBytes {
		return "", "", m8core.ErrImportTooLarge
	}
	if scopeKind == "" {
		scopeKind = "user"
	}
	if scopeKind == "user" {
		scopeID = subjectID
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	id := ulid.Make().String()
	dir, err := sqliteDir(db)
	if err != nil {
		return "", "", err
	}
	cas := filepath.Join(dir, "memory-cas")
	if err = os.MkdirAll(cas, 0o700); err != nil {
		return "", "", err
	}
	ref := filepath.Join(cas, id+".json")
	if err = os.WriteFile(ref, raw, 0o600); err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	if _, err = db.ExecContext(ctx, `INSERT INTO memory_archive_artifacts(
		artifact_id,subject_id,scope_kind,scope_id,content_ref,sha256,size,state,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,'sealed',?,?)`,
		id, subjectID, scopeKind, scopeID, ref, digest, int64(len(raw)), expires, now.Format(time.RFC3339Nano)); err != nil {
		_ = os.Remove(ref)
		return "", "", err
	}
	return id, digest, nil
}

func (s *Store) PreviewMemoryImport(ctx context.Context, subjectID, sourceArtifactID, operationID, idempotencyKey string) (m8core.MemoryImportPreview, error) {
	return previewMemoryImport(ctx, s.db, subjectID, sourceArtifactID, operationID, idempotencyKey)
}

func (r *AgentRuntimeRepository) PreviewMemoryImport(ctx context.Context, subjectID, sourceArtifactID, operationID, idempotencyKey string) (m8core.MemoryImportPreview, error) {
	return previewMemoryImport(ctx, r.db, subjectID, sourceArtifactID, operationID, idempotencyKey)
}

func previewMemoryImport(ctx context.Context, db *sql.DB, subjectID, sourceArtifactID, operationID, idempotencyKey string) (m8core.MemoryImportPreview, error) {
	if subjectID == "" || sourceArtifactID == "" || operationID == "" || idempotencyKey == "" {
		return m8core.MemoryImportPreview{}, fmt.Errorf("import preview missing required fields")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.MemoryImportPreview
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.PreviewID != "" {
			if replay.SourceArtifactID != sourceArtifactID || replay.OperationID != operationID {
				return m8core.MemoryImportPreview{}, m8core.ErrOperationReplayMismatch
			}
			replay.Replay = true
			return replay, tx.Commit()
		}
		return m8core.MemoryImportPreview{}, m8core.ErrOperationReplayMismatch
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryImportPreview{}, err
	}
	raw, artifactSubject, archiveDigest, size, err := readSealedArchive(ctx, tx, sourceArtifactID)
	if err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	if artifactSubject != subjectID {
		return m8core.MemoryImportPreview{}, m8core.ErrImportScopeDenied
	}
	if size > m8core.MemoryArchiveMaxBytes || int64(len(raw)) > m8core.MemoryArchiveMaxBytes {
		return m8core.MemoryImportPreview{}, m8core.ErrImportTooLarge
	}
	if jsonDepthExceeds(raw, m8core.MemoryArchiveMaxJSONDepth) {
		return m8core.MemoryImportPreview{}, m8core.ErrImportSchemaUnsupported
	}
	archive, err := parseFabricArchive(raw)
	if err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	if int64(len(archive.Records)) > m8core.MemoryArchiveMaxRecords {
		return m8core.MemoryImportPreview{}, m8core.ErrImportTooLarge
	}
	counts, warnings := previewImportCounts(ctx, tx, subjectID, archive.Records)
	manifestRaw := archive.Manifest
	if len(manifestRaw) == 0 {
		manifestRaw = []byte(`{}`)
	}
	manSum := sha256.Sum256(manifestRaw)
	manifestDigest := hex.EncodeToString(manSum[:])
	rev, err := memoryDatabaseRevision(ctx, tx, subjectID)
	if err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	previewID := ulid.Make().String()
	summary, _ := json.Marshal(map[string]any{"counts": counts, "warnings": warnings, "operationId": operationID})
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_import_previews(
		preview_id,subject_id,source_artifact_id,archive_digest,manifest_digest,database_revision,summary_json,state,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,'ready',?,?)`,
		previewID, subjectID, sourceArtifactID, archiveDigest, manifestDigest, rev, string(summary),
		expires.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	leaseID := ulid.Make().String()
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_archive_leases(lease_id,artifact_id,owner,expires_at) VALUES(?,?,?,?)`,
		leaseID, sourceArtifactID, "import:"+previewID, now.Add(60*time.Second).Format(time.RFC3339Nano)); err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	out := m8core.MemoryImportPreview{
		PreviewID:        previewID,
		SourceArtifactID: sourceArtifactID,
		ArchiveDigest:    archiveDigest,
		ManifestDigest:   manifestDigest,
		DatabaseRevision: rev,
		ExpiresAt:        expires.UTC().Format(time.RFC3339),
		Counts:           counts,
		Warnings:         warnings,
		OperationID:      operationID,
	}
	payload, _ := json.Marshal(out)
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?, 'user', ?, 'import_preview', ?, 'previewed', ?, ?, ?, ?)`,
		nextSeq, ulid.Make().String(), subjectID, subjectID, previewID, string(payload), idempotencyKey, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	rev, err = memoryDatabaseRevision(ctx, tx, subjectID)
	if err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	out.DatabaseRevision = atLeastOneRevision(rev)
	payload, err = json.Marshal(out)
	if err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_import_previews SET database_revision=? WHERE preview_id=?`, out.DatabaseRevision, previewID); err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_event_log SET payload_json=? WHERE idempotency_key=?`, string(payload), idempotencyKey); err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryImportPreview{}, err
	}
	return out, nil
}

func (s *Store) CommitMemoryImport(ctx context.Context, subjectID, previewID, archiveDigest, manifestDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryImportCommit, error) {
	return commitMemoryImport(ctx, s.db, subjectID, previewID, archiveDigest, manifestDigest, operationID, idempotencyKey, expectedRevision)
}

func (r *AgentRuntimeRepository) CommitMemoryImport(ctx context.Context, subjectID, previewID, archiveDigest, manifestDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryImportCommit, error) {
	return commitMemoryImport(ctx, r.db, subjectID, previewID, archiveDigest, manifestDigest, operationID, idempotencyKey, expectedRevision)
}

func commitMemoryImport(ctx context.Context, db *sql.DB, subjectID, previewID, archiveDigest, manifestDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryImportCommit, error) {
	if subjectID == "" || previewID == "" || archiveDigest == "" || manifestDigest == "" || operationID == "" || idempotencyKey == "" || expectedRevision < 1 {
		return m8core.MemoryImportCommit{}, fmt.Errorf("import commit missing required fields")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM memory_event_log WHERE idempotency_key=?`, idempotencyKey).Scan(&existing)
	if err == nil {
		var replay m8core.MemoryImportCommit
		if json.Unmarshal([]byte(existing), &replay) == nil && replay.PreviewID != "" {
			if replay.PreviewID != previewID || replay.OperationID != operationID {
				return m8core.MemoryImportCommit{}, m8core.ErrOperationReplayMismatch
			}
			replay.Replay = true
			return replay, tx.Commit()
		}
		return m8core.MemoryImportCommit{}, m8core.ErrOperationReplayMismatch
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryImportCommit{}, err
	}
	var row struct {
		subject, artifact, archive, manifest, state, expires string
		revision                                               int64
	}
	err = tx.QueryRowContext(ctx, `SELECT subject_id,source_artifact_id,archive_digest,manifest_digest,database_revision,state,expires_at
		FROM memory_import_previews WHERE preview_id=?`, previewID).
		Scan(&row.subject, &row.artifact, &row.archive, &row.manifest, &row.revision, &row.state, &row.expires)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryImportCommit{}, m8core.ErrImportSourceMissing
	}
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	if row.subject != subjectID {
		return m8core.MemoryImportCommit{}, m8core.ErrImportScopeDenied
	}
	if row.state != "ready" {
		return m8core.MemoryImportCommit{}, m8core.ErrImportPreviewExpired
	}
	exp, _ := time.Parse(time.RFC3339Nano, row.expires)
	if exp.Before(time.Now().UTC()) {
		_, _ = tx.ExecContext(ctx, `UPDATE memory_import_previews SET state='expired' WHERE preview_id=?`, previewID)
		return m8core.MemoryImportCommit{}, m8core.ErrImportPreviewExpired
	}
	if row.archive != archiveDigest || row.manifest != manifestDigest {
		return m8core.MemoryImportCommit{}, m8core.ErrImportDigestMismatch
	}
	current, err := memoryDatabaseRevision(ctx, tx, subjectID)
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	if atLeastOneRevision(current) != expectedRevision || atLeastOneRevision(row.revision) != expectedRevision {
		return m8core.MemoryImportCommit{}, m8core.ErrRevisionConflict
	}
	raw, artifactSubject, liveDigest, _, err := readSealedArchive(ctx, tx, row.artifact)
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	if artifactSubject != subjectID {
		return m8core.MemoryImportCommit{}, m8core.ErrImportScopeDenied
	}
	if liveDigest != archiveDigest {
		return m8core.MemoryImportCommit{}, m8core.ErrImportDigestMismatch
	}
	if jsonDepthExceeds(raw, m8core.MemoryArchiveMaxJSONDepth) {
		return m8core.MemoryImportCommit{}, m8core.ErrImportSchemaUnsupported
	}
	archive, err := parseFabricArchive(raw)
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	imported, review, skipped, conflict, err := applyFabricRecords(ctx, db, subjectID, archive.Records, operationID)
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	finish, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	defer func() { _ = finish.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = finish.ExecContext(ctx, `UPDATE memory_import_previews SET state='committed' WHERE preview_id=? AND state='ready'`, previewID); err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	if _, err = finish.ExecContext(ctx, `DELETE FROM memory_archive_leases WHERE artifact_id=?`, row.artifact); err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	rev, err := memoryDatabaseRevision(ctx, finish, subjectID)
	if err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	out := m8core.MemoryImportCommit{
		PreviewID:        previewID,
		State:            "committed",
		ImportedCount:    imported,
		ReviewCount:      review,
		SkippedCount:     skipped,
		ConflictCount:    conflict,
		DatabaseRevision: rev,
		OperationID:      operationID,
	}
	payload, _ := json.Marshal(out)
	var nextSeq int64
	if err = finish.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq),0)+1 FROM memory_event_log`).Scan(&nextSeq); err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	if _, err = finish.ExecContext(ctx, `INSERT INTO memory_event_log(
		event_seq,event_id,subject_id,scope_kind,scope_id,entity_type,entity_id,event_type,payload_json,idempotency_key,occurred_at,recorded_at)
		VALUES(?,?,?, 'user', ?, 'import_preview', ?, 'committed', ?, ?, ?, ?)`,
		nextSeq, ulid.Make().String(), subjectID, subjectID, previewID, string(payload), idempotencyKey, now, now); err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	if err = finish.Commit(); err != nil {
		return m8core.MemoryImportCommit{}, err
	}
	return out, nil
}

func readSealedArchive(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, artifactID string) ([]byte, string, string, int64, error) {
	var subject, ref, digest, state string
	var size int64
	err := q.QueryRowContext(ctx, `SELECT subject_id,content_ref,sha256,size,state FROM memory_archive_artifacts WHERE artifact_id=?`, artifactID).
		Scan(&subject, &ref, &digest, &size, &state)
	if errors.Is(err, sql.ErrNoRows) || state != "sealed" {
		return nil, "", "", 0, m8core.ErrImportSourceMissing
	}
	if err != nil {
		return nil, "", "", 0, err
	}
	raw, err := os.ReadFile(ref)
	if err != nil {
		return nil, "", "", 0, m8core.ErrImportSourceMissing
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, "", "", 0, m8core.ErrImportDigestMismatch
	}
	return raw, subject, digest, size, nil
}

func parseFabricArchive(raw []byte) (fabricArchive, error) {
	var archive fabricArchive
	if err := json.Unmarshal(raw, &archive); err != nil {
		return fabricArchive{}, m8core.ErrImportSchemaUnsupported
	}
	if archive.Schema != memoryFabricSchema {
		return fabricArchive{}, m8core.ErrImportSchemaUnsupported
	}
	return archive, nil
}

func previewImportCounts(ctx context.Context, tx *sql.Tx, subjectID string, records []fabricRecord) (m8core.MemoryImportCounts, []string) {
	counts := m8core.MemoryImportCounts{Total: len(records)}
	var warnings []string
	for _, rec := range records {
		text := strings.TrimSpace(rec.Text)
		if rec.Forgotten || rec.Kind == "tombstone" {
			counts.Tombstones++
			continue
		}
		if text == "" {
			counts.Review++
			continue
		}
		forgotten, active, err := lookupImportHead(ctx, tx, subjectID, rec.FactID, text)
		if err != nil {
			counts.Review++
			continue
		}
		if forgotten {
			counts.Tombstones++
			warnings = appendWarning(warnings, "local tombstone takes precedence")
			continue
		}
		if active {
			counts.Conflicts++
			counts.Review++
			continue
		}
		counts.Accepted++
	}
	if len(warnings) > 100 {
		warnings = warnings[:100]
	}
	return counts, warnings
}

func applyFabricRecords(ctx context.Context, db *sql.DB, subjectID string, records []fabricRecord, operationID string) (imported, review, skipped, conflict int, err error) {
	for i, rec := range records {
		text := strings.TrimSpace(rec.Text)
		if rec.Forgotten || rec.Kind == "tombstone" {
			skipped++
			continue
		}
		if text == "" {
			review++
			continue
		}
		forgotten, active, lookErr := lookupImportHead(ctx, db, subjectID, rec.FactID, text)
		if lookErr != nil {
			return imported, review, skipped, conflict, lookErr
		}
		if forgotten {
			skipped++
			continue
		}
		if active {
			conflict++
			review++
			_ = putMemoryReview(ctx, db, subjectID, m8core.MemoryReviewWrite{
				CandidateID: ulid.Make().String(),
				Kind:        m8core.ClassifyMemoryKind(text),
				ScopeKind:   "user",
				Novelty:     "conflict",
				ReasonCodes: []string{"import_conflict"},
				Text:        text,
			})
			continue
		}
		op := operationID
		if i > 0 {
			op = ulid.Make().String()
		}
		if _, err = insertCanonicalMemoryItem(ctx, db, m8core.CanonicalMemoryWrite{
			SubjectID:      subjectID,
			ScopeKind:      "user",
			ScopeID:        subjectID,
			Kind:           m8core.ClassifyMemoryKind(text),
			Text:           text,
			OperationID:    op,
			IdempotencyKey: "memory.import.commit:" + operationID + ":" + fmt.Sprintf("%d", i),
		}); err != nil {
			return imported, review, skipped, conflict, err
		}
		imported++
	}
	return imported, review, skipped, conflict, nil
}

func lookupImportHead(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, subjectID, factID, text string) (forgotten, active bool, err error) {
	sum := sha256.Sum256([]byte(text))
	digest := hex.EncodeToString(sum[:])
	var isForgotten int
	err = q.QueryRowContext(ctx, `SELECT h.is_forgotten FROM memory_fact_heads h
		JOIN memory_content_versions v ON v.fact_id=h.fact_id AND v.fact_version=h.current_version
		LEFT JOIN memory_content_bodies b ON b.fact_id=h.fact_id AND b.fact_version=h.current_version
		WHERE h.subject_id=? AND h.scope_kind='user'
		  AND (v.content_digest=? OR IFNULL(b.canonical_text,'')=? OR h.fact_id=?)
		ORDER BY h.is_forgotten DESC LIMIT 1`, subjectID, digest, text, factID).Scan(&isForgotten)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return isForgotten == 1, isForgotten == 0, nil
}

func jsonDepthExceeds(raw []byte, max int) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			return true
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			continue
		}
		switch delim {
		case '{', '[':
			depth++
			if depth > max {
				return true
			}
		case '}', ']':
			depth--
		}
	}
}

func sqliteDir(db *sql.DB) (string, error) {
	var seq int
	var name, file string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file); err != nil {
		return "", err
	}
	if file == "" {
		return "", fmt.Errorf("sqlite has no file path")
	}
	return filepath.Dir(file), nil
}

func appendWarning(dst []string, msg string) []string {
	if len(msg) > 256 {
		msg = msg[:256]
	}
	if len(dst) >= 100 {
		return dst
	}
	return append(dst, msg)
}

func atLeastOneRevision(n int64) int64 {
	if n < 1 {
		return 1
	}
	return n
}

func (s *Store) ExportCanonicalMemoryArchive(ctx context.Context, subjectID string) (m8core.MemoryFabricExport, error) {
	return exportCanonicalMemoryArchive(ctx, s.db, subjectID)
}

func (r *AgentRuntimeRepository) ExportCanonicalMemoryArchive(ctx context.Context, subjectID string) (m8core.MemoryFabricExport, error) {
	return exportCanonicalMemoryArchive(ctx, r.db, subjectID)
}

func exportCanonicalMemoryArchive(ctx context.Context, db *sql.DB, subjectID string) (m8core.MemoryFabricExport, error) {
	if subjectID == "" {
		return m8core.MemoryFabricExport{}, fmt.Errorf("export missing subject")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryFabricExport{}, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT h.fact_id, v.kind, h.scope_kind, h.is_forgotten, IFNULL(b.canonical_text,'')
		FROM memory_fact_heads h
		JOIN memory_content_versions v ON v.fact_id=h.fact_id AND v.fact_version=h.current_version
		LEFT JOIN memory_content_bodies b ON b.fact_id=h.fact_id AND b.fact_version=h.current_version
		WHERE h.subject_id=?
		ORDER BY h.fact_id
		LIMIT ?`, subjectID, m8core.MemoryArchiveMaxRecords+1)
	if err != nil {
		return m8core.MemoryFabricExport{}, err
	}
	var records []fabricRecord
	current, tombs := 0, 0
	for rows.Next() {
		var rec fabricRecord
		var forgotten int
		if err = rows.Scan(&rec.FactID, &rec.Kind, &rec.ScopeKind, &forgotten, &rec.Text); err != nil {
			rows.Close()
			return m8core.MemoryFabricExport{}, err
		}
		if forgotten == 1 {
			rec.Forgotten = true
			rec.Text = ""
			tombs++
		} else {
			current++
		}
		records = append(records, rec)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return m8core.MemoryFabricExport{}, err
	}
	rows.Close()
	if int64(len(records)) > m8core.MemoryArchiveMaxRecords {
		return m8core.MemoryFabricExport{}, m8core.ErrImportTooLarge
	}
	rev, err := memoryDatabaseRevision(ctx, tx, subjectID)
	if err != nil {
		return m8core.MemoryFabricExport{}, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryFabricExport{}, err
	}
	manifest, err := json.Marshal(map[string]any{
		"schema":            memoryFabricSchema,
		"databaseRevision":  atLeastOneRevision(rev),
		"recordCount":       len(records),
		"currentCount":      current,
		"tombstoneCount":    tombs,
	})
	if err != nil {
		return m8core.MemoryFabricExport{}, err
	}
	raw, err := json.Marshal(fabricArchive{Schema: memoryFabricSchema, Records: records, Manifest: manifest})
	if err != nil {
		return m8core.MemoryFabricExport{}, err
	}
	if int64(len(raw)) > m8core.MemoryArchiveMaxBytes {
		return m8core.MemoryFabricExport{}, m8core.ErrImportTooLarge
	}
	artifactID, archiveDigest, err := sealMemoryArchive(ctx, db, subjectID, "user", "", raw)
	if err != nil {
		return m8core.MemoryFabricExport{}, err
	}
	manSum := sha256.Sum256(manifest)
	out := m8core.MemoryFabricExport{
		Format:           "fabric_v2",
		ArtifactID:       artifactID,
		ArchiveDigest:    archiveDigest,
		ManifestDigest:   hex.EncodeToString(manSum[:]),
		DatabaseRevision: atLeastOneRevision(rev),
		Counts: m8core.MemoryFabricExportCounts{
			Records:    len(records),
			Current:    current,
			Tombstones: tombs,
		},
	}
	return out, nil
}
