package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentorchestration"
)

const (
	testProjectID = "01ARZ3NDEKTSV4RRFFQ69G5FA0"
	testPlanID    = "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	testNodeID    = "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	testRunID     = "01ARZ3NDEKTSV4RRFFQ69G5FA3"
	testChildID   = "01ARZ3NDEKTSV4RRFFQ69G5FA4"
	testTodoID    = "01ARZ3NDEKTSV4RRFFQ69G5FA5"
)

func orchestrationStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO projects(id,name,created_at,updated_at) VALUES(?, 'project', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, testProjectID)
	if err == nil {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO message_project_usage(project_id,text_bytes) VALUES(?,0)`, testProjectID)
	}
	if err == nil {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO plans(id,project_id,name,version,status,created_at,updated_at) VALUES(?,?,'plan',1,'draft','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, testPlanID, testProjectID)
	}
	if err == nil {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO plan_nodes(id,plan_id,name,status,risk_level,sequence,created_at,updated_at) VALUES(?,?,'node','pending','low',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, testNodeID, testPlanID)
	}
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	return s
}

func TestAgentOrchestrationRepositoryPersistsReopensAndOrdersEvents(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orchestration.db")
	s := orchestrationStore(t, path)
	ids := []string{testRunID, testChildID}
	c, _ := agentorchestration.New(s.AgentOrchestrationRepository(), agentorchestration.Limits{MaxDepth: 3, MaxConcurrency: 8}, func() string { id := ids[0]; ids = ids[1:]; return id })
	root, err := c.CreateRoot(ctx, testPlanID, testNodeID, "planner", agentorchestration.Todo{ID: testTodoID, Title: "root", Metadata: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Start(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	// Scope Seal (0041): child runs are structurally banned; every run is a root.
	second, err := c.CreateRoot(ctx, testPlanID, testNodeID, "worker", agentorchestration.Todo{ID: "01ARZ3NDEKTSV4RRFFQ69G5FA6", Title: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	s = orchestrationStore(t, path)
	defer s.Close()
	c, _ = agentorchestration.New(s.AgentOrchestrationRepository(), agentorchestration.Limits{MaxDepth: 3, MaxConcurrency: 8}, nil)
	got, err := c.Get(ctx, root.ID)
	if err != nil || got.Status != agentorchestration.StatusRunning || got.Todo.Metadata["k"] != "v" {
		t.Fatalf("reopened root=%#v err=%v", got, err)
	}
	kids, err := c.ListPlanRuns(ctx, testPlanID)
	if err != nil || len(kids) != 2 || kids[1].ParentRunID != "" || kids[1].ID != second.ID {
		t.Fatalf("tree=%#v err=%v", kids, err)
	}
	events, err := c.Events(ctx, "")
	if err != nil || len(events) != 3 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("event %d sequence=%d", i, event.Sequence)
		}
	}
}

func TestAgentOrchestrationRepositoryCallbackRollback(t *testing.T) {
	s := orchestrationStore(t, filepath.Join(t.TempDir(), "rollback.db"))
	defer s.Close()
	repo := s.AgentOrchestrationRepository()
	sentinel := errors.New("abort")
	err := repo.Transact(context.Background(), func(tx agentorchestration.Transaction) error {
		tx.Put(agentorchestration.AgentRun{ID: testRunID, PlanID: testPlanID, NodeID: testNodeID, Role: "r", Todo: agentorchestration.Todo{ID: testTodoID, Title: "todo"}, Status: agentorchestration.StatusQueued, CreatedAt: mustTime(t), UpdatedAt: mustTime(t), Version: 1})
		tx.Append(agentorchestration.Event{RunID: testRunID, Type: "run_created", To: agentorchestration.StatusQueued, At: mustTime(t)})
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
	_ = repo.Transact(context.Background(), func(tx agentorchestration.Transaction) error {
		if _, ok := tx.Get(testRunID); ok || len(tx.ListEvents("")) != 0 {
			t.Fatal("callback writes committed")
		}
		return nil
	})
}

func TestAgentOrchestrationRepositoryRestartReconciliation(t *testing.T) {
	ctx := context.Background()
	s := orchestrationStore(t, filepath.Join(t.TempDir(), "restart.db"))
	defer s.Close()
	c, _ := agentorchestration.New(s.AgentOrchestrationRepository(), agentorchestration.Limits{MaxDepth: 2, MaxConcurrency: 4}, func() string { return testRunID })
	r, err := c.CreateRoot(ctx, testPlanID, testNodeID, "planner", agentorchestration.Todo{ID: testTodoID, Title: "todo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Start(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = c.ReconcileRestart(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := c.Get(ctx, r.ID)
	events, _ := c.Events(ctx, r.ID)
	if got.Status != agentorchestration.StatusQueued || events[len(events)-1].Type != "restart_reconciled" {
		t.Fatalf("run=%#v events=%#v", got, events)
	}
	before := len(events)
	if err = c.ReconcileRestart(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ = c.Events(ctx, r.ID)
	if len(events) != before {
		t.Fatalf("reconciliation not idempotent: %d -> %d", before, len(events))
	}
}

func mustTime(t *testing.T) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return v
}
