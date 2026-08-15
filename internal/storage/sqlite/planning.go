package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/planning"
)

// CreatePlan inserts a new plan.
func (s *Store) CreatePlan(ctx context.Context, plan planning.Plan) (planning.Plan, error) {
	if plan.ID == "" {
		var err error
		plan.ID, err = s.newULID(time.Now())
		if err != nil {
			return plan, err
		}
	}
	plan.CreatedAt = time.Now().UTC()
	plan.UpdatedAt = plan.CreatedAt
	if plan.Version == 0 {
		plan.Version = 1
	}
	if plan.Status == "" {
		plan.Status = planning.PlanStatusDraft
	}
	if plan.Description == "" {
		plan.Description = ""
	}
	if err := plan.Validate(); err != nil {
		return plan, err
	}
	err := s.execWithAudit(ctx, "plan.created", plan.ID, "engine",
		map[string]any{"projectId": plan.ProjectID, "status": plan.Status},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO plans(id, project_id, stage_id, name, description, version, status, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?)`,
				plan.ID, plan.ProjectID, plan.StageID, plan.Name, plan.Description,
				plan.Version, plan.Status, formatTime(plan.CreatedAt), formatTime(plan.UpdatedAt))
			return err
		})
	return plan, mapWriteError(err)
}

// GetPlan returns a plan by ID.
func (s *Store) GetPlan(ctx context.Context, id string) (*planning.Plan, error) {
	var p planning.Plan
	var created, updated, stageID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, stage_id, name, description, version, status, created_at, updated_at
		 FROM plans WHERE id=?`, id).Scan(
		&p.ID, &p.ProjectID, &stageID, &p.Name, &p.Description,
		&p.Version, &p.Status, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated.String)
	if err != nil {
		return nil, err
	}
	if stageID.Valid {
		p.StageID = &stageID.String
	}
	return &p, nil
}

// ListPlansByProject returns plans for a project ordered by creation time.
func (s *Store) ListPlansByProject(ctx context.Context, projectID string, limit int) ([]planning.Plan, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, stage_id, name, description, version, status, created_at, updated_at
		 FROM plans WHERE project_id=? ORDER BY created_at, id LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []planning.Plan
	for rows.Next() {
		var p planning.Plan
		var created, updated, stageID sql.NullString
		if err = rows.Scan(
			&p.ID, &p.ProjectID, &stageID, &p.Name, &p.Description,
			&p.Version, &p.Status, &created, &updated); err != nil {
			return nil, err
		}
		p.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
		if err != nil {
			return nil, err
		}
		p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated.String)
		if err != nil {
			return nil, err
		}
		if stageID.Valid {
			p.StageID = &stageID.String
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// UpdatePlanStatus updates the status of a plan.
func (s *Store) UpdatePlanStatus(ctx context.Context, id, status string) error {
	err := s.execWithAudit(ctx, "plan.status_updated", id, "engine",
		map[string]any{"status": status},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`UPDATE plans SET status=?, updated_at=? WHERE id=?`,
				status, formatTime(time.Now().UTC()), id)
			return err
		})
	return mapWriteError(err)
}

// CreateNode inserts a new plan node.
func (s *Store) CreateNode(ctx context.Context, node planning.Node) (planning.Node, error) {
	if node.ID == "" {
		var err error
		node.ID, err = s.newULID(time.Now())
		if err != nil {
			return node, err
		}
	}
	node.CreatedAt = time.Now().UTC()
	node.UpdatedAt = node.CreatedAt
	if node.Status == "" {
		node.Status = planning.NodeStatusPending
	}
	if node.RiskLevel == "" {
		node.RiskLevel = planning.RiskLow
	}
	if node.Description == "" {
		node.Description = ""
	}
	if node.WorkerRole == "" {
		node.WorkerRole = ""
	}
	if err := node.Validate(); err != nil {
		return node, err
	}
	err := s.execWithAudit(ctx, "node.created", node.ID, "engine",
		map[string]any{"planId": node.PlanID, "sequence": node.Sequence},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO plan_nodes(id, plan_id, parent_node_id, name, description, status, risk_level,
				 budget_tokens, estimate_tokens, worker_role, sequence, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				node.ID, node.PlanID, node.ParentNodeID, node.Name, node.Description,
				node.Status, node.RiskLevel, node.BudgetTokens, node.EstimateTokens,
				node.WorkerRole, node.Sequence, formatTime(node.CreatedAt), formatTime(node.UpdatedAt))
			return err
		})
	return node, mapWriteError(err)
}

// GetNode returns a plan node by ID.
func (s *Store) GetNode(ctx context.Context, id string) (*planning.Node, error) {
	var n planning.Node
	var created, updated, parentNodeID sql.NullString
	var budgetTokens, estimateTokens sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, plan_id, parent_node_id, name, description, status, risk_level,
		 budget_tokens, estimate_tokens, worker_role, sequence, created_at, updated_at
		 FROM plan_nodes WHERE id=?`, id).Scan(
		&n.ID, &n.PlanID, &parentNodeID, &n.Name, &n.Description,
		&n.Status, &n.RiskLevel, &budgetTokens, &estimateTokens,
		&n.WorkerRole, &n.Sequence, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return nil, err
	}
	n.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated.String)
	if err != nil {
		return nil, err
	}
	if parentNodeID.Valid {
		n.ParentNodeID = &parentNodeID.String
	}
	if budgetTokens.Valid {
		n.BudgetTokens = &budgetTokens.Int64
	}
	if estimateTokens.Valid {
		n.EstimateTokens = &estimateTokens.Int64
	}
	return &n, nil
}

// ListNodesByPlan returns nodes for a plan ordered by sequence.
func (s *Store) ListNodesByPlan(ctx context.Context, planID string, limit int) ([]planning.Node, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, plan_id, parent_node_id, name, description, status, risk_level,
		 budget_tokens, estimate_tokens, worker_role, sequence, created_at, updated_at
		 FROM plan_nodes WHERE plan_id=? ORDER BY sequence LIMIT ?`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []planning.Node
	for rows.Next() {
		var n planning.Node
		var created, updated, parentNodeID sql.NullString
		var budgetTokens, estimateTokens sql.NullInt64
		if err = rows.Scan(
			&n.ID, &n.PlanID, &parentNodeID, &n.Name, &n.Description,
			&n.Status, &n.RiskLevel, &budgetTokens, &estimateTokens,
			&n.WorkerRole, &n.Sequence, &created, &updated); err != nil {
			return nil, err
		}
		n.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
		if err != nil {
			return nil, err
		}
		n.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated.String)
		if err != nil {
			return nil, err
		}
		if parentNodeID.Valid {
			n.ParentNodeID = &parentNodeID.String
		}
		if budgetTokens.Valid {
			n.BudgetTokens = &budgetTokens.Int64
		}
		if estimateTokens.Valid {
			n.EstimateTokens = &estimateTokens.Int64
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// UpdateNodeStatus updates the status of a plan node.
func (s *Store) UpdateNodeStatus(ctx context.Context, id, status string) error {
	err := s.execWithAudit(ctx, "node.status_updated", id, "engine",
		map[string]any{"status": status},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`UPDATE plan_nodes SET status=?, updated_at=? WHERE id=?`,
				status, formatTime(time.Now().UTC()), id)
			return err
		})
	return mapWriteError(err)
}