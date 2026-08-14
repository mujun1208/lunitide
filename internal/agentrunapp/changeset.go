// M4-E change set use cases: changeset.preview/apply/revert. A change set is
// an ordered, digest-bound list of UTF-8 text file mutations inside one
// registered workspace. preview snapshots the original state (base digest);
// apply re-checks the base digest (CAS) before writing and marks the set
// conflicted instead of applying onto drifted state; revert restores the
// original snapshots guarded by the applied digest. Mutations use a durable
// prepare transaction, transaction-free atomic filesystem operations, and a
// finalize transaction which stores the effect receipt. Prepared effects are
// reconciled from the filesystem after crashes and are never blindly retried.
package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

// ErrChangeSetApplyFailed is returned when a file system write fails in the
// middle of apply/revert. Already-written operations are rolled back
// best-effort and the set is marked conflicted.
var ErrChangeSetApplyFailed = errors.New("change set file write failed")

// changeSetFault is a deliberately narrow fault-injection seam. Production
// leaves it nil; package tests install it to simulate crashes at phase edges.
var changeSetFault func(point string) error

func injectChangeSetFault(point string) error {
	if changeSetFault != nil {
		return changeSetFault(point)
	}
	return nil
}

const (
	// changesetMaxOps bounds operations per set.
	changesetMaxOps = 64
	// changesetMaxFileBytes bounds one operation's content and the
	// original snapshot (change sets carry UTF-8 text only).
	changesetMaxFileBytes = 1 << 20
	// changesetMaxTotalBytes bounds summed operation content per set.
	changesetMaxTotalBytes = 4 << 20
)

// ChangeSetPreviewOp is one requested mutation in a changeset.preview call.
// Content is required for create/update (may be empty) and must be absent
// for delete.
type ChangeSetPreviewOp struct {
	Op      string `json:"op"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ChangeSetOpProjection is the lean per-operation view returned to callers.
// Full content never crosses the bridge: the caller already holds the
// desired content and the original stays in the engine for revert.
type ChangeSetOpProjection struct {
	Ordinal        int64  `json:"ordinal"`
	Op             string `json:"op"`
	Path           string `json:"path"`
	ContentDigest  string `json:"contentDigest,omitempty"`
	OriginalDigest string `json:"originalDigest,omitempty"`
	AppliedDigest  string `json:"appliedDigest,omitempty"`
}

// ChangeSetPreviewResult pairs the previewed set with its operation plan.
type ChangeSetPreviewResult struct {
	ChangeSet  agentrun.ChangeSet      `json:"changeSet"`
	Operations []ChangeSetOpProjection `json:"operations"`
}

// ChangeSetApplyResult reports a fully applied change set.
type ChangeSetApplyResult struct {
	ChangeSet  agentrun.ChangeSet `json:"changeSet"`
	AppliedOps int                `json:"appliedOps"`
}

// ChangeSetRevertResult reports a fully reverted change set.
type ChangeSetRevertResult struct {
	ChangeSet   agentrun.ChangeSet `json:"changeSet"`
	RevertedOps int                `json:"revertedOps"`
}

func digestText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// changeSetBaseEntry is one row of the base digest preimage. Struct field
// order is alphabetical so json.Marshal yields canonical JSON.
type changeSetBaseEntry struct {
	Op             string `json:"op"`
	OriginalDigest string `json:"originalDigest"`
	Path           string `json:"path"`
}

// changeSetApprovalPreimage binds the approval to the exact operation set,
// the base state, the workspace registration and the run. Field order is
// alphabetical (canonical JSON).
type changeSetApprovalPreimage struct {
	Action           string                `json:"action"`
	BaseDigest       string                `json:"baseDigest"`
	ConfigDigest     string                `json:"configDigest"`
	DescriptorDigest string                `json:"descriptorDigest"`
	Ops              []changeSetApprovalOp `json:"ops"`
	PolicyDigest     string                `json:"policyDigest"`
	RegistrationID   string                `json:"registrationId"`
	RunID            string                `json:"runId"`
}

const changeSetPolicy = "m4-single-agent-policy-v1"
const changeSetDescriptor = "changeset-1.0.0"

func accessConfigDigest(a fsAccess) string {
	return digestText(a.registrationID + "\x00" + strings.Join(a.patterns, "\x00"))
}

type changeSetApprovalOp struct {
	ContentDigest string `json:"contentDigest"`
	Op            string `json:"op"`
	Path          string `json:"path"`
}

func changeSetApprovalDigest(preimage changeSetApprovalPreimage) (string, error) {
	body, err := json.Marshal(preimage)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func approvalOpsFromStored(ops []agentrun.ChangeSetOperation) []changeSetApprovalOp {
	out := make([]changeSetApprovalOp, len(ops))
	for i, op := range ops {
		out[i] = changeSetApprovalOp{ContentDigest: op.ContentDigest, Op: string(op.Op), Path: op.Path}
	}
	return out
}

// resolveForCreate maps rel to an absolute path whose parent exists inside
// the root; the final element must not exist at all (a dangling symlink
// counts as existing, since writing through it could escape the root).
func (a fsAccess) resolveForCreate(rel string) (string, error) {
	if !validFsRelPath(rel) {
		return "", ErrFsPathInvalid
	}
	dirRel, name := path.Split(rel)
	parent, err := a.resolve(strings.TrimSuffix(dirRel, "/"))
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", ErrFsNotFound
	}
	final := filepath.Join(parent, name)
	if _, err := os.Lstat(final); err == nil {
		return "", ErrFsPathExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return final, nil
}

// readChangeSetText loads a regular UTF-8 file up to changesetMaxFileBytes.
func readChangeSetText(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", ErrFsNotAFile
	}
	if info.Size() > changesetMaxFileBytes {
		return "", ErrFsTooLarge
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", ErrFsBinary
	}
	return string(data), nil
}

// snapshotOriginal captures the pre-apply state of one planned operation:
// create requires the path to be absent; update/delete require an existing
// regular UTF-8 text file whose content becomes the revert source.
func snapshotOriginal(access fsAccess, op string, rel string) (original *string, originalDigest string, err error) {
	if op == string(agentrun.ChangeSetOpCreate) {
		if _, err := access.resolveForCreate(rel); err != nil {
			return nil, "", err
		}
		return nil, "", nil
	}
	abs, err := access.resolve(rel)
	if err != nil {
		return nil, "", err
	}
	content, err := readChangeSetText(abs)
	if err != nil {
		return nil, "", err
	}
	return &content, digestText(content), nil
}

// validatePreviewOps checks the requested plan against the granted scope and
// size caps before any file system access.
func validatePreviewOps(access fsAccess, ops []ChangeSetPreviewOp) error {
	if len(ops) < 1 || len(ops) > changesetMaxOps {
		return fmt.Errorf("%w: change set needs 1..%d operations", agentrun.ErrInvalid, changesetMaxOps)
	}
	seen := make(map[string]struct{}, len(ops))
	total := 0
	for _, op := range ops {
		switch op.Op {
		case string(agentrun.ChangeSetOpCreate), string(agentrun.ChangeSetOpUpdate):
			if len(op.Content) > changesetMaxFileBytes {
				return fmt.Errorf("%w: operation content exceeds %d bytes", agentrun.ErrInvalid, changesetMaxFileBytes)
			}
			total += len(op.Content)
		case string(agentrun.ChangeSetOpDelete):
			if op.Content != "" {
				return fmt.Errorf("%w: delete operations carry no content", agentrun.ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: unknown change set op %q", agentrun.ErrInvalid, op.Op)
		}
		if !validFsRelPath(op.Path) {
			return ErrFsPathInvalid
		}
		if _, dup := seen[op.Path]; dup {
			return fmt.Errorf("%w: duplicate operation path %q", agentrun.ErrInvalid, op.Path)
		}
		seen[op.Path] = struct{}{}
		if !scopeAllows(access.patterns, op.Path) {
			return ErrFsScopeDenied
		}
	}
	if total > changesetMaxTotalBytes {
		return fmt.Errorf("%w: change set content exceeds %d bytes", agentrun.ErrInvalid, changesetMaxTotalBytes)
	}
	return nil
}

// ChangesetPreview creates a change set in one transaction: the plan is
// validated against a write-scoped fenced lease, the original workspace
// state is snapshotted per path, and the set transitions draft→previewed
// with base and approval digests computed over the canonical plan.
func (s *Service) ChangesetPreview(ctx context.Context, key, actor string, request any, runID, leaseID string, fencingToken int64, ops []ChangeSetPreviewOp) (ChangeSetPreviewResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return ChangeSetPreviewResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return ChangeSetPreviewResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return ChangeSetPreviewResult{}, err
	}
	var result ChangeSetPreviewResult
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("changeset.preview", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		run, err := tx.GetRun(runID)
		if err != nil {
			return err
		}
		if run.Status != agentrun.RunRunning {
			return fmt.Errorf("%w: change set preview requires a running run, got %s", agentrun.ErrInvalidTransition, run.Status)
		}
		access, err := authorizeFsLease(tx, leaseID, fencingToken, "write", now)
		if err != nil {
			return err
		}
		if err := validatePreviewOps(access, ops); err != nil {
			return err
		}
		operations := make([]agentrun.ChangeSetOperation, len(ops))
		baseEntries := make([]changeSetBaseEntry, len(ops))
		setID := ulid.Make().String()
		for i, op := range ops {
			original, originalDigest, err := snapshotOriginal(access, op.Op, op.Path)
			if err != nil {
				return err
			}
			operation := agentrun.ChangeSetOperation{
				ID:              ulid.Make().String(),
				ChangeSetID:     setID,
				Ordinal:         int64(i) + 1,
				Op:              agentrun.ChangeSetOp(op.Op),
				Path:            op.Path,
				OriginalContent: original,
				OriginalDigest:  originalDigest,
			}
			if op.Op != string(agentrun.ChangeSetOpDelete) {
				content := op.Content
				operation.Content = &content
				operation.ContentDigest = digestText(content)
			}
			operations[i] = operation
			baseEntries[i] = changeSetBaseEntry{Op: op.Op, OriginalDigest: originalDigest, Path: op.Path}
		}
		baseBody, err := json.Marshal(baseEntries)
		if err != nil {
			return err
		}
		baseSum := sha256.Sum256(baseBody)
		approvalDigest, err := changeSetApprovalDigest(changeSetApprovalPreimage{
			Action: "changeset.apply", ConfigDigest: accessConfigDigest(access), DescriptorDigest: changeSetDescriptor, PolicyDigest: digestText(changeSetPolicy),
			BaseDigest:     hex.EncodeToString(baseSum[:]),
			Ops:            approvalOpsFromStored(operations),
			RegistrationID: access.registrationID,
			RunID:          run.ID,
		})
		if err != nil {
			return err
		}
		set := agentrun.ChangeSet{
			ID:             setID,
			RunID:          run.ID,
			BaseDigest:     hex.EncodeToString(baseSum[:]),
			ApprovalDigest: approvalDigest,
			Status:         agentrun.ChangeSetDraft,
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.PutChangeSet(set); err != nil {
			return err
		}
		set, err = set.Transition(agentrun.ChangeSetPreviewed, now)
		if err != nil {
			return err
		}
		if err := tx.PutChangeSet(set); err != nil {
			return err
		}
		for _, operation := range operations {
			if err := tx.PutChangeSetOperation(operation); err != nil {
				return err
			}
		}
		run, err = tx.TransitionRun(run.ID, run.Version, agentrun.RunPausedReview, now)
		if err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, agentrun.EventReviewRequested, map[string]any{
			"approvalDigest": approvalDigest, "action": "changeset.apply", "resourceDigest": digestText(set.ID), "baseDigest": set.BaseDigest,
			"configDigest": accessConfigDigest(access), "policyDigest": digestText(changeSetPolicy), "descriptorDigest": changeSetDescriptor,
		}, now); err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, agentrun.EventChangeSetPreviewCompleted, map[string]any{
			"schemaVersion":  1,
			"runId":          run.ID,
			"changeSetId":    set.ID,
			"status":         string(set.Status),
			"version":        set.Version,
			"baseDigest":     set.BaseDigest,
			"approvalDigest": set.ApprovalDigest,
			"opCount":        len(operations),
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"runId": run.ID, "baseDigest": set.BaseDigest, "opCount": len(operations)})
		if err := s.putAudit(tx, "changeset.previewed", set.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = ChangeSetPreviewResult{ChangeSet: set, Operations: projectChangeSetOps(operations)}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "changeset.preview", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

func projectChangeSetOps(ops []agentrun.ChangeSetOperation) []ChangeSetOpProjection {
	out := make([]ChangeSetOpProjection, len(ops))
	for i, op := range ops {
		out[i] = ChangeSetOpProjection{
			Ordinal:        op.Ordinal,
			Op:             string(op.Op),
			Path:           op.Path,
			ContentDigest:  op.ContentDigest,
			OriginalDigest: op.OriginalDigest,
			AppliedDigest:  op.AppliedDigest,
		}
	}
	return out
}

// changeSetMutation carries the shared preconditions of apply and revert:
// version CAS, running run, approval digest binding and lease authorization.
type changeSetMutation struct {
	set    agentrun.ChangeSet
	run    agentrun.AgentRun
	ops    []agentrun.ChangeSetOperation
	access fsAccess
}

// loadChangeSetMutation loads the set, its ordered operations and its run,
// and enforces the preconditions shared by apply and revert. The presented
// approval digest must match the stored one, and the digest is recomputed
// with the presented lease's registration so an approval captured against
// one workspace can never be replayed against another.
func (s *Service) loadChangeSetMutation(tx Tx, changeSetID string, expectedVersion int64, approvalDigest, leaseID string, fencingToken int64, now time.Time) (changeSetMutation, error) {
	set, err := tx.GetChangeSet(changeSetID)
	if err != nil {
		return changeSetMutation{}, err
	}
	if set.Version != expectedVersion {
		return changeSetMutation{}, agentrun.ErrVersionConflict
	}
	if approvalDigest != set.ApprovalDigest {
		return changeSetMutation{}, agentrun.ErrReviewDigestMismatch
	}
	run, err := tx.GetRun(set.RunID)
	if err != nil {
		return changeSetMutation{}, err
	}
	if run.Status != agentrun.RunRunning {
		return changeSetMutation{}, fmt.Errorf("%w: change set mutation requires a running run, got %s", agentrun.ErrInvalidTransition, run.Status)
	}
	access, err := authorizeFsLease(tx, leaseID, fencingToken, "write", now)
	if err != nil {
		return changeSetMutation{}, err
	}
	ops, err := tx.ListChangeSetOperations(set.ID)
	if err != nil {
		return changeSetMutation{}, err
	}
	if len(ops) == 0 {
		return changeSetMutation{}, fmt.Errorf("%w: change set has no operations", agentrun.ErrInvalid)
	}
	recomputed, err := changeSetApprovalDigest(changeSetApprovalPreimage{
		Action: "changeset.apply", ConfigDigest: accessConfigDigest(access), DescriptorDigest: changeSetDescriptor, PolicyDigest: digestText(changeSetPolicy),
		BaseDigest:     set.BaseDigest,
		Ops:            approvalOpsFromStored(ops),
		RegistrationID: access.registrationID,
		RunID:          run.ID,
	})
	if err != nil {
		return changeSetMutation{}, err
	}
	if recomputed != set.ApprovalDigest {
		return changeSetMutation{}, agentrun.ErrReviewDigestMismatch
	}
	return changeSetMutation{set: set, run: run, ops: ops, access: access}, nil
}

// writeOp performs one operation's apply-phase file system effect at the
// resolved path: create is exclusive, update overwrites, delete removes.
func writeOp(abs string, op agentrun.ChangeSetOperation) error {
	switch op.Op {
	case agentrun.ChangeSetOpCreate:
		return atomicCreate(abs, []byte(*op.Content))
	case agentrun.ChangeSetOpUpdate:
		return atomicReplace(abs, []byte(*op.Content))
	case agentrun.ChangeSetOpDelete:
		return atomicDelete(abs)
	}
	return fmt.Errorf("%w: unknown change set op %q", agentrun.ErrInvalid, op.Op)
}

// undoWrite compensates one already-written apply operation. Delete is
// recreated from its preview snapshot; errors must never be hidden by callers.
func undoWrite(abs string, op agentrun.ChangeSetOperation) error {
	switch op.Op {
	case agentrun.ChangeSetOpCreate:
		return atomicDelete(abs)
	case agentrun.ChangeSetOpUpdate, agentrun.ChangeSetOpDelete:
		if op.OriginalContent != nil {
			if op.Op == agentrun.ChangeSetOpDelete {
				return atomicCreate(abs, []byte(*op.OriginalContent))
			}
			return atomicReplace(abs, []byte(*op.OriginalContent))
		}
	}
	return fmt.Errorf("compensation snapshot missing for %s", op.Path)
}

// undoRevert compensates one already-reverted operation best-effort: the
// file returns to its applied state.
func undoRevert(abs string, op agentrun.ChangeSetOperation) error {
	switch op.Op {
	case agentrun.ChangeSetOpCreate:
		if op.Content != nil {
			return atomicCreate(abs, []byte(*op.Content))
		}
	case agentrun.ChangeSetOpUpdate:
		if op.Content != nil {
			return atomicReplace(abs, []byte(*op.Content))
		}
	case agentrun.ChangeSetOpDelete:
		return atomicDelete(abs)
	}
	return fmt.Errorf("revert compensation content missing for %s", op.Path)
}

// compensateChangeSet rolls back the completed prefix (apply) or suffix
// (revert) of a multi-path filesystem mutation. Each compensation has the
// same per-path guard and CAS discipline as the forward operation. Callers
// must treat any returned error as an unknown effect outcome: another actor
// may have changed a path, or a compensation write may have completed only
// partially from the process's point of view.
func compensateChangeSet(mutation changeSetMutation, completed []int, phase string) error {
	var compensationErr error
	for i := len(completed) - 1; i >= 0; i-- {
		op := mutation.ops[completed[i]]
		guard, err := guardChangeSetPath(mutation.access, op.Path)
		if err != nil {
			compensationErr = errors.Join(compensationErr, fmt.Errorf("%s compensation guard failed for %s: %w", phase, op.Path, err))
			continue
		}

		var abs, conflict string
		if phase == "apply" {
			abs, conflict = casCheckPlannedApplied(mutation.access, op)
		} else {
			abs, conflict = casCheckOriginal(mutation.access, op)
		}
		if conflict != "" {
			guard.Close()
			compensationErr = errors.Join(compensationErr, fmt.Errorf("%s compensation CAS failed for %s", phase, op.Path))
			continue
		}
		if phase == "apply" {
			err = undoWrite(abs, op)
		} else {
			err = undoRevert(abs, op)
		}
		guard.Close()
		if err != nil {
			compensationErr = errors.Join(compensationErr, fmt.Errorf("%s compensation write failed for %s: %w", phase, op.Path, err))
		}
	}
	return compensationErr
}

// ChangesetApply approves and applies a previewed change set. Every path is
// CAS-checked against its original digest before any write; on drift the set
// transitions approved→conflicted (the approval is recorded, the apply is
// refused) and ChangeSetConflicted is emitted. On full success the set
// transitions previewed→approved→applied with per-path applied digests.
func (s *Service) ChangesetApply(ctx context.Context, key, actor string, request any, changeSetID string, expectedVersion int64, approvalDigest, leaseID string, fencingToken int64) (ChangeSetApplyResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return ChangeSetApplyResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return ChangeSetApplyResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return ChangeSetApplyResult{}, err
	}
	var result ChangeSetApplyResult
	var mutation changeSetMutation
	var replayed bool
	var recovering bool
	effectKey := "changeset.apply/" + changeSetID
	// Prepare transaction: all guards and approval consumption precede the effect.
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("changeset.apply", key, now)
		if err != nil {
			return err
		}
		if found {
			replayed = true
			return replay(record, digest, &result)
		}
		if existing, getErr := tx.GetEffectByKey(effectKey); getErr == nil {
			// A fresh request against an already resolved effect is a lifecycle
			// error, not an idempotency collision. Check the durable set state
			// before comparing the prepared effect's request digest; only an
			// unresolved approved set is eligible for crash recovery.
			set, setErr := tx.GetChangeSet(changeSetID)
			if setErr != nil {
				return setErr
			}
			if set.Status.Terminal() {
				return agentrun.ErrTerminal
			}
			if set.Status != agentrun.ChangeSetApproved {
				return fmt.Errorf("%w: change set %s -> applied", agentrun.ErrInvalidTransition, set.Status)
			}
			if existing.RequestDigest != digest || existing.Status != agentrun.EffectPrepared {
				return ErrIdempotencyConflict
			}
			mutation, err = s.loadPreparedChangeSetMutation(tx, changeSetID, leaseID, fencingToken, now)
			recovering = err == nil
			return err
		} else if !errors.Is(getErr, agentrun.ErrNotFound) {
			return getErr
		}
		mutation, err = s.loadChangeSetMutation(tx, changeSetID, expectedVersion, approvalDigest, leaseID, fencingToken, now)
		if err != nil {
			return err
		}
		if recovering {
			return nil
		}
		if _, err = tx.ConsumeReview(mutation.run.ID, approvalDigest, "changeset.apply", now); err != nil {
			return err
		}
		mutation.set, err = mutation.set.Transition(agentrun.ChangeSetApproved, now)
		if err != nil {
			return err
		}
		if err = tx.PutChangeSet(mutation.set); err != nil {
			return err
		}
		return tx.PutEffect(agentrun.EffectJournal{ID: ulid.Make().String(), RunID: mutation.run.ID, EffectKey: effectKey, RequestDigest: digest, Status: agentrun.EffectPrepared, CreatedAt: now, UpdatedAt: now})
	})
	if err != nil || replayed {
		return result, err
	}
	if err = injectChangeSetFault("apply.after_prepare"); err != nil {
		return ChangeSetApplyResult{}, err
	}
	// Atomic filesystem effect outside the database transaction. On retry of a
	// prepared effect, receipt reconciliation proves whether to finalize,
	// execute an untouched plan, or fail a mixed outcome closed.
	skipEffect := false
	if recovering {
		allApplied, allOriginal := reconcilePreparedApply(mutation)
		switch {
		case allApplied:
			skipEffect = true
		case !allOriginal:
			return ChangeSetApplyResult{}, s.failPreparedChangeSet(ctx, mutation, effectKey, mutation.ops[0].Path, "reconcile", digest, actor, agentrun.ErrChangeSetBaseConflict)
		}
	}
	if !skipEffect {
		completed := make([]int, 0, len(mutation.ops))
		for i, op := range mutation.ops {
			guard, guardErr := guardChangeSetPath(mutation.access, op.Path)
			if guardErr != nil {
				return ChangeSetApplyResult{}, s.failPreparedChangeSetMutation(ctx, mutation, effectKey, op.Path, "apply", digest, actor, guardErr, completed)
			}
			// Re-resolve and repeat CAS immediately before every write.
			abs, conflict := casCheckOriginal(mutation.access, op)
			if conflict != "" {
				guard.Close()
				return ChangeSetApplyResult{}, s.failPreparedChangeSetMutation(ctx, mutation, effectKey, conflict, "apply", digest, actor, agentrun.ErrChangeSetBaseConflict, completed)
			}
			err = writeOp(abs, op)
			guard.Close()
			if err != nil {
				return ChangeSetApplyResult{}, s.failPreparedChangeSetMutation(ctx, mutation, effectKey, op.Path, "apply", digest, actor, fmt.Errorf("%w: %v", ErrChangeSetApplyFailed, err), completed)
			}
			completed = append(completed, i)
		}
	}
	if err = injectChangeSetFault("apply.after_effect"); err != nil {
		return ChangeSetApplyResult{}, err
	}
	receipt := digestText(effectKey + "\x00" + digest + "\x00committed")
	// Finalize transaction: receipt and logical completion are one commit.
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		set, err := tx.GetChangeSet(changeSetID)
		if err != nil {
			return err
		}
		if set.Status != agentrun.ChangeSetApproved {
			return agentrun.ErrVersionConflict
		}
		set, err = set.Transition(agentrun.ChangeSetApplied, now)
		if err != nil {
			return err
		}
		if err = tx.PutChangeSet(set); err != nil {
			return err
		}
		for i, op := range mutation.ops {
			if op.Op != agentrun.ChangeSetOpDelete {
				op.AppliedDigest = digestText(*op.Content)
			}
			if err = tx.PutChangeSetOperation(op); err != nil {
				return err
			}
			mutation.ops[i] = op
		}
		effect, err := tx.GetEffectByKey(effectKey)
		if err != nil {
			return err
		}
		effect, err = effect.Resolve(agentrun.EffectCommitted, receipt, now)
		if err != nil {
			return err
		}
		if err = tx.PutEffect(effect); err != nil {
			return err
		}
		if err = appendRunEvent(tx, mutation.run.ID, agentrun.EventChangeSetApplyCompleted, map[string]any{"schemaVersion": 1, "runId": mutation.run.ID, "changeSetId": set.ID, "status": string(set.Status), "version": set.Version, "opsApplied": len(mutation.ops), "receiptId": receipt}, now); err != nil {
			return err
		}
		// Revert is a distinct high-risk action and must consume its own
		// durable approval. Pause only after apply has committed logically,
		// then publish the exact action binding review.decide will persist.
		run, err := tx.GetRun(mutation.run.ID)
		if err != nil {
			return err
		}
		run, err = tx.TransitionRun(run.ID, run.Version, agentrun.RunPausedReview, now)
		if err != nil {
			return err
		}
		if err = appendRunEvent(tx, run.ID, agentrun.EventReviewRequested, map[string]any{
			"approvalDigest": set.ApprovalDigest, "action": "changeset.revert", "resourceDigest": digestText(set.ID), "baseDigest": set.BaseDigest,
			"configDigest": accessConfigDigest(mutation.access), "policyDigest": digestText(changeSetPolicy), "descriptorDigest": changeSetDescriptor,
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"runId": mutation.run.ID, "opsApplied": len(mutation.ops), "version": set.Version, "receiptId": receipt})
		if err = s.putAudit(tx, "changeset.applied", set.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = ChangeSetApplyResult{ChangeSet: set, AppliedOps: len(mutation.ops)}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "changeset.apply", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

func (s *Service) failPreparedChangeSetMutation(ctx context.Context, mutation changeSetMutation, effectKey, conflictPath, phase, digest, actor string, cause error, completed []int) error {
	effectStatus := agentrun.EffectFailed
	if compensationErr := compensateChangeSet(mutation, completed, phase); compensationErr != nil {
		cause = fmt.Errorf("%w; compensation outcome unknown: %v", cause, compensationErr)
		effectStatus = agentrun.EffectOutcomeUnknown
	}
	return s.failPreparedChangeSet(ctx, mutation, effectKey, conflictPath, phase, digest, actor, cause, effectStatus)
}

func (s *Service) failPreparedChangeSet(ctx context.Context, mutation changeSetMutation, effectKey, conflictPath, phase, digest, actor string, cause error, effectStatus ...agentrun.EffectStatus) error {
	status := agentrun.EffectFailed
	if len(effectStatus) > 0 {
		status = effectStatus[0]
	}
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		var outcome error
		if err := s.conflictChangeSet(tx, mutation.run.ID, mutation.set, conflictPath, phase, digest, actor, now, &outcome, cause); err != nil {
			return err
		}
		effect, err := tx.GetEffectByKey(effectKey)
		if err != nil {
			return err
		}
		effect, err = effect.Resolve(status, "", now)
		if err != nil {
			return err
		}
		return tx.PutEffect(effect)
	})
	if err != nil {
		return err
	}
	return cause
}

func (s *Service) loadPreparedChangeSetMutation(tx Tx, changeSetID, leaseID string, fencingToken int64, now time.Time) (changeSetMutation, error) {
	return s.loadPreparedChangeSetMutationForStatus(tx, changeSetID, agentrun.ChangeSetApproved, leaseID, fencingToken, now)
}

func (s *Service) loadPreparedChangeSetMutationForStatus(tx Tx, changeSetID string, status agentrun.ChangeSetStatus, leaseID string, fencingToken int64, now time.Time) (changeSetMutation, error) {
	set, err := tx.GetChangeSet(changeSetID)
	if err != nil {
		return changeSetMutation{}, err
	}
	if set.Status != status {
		return changeSetMutation{}, agentrun.ErrInvalidTransition
	}
	run, err := tx.GetRun(set.RunID)
	if err != nil {
		return changeSetMutation{}, err
	}
	access, err := authorizeFsLease(tx, leaseID, fencingToken, "write", now)
	if err != nil {
		return changeSetMutation{}, err
	}
	ops, err := tx.ListChangeSetOperations(set.ID)
	if err != nil {
		return changeSetMutation{}, err
	}
	return changeSetMutation{set: set, run: run, ops: ops, access: access}, nil
}

// reconcilePreparedApply determines whether an interrupted filesystem phase
// completed fully. A fully applied tree is finalized by the normal retry;
// an untouched tree is safe to execute; any mixed tree is explicit conflict.
func reconcilePreparedApply(m changeSetMutation) (allApplied, allOriginal bool) {
	allApplied, allOriginal = true, true
	for _, op := range m.ops {
		_, originalConflict := casCheckOriginal(m.access, op)
		_, appliedConflict := casCheckPlannedApplied(m.access, op)
		allOriginal = allOriginal && originalConflict == ""
		allApplied = allApplied && appliedConflict == ""
	}
	return
}

func casCheckPlannedApplied(access fsAccess, op agentrun.ChangeSetOperation) (string, string) {
	switch op.Op {
	case agentrun.ChangeSetOpCreate, agentrun.ChangeSetOpUpdate:
		abs, err := access.resolve(op.Path)
		if err != nil {
			return "", op.Path
		}
		content, err := readChangeSetText(abs)
		if err != nil || op.Content == nil || digestText(content) != op.ContentDigest {
			return "", op.Path
		}
		return abs, ""
	case agentrun.ChangeSetOpDelete:
		abs, err := access.resolveForCreate(op.Path)
		if err != nil {
			return "", op.Path
		}
		return abs, ""
	}
	return "", op.Path
}

// casCheckOriginal verifies one path still matches its preview snapshot and
// returns the absolute write target. A non-empty conflict path means drift.
func casCheckOriginal(access fsAccess, op agentrun.ChangeSetOperation) (string, string) {
	if op.Op == agentrun.ChangeSetOpCreate {
		abs, err := access.resolveForCreate(op.Path)
		if err != nil {
			return "", op.Path
		}
		return abs, ""
	}
	abs, err := access.resolve(op.Path)
	if err != nil {
		return "", op.Path
	}
	content, err := readChangeSetText(abs)
	if err != nil || digestText(content) != op.OriginalDigest {
		return "", op.Path
	}
	return abs, ""
}

// conflictChangeSet persists the approved→conflicted transition with its
// event and audit record, commits, and reports the failure via outcome so
// the surrounding transaction is not rolled back.
func (s *Service) conflictChangeSet(tx Tx, runID string, set agentrun.ChangeSet, conflictPath, phase, digest, actor string, now time.Time, outcome *error, cause ...error) error {
	conflicted, err := set.Transition(agentrun.ChangeSetConflicted, now)
	if err != nil {
		return err
	}
	if err := tx.PutChangeSet(conflicted); err != nil {
		return err
	}
	if err := appendRunEvent(tx, runID, agentrun.EventChangeSetConflicted, map[string]any{
		"schemaVersion": 1,
		"runId":         runID,
		"changeSetId":   conflicted.ID,
		"status":        string(conflicted.Status),
		"version":       conflicted.Version,
		"conflictPath":  conflictPath,
		"phase":         phase,
	}, now); err != nil {
		return err
	}
	meta, _ := json.Marshal(map[string]any{"runId": runID, "conflictPath": conflictPath, "phase": phase, "version": conflicted.Version})
	if err := s.putAudit(tx, "changeset.conflicted", conflicted.ID, actor, digest, meta, now); err != nil {
		return err
	}
	if len(cause) > 0 {
		*outcome = cause[0]
	} else {
		*outcome = agentrun.ErrChangeSetBaseConflict
	}
	return nil
}

// ChangesetRevert restores an applied change set from its original
// snapshots, guarded per path by the applied digest. Drift after apply
// (someone touched the files) conflicts the set instead of reverting.
func (s *Service) ChangesetRevert(ctx context.Context, key, actor string, request any, changeSetID string, expectedVersion int64, approvalDigest, leaseID string, fencingToken int64) (ChangeSetRevertResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return ChangeSetRevertResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return ChangeSetRevertResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return ChangeSetRevertResult{}, err
	}
	var result ChangeSetRevertResult
	var mutation changeSetMutation
	var replayed bool
	var recovering bool
	effectKey := "changeset.revert/" + changeSetID
	// Prepare transaction.
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("changeset.revert", key, now)
		if err != nil {
			return err
		}
		if found {
			replayed = true
			return replay(record, digest, &result)
		}
		if existing, getErr := tx.GetEffectByKey(effectKey); getErr == nil {
			set, setErr := tx.GetChangeSet(changeSetID)
			if setErr != nil {
				return setErr
			}
			if set.Status != agentrun.ChangeSetApplied {
				return fmt.Errorf("%w: change set %s -> reverted", agentrun.ErrInvalidTransition, set.Status)
			}
			if existing.RequestDigest != digest || existing.Status != agentrun.EffectPrepared {
				return ErrIdempotencyConflict
			}
			mutation, err = s.loadPreparedChangeSetMutationForStatus(tx, changeSetID, agentrun.ChangeSetApplied, leaseID, fencingToken, now)
			recovering = err == nil
			return err
		} else if !errors.Is(getErr, agentrun.ErrNotFound) {
			return getErr
		}
		mutation, err = s.loadChangeSetMutation(tx, changeSetID, expectedVersion, approvalDigest, leaseID, fencingToken, now)
		if err != nil {
			return err
		}
		if mutation.set.Status != agentrun.ChangeSetApplied {
			return fmt.Errorf("%w: change set %s -> reverted", agentrun.ErrInvalidTransition, mutation.set.Status)
		}
		if _, err = tx.ConsumeReview(mutation.run.ID, approvalDigest, "changeset.revert", now); err != nil {
			return err
		}
		return tx.PutEffect(agentrun.EffectJournal{ID: ulid.Make().String(), RunID: mutation.run.ID, EffectKey: effectKey, RequestDigest: digest, Status: agentrun.EffectPrepared, CreatedAt: now, UpdatedAt: now})
	})
	if err != nil || replayed {
		return result, err
	}
	if err = injectChangeSetFault("revert.after_prepare"); err != nil {
		return result, err
	}
	// Atomic filesystem effect outside the transaction. A retry of the same
	// prepared request first proves whether revert completed, never repeats a
	// possibly completed filesystem mutation blindly.
	skipEffect := false
	if recovering {
		allReverted, allApplied := reconcilePreparedRevert(mutation)
		switch {
		case allReverted:
			skipEffect = true
		case !allApplied:
			return result, s.failPreparedChangeSet(ctx, mutation, effectKey, mutation.ops[0].Path, "reconcile", digest, actor, agentrun.ErrChangeSetBaseConflict)
		}
	}
	if !skipEffect {
		completed := make([]int, 0, len(mutation.ops))
		for i := len(mutation.ops) - 1; i >= 0; i-- {
			op := mutation.ops[i]
			guard, guardErr := guardChangeSetPath(mutation.access, op.Path)
			if guardErr != nil {
				return result, s.failPreparedChangeSetMutation(ctx, mutation, effectKey, op.Path, "revert", digest, actor, guardErr, completed)
			}
			// Re-resolve and repeat CAS immediately before every write.
			abs, conflict := casCheckApplied(mutation.access, op)
			if conflict != "" {
				guard.Close()
				return result, s.failPreparedChangeSetMutation(ctx, mutation, effectKey, conflict, "revert", digest, actor, agentrun.ErrChangeSetBaseConflict, completed)
			}
			err = revertOp(abs, op)
			guard.Close()
			if err != nil {
				return result, s.failPreparedChangeSetMutation(ctx, mutation, effectKey, op.Path, "revert", digest, actor, fmt.Errorf("%w: %v", ErrChangeSetApplyFailed, err), completed)
			}
			completed = append(completed, i)
		}
	}
	if err = injectChangeSetFault("revert.after_effect"); err != nil {
		return result, err
	}
	receipt := digestText(effectKey + "\x00" + digest + "\x00committed")
	// Finalize transaction.
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		set, err := tx.GetChangeSet(changeSetID)
		if err != nil {
			return err
		}
		if set.Status != agentrun.ChangeSetApplied {
			return agentrun.ErrVersionConflict
		}
		set, err = set.Transition(agentrun.ChangeSetReverted, now)
		if err != nil {
			return err
		}
		if err = tx.PutChangeSet(set); err != nil {
			return err
		}
		effect, err := tx.GetEffectByKey(effectKey)
		if err != nil {
			return err
		}
		effect, err = effect.Resolve(agentrun.EffectCommitted, receipt, now)
		if err != nil {
			return err
		}
		if err = tx.PutEffect(effect); err != nil {
			return err
		}
		if err = appendRunEvent(tx, mutation.run.ID, agentrun.EventChangeSetRevertCompleted, map[string]any{"schemaVersion": 1, "runId": mutation.run.ID, "changeSetId": set.ID, "status": string(set.Status), "version": set.Version, "opsReverted": len(mutation.ops), "receiptId": receipt}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"runId": mutation.run.ID, "opsReverted": len(mutation.ops), "version": set.Version, "receiptId": receipt})
		if err = s.putAudit(tx, "changeset.reverted", set.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = ChangeSetRevertResult{ChangeSet: set, RevertedOps: len(mutation.ops)}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "changeset.revert", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

// reconcilePreparedRevert classifies the tree after a crash as wholly
// reverted, wholly still-applied, or mixed/unknown.
func reconcilePreparedRevert(m changeSetMutation) (allReverted, allApplied bool) {
	allReverted, allApplied = true, true
	for _, op := range m.ops {
		_, revertedConflict := casCheckOriginal(m.access, op)
		_, appliedConflict := casCheckApplied(m.access, op)
		allReverted = allReverted && revertedConflict == ""
		allApplied = allApplied && appliedConflict == ""
	}
	return
}

// casCheckApplied verifies one path still matches its post-apply state and
// returns the absolute revert target.
func casCheckApplied(access fsAccess, op agentrun.ChangeSetOperation) (string, string) {
	switch op.Op {
	case agentrun.ChangeSetOpCreate, agentrun.ChangeSetOpUpdate:
		abs, err := access.resolve(op.Path)
		if err != nil {
			return "", op.Path
		}
		content, err := readChangeSetText(abs)
		if err != nil || digestText(content) != op.AppliedDigest {
			return "", op.Path
		}
		return abs, ""
	case agentrun.ChangeSetOpDelete:
		abs, err := access.resolveForCreate(op.Path)
		if err != nil {
			return "", op.Path
		}
		return abs, ""
	}
	return "", op.Path
}

// revertOp restores one operation's pre-apply state at the resolved path.
func revertOp(abs string, op agentrun.ChangeSetOperation) error {
	switch op.Op {
	case agentrun.ChangeSetOpCreate:
		return atomicDelete(abs)
	case agentrun.ChangeSetOpUpdate, agentrun.ChangeSetOpDelete:
		return atomicReplace(abs, []byte(*op.OriginalContent))
	}
	return fmt.Errorf("%w: unknown change set op %q", agentrun.ErrInvalid, op.Op)
}
