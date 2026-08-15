// Package workspace implements the M5 AdHocWorkspace lifecycle (T-5.1.4):
// creation with a unique canonical root, soft/hard quotas (2/4 GiB frozen
// defaults), 7-day expiry with a 24 hour grace window, and a cancellable
// cleanup state machine. Writes beyond the hard quota are rejected read-only
// (M5-WS-001).
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/oklog/ulid/v2"
)

var (
	ErrUOWUnavailable   = errors.New("workspace: unit of work unavailable")
	ErrQuotaExceeded    = errors.New("workspace: hard quota exceeded, workspace is read-only (WS-001)")
	ErrInvalidInput     = errors.New("workspace: invalid input")
)

// Store is the storage contract satisfied by the agent-runtime transaction.
type Store interface {
	PutM5Workspace(m5workspace.Workspace) error
	GetM5Workspace(id string) (m5workspace.Workspace, error)
	GetM5WorkspaceByRoot(root string) (m5workspace.Workspace, error)
	ListM5Workspaces() ([]m5workspace.Workspace, error)
	TransitionM5Workspace(id string, expectedVersion int64, to m5workspace.State, usedBytes int64, lease time.Time, at time.Time) (m5workspace.Workspace, error)
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// AdHocService owns the AdHocWorkspace aggregate.
type AdHocService struct {
	uow   agentrunapp.UnitOfWork
	clock Clock
}

func NewAdHocService(uow agentrunapp.UnitOfWork) *AdHocService {
	return &AdHocService{uow: uow, clock: systemClock{}}
}

func (s *AdHocService) SetClock(c Clock) { s.clock = c }

func (s *AdHocService) store(tx agentrunapp.Tx) (Store, error) {
	st, ok := tx.(Store)
	if !ok {
		return nil, ErrUOWUnavailable
	}
	return st, nil
}

// Create registers a fresh active workspace for one chat Root Run. The
// canonical root is unique among live workspaces (partial unique index).
func (s *AdHocService) Create(ctx context.Context, runID, rootCanonical, displayPath string) (m5workspace.Workspace, error) {
	if s == nil || s.uow == nil {
		return m5workspace.Workspace{}, ErrUOWUnavailable
	}
	if runID == "" || rootCanonical == "" || displayPath == "" {
		return m5workspace.Workspace{}, ErrInvalidInput
	}
	digest := sha256.Sum256([]byte("m5-adhoc-root\x00" + rootCanonical))
	var out m5workspace.Workspace
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		if _, err := st.GetM5WorkspaceByRoot(rootCanonical); err == nil {
			return m5workspace.ErrRootInUse
		} else if !errors.Is(err, m5workspace.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		w := m5workspace.Workspace{
			ID:            ulid.Make().String(),
			RunID:         runID,
			RootCanonical: rootCanonical,
			DisplayPath:   displayPath,
			GrantJSON:     `{"schemaVersion":1}`,
			LeaseExpiry:   now.Add(m5workspace.ExpiryAfter),
			BaseDigest:    hex.EncodeToString(digest[:]),
			QuotaSoft:     m5workspace.DefaultQuotaSoft,
			QuotaHard:     m5workspace.DefaultQuotaHard,
			UsedBytes:     0,
			State:         m5workspace.StateActive,
			Version:       1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := st.PutM5Workspace(w); err != nil {
			return err
		}
		out = w
		return nil
	})
	return out, err
}

// Charge records used bytes after a write and refreshes the activity lease.
// Once the hard quota is breached the workspace flips to readonly_full and
// every further charge answers WS-001. The readonly_full transition itself
// must commit, so the business error is returned after a successful commit.
func (s *AdHocService) Charge(ctx context.Context, workspaceID string, delta int64) (m5workspace.Workspace, error) {
	if delta < 0 {
		return m5workspace.Workspace{}, ErrInvalidInput
	}
	var out m5workspace.Workspace
	var quotaErr error
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		w, err := st.GetM5Workspace(workspaceID)
		if err != nil {
			return err
		}
		if w.State == m5workspace.StateReadonlyFull || w.UsedBytes+delta > w.QuotaHard {
			if w.State != m5workspace.StateReadonlyFull {
				if w, err = st.TransitionM5Workspace(w.ID, w.Version, m5workspace.StateReadonlyFull, -1, time.Time{}, s.clock.Now().UTC()); err != nil {
					return err
				}
			}
			out = w
			quotaErr = ErrQuotaExceeded
			return nil // commit the readonly_full transition
		}
		now := s.clock.Now().UTC()
		w, err = st.TransitionM5Workspace(w.ID, w.Version, w.State, w.UsedBytes+delta, now.Add(m5workspace.ExpiryAfter), now)
		if err != nil {
			return err
		}
		out = w
		return nil
	})
	if quotaErr != nil {
		return out, quotaErr
	}
	return out, err
}

// Tick advances the expiry state machine for all live workspaces: entering
// the 24h grace window marks expiring; past the lease the workspace moves to
// cleaning (cleanup execution itself is out of scope for the state core).
func (s *AdHocService) Tick(ctx context.Context) error {
	if s == nil || s.uow == nil {
		return ErrUOWUnavailable
	}
	return s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		workspaces, err := st.ListM5Workspaces()
		if err != nil {
			return err
		}
		for _, w := range workspaces {
			switch {
			case (w.State == m5workspace.StateActive || w.State == m5workspace.StateReadonlyFull) && now.After(w.LeaseExpiry.Add(-m5workspace.GracePeriod)):
				if _, err := st.TransitionM5Workspace(w.ID, w.Version, m5workspace.StateExpiring, -1, time.Time{}, now); err != nil {
					return err
				}
			case w.State == m5workspace.StateExpiring && !now.Before(w.LeaseExpiry):
				if _, err := st.TransitionM5Workspace(w.ID, w.Version, m5workspace.StateCleaning, -1, time.Time{}, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Retain cancels a pending cleanup because the user kept artifacts or is
// converting the workspace; retained rows are terminal and never reaped.
func (s *AdHocService) Retain(ctx context.Context, workspaceID string) (m5workspace.Workspace, error) {
	var out m5workspace.Workspace
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		w, err := st.GetM5Workspace(workspaceID)
		if err != nil {
			return err
		}
		if w.State.Terminal() || !w.CanTransitionTo(m5workspace.StateRetained) {
			return m5workspace.ErrTransition
		}
		out, err = st.TransitionM5Workspace(w.ID, w.Version, m5workspace.StateRetained, -1, time.Time{}, s.clock.Now().UTC())
		return err
	})
	return out, err
}

// CompleteCleanup closes a cleaning workspace: success deletes it, a failure
// records cleaning_failed so the next pass can retry.
func (s *AdHocService) CompleteCleanup(ctx context.Context, workspaceID string, success bool) (m5workspace.Workspace, error) {
	var out m5workspace.Workspace
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		w, err := st.GetM5Workspace(workspaceID)
		if err != nil {
			return err
		}
		target := m5workspace.StateDeleted
		if !success {
			target = m5workspace.StateCleaningFailed
		}
		if !w.CanTransitionTo(target) {
			return m5workspace.ErrTransition
		}
		out, err = st.TransitionM5Workspace(w.ID, w.Version, target, -1, time.Time{}, s.clock.Now().UTC())
		return err
	})
	return out, err
}
