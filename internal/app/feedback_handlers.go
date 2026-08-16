package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
)

// Learning-loop handlers (P3-3): feedback.record / feedback.candidates.
//
// The loop is: chat-side accept/reject/correct -> append-only feedback_events
// (migration 0065) -> corrections propose governed pending candidates ->
// the user confirms them in the memory center via memory.confirmCandidate ->
// confirmed preferences are injected into the chat system instruction.

// handleFeedbackRecord appends one feedback event; corrections additionally
// propose a pending preference candidate and answer its confirmation token.
func handleFeedbackRecord(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Action     string `json:"action"`
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Text       string `json:"text"`
		Actor      string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "feedback.record 参数无效", false)
	}
	switch p.Action {
	case m8app.FeedbackAccept, m8app.FeedbackReject, m8app.FeedbackCorrect:
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "feedback.record action 无效", false)
	}
	if len(p.TargetType) < 1 || len(p.TargetType) > 64 || len(p.TargetID) < 1 || len(p.TargetID) > 128 ||
		len(p.Text) > 2048 || len(p.Actor) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "feedback.record 参数无效", false)
	}
	if p.Action == m8app.FeedbackCorrect && strings.TrimSpace(p.Text) == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "纠正反馈需要填写偏好内容", false)
	}
	if e.m8memory == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "学习闭环服务暂时不可用", true)
	}
	res, err := e.m8memory.RecordFeedback(ctx, m8app.FeedbackRecordInput{
		Action:     p.Action,
		TargetType: p.TargetType,
		TargetID:   p.TargetID,
		Text:       p.Text,
		Actor:      p.Actor,
	})
	if err != nil {
		return m8MemoryFailure(r, err)
	}
	out := struct {
		EventID           string `json:"eventId"`
		CandidateID       string `json:"candidateId,omitempty"`
		ConfirmationToken string `json:"confirmationToken,omitempty"`
	}{EventID: res.EventID}
	if res.CandidateID != "" {
		out.CandidateID = res.CandidateID
		out.ConfirmationToken = res.ConfirmationToken
	}
	return bridge.Success(r.ID, out)
}

// handleFeedbackCandidates answers pending preference candidates for the
// memory-center confirmation journey.
func handleFeedbackCandidates(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Limit int `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Limit < 0 || p.Limit > 100 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "feedback.candidates 参数无效", false)
	}
	if e.m8memory == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "学习闭环服务暂时不可用", true)
	}
	items, err := e.m8memory.ListPendingCandidates(ctx, p.Limit)
	if err != nil {
		return m8MemoryFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Items []m8app.PendingCandidateView `json:"items"`
	}{Items: items})
}
