// Package decision implements the M9 ADR-011 blocking gate (error M9-031
// ADR_011_BLOCKED, 04 错误目录): while any required architecture decision
// (ADR-011 plus its domain decisions D-2..D-6) is not accepted, no
// production schema/API/Registry/Runner surface may be built or enabled -
// only the disposable prototype environment stays reachable.
//
// The gate is deliberately tiny: it holds decision states, exposes a
// thread-safe state machine and answers RequireOpen(feature) with M9-031
// naming the first non-accepted decision, so callers can report exactly
// which decision still blocks the feature.
package decision

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ConceptError is an M9 concept-taxonomy error (code + name), shaped like
// the sibling M9 packages so the e2e regression can extract codes
// uniformly.
type ConceptError struct {
	Code string
	Name string
}

func (e *ConceptError) Error() string { return e.Code + " " + e.Name }

func (e *ConceptError) Is(target error) bool {
	var other *ConceptError
	if errors.As(target, &other) {
		return other.Code == e.Code
	}
	return false
}

// ErrADR011Blocked is M9-031 ADR_011_BLOCKED (HTTP 424): architecture
// decision not yet unblocked.
var ErrADR011Blocked = &ConceptError{"M9-031", "ADR_011_BLOCKED"}

// Code extracts the M9 concept code when err carries one.
func Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// State is a decision lifecycle state (mirrors the ADR front-matter).
type State string

const (
	StateProposed State = "proposed"
	StateAccepted State = "accepted"
	StateRejected State = "rejected"
)

// Required lists every decision the M9 production gate depends on:
// ADR-011 itself plus the five domain decisions D-2..D-6.
var Required = []string{"ADR-011", "D-2", "D-3", "D-4", "D-5", "D-6"}

// Gate is the shared decision registry guarding M9 production surfaces.
type Gate struct {
	mu        sync.RWMutex
	decisions map[string]State
}

// NewGate opens a gate with every required decision in StateProposed -
// the documented M9 starting condition (阻断期).
func NewGate() *Gate {
	g := &Gate{decisions: make(map[string]State, len(Required))}
	for _, id := range Required {
		g.decisions[id] = StateProposed
	}
	return g
}

// Accept records a decision as accepted. Unknown ids refuse so a typo can
// never silently satisfy the gate.
func (g *Gate) Accept(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.decisions[id]; !ok {
		return fmt.Errorf("decision: unknown decision id %q", id)
	}
	g.decisions[id] = StateAccepted
	return nil
}

// Reject records a decision as rejected (re-blocking the gate).
func (g *Gate) Reject(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.decisions[id]; !ok {
		return fmt.Errorf("decision: unknown decision id %q", id)
	}
	g.decisions[id] = StateRejected
	return nil
}

// FirstBlocking returns the first non-accepted required decision (sorted
// for determinism), or "" when the gate is open.
func (g *Gate) FirstBlocking() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	pending := make([]string, 0, len(g.decisions))
	for id, st := range g.decisions {
		if st != StateAccepted {
			pending = append(pending, id)
		}
	}
	if len(pending) == 0 {
		return ""
	}
	sort.Strings(pending)
	return pending[0]
}

// RequireOpen answers nil only when every required decision is accepted;
// otherwise it fails closed with M9-031 naming the first blocking
// decision and the calling feature, e.g.
//
//	gate.RequireOpen("schema-migration") → M9-031 ADR_011_BLOCKED
//
// Production entry points (schema migrations, bridge schemas, Registry
// registration, Runner dispatch) must call this before enabling anything.
func (g *Gate) RequireOpen(feature string) error {
	if id := g.FirstBlocking(); id != "" {
		return fmt.Errorf("%w: %q blocked by decision %s (accept it first)", ErrADR011Blocked, feature, id)
	}
	return nil
}

// Snapshot copies the current decision states (sorted by id by the
// caller's rendering; map form keeps the e2e assertion simple).
func (g *Gate) Snapshot() map[string]State {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]State, len(g.decisions))
	for id, st := range g.decisions {
		out[id] = st
	}
	return out
}

// Open reports whether every required decision is accepted.
func (g *Gate) Open() bool { return g.FirstBlocking() == "" }
