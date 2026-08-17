// M10 scenario-card storage (migration 0072): expert_scenario_cards on the
// agent-runtime single-writer transaction. Card bodies never UPDATE except
// the soft active->archived archive.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactScenario runs an M10 scenario-card use case on the shared
// single-writer transaction.
func (r *AgentRuntimeRepository) TransactScenario(ctx context.Context, fn func(m8app.ScenarioTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		stx, ok := tx.(m8app.ScenarioTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.ScenarioTx")
		}
		return fn(stx)
	})
}

const m10scenarioColumns = `scenario_card_id,expert_id,title,summary,phase_key,scenario_json,scenario_digest,state,created_at,updated_at`

func scanScenarioCard(s interface{ Scan(...any) error }) (m8core.ScenarioCard, error) {
	var c m8core.ScenarioCard
	if err := s.Scan(&c.ScenarioCardID, &c.ExpertID, &c.Title, &c.Summary,
		&c.PhaseKey, &c.ScenarioJSON, &c.ScenarioDigest, &c.State, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return c, err
	}
	return c, nil
}

func (t *agentRuntimeTx) PutScenarioCard(c m8core.ScenarioCard) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO expert_scenario_cards
		(scenario_card_id,expert_id,title,summary,phase_key,scenario_json,scenario_digest,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.ScenarioCardID, c.ExpertID, c.Title, c.Summary, c.PhaseKey,
		c.ScenarioJSON, c.ScenarioDigest, c.State, c.CreatedAt, c.UpdatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetScenarioCard(id string) (m8core.ScenarioCard, error) {
	c, err := scanScenarioCard(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m10scenarioColumns+` FROM expert_scenario_cards WHERE scenario_card_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, m8core.ErrNotFound
	}
	return c, t.fail(err)
}

func (t *agentRuntimeTx) GetScenarioCardByTitle(expertID, title string) (m8core.ScenarioCard, bool, error) {
	c, err := scanScenarioCard(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m10scenarioColumns+` FROM expert_scenario_cards WHERE expert_id=? AND title=?`, expertID, title))
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, t.fail(err)
	}
	return c, true, nil
}

// ListScenarioCards answers cards of one expert, newest first; empty state
// lists both active and archived.
func (t *agentRuntimeTx) ListScenarioCards(expertID, state string) ([]m8core.ScenarioCard, error) {
	query := `SELECT ` + m10scenarioColumns + ` FROM expert_scenario_cards WHERE expert_id=?`
	args := []any{expertID}
	if state != "" {
		query += ` AND state=?`
		args = append(args, state)
	}
	query += ` ORDER BY created_at DESC, scenario_card_id DESC`
	rows, err := t.tx.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8core.ScenarioCard{}
	for rows.Next() {
		c, err := scanScenarioCard(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, c)
	}
	return out, t.fail(rows.Err())
}

// ArchiveScenarioCard is the guarded active->archived soft delete.
func (t *agentRuntimeTx) ArchiveScenarioCard(id, updatedAt string) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE expert_scenario_cards SET state='archived', updated_at=? WHERE scenario_card_id=? AND state='active'`,
		updatedAt, id)
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
