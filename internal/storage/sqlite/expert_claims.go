package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

func validClaimULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

func validClaimTaskKey(v string) bool {
	n := utf8.RuneCountInString(v)
	return n >= 1 && n <= 128 && !strings.ContainsAny(v, "\n\r\t")
}

func (s *Store) TryClaimExpertTask(ctx context.Context, threadID, taskKey, expertID string) (ownerID string, created bool, err error) {
	if !validClaimULID(threadID) || !validClaimULID(expertID) || !validClaimTaskKey(taskKey) {
		return "", false, fmt.Errorf("expert task claim invalid")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO expert_task_claims(thread_id, task_key, expert_id, claimed_at) VALUES(?,?,?,?)`, threadID, taskKey, expertID, now)
	if err != nil {
		return "", false, fmt.Errorf("insert expert task claim: %w", err)
	}
	n, _ := res.RowsAffected()
	var owner string
	if err := s.db.QueryRowContext(ctx, `SELECT expert_id FROM expert_task_claims WHERE thread_id=? AND task_key=?`, threadID, taskKey).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, fmt.Errorf("expert task claim missing")
		}
		return "", false, fmt.Errorf("read expert task claim: %w", err)
	}
	return owner, n > 0, nil
}
