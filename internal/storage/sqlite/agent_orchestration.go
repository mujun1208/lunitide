package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/agentorchestration"
)

// AgentOrchestrationRepository persists a coordinator transaction and its
// ordered events in one SQLite writer transaction.
type AgentOrchestrationRepository struct{ db *sql.DB }

func (s *Store) AgentOrchestrationRepository() *AgentOrchestrationRepository {
	return &AgentOrchestrationRepository{db: s.db}
}

func (r *AgentOrchestrationRepository) Transact(ctx context.Context, fn func(agentorchestration.Transaction) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t := &agentOrchestrationTx{ctx: ctx, tx: tx}
	if err = fn(t); err != nil {
		return err
	}
	if t.err != nil {
		return t.err
	}
	return tx.Commit()
}

type agentOrchestrationTx struct {
	ctx context.Context
	tx  *sql.Tx
	err error
}

func (t *agentOrchestrationTx) Get(id string) (agentorchestration.AgentRun, bool) {
	if t.err != nil {
		return agentorchestration.AgentRun{}, false
	}
	r, err := scanAgentRun(t.tx.QueryRowContext(t.ctx, `SELECT id,COALESCE(parent_run_id,''),plan_id,node_id,role,todo_id,todo_title,todo_description,todo_metadata_json,status,depth,failure,created_at,updated_at,terminal_at,version FROM agent_plan_runs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return agentorchestration.AgentRun{}, false
	}
	if err != nil {
		t.err = err
		return agentorchestration.AgentRun{}, false
	}
	return r, true
}
func (t *agentOrchestrationTx) Put(r agentorchestration.AgentRun) {
	if t.err != nil {
		return
	}
	metadata, err := json.Marshal(r.Todo.Metadata)
	if err != nil {
		t.err = err
		return
	}
	var parent, terminal any
	if r.ParentRunID != "" {
		parent = r.ParentRunID
	}
	if r.TerminalAt != nil {
		terminal = r.TerminalAt.UTC().Format(time.RFC3339Nano)
	}
	_, t.err = t.tx.ExecContext(t.ctx, `INSERT INTO agent_plan_runs(id,parent_run_id,plan_id,node_id,role,todo_id,todo_title,todo_description,todo_metadata_json,status,depth,failure,created_at,updated_at,terminal_at,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET parent_run_id=excluded.parent_run_id,plan_id=excluded.plan_id,node_id=excluded.node_id,role=excluded.role,todo_id=excluded.todo_id,todo_title=excluded.todo_title,todo_description=excluded.todo_description,todo_metadata_json=excluded.todo_metadata_json,status=excluded.status,depth=excluded.depth,failure=excluded.failure,updated_at=excluded.updated_at,terminal_at=excluded.terminal_at,version=excluded.version`, r.ID, parent, r.PlanID, r.NodeID, r.Role, r.Todo.ID, r.Todo.Title, r.Todo.Description, string(metadata), r.Status, r.Depth, r.Failure, r.CreatedAt.UTC().Format(time.RFC3339Nano), r.UpdatedAt.UTC().Format(time.RFC3339Nano), terminal, r.Version)
}
func (t *agentOrchestrationTx) ListChildren(id string) []agentorchestration.AgentRun {
	return t.list(`WHERE parent_run_id=? ORDER BY created_at,id`, id)
}
func (t *agentOrchestrationTx) ListRuns() []agentorchestration.AgentRun {
	return t.list(`ORDER BY created_at,id`)
}
func (t *agentOrchestrationTx) list(suffix string, args ...any) []agentorchestration.AgentRun {
	if t.err != nil {
		return nil
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,COALESCE(parent_run_id,''),plan_id,node_id,role,todo_id,todo_title,todo_description,todo_metadata_json,status,depth,failure,created_at,updated_at,terminal_at,version FROM agent_plan_runs `+suffix, args...)
	if err != nil {
		t.err = err
		return nil
	}
	defer rows.Close()
	var out []agentorchestration.AgentRun
	for rows.Next() {
		v, e := scanAgentRun(rows)
		if e != nil {
			t.err = e
			return nil
		}
		out = append(out, v)
	}
	t.err = rows.Err()
	return out
}
func (t *agentOrchestrationTx) Append(e agentorchestration.Event) {
	if t.err == nil {
		res, err := t.tx.ExecContext(t.ctx, `INSERT INTO agent_plan_run_events(run_id,type,from_status,to_status,detail,created_at) VALUES(?,?,?,?,?,?)`, e.RunID, e.Type, e.From, e.To, e.Detail, e.At.UTC().Format(time.RFC3339Nano))
		if err == nil {
			n, x := res.LastInsertId()
			e.Sequence = uint64(n)
			err = x
		}
		t.err = err
	}
}
func (t *agentOrchestrationTx) ListEvents(id string) []agentorchestration.Event {
	if t.err != nil {
		return nil
	}
	q := `SELECT sequence,run_id,type,from_status,to_status,detail,created_at FROM agent_plan_run_events`
	var args []any
	if id != "" {
		q += ` WHERE run_id=?`
		args = append(args, id)
	}
	q += ` ORDER BY sequence`
	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		t.err = err
		return nil
	}
	defer rows.Close()
	var out []agentorchestration.Event
	for rows.Next() {
		var e agentorchestration.Event
		var at string
		if err = rows.Scan(&e.Sequence, &e.RunID, &e.Type, &e.From, &e.To, &e.Detail, &at); err != nil {
			t.err = err
			return nil
		}
		e.At, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			t.err = err
			return nil
		}
		out = append(out, e)
	}
	t.err = rows.Err()
	return out
}

func scanAgentRun(s interface{ Scan(...any) error }) (agentorchestration.AgentRun, error) {
	var r agentorchestration.AgentRun
	var metadata, created, updated string
	var terminal sql.NullString
	err := s.Scan(&r.ID, &r.ParentRunID, &r.PlanID, &r.NodeID, &r.Role, &r.Todo.ID, &r.Todo.Title, &r.Todo.Description, &metadata, &r.Status, &r.Depth, &r.Failure, &created, &updated, &terminal, &r.Version)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(metadata), &r.Todo.Metadata); err != nil {
		return r, err
	}
	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return r, err
	}
	if r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return r, err
	}
	if terminal.Valid {
		v, e := time.Parse(time.RFC3339Nano, terminal.String)
		if e != nil {
			return r, e
		}
		r.TerminalAt = &v
	}
	return r, nil
}

var _ agentorchestration.Repository = (*AgentOrchestrationRepository)(nil)
