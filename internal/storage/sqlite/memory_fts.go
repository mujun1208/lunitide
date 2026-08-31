package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lunitide/lunitide/internal/m8app"
)

// SearchMemoryFactFTS ranks confirmed candidates and compaction summaries
// through the 0107 FTS5 trigram index. Short CJK terms fall back to LIKE.
func (s *Store) SearchMemoryFactFTS(ctx context.Context, query string, limit int) ([]m8app.MemoryFTSHit, error) {
	if s == nil || s.db == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit < 1 {
		limit = 8
	}
	if limit > 32 {
		limit = 32
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	hits, err := queryMemoryFactFTS(ctx, tx, query, limit, true)
	if err != nil || len(hits) == 0 {
		fallback, likeErr := queryMemoryFactFTS(ctx, tx, query, limit, false)
		if likeErr == nil {
			hits, err = fallback, nil
		} else if err != nil {
			return nil, nil
		}
	}
	if err != nil {
		return nil, nil
	}
	return hits, nil
}

func queryMemoryFactFTS(ctx context.Context, tx *sql.Tx, query string, limit int, useMatch bool) ([]m8app.MemoryFTSHit, error) {
	long, short := partitionSearchTerms(query)
	terms := append(append([]string{}, long...), short...)
	if len(terms) == 0 {
		return nil, nil
	}
	ors := make([]string, 0, 1+len(terms))
	args := make([]any, 0, 8)
	if useMatch {
		if match := messageFTSMatchTerms(long); match != "" {
			ors = append(ors, "memory_fact_fts MATCH ?")
			args = append(args, match)
		}
		for _, term := range short {
			ors = append(ors, `LOWER(body) LIKE ? ESCAPE '\'`)
			args = append(args, likeContainsArg(term))
		}
	}
	if !useMatch || len(ors) == 0 {
		ors = ors[:0]
		args = args[:0]
		for _, term := range terms {
			ors = append(ors, `LOWER(body) LIKE ? ESCAPE '\'`)
			args = append(args, likeContainsArg(term))
		}
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, `
SELECT source_id, kind, body
FROM memory_fact_fts
WHERE (`+strings.Join(ors, " OR ")+`)
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []m8app.MemoryFTSHit
	seen := map[string]bool{}
	for rows.Next() {
		var hit m8app.MemoryFTSHit
		if err := rows.Scan(&hit.SourceID, &hit.Kind, &hit.Body); err != nil {
			return nil, err
		}
		key := hit.Kind + ":" + hit.SourceID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, hit)
	}
	return out, rows.Err()
}
