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

// ErrSourceDeleted is returned when the capsule's source checkpoint or session
// has been deleted. Import fails closed: a deleted source can never be
// imported as prior context (ADR-005 §5, §6 fail-closed deletion).
var ErrSourceDeleted = errors.New("capsule source deleted")

// ErrDigestMismatch is returned when the capsule digest does not match the
// recomputed digest. This indicates tampering with the checkpoint or carried
// state (ADR-005 §5: "source checkpoint and Message-range digests").
var ErrDigestMismatch = errors.New("capsule digest mismatch")

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
	// RecordImport records that a capsule was imported into a target session
	// as provenance-linked untrusted prior context (ADR-005 §5). The
	// (capsule_id, target_session_id) pair is unique: a repeat import of the
	// same capsule into the same session is idempotent. Returns the import ID,
	// the imported_at timestamp, and isNew=true when this was a new import
	// (isNew=false on idempotent re-import).
	RecordImport(ctx context.Context, capsuleID, targetSessionID string) (importID string, importedAt time.Time, isNew bool, err error)
	// GetImport returns the imported_at timestamp for a capsule imported into
	// a target session. Returns ok=false when no import record exists.
	GetImport(ctx context.Context, capsuleID, targetSessionID string) (importedAt time.Time, ok bool, err error)
	// ListImportedCapsules returns all capsules imported into the target
	// session, ordered by imported_at descending. Each capsule carries its
	// source checkpoint summary as untrusted prior context.
	ListImportedCapsules(ctx context.Context, targetSessionID string) ([]handoff.Capsule, error)
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

// InspectCapsuleResult carries a capsule and its source checkpoint for
// inspection (ADR-005 §5: "allows the user to inspect the summary and jump
// to source Messages").
type InspectCapsuleResult struct {
	Capsule    handoff.Capsule
	Checkpoint *compaction.Checkpoint
}

// InspectCapsule returns a capsule by ID together with its source checkpoint.
// The checkpoint's summary content lets the Renderer display the carried
// context; the capsule's source_session_id and checkpoint source range let
// the user jump back to the source Messages. The checkpoint may be nil if
// the source has been deleted (deletion propagation); the caller must
// fail-closed when using the summary as prior context.
func (s *Service) InspectCapsule(ctx context.Context, id string) (InspectCapsuleResult, error) {
	capsule, err := s.capsules.GetCapsule(ctx, id)
	if err != nil {
		return InspectCapsuleResult{}, fmt.Errorf("get capsule: %w", err)
	}
	if capsule == nil {
		return InspectCapsuleResult{}, ErrCapsuleNotFound
	}
	result := InspectCapsuleResult{Capsule: *capsule}
	cp, err := s.checkpoints.GetCheckpoint(ctx, capsule.CheckpointID)
	if err != nil {
		return result, fmt.Errorf("get checkpoint: %w", err)
	}
	result.Checkpoint = cp
	return result, nil
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

// ImportCapsuleResult describes the outcome of importing a capsule into a
// target session. The capsule's summary becomes available as untrusted prior
// context for the target session (ADR-005 §5).
type ImportCapsuleResult struct {
	Capsule         handoff.Capsule
	Checkpoint      *compaction.Checkpoint
	DigestValid     bool
	ExpiredCheck    bool
	AlreadyImported bool // true when this was an idempotent re-import
	ImportedAt      time.Time
}

// ImportCapsule imports a capsule into a target session as provenance-linked
// untrusted prior context (ADR-005 §5). The Engine, not the Renderer, validates
// the capsule before recording the import.
//
// Semantics:
//   - The capsule must be active (not revoked/expired/activated). Revoked and
//     expired capsules are terminal and can never be imported.
//   - The source checkpoint must still exist; if it was deleted (deletion
//     propagation), import fails closed with ErrSourceDeleted.
//   - The capsule digest is recomputed from the source checkpoint and compared
//     to the stored digest. A mismatch indicates tampering and fails import.
//   - Repeat import of the same capsule into the same target session is
//     idempotent: it returns the existing import record without error.
//   - Import does NOT change the capsule status. The capsule remains active
//     and can be revoked later. This allows a single capsule to be imported
//     into multiple target sessions.
//   - The capsule's summary is never fabricated as original message history.
//     It is injected as untrusted prior context via ContextEnvelope.HandoffCapsules.
func (s *Service) ImportCapsule(ctx context.Context, capsuleID, targetSessionID string) (ImportCapsuleResult, error) {
	result := ImportCapsuleResult{}

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

	// 2. Check expiration. Auto-expire past-due capsules.
	if capsule.ExpiresAt != nil && s.now().After(*capsule.ExpiresAt) {
		_ = s.capsules.ExpireCapsule(ctx, capsuleID)
		result.ExpiredCheck = true
		return result, ErrCapsuleExpired
	}

	// 3. Load the source checkpoint to verify digest binding and fail-closed
	// on source deletion (ADR-005 §6: deletion propagation).
	cp, err := s.checkpoints.GetCheckpoint(ctx, capsule.CheckpointID)
	if err != nil {
		return result, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return result, ErrSourceDeleted
	}

	// 4. Verify digest: recompute and compare. A mismatch indicates the
	// checkpoint or carried state was tampered with.
	digestInput := fmt.Sprintf("%s:%s:%s:%s", cp.ID, cp.SourceDigest, capsule.RecentMessageIDs, capsule.ActiveTasksJSON)
	expectedDigest := sha256.Sum256([]byte(digestInput))
	result.DigestValid = hex.EncodeToString(expectedDigest[:]) == capsule.Digest
	if !result.DigestValid {
		return result, ErrDigestMismatch
	}

	// 5. Record the import. This is idempotent: a repeat import of the same
	// capsule into the same session returns the existing record.
	importID, importedAt, isNew, err := s.capsules.RecordImport(ctx, capsuleID, targetSessionID)
	if err != nil {
		return result, fmt.Errorf("record import: %w", err)
	}
	_ = importID // import ID is recorded for audit; not returned to Renderer
	result.ImportedAt = importedAt
	result.AlreadyImported = !isNew

	// 6. Reload the capsule to return current state.
	current, err := s.capsules.GetCapsule(ctx, capsuleID)
	if err != nil {
		return result, fmt.Errorf("reload capsule: %w", err)
	}
	result.Capsule = *current
	result.Checkpoint = cp
	return result, nil
}

// ListImportedCapsules returns all capsules imported into the target session,
// ordered by imported_at descending. Each capsule's summary is available as
// untrusted prior context for the session (ADR-005 §5).
func (s *Service) ListImportedCapsules(ctx context.Context, targetSessionID string) ([]handoff.Capsule, error) {
	return s.capsules.ListImportedCapsules(ctx, targetSessionID)
}

// CapsuleContext pairs an imported capsule with its source checkpoint. When
// Checkpoint is nil, the source has been deleted (deletion propagation) and
// the caller must fail-closed: skip the capsule rather than injecting stale
// or unverified content.
type CapsuleContext struct {
	Capsule    handoff.Capsule
	Checkpoint *compaction.Checkpoint
}

// ListImportedCapsuleContexts returns all capsules imported into the target
// session together with their source checkpoints. Capsules whose source
// checkpoint was deleted are returned with a nil Checkpoint so the caller
// can fail-closed. Revoked and expired capsules are skipped: they are no
// longer valid prior context (ADR-005 §5, §6).
//
// This is the primary read path used by chat.start to populate
// ContextEnvelope.HandoffCapsules.
func (s *Service) ListImportedCapsuleContexts(ctx context.Context, targetSessionID string) ([]CapsuleContext, error) {
	capsules, err := s.capsules.ListImportedCapsules(ctx, targetSessionID)
	if err != nil {
		return nil, fmt.Errorf("list imported capsules: %w", err)
	}
	result := make([]CapsuleContext, 0, len(capsules))
	for i := range capsules {
		c := &capsules[i]
		// Skip capsules that are no longer active: revoked/expired capsules
		// are terminal and their summaries must not be injected.
		if c.Status != handoff.StatusActive {
			continue
		}
		// Auto-expire past-due capsules.
		if c.ExpiresAt != nil && s.now().After(*c.ExpiresAt) {
			_ = s.capsules.ExpireCapsule(ctx, c.ID)
			continue
		}
		cp, err := s.checkpoints.GetCheckpoint(ctx, c.CheckpointID)
		if err != nil {
			return nil, fmt.Errorf("get checkpoint for capsule %s: %w", c.ID, err)
		}
		// If cp is nil, the source checkpoint was deleted. Fail-closed: skip
		// the capsule rather than injecting unverified content.
		result = append(result, CapsuleContext{Capsule: *c, Checkpoint: cp})
	}
	return result, nil
}

// IsImported returns true when a capsule has been imported into the target
// session. Used by the chat send path to decide whether to inject the
// capsule summary into the assembled envelope.
func (s *Service) IsImported(ctx context.Context, capsuleID, targetSessionID string) (bool, error) {
	_, ok, err := s.capsules.GetImport(ctx, capsuleID, targetSessionID)
	return ok, err
}
