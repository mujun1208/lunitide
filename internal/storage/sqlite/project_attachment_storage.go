package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/projectattachment"
)

const projectAttachmentSelect = `SELECT id, project_id, phase, category, file_name, mime_type, file_path, digest, version, created_at, updated_at
	FROM project_attachments`

func scanProjectAttachment(row interface {
	Scan(dest ...any) error
}) (projectattachment.Attachment, error) {
	var a projectattachment.Attachment
	var created, updated string
	if err := row.Scan(
		&a.ID, &a.ProjectID, &a.Phase, &a.Category, &a.FileName, &a.MimeType, &a.FilePath, &a.Digest,
		&a.Version, &created, &updated,
	); err != nil {
		return a, err
	}
	var err error
	a.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return a, err
	}
	a.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return a, err
	}
	return a, nil
}

// ListProjectAttachments lists attachments for a project and optional phase.
func (s *Store) ListProjectAttachments(ctx context.Context, filter projectattachment.Filter) ([]projectattachment.Attachment, error) {
	query := projectAttachmentSelect + ` WHERE project_id=?`
	args := []any{filter.ProjectID}
	if filter.Phase >= 1 && filter.Phase <= 9 {
		query += ` AND phase=?`
		args = append(args, filter.Phase)
	}
	query += ` ORDER BY phase ASC, updated_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]projectattachment.Attachment, 0)
	for rows.Next() {
		a, err := scanProjectAttachment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

// CreateProjectAttachment inserts a new project attachment record.
func (s *Store) CreateProjectAttachment(ctx context.Context, a projectattachment.Attachment) (projectattachment.Attachment, error) {
	if a.ID == "" {
		var err error
		a.ID, err = s.newULID(time.Now())
		if err != nil {
			return a, err
		}
	}
	now := time.Now().UTC()
	if a.Category == "" {
		a.Category = projectattachment.CategoryPhase
	}
	if a.Version == 0 {
		a.Version = 1
	}
	a.CreatedAt = now
	a.UpdatedAt = now
	if err := a.Validate(); err != nil {
		return projectattachment.Attachment{}, err
	}
	err := s.execWithAudit(ctx, "project_attachment.created", a.ID, "engine",
		map[string]any{"projectId": a.ProjectID, "phase": a.Phase, "fileName": a.FileName},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO project_attachments(
					id, project_id, phase, category, file_name, mime_type, file_path, digest, version, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				a.ID, a.ProjectID, a.Phase, a.Category, a.FileName, a.MimeType, a.FilePath, a.Digest,
				a.Version, formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
			return err
		})
	if err != nil {
		return projectattachment.Attachment{}, mapWriteError(err)
	}
	row := s.db.QueryRowContext(ctx, projectAttachmentSelect+` WHERE id=?`, a.ID)
	return scanProjectAttachment(row)
}

// GetProjectAttachment returns one attachment by ID.
func (s *Store) GetProjectAttachment(ctx context.Context, id string) (projectattachment.Attachment, error) {
	row := s.db.QueryRowContext(ctx, projectAttachmentSelect+` WHERE id=?`, id)
	a, err := scanProjectAttachment(row)
	if err == sql.ErrNoRows {
		return projectattachment.Attachment{}, projectattachment.ErrNotFound
	}
	return a, err
}
