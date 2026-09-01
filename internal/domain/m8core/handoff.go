// M8 slice-3 domain (T-8.3.x/T-8.5.x): handoff acceptance, recursive
// tombstones and the device-sync conflict box.
//
// Handoffs are unread before accept (M8-015), expired refusals answer
// HANDOFF_EXPIRED (M8-014, 410) and repeated accepts are idempotent.
// Tombstones hide the read face immediately, then propagate along the
// dependency graph with a resumable cursor; propagation keeps the row
// unreadable until every projection ACKs (M8-016/017). Devices carry vector
// clocks; revoked devices are blocked (M8-019), stale ACK watermarks are
// refused (M8-020) and same-leaf concurrent edits enter the explicit
// conflict box (M8-018) - never silent last-write-wins.
package m8core

import (
	"encoding/json"
	"fmt"
	"time"
)

// Handoff states (migration 0063 CHECK).
const (
	HandoffSent     = "sent"
	HandoffAccepted = "accepted"
	HandoffExpired  = "expired"
)

// Tombstone states.
const (
	TombPending     = "pending"
	TombPropagating = "propagating"
	TombVerified    = "verified"
	TombCompacted   = "compacted"
)

// Device trust states.
const (
	DeviceTrusted = "trusted"
	DeviceRevoked = "revoked"
)

// Sync conflict states.
const (
	ConflictOpen     = "open"
	ConflictResolved = "resolved"
)

// Field limits mirroring migration 0063 CHECKs.
const (
	MaxRootRef      = 128
	MaxVectorClock  = 64 // entries
	MaxSyncEdits    = 100
	MaxAckSet       = 64
	// DefaultHandoffTTL backs the internal offer path.
	DefaultHandoffTTL = 72 * time.Hour
)

// Handoff is one redacted offer row.
type Handoff struct {
	ID           string
	Sender       string
	Receiver     string
	Manifest     string // canonical JSON
	RedactionLog string // canonical JSON
	State        string
	ExpiresAt    string
	CreatedAt    string
	// AcceptedAt is when the offer was accepted, empty until it is. A
	// repeated accept replays this rather than recomputing, so the effective
	// time an accepted handoff reports never moves.
	AcceptedAt string
}

// AcceptGuard decides the handoff.accept outcome: expired refusals answer
// ErrHandoffExpired (410), accepted rows replay idempotently with the
// original effect, only sent rows transition.
func (h Handoff) AcceptGuard(now time.Time) (state string, expired bool, idempotent bool) {
	if h.State == HandoffAccepted {
		return HandoffAccepted, false, true
	}
	if h.State == HandoffExpired {
		return HandoffExpired, true, false
	}
	exp, err := time.Parse(time.RFC3339, h.ExpiresAt)
	if err != nil {
		return HandoffExpired, true, false
	}
	if now.After(exp) {
		return HandoffExpired, true, false
	}
	return HandoffSent, false, false
}

// Tombstone is one recursive deletion row (FR-07).
type Tombstone struct {
	ID           string
	RootRef      string
	CascadeCursor string
	AckSet       string // canonical JSON array
	ProofDigest  string
	State        string
	CreatedAt    string
	CompletedAt  string
}

// DeviceReplica carries one device's vector clock and ACK watermark.
type DeviceReplica struct {
	DeviceID    string
	SubjectID   string
	VectorClock string // canonical JSON {"device":n}
	LastAck     int64
	TrustState  string
	CreatedAt   string
}

// ParseVectorClock decodes the canonical JSON vector clock map.
func ParseVectorClock(s string) (map[string]int64, error) {
	out := map[string]int64{}
	if s == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("m8core: vector clock %q: %w", s, err)
	}
	if len(out) > MaxVectorClock {
		return nil, fmt.Errorf("m8core: vector clock entries %d > %d", len(out), MaxVectorClock)
	}
	return out, nil
}

// MergeVectorClock answers the pointwise-max merge of two clocks.
func MergeVectorClock(a, b map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v > out[k] {
			out[k] = v
		}
	}
	return out
}

// VectorClockJSON canonicalizes a clock (sorted keys via json.Marshal on
// map[string]int64).
func VectorClockJSON(c map[string]int64) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SyncEdit is one device edit arriving via sync.push.
type SyncEdit struct {
	FactID      string          `json:"factId"`
	Version     int64           `json:"version"`
	JSONPointer string          `json:"jsonPointer"`
	Value       json.RawMessage `json:"value"`
	Source      string          `json:"source"`
}

// SyncConflict is one explicit conflict-box row (M8-018).
type SyncConflict struct {
	ID          string
	JSONPointer string
	Variants    string // canonical JSON [{factId,version,value,source},...]
	Resolution  string
	State       string
	CreatedAt   string
}

// DetectLeafConflicts partitions edits into merged (distinct leaves) and
// conflicted (same factId+jsonPointer appearing more than once, or already
// sitting in an open conflict row). Order is preserved for merged edits.
func DetectLeafConflicts(edits []SyncEdit, openConflicts []SyncConflict) (merged []SyncEdit, conflicted []SyncEdit) {
	pos := map[string]int{}
	dup := map[string]bool{}
	openLeaf := map[string]bool{}
	for _, c := range openConflicts {
		if c.State == ConflictOpen {
			// Conflict rows carry no factId column (it lives inside the
			// variants JSON), so open leaves match by JSONPointer alone.
			openLeaf[c.JSONPointer] = true
		}
	}
	for _, e := range edits {
		key := e.FactID + "|" + e.JSONPointer
		if dup[key] || openLeaf[e.JSONPointer] {
			conflicted = append(conflicted, e)
			continue
		}
		if i, ok := pos[key]; ok {
			// First duplicate for this leaf: retract the earlier edit from
			// merged so every variant enters the conflict box verbatim
			// instead of a silent last-write-wins.
			conflicted = append(conflicted, merged[i])
			merged = append(merged[:i], merged[i+1:]...)
			for j := i; j < len(merged); j++ {
				pos[merged[j].FactID+"|"+merged[j].JSONPointer] = j
			}
			delete(pos, key)
			dup[key] = true
			conflicted = append(conflicted, e)
			continue
		}
		pos[key] = len(merged)
		merged = append(merged, e)
	}
	return merged, conflicted
}
