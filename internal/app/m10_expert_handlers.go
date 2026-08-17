package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// M10 expert scenario-card handlers: expert.scenario.create / list / delete.
//
// Error mapping follows the M10 wire contract (M10-EX-001~003); delete is the
// guarded soft archive, never a hard row removal.

func handleExpertScenarioCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID string          `json:"expertId"`
		Title    string          `json:"title"`
		Summary  string          `json:"summary"`
		PhaseKey string          `json:"phaseKey"`
		Scenario json.RawMessage `json:"scenario"`
		Actor    string          `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) ||
		len(p.Title) < 1 || len(p.Title) > m8core.MaxScenarioTitle ||
		len(p.Summary) < 1 || len(p.Summary) > m8core.MaxScenarioSummary ||
		!m8core.ValidPhaseKey(p.PhaseKey) || len(p.Actor) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.scenario.create 参数无效", false)
	}
	if e.m10scenario == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家场景卡服务暂时不可用", true)
	}
	res, err := e.m10scenario.CreateScenario(ctx, m8app.ScenarioCreateInput{
		ExpertID: p.ExpertID, Title: p.Title, Summary: p.Summary,
		PhaseKey: p.PhaseKey, Scenario: p.Scenario, Actor: p.Actor,
	})
	if err != nil {
		return m10ScenarioFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertScenarioList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID string `json:"expertId"`
		State    string `json:"state"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.scenario.list 参数无效", false)
	}
	switch p.State {
	case "", m8core.ScenarioActive, m8core.ScenarioArchived:
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.scenario.list state 无效", false)
	}
	if e.m10scenario == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家场景卡服务暂时不可用", true)
	}
	items, err := e.m10scenario.ListScenarios(ctx, p.ExpertID, p.State)
	if err != nil {
		return m10ScenarioFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Items []m8app.ScenarioView `json:"items"`
	}{Items: items})
}

func handleExpertScenarioDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScenarioCardID string `json:"scenarioCardId"`
		Actor          string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ScenarioCardID) || len(p.Actor) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.scenario.delete 参数无效", false)
	}
	if e.m10scenario == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家场景卡服务暂时不可用", true)
	}
	if err := e.m10scenario.DeleteScenario(ctx, p.ScenarioCardID, p.Actor); err != nil {
		return m10ScenarioFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		ScenarioCardID string `json:"scenarioCardId"`
		State          string `json:"state"`
	}{ScenarioCardID: p.ScenarioCardID, State: m8core.ScenarioArchived})
}

// m10ScenarioFailure maps m8app scenario errors onto M10-EX-001~003.
func m10ScenarioFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrScenarioNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M10-EX-001", "场景卡不存在", false)
	case errors.Is(err, m8app.ErrScenarioDuplicate):
		return bridge.Failure(r.ID, r.TraceID, "M10-EX-002", "同名场景卡已存在", false)
	case errors.Is(err, m8app.ErrScenarioInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M10-EX-003", "场景卡内容无效", false)
	}
	return m8ExpertFailure(r, err)
}
