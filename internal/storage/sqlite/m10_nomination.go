// M10 nomination storage (migration 0071): memory_nominations rows on the
// agent-runtime single-writer transaction. Nomination writes never touch the
// 0061 memory tables; settlement flows through TransitionNomination only.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactNomination runs an M10 nomination use case on the shared
// single-writer transaction.
func (r *AgentRuntimeRepository) TransactNomination(ctx context.Context, fn func(m8app.NominationTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		ntx, ok := tx.(m8app.NominationTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.NominationTx")
		}
		return fn(ntx)
	})
}

const m10nomColumns = `nomination_id,candidate_id,nominator,reason,source_session_id,state,decided_at,created_at`

func scanNomination(s interface{ Scan(...any) error }) (m8core.Nomination, error) {
	var n m8core.Nomination
	var source, decided *string
	if err := s.Scan(&n.NominationID, &n.CandidateID, &n.Nominator, &n.Reason,
		&source, &n.State, &decided, &n.CreatedAt); err != nil {
		return n, err
	}
	if source != nil {
		n.SourceSessionID = *source
	}
	if decided != nil {
		n.DecidedAt = *decided
	}
	return n, nil
}

func (t *agentRuntimeTx) PutNomination(n m8core.Nomination) error {
	var source any
	if n.SourceSessionID != "" {
		source = n.SourceSessionID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO memory_nominations
		(nomination_id,candidate_id,nominator,reason,source_session_id,state,decided_at,created_at)
		VALUES(?,?,?,?,?,?,NULL,?)`,
		n.NominationID, n.CandidateID, n.Nominator, n.Reason, source, n.State, n.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetNomination(id string) (m8core.Nomination, error) {
	n, err := scanNomination(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m10nomColumns+` FROM memory_nominations WHERE nomination_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return n, m8core.ErrNotFound
	}
	return n, t.fail(err)
}

func (t *agentRuntimeTx) GetNominationByCandidate(candidateID string) (m8core.Nomination, error) {
	n, err := scanNomination(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m10nomColumns+` FROM memory_nominations WHERE candidate_id=?`, candidateID))
	if errors.Is(err, sql.ErrNoRows) {
		return n, m8core.ErrNotFound
	}
	return n, t.fail(err)
}

// ListNominationsByState answers nominations of one state, newest first
// (rides idx_nom_state).
func (t *agentRuntimeTx) ListNominationsByState(state string, limit int) ([]m8core.Nomination, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m10nomColumns+` FROM memory_nominations WHERE state=? ORDER BY created_at DESC, nomination_id DESC LIMIT ?`,
		state, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8core.Nomination{}
	for rows.Next() {
		n, err := scanNomination(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, n)
	}
	return out, t.fail(rows.Err())
}

// ListNominationsWithCandidates joins each nomination with its candidate
// (UNIQUE(candidate_id) keeps the two slices index-aligned, newest first).
func (t *agentRuntimeTx) ListNominationsWithCandidates(state string, limit int) ([]m8core.Nomination, []m8core.MemoryCandidate, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+joinNomCols(`n`)+`, `+joinCandCols(`c`)+`
		FROM memory_nominations n JOIN memory_candidates c ON c.candidate_id = n.candidate_id
		WHERE n.state=? ORDER BY n.created_at DESC, n.nomination_id DESC LIMIT ?`,
		state, limit)
	if err != nil {
		return nil, nil, t.fail(err)
	}
	defer rows.Close()
	noms := []m8core.Nomination{}
	cands := []m8core.MemoryCandidate{}
	for rows.Next() {
		var n m8core.Nomination
		var source, decided *string
		var c m8core.MemoryCandidate
		var inferred int64
		var token, confirmed *string
		if err := rows.Scan(&n.NominationID, &n.CandidateID, &n.Nominator, &n.Reason,
			&source, &n.State, &decided, &n.CreatedAt,
			&c.CandidateID, &c.SubjectID, &c.Payload, &c.PayloadDigest,
			&inferred, &c.Trust, &c.State, &token, &c.ExpiresAt, &c.CreatedAt, &confirmed); err != nil {
			return nil, nil, t.fail(err)
		}
		if source != nil {
			n.SourceSessionID = *source
		}
		if decided != nil {
			n.DecidedAt = *decided
		}
		c.Inferred = inferred == 1
		if token != nil {
			c.ConfirmToken = *token
		}
		if confirmed != nil {
			c.ConfirmedAt = *confirmed
		}
		noms = append(noms, n)
		cands = append(cands, c)
	}
	return noms, cands, t.fail(rows.Err())
}

// TransitionNomination is the guarded nominated -> terminal CAS; it fails
// when the row is not in the expected from-state.
func (t *agentRuntimeTx) TransitionNomination(id, from, to, decidedAt string) error {
	if !m8core.NomTransitionAllowed(from, to) {
		return m8core.ErrNotFound
	}
	var decided any
	if decidedAt != "" {
		decided = decidedAt
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE memory_nominations SET state=?, decided_at=COALESCE(?, decided_at) WHERE nomination_id=? AND state=?`,
		to, decided, id, from)
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

func joinNomCols(alias string) string {
	return alias + `.nomination_id,` + alias + `.candidate_id,` + alias + `.nominator,` + alias +
		`.reason,` + alias + `.source_session_id,` + alias + `.state,` + alias + `.decided_at,` + alias + `.created_at`
}

func joinCandCols(alias string) string {
	return alias + `.candidate_id,` + alias + `.subject_id,` + alias + `.payload,` + alias + `.payload_digest,` +
		alias + `.inferred,` + alias + `.trust,` + alias + `.state,` + alias + `.confirm_token,` + alias +
		`.expires_at,` + alias + `.created_at,` + alias + `.confirmed_at`
}
