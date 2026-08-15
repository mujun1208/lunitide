// M5 T-5.5.1 workspace.convert: the AdHocWorkspace -> permanent project
// conversion state machine (preview -> copying -> publishing -> committed,
// terminals failed/abandoned). CVT-001: without an explicit user Confirm
// not a byte is copied. CVT-002: a failed publish rolls back and the source
// workspace is read-only from the first byte to the last.
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

// convertActor mirrors the desktop-host attribution the app packages use.
const convertActor = "desktop-host"

// ConversionPhase is the durable crash-recovery journal of a conversion.
type ConversionPhase string

const (
	PhasePreview    ConversionPhase = "preview"
	PhaseCopying    ConversionPhase = "copying"
	PhasePublishing ConversionPhase = "publishing"
	PhaseCommitted  ConversionPhase = "committed"
	PhaseFailed     ConversionPhase = "failed"
	PhaseAbandoned  ConversionPhase = "abandoned"
)

// Terminal phases stop all further transitions (the row stays for audit).
func (p ConversionPhase) Terminal() bool {
	return p == PhaseCommitted || p == PhaseFailed || p == PhaseAbandoned
}

// Conversion is the m5_workspace_conversion row.
type Conversion struct {
	ID                string          `json:"id"`
	RunID             string          `json:"runId"`
	SourceWorkspaceID string          `json:"sourceWorkspaceId"`
	TargetProjectID   string          `json:"targetProjectId"`
	PreviewDigest     string          `json:"previewDigest"`
	ScopeJSON         string          `json:"scopeJson"`
	Phase             ConversionPhase `json:"phase"`
	PublishJournal    string          `json:"publishJournal,omitempty"`
	Committed         bool            `json:"committed"`
	CommittedAt       *time.Time      `json:"committedAt,omitempty"`
	AuditEventID      string          `json:"auditEventId"`
	CreatedAt         time.Time       `json:"createdAt"`
}

// publishJournal is the durable crash-recovery shape of publish_journal:
// the directories of the in-flight publish plus every completed file move,
// persisted after each rename so a crash can undo exactly what landed.
type publishJournal struct {
	StagingDir string   `json:"stagingDir"`
	TargetRoot string   `json:"targetRoot"`
	Moved      []string `json:"moved"`
}

// ConvertScope is the shape of scope_json: what the user confirmed to copy.
type ConvertScope struct {
	MessageIDs  []string `json:"messageIds"`
	ArtifactIDs []string `json:"artifactIds"`
	Paths       []string `json:"paths"`
}

// Frozen conversion parameters.
const (
	// MaxConvertScopeEntries bounds a conversion scope.
	MaxConvertScopeEntries = 100
	// ConvertPreviewTTL bounds how long an unconfirmed preview may linger
	// before Reconcile abandons it.
	ConvertPreviewTTL = 24 * time.Hour
	// rollbackDir holds pre-overwrite backups inside staging.
	rollbackDir = ".rollback"
)

var (
	// ErrConvertNotFound answers CVT-404: unknown conversion id.
	ErrConvertNotFound = errors.New("workspace: conversion not found")
	// ErrConvertStateBad answers CVT-009: illegal conversion phase for this
	// operation.
	ErrConvertStateBad = errors.New("workspace: conversion phase does not allow this operation")
	// ErrConvertNoConfirm answers CVT-001: without an explicit user
	// confirmation not a byte is copied.
	ErrConvertNoConfirm = errors.New("workspace: conversion lacks explicit user confirmation (CVT-001)")
	// ErrConvertSourceGone answers CVT-410: the source workspace is deleted
	// (or missing).
	ErrConvertSourceGone = errors.New("workspace: source workspace is gone")
	// ErrConvertPublishFailed answers CVT-002: the publish failed, was
	// rolled back and the source is untouched.
	ErrConvertPublishFailed = errors.New("workspace: publish failed and was rolled back, source untouched (CVT-002)")
	// ErrConvertScopeInvalid answers CVT-400: malformed or oversized scope.
	ErrConvertScopeInvalid = errors.New("workspace: conversion scope invalid")
	// ErrConversionPhase is the storage CAS conflict: the phase already
	// moved on between read and update.
	ErrConversionPhase = errors.New("workspace: conversion phase conflict")
)

type cvtStore interface {
	GetM5Workspace(id string) (m5workspace.Workspace, error)
	PutM5Conversion(Conversion) error
	GetM5Conversion(id string) (Conversion, error)
	GetM5ConversionBySource(workspaceID string) (Conversion, error)
	UpdateM5ConversionPhase(id string, from, to ConversionPhase, at time.Time) error
	UpdateM5ConversionJournal(id, journal string) error
	MarkM5ConversionCommitted(id string, at time.Time) error
}

// ConvertService owns the workspace.convert aggregate.
type ConvertService struct {
	uow   agentrunapp.UnitOfWork
	clock Clock
}

func NewConvertService(uow agentrunapp.UnitOfWork) *ConvertService {
	return &ConvertService{uow: uow, clock: systemClock{}}
}

func (s *ConvertService) SetClock(c Clock) { s.clock = c }

func (s *ConvertService) store(tx agentrunapp.Tx) (cvtStore, error) {
	st, ok := tx.(cvtStore)
	if !ok {
		return nil, ErrUOWUnavailable
	}
	return st, nil
}

func validateScope(scope ConvertScope) error {
	total := len(scope.MessageIDs) + len(scope.ArtifactIDs) + len(scope.Paths)
	if total == 0 || total > MaxConvertScopeEntries {
		return fmt.Errorf("%w: %d entries", ErrConvertScopeInvalid, total)
	}
	for _, p := range scope.Paths {
		if err := ValidateRelPath(p); err != nil {
			return fmt.Errorf("%w: %s", ErrConvertScopeInvalid, p)
		}
	}
	return nil
}

// Preview pre-checks the source workspace, fingerprints its tree and
// journals a phase=preview conversion. Nothing is copied yet; the digest
// lets the user compare what they confirm against what will be converted.
func (s *ConvertService) Preview(ctx context.Context, runID, sourceWorkspaceID, targetProjectID string, scope ConvertScope) (Conversion, error) {
	if s == nil || s.uow == nil {
		return Conversion{}, ErrUOWUnavailable
	}
	if runID == "" || sourceWorkspaceID == "" || targetProjectID == "" {
		return Conversion{}, ErrInvalidInput
	}
	if err := validateScope(scope); err != nil {
		return Conversion{}, err
	}
	scopeJSON, err := json.Marshal(scope)
	if err != nil {
		return Conversion{}, err
	}
	var out Conversion
	err = s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		w, err := st.GetM5Workspace(sourceWorkspaceID)
		if err != nil {
			if errors.Is(err, m5workspace.ErrNotFound) {
				return ErrConvertSourceGone
			}
			return err
		}
		if w.State == m5workspace.StateDeleted {
			return ErrConvertSourceGone
		}
		root, err := NewSecureRoot(w.RootCanonical)
		if err != nil {
			return err
		}
		digest, err := TreeDigest(root)
		if err != nil {
			return err
		}
		auditID := ulid.Make().String()
		out = Conversion{
			ID:                ulid.Make().String(),
			RunID:             runID,
			SourceWorkspaceID: sourceWorkspaceID,
			TargetProjectID:   targetProjectID,
			PreviewDigest:     digest,
			ScopeJSON:         string(scopeJSON),
			Phase:             PhasePreview,
			AuditEventID:      auditID,
			CreatedAt:         s.clock.Now().UTC(),
		}
		meta, err := json.Marshal(map[string]any{
			"conversionId": out.ID, "sourceWorkspaceId": sourceWorkspaceID,
			"targetProjectId": targetProjectID, "previewDigest": digest,
		})
		if err != nil {
			return err
		}
		if err := tx.PutAudit(providerapp.Audit{
			ID: auditID, Action: "workspace.conversion.previewed",
			AggregateID: out.ID, Actor: convertActor,
			Metadata: meta, CreatedAt: out.CreatedAt,
		}); err != nil {
			return err
		}
		return st.PutM5Conversion(out)
	})
	return out, err
}

// Confirm records the explicit user confirmation and is the only door from
// preview to copying (CVT-001): StageCopy refuses anything not in copying.
func (s *ConvertService) Confirm(ctx context.Context, conversionID string) (Conversion, error) {
	var out Conversion
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		c, err := st.GetM5Conversion(conversionID)
		if err != nil {
			return err
		}
		if c.Phase != PhasePreview {
			return ErrConvertStateBad
		}
		w, err := st.GetM5Workspace(c.SourceWorkspaceID)
		if err != nil {
			if errors.Is(err, m5workspace.ErrNotFound) {
				return ErrConvertSourceGone
			}
			return err
		}
		if w.State == m5workspace.StateDeleted {
			return ErrConvertSourceGone
		}
		if err := st.UpdateM5ConversionPhase(c.ID, PhasePreview, PhaseCopying, s.clock.Now().UTC()); err != nil {
			return err
		}
		c.Phase = PhaseCopying
		out = c
		return nil
	})
	return out, err
}

// StageCopy copies the scoped files from the read-only source root into the
// caller-provided staging directory, mirroring the relative structure. A
// single-file failure marks the conversion failed; the source is never
// touched. On success the conversion moves to publishing.
func (s *ConvertService) StageCopy(ctx context.Context, conversionID, sourceRoot, stagingDir string) (Conversion, error) {
	var c Conversion
	var scope ConvertScope
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		if c, err = st.GetM5Conversion(conversionID); err != nil {
			return err
		}
		if c.Phase != PhaseCopying {
			if c.Phase == PhasePreview {
				// CVT-001: no confirmation, not a byte is copied.
				return fmt.Errorf("%w: %w", ErrConvertStateBad, ErrConvertNoConfirm)
			}
			return ErrConvertStateBad
		}
		return json.Unmarshal([]byte(c.ScopeJSON), &scope)
	})
	if err != nil {
		return c, err
	}
	root, err := NewSecureRoot(sourceRoot)
	if err != nil {
		return s.failStaged(ctx, c), ErrConvertPublishFailed
	}
	for _, rel := range scope.Paths {
		if err := stageOne(root, stagingDir, rel); err != nil {
			return s.failStaged(ctx, c), ErrConvertPublishFailed
		}
	}
	err = s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		return st.UpdateM5ConversionPhase(c.ID, PhaseCopying, PhasePublishing, s.clock.Now().UTC())
	})
	if err != nil {
		return c, err
	}
	c.Phase = PhasePublishing
	return c, nil
}

// stageOne reads one file through the secure root (path safety + reparse
// refusal) and writes the mirror image under staging.
func stageOne(root *SecureRoot, stagingDir, rel string) error {
	if err := ValidateRelPath(rel); err != nil {
		return err
	}
	data, ok := readFileSecure(root, rel)
	if !ok {
		return fmt.Errorf("stage copy failed: %s", rel)
	}
	dst := filepath.Join(stagingDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// failStaged converges a failed stage copy to failed and returns the
// updated snapshot. The source was never written (CVT-002).
func (s *ConvertService) failStaged(ctx context.Context, c Conversion) Conversion {
	_ = s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		return st.UpdateM5ConversionPhase(c.ID, PhaseCopying, PhaseFailed, s.clock.Now().UTC())
	})
	c.Phase = PhaseFailed
	return c
}

// Publish atomically moves the staged files into the target root. An
// existing target file is backed up to staging/.rollback first; any failure
// restores the moved files from the backup (or removes newly created
// ones), marks the conversion failed and answers CVT-002. All moves green
// commits the conversion.
func (s *ConvertService) Publish(ctx context.Context, conversionID, stagingDir, targetRoot string) (Conversion, error) {
	var c Conversion
	var scope ConvertScope
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		if c, err = st.GetM5Conversion(conversionID); err != nil {
			return err
		}
		if c.Phase != PhasePublishing {
			return ErrConvertStateBad
		}
		return json.Unmarshal([]byte(c.ScopeJSON), &scope)
	})
	if err != nil {
		return c, err
	}
	// durable crash-recovery journal: persisted after every completed
	// move so a crash between file operations can undo exactly what landed
	journal := publishJournal{StagingDir: stagingDir, TargetRoot: targetRoot, Moved: []string{}}
	persistJournal := func() error {
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		return s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
			st, err := s.store(tx)
			if err != nil {
				return err
			}
			return st.UpdateM5ConversionJournal(c.ID, string(raw))
		})
	}
	var moved []string
	fail := func() (Conversion, error) {
		rollbackMoved(stagingDir, targetRoot, moved)
		_ = s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
			st, err := s.store(tx)
			if err != nil {
				return err
			}
			if err := st.UpdateM5ConversionPhase(c.ID, PhasePublishing, PhaseFailed, s.clock.Now().UTC()); err != nil {
				return err
			}
			return st.UpdateM5ConversionJournal(c.ID, "{}")
		})
		c.Phase = PhaseFailed
		return c, ErrConvertPublishFailed
	}
	for _, rel := range scope.Paths {
		if err := ValidateRelPath(rel); err != nil {
			return fail()
		}
		src := filepath.Join(stagingDir, filepath.FromSlash(rel))
		if _, err := os.Stat(src); err != nil {
			return fail() // staging incomplete
		}
		dst := filepath.Join(targetRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fail()
		}
		if _, err := os.Stat(dst); err == nil {
			// Conflict: back the target file up before overwriting.
			backup := filepath.Join(stagingDir, rollbackDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
				return fail()
			}
			if err := os.Rename(dst, backup); err != nil {
				return fail()
			}
			if err := os.Rename(src, dst); err != nil {
				// the displaced target byte must not stay stranded in the
				// backup dir when the replacement lands nowhere; moved has
				// not recorded this rel yet so fail() cannot restore it.
				_ = os.Rename(backup, dst)
				return fail()
			}
			moved = append(moved, rel) // restorable from the backup
			journal.Moved = append([]string{}, moved...)
			if err := persistJournal(); err != nil {
				return fail()
			}
			continue
		} else if !os.IsNotExist(err) {
			return fail()
		}
		if err := os.Rename(src, dst); err != nil {
			return fail()
		}
		moved = append(moved, rel) // newly created, rollback removes
		journal.Moved = append([]string{}, moved...)
		if err := persistJournal(); err != nil {
			return fail()
		}
	}
	now := s.clock.Now().UTC()
	err = s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		if err := st.MarkM5ConversionCommitted(c.ID, now); err != nil {
			return err
		}
		meta, err := json.Marshal(map[string]any{
			"conversionId": c.ID, "files": len(moved), "previewDigest": c.PreviewDigest,
		})
		if err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "workspace.conversion.published",
			AggregateID: c.ID, Actor: convertActor, Metadata: meta, CreatedAt: now,
		})
	})
	if err != nil {
		return c, err
	}
	c.Phase = PhaseCommitted
	c.Committed = true
	c.CommittedAt = &now
	return c, nil
}

// rollbackMoved undoes already-published files in reverse order: a backup
// in staging/.rollback is restored over the target, a newly created file is
// removed. Best effort — the conversion converges to failed regardless.
func rollbackMoved(stagingDir, targetRoot string, moved []string) {
	for i := len(moved) - 1; i >= 0; i-- {
		rel := moved[i]
		dst := filepath.Join(targetRoot, filepath.FromSlash(rel))
		backup := filepath.Join(stagingDir, rollbackDir, filepath.FromSlash(rel))
		if _, err := os.Stat(backup); err == nil {
			_ = os.MkdirAll(filepath.Dir(dst), 0o755)
			_ = os.Rename(backup, dst)
		} else {
			_ = os.Remove(dst)
		}
	}
}

// Abandon cancels a conversion from any non-terminal phase; the source is
// unchanged (user cancel semantics).
func (s *ConvertService) Abandon(ctx context.Context, conversionID string) (Conversion, error) {
	var out Conversion
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		c, err := st.GetM5Conversion(conversionID)
		if err != nil {
			return err
		}
		if c.Phase.Terminal() {
			return ErrConvertStateBad
		}
		if err := st.UpdateM5ConversionPhase(c.ID, c.Phase, PhaseAbandoned, s.clock.Now().UTC()); err != nil {
			return err
		}
		c.Phase = PhaseAbandoned
		out = c
		return nil
	})
	return out, err
}

// Reconcile converges an orphaned conversion after a crash. The staging and
// target directories are caller-held (their paths never touch the durable
// row), so reconciliation is deliberately conservative: a preview older
// than ConvertPreviewTTL is abandoned; a copying/publishing orphan
// converges to failed. The source workspace is read-only throughout, so
// failing closed always preserves it (CVT-002).
func (s *ConvertService) Reconcile(ctx context.Context, conversionID string) (Conversion, error) {
	var out Conversion
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		c, err := st.GetM5Conversion(conversionID)
		if err != nil {
			return err
		}
		if c.Phase.Terminal() {
			out = c
			return nil
		}
		now := s.clock.Now().UTC()
		switch c.Phase {
		case PhasePreview:
			if now.After(c.CreatedAt.Add(ConvertPreviewTTL)) {
				if err := st.UpdateM5ConversionPhase(c.ID, PhasePreview, PhaseAbandoned, now); err != nil {
					return err
				}
				c.Phase = PhaseAbandoned
			}
		case PhaseCopying, PhasePublishing:
			if c.Phase == PhasePublishing {
				// undo whatever the crashed publish landed, using the
				// durable journal (best effort; then converge to failed)
				var j publishJournal
				if json.Unmarshal([]byte(c.PublishJournal), &j) == nil && len(j.Moved) > 0 {
					rollbackMoved(j.StagingDir, j.TargetRoot, j.Moved)
				}
			}
			if err := st.UpdateM5ConversionPhase(c.ID, c.Phase, PhaseFailed, now); err != nil {
				return err
			}
			if err := st.UpdateM5ConversionJournal(c.ID, "{}"); err != nil {
				return err
			}
			c.Phase = PhaseFailed
		}
		out = c
		return nil
	})
	return out, err
}
