package planning

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// PlanStatus represents the lifecycle state of a plan.
type PlanStatus string

const (
	PlanStatusDraft     PlanStatus = "draft"
	PlanStatusActive    PlanStatus = "active"
	PlanStatusPaused    PlanStatus = "paused"
	PlanStatusCompleted PlanStatus = "completed"
	PlanStatusCancelled PlanStatus = "cancelled"
	PlanStatusFailed    PlanStatus = "failed"
)

// IsTerminal returns true if the plan is in a terminal state.
func (s PlanStatus) IsTerminal() bool {
	return s == PlanStatusCompleted || s == PlanStatusCancelled || s == PlanStatusFailed
}

// NodeStatus represents the lifecycle state of a plan node.
type NodeStatus string

const (
	NodeStatusPending    NodeStatus = "pending"
	NodeStatusReady      NodeStatus = "ready"
	NodeStatusRunning    NodeStatus = "running"
	NodeStatusPaused     NodeStatus = "paused"
	NodeStatusCompleted  NodeStatus = "completed"
	NodeStatusFailed     NodeStatus = "failed"
	NodeStatusCancelled  NodeStatus = "cancelled"
	NodeStatusBlocked    NodeStatus = "blocked"
)

// IsTerminal returns true if the node is in a terminal state.
func (s NodeStatus) IsTerminal() bool {
	return s == NodeStatusCompleted || s == NodeStatusFailed || s == NodeStatusCancelled
}

// IsExecutable returns true if the node can be executed.
func (s NodeStatus) IsExecutable() bool {
	return s == NodeStatusReady || s == NodeStatusRunning
}

// RiskLevel represents the governance risk of a plan node.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// RequiresReview returns true if this risk level requires human review.
func (r RiskLevel) RequiresReview() bool {
	return r == RiskHigh || r == RiskCritical
}

// Plan is a versioned DAG of tasks that the system will execute.
// Each plan is bound to a project and may span multiple stages.
type Plan struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	StageID     *string    `json:"stageId,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Version     int64      `json:"version"`
	Status      PlanStatus `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Validate checks invariants for a plan.
func (p Plan) Validate() error {
	if !canonicalULID(p.ID) || !canonicalULID(p.ProjectID) {
		return errors.New("plan id or project_id is not a canonical ULID")
	}
	if p.StageID != nil && !canonicalULID(*p.StageID) {
		return errors.New("plan stage_id is not a canonical ULID")
	}
	if len(p.Name) < 1 || len(p.Name) > 200 {
		return errors.New("plan name must be 1-200 characters")
	}
	if len(p.Description) > 4096 {
		return errors.New("plan description too long")
	}
	if p.Version < 1 {
		return errors.New("plan version must be positive")
	}
	switch p.Status {
	case PlanStatusDraft, PlanStatusActive, PlanStatusPaused, PlanStatusCompleted, PlanStatusCancelled, PlanStatusFailed:
	default:
		return errors.New("plan status invalid")
	}
	if p.CreatedAt.IsZero() || p.CreatedAt.Location() != time.UTC {
		return errors.New("plan created_at must be UTC")
	}
	if p.UpdatedAt.IsZero() || p.UpdatedAt.Location() != time.UTC {
		return errors.New("plan updated_at must be UTC")
	}
	if p.UpdatedAt.Before(p.CreatedAt) {
		return errors.New("plan updated_at must be >= created_at")
	}
	return nil
}

// CanTransitionTo returns whether the plan can transition to the target status.
func (p Plan) CanTransitionTo(target PlanStatus) bool {
	switch p.Status {
	case PlanStatusDraft:
		return target == PlanStatusActive || target == PlanStatusCancelled
	case PlanStatusActive:
		return target == PlanStatusPaused || target == PlanStatusCompleted || target == PlanStatusFailed || target == PlanStatusCancelled
	case PlanStatusPaused:
		return target == PlanStatusActive || target == PlanStatusCancelled
	case PlanStatusFailed:
		return target == PlanStatusActive
	case PlanStatusCompleted, PlanStatusCancelled:
		return false
	default:
		return false
	}
}

// TransitionTo attempts to move the plan to the target status.
func (p Plan) TransitionTo(target PlanStatus) (Plan, error) {
	if !p.CanTransitionTo(target) {
		return p, errors.New("invalid plan status transition")
	}
	result := p
	result.Status = target
	result.UpdatedAt = time.Now().UTC()
	return result, nil
}

// Node is a single task in a plan DAG with dependencies, budget, and risk policy.
type Node struct {
	ID            string     `json:"id"`
	PlanID        string     `json:"planId"`
	ParentNodeID  *string    `json:"parentNodeId,omitempty"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Status        NodeStatus `json:"status"`
	RiskLevel     RiskLevel  `json:"riskLevel"`
	BudgetTokens  *int64     `json:"budgetTokens,omitempty"`
	EstimateTokens *int64    `json:"estimateTokens,omitempty"`
	WorkerRole    string     `json:"workerRole"`
	Sequence      int64      `json:"sequence"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// Validate checks invariants for a plan node.
func (n Node) Validate() error {
	if !canonicalULID(n.ID) || !canonicalULID(n.PlanID) {
		return errors.New("node id or plan_id is not a canonical ULID")
	}
	if n.ParentNodeID != nil && !canonicalULID(*n.ParentNodeID) {
		return errors.New("node parent_node_id is not a canonical ULID")
	}
	if len(n.Name) < 1 || len(n.Name) > 200 {
		return errors.New("node name must be 1-200 characters")
	}
	if len(n.Description) > 4096 {
		return errors.New("node description too long")
	}
	switch n.Status {
	case NodeStatusPending, NodeStatusReady, NodeStatusRunning, NodeStatusPaused, NodeStatusCompleted, NodeStatusFailed, NodeStatusCancelled, NodeStatusBlocked:
	default:
		return errors.New("node status invalid")
	}
	switch n.RiskLevel {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
	default:
		return errors.New("node risk level invalid")
	}
	if n.BudgetTokens != nil && *n.BudgetTokens < 1 {
		return errors.New("node budget_tokens must be positive")
	}
	if n.EstimateTokens != nil && *n.EstimateTokens < 0 {
		return errors.New("node estimate_tokens must be non-negative")
	}
	if len(n.WorkerRole) < 1 || len(n.WorkerRole) > 128 {
		return errors.New("node worker_role must be 1-128 characters")
	}
	if n.Sequence < 1 {
		return errors.New("node sequence must be positive")
	}
	if n.CreatedAt.IsZero() || n.CreatedAt.Location() != time.UTC {
		return errors.New("node created_at must be UTC")
	}
	if n.UpdatedAt.IsZero() || n.UpdatedAt.Location() != time.UTC {
		return errors.New("node updated_at must be UTC")
	}
	if n.UpdatedAt.Before(n.CreatedAt) {
		return errors.New("node updated_at must be >= created_at")
	}
	return nil
}

// CanTransitionTo returns whether the node can transition to the target status.
func (n Node) CanTransitionTo(target NodeStatus) bool {
	switch n.Status {
	case NodeStatusPending:
		return target == NodeStatusReady || target == NodeStatusCancelled || target == NodeStatusBlocked
	case NodeStatusReady:
		return target == NodeStatusRunning || target == NodeStatusCancelled
	case NodeStatusRunning:
		return target == NodeStatusCompleted || target == NodeStatusFailed || target == NodeStatusPaused || target == NodeStatusCancelled
	case NodeStatusPaused:
		return target == NodeStatusReady || target == NodeStatusCancelled
	case NodeStatusFailed:
		return target == NodeStatusReady
	case NodeStatusBlocked:
		return target == NodeStatusReady || target == NodeStatusCancelled
	case NodeStatusCompleted, NodeStatusCancelled:
		return false
	default:
		return false
	}
}

// TransitionTo attempts to move the node to the target status.
func (n Node) TransitionTo(target NodeStatus) (Node, error) {
	if !n.CanTransitionTo(target) {
		return n, errors.New("invalid node status transition")
	}
	result := n
	result.Status = target
	result.UpdatedAt = time.Now().UTC()
	return result, nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}