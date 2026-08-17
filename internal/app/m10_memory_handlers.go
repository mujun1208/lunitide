package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// M10 memory nomination handlers: memory.nominate / memory.nomination.list /
// memory.nomination.withdraw.
//
// Error mapping follows the M10 wire contract (M10-ME-001~004); confirmation
// stays on the 0061 memory.confirmCandidate path — its handler settles the
// nomination via MarkDecided so the 0061 service stays zero-touch.

func handleMemoryNominate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectID       string              `json:"subjectId"`
		Payload         m8core.PayloadDoc   `json:"payload"`
		Reason          string              `json:"reason"`
		Nominator       string              `json:"nominator"`
		SourceSessionID string              `json:"sourceSessionId"`
		Actor           string              `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		len(p.SubjectID) < 1 || len(p.SubjectID) > 128 ||
		len(p.Payload.Content) < 1 || len(p.Payload.Content) > 2048 ||
		len(p.Payload.ScopeID) < 1 || len(p.Payload.ScopeID) > m8core.MaxScopeID ||
		len(p.Payload.Leaves) < 1 ||
		len(p.Reason) < 1 || len(p.Reason) > m8core.MaxReason ||
		len(p.Nominator) > m8core.MaxNominator || len(p.Actor) > 128 ||
		(p.SourceSessionID != "" && !validCanonicalULID(p.SourceSessionID)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "memory.nominate 参数无效", false)
	}
	if e.m10nomination == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "记忆提名服务暂时不可用", true)
	}
	res, err := e.m10nomination.Nominate(ctx, m8app.NominateInput{
		SubjectID:       p.SubjectID,
		Doc:             p.Payload,
		Reason:          p.Reason,
		Nominator:       p.Nominator,
		SourceSessionID: p.SourceSessionID,
		Actor:           p.Actor,
	})
	if err != nil {
		return m10NominationFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		NominationID       string `json:"nominationId"`
		CandidateID        string `json:"candidateId"`
		ConfirmationToken  string `json:"confirmationToken"`
		State              string `json:"state"`
	}{NominationID: res.Nomination.NominationID, CandidateID: res.CandidateID,
		ConfirmationToken: res.ConfirmToken, State: res.Nomination.State})
}

func handleMemoryNominationList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		State string `json:"state"`
		Limit int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "memory.nomination.list 参数无效", false)
	}
	switch p.State {
	case "", m8core.NomNominated, m8core.NomDecided, m8core.NomWithdrawn:
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "memory.nomination.list state 无效", false)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "memory.nomination.list limit 无效", false)
	}
	if e.m10nomination == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "记忆提名服务暂时不可用", true)
	}
	items, err := e.m10nomination.ListNominations(ctx, p.State, p.Limit)
	if err != nil {
		return m10NominationFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Items []m8app.NominationView `json:"items"`
	}{Items: items})
}

func handleMemoryNominationWithdraw(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		NominationID string `json:"nominationId"`
		Actor        string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.NominationID) || len(p.Actor) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "memory.nomination.withdraw 参数无效", false)
	}
	if e.m10nomination == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "记忆提名服务暂时不可用", true)
	}
	if err := e.m10nomination.Withdraw(ctx, p.NominationID, p.Actor); err != nil {
		return m10NominationFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		NominationID string `json:"nominationId"`
		State        string `json:"state"`
	}{NominationID: p.NominationID, State: m8core.NomWithdrawn})
}

// m10NominationFailure maps m8app nomination errors onto M10-ME-001~004.
func m10NominationFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrNominationNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M10-ME-001", "提名不存在", false)
	case errors.Is(err, m8app.ErrNominationTerminal):
		return bridge.Failure(r.ID, r.TraceID, "M10-ME-002", "提名已终结", false)
	case errors.Is(err, m8app.ErrNominationReasonInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M10-ME-003", "提名理由无效", false)
	case errors.Is(err, m8app.ErrNomineeStateConflict):
		return bridge.Failure(r.ID, r.TraceID, "M10-ME-004", "候选状态冲突", false)
	}
	return m8MemoryFailure(r, err)
}
