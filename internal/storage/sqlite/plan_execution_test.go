package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/oklog/ulid/v2"
)

func planExecutionStorageFixture(t *testing.T) (*Store, *agentrunapp.Service, agentorchestration.AgentRun) {
	t.Helper()
	s, plans, p := planningUpgradeFixture(t)
	n := planningUpgradeNode(t, plans, p, 1, nil)
	if err := plans.Activate(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	c, err := agentorchestration.New(s.AgentOrchestrationRepository(), agentorchestration.Limits{MaxDepth: 3, MaxConcurrency: 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := c.CreateRoot(context.Background(), p.ID, n.ID, "implementer", agentorchestration.Todo{ID: ulid.Make().String(), Title: "report"})
	if err != nil {
		t.Fatal(err)
	}
	return s, agentrunapp.New(s.AgentRuntimeRepository()), task
}
func storageExecutionBudget() agentrun.Budget {
	return agentrun.Budget{MaxModelTurns: 8, MaxToolCalls: 8, MaxTokens: 1000, MaxCostMicros: 1000, MaxWallClockSeconds: 60, MaxOutputBytes: 4096, HardCeiling: true}
}
func claimStorageExecution(t *testing.T, svc *agentrunapp.Service, id string) agentrun.PlanExecution {
	t.Helper()
	ex, created, err := svc.StartPlanExecution(context.Background(), id, "provider", "model", storageExecutionBudget())
	if err != nil || !created {
		t.Fatalf("start created=%v err=%v", created, err)
	}
	return ex
}
func TestPlanExecutionStorageClaimAuditFailureRollsBackEverything(t *testing.T) {
	s, svc, task := planExecutionStorageFixture(t)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_plan_start BEFORE INSERT ON audit_events WHEN NEW.action='agent.run.started' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := svc.StartPlanExecution(context.Background(), task.ID, "provider", "model", storageExecutionBudget()); err == nil || created {
		t.Fatalf("start created=%v err=%v", created, err)
	}
	for _, table := range []string{"plan_run_executions", "agent_run", "sessions"} {
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	n, err := s.GetNode(context.Background(), task.NodeID)
	if err != nil || n.Status != planning.NodeStatusPending {
		t.Fatalf("node=%+v err=%v", n, err)
	}
}
func TestPlanExecutionStorageReceiptFailurePreventsSuccessAndReconcilesUnknown(t *testing.T) {
	s, svc, task := planExecutionStorageFixture(t)
	ctx := context.Background()
	ex := claimStorageExecution(t, svc, task.ID)
	call, err := svc.PreparePlanTool(ctx, task.ID, ex.RunID, "workspace.write", json.RawMessage(`{"path":"report.md","content":"done"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_plan_receipt BEFORE INSERT ON observation WHEN NEW.kind='plan.tool.receipt' BEGIN SELECT RAISE(ABORT,'receipt unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReceiptPlanTool(ctx, task.ID, call.ID, "done", nil, false); err == nil {
		t.Fatal("receipt failure swallowed")
	}
	result, err := svc.FinishPlanExecution(ctx, task.ID, ex.RunID, "succeeded", "claimed complete", "", []agentrun.PlanArtifact{{Path: "report.md", Digest: strings.Repeat("a", 64), Bytes: 4}}, []string{call.ID})
	if err != nil || result.Status != "outcome_unknown" || len(result.Artifacts) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestPlanExecutionStorageTerminalAuditFailureRollsBackEvidenceAndNode(t *testing.T) {
	s, svc, task := planExecutionStorageFixture(t)
	ctx := context.Background()
	ex := claimStorageExecution(t, svc, task.ID)
	call, err := svc.PreparePlanTool(ctx, task.ID, ex.RunID, "workspace.write", json.RawMessage(`{"path":"report.md","content":"done"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ReceiptPlanTool(ctx, task.ID, call.ID, "done", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_plan_terminal BEFORE INSERT ON audit_events WHEN NEW.action='agent.run.reconciled' BEGIN SELECT RAISE(ABORT,'terminal audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.FinishPlanExecution(ctx, task.ID, ex.RunID, "succeeded", "complete", "", []agentrun.PlanArtifact{{Path: "report.md", Digest: strings.Repeat("a", 64), Bytes: 4}}, []string{call.ID}); err == nil {
		t.Fatal("terminal audit failure swallowed")
	}
	saved, err := svc.GetPlanExecution(ctx, task.ID)
	if err != nil || saved.Status != "running" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	var evidence int
	if err = s.db.QueryRow(`SELECT count(*) FROM evidence WHERE run_id=?`, ex.RunID).Scan(&evidence); err != nil || evidence != 0 {
		t.Fatalf("evidence=%d err=%v", evidence, err)
	}
	n, err := s.GetNode(ctx, task.NodeID)
	if err != nil || n.Status != planning.NodeStatusRunning {
		t.Fatalf("node=%+v err=%v", n, err)
	}
}
func TestPlanExecutionStorageRejectsCrossPlanAndUnreviewedRisk(t *testing.T) {
	for _, kind := range []string{"cross_plan", "high_risk", "manual_completion"} {
		t.Run(kind, func(t *testing.T) {
			s, svc, task := planExecutionStorageFixture(t)
			ctx := context.Background()
			switch kind {
			case "cross_plan":
				var projectID string
				if err := s.db.QueryRow(`SELECT project_id FROM plans WHERE id=?`, task.PlanID).Scan(&projectID); err != nil {
					t.Fatal(err)
				}
				other := ulid.Make().String()
				if _, err := s.db.Exec(`INSERT INTO plans(id,project_id,name,status,version,created_at,updated_at) SELECT ?,project_id,'Other','active',1,created_at,updated_at FROM plans WHERE id=?`, other, task.PlanID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.db.Exec(`UPDATE agent_plan_runs SET plan_id=? WHERE id=?`, other, task.ID); err != nil {
					t.Fatal(err)
				}
			case "high_risk":
				if _, err := s.db.Exec(`UPDATE plan_nodes SET risk_level='high' WHERE id=?`, task.NodeID); err != nil {
					t.Fatal(err)
				}
			case "manual_completion":
				claimStorageExecution(t, svc, task.ID)
				if err := s.UpdateNodeStatus(ctx, task.NodeID, string(planning.NodeStatusCompleted)); err == nil {
					t.Fatal("manual node completion bypassed pending task evidence")
				}
				return
			}
			if _, created, err := svc.StartPlanExecution(ctx, task.ID, "provider", "model", storageExecutionBudget()); err == nil || created {
				t.Fatalf("invalid claim created=%v err=%v", created, err)
			}
		})
	}
}

func TestPlanExecutionStorageApprovalBoundToCurrentTask(t *testing.T) {
	s, svc, task := planExecutionStorageFixture(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`UPDATE plan_nodes SET risk_level='high' WHERE id=?`, task.NodeID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.StartPlanExecution(ctx, task.ID, "provider", "model", storageExecutionBudget()); err == nil {
		t.Fatal("unapproved task started")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM governance_reviews WHERE node_id=? AND status='pending' AND action_type='plan.run.start'`, task.NodeID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("review count=%d err=%v", count, err)
	}
	if _, err := s.db.Exec(`UPDATE governance_reviews SET status='approved',reviewed_at=created_at WHERE node_id=?`, task.NodeID); err != nil {
		t.Fatal(err)
	}
	ex := claimStorageExecution(t, svc, task.ID)
	if _, err := s.db.Exec(`UPDATE agent_plan_runs SET todo_description='changed after approval' WHERE id=?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreparePlanTool(ctx, task.ID, ex.RunID, "workspace.write", json.RawMessage(`{}`)); err == nil {
		t.Fatal("stale approval dispatched changed task")
	}
}
