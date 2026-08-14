package agentrun

import (
	"context"
	"time"
)

// Repository persists the M4 agent runtime aggregates. All writes for one
// logical operation (state transition + events + effect journal + outbox)
// happen inside a single Transact call so SQLite remains the single writer.
type Repository interface {
	Transact(ctx context.Context, fn func(Tx) error) error
}

// Tx is the single-writer transaction boundary for the agent runtime.
// Implementations must enforce CAS semantics and first-terminal-wins.
type Tx interface {
	// Runs
	PutRun(AgentRun) error
	GetRun(id string) (AgentRun, error)
	ListRunsBySession(sessionID string) ([]AgentRun, error)
	ListActiveRuns() ([]AgentRun, error)
	// TransitionRun applies a domain-validated status transition with a
	// version CAS. It returns ErrVersionConflict when expectedVersion is
	// stale, ErrNotFound when the run does not exist, and the domain
	// transition error when the move is illegal.
	TransitionRun(id string, expectedVersion int64, to RunStatus, at time.Time) (AgentRun, error)
	ReplaceBudget(id string, expectedVersion int64, budget Budget, at time.Time) (AgentRun, error)
	ReserveUsage(runID, reservationID string, delta Usage, at time.Time) (AgentRun, error)
	ActiveReservedUsage(runID string) (Usage, error)
	CommitUsage(runID, reservationID string, actual Usage, at time.Time) (AgentRun, error)
	RecoverRun(id string, to RunStatus, at time.Time) (AgentRun, error)

	// Turns and steps
	PutTurn(AgentTurn) error
	GetTurn(id string) (AgentTurn, error)
	ListTurns(runID string) ([]AgentTurn, error)
	PutStep(AgentStep) error
	ListSteps(turnID string) ([]AgentStep, error)
	ListRunningSteps() ([]AgentStep, error)
	RunIDForStep(stepID string) (string, error)

	// Tool calls and observations (observations are append-only)
	PutToolCall(ToolCall) error
	GetToolCall(id string) (ToolCall, error)
	ListToolCalls(stepID string) ([]ToolCall, error)
	ListActiveToolCalls() ([]ToolCall, error)
	AppendObservation(Observation) error
	ListObservations(stepID string) ([]Observation, error)

	// Effect journal (effect_key is globally unique for idempotency)
	PutEffect(EffectJournal) error
	GetEffectByKey(effectKey string) (EffectJournal, error)
	ListEffects(runID string) ([]EffectJournal, error)
	ListPreparedEffects() ([]EffectJournal, error)

	// Run events (append-only, UNIQUE(run_id, sequence))
	AppendEvent(RunEvent) error
	ListEvents(runID string) ([]RunEvent, error)

	// Workspace registration / grant / lease
	PutRegistration(WorkspaceRegistration) error
	GetRegistration(id string) (WorkspaceRegistration, error)
	GetRegistrationByRoot(canonicalRoot string) (WorkspaceRegistration, error)
	PutGrant(WorkspaceGrant) error
	GetGrant(id string) (WorkspaceGrant, error)
	// NextFencingToken returns the next monotonically increasing fencing
	// token for the grant (max(existing)+1, starting at 1).
	NextFencingToken(grantID string) (int64, error)
	PutLease(WorkspaceLease) error
	GetLease(id string) (WorkspaceLease, error)

	// Change sets
	PutChangeSet(ChangeSet) error
	GetChangeSet(id string) (ChangeSet, error)
	// Change set operations are the ordered, immutable per-path plan rows;
	// applied_digest is updated once when the apply commits.
	PutChangeSetOperation(ChangeSetOperation) error
	ListChangeSetOperations(changeSetID string) ([]ChangeSetOperation, error)

	// Command jobs
	PutCommandJob(CommandJob) error
	GetCommandJob(id string) (CommandJob, error)
	// ListActiveCommandJobs returns every job in a non-terminal state
	// (queued/running). After an engine crash these are the jobs whose
	// external effect is unprovable; reconcile resolves them to
	// outcome_unknown and never retries them.
	ListActiveCommandJobs() ([]CommandJob, error)

	// Run plan (exactly one per run)
	PutRunPlan(RunPlan) error
	GetRunPlan(runID string) (RunPlan, error)

	// Evidence (append-only)
	AppendEvidence(Evidence) error
	ListEvidence(runID string) ([]Evidence, error)

	// Reviews (append-only)
	AppendReview(RunReview) error
	ListReviews(runID string) ([]RunReview, error)
	ConsumeReview(runID, approvalDigest, action string, at time.Time) (RunReview, error)
}
