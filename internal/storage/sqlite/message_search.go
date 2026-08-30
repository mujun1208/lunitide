package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/messageapp"
)

const ftsTrigramMinRunes = 3

func messageFTSMatch(query string) string {
	return messageFTSMatchTerms(strings.Fields(strings.TrimSpace(query)))
}

func messageFTSMatchTerms(terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	if len(terms) > 8 {
		terms = terms[:8]
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		runes := []rune(term)
		if len(runes) > 64 {
			term = string(runes[:64])
		}
		parts = append(parts, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " OR ")
}

func partitionSearchTerms(query string) (long, short []string) {
	terms := strings.Fields(strings.TrimSpace(query))
	if len(terms) > 8 {
		terms = terms[:8]
	}
	for _, term := range terms {
		if utf8.RuneCountInString(term) >= ftsTrigramMinRunes {
			long = append(long, term)
		} else if term != "" {
			short = append(short, term)
		}
	}
	return long, short
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func likeContainsArg(term string) string {
	return "%" + escapeLike(strings.ToLower(term)) + "%"
}

func clipSearchSnippet(text, query string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	const max = messageapp.MaxSearchSnippetRunes
	start := 0
	hay := strings.ToLower(text)
	for _, term := range strings.Fields(query) {
		if term == "" {
			continue
		}
		at := strings.Index(hay, strings.ToLower(term))
		if at < 0 {
			continue
		}
		start = utf8.RuneCountInString(text[:at])
		break
	}
	if start < 0 {
		start = 0
	}
	if start > 16 {
		start -= 16
	} else {
		start = 0
	}
	if start > len(runes) {
		start = 0
	}
	end := start + max
	if end > len(runes) {
		end = len(runes)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	if utf8.RuneCountInString(out) > max {
		out = string([]rune(out)[:max])
	}
	return out
}

type messageSearchRow struct {
	hit  messageapp.SearchHit
	text string
}

func (s *Store) SearchMessages(ctx context.Context, q messageapp.SearchQuery) ([]messageapp.SearchHit, error) {
	if strings.TrimSpace(q.Query) == "" {
		return []messageapp.SearchHit{}, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = messageapp.DefaultSearchLimit
	}
	if limit > messageapp.MaxSearchLimit {
		limit = messageapp.MaxSearchLimit
	}
	fetch := limit * 4
	if fetch < 32 {
		fetch = 32
	}
	if fetch > 96 {
		fetch = 96
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rowsBuf, err := queryMessageSearch(ctx, tx, q, fetch, true)
	if err != nil || len(rowsBuf) == 0 {
		fallback, likeErr := queryMessageSearch(ctx, tx, q, fetch, false)
		if likeErr == nil {
			rowsBuf, err = fallback, nil
		} else if err != nil {
			return []messageapp.SearchHit{}, nil
		}
	}
	if err != nil {
		return []messageapp.SearchHit{}, nil
	}
	return collapseMessageSearchHits(rowsBuf, q.Query, limit), nil
}

func queryMessageSearch(ctx context.Context, tx *sql.Tx, q messageapp.SearchQuery, fetch int, useMatch bool) ([]messageSearchRow, error) {
	long, short := partitionSearchTerms(q.Query)
	terms := append(append([]string{}, long...), short...)
	if len(terms) == 0 {
		return nil, nil
	}
	ors := make([]string, 0, 1+len(terms))
	args := make([]any, 0, 8)
	if useMatch {
		if match := messageFTSMatchTerms(long); match != "" {
			ors = append(ors, "message_fts MATCH ?")
			args = append(args, match)
		}
		for _, term := range short {
			ors = append(ors, `LOWER(f.text) LIKE ? ESCAPE '\'`)
			args = append(args, likeContainsArg(term))
		}
	}
	if !useMatch || len(ors) == 0 {
		ors = ors[:0]
		args = args[:0]
		for _, term := range terms {
			ors = append(ors, `LOWER(f.text) LIKE ? ESCAPE '\'`)
			args = append(args, likeContainsArg(term))
		}
	}
	args = append(args, q.ProjectID, q.ProjectID, q.SessionID, q.SessionID, fetch)
	rows, err := tx.QueryContext(ctx, `
SELECT f.session_id, f.message_id, f.role, CAST(f.sequence AS INTEGER), f.text, s.title
FROM message_fts f
JOIN sessions s ON s.id = f.session_id
WHERE (`+strings.Join(ors, " OR ")+`)
  AND (? = '' OR s.project_id = ?)
  AND (? = '' OR f.session_id = ?)
ORDER BY CAST(f.sequence AS INTEGER) DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rowsBuf []messageSearchRow
	for rows.Next() {
		var item messageSearchRow
		if err := rows.Scan(&item.hit.SessionID, &item.hit.MessageID, &item.hit.Role, &item.hit.Sequence, &item.text, &item.hit.SessionTitle); err != nil {
			return nil, err
		}
		rowsBuf = append(rowsBuf, item)
	}
	return rowsBuf, rows.Err()
}

func collapseMessageSearchHits(rowsBuf []messageSearchRow, query string, limit int) []messageapp.SearchHit {
	terms := strings.Fields(strings.TrimSpace(query))
	texts := map[string][]string{}
	for _, item := range rowsBuf {
		texts[item.hit.SessionID] = append(texts[item.hit.SessionID], item.text)
	}
	covers := func(sessionID string) bool {
		blob := strings.ToLower(strings.Join(texts[sessionID], "\n"))
		for _, term := range terms {
			if !strings.Contains(blob, strings.ToLower(term)) {
				return false
			}
		}
		return true
	}
	out := make([]messageapp.SearchHit, 0, limit)
	seen := map[string]bool{}
	for _, item := range rowsBuf {
		if seen[item.hit.SessionID] || !covers(item.hit.SessionID) {
			continue
		}
		item.hit.Snippet = clipSearchSnippet(item.text, query)
		if item.hit.Snippet == "" {
			continue
		}
		seen[item.hit.SessionID] = true
		out = append(out, item.hit)
		if len(out) >= limit {
			break
		}
	}
	return out
}
