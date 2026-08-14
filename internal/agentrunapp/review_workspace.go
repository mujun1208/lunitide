package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

// M4-C review + workspace use cases. Same transactional discipline as the
// M4-B run surface: idempotency record, domain state change, run event and
// audit record commit together or not at all.

// ReviewDecideResult pairs the post-decision run snapshot with the recorded
// review. On rejection the run stays paused_review (the state machine only
// allows paused_review→running), so the caller can re-review a new digest.
type ReviewDecideResult struct {
	Run    agentrun.AgentRun  `json:"run"`
	Review agentrun.RunReview `json:"review"`
}

// pendingApprovalDigest extracts the digest the run is currently paused on
// from the latest AgentRunReviewRequested event. A paused_review run without
// such an event is inconsistent and never approvable.
type pendingReview struct{ ApprovalDigest, Action, ResourceDigest, BaseDigest, ConfigDigest, PolicyDigest, DescriptorDigest string }

func pendingApprovalDigest(events []agentrun.RunEvent) (pendingReview, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType != agentrun.EventReviewRequested {
			continue
		}
		var payload pendingReview
		if json.Unmarshal(events[i].Payload, &payload) == nil && payload.ApprovalDigest != "" {
			return payload, true
		}
		return pendingReview{}, false
	}
	return pendingReview{}, false
}

// ReviewDecide records an approval/rejection bound to the exact pending
// approval digest. Approving transitions the run paused_review→running with
// a version CAS; a digest mismatch fails closed with no side effects.
func (s *Service) ReviewDecide(ctx context.Context, key, actor string, request any, runID string, expectedVersion int64, approvalDigest string, decision agentrun.ReviewDecision) (ReviewDecideResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return ReviewDecideResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return ReviewDecideResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return ReviewDecideResult{}, err
	}
	var result ReviewDecideResult
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("review.decide", key, now)
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
		if run.Version != expectedVersion {
			return agentrun.ErrVersionConflict
		}
		if run.Status != agentrun.RunPausedReview {
			return fmt.Errorf("%w: review requires paused_review, got %s", agentrun.ErrInvalidTransition, run.Status)
		}
		events, err := tx.ListEvents(run.ID)
		if err != nil {
			return err
		}
		pending, ok := pendingApprovalDigest(events)
		if !ok || pending.ApprovalDigest != approvalDigest {
			return agentrun.ErrReviewDigestMismatch
		}
		review := agentrun.RunReview{
			ID:             ulid.Make().String(),
			RunID:          run.ID,
			ApprovalDigest: approvalDigest,
			Decision:       decision,
			DecidedBy:      actor,
			DecidedAt:      now,
			CreatedAt:      now,
			Action:         pending.Action, ResourceDigest: pending.ResourceDigest, BaseDigest: pending.BaseDigest,
			ConfigDigest: pending.ConfigDigest, PolicyDigest: pending.PolicyDigest, DescriptorDigest: pending.DescriptorDigest,
		}
		if err := tx.AppendReview(review); err != nil {
			return err
		}
		if decision == agentrun.ReviewApproved {
			run, err = tx.TransitionRun(run.ID, expectedVersion, agentrun.RunRunning, now)
			if err != nil {
				return err
			}
		}
		if err := appendRunEvent(tx, run.ID, agentrun.EventReviewDecideCompleted, map[string]any{
			"schemaVersion": 1,
			"runId":         run.ID,
			"reviewId":      review.ID,
			"decision":      string(decision),
			"status":        string(run.Status),
			"version":       run.Version,
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"decision": string(decision), "reviewId": review.ID, "version": run.Version})
		if err := s.putAudit(tx, "review.decided", run.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = ReviewDecideResult{Run: run, Review: review}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "review.decide", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

// WorkspaceRegister binds a canonical root path to a durable identity. The
// handler canonicalizes the path before calling; registration is idempotent
// by canonical_root identity (UNIQUE), so re-registering the same root
// returns the existing registration.
func (s *Service) WorkspaceRegister(ctx context.Context, key, actor string, request any, canonicalRoot string) (agentrun.WorkspaceRegistration, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.WorkspaceRegistration{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return agentrun.WorkspaceRegistration{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.WorkspaceRegistration{}, err
	}
	rootSum := sha256.Sum256([]byte(canonicalRoot))
	rootDigest := hex.EncodeToString(rootSum[:])
	var result agentrun.WorkspaceRegistration
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("workspace.register", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		registration, err := tx.GetRegistrationByRoot(canonicalRoot)
		if err != nil && !errors.Is(err, agentrun.ErrNotFound) {
			return err
		}
		if errors.Is(err, agentrun.ErrNotFound) {
			registration = agentrun.WorkspaceRegistration{
				ID:            ulid.Make().String(),
				CanonicalRoot: canonicalRoot,
				RootDigest:    rootDigest,
				Status:        agentrun.RegistrationActive,
				Version:       1,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if err := tx.PutRegistration(registration); err != nil {
				return err
			}
			meta, _ := json.Marshal(map[string]any{"canonicalRoot": registration.CanonicalRoot})
			if err := s.putAudit(tx, "workspace.registered", registration.ID, actor, digest, meta, now); err != nil {
				return err
			}
		}
		result = registration
		response, err := json.Marshal(registration)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "workspace.register", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

// WorkspaceGrant issues a scoped, time-boxed grant on an active registration.
// Scope must already be canonical JSON (sorted keys, no whitespace).
func (s *Service) WorkspaceGrant(ctx context.Context, key, actor string, request any, registrationID string, scope []byte, ttlSeconds int64) (agentrun.WorkspaceGrant, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.WorkspaceGrant{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return agentrun.WorkspaceGrant{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.WorkspaceGrant{}, err
	}
	var result agentrun.WorkspaceGrant
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("workspace.grant", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		registration, err := tx.GetRegistration(registrationID)
		if err != nil {
			return err
		}
		if registration.Status != agentrun.RegistrationActive {
			return agentrun.ErrWorkspaceInactive
		}
		grant := agentrun.WorkspaceGrant{
			ID:             ulid.Make().String(),
			RegistrationID: registration.ID,
			Scope:          scope,
			ExpiresAt:      now.Add(time.Duration(ttlSeconds) * time.Second),
			Status:         agentrun.GrantActive,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.PutGrant(grant); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"registrationId": registration.ID, "expiresAt": grant.ExpiresAt})
		if err := s.putAudit(tx, "workspace.granted", grant.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = grant
		response, err := json.Marshal(grant)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "workspace.grant", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

// AcquireLease issues a fenced lease on a usable grant. The fencing token is
// monotonically increasing per grant and the lease never outlives its grant.
func (s *Service) AcquireLease(ctx context.Context, key, actor string, request any, grantID string, ttlSeconds int64) (agentrun.WorkspaceLease, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.WorkspaceLease{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return agentrun.WorkspaceLease{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.WorkspaceLease{}, err
	}
	var result agentrun.WorkspaceLease
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("workspace.lease", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		grant, err := tx.GetGrant(grantID)
		if err != nil {
			return err
		}
		if !grant.UsableAt(now) {
			return agentrun.ErrWorkspaceInactive
		}
		token, err := tx.NextFencingToken(grant.ID)
		if err != nil {
			return err
		}
		expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
		if expiresAt.After(grant.ExpiresAt) {
			expiresAt = grant.ExpiresAt
		}
		lease := agentrun.WorkspaceLease{
			ID:           ulid.Make().String(),
			GrantID:      grant.ID,
			FencingToken: token,
			ExpiresAt:    expiresAt,
			Status:       agentrun.LeaseActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := tx.PutLease(lease); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"grantId": grant.ID, "fencingToken": lease.FencingToken, "expiresAt": lease.ExpiresAt})
		if err := s.putAudit(tx, "workspace.leased", lease.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = lease
		response, err := json.Marshal(lease)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "workspace.lease", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
