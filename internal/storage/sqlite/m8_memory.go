// M8 slice 1 storage (T-8.1.x): memory_candidates / memory_facts /
// memory_source_leaves / memory_recall_traces on the agent-runtime
// single-writer transaction. Audit events ride the shared m7 ledger
// inside the same tx; no second audit ledger exists (migration 0061).
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactMemory runs an m8app slice-1 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactMemory(ctx context.Context, fn func(m8app.MemoryTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		mtx, ok := tx.(m8app.MemoryTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.MemoryTx")
		}
		return fn(mtx)
	})
}

const m8candColumns = `candidate_id,subject_id,payload,payload_digest,inferred,trust,state,confirm_token,expires_at,created_at,confirmed_at`

func scanCandidate(s interface{ Scan(...any) error }) (m8core.MemoryCandidate, error) {
	var c m8core.MemoryCandidate
	var inferred int64
	var token, confirmed *string
	if err := s.Scan(&c.CandidateID, &c.SubjectID, &c.Payload, &c.PayloadDigest,
		&inferred, &c.Trust, &c.State, &token, &c.ExpiresAt, &c.CreatedAt, &confirmed); err != nil {
		return c, err
	}
	c.Inferred = inferred == 1
	if token != nil {
		c.ConfirmToken = *token
	}
	if confirmed != nil {
		c.ConfirmedAt = *confirmed
	}
	return c, nil
}

func (t *agentRuntimeTx) PutCandidate(c m8core.MemoryCandidate) error {
	var inferred int64
	if c.Inferred {
		inferred = 1
	}
	var token any
	if c.ConfirmToken != "" {
		token = c.ConfirmToken
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO memory_candidates
		(candidate_id,subject_id,payload,payload_digest,inferred,trust,state,confirm_token,expires_at,created_at,confirmed_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		c.CandidateID, c.SubjectID, c.Payload, c.PayloadDigest, inferred, c.Trust,
		c.State, token, c.ExpiresAt, c.CreatedAt, nil)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetCandidate(id string) (m8core.MemoryCandidate, error) {
	c, err := scanCandidate(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8candColumns+` FROM memory_candidates WHERE candidate_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, m8core.ErrNotFound
	}
	return c, t.fail(err)
}

// TransitionCandidate is the guarded pending -> terminal CAS; it fails when
// the row is not in the expected from-state (single-use token semantics).
func (t *agentRuntimeTx) TransitionCandidate(id, from, to, confirmedAt string) error {
	if !m8core.CandTransitionAllowed(from, to) {
		return m8core.ErrNotFound
	}
	var confirmed any
	if confirmedAt != "" {
		confirmed = confirmedAt
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE memory_candidates SET state=?, confirmed_at=COALESCE(?, confirmed_at) WHERE candidate_id=? AND state=?`,
		to, confirmed, id, from)
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

func (t *agentRuntimeTx) PutFact(f m8core.MemoryFact) error {
	var superseded any
	if f.SupersededBy != "" {
		superseded = f.SupersededBy
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO memory_facts
		(fact_id,scope_id,version,sensitivity,state,superseded_by,deleted_at,created_at)
		VALUES(?,?,?,?,?,?,NULL,?)`,
		f.FactID, f.ScopeID, f.Version, f.Sensitivity, f.State, superseded, f.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) PutSourceLeaves(leaves []m8core.SourceLeaf) error {
	for _, l := range leaves {
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO memory_source_leaves
			(id,fact_id,fact_version,json_pointer,evidence_ref,digest,created_at)
			VALUES(?,?,?,?,?,?,?)`,
			l.ID, l.FactID, l.FactVersion, l.JSONPointer, l.EvidenceRef, l.Digest, l.CreatedAt); err != nil {
			return t.fail(err)
		}
	}
	return nil
}

func (t *agentRuntimeTx) PutRecallTrace(tr m8core.RecallTrace) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO memory_recall_traces
		(id,query_digest,hits_json,reasons_json,policy_redactions_json,created_at)
		VALUES(?,?,?,?,?,?)`,
		tr.ID, tr.QueryDigest, tr.HitsJSON, tr.ReasonsJSON, tr.RedactionsJSON, tr.CreatedAt)
	return t.fail(err)
}

const m8factColumns = `fact_id,scope_id,version,sensitivity,state,superseded_by,deleted_at,created_at`
const m8leafColumns = `id,fact_id,fact_version,json_pointer,evidence_ref,digest,created_at`

// ListActiveFactsWithLeaves answers the active fact versions of one scope
// with their leaf bindings (the recall read snapshot).
func (t *agentRuntimeTx) ListActiveFactsWithLeaves(scopeID string) ([]m8core.MemoryFact, map[string][]m8core.SourceLeaf, error) {
	frows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m8factColumns+` FROM memory_facts WHERE scope_id=? AND state='active' ORDER BY fact_id, version`, scopeID)
	if err != nil {
		return nil, nil, t.fail(err)
	}
	defer frows.Close()
	facts := []m8core.MemoryFact{}
	for frows.Next() {
		var f m8core.MemoryFact
		var superseded, deleted *string
		if err := frows.Scan(&f.FactID, &f.ScopeID, &f.Version, &f.Sensitivity, &f.State,
			&superseded, &deleted, &f.CreatedAt); err != nil {
			return nil, nil, t.fail(err)
		}
		if superseded != nil {
			f.SupersededBy = *superseded
		}
		facts = append(facts, f)
	}
	if err := frows.Err(); err != nil {
		return nil, nil, t.fail(err)
	}
	lrows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m8leafColumns+` FROM memory_source_leaves WHERE fact_id IN (SELECT fact_id FROM memory_facts WHERE scope_id=? AND state='active') ORDER BY fact_id, fact_version, json_pointer`, scopeID)
	if err != nil {
		return nil, nil, t.fail(err)
	}
	defer lrows.Close()
	leaves := map[string][]m8core.SourceLeaf{}
	for lrows.Next() {
		var l m8core.SourceLeaf
		if err := lrows.Scan(&l.ID, &l.FactID, &l.FactVersion, &l.JSONPointer, &l.EvidenceRef, &l.Digest, &l.CreatedAt); err != nil {
			return nil, nil, t.fail(err)
		}
		leaves[l.FactID] = append(leaves[l.FactID], l)
	}
	return facts, leaves, t.fail(lrows.Err())
}
