package stdiopoc

import "time"

// The six isolation assumptions of the stdio POC (slice 5A). EnforcedBy
// records which layer actually blocks the attack today — the evidence
// bundle must be honest about this: guard-level means the stdio worker
// runtime funnels every access through the host guard (the design's
// enforcement path), os-level means the OS itself makes the attack
// physically impossible for the child.
type Assumption struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	EnforcedBy string    `json:"enforcedBy"` // os-level | guard-level | protocol-level
	Passed     bool      `json:"passed"`
	Summary    string    `json:"summary,omitempty"`
	Attacks    []Attack  `json:"attacks"`
	HostCheck  HostCheck `json:"hostCheck"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
	// Digest is SHA-256 over the canonical JSON of the assumption record
	// without the Digest field (chain link).
	Digest string `json:"digest"`
}

// Attack is one attacker-side attempt inside the probe child.
type Attack struct {
	Vector  string `json:"vector"`          // e.g. "../traversal"
	Attempt string `json:"attempt"`         // what the probe tried
	Blocked bool   `json:"blocked"`         // did the enforcement layer reject it
	Detail  string `json:"detail,omitempty"` // observed result (trimmed)
}

// HostCheck is the host-side cross validation: the harness independently
// confirms the precondition (e.g. the marker file exists and is readable by
// the host) and the enforcement verdict (e.g. the same guard rejects the
// same vectors), so a probe "blocked" claim can never pass on its own.
type HostCheck struct {
	Precondition string `json:"precondition,omitempty"` // what the host verified first
	Confirmed    bool   `json:"confirmed"`
	Detail       string `json:"detail,omitempty"`
}

// Assumption IDs (canonical order in bundles).
const (
	AssumptionHostFile = "host-file"
	AssumptionNetwork  = "network"
	AssumptionSecret   = "secret"
	AssumptionProcTree = "proctree"
	AssumptionResource = "resource"
	AssumptionProtocol = "protocol"
)

// assumptionOrder is the canonical order evidence bundles list assumptions in.
var assumptionOrder = []string{
	AssumptionHostFile, AssumptionNetwork, AssumptionSecret,
	AssumptionProcTree, AssumptionResource, AssumptionProtocol,
}

var assumptionMeta = map[string]struct{ title, enforcedBy string }{
	AssumptionHostFile: {"host filesystem escapes (../, symlink, junction)", "guard-level"},
	AssumptionNetwork:  {"network egress to host/metadata/intranet targets", "guard-level"},
	AssumptionSecret:   {"parent environment / secret inheritance", "os-level"},
	AssumptionProcTree: {"fork bomb and process-tree survival", "os-level"},
	AssumptionResource: {"memory exhaustion", "os-level"},
	AssumptionProtocol: {"protocol cheating (oversize/malformed/forged frames)", "protocol-level"},
}

func assumptionByID(id string) (struct{ title, enforcedBy string }, bool) {
	m, ok := assumptionMeta[id]
	return m, ok
}

// Verdicts.
const (
	VerdictPass = "PASS"
	VerdictFail = "FAIL"
)
