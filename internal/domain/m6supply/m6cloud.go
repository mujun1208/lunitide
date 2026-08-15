// Cloud execution registry (migration 0053, m6-cloud-execution-canonical):
// CloudRunner registration + attestation, RegionPolicySnapshot, fenced
// WorkerLease (fencing epochs), RemoteReceipt with outcome_unknown and
// ReconcileDecision.
//
// Core invariants (M6/02 §10):
//
//	Registration  a runner registers with a region, a workload identity
//	              (UNIQUE), an attestation digest/status and an mTLS
//	              fingerprint; only verified runners may activate.
//	Policy        the latest RegionPolicySnapshot pins allowed regions,
//	              the egress policy and the data classification; dispatch
//	              consults it — a runner outside the allowed regions is
//	              never scheduled.
//	Lease         one task has at most one lease (task_id UNIQUE); the
//	              epoch is the fencing token — a lease write with a stale
//	              epoch loses; expiry moves state to expired.
//	Receipt       receipts are append-only facts; outcome_unknown is a
//	              first-class outcome. A lost receipt NEVER re-dispatches
//	              the task: reconcile draws accepted/rejected/requeued/
//	              manual_review from the evidence at hand.
package m6supply

import (
	"fmt"
	"time"
)

// Runner states (m6_cloudrunner.state CHECK set).
const (
	RunnerRegistered = "registered"
	RunnerActive     = "active"
	RunnerSuspended  = "suspended"
	RunnerRevoked    = "revoked"
)

// runnerTransitions guards the lifecycle. revoked is terminal.
var runnerTransitions = map[string]map[string]bool{
	RunnerRegistered: {RunnerActive: true, RunnerRevoked: true},
	RunnerActive:     {RunnerSuspended: true, RunnerRevoked: true},
	RunnerSuspended:  {RunnerActive: true, RunnerRevoked: true},
	RunnerRevoked:    {},
}

// RunnerTransitionAllowed guards the lifecycle CAS.
func RunnerTransitionAllowed(from, to string) bool {
	if _, ok := runnerTransitions[from]; !ok {
		return false
	}
	return runnerTransitions[from][to]
}

// Attestation statuses (m6_cloudrunner.attestation_status CHECK set).
const (
	AttestationVerified   = "verified"
	AttestationUnverified = "unverified"
	AttestationRevoked    = "revoked"
)

// CloudRunner is one registered execution node.
type CloudRunner struct {
	ID                string
	Region            string
	WorkloadIdentity  string
	AttestationDigest string
	AttestationStatus string
	MTLSFingerprint   string
	Capabilities      string // JSON
	State             string
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ValidateRunnerInput checks the registration payload shape.
func ValidateRunnerInput(region, workloadIdentity, attestationDigest, attestationStatus, mtlsFingerprint, capabilities string) error {
	if len(region) < 1 || len(region) > 64 {
		return fmt.Errorf("region length must be 1..64")
	}
	if len(workloadIdentity) < 1 || len(workloadIdentity) > 256 {
		return fmt.Errorf("workloadIdentity length must be 1..256")
	}
	if len(attestationDigest) != 64 || !isLowerHex(attestationDigest) {
		return fmt.Errorf("attestationDigest must be a 64-char lowercase hex sha-256")
	}
	switch attestationStatus {
	case AttestationVerified, AttestationUnverified, AttestationRevoked:
	default:
		return fmt.Errorf("attestationStatus must be verified|unverified|revoked")
	}
	if len(mtlsFingerprint) < 1 || len(mtlsFingerprint) > 256 {
		return fmt.Errorf("mtlsFingerprint length must be 1..256")
	}
	if err := ValidateJSONDoc(capabilities, 8192); err != nil {
		return fmt.Errorf("capabilities: %v", err)
	}
	return nil
}

// RegionPolicy is one pinned policy snapshot.
type RegionPolicy struct {
	ID                string
	Version           int64
	AllowedRegions    string // JSON array
	EgressPolicy      string // JSON
	DataClassification string // JSON
	CreatedAt         time.Time
}

// ValidateRegionPolicyInput checks the snapshot payload shape.
func ValidateRegionPolicyInput(allowedRegions, egressPolicy, dataClassification string) error {
	if err := ValidateJSONDoc(allowedRegions, 16384); err != nil {
		return fmt.Errorf("allowedRegions: %v", err)
	}
	if err := ValidateJSONDoc(egressPolicy, 16384); err != nil {
		return fmt.Errorf("egressPolicy: %v", err)
	}
	if err := ValidateJSONDoc(dataClassification, 16384); err != nil {
		return fmt.Errorf("dataClassification: %v", err)
	}
	return nil
}

// Lease states (m6_worker_lease.state CHECK set).
const (
	LeaseActive  = "active"
	LeaseExpired = "expired"
	LeaseReleased = "released"
	LeaseRevoked = "revoked"
)

// WorkerLease is the fenced lease of one task by one runner. The epoch is
// the fencing token: it only grows, and a write carrying a stale epoch
// must lose.
type WorkerLease struct {
	ID        string
	RunnerID  string
	TaskID    string
	Epoch     int64
	ExpiresAt time.Time
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Receipt outcomes (m6_remote_receipt.outcome CHECK set) — the same
// outcome vocabulary as the call log.
const (
	ReceiptSucceeded      = "succeeded"
	ReceiptFailed         = "failed"
	ReceiptCancelled      = "cancelled"
	ReceiptOutcomeUnknown = "outcome_unknown"
)

// Reconcile states (m6_remote_receipt.reconcile_state CHECK set).
const (
	ReconcilePending   = "pending"
	ReconcileReconciled = "reconciled"
	ReconcileDisputed  = "disputed"
)

// RemoteReceipt is one appended outcome report from a runner.
type RemoteReceipt struct {
	ID             string
	TaskID         string
	RunnerID       string
	Outcome        string
	ResultDigest   string
	Usage          string // JSON
	ReceivedAt     time.Time
	ReconcileState string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ValidateReceiptInput checks the receipt payload shape.
func ValidateReceiptInput(outcome, resultDigest, usage string) error {
	switch outcome {
	case ReceiptSucceeded, ReceiptFailed, ReceiptCancelled, ReceiptOutcomeUnknown:
	default:
		return fmt.Errorf("outcome must be succeeded|failed|cancelled|outcome_unknown")
	}
	if resultDigest != "" && (len(resultDigest) != 64 || !isLowerHex(resultDigest)) {
		return fmt.Errorf("resultDigest must be a 64-char lowercase hex sha-256")
	}
	if err := ValidateJSONDoc(usage, 8192); err != nil {
		return fmt.Errorf("usage: %v", err)
	}
	return nil
}

// Reconcile decisions (m6_reconcile_decision.decision CHECK set).
const (
	ReconcileAccepted     = "accepted"
	ReconcileRejected     = "rejected"
	ReconcileRequeued     = "requeued"
	ReconcileManualReview = "manual_review"
)

// ReconcileDecision is one decision drawn from a receipt.
type ReconcileDecision struct {
	ID        string
	ReceiptID string
	TaskID    string
	Decision  string
	Reason    string
	DecidedAt time.Time
	CreatedAt time.Time
}

// ValidateReconcileInput checks the decision payload shape.
func ValidateReconcileInput(decision, reason string) error {
	switch decision {
	case ReconcileAccepted, ReconcileRejected, ReconcileRequeued, ReconcileManualReview:
	default:
		return fmt.Errorf("decision must be accepted|rejected|requeued|manual_review")
	}
	if len(reason) < 1 || len(reason) > 2048 {
		return fmt.Errorf("reason length must be 1..2048")
	}
	return nil
}
