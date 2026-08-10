// Package planningapp coordinates plan DAG execution with governance gatekeeping.
package planningapp

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/oklog/ulid/v2"
)

var (
	ErrPlanNotFound       = errors.New("plan not found")
	ErrNodeNotFound       = errors.New("node not found")
	ErrInvalidTransition  = errors.New("invalid status transition")
	ErrPlanNotActive      = errors.New("plan is not active")
	ErrNodeNotReady       = errors.New("node is not ready for execution")
	ErrDependencyNotMet   = errors.New("node dependencies are not satisfied")
	ErrReviewRequired     = errors.New("review required before executing this node")
	ErrReviewNotApproved  = errors.New("review is not approved")
	ErrCyclicDependency   = errors.New("cyclic dependency detected in plan DAG")
)

// PlanReader reads plans and nodes from storage.
type PlanReader interface {
	GetPlan(ctx context.Context, id string) (*planning.Plan, error)
	ListPlansByProject(ctx context.Context, projectID string, limit int) ([]planning.Plan, error)
	GetNode(ctx context.Context, id string) (*planning.Node, error)
	ListNodesByPlan(ctx context.Context, planID string, limit int) ([]planning.Node, error)
}

// PlanWriter writes plan and node status updates.
type PlanWriter interface {
	CreatePlan(ctx context.Context, plan planning.Plan) (planning.Plan, error)
	CreateNode(ctx context.Context, node planning.Node) (planning.Node, error)
	UpdatePlanStatus(ctx context.Context, id, status string) error
	UpdateNodeStatus(ctx context.Context, id, status string) error
}

// GovernanceGate checks whether a node requires review and whether review is approved.
type GovernanceGate interface {
	RequiresReview(ctx context.Context, node planning.Node) (bool, error)
	IsApproved(ctx context.Context, node planning.Node) (bool, error)
}

// Clock provides the current time.
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Service coordinates plan lifecycle and DAG execution.
type Service struct {
	read  PlanReader
	write PlanWriter
	gate  GovernanceGate
	clock Clock
}

// New creates a planning service with the given dependencies.
func New(r PlanReader, w PlanWriter, gate GovernanceGate) *Service {
	return &Service{read: r, write: w, gate: gate, clock: systemClock{}}
}

// Get returns a plan by ID.
func (s *Service) Get(ctx context.Context, id string) (*planning.Plan, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("planning reader unavailable")
	}
	p, err := s.read.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrPlanNotFound
	}
	return p, nil
}

// CreatePlan validates and persists a new plan with initial draft status.
func (s *Service) CreatePlan(ctx context.Context, plan planning.Plan) (planning.Plan, error) {
	if s == nil || s.write == nil {
		return planning.Plan{}, errors.New("planning writer unavailable")
	}
	if !canonicalULID(plan.ProjectID) {
		return planning.Plan{}, errors.New("plan project_id is not a canonical ULID")
	}
	if len(plan.Name) < 1 || len(plan.Name) > 200 {
		return planning.Plan{}, errors.New("plan name must be 1-200 characters")
	}
	if len(plan.Description) > 4096 {
		return planning.Plan{}, errors.New("plan description too long")
	}
	if plan.StageID != nil && !canonicalULID(*plan.StageID) {
		return planning.Plan{}, errors.New("plan stage_id is not a canonical ULID")
	}
	now := s.clock.Now()
	plan.ID = ""
	plan.Version = 1
	plan.Status = planning.PlanStatusDraft
	plan.CreatedAt = now
	plan.UpdatedAt = now
	return s.write.CreatePlan(ctx, plan)
}

// CreateNode validates and persists a new plan node with initial pending status.
func (s *Service) CreateNode(ctx context.Context, node planning.Node) (planning.Node, error) {
	if s == nil || s.write == nil {
		return planning.Node{}, errors.New("planning writer unavailable")
	}
	if !canonicalULID(node.PlanID) {
		return planning.Node{}, errors.New("node plan_id is not a canonical ULID")
	}
	if node.ParentNodeID != nil && !canonicalULID(*node.ParentNodeID) {
		return planning.Node{}, errors.New("node parent_node_id is not a canonical ULID")
	}
	if len(node.Name) < 1 || len(node.Name) > 200 {
		return planning.Node{}, errors.New("node name must be 1-200 characters")
	}
	if len(node.Description) > 4096 {
		return planning.Node{}, errors.New("node description too long")
	}
	switch node.RiskLevel {
	case planning.RiskLow, planning.RiskMedium, planning.RiskHigh, planning.RiskCritical:
	default:
		return planning.Node{}, errors.New("node risk level invalid")
	}
	if len(node.WorkerRole) < 1 || len(node.WorkerRole) > 128 {
		return planning.Node{}, errors.New("node worker_role must be 1-128 characters")
	}
	if node.Sequence < 0 {
		return planning.Node{}, errors.New("node sequence must be non-negative")
	}
	if node.BudgetTokens != nil && *node.BudgetTokens < 1 {
		return planning.Node{}, errors.New("node budget_tokens must be positive")
	}
	if node.EstimateTokens != nil && *node.EstimateTokens < 0 {
		return planning.Node{}, errors.New("node estimate_tokens must be non-negative")
	}
	now := s.clock.Now()
	node.ID = ""
	node.Status = planning.NodeStatusPending
	node.CreatedAt = now
	node.UpdatedAt = now
	return s.write.CreateNode(ctx, node)
}

// ListByProject returns plans for a project.
func (s *Service) ListByProject(ctx context.Context, projectID string) ([]planning.Plan, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("planning reader unavailable")
	}
	return s.read.ListPlansByProject(ctx, projectID, 100)
}

// ListNodes returns all nodes for a plan ordered by sequence.
func (s *Service) ListNodes(ctx context.Context, planID string) ([]planning.Node, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("planning reader unavailable")
	}
	return s.read.ListNodesByPlan(ctx, planID, 100)
}

// Activate transitions a plan from draft to active.
func (s *Service) Activate(ctx context.Context, planID string) error {
	if s == nil || s.write == nil {
		return errors.New("planning writer unavailable")
	}
	plan, err := s.read.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	if plan == nil {
		return ErrPlanNotFound
	}
	if !plan.CanTransitionTo(planning.PlanStatusActive) {
		return ErrInvalidTransition
	}
	// Validate DAG has no cycles before activation.
	nodes, err := s.read.ListNodesByPlan(ctx, planID, 100)
	if err != nil {
		return err
	}
	if err := validateDAG(nodes); err != nil {
		return err
	}
	return s.write.UpdatePlanStatus(ctx, planID, string(planning.PlanStatusActive))
}

// GetReadyNodes returns nodes that are ready to execute (all parent dependencies completed).
func (s *Service) GetReadyNodes(ctx context.Context, planID string) ([]planning.Node, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("planning reader unavailable")
	}
	plan, err := s.read.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, ErrPlanNotFound
	}
	if plan.Status != planning.PlanStatusActive {
		return nil, ErrPlanNotActive
	}
	nodes, err := s.read.ListNodesByPlan(ctx, planID, 100)
	if err != nil {
		return nil, err
	}
	nodeMap := make(map[string]*planning.Node, len(nodes))
	for i := range nodes {
		nodeMap[nodes[i].ID] = &nodes[i]
	}
	var ready []planning.Node
	for _, node := range nodes {
		if node.Status != planning.NodeStatusPending {
			continue
		}
		if isDependenciesMet(node, nodeMap) {
			ready = append(ready, node)
		}
	}
	return ready, nil
}

// StartNode transitions a ready node to running, enforcing governance review for high-risk nodes.
func (s *Service) StartNode(ctx context.Context, nodeID string) error {
	if s == nil || s.write == nil {
		return errors.New("planning writer unavailable")
	}
	node, err := s.read.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}
	if !node.CanTransitionTo(planning.NodeStatusRunning) {
		return ErrNodeNotReady
	}
	// Check governance gate for high-risk nodes.
	if s.gate != nil {
		requires, err := s.gate.RequiresReview(ctx, *node)
		if err != nil {
			return err
		}
		if requires {
			approved, err := s.gate.IsApproved(ctx, *node)
			if err != nil {
				return err
			}
			if !approved {
				return ErrReviewRequired
			}
		}
	}
	return s.write.UpdateNodeStatus(ctx, nodeID, string(planning.NodeStatusRunning))
}

// CompleteNode transitions a running node to completed and checks if the plan is done.
func (s *Service) CompleteNode(ctx context.Context, nodeID string) error {
	if s == nil || s.write == nil {
		return errors.New("planning writer unavailable")
	}
	node, err := s.read.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}
	if !node.CanTransitionTo(planning.NodeStatusCompleted) {
		return ErrInvalidTransition
	}
	if err := s.write.UpdateNodeStatus(ctx, nodeID, string(planning.NodeStatusCompleted)); err != nil {
		return err
	}
	// Check if all nodes in the plan are terminal and none failed.
	return s.checkPlanCompletion(ctx, node.PlanID)
}

// FailNode transitions a running node to failed and marks the plan as failed.
func (s *Service) FailNode(ctx context.Context, nodeID string) error {
	if s == nil || s.write == nil {
		return errors.New("planning writer unavailable")
	}
	node, err := s.read.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}
	if !node.CanTransitionTo(planning.NodeStatusFailed) {
		return ErrInvalidTransition
	}
	if err := s.write.UpdateNodeStatus(ctx, nodeID, string(planning.NodeStatusFailed)); err != nil {
		return err
	}
	// Mark the plan as failed since a node failed.
	return s.write.UpdatePlanStatus(ctx, node.PlanID, string(planning.PlanStatusFailed))
}

// PausePlan transitions an active plan to paused.
func (s *Service) PausePlan(ctx context.Context, planID string) error {
	if s == nil || s.write == nil {
		return errors.New("planning writer unavailable")
	}
	plan, err := s.read.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	if plan == nil {
		return ErrPlanNotFound
	}
	if !plan.CanTransitionTo(planning.PlanStatusPaused) {
		return ErrInvalidTransition
	}
	return s.write.UpdatePlanStatus(ctx, planID, string(planning.PlanStatusPaused))
}

// ResumePlan transitions a paused plan back to active.
func (s *Service) ResumePlan(ctx context.Context, planID string) error {
	if s == nil || s.write == nil {
		return errors.New("planning writer unavailable")
	}
	plan, err := s.read.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	if plan == nil {
		return ErrPlanNotFound
	}
	if !plan.CanTransitionTo(planning.PlanStatusActive) {
		return ErrInvalidTransition
	}
	return s.write.UpdatePlanStatus(ctx, planID, string(planning.PlanStatusActive))
}

// CompletePlan transitions an active plan to completed if all nodes are in terminal states.
func (s *Service) CompletePlan(ctx context.Context, planID string) error {
	if s == nil || s.write == nil {
		return errors.New("planning writer unavailable")
	}
	plan, err := s.read.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	if plan == nil {
		return ErrPlanNotFound
	}
	if !plan.CanTransitionTo(planning.PlanStatusCompleted) {
		return ErrInvalidTransition
	}
	nodes, err := s.read.ListNodesByPlan(ctx, planID, 100)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if !node.Status.IsTerminal() {
			return errors.New("plan has non-terminal nodes")
		}
		if node.Status == planning.NodeStatusFailed {
			return errors.New("plan has failed nodes")
		}
	}
	return s.write.UpdatePlanStatus(ctx, planID, string(planning.PlanStatusCompleted))
}

// checkPlanCompletion checks if all nodes are completed and transitions the plan to completed.
func (s *Service) checkPlanCompletion(ctx context.Context, planID string) error {
	nodes, err := s.read.ListNodesByPlan(ctx, planID, 100)
	if err != nil {
		return err
	}
	allCompleted := true
	for _, node := range nodes {
		if !node.Status.IsTerminal() {
			allCompleted = false
			break
		}
	}
	if allCompleted {
		return s.write.UpdatePlanStatus(ctx, planID, string(planning.PlanStatusCompleted))
	}
	return nil
}

// isDependenciesMet returns true if all parent nodes of the given node are completed.
func isDependenciesMet(node planning.Node, nodeMap map[string]*planning.Node) bool {
	if node.ParentNodeID == nil {
		return true
	}
	parent, ok := nodeMap[*node.ParentNodeID]
	if !ok {
		return false
	}
	return parent.Status == planning.NodeStatusCompleted
}

// validateDAG checks that the node list forms a valid DAG (no cycles).
func validateDAG(nodes []planning.Node) error {
	nodeMap := make(map[string]*planning.Node, len(nodes))
	for i := range nodes {
		nodeMap[nodes[i].ID] = &nodes[i]
	}
	// Use color-based cycle detection: white=unvisited, gray=in-progress, black=done.
	color := make(map[string]int, len(nodes))
	var visit func(id string) error
	visit = func(id string) error {
		switch color[id] {
		case 1:
			return ErrCyclicDependency
		case 2:
			return nil
		}
		color[id] = 1
		node := nodeMap[id]
		if node != nil && node.ParentNodeID != nil {
			if _, ok := nodeMap[*node.ParentNodeID]; ok {
				if err := visit(*node.ParentNodeID); err != nil {
					return err
				}
			}
		}
		color[id] = 2
		return nil
	}
	for _, node := range nodes {
		if err := visit(node.ID); err != nil {
			return err
		}
	}
	return nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}
