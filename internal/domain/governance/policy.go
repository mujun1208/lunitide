package governance

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// ReviewStatus represents the lifecycle state of a review.
type ReviewStatus string

const (
	ReviewStatusPending              ReviewStatus = "pending"
	ReviewStatusApproved             ReviewStatus = "approved"
	ReviewStatusRejected             ReviewStatus = "rejected"
	ReviewStatusExpired              ReviewStatus = "expired"
	ReviewStatusChangedAfterApproval ReviewStatus = "changed_after_approval"
)

// IsTerminal returns true if the review is in a terminal state.
func (s ReviewStatus) IsTerminal() bool {
	return s == ReviewStatusApproved || s == ReviewStatusRejected || s == ReviewStatusExpired
}

// ActionType categorizes the operation being reviewed.
type ActionType string

const (
	ActionPush             ActionType = "push"
	ActionPublish          ActionType = "publish"
	ActionDeploy           ActionType = "deploy"
	ActionDestructiveDB    ActionType = "destructive_db"
	ActionDestructiveFS    ActionType = "destructive_fs"
	ActionCredentialExport ActionType = "credential_export"
	ActionCrossDomainSend  ActionType = "cross_domain_send"
	ActionUntrustedCode    ActionType = "untrusted_code"
	ActionSkillExpand      ActionType = "skill_expand"
	ActionSecurityPolicy   ActionType = "security_policy"
	ActionAuditChange      ActionType = "audit_change"
	ActionTrustRoot        ActionType = "trust_root"
)

// AlwaysRequiresReview returns true if this action type always requires human review,
// even in full-automation mode.
func (a ActionType) AlwaysRequiresReview() bool {
	switch a {
	case ActionPush, ActionPublish, ActionDeploy,
		ActionDestructiveDB, ActionDestructiveFS,
		ActionCredentialExport, ActionCrossDomainSend,
		ActionUntrustedCode, ActionSkillExpand,
		ActionSecurityPolicy, ActionAuditChange, ActionTrustRoot:
		return true
	default:
		return false
	}
}

// Review is an approval record for an immutable operation digest.
// Reviews are bound to exact action/input/state digests to prevent replay.
type Review struct {
	ID            string       `json:"id"`
	PlanID        *string      `json:"planId,omitempty"`
	NodeID        *string      `json:"nodeId,omitempty"`
	ActionType    ActionType   `json:"actionType"`
	ActionDigest  string       `json:"actionDigest"`
	InputDigest   string       `json:"inputDigest"`
	StateDigest   string       `json:"stateDigest"`
	PolicyVersion int64        `json:"policyVersion"`
	RiskLevel     string       `json:"riskLevel"`
	Status        ReviewStatus `json:"status"`
	ReviewerNote  string       `json:"reviewerNote"`
	ExpiresAt     *time.Time   `json:"expiresAt,omitempty"`
	CreatedAt     time.Time    `json:"createdAt"`
	ReviewedAt    *time.Time   `json:"reviewedAt,omitempty"`
}

// Validate checks invariants for a review.
func (r Review) Validate() error {
	if !canonicalULID(r.ID) {
		return errors.New("review id is not a canonical ULID")
	}
	if r.PlanID != nil && !canonicalULID(*r.PlanID) {
		return errors.New("review plan_id is not a canonical ULID")
	}
	if r.NodeID != nil && !canonicalULID(*r.NodeID) {
		return errors.New("review node_id is not a canonical ULID")
	}
	if len(r.ActionType) < 1 || len(r.ActionType) > 64 {
		return errors.New("review action_type invalid")
	}
	if len(r.ActionDigest) != 64 || !isHex(r.ActionDigest) {
		return errors.New("review action_digest must be 64 hex chars")
	}
	if len(r.InputDigest) != 64 || !isHex(r.InputDigest) {
		return errors.New("review input_digest must be 64 hex chars")
	}
	if len(r.StateDigest) != 64 || !isHex(r.StateDigest) {
		return errors.New("review state_digest must be 64 hex chars")
	}
	if r.PolicyVersion < 1 {
		return errors.New("review policy_version must be positive")
	}
	switch r.RiskLevel {
	case "low", "medium", "high", "critical":
	default:
		return errors.New("review risk_level invalid")
	}
	switch r.Status {
	case ReviewStatusPending, ReviewStatusApproved, ReviewStatusRejected, ReviewStatusExpired, ReviewStatusChangedAfterApproval:
	default:
		return errors.New("review status invalid")
	}
	if len(r.ReviewerNote) > 4096 {
		return errors.New("review reviewer_note too long")
	}
	if r.CreatedAt.IsZero() || r.CreatedAt.Location() != time.UTC {
		return errors.New("review created_at must be UTC")
	}
	if r.Status.IsTerminal() && r.Status != ReviewStatusChangedAfterApproval && r.ReviewedAt == nil {
		return errors.New("review reviewed_at must be set for terminal status")
	}
	if r.ReviewedAt != nil && r.ReviewedAt.Location() != time.UTC {
		return errors.New("review reviewed_at must be UTC")
	}
	if r.ExpiresAt != nil && r.ExpiresAt.Location() != time.UTC {
		return errors.New("review expires_at must be UTC")
	}
	return nil
}

// IsApproved returns true if the review is currently approved and not expired.
func (r Review) IsApproved(now time.Time) bool {
	if r.Status != ReviewStatusApproved {
		return false
	}
	if r.ExpiresAt != nil && now.After(*r.ExpiresAt) {
		return false
	}
	return true
}

// Policy represents a governance rule that controls risk evaluation.
type Policy struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     int64     `json:"version"`
	IsActive    bool      `json:"isActive"`
	RulesJSON   string    `json:"rulesJson"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Validate checks invariants for a policy.
func (p Policy) Validate() error {
	if !canonicalULID(p.ID) {
		return errors.New("policy id is not a canonical ULID")
	}
	if len(p.Name) < 1 || len(p.Name) > 200 {
		return errors.New("policy name must be 1-200 characters")
	}
	if len(p.Description) > 4096 {
		return errors.New("policy description too long")
	}
	if p.Version < 1 {
		return errors.New("policy version must be positive")
	}
	if len(p.RulesJSON) < 2 || len(p.RulesJSON) > 65536 {
		return errors.New("policy rules_json size out of bounds")
	}
	if p.CreatedAt.IsZero() || p.CreatedAt.Location() != time.UTC {
		return errors.New("policy created_at must be UTC")
	}
	if p.UpdatedAt.IsZero() || p.UpdatedAt.Location() != time.UTC {
		return errors.New("policy updated_at must be UTC")
	}
	if p.UpdatedAt.Before(p.CreatedAt) {
		return errors.New("policy updated_at must be >= created_at")
	}
	return nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
