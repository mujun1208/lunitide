package agentrun

import (
	"errors"
	"fmt"
	"time"
)

// M4-C errors for review decisions and workspace registration/grant/lease.
var (
	// ErrReviewDigestMismatch is returned when the approval digest the
	// reviewer saw does not match the digest the run is paused on. No side
	// effect may happen on mismatch (PRD M4 user story: 摘要变化则无副作用).
	ErrReviewDigestMismatch = errors.New("review approval digest mismatch")
	// ErrWorkspaceInactive is returned when a grant targets a revoked
	// registration or a lease targets a non-active/expired grant.
	ErrWorkspaceInactive = errors.New("workspace registration or grant is not active")
	// ErrChangeSetBaseConflict is returned when the workspace state no
	// longer matches the change set base digest (apply) or the applied
	// digest (revert). The set is marked conflicted; the caller must
	// re-preview instead of retrying (PRD M4: CHANGESET_BASE_CONFLICT).
	ErrChangeSetBaseConflict = errors.New("change set base state has changed")
	ErrReviewRequired        = errors.New("durable unconsumed review approval required")
)

// Run-scoped event types emitted by the M4-E change set flow.
// ChangeSetConflicted is a MUST event per PRD M4 RPC/事件 row.
const (
	EventChangeSetPreviewCompleted = "ChangeSetPreviewCompleted"
	EventChangeSetApplyCompleted   = "ChangeSetApplyCompleted"
	EventChangeSetRevertCompleted  = "ChangeSetRevertCompleted"
	EventChangeSetConflicted       = "ChangeSetConflicted"
)

// Run-scoped event types emitted by the M4-C review flow. The pending
// approval digest a run is paused on is carried by the latest
// EventReviewRequested event; review.decide binds to exactly that digest.
const (
	EventReviewRequested       = "AgentRunReviewRequested"
	EventReviewDecideCompleted = "ReviewDecideCompleted"
)

// Run-scoped event types emitted by the M4-F command flow. JobOutputChunk
// is the MUST terminal output record of a command job: it carries the
// captured (possibly truncated) combined output, its digest and the job's
// terminal status/exit code (PRD M4 RPC/事件 row).
const (
	EventCommandStartCompleted  = "CommandStartCompleted"
	EventCommandCancelCompleted = "CommandCancelCompleted"
	EventCommandJobOutputChunk  = "JobOutputChunk"
)

// Run-scoped event type emitted by the M4-G web flow. EvidenceRecorded is a
// MUST event per PRD M4 RPC/事件 row: it carries the provenance triple
// (source URI, capture time, content digest) of externally fetched content.
const (
	EventEvidenceRecorded = "EvidenceRecorded"
)

// Run-scoped event type emitted by the M4-H plan flow. RunPlanPutCompleted
// is a MUST event per PRD M4 FR→Bridge→事件 row: it carries the committed
// plan digest and version so reviewers can detect plan drift between turns.
const (
	EventRunPlanPutCompleted = "RunPlanPutCompleted"
)

// RegistrationStatus is the lifecycle of a workspace registration.
type RegistrationStatus string

const (
	RegistrationActive  RegistrationStatus = "active"
	RegistrationRevoked RegistrationStatus = "revoked"
)

// WorkspaceRegistration binds a canonical file system root to a durable
// identity. CanonicalRoot is unique and immutable after registration.
type WorkspaceRegistration struct {
	ID            string             `json:"id"`
	CanonicalRoot string             `json:"canonicalRoot"`
	RootDigest    string             `json:"rootDigest"`
	Status        RegistrationStatus `json:"status"`
	Version       int64              `json:"version"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

func (w WorkspaceRegistration) Validate() error {
	if !canonicalULID(w.ID) {
		return fmt.Errorf("%w: registration ID must be an uppercase canonical ULID", ErrInvalid)
	}
	if len(w.CanonicalRoot) < 1 || len(w.CanonicalRoot) > 1024 {
		return fmt.Errorf("%w: canonical root length", ErrInvalid)
	}
	if !validHexDigest(w.RootDigest) {
		return fmt.Errorf("%w: root_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	if w.Status != RegistrationActive && w.Status != RegistrationRevoked {
		return fmt.Errorf("%w: unknown registration status %q", ErrInvalid, w.Status)
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.Before(w.CreatedAt) || w.Version < 1 {
		return fmt.Errorf("%w: registration timestamps/version", ErrInvalid)
	}
	return nil
}

// GrantStatus is the lifecycle of a workspace grant.
type GrantStatus string

const (
	GrantActive  GrantStatus = "active"
	GrantExpired GrantStatus = "expired"
	GrantRevoked GrantStatus = "revoked"
)

// WorkspaceGrant scopes access to a registered workspace until ExpiresAt.
// Scope is canonical JSON describing allowed paths/operations.
type WorkspaceGrant struct {
	ID             string      `json:"id"`
	RegistrationID string      `json:"registrationId"`
	Scope          []byte      `json:"scope"` // canonical JSON
	ExpiresAt      time.Time   `json:"expiresAt"`
	Status         GrantStatus `json:"status"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}

func (g WorkspaceGrant) Validate() error {
	if !canonicalULID(g.ID) || !canonicalULID(g.RegistrationID) {
		return fmt.Errorf("%w: grant IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if len(g.Scope) < 2 {
		return fmt.Errorf("%w: grant scope must be valid JSON", ErrInvalid)
	}
	switch g.Status {
	case GrantActive, GrantExpired, GrantRevoked:
	default:
		return fmt.Errorf("%w: unknown grant status %q", ErrInvalid, g.Status)
	}
	if g.ExpiresAt.IsZero() || g.CreatedAt.IsZero() || g.UpdatedAt.Before(g.CreatedAt) {
		return fmt.Errorf("%w: grant timestamps", ErrInvalid)
	}
	return nil
}

// UsableAt reports whether the grant authorizes access at the given time.
func (g WorkspaceGrant) UsableAt(at time.Time) bool {
	return g.Status == GrantActive && at.Before(g.ExpiresAt)
}

// LeaseStatus is the lifecycle of a workspace lease.
type LeaseStatus string

const (
	LeaseActive   LeaseStatus = "active"
	LeaseExpired  LeaseStatus = "expired"
	LeaseReleased LeaseStatus = "released"
)

// WorkspaceLease is a fenced handle on a grant. FencingToken increases
// monotonically per grant; after expiry every handle call must fail.
type WorkspaceLease struct {
	ID           string      `json:"id"`
	GrantID      string      `json:"grantId"`
	FencingToken int64       `json:"fencingToken"`
	ExpiresAt    time.Time   `json:"expiresAt"`
	Status       LeaseStatus `json:"status"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

func (l WorkspaceLease) Validate() error {
	if !canonicalULID(l.ID) || !canonicalULID(l.GrantID) {
		return fmt.Errorf("%w: lease IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if l.FencingToken < 1 {
		return fmt.Errorf("%w: fencing token must be positive", ErrInvalid)
	}
	switch l.Status {
	case LeaseActive, LeaseExpired, LeaseReleased:
	default:
		return fmt.Errorf("%w: unknown lease status %q", ErrInvalid, l.Status)
	}
	if l.ExpiresAt.IsZero() || l.CreatedAt.IsZero() || l.UpdatedAt.Before(l.CreatedAt) {
		return fmt.Errorf("%w: lease timestamps", ErrInvalid)
	}
	return nil
}

// UsableAt reports whether the lease authorizes handle calls at the time.
func (l WorkspaceLease) UsableAt(at time.Time) bool {
	return l.Status == LeaseActive && at.Before(l.ExpiresAt)
}

// ChangeSetStatus is the lifecycle of a change set.
type ChangeSetStatus string

const (
	ChangeSetDraft      ChangeSetStatus = "draft"
	ChangeSetPreviewed  ChangeSetStatus = "previewed"
	ChangeSetApproved   ChangeSetStatus = "approved"
	ChangeSetApplied    ChangeSetStatus = "applied"
	ChangeSetReverted   ChangeSetStatus = "reverted"
	ChangeSetConflicted ChangeSetStatus = "conflicted"
)

// validChangeSetTransition encodes draft→previewed→approved→applied→
// reverted|conflicted. A base-digest CAS failure during apply marks the set
// conflicted instead of applying.
func validChangeSetTransition(from, to ChangeSetStatus) bool {
	switch from {
	case ChangeSetDraft:
		return to == ChangeSetPreviewed
	case ChangeSetPreviewed:
		return to == ChangeSetApproved
	case ChangeSetApproved:
		return to == ChangeSetApplied || to == ChangeSetConflicted
	case ChangeSetApplied:
		return to == ChangeSetReverted || to == ChangeSetConflicted
	}
	return false
}

// Terminal reports whether the change set state is final.
func (s ChangeSetStatus) Terminal() bool { return s == ChangeSetReverted || s == ChangeSetConflicted }

// ChangeSet is a reviewed, digest-bound set of file system changes.
// BaseDigest pins the workspace state the changes were computed against;
// ApprovalDigest binds the exact approved preview and any change invalidates it.
type ChangeSet struct {
	ID             string          `json:"id"`
	RunID          string          `json:"runId"`
	BaseDigest     string          `json:"baseDigest"`
	ApprovalDigest string          `json:"approvalDigest"`
	Status         ChangeSetStatus `json:"status"`
	Version        int64           `json:"version"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func (c ChangeSet) Validate() error {
	if !canonicalULID(c.ID) || !canonicalULID(c.RunID) {
		return fmt.Errorf("%w: change set IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if !validHexDigest(c.BaseDigest) || !validHexDigest(c.ApprovalDigest) {
		return fmt.Errorf("%w: change set digests must be lowercase sha256 hex", ErrInvalid)
	}
	switch c.Status {
	case ChangeSetDraft, ChangeSetPreviewed, ChangeSetApproved,
		ChangeSetApplied, ChangeSetReverted, ChangeSetConflicted:
	default:
		return fmt.Errorf("%w: unknown change set status %q", ErrInvalid, c.Status)
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) || c.Version < 1 {
		return fmt.Errorf("%w: change set timestamps/version", ErrInvalid)
	}
	return nil
}

// Transition returns a copy of the change set moved to the target status.
func (c ChangeSet) Transition(to ChangeSetStatus, at time.Time) (ChangeSet, error) {
	if c.Status.Terminal() {
		return c, ErrTerminal
	}
	if !validChangeSetTransition(c.Status, to) {
		return c, fmt.Errorf("%w: change set %s -> %s", ErrInvalidTransition, c.Status, to)
	}
	c.Status = to
	c.Version++
	c.UpdatedAt = at
	return c, nil
}

// ChangeSetOp is one file system mutation inside a change set.
type ChangeSetOp string

const (
	ChangeSetOpCreate ChangeSetOp = "create"
	ChangeSetOpUpdate ChangeSetOp = "update"
	ChangeSetOpDelete ChangeSetOp = "delete"
)

// ChangeSetOperation is one immutable, ordered per-path plan row. Content
// holds the desired UTF-8 text for create/update; OriginalContent snapshots
// the pre-apply file (revert source); AppliedDigest records the post-apply
// file digest (revert CAS). Ordinal starts at 1 and is unique per set.
type ChangeSetOperation struct {
	ID              string      `json:"id"`
	ChangeSetID     string      `json:"changeSetId"`
	Ordinal         int64       `json:"ordinal"`
	Op              ChangeSetOp `json:"op"`
	Path            string      `json:"path"`
	Content         *string     `json:"content,omitempty"`
	ContentDigest   string      `json:"contentDigest,omitempty"`
	OriginalContent *string     `json:"originalContent,omitempty"`
	OriginalDigest  string      `json:"originalDigest,omitempty"`
	AppliedDigest   string      `json:"appliedDigest,omitempty"`
}

func (o ChangeSetOperation) Validate() error {
	if !canonicalULID(o.ID) || !canonicalULID(o.ChangeSetID) {
		return fmt.Errorf("%w: change set operation IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if o.Ordinal < 1 {
		return fmt.Errorf("%w: operation ordinal must be positive", ErrInvalid)
	}
	switch o.Op {
	case ChangeSetOpCreate, ChangeSetOpUpdate:
		if o.Content == nil || !validHexDigest(o.ContentDigest) {
			return fmt.Errorf("%w: %s operations require content and its digest", ErrInvalid, o.Op)
		}
	case ChangeSetOpDelete:
		if o.Content != nil || o.ContentDigest != "" {
			return fmt.Errorf("%w: delete operations carry no content", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown change set op %q", ErrInvalid, o.Op)
	}
	if len(o.Path) < 1 || len(o.Path) > 512 {
		return fmt.Errorf("%w: operation path length", ErrInvalid)
	}
	if o.OriginalDigest != "" && !validHexDigest(o.OriginalDigest) {
		return fmt.Errorf("%w: original_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	if o.AppliedDigest != "" && !validHexDigest(o.AppliedDigest) {
		return fmt.Errorf("%w: applied_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	if o.OriginalContent == nil && o.OriginalDigest != "" {
		return fmt.Errorf("%w: original digest requires original content", ErrInvalid)
	}
	return nil
}

// JobStatus is the lifecycle of a command job.
type JobStatus string

const (
	JobQueued         JobStatus = "queued"
	JobRunning        JobStatus = "running"
	JobCompleted      JobStatus = "completed"
	JobFailed         JobStatus = "failed"
	JobCancelled      JobStatus = "cancelled"
	JobOutcomeUnknown JobStatus = "outcome_unknown"
)

func (s JobStatus) Terminal() bool {
	switch s {
	case JobCompleted, JobFailed, JobCancelled, JobOutcomeUnknown:
		return true
	}
	return false
}

// CommandJob is one execution of a signed CommandSpec. The job never stores
// a shell command line; it binds the immutable spec by digest.
type CommandJob struct {
	ID                string    `json:"id"`
	RunID             string    `json:"runId"`
	CommandSpecDigest string    `json:"commandSpecDigest"`
	Status            JobStatus `json:"status"`
	ExitCode          *int64    `json:"exitCode,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func (j CommandJob) Validate() error {
	if !canonicalULID(j.ID) || !canonicalULID(j.RunID) {
		return fmt.Errorf("%w: command job IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if !validHexDigest(j.CommandSpecDigest) {
		return fmt.Errorf("%w: command_spec_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	switch j.Status {
	case JobQueued, JobRunning, JobCompleted, JobFailed, JobCancelled, JobOutcomeUnknown:
	default:
		return fmt.Errorf("%w: unknown command job status %q", ErrInvalid, j.Status)
	}
	if j.ExitCode != nil && j.Status != JobCompleted && j.Status != JobFailed {
		return fmt.Errorf("%w: exit code only set for completed/failed jobs", ErrInvalid)
	}
	if j.CreatedAt.IsZero() || j.UpdatedAt.Before(j.CreatedAt) {
		return fmt.Errorf("%w: command job timestamps", ErrInvalid)
	}
	return nil
}

// Transition returns a copy of the job moved to the target status.
func (j CommandJob) Transition(to JobStatus, exitCode *int64, at time.Time) (CommandJob, error) {
	if j.Status.Terminal() {
		return j, ErrTerminal
	}
	ok := false
	switch j.Status {
	case JobQueued:
		ok = to == JobRunning || to == JobCancelled
	case JobRunning:
		ok = to == JobCompleted || to == JobFailed || to == JobCancelled || to == JobOutcomeUnknown
	}
	if !ok {
		return j, fmt.Errorf("%w: command job %s -> %s", ErrInvalidTransition, j.Status, to)
	}
	j.Status = to
	j.ExitCode = exitCode
	j.UpdatedAt = at
	return j, nil
}

// RunPlan is the agent-authored plan projection for a run. Exactly one plan
// exists per run; updates are version-checked.
type RunPlan struct {
	ID         string    `json:"id"`
	RunID      string    `json:"runId"`
	PlanDigest string    `json:"planDigest"`
	Content    []byte    `json:"content"` // canonical JSON
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (p RunPlan) Validate() error {
	if !canonicalULID(p.ID) || !canonicalULID(p.RunID) {
		return fmt.Errorf("%w: run plan IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if !validHexDigest(p.PlanDigest) {
		return fmt.Errorf("%w: plan_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	if len(p.Content) < 2 {
		return fmt.Errorf("%w: plan content must be valid JSON", ErrInvalid)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) || p.Version < 1 {
		return fmt.Errorf("%w: run plan timestamps/version", ErrInvalid)
	}
	return nil
}

// Evidence is an append-only provenance record binding external content to a
// run by source URI, capture time and content digest.
type Evidence struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	Kind          string    `json:"kind"`
	SourceURI     string    `json:"sourceUri"`
	ContentDigest string    `json:"contentDigest"`
	CapturedAt    time.Time `json:"capturedAt"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (e Evidence) Validate() error {
	if !canonicalULID(e.ID) || !canonicalULID(e.RunID) {
		return fmt.Errorf("%w: evidence IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if len(e.Kind) < 1 || len(e.Kind) > 64 {
		return fmt.Errorf("%w: evidence kind length", ErrInvalid)
	}
	if len(e.SourceURI) < 1 || len(e.SourceURI) > 2048 {
		return fmt.Errorf("%w: evidence source URI length", ErrInvalid)
	}
	if !validHexDigest(e.ContentDigest) {
		return fmt.Errorf("%w: evidence content_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	if e.CapturedAt.IsZero() || e.CreatedAt.IsZero() {
		return fmt.Errorf("%w: evidence timestamps", ErrInvalid)
	}
	return nil
}

// ReviewDecision is the outcome of a run review.
type ReviewDecision string

const (
	ReviewApproved ReviewDecision = "approved"
	ReviewRejected ReviewDecision = "rejected"
)

// RunReview is an append-only approval/rejection record bound to an exact
// approval digest. Approving a different digest is a different review.
type RunReview struct {
	ID               string         `json:"id"`
	RunID            string         `json:"runId"`
	ApprovalDigest   string         `json:"approvalDigest"`
	Decision         ReviewDecision `json:"decision"`
	DecidedBy        string         `json:"decidedBy"`
	DecidedAt        time.Time      `json:"decidedAt"`
	CreatedAt        time.Time      `json:"createdAt"`
	Action           string         `json:"action,omitempty"`
	ResourceDigest   string         `json:"resourceDigest,omitempty"`
	BaseDigest       string         `json:"baseDigest,omitempty"`
	ConfigDigest     string         `json:"configDigest,omitempty"`
	PolicyDigest     string         `json:"policyDigest,omitempty"`
	DescriptorDigest string         `json:"descriptorDigest,omitempty"`
	ConsumedAt       *time.Time     `json:"consumedAt,omitempty"`
}

func (r RunReview) Validate() error {
	if !canonicalULID(r.ID) || !canonicalULID(r.RunID) {
		return fmt.Errorf("%w: review IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if !validHexDigest(r.ApprovalDigest) {
		return fmt.Errorf("%w: approval_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	if r.Decision != ReviewApproved && r.Decision != ReviewRejected {
		return fmt.Errorf("%w: unknown review decision %q", ErrInvalid, r.Decision)
	}
	if len(r.DecidedBy) < 1 || len(r.DecidedBy) > 128 {
		return fmt.Errorf("%w: review decided_by length", ErrInvalid)
	}
	if r.DecidedAt.IsZero() || r.CreatedAt.IsZero() {
		return fmt.Errorf("%w: review timestamps", ErrInvalid)
	}
	return nil
}
