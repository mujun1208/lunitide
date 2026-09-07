package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"github.com/lunitide/lunitide/internal/compactionapp"
)

// Only closed calendar weeks are eligible. No messages are deleted or moved.
func (s *Store) ListDueCompanionWeeks(ctx context.Context, now time.Time) ([]compactionapp.CompanionWeek, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions WHERE title IN ('月伴对话','Companion talk') AND status='active' ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var due []compactionapp.CompanionWeek
	for _, id := range ids {
		var after int64
		if err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(source_end_seq),0) FROM compaction_checkpoints WHERE session_id=? AND trigger_reason LIKE 'companion-weekly:%' AND status IN ('succeeded','superseded')`, id).Scan(&after); err != nil {
			return nil, err
		}
		var startSeq int64
		var created string
		err = s.db.QueryRowContext(ctx, `SELECT sequence,created_at FROM messages WHERE session_id=? AND sequence>? ORDER BY sequence LIMIT 1`, id, after).Scan(&startSeq, &created)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		stamp, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		start := compactionapp.WeekStart(stamp.In(now.Location()))
		end := start.AddDate(0, 0, 7)
		if now.Before(end) {
			continue
		}
		var endSeq int64
		// Stop at the first newer-week message, retaining a contiguous sequence
		// even if the local clock was adjusted during recording.
		err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(CASE WHEN julianday(created_at)>=julianday(?) THEN sequence END)-1,MAX(sequence)) FROM messages WHERE session_id=? AND sequence>=?`, formatTime(end.UTC()), id, startSeq).Scan(&endSeq)
		if err != nil {
			return nil, err
		}
		if endSeq < startSeq {
			continue
		}
		due = append(due, compactionapp.CompanionWeek{SessionID: id, StartSeq: startSeq, EndSeq: endSeq, Start: start, End: end})
	}
	return due, nil
}

func archiveQueryTerms(query string) []string {
	var terms []string
	seen := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		runes := []rune(word)
		if len(runes) < 2 {
			continue
		}
		for i := 0; i < len(runes)-1 && len(terms) < 24; i++ {
			term := string(runes[i:min(i+3, len(runes))])
			if !seen[term] {
				seen[term] = true
				terms = append(terms, term)
			}
		}
	}
	return terms
}

// Search all successful weeks, not just the recent context window. Results
// remain scoped to the caller-authorized session and bounded for voice latency.
func (s *Store) SearchCompanionArchives(ctx context.Context, sessionID, query string, limit int) ([]compactionapp.CompanionArchive, error) {
	if limit < 1 || limit > 6 {
		limit = 3
	}
	rank := "0"
	args := []any{}
	for _, term := range archiveQueryTerms(query) {
		rank += "+CASE WHEN instr(lower(human_summary),?)>0 THEN 1 ELSE 0 END"
		args = append(args, term)
	}
	args = append(args, sessionID, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id,trigger_reason,substr(human_summary,1,2400),(`+rank+`) AS relevance FROM compaction_checkpoints WHERE session_id=? AND trigger_reason LIKE 'companion-weekly:%' AND status IN ('succeeded','superseded') ORDER BY relevance DESC,source_end_seq DESC,version DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []compactionapp.CompanionArchive
	for rows.Next() {
		var a compactionapp.CompanionArchive
		var score int
		if err := rows.Scan(&a.CheckpointID, &a.Period, &a.Summary, &score); err != nil {
			return nil, err
		}
		a.Period = strings.TrimPrefix(a.Period, compactionapp.CompanionWeekReason)
		out = append(out, a)
	}
	return out, rows.Err()
}
