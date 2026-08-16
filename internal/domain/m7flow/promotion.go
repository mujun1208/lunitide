// M7 slice 4 domain values (T-7.4.x): the promotion saga. A promotion moves
// one sealed release package across environment edges (dev -> stage -> prod)
// carrying the same blob_digest everywhere. Before any external side effect
// the saga verifies a SoD approval bound to the canonical intent digest; the
// digest covers package, manifest, blob, environments, migration/deployment/
// rollback plans, policy and expiry, so any input change invalidates the
// approval and the promotion returns to approval_check.
//
// The 15 canonical states live in promotions.state (migration 0056); the
// release.promote / release.rollback wire responses project them onto the
// five-state external enum (planned/applying/verifying/done/rolled_back and
// rolling_back/failed) per the 02 technical design state-projection note.
package m7flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Promotion states (promotions.state CHECK set, 15 canonical states).
const (
	PrmRequested      = "requested"
	PrmPolicyCheck    = "policy_check"
	PrmApprovalCheck  = "approval_check"
	PrmDenied         = "denied"
	PrmExpired        = "expired"
	PrmMigrating      = "migrating"
	PrmDeploying      = "deploying"
	PrmValidating     = "validating"
	PrmSucceeded      = "succeeded"
	PrmFailed         = "failed"
	PrmRollingBack    = "rolling_back"
	PrmRolledBack     = "rolled_back"
	PrmRollbackFailed = "rollback_failed"
	PrmOutcomeUnknown = "outcome_unknown"
	PrmManual         = "manual"
)

// Target environments (promotions.to_env CHECK set).
const (
	EnvDev   = "dev"
	EnvStage = "stage"
	EnvProd  = "prod"
	// EnvNone is the synthetic source edge for the first promotion of a
	// package (nothing was promoted yet).
	EnvNone = "none"
)

// MigrationExecution states.
const (
	MigPlanned  = "planned"
	MigApplied  = "applied"
	MigVerified = "verified"
	MigFailed   = "failed"
)

// Deployment states.
const (
	DepPending        = "pending"
	DepRunning        = "running"
	DepSucceeded      = "succeeded"
	DepFailed         = "failed"
	DepOutcomeUnknown = "outcome_unknown"
)

// RollbackAttempt states.
const (
	RbkPending  = "pending"
	RbkRunning  = "running"
	RbkSucceeded = "succeeded"
	RbkFailed   = "failed"
)

// Rollback dimensions.
const (
	RbkBinary   = "binary"
	RbkSchema   = "schema"
	RbkData     = "data"
	RbkExternal = "external"
)

// Promotion is one saga execution of a sealed package across an environment
// edge. IdempotencyKey is unique per environment-edge request; state moves
// only along promotionTransitions.
type Promotion struct {
	ID                    string
	PackageID             string
	FromEnv               string
	ToEnv                 string
	CanonicalIntentDigest string
	PolicyVersion         string
	ApprovalExpiry        *time.Time
	State                 string
	IdempotencyKey        string
	RequestedBy           string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// MigrationExecution is one external schema migration bound to a promotion;
// rows are only ever created inside the Promotion aggregate (MIG-001).
type MigrationExecution struct {
	ID          string
	PromotionID string
	PlanDigest  string
	State       string
	RollbackRef string
	CreatedAt   time.Time
}

// Deployment is one external deployment of the package blob to the target
// environment; RESULT_UNKNOWN dispatches park in outcome_unknown until the
// reconciler resolves the remote fact.
type Deployment struct {
	ID          string
	PromotionID string
	TargetEnv   string
	State       string
	StartedAt   *time.Time
	CompletedAt *time.Time
	ReceiptJSON string
}

// RollbackAttempt is one append-only rollback execution over one of the four
// dimensions (RBK-002: attempts are append-only, never deleted).
type RollbackAttempt struct {
	ID          string
	PromotionID string
	Dimension   string
	State       string
	PlanDigest  string
	OperatorID  string
	ResultJSON  string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

// promotionTransitions is the canonical Promotion state machine:
//
//	requested -> policy_check -> approval_check -> migrating -> deploying -> validating -> succeeded
//	                          -> denied | expired              -> failed -> rolling_back -> rolled_back | rollback_failed
//	non-terminal states -> outcome_unknown | manual (reconciler outcomes)
var promotionTransitions = map[string]map[string]bool{
	PrmRequested:      {PrmPolicyCheck: true, PrmFailed: true},
	PrmPolicyCheck:    {PrmApprovalCheck: true, PrmDenied: true, PrmExpired: true, PrmFailed: true},
	PrmApprovalCheck:  {PrmMigrating: true, PrmDenied: true, PrmExpired: true, PrmFailed: true},
	PrmDenied:         {},
	PrmExpired:        {},
	PrmMigrating:      {PrmDeploying: true, PrmFailed: true, PrmOutcomeUnknown: true, PrmManual: true},
	PrmDeploying:      {PrmValidating: true, PrmFailed: true, PrmOutcomeUnknown: true, PrmManual: true},
	PrmValidating:     {PrmSucceeded: true, PrmFailed: true, PrmOutcomeUnknown: true, PrmManual: true},
	PrmSucceeded:      {PrmRollingBack: true},
	PrmFailed:         {PrmRollingBack: true, PrmManual: true},
	PrmRollingBack:    {PrmRolledBack: true, PrmRollbackFailed: true, PrmManual: true},
	PrmRolledBack:     {},
	PrmRollbackFailed: {PrmManual: true},
	PrmOutcomeUnknown: {PrmMigrating: true, PrmDeploying: true, PrmValidating: true, PrmSucceeded: true, PrmFailed: true, PrmRollingBack: true, PrmManual: true},
	PrmManual:         {PrmMigrating: true, PrmDeploying: true, PrmValidating: true, PrmRollingBack: true, PrmFailed: true},
}

// LegalPromotionTransition guards the canonical Promotion state machine.
func LegalPromotionTransition(from, to string) bool { return promotionTransitions[from][to] }

// PromotionTerminal reports whether the state is a saga end state (no
// automatic forward movement anymore; rollback may still follow succeeded).
func PromotionTerminal(state string) bool {
	switch state {
	case PrmDenied, PrmExpired, PrmSucceeded, PrmRolledBack, PrmRollbackFailed, PrmManual:
		return true
	}
	return false
}

// EnvRank orders the promotion channel none < dev < stage < prod.
func EnvRank(env string) int {
	switch env {
	case EnvNone:
		return 0
	case EnvDev:
		return 1
	case EnvStage:
		return 2
	case EnvProd:
		return 3
	}
	return -1
}

// LegalPromotionEdge reports whether from -> to is a legal channel edge:
// none -> dev is the first promotion; dev -> stage, dev -> prod (the
// high-risk shortcut when no stage environment is configured) and
// stage -> prod are the promotion edges. Skipping dev is not allowed because
// every package must prove itself in dev before it may climb.
func LegalPromotionEdge(from, to string) bool {
	if EnvRank(to) <= EnvRank(from) {
		return false
	}
	switch {
	case from == EnvNone:
		return to == EnvDev
	}
	return true
}

// CanonicalIntent is the JCS-canonical promotion intent. Every field change
// (including policy version or approval expiry) produces a new digest and
// thereby invalidates any approval bound to the previous digest.
type CanonicalIntent struct {
	PackageDigest        string `json:"package_digest"` // release package id binding
	ManifestDigest       string `json:"manifest_digest"`
	BlobDigest           string `json:"blob_digest"`
	FromEnv              string `json:"from_env"`
	ToEnv                string `json:"to_env"`
	MigrationPlanDigest  string `json:"migration_plan_digest"`
	DeploymentPlanDigest string `json:"deployment_plan_digest"`
	RollbackPlanDigest   string `json:"rollback_plan_digest"`
	PolicyVersion        string `json:"policy_version"`
	ApprovalExpiry       string `json:"approval_expiry"` // RFC3339 or ""
}

// CanonicalIntentJSON serializes the intent with fixed field order (struct
// order above is the canonical order; json.Marshal preserves it).
func CanonicalIntentJSON(ci CanonicalIntent) string {
	b, err := json.Marshal(ci)
	if err != nil {
		return ""
	}
	return string(b)
}

// CanonicalIntentDigest = SHA-256(UTF8(canonical_intent)).
func CanonicalIntentDigest(ci CanonicalIntent) string {
	sum := sha256.Sum256([]byte(CanonicalIntentJSON(ci)))
	return hex.EncodeToString(sum[:])
}

// ApprovalHash binds one approver decision to one canonical intent digest:
//
//	SHA-256(UTF8("M7-APPROVAL-V2\n") || digest || "\n" || approver || "\n" || approvedAt || "\n" || expiry)
//
// approverID / approvedAt / approvalExpiry are the exact RFC3339 strings
// recorded in the approval payload; any change re-computes a different hash
// (V2 formula), so old approvals can never be replayed over new intents.
func ApprovalHash(canonicalIntentDigest, approverID, approvedAt, approvalExpiry string) string {
	h := sha256.New()
	h.Write([]byte("M7-APPROVAL-V2\n"))
	h.Write([]byte(canonicalIntentDigest))
	h.Write([]byte("\n"))
	h.Write([]byte(approverID))
	h.Write([]byte("\n"))
	h.Write([]byte(approvedAt))
	h.Write([]byte("\n"))
	h.Write([]byte(approvalExpiry))
	return hex.EncodeToString(h.Sum(nil))
}

// Wire projections for release.promote / release.rollback responses.
const (
	WirePlanned    = "planned"
	WireApplying   = "applying"
	WireVerifying  = "verifying"
	WireDone       = "done"
	WireRolledBack = "rolled_back"
	WireRolling    = "rolling_back"
	WireFailed     = "failed"
)

// PromoteWireState projects a canonical promotion state onto the
// release.promote response enum. Terminal failure states never project -
// they surface through the error envelope instead (PRM/MIG/DEP codes).
func PromoteWireState(state string) string {
	switch state {
	case PrmRequested, PrmPolicyCheck, PrmApprovalCheck, PrmManual:
		return WirePlanned
	case PrmMigrating, PrmDeploying:
		return WireApplying
	case PrmValidating, PrmOutcomeUnknown:
		return WireVerifying
	case PrmSucceeded:
		return WireDone
	case PrmRollingBack:
		return WireRolling
	case PrmRolledBack:
		return WireRolledBack
	}
	return WireFailed
}

// RollbackWireState projects a canonical promotion / attempt state onto the
// release.rollback response enum.
func RollbackWireState(state string) string {
	switch state {
	case RbkPending, RbkRunning, PrmRollingBack:
		return WireRolling
	case RbkSucceeded, PrmRolledBack:
		return WireRolledBack
	}
	return WireFailed
}