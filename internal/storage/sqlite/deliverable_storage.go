package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/deliverable"
)

func scanProjectDeliverable(row interface {
	Scan(dest ...any) error
}) (deliverable.ProjectDeliverable, error) {
	var d deliverable.ProjectDeliverable
	var templateID, attachmentID sql.NullString
	var created, updated string
	if err := row.Scan(
		&d.ID, &d.ProjectID, &d.Phase, &d.DocumentType, &d.Title,
		&templateID, &attachmentID, &d.Status, &d.GateConfirmations, &d.Digest,
		&created, &updated, &d.Version,
	); err != nil {
		return d, err
	}
	if templateID.Valid {
		d.TemplateID = templateID.String
	}
	if attachmentID.Valid {
		d.AttachmentID = attachmentID.String
	}
	var err error
	d.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return d, err
	}
	d.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return d, err
	}
	return d, nil
}

const deliverableSelect = `SELECT id, project_id, phase, document_type, title,
	template_id, attachment_id, status, gate_confirmations, digest, created_at, updated_at, version
	FROM project_deliverables`

// ListProjectDeliverables lists deliverables for a project and optional phase/status.
func (s *Store) ListProjectDeliverables(ctx context.Context, filter deliverable.Filter) ([]deliverable.ProjectDeliverable, error) {
	query := deliverableSelect + ` WHERE project_id=?`
	args := []any{filter.ProjectID}
	if filter.Phase >= 1 && filter.Phase <= 9 {
		query += ` AND phase=?`
		args = append(args, filter.Phase)
	}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY phase ASC, document_type ASC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]deliverable.ProjectDeliverable, 0)
	for rows.Next() {
		d, err := scanProjectDeliverable(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// UpsertProjectDeliverable creates or updates a deliverable keyed by project+phase+document_type.
func (s *Store) UpsertProjectDeliverable(ctx context.Context, d deliverable.ProjectDeliverable) (deliverable.ProjectDeliverable, error) {
	if d.ID == "" {
		var err error
		d.ID, err = s.newULID(time.Now())
		if err != nil {
			return d, err
		}
	}
	now := time.Now().UTC()
	if d.Status == "" {
		d.Status = deliverable.StatusDraft
	}
	if d.Version == 0 {
		d.Version = 1
	}
	d.CreatedAt = now
	d.UpdatedAt = now

	var templateID, attachmentID any
	if d.TemplateID != "" {
		templateID = d.TemplateID
	}
	if d.AttachmentID != "" {
		attachmentID = d.AttachmentID
	}

	err := s.execWithAudit(ctx, "project_deliverable.upserted", d.ID, "engine",
		map[string]any{"projectId": d.ProjectID, "phase": d.Phase, "documentType": d.DocumentType},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO project_deliverables(
					id, project_id, phase, document_type, title, template_id, attachment_id,
					status, gate_confirmations, digest, created_at, updated_at, version)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
				 ON CONFLICT(project_id, phase, document_type) DO UPDATE SET
					title=excluded.title,
					template_id=excluded.template_id,
					attachment_id=excluded.attachment_id,
					status=CASE WHEN project_deliverables.status='immutable' THEN project_deliverables.status ELSE excluded.status END,
					digest=excluded.digest,
					updated_at=excluded.updated_at,
					version=project_deliverables.version+1
				 WHERE project_deliverables.status!='immutable'`,
				d.ID, d.ProjectID, d.Phase, d.DocumentType, d.Title, templateID, attachmentID,
				d.Status, d.GateConfirmations, d.Digest,
				formatTime(d.CreatedAt), formatTime(d.UpdatedAt), d.Version)
			return err
		})
	if err != nil {
		return deliverable.ProjectDeliverable{}, mapWriteError(err)
	}
	row := s.db.QueryRowContext(ctx, deliverableSelect+
		` WHERE project_id=? AND phase=? AND document_type=?`, d.ProjectID, d.Phase, d.DocumentType)
	return scanProjectDeliverable(row)
}

// ConfirmDeliverableGate increments gate confirmations; at 3 sets status immutable.
func (s *Store) ConfirmDeliverableGate(ctx context.Context, projectID, id string, expectedVersion int64) (deliverable.ProjectDeliverable, error) {
	cur, err := s.GetProjectDeliverable(ctx, id)
	if err != nil {
		return deliverable.ProjectDeliverable{}, err
	}
	if cur.ProjectID != projectID {
		return deliverable.ProjectDeliverable{}, deliverable.ErrNotFound
	}
	if cur.Version != expectedVersion {
		return deliverable.ProjectDeliverable{}, deliverable.ErrVersionConflict
	}
	if !deliverable.CanConfirmGate(cur) {
		return deliverable.ProjectDeliverable{}, deliverable.ErrGateLocked
	}
	now := time.Now().UTC()
	nextConfirmations := cur.GateConfirmations + 1
	nextStatus := cur.Status
	if nextConfirmations >= 3 {
		nextStatus = deliverable.StatusImmutable
	}
	err = s.execWithAudit(ctx, "project_deliverable.gate_confirmed", id, "engine",
		map[string]any{"gateConfirmations": nextConfirmations, "status": nextStatus},
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`UPDATE project_deliverables
				 SET gate_confirmations=?, status=?, updated_at=?, version=version+1
				 WHERE id=? AND project_id=? AND version=? AND gate_confirmations<? AND status!='immutable'`,
				nextConfirmations, nextStatus, formatTime(now), id, projectID, expectedVersion, 3)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return deliverable.ErrVersionConflict
			}
			return nil
		})
	if err != nil {
		return deliverable.ProjectDeliverable{}, mapWriteError(err)
	}
	return s.GetProjectDeliverable(ctx, id)
}

// GetProjectDeliverable returns one deliverable by ID.
func (s *Store) GetProjectDeliverable(ctx context.Context, id string) (deliverable.ProjectDeliverable, error) {
	row := s.db.QueryRowContext(ctx, deliverableSelect+` WHERE id=?`, id)
	d, err := scanProjectDeliverable(row)
	if err == sql.ErrNoRows {
		return deliverable.ProjectDeliverable{}, deliverable.ErrNotFound
	}
	return d, err
}
