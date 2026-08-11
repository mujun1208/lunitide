package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/token"
)

// UpsertTokenLedger inserts or replaces a token ledger entry.
func (s *Store) UpsertTokenLedger(ctx context.Context, entry token.LedgerEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO token_ledger(id, message_id, provider, model, tokenizer_revision, token_count, estimation_method, utf8_bytes, computed_at, subject_type, subject_id, tokenizer_id)
		 VALUES(?,?,?,?,?,?,?,?,?, 'message', ?, 'lunitide-canonical-v1')
		 ON CONFLICT(message_id, provider, model, tokenizer_revision)
		 DO UPDATE SET token_count=excluded.token_count, estimation_method=excluded.estimation_method,
		              utf8_bytes=excluded.utf8_bytes, computed_at=excluded.computed_at`,
		entry.ID, entry.MessageID, entry.Provider, entry.Model, entry.TokenizerRevision,
		entry.TokenCount, entry.EstimationMethod, entry.UTF8Bytes, formatTime(entry.ComputedAt), entry.MessageID)
	return mapWriteError(err)
}

// GetTokenLedger returns the ledger entry for the given tuple.
func (s *Store) GetTokenLedger(ctx context.Context, messageID, provider, model, tokenizerRevision string) (*token.LedgerEntry, error) {
	var e token.LedgerEntry
	var computed string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, message_id, provider, model, tokenizer_revision, token_count, estimation_method, utf8_bytes, computed_at
		 FROM token_ledger WHERE message_id=? AND provider=? AND model=? AND tokenizer_revision=?`,
		messageID, provider, model, tokenizerRevision).Scan(
		&e.ID, &e.MessageID, &e.Provider, &e.Model, &e.TokenizerRevision,
		&e.TokenCount, &e.EstimationMethod, &e.UTF8Bytes, &computed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.ComputedAt, err = time.Parse(time.RFC3339Nano, computed)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListTokenLedgerByMessage returns all ledger entries for a message.
func (s *Store) ListTokenLedgerByMessage(ctx context.Context, messageID string) ([]token.LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, message_id, provider, model, tokenizer_revision, token_count, estimation_method, utf8_bytes, computed_at
		 FROM token_ledger WHERE message_id=? ORDER BY provider, model, tokenizer_revision`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []token.LedgerEntry
	for rows.Next() {
		var e token.LedgerEntry
		var computed string
		if err = rows.Scan(&e.ID, &e.MessageID, &e.Provider, &e.Model, &e.TokenizerRevision,
			&e.TokenCount, &e.EstimationMethod, &e.UTF8Bytes, &computed); err != nil {
			return nil, err
		}
		e.ComputedAt, err = time.Parse(time.RFC3339Nano, computed)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SumTokenLedgerBySession returns the total token count for all messages in a session.
func (s *Store) SumTokenLedgerBySession(ctx context.Context, sessionID, provider, model, tokenizerRevision string) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(tl.token_count), 0) FROM token_ledger tl
		 JOIN messages m ON m.id = tl.message_id
		 WHERE m.session_id=? AND tl.provider=? AND tl.model=? AND tl.tokenizer_revision=?`,
		sessionID, provider, model, tokenizerRevision).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// SumTokenLedgerAfterSeq returns the total token count for messages in a session
// with sequence > afterSeq. Used for low-watermark verification after compaction
// (remaining uncompacted messages).
func (s *Store) SumTokenLedgerAfterSeq(ctx context.Context, sessionID, provider, model, tokenizerRevision string, afterSeq int64) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(tl.token_count), 0) FROM token_ledger tl
		 JOIN messages m ON m.id = tl.message_id
		 WHERE m.session_id=? AND m.sequence > ? AND tl.provider=? AND tl.model=?`,
		sessionID, afterSeq, provider, model).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// DeleteTokenLedgerByMessage removes all ledger entries for a message.
func (s *Store) DeleteTokenLedgerByMessage(ctx context.Context, messageID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM token_ledger WHERE message_id=?`, messageID)
	return err
}

// Ensure Store implements token.Repository.
var _ token.Repository = (*Store)(nil)