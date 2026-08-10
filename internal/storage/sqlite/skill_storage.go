package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

// CreateSkill inserts a new skill. If sk.ID is empty a ULID is generated.
// The sk.Permissions slice is serialized to JSON for the permissions_json column.
func (s *Store) CreateSkill(ctx context.Context, sk skill.Skill) (skill.Skill, error) {
	if sk.ID == "" {
		var err error
		sk.ID, err = s.newULID(time.Now())
		if err != nil {
			return sk, err
		}
	}
	now := time.Now().UTC()
	sk.CreatedAt = now
	sk.UpdatedAt = now
	if sk.Status == "" {
		sk.Status = skill.SkillStatusDraft
	}
	if sk.Description == "" {
		sk.Description = ""
	}
	permJSON, err := json.Marshal(sk.Permissions)
	if err != nil {
		return sk, err
	}
	if err := sk.Validate(); err != nil {
		return sk, err
	}
	var signature, publisherID, minEngineVersion any
	if sk.Signature != nil {
		signature = *sk.Signature
	}
	if sk.PublisherID != nil {
		publisherID = *sk.PublisherID
	}
	if sk.MinEngineVersion != nil {
		minEngineVersion = *sk.MinEngineVersion
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO skills(id, name, display_name, description, version, status,
		 permissions_json, entry_point, manifest_json, signature, publisher_id,
		 min_engine_version, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sk.ID, sk.Name, sk.DisplayName, sk.Description, sk.Version, sk.Status,
		string(permJSON), sk.EntryPoint, sk.ManifestJSON, signature, publisherID,
		minEngineVersion, formatTime(sk.CreatedAt), formatTime(sk.UpdatedAt))
	return sk, mapWriteError(err)
}

// GetSkill returns a skill by ID, or nil if not found.
func (s *Store) GetSkill(ctx context.Context, id string) (*skill.Skill, error) {
	var sk skill.Skill
	var created, updated string
	var signature, publisherID, minEngineVersion sql.NullString
	var permissionsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, version, status,
		 permissions_json, entry_point, manifest_json, signature, publisher_id,
		 min_engine_version, created_at, updated_at
		 FROM skills WHERE id=?`, id).Scan(
		&sk.ID, &sk.Name, &sk.DisplayName, &sk.Description, &sk.Version, &sk.Status,
		&permissionsJSON, &sk.EntryPoint, &sk.ManifestJSON, &signature, &publisherID,
		&minEngineVersion, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sk.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, err
	}
	sk.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(permissionsJSON), &sk.Permissions); err != nil {
		return nil, err
	}
	if signature.Valid {
		sk.Signature = &signature.String
	}
	if publisherID.Valid {
		sk.PublisherID = &publisherID.String
	}
	if minEngineVersion.Valid {
		sk.MinEngineVersion = &minEngineVersion.String
	}
	return &sk, nil
}

// GetSkillByNameVersion returns a skill by name and version, or nil if not found.
func (s *Store) GetSkillByNameVersion(ctx context.Context, name, version string) (*skill.Skill, error) {
	var sk skill.Skill
	var created, updated string
	var signature, publisherID, minEngineVersion sql.NullString
	var permissionsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, description, version, status,
		 permissions_json, entry_point, manifest_json, signature, publisher_id,
		 min_engine_version, created_at, updated_at
		 FROM skills WHERE name=? AND version=?`, name, version).Scan(
		&sk.ID, &sk.Name, &sk.DisplayName, &sk.Description, &sk.Version, &sk.Status,
		&permissionsJSON, &sk.EntryPoint, &sk.ManifestJSON, &signature, &publisherID,
		&minEngineVersion, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sk.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, err
	}
	sk.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(permissionsJSON), &sk.Permissions); err != nil {
		return nil, err
	}
	if signature.Valid {
		sk.Signature = &signature.String
	}
	if publisherID.Valid {
		sk.PublisherID = &publisherID.String
	}
	if minEngineVersion.Valid {
		sk.MinEngineVersion = &minEngineVersion.String
	}
	return &sk, nil
}

// ListSkills returns skills optionally filtered by status, ordered by created_at descending.
func (s *Store) ListSkills(ctx context.Context, status string, limit int) ([]skill.Skill, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	query := `SELECT id, name, display_name, description, version, status,
	 permissions_json, entry_point, manifest_json, signature, publisher_id,
	 min_engine_version, created_at, updated_at
	 FROM skills`
	args := []any{}
	if status != "" {
		query += ` WHERE status=?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC, id LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []skill.Skill
	for rows.Next() {
		var sk skill.Skill
		var created, updated string
		var signature, publisherID, minEngineVersion sql.NullString
		var permissionsJSON string
		if err = rows.Scan(
			&sk.ID, &sk.Name, &sk.DisplayName, &sk.Description, &sk.Version, &sk.Status,
			&permissionsJSON, &sk.EntryPoint, &sk.ManifestJSON, &signature, &publisherID,
			&minEngineVersion, &created, &updated); err != nil {
			return nil, err
		}
		sk.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		sk.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(permissionsJSON), &sk.Permissions); err != nil {
			return nil, err
		}
		if signature.Valid {
			sk.Signature = &signature.String
		}
		if publisherID.Valid {
			sk.PublisherID = &publisherID.String
		}
		if minEngineVersion.Valid {
			sk.MinEngineVersion = &minEngineVersion.String
		}
		result = append(result, sk)
	}
	return result, rows.Err()
}

// UpdateSkill updates the display name and description of a skill.
func (s *Store) UpdateSkill(ctx context.Context, id, displayName, description string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE skills SET display_name=?, description=?, updated_at=? WHERE id=?`,
		displayName, description, formatTime(time.Now().UTC()), id)
	return mapWriteError(err)
}

// UpdateSkillStatus updates the status of a skill.
func (s *Store) UpdateSkillStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE skills SET status=?, updated_at=? WHERE id=?`,
		status, formatTime(time.Now().UTC()), id)
	return mapWriteError(err)
}

// DeleteSkill deletes a skill by ID.
func (s *Store) DeleteSkill(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM skills WHERE id=?`, id)
	return mapWriteError(err)
}