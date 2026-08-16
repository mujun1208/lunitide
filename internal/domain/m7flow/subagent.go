package m7flow

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// M7 slice 6 (T-7.6.x): the read-only subagent domain. A subagent is a
// capability-restricted derived execution unit of one Root Run: it only
// collects read-only evidence, never writes business tables, and its
// readCaps are bound to the spawn-time whitelist snapshot via
// capability_digest. Any whitelist, policy or persona drift between spawn
// and join fails closed (M7-SAG-004).

// SubagentRun states per the design state machine:
// queued -> running -> completed | failed | cancelled, plus orphaned for
// crash-recovery reconciliation (orphaned -> completed | failed).
const (
	SagQueued    = "queued"
	SagRunning   = "running"
	SagCompleted = "completed"
	SagFailed    = "failed"
	SagCancelled = "cancelled"
	SagOrphaned  = "orphaned"
)

// sagTransitions is the legal SubagentRun state machine. Terminal states
// (completed/failed/cancelled) accept no outgoing edge; orphaned is only
// reachable from running via the crash-recovery reconciler.
var sagTransitions = map[string]map[string]bool{
	SagQueued:    {SagRunning: true, SagFailed: true, SagCancelled: true},
	SagRunning:   {SagCompleted: true, SagFailed: true, SagCancelled: true, SagOrphaned: true},
	SagOrphaned:  {SagCompleted: true, SagFailed: true},
	SagCompleted: {},
	SagFailed:    {},
	SagCancelled: {},
}

// SagTransitionAllowed reports whether from -> to is a legal edge.
func SagTransitionAllowed(from, to string) bool {
	return sagTransitions[from][to]
}

// SagTerminal reports whether state accepts no further transition.
func SagTerminal(state string) bool {
	if _, ok := sagTransitions[state]; !ok {
		return false
	}
	return len(sagTransitions[state]) == 0
}

// Frozen M7 subagent guard values (02-技术设计 §策略、预算与级联).
const (
	// SubagentMaxConcurrent: at most 4 live subagents per Root Run.
	SubagentMaxConcurrent = 4
	// SubagentMaxBudgetTokens: budgetTokens ceiling per spawn.
	SubagentMaxBudgetTokens = 50000
	// SubagentMinDeadlineMS / SubagentMaxDeadlineMS: the 1-15 minute
	// deadline window.
	SubagentMinDeadlineMS = 60 * 1000
	SubagentMaxDeadlineMS = 15 * 60 * 1000
	// SubagentMaxPurpose: purpose length ceiling (M7-SAG-001).
	SubagentMaxPurpose = 2000
	// SubagentMaxSummary: join summary ceiling in bytes.
	SubagentMaxSummary = 64 * 1024
)

// subagentReadCaps is the frozen M7 read-only whitelist. Everything else -
// including changeset.*, command.*, terminal.*, MCP write semantics, Secret
// Lease, browser write ops and recursive subagent.spawn - is fail-closed
// refused with M7-SAG-002.
var subagentReadCaps = map[string]bool{
	"fs.tree":            true,
	"fs.stat":            true,
	"fs.read":            true,
	"fs.readMany":        true,
	"fs.glob":            true,
	"fs.grep":            true,
	"web.fetch":          true,
	"web.search":         true,
	"evidence.list":      true,
	"browser.act:navigate": true,
	"browser.act:read":     true,
	"browser.act:snapshot": true,
}

// SagCapAllowed reports whether one capability is inside the frozen
// read-only whitelist.
func SagCapAllowed(cap string) bool { return subagentReadCaps[cap] }

// SubagentCapsDigest derives capability_digest: the sha256 over the sorted,
// newline-joined capability list. The digest pins the exact whitelist
// subset granted at spawn time; join re-verifies it (TOCTOU).
func SubagentCapsDigest(caps []string) string {
	sorted := append([]string(nil), caps...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(joinLines(sorted)))
	return hex.EncodeToString(sum[:])
}

func joinLines(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n"
		}
		out += p
	}
	return out
}

// SubagentRun is one derived read-only execution unit.
type SubagentRun struct {
	ID               string
	RootRunID        string
	StageRunID       string
	Purpose          string
	CapabilityDigest string
	PolicyVersion    string
	PersonaDigest    string
	Status           string
	BudgetTokens     int64
	SpentTokens      int64
	DeadlineMS       int64
	IdempotencyKey   string
	CreatedAt        string
	CompletedAt      *string
}

// SubagentObservation is one append-only join-evidence row (seq ordered).
type SubagentObservation struct {
	ID            string
	SubagentRunID string
	Seq           int64
	EvidenceID    string
	Summary       string
	Digest        string
	CreatedAt     string
}