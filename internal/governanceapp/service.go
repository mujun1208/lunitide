// Package governanceapp enforces policy-based review gatekeeping for high-risk operations.
package governanceapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/governance"
	"github.com/lunitide/lunitide/internal/domain/planning"
)

var (
	ErrReviewNotFound     = errors.New("review not found")
	ErrReviewNotPending   = errors.New("review is not pending")
	ErrPolicyNotFound     = errors.New("policy not found")
	ErrReviewExpired      = errors.New("review has expired")
	ErrReviewNotApproved  = errors.New("review is not approved")
	ErrInvalidRiskLevel   = errors.New("invalid risk level")
	ErrInvalidActionType  = errors.New("invalid action type")
)

// ReviewReader reads reviews and policies from storage.
type ReviewReader interface {
	GetReview(ctx context.Context, id string) (*governance.Review, error)
	ListReviewsByPlan(ctx context.Context, planID string, limit int) ([]governance.Review, error)
	GetPolicy(ctx context.Context, id string) (*governance.Policy, error)
	ListPolicies(ctx context.Context, activeOnly bool, limit int) ([]governance.Policy, error)
}

// ReviewWriter writes review status updates.
type ReviewWriter interface {
	UpdateReviewStatus(ctx context.Context, id string, status governance.ReviewStatus, reviewerNote string, reviewedAt *time.Time) error
}

// Clock provides the current time.
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Service enforces governance policy and manages the review lifecycle.
type Service struct {
	read  ReviewReader
	write ReviewWriter
	clock Clock
}

// New creates a governance service with the given dependencies.
func New(r ReviewReader, w ReviewWriter) *Service {
	return &Service{read: r, write: w, clock: systemClock{}}
}

// RequiresReview returns true if the node's risk level requires human review.
func (s *Service) RequiresReview(_ context.Context, node planning.Node) (bool, error) {
	return node.RiskLevel.RequiresReview(), nil
}

// IsApproved checks whether there is an active, non-expired review approval for the given node.
// It looks for reviews associated with the node's plan and checks if any are approved.
func (s *Service) IsApproved(ctx context.Context, node planning.Node) (bool, error) {
	if s == nil || s.read == nil {
		return false, errors.New("governance reader unavailable")
	}
	if node.PlanID == "" {
		return false, nil
	}
	now := s.clock.Now()
	reviews, err := s.read.ListReviewsByPlan(ctx, node.PlanID, 100)
	if err != nil {
		return false, err
	}
	for _, r := range reviews {
		if r.IsApproved(now) && r.NodeID != nil && *r.NodeID == node.ID {
			return true, nil
		}
	}
	return false, nil
}

// ApproveReview transitions a pending review to approved.
func (s *Service) ApproveReview(ctx context.Context, reviewID, reviewerNote string) error {
	if s == nil || s.write == nil {
		return errors.New("governance writer unavailable")
	}
	review, err := s.read.GetReview(ctx, reviewID)
	if err != nil {
		return err
	}
	if review == nil {
		return ErrReviewNotFound
	}
	if review.Status != governance.ReviewStatusPending {
		return ErrReviewNotPending
	}
	// Check expiration.
	if review.ExpiresAt != nil && s.clock.Now().After(*review.ExpiresAt) {
		now := s.clock.Now()
		if err := s.write.UpdateReviewStatus(ctx, reviewID, governance.ReviewStatusExpired, "auto-expired", &now); err != nil {
			return err
		}
		return ErrReviewExpired
	}
	now := s.clock.Now()
	return s.write.UpdateReviewStatus(ctx, reviewID, governance.ReviewStatusApproved, reviewerNote, &now)
}

// RejectReview transitions a pending review to rejected.
func (s *Service) RejectReview(ctx context.Context, reviewID, reviewerNote string) error {
	if s == nil || s.write == nil {
		return errors.New("governance writer unavailable")
	}
	review, err := s.read.GetReview(ctx, reviewID)
	if err != nil {
		return err
	}
	if review == nil {
		return ErrReviewNotFound
	}
	if review.Status != governance.ReviewStatusPending {
		return ErrReviewNotPending
	}
	now := s.clock.Now()
	return s.write.UpdateReviewStatus(ctx, reviewID, governance.ReviewStatusRejected, reviewerNote, &now)
}

// GetReview returns a review by ID.
func (s *Service) GetReview(ctx context.Context, id string) (*governance.Review, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("governance reader unavailable")
	}
	r, err := s.read.GetReview(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrReviewNotFound
	}
	return r, nil
}

// ListReviewsByPlan returns reviews for a plan.
func (s *Service) ListReviewsByPlan(ctx context.Context, planID string) ([]governance.Review, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("governance reader unavailable")
	}
	return s.read.ListReviewsByPlan(ctx, planID, 100)
}

// ListActivePolicies returns all active governance policies.
func (s *Service) ListActivePolicies(ctx context.Context) ([]governance.Policy, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("governance reader unavailable")
	}
	return s.read.ListPolicies(ctx, true, 100)
}

// ComputeDigest computes a SHA-256 hex digest of the given data.
// Used for creating action/input/state digests for review records.
func ComputeDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// EnforceGate is the main entry point for governance enforcement.
// It checks if a node requires review and if so, verifies that an approved review exists.
// Returns nil if the node can proceed, or an error explaining why it cannot.
func (s *Service) EnforceGate(ctx context.Context, node planning.Node) error {
	if s == nil {
		return errors.New("governance service unavailable")
	}
	requires, err := s.RequiresReview(ctx, node)
	if err != nil {
		return err
	}
	if !requires {
		return nil
	}
	approved, err := s.IsApproved(ctx, node)
	if err != nil {
		return err
	}
	if !approved {
		return ErrReviewNotApproved
	}
	return nil
}
