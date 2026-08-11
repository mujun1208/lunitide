package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO attachments(id, project_id, session_id, file_ref, original_name,
		  mime, size, sha256, parse_status, parse_error_code, parsed_text,
		  parsed_text_bytes, created_at, deleted_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.ProjectID, sessionID, a.FileRef, a.OriginalName,
		a.MIME, a.Size, a.SHA256, string(a.ParseStatus), a.ParseErrorCode,
		a.ParsedText, a.ParsedTextBytes, formatTime(a.CreatedAt), nil)
	if err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	return nil
}

// GetAttachment returns an attachment by ID, or nil when not found.
func (s *Store) GetAttachment(ctx context.Context, id string) (*attachment.Attachment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, COALESCE(session_id,''), file_ref, original_name,
		  mime, size, sha256, parse_status, parse_error_code, parsed_text,
		  parsed_text_bytes, created_at, deleted_at
		 FROM attachments WHERE id=?`, id)
	a, err := scanAttachment(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return &a, nil
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
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE attachments SET deleted_at=? WHERE id=? AND deleted_at IS NULL`,
		formatTime(now), id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Already deleted (or never existed) — idempotent no-op.
		return nil
	}
	if err := s.RecordTombstone(ctx, "attachment", id); err != nil {
		return err
	}
	return nil
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
