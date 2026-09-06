package agentrun

import "time"

// PlanExecution binds a project task to one durable runtime execution. Its
// terminal state and verified evidence are committed with the runtime state.
type PlanExecution struct {
	PlanRunID string            `json:"planRunId"`
	RunID     string            `json:"agentRunId"`
	SessionID string            `json:"sessionId"`
	Spec      PlanExecutionSpec `json:"spec"`
	Status    string            `json:"status"`
	Summary   string            `json:"summary"`
	Failure   string            `json:"failure"`
	Artifacts []PlanArtifact    `json:"artifacts"`
	Version   int64             `json:"version"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type PlanExecutionSpec struct {
	ProjectID     string `json:"projectId"`
	PlanID        string `json:"planId"`
	NodeID        string `json:"nodeId"`
	Role          string `json:"role"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	ProviderID    string `json:"providerId"`
	ModelID       string `json:"modelId"`
	Budget        Budget `json:"budget"`
	PreviousRunID string `json:"previousRunId,omitempty"`
}

type PlanArtifact struct {
	Path   string `json:"path"`
	Digest string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

func (p PlanExecution) Terminal() bool {
	switch p.Status {
	case "succeeded", "failed", "cancelled", "interrupted", "outcome_unknown":
		return true
	}
	return false
}

// PlanExecutionTx is implemented alongside the runtime transaction so task
// identity, session, execution, events and completion never split commits.
type PlanExecutionTx interface {
	GetPlanExecution(planRunID string) (PlanExecution, error)
	ListPlanExecutions() ([]PlanExecution, error)
	LoadPlanExecutionSpec(planRunID string) (PlanExecutionSpec, error)
	CheckPlanExecutionReady(planRunID string, at time.Time) error
	CreatePlanExecutionSession(id, projectID, title string, at time.Time) error
	PutPlanExecution(PlanExecution, int64) error
	ReopenPlanExecution(PlanExecution) error
	PreparePlanExecutionReview(string, time.Time) (bool, error)
}
