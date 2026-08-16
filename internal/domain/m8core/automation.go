// M8 slice-4 domain (T-8.4.x): workflow bundle prechecks and the
// automation run projection.
//
// Bundle checksum/permission precheck failures quarantine with zero
// dispatch (M8-021/022); high-risk actions wait at the execution point for
// just-in-time confirmation (M8-023); budgets over the bundle ceiling are
// refused (M8-024). AutomationRun is an M5/M6 Run projection: M8 never
// advances run state itself and creates no second execution kernel - the
// dispatch path only writes the RECEIVED/DISPATCHED/WAITING_CONFIRMATION/
// QUARANTINED projection rows (migration 0064).
package m8core

import (
	"encoding/json"
	"fmt"
)

// Workflow bundle states (migration 0064 CHECK).
const (
	BundleVerified    = "verified"
	BundleQuarantined = "quarantined"
)

// Automation run projection states (migration 0064 CHECK; M5/M6 canonical).
const (
	RunReceived           = "RECEIVED"
	RunPolicyChecked      = "POLICY_CHECKED"
	RunWaitingConfirmation = "WAITING_CONFIRMATION"
	RunDispatched         = "DISPATCHED"
	RunCheckpointed       = "CHECKPOINTED"
	RunSucceeded          = "SUCCEEDED"
	RunCompensating       = "COMPENSATING"
	RunQuarantined        = "QUARANTINED"
)

// Field limits mirroring migration 0064 CHECKs.
const (
	MaxIdempotencyKey = 128
	// DefaultBudgetCeiling backs the precheck when the bundle carries no
	// explicit ceiling (tokens).
	DefaultBudgetCeiling = 100000
)

// WorkflowBundle is one verified-or-quarantined bundle row.
type WorkflowBundle struct {
	ID          string
	Version     int64
	Checksum    string
	Permissions string // canonical JSON {allow:[...], highRisk:[...], budgetCeiling:n}
	RollbackRef string
	State       string
	CreatedAt   string
}

// BundlePermissions is the decoded permission document.
type BundlePermissions struct {
	Allow         []string `json:"allow"`
	HighRisk      []string `json:"highRisk"`
	BudgetCeiling int64    `json:"budgetCeiling"`
}

// DecodePermissions parses the stored permission document.
func (b WorkflowBundle) DecodePermissions() (BundlePermissions, error) {
	var p BundlePermissions
	if err := json.Unmarshal([]byte(b.Permissions), &p); err != nil {
		return p, fmt.Errorf("m8core: permissions %q: %w", b.Permissions, err)
	}
	if p.BudgetCeiling <= 0 {
		p.BudgetCeiling = DefaultBudgetCeiling
	}
	return p, nil
}

// PrecheckInput feeds the dispatch-time prechecks.
type PrecheckInput struct {
	BundleID      string
	BundleVersion int64
	BundleState   string
	Checksum      string
	Permissions   BundlePermissions
	// TriggerActions are the capability names the trigger intends to use.
	TriggerActions []string
	BudgetTokens   int64
	HighRiskHit    func(action string) bool
}

// PrecheckOutcome is the zero-dispatch/confirm/dispatch decision.
type PrecheckOutcome struct {
	Decision string // "dispatch" | "waiting_confirmation" | "blocked"
	Code     string // "" | "M8-021" | "M8-022" | "M8-023" | "M8-024" | "M8-026"
	Reason   string
}

// Precheck enacts M8-021/022/023/024/026 in order: quarantined bundles and
// checksum mismatches quarantine with zero dispatch; an action outside the
// allow list is denied; a budget over the ceiling is refused; a high-risk
// action waits for just-in-time confirmation; otherwise dispatch.
func Precheck(in PrecheckInput) PrecheckOutcome {
	if in.BundleState == BundleQuarantined {
		return PrecheckOutcome{Decision: "blocked", Code: "M8-026", Reason: "bundle quarantined"}
	}
	if !ValidHexDigest(in.Checksum) {
		return PrecheckOutcome{Decision: "blocked", Code: "M8-021", Reason: "bundle checksum invalid"}
	}
	allow := map[string]bool{}
	for _, a := range in.Permissions.Allow {
		allow[a] = true
	}
	// High-risk entries are allowed-by-declaration: they pass M8-022 and
	// land on the M8-023 just-in-time confirmation instead.
	for _, a := range in.Permissions.HighRisk {
		allow[a] = true
	}
	for _, a := range in.TriggerActions {
		if !allow[a] {
			return PrecheckOutcome{Decision: "blocked", Code: "M8-022", Reason: "action not allowed: " + a}
		}
	}
	if in.BudgetTokens > in.Permissions.BudgetCeiling {
		return PrecheckOutcome{Decision: "blocked", Code: "M8-024", Reason: "budget over ceiling"}
	}
	if in.HighRiskHit != nil {
		for _, a := range in.TriggerActions {
			if in.HighRiskHit(a) {
				return PrecheckOutcome{Decision: "waiting_confirmation", Code: "M8-023", Reason: "high-risk action: " + a}
			}
		}
	}
	return PrecheckOutcome{Decision: "dispatch"}
}

// AutomationRun is one run projection row (idempotency_key unique).
type AutomationRun struct {
	ID             string
	BundleID       string
	State          string
	ApprovalRef    string
	BudgetJSON     string
	CheckpointJSON string
	IdempotencyKey string
	InputDigest    string
	CreatedAt      string
}

// CanonicalTriggerDigest hashes the trigger+budget documents for the run
// input digest.
func CanonicalTriggerDigest(trigger, budget string) string {
	return DigestOf(trigger + "|" + budget)
}
