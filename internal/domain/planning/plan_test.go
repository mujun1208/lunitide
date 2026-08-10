package planning

import (
	"testing"
	"time"
)

func TestPlanValidate(t *testing.T) {
	valid := Plan{
		ID:          "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:        "Test Plan",
		Description: "A test plan",
		Version:     1,
		Status:      PlanStatusDraft,
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.Version = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero version accepted")
	}
	invalid = valid
	invalid.Status = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown status accepted")
	}
	invalid = valid
	invalid.CreatedAt = time.Time{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero created_at accepted")
	}
	invalid = valid
	invalid.UpdatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := invalid.Validate(); err == nil {
		t.Fatal("updated before created accepted")
	}
}

func TestPlanStatusTransitions(t *testing.T) {
	p := Plan{Status: PlanStatusDraft}
	if _, err := p.TransitionTo(PlanStatusActive); err != nil {
		t.Fatalf("draft->active: %v", err)
	}
	p.Status = PlanStatusActive
	if _, err := p.TransitionTo(PlanStatusCompleted); err != nil {
		t.Fatalf("active->completed: %v", err)
	}
	if _, err := p.TransitionTo(PlanStatusPaused); err != nil {
		t.Fatalf("active->paused: %v", err)
	}
	p.Status = PlanStatusPaused
	if _, err := p.TransitionTo(PlanStatusActive); err != nil {
		t.Fatalf("paused->active: %v", err)
	}
	p.Status = PlanStatusFailed
	if _, err := p.TransitionTo(PlanStatusActive); err != nil {
		t.Fatalf("failed->active: %v", err)
	}
	p.Status = PlanStatusCompleted
	if _, err := p.TransitionTo(PlanStatusDraft); err == nil {
		t.Fatal("completed->draft should fail")
	}
}

func TestNodeValidate(t *testing.T) {
	valid := Node{
		ID:          "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		PlanID:      "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:        "Test Node",
		Description: "A test node",
		Status:      NodeStatusPending,
		RiskLevel:   RiskLow,
		WorkerRole:  "developer",
		Sequence:    1,
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid node rejected: %v", err)
	}

	invalid := valid
	invalid.RiskLevel = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown risk level accepted")
	}
	invalid = valid
	invalid.Status = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown status accepted")
	}
	invalid = valid
	invalid.Sequence = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero sequence accepted")
	}
}

func TestNodeStatusTransitions(t *testing.T) {
	n := Node{Status: NodeStatusPending}
	if _, err := n.TransitionTo(NodeStatusReady); err != nil {
		t.Fatalf("pending->ready: %v", err)
	}
	n.Status = NodeStatusReady
	if _, err := n.TransitionTo(NodeStatusRunning); err != nil {
		t.Fatalf("ready->running: %v", err)
	}
	n.Status = NodeStatusRunning
	if _, err := n.TransitionTo(NodeStatusCompleted); err != nil {
		t.Fatalf("running->completed: %v", err)
	}
	if _, err := n.TransitionTo(NodeStatusFailed); err != nil {
		t.Fatalf("running->failed: %v", err)
	}
	n.Status = NodeStatusCompleted
	if _, err := n.TransitionTo(NodeStatusRunning); err == nil {
		t.Fatal("completed->running should fail")
	}
}

func TestRiskLevelRequiresReview(t *testing.T) {
	if RiskLow.RequiresReview() {
		t.Fatal("low risk should not require review")
	}
	if RiskMedium.RequiresReview() {
		t.Fatal("medium risk should not require review")
	}
	if !RiskHigh.RequiresReview() {
		t.Fatal("high risk should require review")
	}
	if !RiskCritical.RequiresReview() {
		t.Fatal("critical risk should require review")
	}
}