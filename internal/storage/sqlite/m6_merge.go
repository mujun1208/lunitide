// T-6.4.x storage: root-writer merge intents and the transactional
// outbox (0046). Intents update under optimistic concurrency (version
// matched and bumped; zero rows answers ErrVersionConflict or
// ErrNotFound); outbox rows append-only until the publisher marks them,
// and prune touches published rows only.
package sqlite

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// --- merge intents ----------------------------------------------------------

const mergeIntentColumns = `id, root_id, child_id, sequence, expected_head, current_head,
	patch_digest, tests_ref, state, version, created_at, updated_at`

func scanM6MergeIntent(scan func(dest ...any) error) (m6supply.MergeIntent, error) {
	var m m6supply.MergeIntent
	var currentHead sql.NullString
	var created, updated string
	if err := scan(&m.ID, &m.RootID, &m.ChildID, &m.Sequence, &m.ExpectedHead, &currentHead,
		&m.PatchDigest, &m.TestsRef, &m.State, &m.Version, &created, &updated); err != nil {
		return m, err
	}
	m.CurrentHead = currentHead.String
	var err error
	if m.CreatedAt, err = parseRFC(created); err != nil {
		return m, err
	}
	if m.UpdatedAt, err = parseRFC(updated); err != nil {
		return m, err
	}
	return m, nil
}

func (t *agentRuntimeTx) GetM6MergeIntent(id string) (m6supply.MergeIntent, error) {
	m, err := scanM6MergeIntent(func(dest ...any) error {
		return t.tx.QueryRowContext(t.ctx,
			"SELECT "+mergeIntentColumns+" FROM m6_merge_intent WHERE id = ?", id).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return m, m6supply.ErrNotFound
	}
	if err != nil {
		return m, t.fail(err)
	}
	return m, nil
}

func (t *agentRuntimeTx) GetM6MergeIntentBySequence(rootID string, sequence int64) (m6supply.MergeIntent, error) {
	m, err := scanM6MergeIntent(func(dest ...any) error {
		return t.tx.QueryRowContext(t.ctx,
			"SELECT "+mergeIntentColumns+" FROM m6_merge_intent WHERE root_id = ? AND sequence = ?",
			rootID, sequence).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return m, m6supply.ErrNotFound
	}
	if err != nil {
		return m, t.fail(err)
	}
	return m, nil
}

func (t *agentRuntimeTx) PutM6MergeIntent(m m6supply.MergeIntent) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_merge_intent
		(id, root_id, child_id, sequence, expected_head, current_head, patch_digest, tests_ref, state, version, created_at, updated_at)
		VALUES(?,?,?,?,?,NULL,?,?,?,1,?,?)`,
		m.ID, m.RootID, m.ChildID, m.Sequence, m.ExpectedHead, m.PatchDigest, m.TestsRef,
		m.State, rfc(m.CreatedAt), rfc(m.UpdatedAt))
	if err != nil && isUniqueViolation(err) {
		return m6supply.ErrSequenceTaken
	}
	return t.fail(err)
}

// isUniqueViolation recognises SQLite UNIQUE constraint failures without
// importing the driver error type (the stdlib message carries the
// constraint name).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func (t *agentRuntimeTx) UpdateM6MergeIntentState(id string, expectedVersion int64, to string, currentHead *string, at time.Time) (m6supply.MergeIntent, error) {
	var head any
	if currentHead != nil {
		head = *currentHead
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_merge_intent
		SET state = ?, current_head = COALESCE(?, current_head), version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, to, head, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.MergeIntent{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.MergeIntent{}, t.conflictOrMissing("m6_merge_intent", id)
	}
	return t.GetM6MergeIntent(id)
}

// UpdateM6MergeIntentRebased lands the serial-rebase verdict (MRG-001):
// the intent moves to rebase_required with the rebase target pinned as
// its new expected head — the requeued walk CASes against that head.
func (t *agentRuntimeTx) UpdateM6MergeIntentRebased(id string, expectedVersion int64, newExpectedHead string, at time.Time) (m6supply.MergeIntent, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_merge_intent
		SET state = 'rebase_required', expected_head = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, newExpectedHead, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.MergeIntent{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.MergeIntent{}, t.conflictOrMissing("m6_merge_intent", id)
	}
	return t.GetM6MergeIntent(id)
}

func (t *agentRuntimeTx) ListM6MergeIntentsByRoot(rootID string) ([]m6supply.MergeIntent, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		"SELECT "+mergeIntentColumns+" FROM m6_merge_intent WHERE root_id = ? ORDER BY sequence", rootID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.MergeIntent
	for rows.Next() {
		m, err := scanM6MergeIntent(rows.Scan)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, m)
	}
	return out, t.fail(rows.Err())
}

// --- outbox -----------------------------------------------------------------

const outboxColumns = `id, aggregate_type, aggregate_id, event_type, payload, published, published_at, created_at`

func scanM6Outbox(scan func(dest ...any) error) (m6supply.OutboxEvent, error) {
	var e m6supply.OutboxEvent
	var publishedAt sql.NullString
	var created string
	if err := scan(&e.ID, &e.AggregateType, &e.AggregateID, &e.EventType, &e.Payload,
		&e.Published, &publishedAt, &created); err != nil {
		return e, err
	}
	if publishedAt.Valid && publishedAt.String != "" {
		at, err := parseRFC(publishedAt.String)
		if err != nil {
			return e, err
		}
		e.PublishedAt = &at
	}
	var err error
	if e.CreatedAt, err = parseRFC(created); err != nil {
		return e, err
	}
	return e, nil
}

func (t *agentRuntimeTx) AppendM6Outbox(e m6supply.OutboxEvent) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_outbox
		(id, aggregate_type, aggregate_id, event_type, payload, published, published_at, created_at)
		VALUES(?,?,?,?,?,?,NULL,?)`,
		e.ID, e.AggregateType, e.AggregateID, e.EventType, e.Payload, e.Published, rfc(e.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListUnpublishedM6Outbox(limit int) ([]m6supply.OutboxEvent, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		"SELECT "+outboxColumns+" FROM m6_outbox WHERE published = 0 ORDER BY created_at LIMIT ?", limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.OutboxEvent
	for rows.Next() {
		e, err := scanM6Outbox(rows.Scan)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, e)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) CountUnpublishedM6Outbox() (int64, error) {
	var n int64
	err := t.tx.QueryRowContext(t.ctx,
		"SELECT COUNT(*) FROM m6_outbox WHERE published = 0").Scan(&n)
	return n, t.fail(err)
}

func (t *agentRuntimeTx) MarkM6OutboxPublished(ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	// placeholder order mirrors the SQL: published_at first, then the ids
	args := make([]any, 0, len(ids)+1)
	args = append(args, rfc(at))
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := t.tx.ExecContext(t.ctx,
		"UPDATE m6_outbox SET published = 1, published_at = ? WHERE id IN ("+placeholders+") AND published = 0",
		args...)
	return t.fail(err)
}

func (t *agentRuntimeTx) PruneM6Outbox(publishedBefore time.Time) (int64, error) {
	res, err := t.tx.ExecContext(t.ctx,
		"DELETE FROM m6_outbox WHERE published = 1 AND published_at IS NOT NULL AND published_at < ?",
		rfc(publishedBefore))
	if err != nil {
		return 0, t.fail(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
