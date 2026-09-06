package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

func phasePlanID(projectID, stageID, kind string) string {
	hash := sha256.Sum256([]byte("phase-plan\x00" + projectID + "\x00" + stageID + "\x00" + kind))
	var id ulid.ULID
	copy(id[:], hash[:16])
	return id.String()
}

// ensurePhasePlan creates the bound plan, its positive-sequence root, and the
// active state atomically. Deterministic IDs also prevent cross-process twins.
func (s *Store) ensurePhasePlan(ctx context.Context, plan planning.Plan) (planning.Plan, error) {
	plan.ID = phasePlanID(plan.ProjectID, *plan.StageID, "plan")
	err := s.DoProject(ctx, func(projectTx projectapp.Tx) error {
		t := projectTx.(*txAdapter)
		p, err := t.getProject(ctx, plan.ProjectID)
		if err != nil {
			return err
		}
		if !p.CanEditMutableFields() {
			return projectapp.ErrInvalidTransition
		}
		var owner string
		if err = t.q.QueryRowContext(ctx, `SELECT project_id FROM stages WHERE id=?`, *plan.StageID).Scan(&owner); err != nil {
			return err
		}
		if owner != plan.ProjectID {
			return projectapp.ErrInvalidTransition
		}
		var found string
		err = t.q.QueryRowContext(ctx, `SELECT id FROM plans WHERE id=? AND project_id=? AND stage_id=?`, plan.ID, plan.ProjectID, *plan.StageID).Scan(&found)
		if err == nil {
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		now := time.Now().UTC()
		_, err = t.q.ExecContext(ctx, `INSERT INTO plans(id,project_id,stage_id,name,description,version,status,created_at,updated_at) VALUES(?,?,?,?,?,1,'active',?,?)`, plan.ID, plan.ProjectID, *plan.StageID, plan.Name, plan.Description, formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		rootID := phasePlanID(plan.ProjectID, *plan.StageID, "root")
		_, err = t.q.ExecContext(ctx, `INSERT INTO plan_nodes(id,plan_id,parent_node_id,name,description,status,risk_level,worker_role,sequence,created_at,updated_at) VALUES(?,?,NULL,'Root','','ready','low','planner',1,?,?)`, rootID, plan.ID, formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"projectId": plan.ProjectID, "stageId": *plan.StageID, "rootNodeId": rootID, "status": "active"})
		for _, event := range []struct{ action, id string }{{"plan.created", plan.ID}, {"node.created", rootID}} {
			if err = t.PutAudit(ctx, providerapp.Audit{ID: ulid.Make().String(), Action: event.action, AggregateID: event.id, Actor: "engine", Metadata: meta, CreatedAt: now}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return planning.Plan{}, err
	}
	result, err := s.GetPlan(ctx, plan.ID)
	if err != nil {
		return planning.Plan{}, err
	}
	if result == nil {
		return planning.Plan{}, projectapp.ErrInvalidTransition
	}
	return *result, nil
}
