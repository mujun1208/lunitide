// M8 FR-17 domain core (G1/G2): the write-collaboration evaluation gate.
//
// The gate stays DISABLED through all of M8: this package only adjudicates
// frozen-threshold evidence into an append-only evaluation snapshot and
// drives the one-time-token decision lifecycle. No write-collaboration
// execution path lives here.
package m8core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Evaluation outcomes (migration 0066 CHECK).
const (
	EvalComputing            = "computing"
	EvalInsufficientEvidence = "insufficient_evidence"
	EvalPass                 = "pass"
	EvalFail                 = "fail"
)

// Decision states (migration 0066 CHECK).
const (
	DecisionPending   = "pending"
	DecisionConfirmed = "confirmed"
	DecisionExpired   = "expired"
	DecisionRevoked   = "revoked"
)

// Decision actions (migration 0066 CHECK): only a pass evaluation may carry
// an enable decision; disable decisions may be produced while enabled.
const (
	DecisionEnable  = "enable"
	DecisionDisable = "disable"
)

// Capability states.
const (
	CapabilityDisabled = "disabled"
	CapabilityEnabled  = "enabled"
)

// Frozen adjudication thresholds (02 技术设计「评估证据与门禁条件」,
// criteria_version bound - changing any of these requires a new version).
const (
	// GateMinWindowDays: the evidence window must span >= 30 days.
	GateMinWindowDays = 30
	// GateMinSubagentRuns: >= 500 subagent runs inside the window.
	GateMinSubagentRuns = 500
	// GateMinRootRuns: >= 20 distinct root runs covered.
	GateMinRootRuns = 20
	// GateMaxTimeoutRatio: timeout share <= 5%.
	GateMaxTimeoutRatio = 0.05
	// GateMinCompensationSuccess: 90-day compensation success >= 99%.
	GateMinCompensationSuccess = 0.99
)

// Failed-criteria keys (frozen, surfaced in failedCriteria).
const (
	CritWindowDays           = "window_days"
	CritSubagentRuns         = "subagent_runs"
	CritRootRuns             = "root_runs"
	CritWriteInterceptRate   = "write_intercept_rate"
	CritUndeclaredWrites     = "undeclared_write_effects"
	CritToctouReplayGuard    = "toctou_replay_guard"
	CritOrphanSubagents      = "orphan_subagents"
	CritCrashRecovery        = "crash_recovery_convergence"
	CritTimeoutRatio         = "timeout_ratio"
	CritCompensationSuccess  = "compensation_success_rate"
)

// GateEvidence is the aggregated read-only evidence snapshot over the
// evaluation window: M7 subagent_runs/observations plus the M5/M6
// EffectJournal. Missing lists the evidence-source keys that were absent
// (partial evidence -> insufficient_evidence, never a partial pass).
type GateEvidence struct {
	WindowDays          int     `json:"windowDays"`
	SubagentRuns        int     `json:"subagentRuns"`
	RootRunsCovered     int     `json:"rootRunsCovered"`
	WriteInterceptRate  float64 `json:"writeInterceptRate"`
	UndeclaredWrites    int     `json:"undeclaredWrites"`
	ToctouReplayGuard   float64 `json:"toctouReplayGuard"`
	OrphanSubagents     int     `json:"orphanSubagents"`
	CrashRecoveryRate   float64 `json:"crashRecoveryRate"`
	TimeoutRatio        float64 `json:"timeoutRatio"`
	CompensationSuccess float64 `json:"compensationSuccess"`
	Missing             []string `json:"missing,omitempty"`
}

// CanonicalJSON answers the canonical evidence body (sorted keys, no
// whitespace) - the digest input.
func (e GateEvidence) CanonicalJSON() []byte {
	b, _ := json.Marshal(e)
	return b
}

// Digest answers the SHA-256 evidence digest.
func (e GateEvidence) Digest() string {
	sum := sha256.Sum256(e.CanonicalJSON())
	return hex.EncodeToString(sum[:])
}

// GateEvaluation is the append-only evaluation snapshot (WORM: UPDATE and
// DELETE trip M8-034, idempotent on the four-part unique key).
type GateEvaluation struct {
	EvaluationID      string
	SubjectID         string
	WindowStart       int64
	WindowEnd         int64
	EvidenceJSON      string
	EvidenceDigest    string
	CriteriaVersion   string
	Outcome           string
	FailedCriteria    []string
	CreatedAt         string
}

// GateDecision is the one-time-token user decision. Only a pass evaluation
// may yield an enable decision; the token is single-use and expires.
type GateDecision struct {
	DecisionID       string
	EvaluationID     string
	SubjectID        string
	DecisionToken    string
	PolicyVersion    string
	CapabilityDigest string
	Action           string
	State            string
	ConfirmedAt      string
	ExpiresAt        string
	CreatedAt        string
}

// Adjudicate enacts the frozen criteria chain (02 技术设计 G1):
//  1. any missing evidence source -> insufficient_evidence (fail-closed,
//     no partial results);
//  2. sample volume (window >= 30d, runs >= 500, root runs >= 20) ->
//     insufficient_evidence with the missing items listed;
//  3. every gate condition compared one by one -> fail with failedCriteria
//     listing each violation;
//  4. otherwise pass.
func Adjudicate(e GateEvidence) (outcome string, failed []string) {
	failed = []string{}
	if len(e.Missing) > 0 {
		failed = append(failed, e.Missing...)
		return EvalInsufficientEvidence, failed
	}
	if e.WindowDays < GateMinWindowDays {
		failed = append(failed, CritWindowDays)
	}
	if e.SubagentRuns < GateMinSubagentRuns {
		failed = append(failed, CritSubagentRuns)
	}
	if e.RootRunsCovered < GateMinRootRuns {
		failed = append(failed, CritRootRuns)
	}
	if len(failed) > 0 {
		return EvalInsufficientEvidence, failed
	}
	if e.WriteInterceptRate < 1 {
		failed = append(failed, CritWriteInterceptRate)
	}
	if e.UndeclaredWrites > 0 {
		failed = append(failed, CritUndeclaredWrites)
	}
	if e.ToctouReplayGuard < 1 {
		failed = append(failed, CritToctouReplayGuard)
	}
	if e.OrphanSubagents > 0 {
		failed = append(failed, CritOrphanSubagents)
	}
	if e.CrashRecoveryRate < 1 {
		failed = append(failed, CritCrashRecovery)
	}
	if e.TimeoutRatio > GateMaxTimeoutRatio {
		failed = append(failed, CritTimeoutRatio)
	}
	if e.CompensationSuccess < GateMinCompensationSuccess {
		failed = append(failed, CritCompensationSuccess)
	}
	if len(failed) > 0 {
		return EvalFail, failed
	}
	return EvalPass, failed
}

// ValidOutcome guards the migration CHECK family.
func ValidOutcome(o string) bool {
	switch o {
	case EvalComputing, EvalInsufficientEvidence, EvalPass, EvalFail:
		return true
	}
	return false
}

// ValidDecisionState guards the migration CHECK family.
func ValidDecisionState(s string) bool {
	switch s {
	case DecisionPending, DecisionConfirmed, DecisionExpired, DecisionRevoked:
		return true
	}
	return false
}

// ValidDecisionAction guards the migration CHECK family.
func ValidDecisionAction(a string) bool {
	return a == DecisionEnable || a == DecisionDisable
}

// EncodeFailedCriteria answers the canonical JSON for failed_criteria_json
// ([] -> "[]" so the column stays non-null on insufficient_evidence paths).
func EncodeFailedCriteria(items []string) string {
	if items == nil {
		items = []string{}
	}
	b, _ := json.Marshal(items)
	return string(b)
}

// DecodeFailedCriteria parses failed_criteria_json (NULL -> empty).
func DecodeFailedCriteria(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// CapabilityBinding is the runtime binding a decision pins: any drift
// between the pinned pair and the live runtime pair flips the capability
// back to disabled immediately (M8-032).
type CapabilityBinding struct {
	PolicyVersion    string
	CapabilityDigest string
}

// WriteCollabPolicyVersion freezes the M8 write-collaboration policy
// version (the M7 read-only capability whitelist generation).
const WriteCollabPolicyVersion = "write-collab-v1"

// WriteCollabBinding derives the frozen runtime binding: the capability
// digest commits the policy version and the M7 read-only capability
// whitelist family, so any whitelist/policy regeneration drifts the digest
// and rolls the capability back to disabled (M8-032).
func WriteCollabBinding() CapabilityBinding {
	h := sha256.New()
	h.Write([]byte("lunitide-write-collab|" + WriteCollabPolicyVersion + "|read-only-subagent-whitelist"))
	return CapabilityBinding{
		PolicyVersion:    WriteCollabPolicyVersion,
		CapabilityDigest: hex.EncodeToString(h.Sum(nil)),
	}
}

// Matches answers whether two bindings agree.
func (b CapabilityBinding) Matches(other CapabilityBinding) bool {
	return b.PolicyVersion == other.PolicyVersion && b.CapabilityDigest == other.CapabilityDigest
}

// String answers the printable pair (audit-friendly).
func (b CapabilityBinding) String() string {
	return fmt.Sprintf("policy=%s digest=%s", b.PolicyVersion, b.CapabilityDigest)
}
