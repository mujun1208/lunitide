// Package audit implements the M7 audit outbox (T-7.5.2): an append-only
// WORM ledger whose rows form a tamper-evident hash chain. Every event
// records actor, action, resource, before/after digests, correlation id and
// the previous event's hash; the event hash covers the canonical event
// document, so any edit, deletion, reordering or insertion breaks the chain
// and VerifyChain reports ErrChainBroken (M7-DR-001 - production
// promotions freeze until the ledger is reconciled).
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrChainBroken answers any detected chain inconsistency (M7-DR-001).
var ErrChainBroken = errors.New("audit: hash chain broken")

// GenesisPrev is the prev_hash of the first chain event (seq 1).
const GenesisPrev = "0000000000000000000000000000000000000000000000000000000000000000"

// Event is one append-only audit ledger row. Seq is strictly increasing and
// gap-free from 1; PrevHash chains to the prior event; EventHash is the
// SHA-256 over the canonical document (excluding EventHash itself).
type Event struct {
	Seq           int64
	ID            string
	Action        string
	ResourceType  string
	ResourceID    string
	Actor         string
	BeforeDigest  string
	AfterDigest   string
	CorrelationID string
	PrevHash      string
	EventHash     string
	CreatedAt     string // UTC RFC3339
}

// eventDoc is the canonical hashing document (fixed field order).
type eventDoc struct {
	Seq           int64  `json:"seq"`
	ID            string `json:"id"`
	Action        string `json:"action"`
	ResourceType  string `json:"resource_type"`
	ResourceID    string `json:"resource_id"`
	Actor         string `json:"actor"`
	BeforeDigest  string `json:"before_digest,omitempty"`
	AfterDigest   string `json:"after_digest,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	PrevHash      string `json:"prev_hash"`
	CreatedAt     string `json:"created_at"`
}

// ComputeHash derives the event hash from the canonical document. The hash
// binds every mutable field plus the chain position, so two events with
// identical content at different seq values hash differently.
func ComputeHash(e Event) string {
	doc := eventDoc{
		Seq: e.Seq, ID: e.ID, Action: e.Action,
		ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		Actor: e.Actor, BeforeDigest: e.BeforeDigest, AfterDigest: e.AfterDigest,
		CorrelationID: e.CorrelationID, PrevHash: e.PrevHash, CreatedAt: e.CreatedAt,
	}
	b, _ := json.Marshal(doc)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Seal returns a copy with EventHash computed. Callers must set Seq,
// PrevHash and CreatedAt first; Seal panics on an unchainable event so a
// programming error surfaces before anything reaches the ledger.
func Seal(e Event) Event {
	if e.Seq < 1 || len(e.PrevHash) != 64 {
		panic("audit: event is not chainable (seq/prevHash unset)")
	}
	e.EventHash = ComputeHash(e)
	return e
}

// Link chains event e after prev: seq continues the sequence and prevHash
// binds to the prior event hash (GenesisPrev when prev is nil).
func Link(prev *Event, e Event) Event {
	if prev == nil {
		e.Seq, e.PrevHash = 1, GenesisPrev
	} else {
		e.Seq, e.PrevHash = prev.Seq+1, prev.EventHash
	}
	return Seal(e)
}

// VerifyChain walks events in storage order and proves, for every row:
// seq continuity from 1, prev-hash linkage to the prior event hash, and a
// recomputed event hash. Any inconsistency answers ErrChainBroken wrapped
// with the offending seq. An empty chain is intact.
func VerifyChain(events []Event) error {
	var prev *Event
	for i := range events {
		e := &events[i]
		wantSeq := int64(i + 1)
		if e.Seq != wantSeq {
			return fmt.Errorf("%w: seq %d, want %d", ErrChainBroken, e.Seq, wantSeq)
		}
		if i == 0 {
			if e.PrevHash != GenesisPrev {
				return fmt.Errorf("%w: seq 1 prev_hash is not genesis", ErrChainBroken)
			}
		} else if e.PrevHash != prev.EventHash {
			return fmt.Errorf("%w: seq %d prev_hash mismatch", ErrChainBroken, e.Seq)
		}
		if got := ComputeHash(*e); got != e.EventHash {
			return fmt.Errorf("%w: seq %d event_hash mismatch", ErrChainBroken, e.Seq)
		}
		prev = e
	}
	return nil
}