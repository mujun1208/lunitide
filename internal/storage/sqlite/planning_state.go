package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/lunitide/lunitide/internal/planningapp"
)

func (s *Store) transitionPlanTx(ctx context.Context, tx *sql.Tx, id string, target planning.PlanStatus) error {
	var current planning.PlanStatus
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT status,version FROM plans WHERE id=?`, id).Scan(&current, &version)
	if err == sql.ErrNoRows {
		return planningapp.ErrPlanNotFound
	}
	if err != nil {
		return err
	}
	if current == target {
		return nil
	}
	if !(planning.Plan{Status: current}).CanTransitionTo(target) {
		return planningapp.ErrInvalidTransition
	}
	if target == planning.PlanStatusCompleted {
		var total, unfinished int
		if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(CASE WHEN status!='completed' THEN 1 ELSE 0 END),0) FROM plan_nodes WHERE plan_id=?`, id).Scan(&total, &unfinished); err != nil {
			return err
		}
		if total == 0 || unfinished != 0 {
			return planningapp.ErrInvalidTransition
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE plans SET status=?,version=version+1,updated_at=? WHERE id=? AND status=? AND version=?`, target, formatTime(time.Now().UTC()), id, current, version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return planningapp.ErrInvalidTransition
	}
	return nil
}

func (s *Store) transitionNodeTx(ctx context.Context, tx *sql.Tx, id string, target planning.NodeStatus) error {
	var planID string
	var parent sql.NullString
	var current planning.NodeStatus
	err := tx.QueryRowContext(ctx, `SELECT plan_id,parent_node_id,status FROM plan_nodes WHERE id=?`, id).Scan(&planID, &parent, &current)
	if err == sql.ErrNoRows {
		return planningapp.ErrNodeNotFound
	}
	if err != nil {
		return err
	}
	if current == target {
		return nil
	}
	// Readiness and dispatch are a single transaction. A dependency may not
	// become unfinished between these checks and the running transition.
	startPending := current == planning.NodeStatusPending && target == planning.NodeStatusRunning
	if !(planning.Node{Status: current}).CanTransitionTo(target) && !startPending {
		return planningapp.ErrInvalidTransition
	}
	if target == planning.NodeStatusCompleted {
		var unverified int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_plan_runs r LEFT JOIN plan_run_executions x ON x.plan_run_id=r.id WHERE r.node_id=? AND (r.status!='succeeded' OR x.status IS NULL OR x.status!='succeeded')`, id).Scan(&unverified); err != nil {
			return err
		}
		if unverified > 0 {
			return planningapp.ErrInvalidTransition
		}
	}
	var planStatus planning.PlanStatus
	if err = tx.QueryRowContext(ctx, `SELECT status FROM plans WHERE id=?`, planID).Scan(&planStatus); err != nil {
		return err
	}
	if target == planning.NodeStatusRunning || target == planning.NodeStatusReady {
		if planStatus != planning.PlanStatusActive {
			return planningapp.ErrPlanNotActive
		}
		if parent.Valid {
			var parentPlan string
			var status planning.NodeStatus
			if err = tx.QueryRowContext(ctx, `SELECT plan_id,status FROM plan_nodes WHERE id=?`, parent.String).Scan(&parentPlan, &status); err == sql.ErrNoRows {
				return planningapp.ErrDependencyNotMet
			}
			if err != nil {
				return err
			}
			if parentPlan != planID || status != planning.NodeStatusCompleted {
				return planningapp.ErrDependencyNotMet
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE plan_nodes SET status=?,updated_at=? WHERE id=? AND status=?`, target, formatTime(time.Now().UTC()), id, current)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return planningapp.ErrInvalidTransition
	}
	if target == planning.NodeStatusFailed || target == planning.NodeStatusCompleted || target == planning.NodeStatusCancelled {
		var total, complete, failed, cancelled int
		if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(status='completed'),0),COALESCE(sum(status='failed'),0),COALESCE(sum(status='cancelled'),0) FROM plan_nodes WHERE plan_id=?`, planID).Scan(&total, &complete, &failed, &cancelled); err != nil {
			return err
		}
		var next planning.PlanStatus
		if !planStatus.IsTerminal() {
			switch {
			case failed > 0:
				next = planning.PlanStatusFailed
			case total > 0 && complete == total && planStatus == planning.PlanStatusActive:
				next = planning.PlanStatusCompleted
			case total > 0 && complete+cancelled == total:
				next = planning.PlanStatusCancelled
			}
		}
		if next != "" {
			if err = s.transitionPlanTx(ctx, tx, planID, next); err != nil {
				return err
			}
			return s.appendAuditTx(ctx, tx, "plan.status_updated", planID, "engine", map[string]any{"status": next, "source": "node-aggregation"})
		}
	}
	return nil
}
