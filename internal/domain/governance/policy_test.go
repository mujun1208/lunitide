package governance

import (
	"testing"
	"time"
)

func TestReviewValidate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := Review{
		ID:            "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ActionType:    ActionPush,
		ActionDigest:  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		InputDigest:   "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		StateDigest:   "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		PolicyVersion: 1,
		RiskLevel:     "high",
		Status:        ReviewStatusPending,
		CreatedAt:     now,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid review rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.ActionDigest = "short"
	if err := invalid.Validate(); err == nil {
		t.Fatal("short action digest accepted")
	}
	invalid = valid
	invalid.Status = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown status accepted")
	}
	invalid = valid
	invalid.RiskLevel = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown risk level accepted")
	}
	invalid = valid
	invalid.Status = ReviewStatusApproved
	invalid.ReviewedAt = nil
	if err := invalid.Validate(); err == nil {
		t.Fatal("approved without reviewed_at accepted")
	}
}

func TestReviewIsApproved(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := Review{Status: ReviewStatusApproved}
	if !r.IsApproved(now) {
		t.Fatal("approved review should be approved")
	}
	r.Status = ReviewStatusPending
	if r.IsApproved(now) {
		t.Fatal("pending review should not be approved")
	}
	r.Status = ReviewStatusApproved
	expires := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	r.ExpiresAt = &expires
	if r.IsApproved(now) {
		t.Fatal("expired review should not be approved")
	}
}

func TestActionTypeAlwaysRequiresReview(t *testing.T) {
	alwaysReview := []ActionType{
		ActionPush, ActionPublish, ActionDeploy,
		ActionDestructiveDB, ActionDestructiveFS,
		ActionCredentialExport, ActionCrossDomainSend,
		ActionUntrustedCode, ActionSkillExpand,
		ActionSecurityPolicy, ActionAuditChange, ActionTrustRoot,
	}
	for _, a := range alwaysReview {
		if !a.AlwaysRequiresReview() {
			t.Fatalf("action %s should always require review", a)
		}
	}
}

func TestPolicyValidate(t *testing.T) {
	valid := Policy{
		ID:          "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:        "Test Policy",
		Description: "A test policy",
		Version:     1,
		IsActive:    true,
		RulesJSON:   "{}",
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.Version = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero version accepted")
	}
	invalid = valid
	invalid.RulesJSON = "{"
	if err := invalid.Validate(); err == nil {
		t.Fatal("too short rules_json accepted")
	}
}