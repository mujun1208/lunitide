package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	ErrNoVerifiedRuntimeProfile = errors.New("NO_VERIFIED_RUNTIME_PROFILE")
	ErrOCRActiveOperation       = errors.New("OCR_PACK_BUSY")
	ErrOCRNoticeUnavailable     = errors.New("OCR_PACK_NOTICE_UNAVAILABLE")
	ErrOCRRunNotFound           = errors.New("OCR_RUN_NOT_FOUND")
	ErrOCRScopeDenied           = errors.New("OCR_SCOPE_DENIED")
	ErrOCRIdempotencyConflict   = errors.New("OCR_IDEMPOTENCY_CONFLICT")
)

type OCRPackGateRow struct {
	PackID                       string
	InstallEnabled               bool
	AutoRouteEnabled             bool
	VerifiedRuntimeProfileDigest sql.NullString
	DisabledReason               string
	Revision                     int64
	UpdatedAt                    string
}

type OCRPackOperationRow struct {
	OperationID string
	PackID      string
	Phase       string
	ErrorCode   string
	Revision    int64
	CreatedAt   string
	UpdatedAt   string
}

type OCRNoticeRow struct {
	PackID         string
	ManifestDigest string
	Version        string
	NoticeDigest   string
	NoticeBytes    []byte
	VerifiedAt     string
	InstalledAt    sql.NullString
	UninstalledAt  sql.NullString
}

type OCRPackStateRow struct {
	PackID          string
	Availability    string
	CurrentVersion  sql.NullString
	PreviousVersion sql.NullString
	LastHealthAt    sql.NullString
	LastErrorCode   string
	Revision        int64
	UpdatedAt       string
}

func (s *Store) OCRPackState(ctx context.Context, packID string) (OCRPackStateRow, error) {
	var row OCRPackStateRow
	err := s.db.QueryRowContext(ctx, `SELECT pack_id,availability,current_version,previous_version,last_health_at,last_error_code,revision,updated_at
		FROM ocr_pack_state WHERE pack_id=?`, packID).Scan(
		&row.PackID, &row.Availability, &row.CurrentVersion, &row.PreviousVersion, &row.LastHealthAt,
		&row.LastErrorCode, &row.Revision, &row.UpdatedAt)
	return row, err
}

func (s *Store) OCRPackGate(ctx context.Context, packID string) (OCRPackGateRow, error) {
	var row OCRPackGateRow
	err := s.db.QueryRowContext(ctx, `SELECT pack_id,install_enabled,auto_route_enabled,verified_runtime_profile_digest,disabled_reason,revision,updated_at
		FROM ocr_pack_gates WHERE pack_id=?`, packID).Scan(
		&row.PackID, sqliteBool{&row.InstallEnabled}, sqliteBool{&row.AutoRouteEnabled},
		&row.VerifiedRuntimeProfileDigest, &row.DisabledReason, &row.Revision, &row.UpdatedAt)
	return row, err
}

type sqliteBool struct{ dest *bool }

func (b sqliteBool) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		*b.dest = v != 0
	case nil:
		*b.dest = false
	default:
		return errors.New("sqliteBool")
	}
	return nil
}

func (s *Store) OCRInsertPackOperation(ctx context.Context, packID, subjectID, action, idempotencyKey, requestDigest string) (OCRPackOperationRow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OCRPackOperationRow{}, err
	}
	defer tx.Rollback()
	var digest sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT verified_runtime_profile_digest FROM ocr_pack_gates WHERE pack_id=?`, packID).Scan(&digest); err != nil {
		return OCRPackOperationRow{}, err
	}
	if !digest.Valid || digest.String == "" {
		return OCRPackOperationRow{}, ErrNoVerifiedRuntimeProfile
	}
	var existing OCRPackOperationRow
	err = tx.QueryRowContext(ctx, `SELECT operation_id,pack_id,phase,revision FROM ocr_pack_operations WHERE pack_id=? AND idempotency_key=?`, packID, idempotencyKey).
		Scan(&existing.OperationID, &existing.PackID, &existing.Phase, &existing.Revision)
	if err == nil {
		var storedDigest string
		if err = tx.QueryRowContext(ctx, `SELECT request_digest FROM ocr_pack_operations WHERE operation_id=?`, existing.OperationID).Scan(&storedDigest); err != nil {
			return OCRPackOperationRow{}, err
		}
		if storedDigest != requestDigest {
			return OCRPackOperationRow{}, ErrOCRIdempotencyConflict
		}
		if err = tx.Commit(); err != nil {
			return OCRPackOperationRow{}, err
		}
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return OCRPackOperationRow{}, err
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ocr_pack_operations WHERE pack_id=? AND phase NOT IN ('succeeded','failed','cancelled')`, packID).Scan(&active); err != nil {
		return OCRPackOperationRow{}, err
	}
	if active > 0 {
		return OCRPackOperationRow{}, ErrOCRActiveOperation
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := ulid.Make().String()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ocr_pack_operations(
		operation_id,pack_id,subject_id,action,idempotency_key,request_digest,catalog_revision,phase,progress,cancel_requested,result_json,error_code,attempt,fence,revision,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'', 'requested',0,0,'{}','',0,0,1,?,?)`,
		id, packID, subjectID, action, idempotencyKey, requestDigest, now, now); err != nil {
		return OCRPackOperationRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return OCRPackOperationRow{}, err
	}
	return OCRPackOperationRow{OperationID: id, PackID: packID, Phase: "requested", Revision: 1}, nil
}

func (s *Store) OCRRetainNotice(ctx context.Context, packID, version, manifestDigest, noticeText, noticeRef string) error {
	digest := sha256HexBytes([]byte(noticeText))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO ocr_pack_notices(pack_id,manifest_digest,version,notice_digest,notice_bytes,notice_ref,verified_at)
		VALUES(?,?,?,?,?,?,?)`, packID, manifestDigest, version, digest, []byte(noticeText), noticeRef, now)
	return err
}

func (s *Store) OCRNoticeList(ctx context.Context, packID string) ([]OCRNoticeRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pack_id,manifest_digest,version,notice_digest,notice_bytes,verified_at,installed_at,uninstalled_at
		FROM ocr_pack_notices WHERE pack_id=? ORDER BY verified_at DESC, manifest_digest ASC`, packID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OCRNoticeRow
	for rows.Next() {
		var row OCRNoticeRow
		if err := rows.Scan(&row.PackID, &row.ManifestDigest, &row.Version, &row.NoticeDigest, &row.NoticeBytes, &row.VerifiedAt, &row.InstalledAt, &row.UninstalledAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) OCRNoticeRead(ctx context.Context, packID, manifestDigest string) (OCRNoticeRow, error) {
	var row OCRNoticeRow
	err := s.db.QueryRowContext(ctx, `SELECT pack_id,manifest_digest,version,notice_digest,notice_bytes,verified_at,installed_at,uninstalled_at
		FROM ocr_pack_notices WHERE pack_id=? AND manifest_digest=?`, packID, manifestDigest).
		Scan(&row.PackID, &row.ManifestDigest, &row.Version, &row.NoticeDigest, &row.NoticeBytes, &row.VerifiedAt, &row.InstalledAt, &row.UninstalledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OCRNoticeRow{}, ErrOCRNoticeUnavailable
	}
	if err != nil {
		return OCRNoticeRow{}, err
	}
	if sha256HexBytes(row.NoticeBytes) != row.NoticeDigest {
		return OCRNoticeRow{}, ErrOCRNoticeUnavailable
	}
	return row, nil
}

func (s *Store) OCRMarkNoticeUninstalled(ctx context.Context, packID, manifestDigest string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE ocr_pack_notices SET uninstalled_at=? WHERE pack_id=? AND manifest_digest=?`, now, packID, manifestDigest)
	return err
}

func (s *Store) OCRImportPPOCR(ctx context.Context, ownerSubjectID, scopeKind, scopeID, rootRef string, marker bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := ulid.Make().String()
	detected := 0
	if marker {
		detected = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ocr_legacy_registrations(
		registration_id,owner_subject_id,scope_kind,scope_id,engine_id,root_ref,marker_detected,state,available,imported_at,revision)
		VALUES(?,?,?,?, 'ppocr', ?,?,'registered_unwired',0,?,1)`,
		id, ownerSubjectID, scopeKind, scopeID, rootRef, detected, now)
	return err
}

func (s *Store) OCRGetPPOCR(ctx context.Context, ownerSubjectID, scopeKind, scopeID string) (state string, available int, marker int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT state,available,marker_detected FROM ocr_legacy_registrations
		WHERE owner_subject_id=? AND scope_kind=? AND scope_id=? AND engine_id='ppocr'`,
		ownerSubjectID, scopeKind, scopeID).Scan(&state, &available, &marker)
	return
}

func (s *Store) OCRInsertRun(ctx context.Context, ownerSubjectID, scopeKind, scopeID, requestID, documentDigest, snapshotDigest string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	runID := ulid.Make().String()
	_, err := s.db.ExecContext(ctx, `INSERT INTO ocr_document_runs(
		run_id,request_id,owner_subject_id,scope_kind,scope_id,document_digest,policy_json,gate_json,mode,
		allow_remote,credential_ref,request_snapshot_digest,state,page_count,actual_engine,created_at,updated_at)
		VALUES(?,?,?,?,?,?, '{}','{}','auto', 0,'', ?, 'queued',0,'',?,?)`,
		runID, requestID, ownerSubjectID, scopeKind, scopeID, documentDigest, snapshotDigest, now, now)
	return runID, err
}

func (s *Store) OCRGetRun(ctx context.Context, ownerSubjectID, scopeKind, scopeID, runID string) (string, error) {
	row, err := s.OCRGetRunRecord(ctx, ownerSubjectID, runID)
	if err != nil {
		return "", err
	}
	if row.ScopeKind != scopeKind || row.ScopeID != scopeID {
		return "", ErrOCRScopeDenied
	}
	return row.RunID, nil
}

type OCRRunRecord struct {
	RunID        string
	Owner        string
	ScopeKind    string
	ScopeID      string
	State        string
	ActualEngine string
	PageCount    int
	CreatedAt    string
	UpdatedAt    string
}

type OCRRunListItem struct {
	RunID        string
	CreatedAt    string
	State        string
	ActualEngine string
	PageCount    int
	ArtifactID   string
	PreviewBytes int64
}

type OCRPageRecord struct {
	Page      int
	Complete  bool
	Uncertain bool
}

type OCRArtifactRecord struct {
	ArtifactID string
	Owner      string
	RunID      string
	CASRef     string
	SHA256     string
	Size       int64
	MIME       string
	ExpiresAt  string
}

func (s *Store) OCRGetRunRecord(ctx context.Context, ownerSubjectID, runID string) (OCRRunRecord, error) {
	var row OCRRunRecord
	err := s.db.QueryRowContext(ctx, `SELECT run_id,owner_subject_id,scope_kind,scope_id,state,actual_engine,page_count,created_at,updated_at
		FROM ocr_document_runs WHERE run_id=?`, runID).
		Scan(&row.RunID, &row.Owner, &row.ScopeKind, &row.ScopeID, &row.State, &row.ActualEngine, &row.PageCount, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OCRRunRecord{}, ErrOCRRunNotFound
	}
	if err != nil {
		return OCRRunRecord{}, err
	}
	if row.Owner != ownerSubjectID {
		return OCRRunRecord{}, ErrOCRScopeDenied
	}
	return row, nil
}

func (s *Store) OCRListRuns(ctx context.Context, ownerSubjectID, scopeKind, scopeID string, limit int) ([]OCRRunListItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.run_id,r.created_at,r.state,r.actual_engine,r.page_count,IFNULL(a.artifact_id,''),IFNULL(a.size,0)
		FROM ocr_document_runs r
		LEFT JOIN ocr_artifacts a ON a.run_id=r.run_id
		WHERE r.owner_subject_id=? AND r.scope_kind=? AND r.scope_id=?
		ORDER BY r.created_at DESC, r.run_id DESC LIMIT ?`, ownerSubjectID, scopeKind, scopeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OCRRunListItem
	for rows.Next() {
		var item OCRRunListItem
		if err := rows.Scan(&item.RunID, &item.CreatedAt, &item.State, &item.ActualEngine, &item.PageCount, &item.ArtifactID, &item.PreviewBytes); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) OCRListRunPages(ctx context.Context, runID string, limit int) ([]OCRPageRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT page,complete,uncertain FROM ocr_page_results WHERE run_id=? ORDER BY page ASC LIMIT ?`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OCRPageRecord
	for rows.Next() {
		var page OCRPageRecord
		var complete, uncertain int
		if err := rows.Scan(&page.Page, &complete, &uncertain); err != nil {
			return nil, err
		}
		page.Complete = complete != 0
		page.Uncertain = uncertain != 0
		out = append(out, page)
	}
	return out, rows.Err()
}

func (s *Store) OCRGetArtifact(ctx context.Context, ownerSubjectID, artifactID string) (OCRArtifactRecord, error) {
	var row OCRArtifactRecord
	err := s.db.QueryRowContext(ctx, `SELECT artifact_id,owner_subject_id,run_id,cas_ref,sha256,size,mime,expires_at
		FROM ocr_artifacts WHERE artifact_id=?`, artifactID).
		Scan(&row.ArtifactID, &row.Owner, &row.RunID, &row.CASRef, &row.SHA256, &row.Size, &row.MIME, &row.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OCRArtifactRecord{}, ErrOCRRunNotFound
	}
	if err != nil {
		return OCRArtifactRecord{}, err
	}
	if row.Owner != ownerSubjectID {
		return OCRArtifactRecord{}, ErrOCRScopeDenied
	}
	if row.ExpiresAt != "" {
		when, parseErr := time.Parse(time.RFC3339Nano, row.ExpiresAt)
		if parseErr != nil {
			when, parseErr = time.Parse(time.RFC3339, row.ExpiresAt)
		}
		if parseErr == nil && time.Now().UTC().After(when) {
			return OCRArtifactRecord{}, ErrOCRRunNotFound
		}
	}
	return row, nil
}

func (s *Store) OCRReadArtifactBytes(rec OCRArtifactRecord) ([]byte, error) {
	if rec.CASRef == "" || strings.Contains(rec.CASRef, "..") {
		return nil, ErrOCRRunNotFound
	}
	root := filepath.Join(filepath.Dir(s.path), "ocr-cas")
	path := filepath.Join(root, filepath.Base(rec.CASRef))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrOCRRunNotFound
	}
	return raw, nil
}

func (s *Store) OCRPersistRecognition(ctx context.Context, ownerSubjectID, scopeKind, scopeID string, document, text []byte, engine string, pages int, complete, uncertain bool) (string, string, error) {
	if s == nil {
		return "", "", errors.New("ocr store missing")
	}
	if scopeKind == "user" {
		scopeID = ownerSubjectID
	}
	engine = strings.TrimSpace(engine)
	if engine == "" {
		engine = "unknown"
	}
	if len(engine) > 128 {
		engine = engine[:128]
	}
	docDigest := sha256HexBytes(document)
	snapDigest := sha256HexBytes(append(append([]byte(engine), 0), text...))
	runID, err := s.OCRInsertRun(ctx, ownerSubjectID, scopeKind, scopeID, ulid.Make().String(), docDigest, snapDigest)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if pages < 1 {
		pages = 1
	}
	completeFlag, uncertainFlag := 0, 0
	if complete {
		completeFlag = 1
	}
	if uncertain {
		uncertainFlag = 1
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE ocr_document_runs SET state='succeeded', page_count=?, actual_engine=?, updated_at=? WHERE run_id=?`,
		pages, engine, now, runID); err != nil {
		return runID, "", err
	}
	for page := 1; page <= pages && page <= 20; page++ {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO ocr_page_results(
			run_id,page,request_snapshot_digest,actual_engine,source_digest,complete,uncertain,layout_json,warnings_json)
			VALUES(?,?,?,?,?,?,?,'{}','[]')`,
			runID, page, snapDigest, engine, docDigest, completeFlag, uncertainFlag); err != nil {
			return runID, "", err
		}
	}
	artifactID := ulid.Make().String()
	root := filepath.Join(filepath.Dir(s.path), "ocr-cas")
	if err := os.MkdirAll(root, 0700); err != nil {
		return runID, "", err
	}
	casName := artifactID + ".txt"
	if err := os.WriteFile(filepath.Join(root, casName), text, 0600); err != nil {
		return runID, "", err
	}
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO ocr_artifacts(
		artifact_id,owner_subject_id,scope_kind,scope_id,run_id,cas_ref,sha256,size,mime,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?, 'text/plain; charset=utf-8', ?,?)`,
		artifactID, ownerSubjectID, scopeKind, scopeID, runID, casName, sha256HexBytes(text), int64(len(text)), expires, now); err != nil {
		return runID, "", err
	}
	return runID, artifactID, nil
}

func requestDigestHex(s string) string {
	return sha256HexBytes([]byte(s))
}
