package app

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/governance"
	"github.com/lunitide/lunitide/internal/governanceapp"
)

type GovernanceService interface {
	ListReviewsByPlan(context.Context, string) ([]governance.Review, error)
	ApproveReview(context.Context, string, string) error
	RejectReview(context.Context, string, string) error
}

type reviewDTO struct {
	ID            string                 `json:"id"`
	PlanID        *string                `json:"planId,omitempty"`
	NodeID        *string                `json:"nodeId,omitempty"`
	ActionType    governance.ActionType  `json:"actionType"`
	ActionDigest  string                 `json:"actionDigest"`
	InputDigest   string                 `json:"inputDigest"`
	StateDigest   string                 `json:"stateDigest"`
	PolicyVersion int64                  `json:"policyVersion"`
	RiskLevel     string                 `json:"riskLevel"`
	Status        governance.ReviewStatus `json:"status"`
	ReviewerNote  string                 `json:"reviewerNote"`
	ExpiresAt     *time.Time             `json:"expiresAt,omitempty"`
	CreatedAt     time.Time              `json:"createdAt"`
	ReviewedAt    *time.Time             `json:"reviewedAt,omitempty"`
}

func newReviewDTO(r governance.Review) reviewDTO {
	return reviewDTO{
		ID:            r.ID,
		PlanID:        r.PlanID,
		NodeID:        r.NodeID,
		ActionType:    r.ActionType,
		ActionDigest:  r.ActionDigest,
		InputDigest:   r.InputDigest,
		StateDigest:   r.StateDigest,
		PolicyVersion: r.PolicyVersion,
		RiskLevel:     r.RiskLevel,
		Status:        r.Status,
		ReviewerNote:  r.ReviewerNote,
		ExpiresAt:     r.ExpiresAt,
		CreatedAt:     r.CreatedAt,
		ReviewedAt:    r.ReviewedAt,
	}
}

func governanceServiceAvailable(service GovernanceService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

func handleReviewList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "review.list 参数无效", false)
	}
	if !governanceServiceAvailable(e.governance) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "审批数据暂时不可用", true)
	}
	items, err := e.governance.ListReviewsByPlan(ctx, p.PlanID)
	if err != nil {
		return reviewFailure(r, err)
	}
	dtos := make([]reviewDTO, len(items))
	for i := range items {
		dtos[i] = newReviewDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []reviewDTO `json:"items"`
	}{Items: dtos})
}

func handleReviewApprove(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ReviewID string `json:"reviewId"`
		Note     string `json:"note"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ReviewID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "review.approve 参数无效", false)
	}
	if !governanceServiceAvailable(e.governance) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "审批数据暂时不可用", true)
	}
	if err := e.governance.ApproveReview(ctx, p.ReviewID, p.Note); err != nil {
		return reviewFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"approved": true})
}

func handleReviewReject(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ReviewID string `json:"reviewId"`
		Note     string `json:"note"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ReviewID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "review.reject 参数无效", false)
	}
	if !governanceServiceAvailable(e.governance) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "审批数据暂时不可用", true)
	}
	if err := e.governance.RejectReview(ctx, p.ReviewID, p.Note); err != nil {
		return reviewFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"rejected": true})
}

func reviewFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, governanceapp.ErrReviewNotFound):
		return bridge.Failure(r.ID, r.TraceID, "REVIEW_NOT_FOUND", "审批记录不存在", false)
	case errors.Is(err, governanceapp.ErrReviewNotPending):
		return bridge.Failure(r.ID, r.TraceID, "REVIEW_NOT_PENDING", "审批记录不在待处理状态", false)
	case errors.Is(err, governanceapp.ErrReviewExpired):
		return bridge.Failure(r.ID, r.TraceID, "REVIEW_EXPIRED", "审批记录已过期", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "审批数据暂时不可用", true)
	}
}
