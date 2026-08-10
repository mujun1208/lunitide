package governanceapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/governance"
	"github.com/lunitide/lunitide/internal/domain/planning"
)

type mockReviewReader struct {
	review   *governance.Review
	reviews  []governance.Review
	policies []governance.Policy
	err      error
}
func (m *mockReviewReader) GetReview(_ context.Context, _ string) (*governance.Review, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.review, nil
}
func (m *mockReviewReader) ListReviewsByPlan(_ context.Context, _ string, _ int) ([]governance.Review, error) {
	return m.reviews, m.err
}
func (m *mockReviewReader) GetPolicy(_ context.Context, _ string) (*governance.Policy, error) {
	return nil, nil
}
func (m *mockReviewReader) ListPolicies(_ context.Context, _ bool, _ int) ([]governance.Policy, error) {
	return m.policies, nil
}

type mockReviewWriter struct {
	status      governance.ReviewStatus
	note        string
	reviewedAt  *time.Time
	err         error
}
func (m *mockReviewWriter) UpdateReviewStatus(_ context.Context, _ string, status governance.ReviewStatus, note string, reviewedAt *time.Time) error {
	m.status = status
	m.note = note
	m.reviewedAt = reviewedAt
	return m.err
}

func utcTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func TestRequiresReview(t *testing.T) {
	s := New(&mockReviewReader{}, &mockReviewWriter{})
	tests := []struct {
		risk    planning.RiskLevel
		want    bool
	}{
		{planning.RiskLow, false},
		{planning.RiskMedium, false},
		{planning.RiskHigh, true},
		{planning.RiskCritical, true},
	}
	for _, tc := range tests {
		node := planning.Node{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", PlanID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "N", RiskLevel: tc.risk, WorkerRole: "w", Sequence: 1, CreatedAt: utcTime("2025-01-01T00:00:00Z"), UpdatedAt: utcTime("2025-01-01T00:00:00Z")}
		got, err := s.RequiresReview(context.Background(), node)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("risk %s: expected %v, got %v", tc.risk, tc.want, got)
		}
	}
}

func TestIsApprovedFindsApprovedReview(t *testing.T) {
	nodeID := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	planID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	review := governance.Review{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAB", PlanID: &planID, NodeID: &nodeID,
		ActionType: governance.ActionDeploy, ActionDigest: "abcd", InputDigest: "abcd", StateDigest: "abcd",
		PolicyVersion: 1, RiskLevel: "high", Status: governance.ReviewStatusApproved,
		CreatedAt: utcTime("2025-01-01T00:00:00Z"), ReviewedAt: ptrTime(utcTime("2025-01-01T01:00:00Z")),
	}
	r := &mockReviewReader{reviews: []governance.Review{review}}
	s := New(r, &mockReviewWriter{})
	node := planning.Node{ID: nodeID, PlanID: planID, Name: "N", RiskLevel: planning.RiskHigh, WorkerRole: "w", Sequence: 1, CreatedAt: utcTime("2025-01-01T00:00:00Z"), UpdatedAt: utcTime("2025-01-01T00:00:00Z")}
	approved, err := s.IsApproved(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Fatal("expected approved")
	}
}

func TestIsApprovedRejectsExpiredReview(t *testing.T) {
	nodeID := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	planID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	expired := utcTime("2025-01-01T00:00:00Z")
	review := governance.Review{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAB", PlanID: &planID, NodeID: &nodeID,
		ActionType: governance.ActionDeploy, ActionDigest: "abcd", InputDigest: "abcd", StateDigest: "abcd",
		PolicyVersion: 1, RiskLevel: "high", Status: governance.ReviewStatusApproved,
		CreatedAt: utcTime("2024-01-01T00:00:00Z"), ExpiresAt: &expired, ReviewedAt: ptrTime(utcTime("2024-01-01T01:00:00Z")),
	}
	r := &mockReviewReader{reviews: []governance.Review{review}}
	s := New(r, &mockReviewWriter{})
	node := planning.Node{ID: nodeID, PlanID: planID, Name: "N", RiskLevel: planning.RiskHigh, WorkerRole: "w", Sequence: 1, CreatedAt: utcTime("2025-01-01T00:00:00Z"), UpdatedAt: utcTime("2025-01-01T00:00:00Z")}
	approved, _ := s.IsApproved(context.Background(), node)
	if approved {
		t.Fatal("expected not approved (expired)")
	}
}

func TestApproveReviewPending(t *testing.T) {
	review := &governance.Review{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ActionType: governance.ActionDeploy, ActionDigest: "abcd", InputDigest: "abcd", StateDigest: "abcd", PolicyVersion: 1, RiskLevel: "high", Status: governance.ReviewStatusPending, CreatedAt: utcTime("2025-01-01T00:00:00Z")}
	r := &mockReviewReader{review: review}
	w := &mockReviewWriter{}
	s := New(r, w)
	if err := s.ApproveReview(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", "approved"); err != nil {
		t.Fatal(err)
	}
	if w.status != governance.ReviewStatusApproved {
		t.Fatalf("expected approved, got %s", w.status)
	}
	if w.note != "approved" {
		t.Fatalf("expected note 'approved', got %s", w.note)
	}
	if w.reviewedAt == nil {
		t.Fatal("expected reviewedAt to be set")
	}
}

func TestApproveReviewRejectsNonPending(t *testing.T) {
	review := &governance.Review{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ActionType: governance.ActionDeploy, ActionDigest: "abcd", InputDigest: "abcd", StateDigest: "abcd", PolicyVersion: 1, RiskLevel: "high", Status: governance.ReviewStatusApproved, CreatedAt: utcTime("2025-01-01T00:00:00Z"), ReviewedAt: ptrTime(utcTime("2025-01-01T01:00:00Z"))}
	r := &mockReviewReader{review: review}
	s := New(r, &mockReviewWriter{})
	if err := s.ApproveReview(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", ""); err != ErrReviewNotPending {
		t.Fatalf("expected ErrReviewNotPending, got %v", err)
	}
}

func TestRejectReview(t *testing.T) {
	review := &governance.Review{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ActionType: governance.ActionDeploy, ActionDigest: "abcd", InputDigest: "abcd", StateDigest: "abcd", PolicyVersion: 1, RiskLevel: "high", Status: governance.ReviewStatusPending, CreatedAt: utcTime("2025-01-01T00:00:00Z")}
	r := &mockReviewReader{review: review}
	w := &mockReviewWriter{}
	s := New(r, w)
	if err := s.RejectReview(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", "rejected"); err != nil {
		t.Fatal(err)
	}
	if w.status != governance.ReviewStatusRejected {
		t.Fatalf("expected rejected, got %s", w.status)
	}
}

func TestEnforceGateAllowsLowRisk(t *testing.T) {
	s := New(&mockReviewReader{}, &mockReviewWriter{})
	node := planning.Node{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", PlanID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "N", RiskLevel: planning.RiskLow, WorkerRole: "w", Sequence: 1, CreatedAt: utcTime("2025-01-01T00:00:00Z"), UpdatedAt: utcTime("2025-01-01T00:00:00Z")}
	if err := s.EnforceGate(context.Background(), node); err != nil {
		t.Fatalf("low-risk should pass gate, got %v", err)
	}
}

func TestEnforceGateBlocksHighRiskWithoutApproval(t *testing.T) {
	r := &mockReviewReader{reviews: nil}
	s := New(r, &mockReviewWriter{})
	node := planning.Node{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", PlanID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "N", RiskLevel: planning.RiskHigh, WorkerRole: "w", Sequence: 1, CreatedAt: utcTime("2025-01-01T00:00:00Z"), UpdatedAt: utcTime("2025-01-01T00:00:00Z")}
	if err := s.EnforceGate(context.Background(), node); err != ErrReviewNotApproved {
		t.Fatalf("expected ErrReviewNotApproved, got %v", err)
	}
}

func TestEnforceGateAllowsHighRiskWithApproval(t *testing.T) {
	nodeID := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	planID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	review := governance.Review{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAB", PlanID: &planID, NodeID: &nodeID,
		ActionType: governance.ActionDeploy, ActionDigest: "abcd", InputDigest: "abcd", StateDigest: "abcd",
		PolicyVersion: 1, RiskLevel: "high", Status: governance.ReviewStatusApproved,
		CreatedAt: utcTime("2025-01-01T00:00:00Z"), ReviewedAt: ptrTime(utcTime("2025-01-01T01:00:00Z")),
	}
	r := &mockReviewReader{reviews: []governance.Review{review}}
	s := New(r, &mockReviewWriter{})
	node := planning.Node{ID: nodeID, PlanID: planID, Name: "N", RiskLevel: planning.RiskHigh, WorkerRole: "w", Sequence: 1, CreatedAt: utcTime("2025-01-01T00:00:00Z"), UpdatedAt: utcTime("2025-01-01T00:00:00Z")}
	if err := s.EnforceGate(context.Background(), node); err != nil {
		t.Fatalf("approved high-risk should pass gate, got %v", err)
	}
}

func TestGetReviewNotFound(t *testing.T) {
	r := &mockReviewReader{review: nil}
	s := New(r, &mockReviewWriter{})
	if _, err := s.GetReview(context.Background(), "missing"); err != ErrReviewNotFound {
		t.Fatalf("expected ErrReviewNotFound, got %v", err)
	}
}

func TestComputeDigest(t *testing.T) {
	d := ComputeDigest([]byte("test"))
	if len(d) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(d))
	}
}

func TestApproveReviewNotFound(t *testing.T) {
	r := &mockReviewReader{review: nil}
	s := New(r, &mockReviewWriter{})
	if err := s.ApproveReview(context.Background(), "missing", ""); err != ErrReviewNotFound {
		t.Fatalf("expected ErrReviewNotFound, got %v", err)
	}
}

func TestApproveReviewPropagatesError(t *testing.T) {
	r := &mockReviewReader{review: &governance.Review{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ActionType: governance.ActionDeploy, ActionDigest: "abcd", InputDigest: "abcd", StateDigest: "abcd", PolicyVersion: 1, RiskLevel: "high", Status: governance.ReviewStatusPending, CreatedAt: utcTime("2025-01-01T00:00:00Z")}, err: nil}
	w := &mockReviewWriter{err: errors.New("write failure")}
	s := New(r, w)
	if err := s.ApproveReview(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", ""); err == nil {
		t.Fatal("expected error propagation")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
