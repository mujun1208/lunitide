// M7 slice 2 domain values (T-7.2.x): the evidence graph. Everything in this
// file is append-only at the storage layer (M7-EVD-001) except dev_tasks,
// which carries its own state machine with optimistic locking. State strings
// mirror the CHECK constraints in migrations/0052.
package m7flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrDanglingEdge: a trace endpoint does not exist (TRC-001).
	ErrDanglingEdge = errors.New("m7flow: trace edge endpoint missing")
	// ErrStaleOutstanding: unresolved stale marks block the gate (TRC-002).
	ErrStaleOutstanding = errors.New("m7flow: stale marks outstanding")
	// ErrGateInputsIncomplete: required gate inputs missing (GAT-001).
	ErrGateInputsIncomplete = errors.New("m7flow: gate inputs incomplete")
	// ErrCheckpointDenied: gate not PASS, checkpoint refused (CHK-001).
	ErrCheckpointDenied = errors.New("m7flow: gate not PASS")
	// ErrSelfReview: reviewer equals author (REV-001).
	ErrSelfReview = errors.New("m7flow: self review forbidden")
	// ErrBadRelation / ErrBadResolution / ErrBadVerdict: enum violations.
	ErrBadRelation   = errors.New("m7flow: illegal trace relation")
	ErrBadResolution = errors.New("m7flow: illegal stale resolution type")
	ErrBadVerdict    = errors.New("m7flow: illegal review verdict")
)

// Trace relations (trace_edges.relation CHECK set).
const (
	RelImplements  = "implements"
	RelVerifies    = "verifies"
	RelTracesTo    = "traces_to"
	RelDerivedFrom = "derived_from"
	RelReviews     = "reviews"
	RelProduces    = "produces"
	RelPromotes    = "promotes"
)

// LegalRelation guards the canonical relation allowlist.
func LegalRelation(r string) bool {
	switch r {
	case RelImplements, RelVerifies, RelTracesTo, RelDerivedFrom,
		RelReviews, RelProduces, RelPromotes:
		return true
	}
	return false
}

// NodeRef is one heterogeneous trace endpoint.
type NodeRef struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// TraceEdge is one directed evidence edge; both endpoints carry the digest
// they had when the edge was created, so later digest drift is detectable.
type TraceEdge struct {
	ID        string
	FromType  string
	FromID    string
	FromDigest string
	Relation  string
	ToType    string
	ToID      string
	ToDigest  string
	CreatedAt time.Time
}

// Review verdicts.
const (
	VerdictApprove = "approve"
	VerdictReject  = "reject"
)

// Review is one immutable review record; reviewer must differ from the
// subject author (REV-001 is enforced by the service with author context).
type Review struct {
	ID             string
	SubjectType    string
	SubjectID      string
	SubjectVersion int64
	Verdict        string
	ReviewerID     string
	Reason         string
	CreatedAt      time.Time
}

// StaleMark records that an upstream digest drifted; clearing is a separate
// append-only resolution row, never an update.
type StaleMark struct {
	ID         string
	SubjectType string
	SubjectID  string
	CauseEdge  string
	DetectedAt time.Time
}

// Stale resolution types.
const (
	ResolveRecaptured = "recaptured"
	ResolveReevaluated = "reevaluated"
	ResolveWaived     = "waived"
)

// StaleResolution closes one stale mark.
type StaleResolution struct {
	ID             string
	StaleMarkID    string
	ResolutionType string
	ReevaluationID string
	ResolvedBy     string
	ResolvedAt     time.Time
}

// Gate decisions.
const (
	GatePass    = "PASS"
	GateFail    = "FAIL"
	GateBlocked = "BLOCKED"
)

// Finding is one structured, renderable gate finding.
type Finding struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity,omitempty"`
	Ref      string `json:"ref,omitempty"`
}

// GateEvaluation is one server-side recomputation of a gate for a stage run.
type GateEvaluation struct {
	ID          string
	StageRunID  string
	GateKey     string
	InputDigest string
	Decision    string
	Findings    []Finding
	CreatedAt   time.Time
}

// Checkpoint is one replayable stage completion point; only Gate PASS may
// create one (CHK-001) and the sequence is per-run monotonic.
type Checkpoint struct {
	ID             string
	StageRunID     string
	SnapshotDigest string
	TraceRoot      string
	Sequence       int64
	CreatedAt      time.Time
}

// DevTask states (dev_tasks.state CHECK set).
const (
	TaskDraft     = "draft"
	TaskReady     = "ready"
	TaskInProgress = "in_progress"
	TaskBlocked   = "blocked"
	TaskInReview  = "in_review"
	TaskDone      = "done"
	TaskReopened  = "reopened"
	TaskCancelled = "cancelled"
)

// taskTransitions is the legal dev-task state machine.
var taskTransitions = map[string]map[string]bool{
	TaskDraft:     {TaskReady: true, TaskCancelled: true},
	TaskReady:     {TaskInProgress: true, TaskBlocked: true, TaskCancelled: true},
	TaskInProgress: {TaskInReview: true, TaskBlocked: true, TaskCancelled: true},
	TaskBlocked:   {TaskInProgress: true, TaskCancelled: true},
	TaskInReview:  {TaskDone: true, TaskReopened: true, TaskCancelled: true},
	TaskDone:      {TaskReopened: true},
	TaskReopened:  {TaskInProgress: true, TaskCancelled: true},
	TaskCancelled: {},
}

// LegalTaskTransition guards the dev-task state machine.
func LegalTaskTransition(from, to string) bool { return taskTransitions[from][to] }

// Task priorities and risks.
const (
	PriorityP0 = "P0"
	PriorityP1 = "P1"
	PriorityP2 = "P2"
	PriorityP3 = "P3"
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// DevTask is one development task bound to a stage run.
type DevTask struct {
	ID               string
	StageRunID       string
	Title            string
	State            string
	Priority         string
	Risk             string
	AcceptanceDigest string
	AssigneeID       string
	StateReason      string
	BlockReason      string
	BlockerRef       string
	LockVersion      int64
	TraceEdgeID      string
	CreatedAt        time.Time
}

// TestRun is one executed test evidence row.
type TestRun struct {
	ID           string
	TaskRef      string
	Result       string // pass|fail|error|timeout
	ReportDigest string
	CreatedAt    time.Time
}

// ScanRun is one executed scan evidence row.
type ScanRun struct {
	ID           string
	TaskRef      string
	Scanner      string
	SeverityGate string
	ReportDigest string
	CreatedAt    time.Time
}

// ArtifactDerivation records artifact-to-artifact lineage.
type ArtifactDerivation struct {
	ID                string
	ArtifactVersionID string
	DerivedFromVersion string
	Relation          string // derived_from|rebuilt_from|supersedes
	CreatedAt         time.Time
}

// ReproductionManifest pins how an artifact can be reproduced.
type ReproductionManifest struct {
	ID                string
	ArtifactVersionID string
	ManifestJSON      string
	Digest            string
	CreatedAt         time.Time
}

// EvaluationBaseline pins an evaluation baseline for a scope.
type EvaluationBaseline struct {
	ID           string
	ScopeType    string
	ScopeID      string
	BaselineJSON string
	Digest       string
	CreatedAt    time.Time
}

// Digest256 hashes canonical JSON (sorted keys, no whitespace) of v.
func Digest256(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// FindingsJSON renders findings canonically for storage.
func FindingsJSON(fs []Finding) string {
	if fs == nil {
		fs = []Finding{}
	}
	b, _ := json.Marshal(fs)
	return string(b)
}

// ParseFindings decodes stored findings JSON.
func ParseFindings(s string) ([]Finding, error) {
	var fs []Finding
	if err := json.Unmarshal([]byte(s), &fs); err != nil {
		return nil, err
	}
	return fs, nil
}
