package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/planningapp"
	"github.com/lunitide/lunitide/internal/stageapp"
)

func planningUpgradeFixture(t *testing.T) (*Store, *planningapp.Service, planning.Plan) {
	t.Helper()
	s, _, p := phaseTestStore(t, project.TypeImplementation)
	svc := planningapp.New(s, s, nil)
	plan, err := svc.CreatePlan(context.Background(), planning.Plan{ProjectID: p.ID, Name: "Plan"})
	if err != nil {
		t.Fatal(err)
	}
	return s, svc, plan
}
func planningUpgradeNode(t *testing.T, svc *planningapp.Service, p planning.Plan, i int, parent *string) planning.Node {
	t.Helper()
	n, err := svc.CreateNode(context.Background(), planning.Node{PlanID: p.ID, Name: fmt.Sprintf("Task %d", i), RiskLevel: planning.RiskLow, WorkerRole: "planner", Sequence: int64(i), ParentNodeID: parent})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func TestPlanningUpgradeRejectsZeroSequence(t *testing.T) {
	_, svc, p := planningUpgradeFixture(t)
	_, err := svc.CreateNode(context.Background(), planning.Node{PlanID: p.ID, Name: "Root", RiskLevel: planning.RiskLow, WorkerRole: "planner", Sequence: 0})
	if err == nil {
		t.Fatal("zero sequence accepted")
	}
}
func TestPlanningUpgradePausedPlanCannotStartNode(t *testing.T) {
	s, svc, p := planningUpgradeFixture(t)
	ctx := context.Background()
	n := planningUpgradeNode(t, svc, p, 1, nil)
	if err := svc.Activate(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNodeStatus(ctx, n.ID, string(planning.NodeStatusReady)); err != nil {
		t.Fatal(err)
	}
	if err := svc.PausePlan(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartNode(ctx, n.ID); !errors.Is(err, planningapp.ErrPlanNotActive) {
		t.Fatalf("err=%v", err)
	}
	if err := s.UpdateNodeStatus(ctx, n.ID, string(planning.NodeStatusRunning)); !errors.Is(err, planningapp.ErrPlanNotActive) {
		t.Fatalf("storage bypass err=%v", err)
	}
	saved, _ := s.GetNode(ctx, n.ID)
	if saved.Status != planning.NodeStatusReady {
		t.Fatalf("node=%+v", saved)
	}
}
func TestPlanningUpgradeFailedNodeCannotBeOverwrittenByCompletion(t *testing.T) {
	s, svc, p := planningUpgradeFixture(t)
	ctx := context.Background()
	a := planningUpgradeNode(t, svc, p, 1, nil)
	b := planningUpgradeNode(t, svc, p, 2, nil)
	if err := svc.Activate(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	for _, n := range []planning.Node{a, b} {
		if err := svc.StartNode(ctx, n.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.FailNode(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteNode(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.GetPlan(ctx, p.ID)
	if saved.Status != planning.PlanStatusFailed {
		t.Fatalf("plan=%+v", saved)
	}
}
func TestPlanningUpgradeStartEnforcesParentCompletion(t *testing.T) {
	_, svc, p := planningUpgradeFixture(t)
	ctx := context.Background()
	parent := planningUpgradeNode(t, svc, p, 1, nil)
	child := planningUpgradeNode(t, svc, p, 2, &parent.ID)
	if err := svc.Activate(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartNode(ctx, child.ID); !errors.Is(err, planningapp.ErrDependencyNotMet) {
		t.Fatalf("err=%v", err)
	}
	if err := svc.StartNode(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteNode(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartNode(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
}
func TestPlanningUpgradeAggregateReadsBeyondOneHundredNodes(t *testing.T) {
	s, svc, p := planningUpgradeFixture(t)
	ctx := context.Background()
	nodes := make([]planning.Node, 101)
	for i := range nodes {
		nodes[i] = planningUpgradeNode(t, svc, p, i+1, nil)
	}
	if err := svc.Activate(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE plan_nodes SET status='completed' WHERE plan_id=? AND sequence<100`, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartNode(ctx, nodes[99].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteNode(ctx, nodes[99].ID); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.GetPlan(ctx, p.ID)
	if saved.Status != planning.PlanStatusActive {
		t.Fatalf("101st pending was ignored: %+v", saved)
	}
	if err := svc.CompletePlan(ctx, p.ID); err == nil {
		t.Fatal("completed with pending 101st node")
	}
}
func TestPlanningUpgradeNodeAndAggregateRollbackTogether(t *testing.T) {
	s, svc, p := planningUpgradeFixture(t)
	ctx := context.Background()
	n := planningUpgradeNode(t, svc, p, 1, nil)
	if err := svc.Activate(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER injected_plan_failure BEFORE UPDATE ON plans BEGIN SELECT RAISE(ABORT,'injected plan failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteNode(ctx, n.ID); err == nil {
		t.Fatal("injected failure ignored")
	}
	saved, _ := s.GetNode(ctx, n.ID)
	if saved.Status != planning.NodeStatusRunning {
		t.Fatalf("partial completion: %+v", saved)
	}
}
func TestPlanningUpgradePhasePlanCreationIsAtomicAndUnique(t *testing.T) {
	s, _, p := phaseTestStore(t, project.TypeImplementation)
	ctx := context.Background()
	stages := stageapp.New(s, s)
	svc := planningapp.New(s, s, nil)
	st, err := stages.Create(ctx, "phase-stage", "test", map[string]int{"phase": 1}, stage.Stage{ProjectID: p.ID, Phase: 1, Title: "Phase 1"})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	results := make(chan planning.Plan, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plan, err := svc.CreatePlan(ctx, planning.Plan{ProjectID: p.ID, StageID: &st.ID, Name: "Stage plan"})
			results <- plan
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for plan := range results {
		if first == "" {
			first = plan.ID
		}
		if plan.ID != first || plan.Status != planning.PlanStatusActive {
			t.Fatalf("plan=%+v", plan)
		}
	}
	nodes, err := svc.ListNodes(ctx, first)
	if err != nil || len(nodes) != 1 || nodes[0].Sequence != 1 || nodes[0].Status != planning.NodeStatusReady {
		t.Fatalf("nodes=%+v err=%v", nodes, err)
	}
	var count int
	_ = s.db.QueryRow(`SELECT count(*) FROM plans WHERE project_id=?`, p.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate plans=%d", count)
	}
}
func TestPlanningUpgradeFailedRootCreationLeavesNoPlan(t *testing.T) {
	s, _, p := phaseTestStore(t, project.TypeImplementation)
	ctx := context.Background()
	st, err := stageapp.New(s, s).Create(ctx, "stage-fail", "test", map[string]int{"phase": 1}, stage.Stage{ProjectID: p.ID, Phase: 1, Title: "Phase 1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER injected_root_failure BEFORE INSERT ON plan_nodes BEGIN SELECT RAISE(ABORT,'injected root failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = planningapp.New(s, s, nil).CreatePlan(ctx, planning.Plan{ProjectID: p.ID, StageID: &st.ID, Name: "Phase plan"})
	if err == nil {
		t.Fatal("root failure ignored")
	}
	var count int
	_ = s.db.QueryRow(`SELECT count(*) FROM plans WHERE project_id=?`, p.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("partial plans=%d", count)
	}
}
