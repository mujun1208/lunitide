package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/memory"
)

// CreateMemory inserts a new memory.
func (s *Store) CreateMemory(ctx context.Context, mem memory.Memory) (memory.Memory, error) {
	if mem.ID == "" {
		var err error
		mem.ID, err = s.newULID(time.Now())
		if err != nil {
			return mem, err
		}
	}
	now := time.Now().UTC()
	mem.CreatedAt = now
	mem.UpdatedAt = now
	if mem.AccessCount == 0 {
		mem.AccessCount = 0
	}
	if mem.Confidence == 0 {
		mem.Confidence = 1.0
	}
	if err := mem.Validate(); err != nil {
		return mem, err
	}
	var embeddingID, sourceID, sourceType, lastAccessed, expiresAt any
	if mem.EmbeddingID != nil {
		embeddingID = *mem.EmbeddingID
	}
	if mem.SourceID != nil {
		sourceID = *mem.SourceID
	}
	if mem.SourceType != nil {
		sourceType = *mem.SourceType
	}
	if mem.LastAccessed != nil {
		lastAccessed = formatTime(*mem.LastAccessed)
	}
	if mem.ExpiresAt != nil {
		expiresAt = formatTime(*mem.ExpiresAt)
	}
	err := s.execWithAudit(ctx, "memory.created", mem.ID, "engine",
		map[string]any{"projectId": mem.ProjectID, "layer": mem.Layer, "scope": mem.Scope},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO memories(id, project_id, layer, scope, key, content,
				 embedding_id, source_id, source_type, confidence, access_count,
				 last_accessed, expires_at, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				mem.ID, mem.ProjectID, string(mem.Layer), string(mem.Scope), mem.Key, mem.Content,
				embeddingID, sourceID, sourceType, float64(mem.Confidence), mem.AccessCount,
				lastAccessed, expiresAt, formatTime(mem.CreatedAt), formatTime(mem.UpdatedAt))
			return err
		})
	return mem, mapWriteError(err)
}

// GetMemory returns a memory by ID.
func (s *Store) GetMemory(ctx context.Context, id string) (*memory.Memory, error) {
	var m memory.Memory
	var layer, scope string
	var embeddingID, sourceID, sourceType sql.NullString
	var lastAccessed, expiresAt sql.NullString
	var created, updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, layer, scope, key, content,
		 embedding_id, source_id, source_type, confidence, access_count,
		 last_accessed, expires_at, created_at, updated_at
		 FROM memories WHERE id=?`, id).Scan(
		&m.ID, &m.ProjectID, &layer, &scope, &m.Key, &m.Content,
		&embeddingID, &sourceID, &sourceType, &m.Confidence, &m.AccessCount,
		&lastAccessed, &expiresAt, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Layer = memory.Layer(layer)
	m.Scope = memory.MemoryScope(scope)
	if embeddingID.Valid {
		m.EmbeddingID = &embeddingID.String
	}
	if sourceID.Valid {
		m.SourceID = &sourceID.String
	}
	if sourceType.Valid {
		m.SourceType = &sourceType.String
	}
	m.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, err
	}
	m.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return nil, err
	}
	if lastAccessed.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastAccessed.String)
		if err != nil {
			return nil, err
		}
		m.LastAccessed = &t
	}
	if expiresAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, expiresAt.String)
		if err != nil {
			return nil, err
		}
		m.ExpiresAt = &t
	}
	return &m, nil
}

// ListMemoriesByProject returns memories for a project, optionally filtered by layer.
func (s *Store) ListMemoriesByProject(ctx context.Context, projectID string, layer string, limit int) ([]memory.Memory, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if layer == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, project_id, layer, scope, key, content,
			 embedding_id, source_id, source_type, confidence, access_count,
			 last_accessed, expires_at, created_at, updated_at
			 FROM memories WHERE project_id=? ORDER BY created_at DESC LIMIT ?`, projectID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, project_id, layer, scope, key, content,
			 embedding_id, source_id, source_type, confidence, access_count,
			 last_accessed, expires_at, created_at, updated_at
			 FROM memories WHERE project_id=? AND layer=? ORDER BY created_at DESC LIMIT ?`, projectID, layer, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []memory.Memory
	for rows.Next() {
		var m memory.Memory
		var l, sc string
		var embeddingID, sourceID, sourceType sql.NullString
		var lastAccessed, expiresAt sql.NullString
		var created, updated string
		if err = rows.Scan(
			&m.ID, &m.ProjectID, &l, &sc, &m.Key, &m.Content,
			&embeddingID, &sourceID, &sourceType, &m.Confidence, &m.AccessCount,
			&lastAccessed, &expiresAt, &created, &updated); err != nil {
			return nil, err
		}
		m.Layer = memory.Layer(l)
		m.Scope = memory.MemoryScope(sc)
		if embeddingID.Valid {
			m.EmbeddingID = &embeddingID.String
		}
		if sourceID.Valid {
			m.SourceID = &sourceID.String
		}
		if sourceType.Valid {
			m.SourceType = &sourceType.String
		}
		m.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		m.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		if lastAccessed.Valid {
			t, err := time.Parse(time.RFC3339Nano, lastAccessed.String)
			if err != nil {
				return nil, err
			}
			m.LastAccessed = &t
		}
		if expiresAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, expiresAt.String)
			if err != nil {
				return nil, err
			}
			m.ExpiresAt = &t
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// UpdateMemory updates the content of a memory.
func (s *Store) UpdateMemory(ctx context.Context, id string, content string) error {
	err := s.execWithAudit(ctx, "memory.updated", id, "engine", nil,
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`UPDATE memories SET content=?, updated_at=? WHERE id=?`,
				content, formatTime(time.Now().UTC()), id)
			return err
		})
	return mapWriteError(err)
}

// DeleteMemory deletes a memory by ID.
func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	err := s.execWithAudit(ctx, "memory.deleted", id, "engine", nil,
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`DELETE FROM memories WHERE id=?`, id)
			return err
		})
	return mapWriteError(err)
}

// IncrementAccessCount increments the access count and updates last_accessed for a memory.
func (s *Store) IncrementAccessCount(ctx context.Context, id string) error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx,
		`UPDATE memories SET access_count = access_count + 1, last_accessed = ? WHERE id=?`,
		now, id)
	return mapWriteError(err)
}