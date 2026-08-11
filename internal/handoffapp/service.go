package handoffapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/handoff"
	"github.com/oklog/ulid/v2"
)

// ErrCheckpointNotFound is returned when the referenced checkpoint does not exist.
var ErrCheckpointNotFound = errors.New("checkpoint not found")

// ErrCheckpointNotSucceeded is returned when the referenced checkpoint is not succeeded.
var ErrCheckpointNotSucceeded = errors.New("checkpoint not succeeded")

// ErrCapsuleNotFound is returned when the capsule does not exist.
var ErrCapsuleNotFound = errors.New("capsule not found")

// ErrCapsuleNotActive is returned when the capsule is not in active state.
var ErrCapsuleNotActive = errors.New("capsule not active")

// ErrCapsuleExpired is returned when the capsule has expired.
var ErrCapsuleExpired = errors.New("capsule expired")

// CheckpointReader provides access to compaction checkpoints for capsule creation.
type CheckpointReader interface {
	GetCheckpoint(ctx context.Context, id string) (*compaction.Checkpoint, error)
}

// CapsuleStore defines the storage operations for handoff capsules.
type CapsuleStore interface {
	CreateCapsule(ctx context.Context, c handoff.Capsule) (handoff.Capsule, error)
	GetCapsule(ctx context.Context, id string) (*handoff.Capsule, error)
	ListCapsulesBySourceSession(ctx context.Context, sessionID string, limit int) ([]handoff.Capsule, error)
	ListActiveCapsules(ctx context.Context, sessionID string) ([]handoff.Capsule, error)
	ActivateCapsule(ctx context.Context, id string, destSessionID string) error
	RevokeCapsule(ctx context.Context, id string) error
	ExpireCapsule(ctx context.Context, id string) error
}

// Service provides handoff capsule operations.
// The Engine delegates to this service for cross-window handoff
// creation, inspection, and activation (ADR-005 §5).
type Service struct {
	checkpoints CheckpointReader
	capsules    CapsuleStore
	idFactory   func() string
	now         func() time.Time
}

// NewService creates a new handoff service.
func NewService(checkpoints CheckpointReader, capsules CapsuleStore) *Service {
	return &Service{
		checkpoints: checkpoints,
		capsules:    capsules,
		idFactory:   func() string { return ulid.Make().String() },
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// CreateCapsuleRequest defines the parameters for creating a handoff capsule.
type CreateCapsuleRequest struct {
	SourceSessionID string
	CheckpointID    string
	// RecentMessageIDs is a JSON array of message IDs retained verbatim
	// within a bounded budget (ADR-005 §5: "latest relevant original turns
	// retained verbatim within a bounded budget").
	RecentMessageIDs []string
	// ActiveTasksJSON is the active task/TODO state JSON.
	ActiveTasksJSON string
	// ExpiresAt is the optional expiration time. When nil, the capsule
	// never expires.
	ExpiresAt *time.Time
}

// CreateCapsule creates a handoff capsule from a succeeded compaction checkpoint.
// The capsule carries the checkpoint's structured summary plus active task state
// and recent message IDs for cross-window continuation (ADR-005 §5).
func (s *Service) CreateCapsule(ctx context.Context, req CreateCapsuleRequest) (handoff.Capsule, error) {
	// 1. Load and validate the source checkpoint.
	cp, err := s.checkpoints.GetCheckpoint(ctx, req.CheckpointID)
	if err != nil {
		return handoff.Capsule{}, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return handoff.Capsule{}, ErrCheckpointNotFound
	}
	if cp.Status != compaction.StatusSucceeded {
		return handoff.Capsule{}, fmt.Errorf("%w: status %s", ErrCheckpointNotSucceeded, cp.Status)
	}
	if cp.SessionID != req.SourceSessionID {
		return handoff.Capsule{}, errors.New("checkpoint session mismatch")
	}

	// 2. Serialize recent message IDs.
	recentIDsJSON := "[]"
	if len(req.RecentMessageIDs) > 0 {
		data, err := json.Marshal(req.RecentMessageIDs)
		if err != nil {
			return handoff.Capsule{}, fmt.Errorf("marshal recent message IDs: %w", err)
		}
		recentIDsJSON = string(data)
	}

	// 3. Serialize active tasks.
	activeTasks := req.ActiveTasksJSON
	if activeTasks == "" {
		activeTasks = "[]"
	}

	// 4. Compute capsule digest: SHA-256 of checkpoint ID + source digest +
	// recent message IDs + active tasks. This binds the capsule to its
	// source checkpoint and carried state (ADR-005 §5: "source checkpoint
	// and Message-range digests").
	digestInput := fmt.Sprintf("%s:%s:%s:%s", cp.ID, cp.SourceDigest, recentIDsJSON, activeTasks)
	digest := sha256.Sum256([]byte(digestInput))

	// 5. Create the capsule.
	capsule := handoff.Capsule{
		ID:               s.idFactory(),
		SourceSessionID:  req.SourceSessionID,
		CheckpointID:     req.CheckpointID,
		ActiveTasksJSON:  activeTasks,
		RecentMessageIDs: recentIDsJSON,
		Digest:           hex.EncodeToString(digest[:]),
		Status:           handoff.StatusActive,
		CreatedAt:        s.now(),
		ExpiresAt:        req.ExpiresAt,
	}

	created, err := s.capsules.CreateCapsule(ctx, capsule)
	if err != nil {
		return handoff.Capsule{}, fmt.Errorf("create capsule: %w", err)
	}
	return created, nil
}

// GetCapsule returns a capsule by ID for inspection (ADR-005 §5: "allows the
// user to inspect the summary and jump to source Messages").
func (s *Service) GetCapsule(ctx context.Context, id string) (*handoff.Capsule, error) {
	c, err := s.capsules.GetCapsule(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get capsule: %w", err)
	}
	if c == nil {
		return nil, ErrCapsuleNotFound
	}
	return c, nil
}

// ListCapsulesBySourceSession returns capsules for a source session.
func (s *Service) ListCapsulesBySourceSession(ctx context.Context, sessionID string, limit int) ([]handoff.Capsule, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.capsules.ListCapsulesBySourceSession(ctx, sessionID, limit)
}

// ListActiveCapsules returns all active capsules for a session.
func (s *Service) ListActiveCapsules(ctx context.Context, sessionID string) ([]handoff.Capsule, error) {
	return s.capsules.ListActiveCapsules(ctx, sessionID)
}

// ActivateCapsuleResult describes the outcome of capsule activation.
type ActivateCapsuleResult struct {
	Capsule       handoff.Capsule
	Checkpoint    *compaction.Checkpoint
	DigestValid   bool
	ExpiredCheck  bool
}

// ActivateCapsule binds a capsule to a destination session and activates it.
// The Engine validates the capsule (digest binding, expiration) before
// activation (ADR-005 §5: "The Engine, not the Renderer, validates and
// activates capsules").
//
// On success, the capsule's summary becomes available as prior context
// for the destination session. The caller is responsible for injecting
// the summary into the assembled prompt via AssembleOptions.PriorSummary.
func (s *Service) ActivateCapsule(ctx context.Context, capsuleID, destSessionID string) (ActivateCapsuleResult, error) {
	result := ActivateCapsuleResult{}

	// 1. Load the capsule.
	capsule, err := s.capsules.GetCapsule(ctx, capsuleID)
	if err != nil {
		return result, fmt.Errorf("get capsule: %w", err)
	}
	if capsule == nil {
		return result, ErrCapsuleNotFound
	}
	if capsule.Status != handoff.StatusActive {
		return result, fmt.Errorf("%w: status %s", ErrCapsuleNotActive, capsule.Status)
	}

	// 2. Check expiration.
	if capsule.ExpiresAt != nil && s.now().After(*capsule.ExpiresAt) {
		// Auto-expire the capsule.
		_ = s.capsules.ExpireCapsule(ctx, capsuleID)
		result.ExpiredCheck = true
		return result, ErrCapsuleExpired
	}

	// 3. Load the source checkpoint to verify digest binding.
	cp, err := s.checkpoints.GetCheckpoint(ctx, capsule.CheckpointID)
	if err != nil {
		return result, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return result, ErrCheckpointNotFound
	}

	// 4. Verify digest: recompute and compare.
	digestInput := fmt.Sprintf("%s:%s:%s:%s", cp.ID, cp.SourceDigest, capsule.RecentMessageIDs, capsule.ActiveTasksJSON)
	expectedDigest := sha256.Sum256([]byte(digestInput))
	result.DigestValid = hex.EncodeToString(expectedDigest[:]) == capsule.Digest
	if !result.DigestValid {
		return result, errors.New("capsule digest mismatch: checkpoint or carried state has been tampered with")
	}

	// 5. Activate the capsule.
	if err := s.capsules.ActivateCapsule(ctx, capsuleID, destSessionID); err != nil {
		return result, fmt.Errorf("activate capsule: %w", err)
	}

	// 6. Reload the activated capsule.
	activated, err := s.capsules.GetCapsule(ctx, capsuleID)
	if err != nil {
		return result, fmt.Errorf("reload capsule: %w", err)
	}
	result.Capsule = *activated
	result.Checkpoint = cp
	return result, nil
}

// RevokeCapsule revokes an active capsule.
func (s *Service) RevokeCapsule(ctx context.Context, id string) error {
	capsule, err := s.capsules.GetCapsule(ctx, id)
	if err != nil {
		return fmt.Errorf("get capsule: %w", err)
	}
	if capsule == nil {
		return ErrCapsuleNotFound
	}
	if capsule.Status != handoff.StatusActive {
		return fmt.Errorf("%w: status %s", ErrCapsuleNotActive, capsule.Status)
	}
	return s.capsules.RevokeCapsule(ctx, id)
}
