package planningapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/planning"
)

type mockReader struct {
	plan  *planning.Plan
	nodes []planning.Node
	err   error
}

func (m *mockReader) GetPlan(_ context.Context, _ string) (*planning.Plan, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.plan, nil
}
func (m *mockReader) ListPlansByProject(_ context.Context, _ string, _ int) ([]planning.Plan, error) {
	if m.plan != nil {
		return []planning.Plan{*m.plan}, nil
	}
	return nil, nil
}
func (m *mockReader) GetNode(_ context.Context, _ string) (*planning.Node, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(m.nodes) > 0 {
		return &m.nodes[0], nil
	}
	return nil, nil
}
func (m *mockReader) ListNodesByPlan(_ context.Context, _ string, _ int) ([]planning.Node, error) {
	return m.nodes, m.err
}

type mockWriter struct {
	planStatus  string
	nodeStatus  string
	err         error
	reader      *mockReader
}
func (m *mockWriter) CreatePlan(_ context.Context, plan planning.Plan) (planning.Plan, error) {
	if m.err != nil {
		return plan, m.err
	}
	if plan.ID == "" {
		plan.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	}
	return plan, nil
}
func (m *mockWriter) CreateNode(_ context.Context, node planning.Node) (planning.Node, error) {
	if m.err != nil {
		return node, m.err
	}
	if node.ID == "" {
		node.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	}
	return node, nil
}
func (m *mockWriter) UpdatePlanStatus(_ context.Context, _, status string) error {
	m.planStatus = status
	if m.reader != nil && m.reader.plan != nil {
		m.reader.plan.Status = planning.PlanStatus(status)
	}
	return m.err
}
func (m *mockWriter) UpdateNodeStatus(_ context.Context, id, status string) error {
	m.nodeStatus = status
	if m.reader != nil {
		for i := range m.reader.nodes {
			if m.reader.nodes[i].ID == id {
				m.reader.nodes[i].Status = planning.NodeStatus(status)
			}
		}
	}
	return m.err
}

type mockGate struct {
	requires bool
	approved bool
	err      error
}
func (m *mockGate) RequiresReview(_ context.Context, _ planning.Node) (bool, error) {
	return m.requires, m.err
}
func (m *mockGate) IsApproved(_ context.Context, _ planning.Node) (bool, error) {
	return m.approved, m.err
}

func now() time.Time { return time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC) }
func newPlan(status planning.PlanStatus) *planning.Plan {
	return &planning.Plan{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", Name: "Test Plan", Version: 1, Status: status, CreatedAt: now(), UpdatedAt: now()}
}
func newNode(id string, status planning.NodeStatus, risk planning.RiskLevel, parent *string) planning.Node {
	return planning.Node{ID: id, PlanID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ParentNodeID: parent, Name: "Node", Status: status, RiskLevel: risk, WorkerRole: "agent", Sequence: 1, CreatedAt: now(), UpdatedAt: now()}
}

func TestActivateValidDAG(t *testing.T) {
	parent := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusPending, planning.RiskLow, nil)
	child := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAAB", planning.NodeStatusPending, planning.RiskLow, &parent.ID)
	r := &mockReader{plan: newPlan(planning.PlanStatusDraft), nodes: []planning.Node{parent, child}}
	w := &mockWriter{}
	s := New(r, w, &mockGate{})
	if err := s.Activate(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.planStatus != string(planning.PlanStatusActive) {
		t.Fatalf("expected active, got %s", w.planStatus)
	}
}

func TestActivateRejectsCyclicDAG(t *testing.T) {
	a := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusPending, planning.RiskLow, strPtr("01ARZ3NDEKTSV4RRFFQ69G5FAAB"))
	b := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAAB", planning.NodeStatusPending, planning.RiskLow, strPtr("01ARZ3NDEKTSV4RRFFQ69G5FAA"))
	r := &mockReader{plan: newPlan(planning.PlanStatusDraft), nodes: []planning.Node{a, b}}
	w := &mockWriter{}
	s := New(r, w, &mockGate{})
	if err := s.Activate(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != ErrCyclicDependency {
		t.Fatalf("expected ErrCyclicDependency, got %v", err)
	}
}

func TestActivateRejectsNonDraftPlan(t *testing.T) {
	r := &mockReader{plan: newPlan(planning.PlanStatusActive)}
	w := &mockWriter{}
	s := New(r, w, &mockGate{})
	if err := s.Activate(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestGetReadyNodes(t *testing.T) {
	parent := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusCompleted, planning.RiskLow, nil)
	child := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAAB", planning.NodeStatusPending, planning.RiskLow, &parent.ID)
	orphan := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAAC", planning.NodeStatusPending, planning.RiskLow, nil)
	blocked := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAAD", planning.NodeStatusPending, planning.RiskLow, &child.ID)
	r := &mockReader{plan: newPlan(planning.PlanStatusActive), nodes: []planning.Node{parent, child, orphan, blocked}}
	s := New(r, &mockWriter{}, &mockGate{})
	ready, err := s.GetReadyNodes(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready nodes, got %d", len(ready))
	}
	ids := map[string]bool{}
	for _, n := range ready {
		ids[n.ID] = true
	}
	if !ids["01ARZ3NDEKTSV4RRFFQ69G5FAAB"] || !ids["01ARZ3NDEKTSV4RRFFQ69G5FAAC"] {
		t.Fatalf("expected child and orphan to be ready, got %v", ids)
	}
}

func TestStartNodeRequiresReviewForHighRisk(t *testing.T) {
	node := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusReady, planning.RiskHigh, nil)
	r := &mockReader{plan: newPlan(planning.PlanStatusActive), nodes: []planning.Node{node}}
	w := &mockWriter{}
	s := New(r, w, &mockGate{requires: true, approved: false})
	if err := s.StartNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA"); err != ErrReviewRequired {
		t.Fatalf("expected ErrReviewRequired, got %v", err)
	}
}

func TestStartNodeProceedsWhenReviewApproved(t *testing.T) {
	node := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusReady, planning.RiskCritical, nil)
	r := &mockReader{plan: newPlan(planning.PlanStatusActive), nodes: []planning.Node{node}}
	w := &mockWriter{}
	s := New(r, w, &mockGate{requires: true, approved: true})
	if err := s.StartNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA"); err != nil {
		t.Fatal(err)
	}
	if w.nodeStatus != string(planning.NodeStatusRunning) {
		t.Fatalf("expected running, got %s", w.nodeStatus)
	}
}

func TestStartNodeRejectsNonReadyNode(t *testing.T) {
	node := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusPending, planning.RiskLow, nil)
	r := &mockReader{plan: newPlan(planning.PlanStatusActive), nodes: []planning.Node{node}}
	s := New(r, &mockWriter{}, &mockGate{})
	if err := s.StartNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA"); err != ErrNodeNotReady {
		t.Fatalf("expected ErrNodeNotReady, got %v", err)
	}
}

func TestCompleteNodeCompletesPlanWhenAllDone(t *testing.T) {
	node := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusRunning, planning.RiskLow, nil)
	r := &mockReader{plan: newPlan(planning.PlanStatusActive), nodes: []planning.Node{node}}
	w := &mockWriter{reader: r}
	s := New(r, w, &mockGate{})
	if err := s.CompleteNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA"); err != nil {
		t.Fatal(err)
	}
	if w.nodeStatus != string(planning.NodeStatusCompleted) {
		t.Fatalf("expected node completed, got %s", w.nodeStatus)
	}
	if w.planStatus != string(planning.PlanStatusCompleted) {
		t.Fatalf("expected plan completed, got %s", w.planStatus)
	}
}

func TestFailNodeFailsPlan(t *testing.T) {
	node := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusRunning, planning.RiskLow, nil)
	r := &mockReader{plan: newPlan(planning.PlanStatusActive), nodes: []planning.Node{node}}
	w := &mockWriter{}
	s := New(r, w, &mockGate{})
	if err := s.FailNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA"); err != nil {
		t.Fatal(err)
	}
	if w.nodeStatus != string(planning.NodeStatusFailed) {
		t.Fatalf("expected node failed, got %s", w.nodeStatus)
	}
	if w.planStatus != string(planning.PlanStatusFailed) {
		t.Fatalf("expected plan failed, got %s", w.planStatus)
	}
}

func TestPauseAndResumePlan(t *testing.T) {
	r := &mockReader{plan: newPlan(planning.PlanStatusActive)}
	w := &mockWriter{}
	s := New(r, w, &mockGate{})
	if err := s.PausePlan(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.planStatus != string(planning.PlanStatusPaused) {
		t.Fatalf("expected paused, got %s", w.planStatus)
	}
	r.plan = newPlan(planning.PlanStatusPaused)
	if err := s.ResumePlan(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.planStatus != string(planning.PlanStatusActive) {
		t.Fatalf("expected active, got %s", w.planStatus)
	}
}

func TestGetPlanNotFound(t *testing.T) {
	r := &mockReader{plan: nil}
	s := New(r, &mockWriter{}, &mockGate{})
	if _, err := s.Get(context.Background(), "missing"); err != ErrPlanNotFound {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestStartNodePropagatesGateError(t *testing.T) {
	node := newNode("01ARZ3NDEKTSV4RRFFQ69G5FAA", planning.NodeStatusReady, planning.RiskHigh, nil)
	r := &mockReader{plan: newPlan(planning.PlanStatusActive), nodes: []planning.Node{node}}
	gateErr := errors.New("gate failure")
	s := New(r, &mockWriter{}, &mockGate{err: gateErr})
	if err := s.StartNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA"); err != gateErr {
		t.Fatalf("expected gate error, got %v", err)
	}
}

func strPtr(s string) *string { return &s }
