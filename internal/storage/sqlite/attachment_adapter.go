package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/attachment"
)

// CreateAttachment inserts a new attachment record.
func (s *Store) CreateAttachment(ctx context.Context, a attachment.Attachment) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.ParseStatus == "" {
		a.ParseStatus = attachment.StatusPending
	}
	if a.MIME == "" {
		a.MIME = "application/octet-stream"
	}
	var sessionID any
	if a.SessionID != "" {
		sessionID = a.SessionID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO attachments(id, project_id, session_id, file_ref, original_name,
		  mime, size, sha256, parse_status, parse_error_code, parsed_text,
		  parsed_text_bytes, created_at, deleted_at)
		 SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?
		 WHERE EXISTS (SELECT 1 FROM projects WHERE id=?)
		   AND (? IS NULL OR EXISTS (SELECT 1 FROM sessions WHERE id=? AND project_id=?))`,
		a.ID, a.ProjectID, sessionID, a.FileRef, a.OriginalName,
		a.MIME, a.Size, a.SHA256, string(a.ParseStatus), a.ParseErrorCode,
		a.ParsedText, a.ParsedTextBytes, formatTime(a.CreatedAt), nil,
		a.ProjectID, sessionID, sessionID, a.ProjectID)
	if err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return attachmentapp.ErrScopeMismatch
	}
	return nil
}

// GetAttachment returns an attachment by ID, or nil when not found.
func (s *Store) GetAttachment(ctx context.Context, id string) (*attachment.Attachment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, COALESCE(session_id,''), file_ref, original_name,
		  mime, size, sha256, parse_status, parse_error_code, parsed_text,
		  parsed_text_bytes, created_at, deleted_at
		 FROM attachments WHERE id=? AND deleted_at IS NULL`, id)
	a, err := scanAttachment(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return &a, nil
}

// GetAttachmentForDeletion includes deleted rows for idempotent retry while
// deliberately omitting parsed text from this administrative lookup.
func (s *Store) GetAttachmentForDeletion(ctx context.Context, id string) (*attachment.Attachment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, COALESCE(session_id,''), file_ref, original_name,
		  mime, size, sha256, parse_status, parse_error_code, '',
		  parsed_text_bytes, created_at, deleted_at FROM attachments WHERE id=?`, id)
	a, err := scanAttachment(row)
	if err == nil {
		return &a, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("get attachment for deletion: %w", err)
	}
	var deletedAt string
	err = s.db.QueryRowContext(ctx, `SELECT deleted_at FROM deletion_tombstones WHERE owner_type='attachment' AND owner_id=?`, id).Scan(&deletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment tombstone for deletion: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, deletedAt)
	if err != nil {
		return nil, fmt.Errorf("parse attachment tombstone time: %w", err)
	}
	return &attachment.Attachment{ID: id, DeletedAt: &t}, nil
}

// ListAttachmentsByProject returns attachments for a project ordered by
// creation time descending. Soft-deleted attachments are excluded.
func (s *Store) ListAttachmentsByProject(ctx context.Context, projectID string, limit int) ([]attachment.Attachment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, COALESCE(session_id,''), file_ref, original_name,
		  mime, size, sha256, parse_status, parse_error_code, parsed_text,
		  parsed_text_bytes, created_at, deleted_at
		 FROM attachments WHERE project_id=? AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list attachments by project: %w", err)
	}
	defer rows.Close()
	return scanAttachments(rows)
}

// ListAttachmentsBySession returns attachments for a session ordered by
// creation time descending. Soft-deleted attachments are excluded.
func (s *Store) ListAttachmentsBySession(ctx context.Context, sessionID string, limit int) ([]attachment.Attachment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, COALESCE(session_id,''), file_ref, original_name,
		  mime, size, sha256, parse_status, parse_error_code, parsed_text,
		  parsed_text_bytes, created_at, deleted_at
		 FROM attachments WHERE session_id=? AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list attachments by session: %w", err)
	}
	defer rows.Close()
	return scanAttachments(rows)
}

// UpdateParseResult updates the parse status, error code, parsed text, and
// parsed_text_bytes for an attachment. Only readable (non-deleted) attachments
// may be updated.
func (s *Store) UpdateParseResult(ctx context.Context, id string, status attachment.ParseStatus, errCode string, parsedText string, parsedTextBytes int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE attachments SET parse_status=?, parse_error_code=?, parsed_text=?, parsed_text_bytes=?
		 WHERE id=? AND deleted_at IS NULL`,
		string(status), errCode, parsedText, parsedTextBytes, id)
	if err != nil {
		return fmt.Errorf("update parse result: %w", err)
	}
	return nil
}

// DeleteAttachment soft-deletes an attachment by setting deleted_at and
// recording a tombstone for fail-closed readability (ADR-005 §7). Idempotent:
// deleting an already-deleted attachment is a no-op.
func (s *Store) DeleteAttachment(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete attachment: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	var fileRef string
	if err := tx.QueryRowContext(ctx, `SELECT file_ref FROM attachments WHERE id=?`, id).Scan(&fileRef); err != nil {
		if err == sql.ErrNoRows {
			return tx.Commit()
		}
		return fmt.Errorf("find attachment for delete: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE attachments SET deleted_at=? WHERE id=? AND deleted_at IS NULL`,
		formatTime(now), id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Already deleted (or never existed) — idempotent no-op.
		return tx.Commit()
	}
	stamp := formatTime(now)
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) VALUES('attachment',?,?,'pending')`, id, stamp); err != nil {
		return fmt.Errorf("record attachment tombstone: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) VALUES('attachment_file',?,?,'pending')`, fileRef, stamp); err != nil {
		return fmt.Errorf("schedule attachment file cleanup: %w", err)
	}
	return tx.Commit()
}

func (s *Store) ListPendingAttachmentFileCleanup(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT owner_id FROM deletion_tombstones WHERE owner_type='attachment_file' AND propagation_status='pending' ORDER BY deleted_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Store) CompleteAttachmentFileCleanup(ctx context.Context, fileRef string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deletion_tombstones SET propagation_status='propagated' WHERE owner_type='attachment_file' AND owner_id=?`, fileRef)
	return err
}

type attachmentScanner interface {
	Scan(dest ...any) error
}

func scanAttachment(row attachmentScanner) (attachment.Attachment, error) {
	var a attachment.Attachment
	var createdAt string
	var deletedAt sql.NullString
	var sessionID sql.NullString
	err := row.Scan(
		&a.ID, &a.ProjectID, &sessionID, &a.FileRef, &a.OriginalName,
		&a.MIME, &a.Size, &a.SHA256, &a.ParseStatus, &a.ParseErrorCode,
		&a.ParsedText, &a.ParsedTextBytes, &createdAt, &deletedAt)
	if err != nil {
		return a, err
	}
	if sessionID.Valid {
		a.SessionID = sessionID.String
	}
	a.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return a, err
	}
	if deletedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, deletedAt.String)
		if err != nil {
			return a, err
		}
		a.DeletedAt = &t
	}
	return a, nil
}

func scanAttachments(rows *sql.Rows) ([]attachment.Attachment, error) {
	var result []attachment.Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
