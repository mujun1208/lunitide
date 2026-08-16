// M8 slice-3 storage (T-8.3.x/T-8.5.x): handoffs / memory_tombstones /
// device_replicas / sync_conflicts on the agent-runtime single-writer
// transaction. The tombstone read-face guard hides memory facts inside the
// same transaction that persists the tombstone row.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactHandoff runs an m8app slice-3 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactHandoff(ctx context.Context, fn func(m8app.HandoffTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		htx, ok := tx.(m8app.HandoffTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.HandoffTx")
		}
		return fn(htx)
	})
}

const m8handoffColumns = `id,sender,receiver,manifest,redaction_log,state,expires_at,created_at`

func scanHandoff(s interface{ Scan(...any) error }) (m8core.Handoff, error) {
	var h m8core.Handoff
	err := s.Scan(&h.ID, &h.Sender, &h.Receiver, &h.Manifest, &h.RedactionLog,
		&h.State, &h.ExpiresAt, &h.CreatedAt)
	return h, err
}

func (t *agentRuntimeTx) PutHandoff(h m8core.Handoff) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO handoffs
		(id,sender,receiver,manifest,redaction_log,state,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		h.ID, h.Sender, h.Receiver, h.Manifest, h.RedactionLog,
		h.State, h.ExpiresAt, h.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetHandoff(id string) (m8core.Handoff, error) {
	h, err := scanHandoff(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8handoffColumns+` FROM handoffs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return h, m8core.ErrNotFound
	}
	return h, t.fail(err)
}

// TransitionHandoff is the guarded sent -> terminal CAS.
func (t *agentRuntimeTx) TransitionHandoff(id, from, to string) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE handoffs SET state=? WHERE id=? AND state=?`, to, id, from)
	if err != nil {
		return t.fail(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return t.fail(err)
	}
	if n == 0 {
		return m8core.ErrNotFound
	}
	return nil
}

const m8tombColumns = `id,root_ref,cascade_cursor,ack_set,proof_digest,state,created_at,completed_at`

func scanTombstone(s interface{ Scan(...any) error }) (m8core.Tombstone, error) {
	var t m8core.Tombstone
	var proof, completed *string
	err := s.Scan(&t.ID, &t.RootRef, &t.CascadeCursor, &t.AckSet, &proof,
		&t.State, &t.CreatedAt, &completed)
	if proof != nil {
		t.ProofDigest = *proof
	}
	if completed != nil {
		t.CompletedAt = *completed
	}
	return t, err
}

func (t *agentRuntimeTx) GetTombstoneByRoot(rootRef string) (m8core.Tombstone, bool, error) {
	row := t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8tombColumns+` FROM memory_tombstones WHERE root_ref=? ORDER BY created_at DESC LIMIT 1`, rootRef)
	tb, err := scanTombstone(row)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.Tombstone{}, false, nil
	}
	if err != nil {
		return m8core.Tombstone{}, false, t.fail(err)
	}
	return tb, true, nil
}

func (t *agentRuntimeTx) PutTombstone(tb m8core.Tombstone) error {
	var proof, completed any
	if tb.ProofDigest != "" {
		proof = tb.ProofDigest
	}
	if tb.CompletedAt != "" {
		completed = tb.CompletedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO memory_tombstones
		(id,root_ref,cascade_cursor,ack_set,proof_digest,state,created_at,completed_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			cascade_cursor=excluded.cascade_cursor,
			ack_set=excluded.ack_set,
			proof_digest=excluded.proof_digest,
			state=excluded.state,
			completed_at=excluded.completed_at`,
		tb.ID, tb.RootRef, tb.CascadeCursor, tb.AckSet, proof,
		tb.State, tb.CreatedAt, completed)
	return t.fail(err)
}

// TombstoneFacts hides the read face immediately: the root fact (and its
// scope siblings under the same root ref) flips to tombstoned before the
// cascade starts (FR-07 读面立即隐藏先行).
func (t *agentRuntimeTx) TombstoneFacts(rootRef, scopeID string) (int64, error) {
	// rootRef is "fact:<id>"; the fact row plus every active fact bound to
	// the same scope root hides at once.
	factID := ""
	if len(rootRef) > 5 && rootRef[:5] == "fact:" {
		factID = rootRef[5:]
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE memory_facts SET state='tombstoned' WHERE (fact_id=? OR scope_id=?) AND state='active'`,
		factID, scopeID)
	if err != nil {
		return 0, t.fail(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, t.fail(err)
	}
	// The KB chunk projection and graph node binding also hide: chunks of
	// tombstoned scopes are removed from the searchable face by state, and
	// graph rows stay queryable only through tombstone-aware reads.
	if _, err := t.tx.ExecContext(t.ctx,
		`UPDATE kb_documents SET index_state='failed' WHERE collection_id IN
			(SELECT collection_id FROM kb_collections WHERE scope_id=?) AND index_state='ready'`,
		scopeID); err != nil {
		return n, t.fail(err)
	}
	return n, nil
}

// ListTombstoneProjections answers the single-engine cascade target set:
// the memory fact read face, the KB chunk projection and the graph node
// binding (the same set the propagator must ACK before compaction).
func (t *agentRuntimeTx) ListTombstoneProjections() ([]string, error) {
	return []string{"memory_facts", "kb_chunks", "graph_nodes"}, nil
}

const m8deviceColumns = `device_id,subject_id,vector_clock,last_ack,trust_state,created_at`

func scanDevice(s interface{ Scan(...any) error }) (m8core.DeviceReplica, error) {
	var d m8core.DeviceReplica
	err := s.Scan(&d.DeviceID, &d.SubjectID, &d.VectorClock, &d.LastAck,
		&d.TrustState, &d.CreatedAt)
	return d, err
}

func (t *agentRuntimeTx) PutDeviceReplica(d m8core.DeviceReplica) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO device_replicas
		(device_id,subject_id,vector_clock,last_ack,trust_state,created_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET
			vector_clock=excluded.vector_clock,
			last_ack=excluded.last_ack,
			trust_state=excluded.trust_state`,
		d.DeviceID, d.SubjectID, d.VectorClock, d.LastAck, d.TrustState, d.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetDeviceReplica(deviceID string) (m8core.DeviceReplica, error) {
	d, err := scanDevice(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8deviceColumns+` FROM device_replicas WHERE device_id=?`, deviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return d, m8core.ErrNotFound
	}
	return d, t.fail(err)
}

const m8conflictColumns = `id,json_pointer,variants,resolution,state,created_at`

func scanConflict(s interface{ Scan(...any) error }) (m8core.SyncConflict, error) {
	var c m8core.SyncConflict
	var resolution *string
	err := s.Scan(&c.ID, &c.JSONPointer, &c.Variants, &resolution, &c.State, &c.CreatedAt)
	if resolution != nil {
		c.Resolution = *resolution
	}
	return c, err
}

// ListOpenConflicts answers every open conflict-box row (the same-leaf
// concurrency oracle for sync.push).
func (t *agentRuntimeTx) ListOpenConflicts() ([]m8core.SyncConflict, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m8conflictColumns+` FROM sync_conflicts WHERE state='open' ORDER BY created_at, id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8core.SyncConflict{}
	for rows.Next() {
		c, err := scanConflict(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, c)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) PutSyncConflict(c m8core.SyncConflict) error {
	var resolution any
	if c.Resolution != "" {
		resolution = c.Resolution
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO sync_conflicts
		(id,json_pointer,variants,resolution,state,created_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET variants=excluded.variants, resolution=excluded.resolution, state=excluded.state`,
		c.ID, c.JSONPointer, c.Variants, resolution, c.State, c.CreatedAt)
	return t.fail(err)
}
