// governance.go holds the phase-3 governance feature switches (M1 memory
// auto-accept, M2 expert shared working-memory bus, S2 collab-gate write
// handoff). Every switch is DEFAULT-OFF: with the zero value each frozen
// invariant holds exactly as before (memory needs explicit human
// confirmation, experts stay isolated on the single engine, the collab
// gate stays disabled). A switch only opens an audited, risk-classified or
// gate-checked opt-in path; it never removes a safety check, only adds a
// governed one on top. Flips are process-level (env at startup or a test
// setter), never silent per-turn heuristics.
package config

import (
	"os"
	"strings"
	"sync"
)

// GovernanceFlags is the concurrency-safe holder for the three phase-3
// switches. The zero value (all false) is the frozen-safe default.
type GovernanceFlags struct {
	mu             sync.RWMutex
	memAutoAccept  bool
	expertBus      bool
	collabHandoff  bool
}

// NewGovernanceFlags returns an all-off holder.
func NewGovernanceFlags() *GovernanceFlags { return &GovernanceFlags{} }

// LoadGovernanceFlagsFromEnv builds the holder from environment overrides.
// Unset or non-truthy variables stay off, so a stock install keeps every
// freeze. Recognised (truthy = 1/true/on/yes, case-insensitive):
//
//	LUNITIDE_MEMORY_AUTOACCEPT   -> M1 memory low-risk auto-accept
//	LUNITIDE_EXPERT_SHARED_BUS   -> M2 expert shared working-memory bus
//	LUNITIDE_COLLABGATE_HANDOFF  -> S2 collab-gate write handoff
func LoadGovernanceFlagsFromEnv() *GovernanceFlags {
	return &GovernanceFlags{
		memAutoAccept: envTruthy(os.Getenv("LUNITIDE_MEMORY_AUTOACCEPT")),
		expertBus:     envTruthy(os.Getenv("LUNITIDE_EXPERT_SHARED_BUS")),
		collabHandoff: envTruthy(os.Getenv("LUNITIDE_COLLABGATE_HANDOFF")),
	}
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// MemoryAutoAccept reports whether M1 low-risk memory auto-accept is armed.
func (g *GovernanceFlags) MemoryAutoAccept() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.memAutoAccept
}

// ExpertSharedBus reports whether M2 expert shared working-memory is armed.
func (g *GovernanceFlags) ExpertSharedBus() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.expertBus
}

// CollabHandoff reports whether S2 collab-gate write handoff is armed.
func (g *GovernanceFlags) CollabHandoff() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.collabHandoff
}

// SetMemoryAutoAccept arms/disarms M1 (used by tests and any future
// governed toggle path).
func (g *GovernanceFlags) SetMemoryAutoAccept(on bool) { g.set(&g.memAutoAccept, on) }

// SetExpertSharedBus arms/disarms M2.
func (g *GovernanceFlags) SetExpertSharedBus(on bool) { g.set(&g.expertBus, on) }

// SetCollabHandoff arms/disarms S2.
func (g *GovernanceFlags) SetCollabHandoff(on bool) { g.set(&g.collabHandoff, on) }

func (g *GovernanceFlags) set(field *bool, on bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	*field = on
}
