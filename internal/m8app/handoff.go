// M8 slice-3 application services (T-8.3.x/T-8.5.x): handoff.accept,
// tombstone.delete and sync.push.
//
// handoff.accept: expired refusals answer M8-014 (410), unread-before-
// accept is enforced on the read path (M8-015) and repeated accepts replay
// idempotently with the original effectiveAt. tombstone.delete: the read
// face hides immediately, propagation keeps the row unreadable until every
// projection ACKs (M8-016 202-poll / M8-017 resume-on-failure) and the
// collected ACK set yields the proof digest. sync.push: revoked devices are
// blocked (M8-019), stale ACK watermarks refused (M8-020) and same-leaf
// concurrent edits enter the explicit conflict box (M8-018) - distinct
// leaves auto-merge via pointwise-max vector clocks.
package m8app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M8 slice-3 error family (04 错误矩阵 M8-013~020).
var (
	// ErrHandoffNotFound: handoff row missing.
	ErrHandoffNotFound = errors.New("m8app: handoff not found")
	// ErrHandoffExpired: offer past expiry (M8-014, 410).
	ErrHandoffExpired = errors.New("m8app: handoff expired")
	// ErrHandoffNotAccepted: read attempted before accept (M8-015).
	ErrHandoffNotAccepted = errors.New("m8app: handoff not accepted")
	// ErrHandoffRedacted: the offer was policy-redacted and needs
	// reconfirmation (M8-013).
	ErrHandoffRedacted = errors.New("m8app: handoff redacted")
	// ErrTombstoneInProgress: propagation still running (M8-016, 202).
	ErrTombstoneInProgress = errors.New("m8app: tombstone in progress")
	// ErrTombstoneCascadeFailed: propagation failed; row stays unreadable
	// and the cursor resumes (M8-017).
	ErrTombstoneCascadeFailed = errors.New("m8app: tombstone cascade failed")
	// ErrTombstoneConfirmInvalid: the deletion confirmation token is
	// malformed.
	ErrTombstoneConfirmInvalid = errors.New("m8app: tombstone confirmation invalid")
	// ErrSyncVectorConflict: same-leaf concurrent edits entered the
	// conflict box (M8-018) - returned alongside the conflict payload,
	// never as a silent merge.
	ErrSyncVectorConflict = errors.New("m8app: sync vector conflict")
	// ErrDeviceRevoked: device revoked, push blocked (M8-019).
	ErrDeviceRevoked = errors.New("m8app: device revoked")
	// ErrSyncAckStale: the pushed watermark is behind the known ACK
	// watermark (M8-020).
	ErrSyncAckStale = errors.New("m8app: sync ack stale")
	// ErrDeviceNotFound: device never registered.
	ErrDeviceNotFound = errors.New("m8app: device not found")
)

// HandoffTx is the slice-3 single-writer transaction (handoff, tombstone,
// device and conflict tables plus the shared audit ledger and the memory
// fact read-face guard).
type HandoffTx interface {
	PutHandoff(m8core.Handoff) error
	GetHandoff(id string) (m8core.Handoff, error)
	TransitionHandoff(id, from, to string) error
	GetTombstoneByRoot(rootRef string) (m8core.Tombstone, bool, error)
	PutTombstone(m8core.Tombstone) error
	TombstoneFacts(rootRef, scopeID string) (int64, error)
	// ListTombstoneProjections answers the projection names the cascade
	// must reach (single-engine set: memory_facts read face, kb chunk
	// projection, graph node binding).
	ListTombstoneProjections() ([]string, error)
	PutDeviceReplica(m8core.DeviceReplica) error
	GetDeviceReplica(deviceID string) (m8core.DeviceReplica, error)
	ListOpenConflicts() ([]m8core.SyncConflict, error)
	PutSyncConflict(m8core.SyncConflict) error
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// HandoffUnitOfWork is the slice-3 single-writer boundary.
type HandoffUnitOfWork interface {
	TransactHandoff(ctx context.Context, fn func(HandoffTx) error) error
}

// Propagator is the resumable cascade step of tombstone.delete: it walks
// the dependency graph from the cursor and answers the advanced cursor
// plus the projections ACKed in this pass. An error keeps the tombstone
// propagating (M8-017) with the persisted cursor for resume.
type Propagator func(ctx context.Context, t m8core.Tombstone, projections []string) (cursor string, acks []string, err error)

// DefaultPropagator ACKs the full local projection set in one pass (the
// single-engine cascade has no remote replicas).
func DefaultPropagator(ctx context.Context, t m8core.Tombstone, projections []string) (string, []string, error) {
	return "{}", projections, nil
}

// HandoffService implements the slice-3 use cases.
type HandoffService struct {
	uow       HandoffUnitOfWork
	clock     Clock
	subject   string
	propagate Propagator
}

// NewHandoffService wires the slice-3 service.
func NewHandoffService(uow HandoffUnitOfWork, localSubject string) *HandoffService {
	return &HandoffService{uow: uow, clock: systemClock{}, subject: localSubject, propagate: DefaultPropagator}
}

// SetClock substitutes the clock (tests).
func (s *HandoffService) SetClock(c Clock) { s.clock = c }

// SetPropagator substitutes the cascade step (M8-017 failure tests).
func (s *HandoffService) SetPropagator(p Propagator) { s.propagate = p }

// OfferHandoffInput feeds the internal send-side offer path (redaction
// happens before the offer is stored; the log records the difference).
type OfferHandoffInput struct {
	HandoffID string
	Sender    string
	Receiver  string
	Manifest  string
	RedactionLog string
	TTL       time.Duration
}

// OfferHandoff stores one sent offer with expiry (internal path; the
// bridge only exposes accept).
func (s *HandoffService) OfferHandoff(ctx context.Context, in OfferHandoffInput) (m8core.Handoff, error) {
	if s == nil || s.uow == nil {
		return m8core.Handoff{}, ErrServiceUnavailable
	}
	if len(in.HandoffID) != 26 || len(in.Sender) < 1 || len(in.Receiver) < 1 ||
		len(in.Manifest) < 2 || len(in.RedactionLog) < 2 {
		return m8core.Handoff{}, fmt.Errorf("%w: offer fields invalid", ErrPayloadInvalid)
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = m8core.DefaultHandoffTTL
	}
	now := s.clock.Now().UTC()
	h := m8core.Handoff{
		ID:           in.HandoffID,
		Sender:       in.Sender,
		Receiver:     in.Receiver,
		Manifest:     in.Manifest,
		RedactionLog: in.RedactionLog,
		State:        m8core.HandoffSent,
		ExpiresAt:    now.Add(ttl).Format(time.RFC3339),
		CreatedAt:    now.Format(time.RFC3339),
	}
	err := s.uow.TransactHandoff(ctx, func(tx HandoffTx) error {
		if err := tx.PutHandoff(h); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "handoff.offer",
			ResourceType: "handoff", ResourceID: h.ID,
			Actor: actorOr(in.Sender), CreatedAt: h.CreatedAt,
		})
		return err
	})
	return h, err
}

// ReadHandoff enforces M8-015: the manifest is unreadable before accept
// (only the redaction log stays visible to the receiver).
func (s *HandoffService) ReadHandoff(ctx context.Context, handoffID string) (manifest string, err error) {
	if s == nil || s.uow == nil {
		return "", ErrServiceUnavailable
	}
	err = s.uow.TransactHandoff(ctx, func(tx HandoffTx) error {
		h, err := tx.GetHandoff(handoffID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrHandoffNotFound, handoffID)
		}
		if err != nil {
			return err
		}
		if h.State != m8core.HandoffAccepted {
			return fmt.Errorf("%w: %s", ErrHandoffNotAccepted, handoffID)
		}
		manifest = h.Manifest
		return nil
	})
	return manifest, err
}

// HandoffAcceptInput is the handoff.accept command.
type HandoffAcceptInput struct {
	HandoffID string
	RequestID string
	Actor     string
}

// HandoffAcceptResult is the handoff.accept outcome.
type HandoffAcceptResult struct {
	HandoffID   string `json:"handoffId"`
	State       string `json:"state"`
	EffectiveAt string `json:"effectiveAt"`
}

// AcceptHandoff enacts the idempotent accept: expired offers refuse with
// M8-014 (also flipping the row to expired), accepted offers replay the
// original effectiveAt, sent offers transition inside one transaction with
// an audit event.
func (s *HandoffService) AcceptHandoff(ctx context.Context, in HandoffAcceptInput) (HandoffAcceptResult, error) {
	if s == nil || s.uow == nil {
		return HandoffAcceptResult{}, ErrServiceUnavailable
	}
	if len(in.HandoffID) != 26 {
		return HandoffAcceptResult{}, fmt.Errorf("%w: handoffId invalid", ErrPayloadInvalid)
	}
	now := s.clock.Now().UTC()
	var out HandoffAcceptResult
	err := s.uow.TransactHandoff(ctx, func(tx HandoffTx) error {
		h, err := tx.GetHandoff(in.HandoffID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrHandoffNotFound, in.HandoffID)
		}
		if err != nil {
			return err
		}
		state, expired, idem := h.AcceptGuard(now)
		if expired {
			if h.State == m8core.HandoffSent {
				if err := tx.TransitionHandoff(h.ID, m8core.HandoffSent, m8core.HandoffExpired); err != nil {
					return err
				}
			}
			return fmt.Errorf("%w: %s", ErrHandoffExpired, in.HandoffID)
		}
		if idem {
			out = HandoffAcceptResult{HandoffID: h.ID, State: m8core.HandoffAccepted, EffectiveAt: h.CreatedAt}
			return nil
		}
		if state != m8core.HandoffSent {
			return fmt.Errorf("%w: state %s", ErrHandoffRedacted, h.State)
		}
		effective := now.Format(time.RFC3339)
		if err := tx.TransitionHandoff(h.ID, m8core.HandoffSent, m8core.HandoffAccepted); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "handoff.accept",
			ResourceType: "handoff", ResourceID: h.ID,
			Actor: actorOr(in.Actor), CorrelationID: in.RequestID,
			CreatedAt: effective,
		}); err != nil {
			return err
		}
		out = HandoffAcceptResult{HandoffID: h.ID, State: m8core.HandoffAccepted, EffectiveAt: effective}
		return nil
	})
	if err != nil {
		return HandoffAcceptResult{}, err
	}
	return out, nil
}

// TombstoneDeleteInput is the tombstone.delete command.
type TombstoneDeleteInput struct {
	RootRef            string
	ScopeID            string
	ConfirmationToken  string
	Actor              string
}

// TombstoneDeleteResult is the tombstone.delete outcome.
type TombstoneDeleteResult struct {
	TombstoneID  string   `json:"tombstoneId"`
	State        string   `json:"state"`
	CascadeCursor string  `json:"cascadeCursor"`
	AckSet       []string `json:"ackSet"`
}

// DeleteWithTombstone enacts FR-07: the read face hides immediately (facts
// of the root flip to tombstoned before any cascade), then the cascade
// runs from the resumable cursor collecting per-projection ACKs. Full ACK
// coverage promotes the row to verified with the proof digest; a cascade
// error keeps it propagating and unreadable (M8-017). Re-deleting the same
// root is idempotent and answers the persisted state.
func (s *HandoffService) DeleteWithTombstone(ctx context.Context, in TombstoneDeleteInput) (TombstoneDeleteResult, error) {
	if s == nil || s.uow == nil {
		return TombstoneDeleteResult{}, ErrServiceUnavailable
	}
	if len(in.RootRef) < 1 || len(in.RootRef) > m8core.MaxRootRef ||
		len(in.ScopeID) < 1 || len(in.ScopeID) > m8core.MaxScopeID ||
		!m8core.ValidHexDigest(in.ConfirmationToken) {
		return TombstoneDeleteResult{}, fmt.Errorf("%w: rootRef/scope/token invalid", ErrTombstoneConfirmInvalid)
	}
	now := s.clock.Now().UTC()
	var out TombstoneDeleteResult
	var cascadeErr error
	err := s.uow.TransactHandoff(ctx, func(tx HandoffTx) error {
		if prior, has, err := tx.GetTombstoneByRoot(in.RootRef); err != nil {
			return err
		} else if has {
			// Idempotent re-delete answers the persisted state.
			acks := ackSetOf(prior)
			out = TombstoneDeleteResult{
				TombstoneID: prior.ID, State: prior.State,
				CascadeCursor: prior.CascadeCursor, AckSet: acks,
			}
			if prior.State == m8core.TombPropagating || prior.State == m8core.TombPending {
				return fmt.Errorf("%w: %s", ErrTombstoneInProgress, prior.ID)
			}
			return nil
		}
		projections, err := tx.ListTombstoneProjections()
		if err != nil {
			return err
		}
		if len(projections) < 1 || len(projections) > m8core.MaxAckSet {
			return fmt.Errorf("%w: projection set %d", ErrTombstoneCascadeFailed, len(projections))
		}
		// Read face hides first: the fact row flips to tombstoned before
		// the cascade starts.
		if _, err := tx.TombstoneFacts(in.RootRef, in.ScopeID); err != nil {
			return err
		}
		t := m8core.Tombstone{
			ID:            ulid.Make().String(),
			RootRef:       in.RootRef,
			CascadeCursor: "{}",
			AckSet:        "[]",
			State:         m8core.TombPropagating,
			CreatedAt:     now.Format(time.RFC3339),
		}
		cursor, acks, aerr := s.propagate(ctx, t, projections)
		if aerr != nil {
			// M8-017: the propagating row MUST persist (the read face
			// stays hidden and the cursor survives for resume), so the
			// error rides outside the transaction instead of rolling it
			// back; the caller polls (M8-016).
			if err := tx.PutTombstone(t); err != nil {
				return err
			}
			out = TombstoneDeleteResult{TombstoneID: t.ID, State: t.State, CascadeCursor: t.CascadeCursor, AckSet: []string{}}
			cascadeErr = fmt.Errorf("%w: %v", ErrTombstoneCascadeFailed, aerr)
			return nil
		}
		ackJSON, err := json.Marshal(acks)
		if err != nil {
			return err
		}
		t.CascadeCursor = cursor
		t.AckSet = string(ackJSON)
		t.ProofDigest = m8core.DigestOf(t.ID + "|" + t.RootRef + "|" + string(ackJSON))
		t.State = m8core.TombVerified
		t.CompletedAt = now.Format(time.RFC3339)
		if err := tx.PutTombstone(t); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "tombstone.delete",
			ResourceType: "memory_tombstone", ResourceID: t.ID,
			Actor: actorOr(in.Actor), AfterDigest: t.ProofDigest,
			CreatedAt: t.CreatedAt,
		}); err != nil {
			return err
		}
		out = TombstoneDeleteResult{TombstoneID: t.ID, State: t.State, CascadeCursor: cursor, AckSet: acks}
		return nil
	})
	if err != nil {
		return out, err
	}
	if cascadeErr != nil {
		return out, cascadeErr
	}
	return out, nil
}

// SyncPushInput is the sync.push command.
type SyncPushInput struct {
	DeviceID    string
	VectorClock map[string]int64
	Edits       []m8core.SyncEdit
}

// SyncPushResult is the sync.push outcome.
type SyncPushResult struct {
	AckWatermark int64                    `json:"ackWatermark"`
	Conflicts    []SyncPushConflictView   `json:"conflicts"`
}

// SyncPushConflictView mirrors the x-result conflict item.
type SyncPushConflictView struct {
	ConflictID  string            `json:"conflictId"`
	FactID      string            `json:"factId"`
	JSONPointer string            `json:"jsonPointer"`
	Variants    []json.RawMessage `json:"variants"`
}

// Push enacts the device sync: revoked devices are blocked (M8-019), a
// watermark behind the known ACK is refused (M8-020), distinct-leaf edits
// auto-merge with pointwise-max clocks and same-leaf concurrent edits
// enter the explicit conflict box (M8-018) with every variant preserved.
func (s *HandoffService) Push(ctx context.Context, in SyncPushInput) (SyncPushResult, error) {
	if s == nil || s.uow == nil {
		return SyncPushResult{}, ErrServiceUnavailable
	}
	if len(in.DeviceID) != 26 || in.VectorClock == nil || len(in.VectorClock) > m8core.MaxVectorClock ||
		len(in.Edits) > m8core.MaxSyncEdits {
		return SyncPushResult{}, fmt.Errorf("%w: deviceId/clock/edits invalid", ErrPayloadInvalid)
	}
	for _, e := range in.Edits {
		if len(e.FactID) != 26 || !m8core.ValidJSONPointer(e.JSONPointer) || len(e.Source) < 1 {
			return SyncPushResult{}, fmt.Errorf("%w: edit fields invalid", ErrPayloadInvalid)
		}
	}
	now := s.clock.Now().UTC()
	out := SyncPushResult{Conflicts: []SyncPushConflictView{}}
	err := s.uow.TransactHandoff(ctx, func(tx HandoffTx) error {
		dev, err := tx.GetDeviceReplica(in.DeviceID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrDeviceNotFound, in.DeviceID)
		}
		if err != nil {
			return err
		}
		if dev.TrustState == m8core.DeviceRevoked {
			return fmt.Errorf("%w: %s", ErrDeviceRevoked, in.DeviceID)
		}
		known, err := m8core.ParseVectorClock(dev.VectorClock)
		if err != nil {
			return err
		}
		pushed := in.DeviceID
		pushedKnown, ok := known[pushed]
		pushedHave := in.VectorClock[pushed]
		if ok && pushedHave < pushedKnown {
			// The device pushed from a watermark behind the server's
			// record (M8-020): refuse instead of regressing.
			return fmt.Errorf("%w: device %s pushed %d < known %d", ErrSyncAckStale, in.DeviceID, pushedHave, pushedKnown)
		}
		open, err := tx.ListOpenConflicts()
		if err != nil {
			return err
		}
		merged, conflicted := m8core.DetectLeafConflicts(in.Edits, open)
		// Group same-key conflicted edits into one conflict-box row each
		// (M8-018): every variant is preserved verbatim for manual
		// resolution - never a silent last-write-wins.
		groups := map[string][]m8core.SyncEdit{}
		var order []string
		for _, c := range conflicted {
			key := c.FactID + "|" + c.JSONPointer
			if _, seen := groups[key]; !seen {
				order = append(order, key)
			}
			groups[key] = append(groups[key], c)
		}
		for _, key := range order {
			edits := groups[key]
			variants := make([]syncVariant, len(edits))
			raws := make([]json.RawMessage, len(edits))
			for i, e := range edits {
				variants[i] = syncVariant{FactID: e.FactID, Version: e.Version, Value: e.Value, Source: e.Source}
				raws[i] = e.Value
			}
			conflictID := ulid.Make().String()
			vb, err := json.Marshal(variants)
			if err != nil {
				return err
			}
			if err := tx.PutSyncConflict(m8core.SyncConflict{
				ID: conflictID, JSONPointer: edits[0].JSONPointer,
				Variants: string(vb), State: m8core.ConflictOpen,
				CreatedAt: now.Format(time.RFC3339),
			}); err != nil {
				return err
			}
			out.Conflicts = append(out.Conflicts, SyncPushConflictView{
				ConflictID: conflictID, FactID: edits[0].FactID,
				JSONPointer: edits[0].JSONPointer, Variants: raws,
			})
		}
		// Distinct-leaf edits auto-merge: the merged clock advances.
		mergedClock := m8core.MergeVectorClock(known, in.VectorClock)
		// Auto-merged edits cannot regress the device watermark either.
		watermark := mergedClock[pushed]
		clockJSON, err := m8core.VectorClockJSON(mergedClock)
		if err != nil {
			return err
		}
		dev.VectorClock = clockJSON
		dev.LastAck = watermark
		if err := tx.PutDeviceReplica(dev); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "sync.push",
			ResourceType: "device_replica", ResourceID: in.DeviceID,
			Actor: actorOr(s.subject), CorrelationID: fmt.Sprintf("merged=%d,conflicts=%d", len(merged), len(conflicted)),
			CreatedAt: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		out.AckWatermark = watermark
		return nil
	})
	if err != nil {
		return SyncPushResult{}, err
	}
	return out, nil
}

// RegisterDevice bootstraps a trusted replica (internal path).
func (s *HandoffService) RegisterDevice(ctx context.Context, deviceID, subjectID string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	clockJSON, err := m8core.VectorClockJSON(map[string]int64{deviceID: 0})
	if err != nil {
		return err
	}
	return s.uow.TransactHandoff(ctx, func(tx HandoffTx) error {
		return tx.PutDeviceReplica(m8core.DeviceReplica{
			DeviceID: deviceID, SubjectID: subjectID,
			VectorClock: clockJSON, LastAck: 0,
			TrustState: m8core.DeviceTrusted,
			CreatedAt:  s.clock.Now().UTC().Format(time.RFC3339),
		})
	})
}

// RevokeDevice flips the trust state (internal path; M8-019 block source).
func (s *HandoffService) RevokeDevice(ctx context.Context, deviceID string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return s.uow.TransactHandoff(ctx, func(tx HandoffTx) error {
		dev, err := tx.GetDeviceReplica(deviceID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
		}
		if err != nil {
			return err
		}
		dev.TrustState = m8core.DeviceRevoked
		return tx.PutDeviceReplica(dev)
	})
}

// syncVariant is one conflict-box variant document.
type syncVariant struct {
	FactID  string          `json:"factId"`
	Version int64           `json:"version"`
	Value   json.RawMessage `json:"value"`
	Source  string          `json:"source"`
}

// ackSetOf decodes the persisted ACK set JSON.
func ackSetOf(t m8core.Tombstone) []string {
	var out []string
	_ = json.Unmarshal([]byte(t.AckSet), &out)
	if out == nil {
		out = []string{}
	}
	return out
}
