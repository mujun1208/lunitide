// Package legal implements the M9 slice-4 Legal Hold authority flow and
// delete gate (T-9.4.3, ADR-011/D-5 accepted, threat model m9-06 S6/S7).
//
// Authority flow: activating and releasing a hold both demand a concrete
// authority reference (case number) — M9-029 when missing. Every state
// change and every evidence access/export is journaled (激活/访问/导出/解除
// 全审计). Release additionally demands its own authority basis, never a
// blank reuse of the activation.
//
// Delete gate (T-18, zero wrongful purges): a delete that matches an active
// hold scope is NEVER physically purged. The object is hidden from the
// user's view, moved into the restricted evidence store and replaced with a
// tombstone (用户侧隐藏 + 转受限证据库 + 替代墓碑 → M9-028 LEGAL_HOLD_ACTIVE).
// The gate screens twice: once against the hold projection and once against
// the authority registry itself, so a projection lag cannot purge a
// preserved object.
//
// Restore order (T-19, S7): a restored deployment must replay holds (and
// policy / identity revocation waterlines, owned elsewhere) BEFORE reads
// open. This registry fail-closes: ScreenDelete and evidence access on an
// un-replayed registry are refused, eliminating the restoration-window leak
// (备份恢复先加载 Hold).
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-028 LEGAL_HOLD_ACTIVE            保全阻断清除
//	M9-029 LEGAL_HOLD_AUTHORITY_REQUIRED 缺权威依据
package legal

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ConceptError is an M9 concept-taxonomy error (code + name).
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

var (
	ErrHoldActive          = &ConceptError{"M9-028", "LEGAL_HOLD_ACTIVE"}
	ErrHoldAuthorityNeeded = &ConceptError{"M9-029", "LEGAL_HOLD_AUTHORITY_REQUIRED"}
)

// M9Code extracts the M9 concept code when err carries one.
func M9Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Hold states.
const (
	HoldActive   = "active"
	HoldReleased = "released"
)

// Hold is one preservation order: an org-scoped subject selector backed by
// an authority reference, with a bounded lifetime.
type Hold struct {
	ID               string
	OrgID            string
	Scope            string // e.g. "user:alice", "project:p1", "message:01JDM..."
	AuthorityRef     string // case / order number — the legal basis
	State            string
	EvidenceStoreRef string
	ExpiresAt        time.Time
	ActivatedBy      string
	ActivatedAt      time.Time
	ReleasedBy       string
	ReleasedAt       time.Time
	ReleaseAuthority string
}

// PreservedObject is one diverted object living in the restricted store.
type PreservedObject struct {
	ObjectID   string
	OrgID      string
	HoldID     string
	EvidenceOf string // where it was diverted from
	PreservedAt time.Time
}

// Tombstone is the user-facing replacement for a diverted object.
type Tombstone struct {
	ObjectID  string
	OrgID     string
	HoldID    string
	Notice    string
	CreatedAt time.Time
}

// DeleteDecision is the gate's verdict for one screened delete.
type DeleteDecision struct {
	ObjectID   string
	OrgID      string
	Redirected bool // true → evidence store, never a physical purge
	HoldID     string
	Tombstone  Tombstone
}

// EventKind lists the journaled hold lifecycle / evidence operations.
const (
	EventActivate      = "hold.activate"
	EventRelease       = "hold.release"
	EventScreen        = "hold.screen"
	EventScreenBlocked = "hold.screen.blocked"
	EventAccess        = "evidence.access"
	EventExport        = "evidence.export"
)

// Event is one journal line of the hold authority flow.
type Event struct {
	At    time.Time
	Kind  string
	OrgID string
	Actor string
	HoldID string
	Detail string
}

// Registry is the hold registry + projection + restricted evidence store.
type Registry struct {
	mu         sync.Mutex
	holds      map[string]*Hold
	projection map[string]map[string]bool // orgID → active hold ids (快查投影)
	evidence   map[string][]PreservedObject // hold id → diverted objects
	tombstones map[string]Tombstone         // object id → tombstone
	journal    []Event
	replayed   bool // S7: restore replays holds before reads open
}

// NewRegistry builds a live (already replayed) registry.
func NewRegistry() *Registry {
	return &Registry{
		holds:      make(map[string]*Hold),
		projection: make(map[string]map[string]bool),
		evidence:   make(map[string][]PreservedObject),
		tombstones: make(map[string]Tombstone),
		replayed:   true,
	}
}

// NewRestoringRegistry builds a registry in restore mode: it stays closed
// (fail-closed) until ReplaySnapshot loads the hold state (T-19).
func NewRestoringRegistry() *Registry {
	r := NewRegistry()
	r.replayed = false
	return r
}

func (r *Registry) project(h *Hold) {
	set, ok := r.projection[h.OrgID]
	if !ok {
		set = make(map[string]bool)
		r.projection[h.OrgID] = set
	}
	set[h.ID] = true
}

// ReplaySnapshot loads a backed-up hold set during restoration. It must run
// before reads open; activating new holds or screening deletes on a closed
// registry is refused (恢复窗口泄漏防护).
func (r *Registry) ReplaySnapshot(holds []Hold, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.replayed {
		return errors.New("legal: registry is already open; replay is a restore-time operation")
	}
	for i := range holds {
		h := holds[i]
		if h.ID == "" || h.OrgID == "" || h.Scope == "" {
			return fmt.Errorf("legal: snapshot hold %d lacks id/org/scope", i)
		}
		cp := h
		if cp.State == "" {
			cp.State = HoldActive
		}
		r.holds[cp.ID] = &cp
		if cp.State == HoldActive {
			r.project(&cp)
		}
	}
	r.replayed = true
	r.journal = append(r.journal, Event{At: at, Kind: "hold.replay", OrgID: "*", Actor: "restore", Detail: fmt.Sprintf("replayed %d holds before reads opened", len(holds))})
	return nil
}

// Activate registers a preservation order. Missing authority reference,
// missing expiry or a non-future expiry refuses with M9-029 / errors; the
// activation is journaled.
func (r *Registry) Activate(h Hold, actor string, now time.Time) (*Hold, error) {
	if h.ID == "" || h.OrgID == "" || h.Scope == "" {
		return nil, errors.New("legal: hold id, org and scope are required")
	}
	if h.AuthorityRef == "" {
		return nil, fmt.Errorf("%w: activation of hold %s needs an authority reference (case/order number)", ErrHoldAuthorityNeeded, h.ID)
	}
	if h.ExpiresAt.IsZero() || !h.ExpiresAt.After(now) {
		return nil, fmt.Errorf("%w: hold %s needs a future expiry (bounded preservation)", ErrHoldAuthorityNeeded, h.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.replayed {
		return nil, errors.New("legal: registry not replayed yet (restore order T-19: hold snapshot first)")
	}
	if _, dup := r.holds[h.ID]; dup {
		return nil, fmt.Errorf("legal: hold %s already exists", h.ID)
	}
	h.State = HoldActive
	if h.EvidenceStoreRef == "" {
		h.EvidenceStoreRef = "evidence://" + h.OrgID + "/" + h.ID
	}
	h.ActivatedBy = actor
	h.ActivatedAt = now
	r.holds[h.ID] = &h
	r.project(&h)
	r.journal = append(r.journal, Event{At: now, Kind: EventActivate, OrgID: h.OrgID, Actor: actor, HoldID: h.ID, Detail: h.AuthorityRef})
	return &h, nil
}

// Release lifts a hold — only with its own authority basis (never a blank
// reuse of the activation reference), journaled like every state change.
func (r *Registry) Release(holdID, authorityRef, actor string, now time.Time) error {
	if authorityRef == "" {
		return fmt.Errorf("%w: releasing hold %s needs its own authority basis", ErrHoldAuthorityNeeded, holdID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.holds[holdID]
	if !ok {
		return fmt.Errorf("legal: unknown hold %s", holdID)
	}
	if h.State != HoldActive {
		return fmt.Errorf("legal: hold %s is already %s", holdID, h.State)
	}
	h.State = HoldReleased
	h.ReleasedBy = actor
	h.ReleasedAt = now
	h.ReleaseAuthority = authorityRef
	if set, ok := r.projection[h.OrgID]; ok {
		delete(set, holdID)
	}
	r.journal = append(r.journal, Event{At: now, Kind: EventRelease, OrgID: h.OrgID, Actor: actor, HoldID: holdID, Detail: authorityRef})
	return nil
}

func scopeMatches(holdScope, objectScope string) bool {
	if holdScope == "*" {
		return true
	}
	// exact subject or a parent selector prefix ("project:p1" covers
	// "project:p1/message:01JDM...")
	if holdScope == objectScope || strings.HasPrefix(objectScope, holdScope+"/") {
		return true
	}
	// "user:alice" also covers anything carrying the subject as a segment
	for _, seg := range strings.Split(objectScope, "/") {
		if seg == holdScope {
			return true
		}
	}
	return false
}

// screenProjection walks the fast projection set (one org's active ids).
func (r *Registry) screenProjection(orgID, objectScope string, now time.Time) *Hold {
	for id := range r.projection[orgID] {
		h, ok := r.holds[id]
		if !ok || h.State != HoldActive || now.After(h.ExpiresAt) {
			continue
		}
		if scopeMatches(h.Scope, objectScope) {
			return h
		}
	}
	return nil
}

// screenAuthority re-walks the full authority registry (sorted for stable
// verdicts) — the second, independent source of truth.
func (r *Registry) screenAuthority(orgID, objectScope string, now time.Time) *Hold {
	for _, id := range r.sortedHoldIDs() {
		h := r.holds[id]
		if h.OrgID != orgID || h.State != HoldActive || now.After(h.ExpiresAt) {
			continue
		}
		if scopeMatches(h.Scope, objectScope) {
			return h
		}
	}
	return nil
}

func (r *Registry) sortedHoldIDs() []string {
	ids := make([]string, 0, len(r.holds))
	for id := range r.holds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ScreenDelete is the delete gate (T-18). A delete matching an active hold
// is diverted: hidden from the user, preserved in the restricted evidence
// store and replaced by a tombstone — the physical purge path never runs
// for preserved objects (零误清除). The gate screens twice: projection pass,
// then an independent re-walk of the authority registry (二次查权威库), and
// fail-closes on any disagreement (M9-028).
func (r *Registry) ScreenDelete(orgID, objectScope, objectID, actor string, now time.Time) (DeleteDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.replayed {
		return DeleteDecision{}, errors.New("legal: registry not replayed yet (restore order T-19: hold snapshot first)")
	}
	first := r.screenProjection(orgID, objectScope, now)
	second := r.screenAuthority(orgID, objectScope, now)
	if (first == nil) != (second == nil) {
		return DeleteDecision{}, fmt.Errorf("%w: hold projection disagrees with authority registry for object %s — gate fails closed", ErrHoldActive, objectID)
	}
	if first == nil {
		d := DeleteDecision{ObjectID: objectID, OrgID: orgID}
		r.journal = append(r.journal, Event{At: now, Kind: EventScreen, OrgID: orgID, Actor: actor, Detail: objectID + " cleared"})
		return d, nil // no hold: ordinary delete may proceed
	}
	h := first
	if _, already := r.tombstones[objectID]; !already {
		r.evidence[h.ID] = append(r.evidence[h.ID], PreservedObject{
			ObjectID: objectID, OrgID: orgID, HoldID: h.ID,
			EvidenceOf: objectScope, PreservedAt: now,
		})
		r.tombstones[objectID] = Tombstone{
			ObjectID: objectID, OrgID: orgID, HoldID: h.ID,
			Notice:    "preserved under legal hold; removed from your view",
			CreatedAt: now,
		}
	}
	r.journal = append(r.journal, Event{At: now, Kind: EventScreenBlocked, OrgID: orgID, Actor: actor, HoldID: h.ID, Detail: objectID + " diverted to evidence store"})
	return DeleteDecision{
		ObjectID: objectID, OrgID: orgID, Redirected: true, HoldID: h.ID,
		Tombstone: r.tombstones[objectID],
	}, nil
}

// Held reports whether an org/scope currently sits under an active hold
// (projection read for other delete-adjacent flows).
func (r *Registry) Held(orgID, objectScope string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.screenAuthority(orgID, objectScope, now) != nil
}

// AccessEvidence records an evidence-store access (访问审计). Access without
// an authority reference refuses with M9-029.
func (r *Registry) AccessEvidence(holdID, authorityRef, actor string, now time.Time) ([]PreservedObject, error) {
	if authorityRef == "" {
		return nil, fmt.Errorf("%w: evidence access for hold %s needs an authority reference", ErrHoldAuthorityNeeded, holdID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.holds[holdID]
	if !ok {
		return nil, fmt.Errorf("legal: unknown hold %s", holdID)
	}
	if h.State != HoldActive {
		return nil, fmt.Errorf("legal: hold %s is %s; evidence is sealed", holdID, h.State)
	}
	r.journal = append(r.journal, Event{At: now, Kind: EventAccess, OrgID: h.OrgID, Actor: actor, HoldID: holdID, Detail: authorityRef})
	out := make([]PreservedObject, len(r.evidence[holdID]))
	copy(out, r.evidence[holdID])
	return out, nil
}

// ExportEvidence exports preserved objects (导出审计), authority-gated the
// same way as access.
func (r *Registry) ExportEvidence(holdID, authorityRef, actor string, now time.Time) ([]PreservedObject, error) {
	if authorityRef == "" {
		return nil, fmt.Errorf("%w: evidence export for hold %s needs an authority reference", ErrHoldAuthorityNeeded, holdID)
	}
	objs, err := r.AccessEvidence(holdID, authorityRef+"/export", actor, now)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	h := r.holds[holdID]
	r.journal = append(r.journal, Event{At: now, Kind: EventExport, OrgID: h.OrgID, Actor: actor, HoldID: holdID, Detail: fmt.Sprintf("exported %d objects under %s", len(objs), authorityRef)})
	r.mu.Unlock()
	return objs, nil
}

// Tombstone returns the replacement marker for a diverted object.
func (r *Registry) Tombstone(objectID string) (Tombstone, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tombstones[objectID]
	return t, ok
}

// Journal returns the audit journal of the authority flow.
func (r *Registry) Journal() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.journal))
	copy(out, r.journal)
	return out
}

// ActiveHolds lists one org's active holds (ops view).
func (r *Registry) ActiveHolds(orgID string, now time.Time) []Hold {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Hold
	for _, id := range r.sortedHoldIDs() {
		h := r.holds[id]
		if h.OrgID == orgID && h.State == HoldActive && !now.After(h.ExpiresAt) {
			out = append(out, *h)
		}
	}
	return out
}
