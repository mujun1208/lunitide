package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/memory"
)

// SearchMemoriesFTS ranks project memories through the 0121 memory_fts FTS5
// trigram index, ordered by confidence descending. Short CJK terms fall back
// to LIKE (same MATCH-then-LIKE strategy as message/memory_fact search).
func (s *Store) SearchMemoriesFTS(ctx context.Context, projectID string, query string, limit int) ([]memory.Memory, error) {
	return s.SearchActiveMemoriesFTS(ctx, projectID, query, time.Now().UTC(), limit)
}
func (s *Store) SearchActiveMemoriesFTS(ctx context.Context, projectID, query string, now time.Time, limit int) ([]memory.Memory, error) {
	if s == nil || s.db == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := queryMemoriesFTS(ctx, tx, projectID, query, now, limit, true)
	if err != nil || len(rows) == 0 {
		fallback, likeErr := queryMemoriesFTS(ctx, tx, projectID, query, now, limit, false)
		if likeErr == nil {
			rows, err = fallback, nil
		} else if err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func queryMemoriesFTS(ctx context.Context, tx *sql.Tx, projectID, query string, now time.Time, limit int, useMatch bool) ([]memory.Memory, error) {
	long, short := partitionSearchTerms(query)
	terms := append(append([]string{}, long...), short...)
	if len(terms) == 0 {
		return nil, nil
	}
	ors := make([]string, 0, 1+len(terms))
	args := make([]any, 0, 8)
	if useMatch {
		if match := messageFTSMatchTerms(long); match != "" {
			ors = append(ors, "memory_fts MATCH ?")
			args = append(args, match)
		}
		for _, term := range short {
			ors = append(ors, `(LOWER(f.key) LIKE ? ESCAPE '\' OR LOWER(f.content) LIKE ? ESCAPE '\')`)
			args = append(args, likeContainsArg(term), likeContainsArg(term))
		}
	}
	if !useMatch || len(ors) == 0 {
		ors = ors[:0]
		args = args[:0]
		for _, term := range terms {
			ors = append(ors, `(LOWER(f.key) LIKE ? ESCAPE '\' OR LOWER(f.content) LIKE ? ESCAPE '\')`)
			args = append(args, likeContainsArg(term), likeContainsArg(term))
		}
	}
	args = append(args, projectID, expiryCutoff(now), limit)
	sqlRows, err := tx.QueryContext(ctx, `
SELECT m.id, m.project_id, m.layer, m.scope, m.key, m.content,
       m.embedding_id, m.source_id, m.source_type, m.confidence, m.access_count,
       m.last_accessed, m.expires_at, m.created_at, m.updated_at
FROM memory_fts f
JOIN memories m ON m.id = f.memory_id
WHERE (`+strings.Join(ors, " OR ")+`)
  AND m.project_id = ?
  AND (m.expires_at IS NULL OR `+memoryExpiryOrderSQL("m.expires_at")+`>?)
ORDER BY m.confidence DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var out []memory.Memory
	seen := map[string]bool{}
	for sqlRows.Next() {
		var m memory.Memory
		var layer, scope string
		var embeddingID, sourceID, sourceType sql.NullString
		var lastAccessed, expiresAt sql.NullString
		var created, updated string
		if err := sqlRows.Scan(
			&m.ID, &m.ProjectID, &layer, &scope, &m.Key, &m.Content,
			&embeddingID, &sourceID, &sourceType, &m.Confidence, &m.AccessCount,
			&lastAccessed, &expiresAt, &created, &updated); err != nil {
			return nil, err
		}
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
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
		out = append(out, m)
	}
	return out, sqlRows.Err()
}
