package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/asset"
)

func scanAssetTemplate(row interface {
	Scan(dest ...any) error
}) (asset.AssetTemplate, error) {
	var t asset.AssetTemplate
	var created, updated string
	if err := row.Scan(
		&t.ID, &t.TemplateCode, &t.Name, &t.TemplateType, &t.DocumentType,
		&t.Description, &t.Client, &t.MimeType, &t.FileName, &t.FilePath,
		&t.Status, &created, &updated, &t.Version,
	); err != nil {
		return t, err
	}
	var err error
	t.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return t, err
	}
	t.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return t, err
	}
	return t, nil
}

const assetTemplateSelect = `SELECT id, template_code, name, template_type, document_type,
	description, client, mime_type, file_name, file_path, status, created_at, updated_at, version
	FROM asset_templates`

// NextAssetTemplateCode allocates the next TPL##### code inside a transaction.
func (s *Store) NextAssetTemplateCode(ctx context.Context, tx *sql.Tx) (string, error) {
	// A method value bound to a nil *sql.Tx is itself non-nil, so the tx has to
	// be nil-checked directly — testing the bound function would never fall back
	// and a nil tx would panic on the first call instead.
	q := s.db.QueryRowContext
	if tx != nil {
		q = tx.QueryRowContext
	}
	var last sql.NullString
	err := q(ctx, `SELECT template_code FROM asset_templates
		WHERE template_code GLOB 'TPL[0-9]*'
		ORDER BY CAST(substr(template_code, 4) AS INTEGER) DESC LIMIT 1`).Scan(&last)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	next := 1
	if last.Valid {
		n, parseErr := strconv.Atoi(strings.TrimPrefix(last.String, "TPL"))
		if parseErr != nil {
			return "", parseErr
		}
		next = n + 1
	}
	return fmt.Sprintf("TPL%05d", next), nil
}

// CreateAssetTemplate inserts a new draft template.
func (s *Store) CreateAssetTemplate(ctx context.Context, tpl asset.AssetTemplate) (asset.AssetTemplate, error) {
	if tpl.ID == "" {
		var err error
		tpl.ID, err = s.newULID(time.Now())
		if err != nil {
			return tpl, err
		}
	}
	now := time.Now().UTC()
	tpl.CreatedAt = now
	tpl.UpdatedAt = now
	if tpl.Status == "" {
		tpl.Status = asset.StatusDraft
	}
	if tpl.Version == 0 {
		tpl.Version = 1
	}
	err := s.execWithAudit(ctx, "asset_template.created", tpl.ID, "engine",
		map[string]any{"templateCode": tpl.TemplateCode, "name": tpl.Name},
		func(tx *sql.Tx) error {
			code, err := s.NextAssetTemplateCode(ctx, tx)
			if err != nil {
				return err
			}
			tpl.TemplateCode = code
			_, err = tx.ExecContext(ctx,
				`INSERT INTO asset_templates(
					id, template_code, name, template_type, document_type, description, client,
					mime_type, file_name, file_path, status, created_at, updated_at, version)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				tpl.ID, tpl.TemplateCode, tpl.Name, tpl.TemplateType, string(tpl.DocumentType),
				tpl.Description, tpl.Client, tpl.MimeType, tpl.FileName, tpl.FilePath,
				tpl.Status, formatTime(tpl.CreatedAt), formatTime(tpl.UpdatedAt), tpl.Version)
			return err
		})
	return tpl, mapWriteError(err)
}

// GetAssetTemplate returns a template by ID.
func (s *Store) GetAssetTemplate(ctx context.Context, id string) (asset.AssetTemplate, error) {
	row := s.db.QueryRowContext(ctx, assetTemplateSelect+` WHERE id=?`, id)
	t, err := scanAssetTemplate(row)
	if err == sql.ErrNoRows {
		return asset.AssetTemplate{}, asset.ErrNotFound
	}
	return t, err
}

// ListAssetTemplates returns templates filtered by status/type/document type.
func (s *Store) ListAssetTemplates(ctx context.Context, filter asset.Filter) ([]asset.AssetTemplate, error) {
	query := assetTemplateSelect + ` WHERE 1=1`
	args := make([]any, 0, 3)
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	if filter.TemplateType != "" {
		query += ` AND template_type=?`
		args = append(args, filter.TemplateType)
	}
	if filter.DocumentType != "" {
		query += ` AND document_type=?`
		args = append(args, filter.DocumentType)
	}
	query += ` ORDER BY updated_at DESC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]asset.AssetTemplate, 0)
	for rows.Next() {
		t, err := scanAssetTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// UpdateAssetTemplateStatus transitions template status with optimistic locking.
func (s *Store) UpdateAssetTemplateStatus(ctx context.Context, id string, expectedVersion int64, next asset.Status) (asset.AssetTemplate, error) {
	cur, err := s.GetAssetTemplate(ctx, id)
	if err != nil {
		return asset.AssetTemplate{}, err
	}
	if cur.Version != expectedVersion {
		return asset.AssetTemplate{}, asset.ErrVersionConflict
	}
	switch next {
	case asset.StatusEnabled:
		if !asset.CanEnable(cur.Status) {
			return asset.AssetTemplate{}, asset.ErrInvalidTransition
		}
	case asset.StatusVoid:
		if !asset.CanVoid(cur.Status) {
			return asset.AssetTemplate{}, asset.ErrInvalidTransition
		}
	case asset.StatusDraft:
		if cur.Status != asset.StatusVoid {
			return asset.AssetTemplate{}, asset.ErrInvalidTransition
		}
	default:
		return asset.AssetTemplate{}, asset.ErrInvalidTransition
	}
	now := time.Now().UTC()
	err = s.execWithAudit(ctx, "asset_template.status", id, "engine",
		map[string]any{"from": cur.Status, "to": next},
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`UPDATE asset_templates SET status=?, updated_at=?, version=version+1
				 WHERE id=? AND version=?`, next, formatTime(now), id, expectedVersion)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return asset.ErrVersionConflict
			}
			return nil
		})
	if err != nil {
		return asset.AssetTemplate{}, mapWriteError(err)
	}
	return s.GetAssetTemplate(ctx, id)
}

// CountAssetTemplateReferences counts deliverables referencing a template.
func (s *Store) CountAssetTemplateReferences(ctx context.Context, templateID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM project_deliverables WHERE template_id=?`, templateID).Scan(&n)
	return n, err
}

// DeleteAssetTemplate removes a draft template with no references.
func (s *Store) DeleteAssetTemplate(ctx context.Context, id string, expectedVersion int64) error {
	cur, err := s.GetAssetTemplate(ctx, id)
	if err != nil {
		return err
	}
	if cur.Version != expectedVersion {
		return asset.ErrVersionConflict
	}
	if !asset.CanDelete(cur.Status) {
		return asset.ErrInvalidTransition
	}
	n, err := s.CountAssetTemplateReferences(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return asset.ErrTemplateReferenced
	}
	return s.execWithAudit(ctx, "asset_template.deleted", id, "engine", nil,
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`DELETE FROM asset_templates WHERE id=? AND version=? AND status='draft'`, id, expectedVersion)
			if err != nil {
				return err
			}
			aff, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if aff == 0 {
				return asset.ErrVersionConflict
			}
			return nil
		})
}
