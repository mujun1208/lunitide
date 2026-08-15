// Package m6supply holds the M6 supply-chain domain values (T-6.1.x): the
// verified extension artifact catalog, per-subject installs and the MCP
// endpoint registry rows. All state strings mirror the CHECK constraints in
// migrations 0044/0046; changing a state set needs both a migration and an
// ADR.
package m6supply

import (
	"errors"
	"time"
)

var (
	// ErrNotFound answers any missing artifact/install/endpoint lookup.
	ErrNotFound = errors.New("m6supply: entity not found")
	// ErrVersionConflict is the optimistic-concurrency guard for installs
	// and endpoints.
	ErrVersionConflict = errors.New("m6supply: version conflict")
	// ErrInvalidTransition guards illegal lifecycle transitions.
	ErrInvalidTransition = errors.New("m6supply: transition not allowed")
)

// Artifact signature outcomes (m6_extension_artifact.signature_state).
const (
	SignatureVerified = "verified"
	SignatureFailed   = "failed"
	SignatureRevoked  = "revoked"
)

// Risk tiers (m6_extension_artifact.risk). extension.search refuses to
// surface high-risk artifacts: maxRisk filters to low/medium only.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// RiskRank orders risk tiers for maxRisk filtering; high stays last and is
// rejected by the wire layer.
func RiskRank(r string) int {
	switch r {
	case RiskLow:
		return 0
	case RiskMedium:
		return 1
	default:
		return 2
	}
}

// Artifact is one immutable verified supply-chain record. The digest is the
// sha-256 of the artifact bytes; manifest_json is the canonical signed
// manifest snapshot.
type Artifact struct {
	ID             string
	Name           string
	Publisher      string
	Version        string
	Digest         string
	SignatureState string
	SBOMRef        string
	ManifestJSON   string
	Risk           string
	CreatedAt      time.Time
}

// Install states (m6_extension_install.state CHECK set).
type InstallState string

const (
	InstallDiscovered  InstallState = "discovered"
	InstallVerifying   InstallState = "verifying"
	InstallInstalled   InstallState = "installed"
	InstallEnabled     InstallState = "enabled"
	InstallPaused      InstallState = "paused"
	InstallBlocked     InstallState = "blocked"
	InstallQuarantined InstallState = "quarantined"
	InstallUninstalled InstallState = "uninstalled"
	InstallRollingBack InstallState = "rolling_back"
)

// Terminal reports whether the install left the mutable lifecycle.
func (s InstallState) Terminal() bool {
	return s == InstallUninstalled
}

// ValidLifecycleOp maps a wire op onto the target state for from-state
// validation (extension.lifecycle schema enum).
func ValidLifecycleOp(op string) bool {
	switch op {
	case "enable", "disable", "pause", "upgrade", "rollback", "uninstall":
		return true
	}
	return false
}

// Install is one subject's install of an artifact. Scope mirrors the M6
// supply-chain scope set (personal / ad_hoc / project).
type Install struct {
	ID                  string
	ArtifactID          string
	Subject             string
	Scope               string
	ProjectID           string
	State               InstallState
	PermissionGrantJSON string
	Version             int64
	InstalledAt         time.Time
	UpdatedAt           time.Time
}

// Endpoint states (m6_mcp_endpoint.state CHECK set). Wire semantics live in
// internal/mcp6; this row is the durable mirror.
const (
	EndpointRegistered = "registered"
	EndpointProbe      = "probe"
	EndpointReady      = "ready"
	EndpointDegraded   = "degraded"
	EndpointRevoked    = "revoked"
	EndpointDisabled   = "disabled"
)

// Endpoint is one registered MCP endpoint row.
type Endpoint struct {
	ID                string
	Transport         string
	URL               string
	AuthRef           string
	CapabilityPinJSON string
	State             string
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Metadata scopes (m6_connector_catalog.metadata_scope CHECK set, 0045).
// The frozen enum is mirrored by internal/connector.MetadataScopes; a scope
// outside it is M6-DB-002.
const (
	ScopeCatalog    = "catalog"
	ScopeSchema     = "schema"
	ScopeTable      = "table"
	ScopeColumn     = "column"
	ScopeIndex      = "index"
	ScopeConstraint = "constraint"
)

// ConnectorSnapshot is one read-only metadata snapshot (T-6.2.1). Only
// metadata lands in objects_json — the adapter layer's double allowlist
// (statement + driver method) rejects business rows before anything is
// fetched; snapshot_version is per-connector monotonic.
type ConnectorSnapshot struct {
	ID              string
	ConnectorID     string
	Scope           string
	SnapshotVersion int64
	MetadataScope   string
	ObjectsJSON     string
	FetchedAt       time.Time
}

// Cloud task states (m6_cloud_task.state CHECK set, 0046). Same key one
// effect; a lease may be taken over only after expiry — TSK-001/TSK-002
// govern the transitions.
const (
	CloudTaskCreated   = "created"
	CloudTaskQueued    = "queued"
	CloudTaskLeased    = "leased"
	CloudTaskRunning   = "running"
	CloudTaskJoining   = "joining"
	CloudTaskSucceeded = "succeeded"
	CloudTaskFailed    = "failed"
	CloudTaskCancelled = "cancelled"
)

// CloudTask is one durable cloud task row (T-6.2.2/T-6.3.1). The
// idempotency key derives from (jobSpecDigest, budgetLeaseId);
// payload_digest makes same-key-different-payload replays fail as
// M6-TSK-001 instead of silently returning the first result.
type CloudTask struct {
	ID             string
	IdempotencyKey string
	PayloadDigest  string
	LeaseOwner     string
	LeaseExpiresAt *time.Time
	Attempt        int64
	State          string
	ResultRef      string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Delegation states (m6_delegation.state CHECK set, 0046). The child walk is
// planned -> grant_reserved -> dispatched -> arrived -> settled; rejected and
// expired are terminal side exits (M6-DLG-001 verdict or deadline overrun).
const (
	DelegationPlanned       = "planned"
	DelegationGrantReserved = "grant_reserved"
	DelegationDispatched    = "dispatched"
	DelegationArrived       = "arrived"
	DelegationSettled       = "settled"
	DelegationRejected      = "rejected"
	DelegationExpired       = "expired"
)

// DelegationActive reports whether the row still counts against the fan-out
// and tree-child caps (settled/rejected/expired rows free their slot).
func DelegationActive(state string) bool {
	switch state {
	case DelegationPlanned, DelegationGrantReserved, DelegationDispatched, DelegationArrived:
		return true
	}
	return false
}

// Delegation is one durable delegation envelope record (T-6.3.2). The
// canonical envelope JSON plus its digest is stored so the signature chain
// can be replayed after a crash; UNIQUE(root_id, nonce) is the anti-replay
// backstop for the nonce the control plane minted.
type Delegation struct {
	ID             string
	RootID         string
	ParentID       string
	ChildTaskID    string
	Envelope       string
	EnvelopeDigest string
	Nonce          string
	Depth          int
	State          string
	Version        int64
	CreatedAt      time.Time
	SettledAt      *time.Time
	UpdatedAt      time.Time
}

// Budget dimensions (m6_budget_account.dimension CHECK set, 0046). The
// envelope's wire fields map 1:1: cpuSeconds->cpu_seconds, tokens->tokens,
// cost->cost, wallClockMs->wall_clock.
const (
	BudgetCPUSeconds = "cpu_seconds"
	BudgetTokens     = "tokens"
	BudgetCost       = "cost"
	BudgetWallClock  = "wall_clock"
)

// BudgetDimensions is the frozen four-dimension set in ledger order.
var BudgetDimensions = []string{BudgetCPUSeconds, BudgetTokens, BudgetCost, BudgetWallClock}

// BudgetAccount is one (root, dimension) conservation row (T-6.3.3). The
// DB-level CHECK(granted = reserved + consumed + refundable) mirrors the
// ledger invariant: every granted unit sits in exactly one of the three
// buckets. A reserve moves granted+reserved; consume moves reserved->
// consumed; a refund of an unstarted child moves reserved->refundable
// (consumed units are never refundable).
type BudgetAccount struct {
	ID         string
	RootID     string
	Dimension  string
	Granted    int64
	Reserved   int64
	Consumed   int64
	Refundable int64
	Version    int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Drift reports budget_drift: nonzero means conservation broke (BGT-002).
func (b BudgetAccount) Drift() int64 {
	return b.Granted - b.Reserved - b.Consumed - b.Refundable
}

// Barrier policies and states (m6_barrier CHECK sets, 0046).
const (
	BarrierPolicyAll      = "ALL"
	BarrierPolicyQuorum   = "QUORUM"
	BarrierPolicyFailFast = "FAIL_FAST"

	BarrierStateOpen   = "open"
	BarrierStateClosed = "closed"
)

// Closed reasons recorded on the CAS transition.
const (
	BarrierClosedAllArrived    = "all_arrived"
	BarrierClosedQuorumReached = "quorum_reached"
	BarrierClosedBelowQuorum   = "all_arrived_below_quorum"
	BarrierClosedFailFast      = "fail_fast"
	BarrierClosedCancelled     = "root_cancelled"
	BarrierClosedDeadline      = "deadline"
)

// Barrier is one join barrier row (T-6.3.4). CLOSED is terminal — a late
// arrival is recorded as evidence but can never reopen or flip the verdict.
type Barrier struct {
	ID               string
	RootID           string
	Policy           string
	ExpectedChildren int
	Quorum           int // 0 = NULL (ALL / FAIL_FAST)
	State            string
	ClosedReason     string
	Version          int64
	CreatedAt        time.Time
	ClosedAt         *time.Time
	UpdatedAt        time.Time
}

// Arrival outcomes (m6_barrier_arrival.outcome CHECK set, 0046).
const (
	ArrivalSucceeded = "succeeded"
	ArrivalFailed    = "failed"
	ArrivalCancelled = "cancelled"
	ArrivalExpired   = "expired"
)

// BarrierArrival is one child settlement inside a barrier. UNIQUE
// (barrier_id, child_id) is the "every child settles exactly once" guard:
// a duplicate arrival is answered with the existing settlement and never
// refunds the budget twice.
type BarrierArrival struct {
	ID           string
	BarrierID    string
	ChildID      string
	Attempt      int64
	Outcome      string
	ResultDigest string
	ArrivedAt    time.Time
}

// ErrSequenceTaken: the (root, sequence) total-order slot already holds
// an intent (UNIQUE(root_id, sequence) is the hard stop; services pre-
// check and translate this into their own conflict error).
var ErrSequenceTaken = errors.New("m6supply: merge sequence already taken")

// MergeIntent states (m6_merge_intent.state CHECK set, 0046). The Root
// Writer walk is submitted -> validating -> queued -> cas_check ->
// applying -> merged; rejected exits at validating (schema/sequence
// verdicts), stale -> rebase_required is the CAS-conflict requeue path
// (MRG-001: 全序串行重基).
const (
	MergeIntentSubmitted    = "submitted"
	MergeIntentValidating   = "validating"
	MergeIntentQueued       = "queued"
	MergeIntentCasCheck     = "cas_check"
	MergeIntentApplying     = "applying"
	MergeIntentMerged       = "merged"
	MergeIntentRejected     = "rejected"
	MergeIntentStale        = "stale"
	MergeIntentRebaseNeeded = "rebase_required"
)

// MergeIntent is one durable, totally-ordered merge request (T-6.4.2).
// UNIQUE(root_id, sequence) is the total order the single Root Writer
// follows; expected_head is the root-tree head the child patched against
// and current_head records the head observed at CAS time (nil until the
// writer looks; on merge it advances to the post-patch head).
type MergeIntent struct {
	ID           string
	RootID       string
	ChildID      string
	Sequence     int64
	ExpectedHead string
	CurrentHead  string
	PatchDigest  string
	TestsRef     string
	State        string
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// MergeTerminal reports whether the intent left the writer queue for good
// (merged, rejected); stale/rebase_required still count as in-flight until
// the rebased successor merges.
func MergeTerminal(state string) bool {
	return state == MergeIntentMerged || state == MergeIntentRejected
}

// Final-gate states carried by outbox events (state machine C tail):
// FINAL_TESTING -> COMPLETE or FINAL_TEST_FAILED -> RECOVERY_REQUIRED.
// The gate itself is an outbox/audit fact, not a new table — the root's
// completion identity is the final-tree digest bound to those events.
const (
	FinalGateTesting  = "final_testing"
	FinalGateComplete = "complete"
	FinalGateFailed   = "final_test_failed"
	FinalGateRecovery = "recovery_required"
)

// OutboxEvent is one transactional-outbox row (T-6.4.4). Producers append
// it in the SAME transaction as the state change; the publisher drains
// unpublished rows at-least-once and consumers dedupe by event id.
// Retention: 30 days (published rows may be pruned).
type OutboxEvent struct {
	ID            string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       string
	Published     bool
	PublishedAt   *time.Time
	CreatedAt     time.Time
}
