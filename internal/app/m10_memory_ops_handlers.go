package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// M10 memory-operations handlers: memory.stats / memory.facts.list /
// memory.facts.flag / memory.traces.list / memory.growth.list /
// memory.growth.decide / memory.settings.get / memory.settings.update /
// memory.export / memory.purge. Error mapping follows the M10-MO wire
// contract; purge is the only destructive path and stays behind the
// explicit renderer confirm dialog.

func handleMemoryOpsStats(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	stats, err := e.memoryOps.Stats(ctx)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	return r.Ok(statsDTO(stats))
}

func handleMemoryFactsList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		State  string `json:"state"`
		Scope  string `json:"scope"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.State != "" && !validFactState(p.State)) ||
		(p.Limit < 1 || p.Limit > 100) || p.Offset < 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.facts.list 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	items, total, err := e.memoryOps.Facts(ctx, p.State, p.Scope, p.Limit, p.Offset)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	dtos := make([]factViewDTO, 0, len(items))
	for _, f := range items {
		dtos = append(dtos, factViewDTO{
			FactID: f.FactID, ScopeID: f.ScopeID, Version: f.Version,
			Sensitivity: f.Sensitivity, State: f.State, CreatedAt: f.CreatedAt,
			Pinned: f.Pinned, Hidden: f.Hidden, Note: f.Note,
		})
	}
	return r.Ok(struct {
		Items  []factViewDTO `json:"items"`
		Total  int           `json:"total"`
		Limit  int           `json:"limit"`
		Offset int           `json:"offset"`
	}{Items: dtos, Total: total, Limit: p.Limit, Offset: p.Offset})
}

func handleMemoryFactsFlag(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FactID string `json:"factId"`
		Flag   string `json:"flag"`
		Note   string `json:"note"`
		On     bool   `json:"on"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.FactID) || !m8core.ValidFactFlag(p.Flag) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.facts.flag 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	if err := e.memoryOps.FlagFact(ctx, p.FactID, p.Flag, p.Note, p.On); err != nil {
		return memoryOpsFailure(r, err)
	}
	return r.Ok(struct {
		FactID string `json:"factId"`
		Flag   string `json:"flag"`
		On     bool   `json:"on"`
	}{FactID: p.FactID, Flag: p.Flag, On: p.On})
}

func handleMemoryTracesList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Limit < 1 || p.Limit > 100 || p.Offset < 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.traces.list 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	rows, total, err := e.memoryOps.Traces(ctx, p.Limit, p.Offset)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	items := make([]traceDTO, 0, len(rows))
	for _, t := range rows {
		items = append(items, traceDTO{
			ID: t.ID, QueryDigest: t.QueryDigest, HitsJSON: t.HitsJSON,
			ReasonsJSON: t.ReasonsJSON, RedactionsJSON: t.PolicyRedactionsJSON, CreatedAt: t.CreatedAt,
		})
	}
	return r.Ok(struct {
		Items  []traceDTO `json:"items"`
		Total  int        `json:"total"`
		Limit  int        `json:"limit"`
		Offset int        `json:"offset"`
	}{Items: items, Total: total, Limit: p.Limit, Offset: p.Offset})
}

func handleMemoryGrowthList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.Status != "" && !m8core.ValidGrowthStatus(p.Status)) ||
		p.Limit < 1 || p.Limit > 100 || p.Offset < 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.growth.list 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	rows, total, err := e.memoryOps.GrowthList(ctx, p.Status, p.Limit, p.Offset)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	items := make([]growthDTO, 0, len(rows))
	for _, g := range rows {
		items = append(items, growthDTO{
			FactID: g.FactID, ScopeID: g.ScopeID, Status: g.Status,
			ReferenceCount: g.ReferenceCount, LastReferencedAt: g.LastReferencedAt,
			ReviewAt: g.ReviewAt, DecidedAt: g.DecidedAt, CreatedAt: g.CreatedAt,
		})
	}
	return r.Ok(struct {
		Items  []growthDTO `json:"items"`
		Total  int         `json:"total"`
		Limit  int         `json:"limit"`
		Offset int         `json:"offset"`
	}{Items: items, Total: total, Limit: p.Limit, Offset: p.Offset})
}

func handleMemoryGrowthDecide(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FactID   string `json:"factId"`
		Decision string `json:"decision"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.FactID) ||
		(p.Decision != "promoted" && p.Decision != "dropped") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.growth.decide 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	if err := e.memoryOps.GrowthDecide(ctx, p.FactID, p.Decision); err != nil {
		return memoryOpsFailure(r, err)
	}
	return r.Ok(struct {
		FactID   string `json:"factId"`
		Decision string `json:"decision"`
	}{FactID: p.FactID, Decision: p.Decision})
}

func handleMemorySettingsGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectID string `json:"subjectId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.SubjectID) < 1 || len(p.SubjectID) > m8core.MaxSubjectID {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.settings.get 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	st, err := e.memoryOps.SettingsGet(ctx, p.SubjectID)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	return r.Ok(settingsDTO(st))
}

func handleMemorySettingsUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectID     string `json:"subjectId"`
		MemoryEnabled bool   `json:"memoryEnabled"`
		AutoNominate  bool   `json:"autoNominate"`
		GrowthDays    int    `json:"growthDays"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.SubjectID) < 1 || len(p.SubjectID) > m8core.MaxSubjectID {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.settings.update 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	if err := e.memoryOps.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: p.SubjectID, MemoryEnabled: p.MemoryEnabled,
		AutoNominate: p.AutoNominate, GrowthDays: p.GrowthDays,
	}); err != nil {
		return memoryOpsFailure(r, err)
	}
	st, err := e.memoryOps.SettingsGet(ctx, p.SubjectID)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	return r.Ok(settingsDTO(st))
}

func handleMemoryExport(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.export 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	bundle, err := e.memoryOps.Export(ctx)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	facts := make([]map[string]any, 0, len(bundle.Facts))
	for _, f := range bundle.Facts {
		facts = append(facts, map[string]any{
			"factId": f.FactID, "scopeId": f.ScopeID, "version": f.Version,
			"sensitivity": f.Sensitivity, "state": f.State,
			"supersededBy": f.SupersededBy, "deletedAt": f.DeletedAt, "createdAt": f.CreatedAt,
		})
	}
	leaves := make([]map[string]any, 0, len(bundle.Leaves))
	for _, l := range bundle.Leaves {
		leaves = append(leaves, map[string]any{
			"id": l.ID, "factId": l.FactID, "factVersion": l.FactVersion,
			"jsonPointer": l.JSONPointer, "evidenceRef": l.EvidenceRef,
			"digest": l.Digest, "createdAt": l.CreatedAt,
		})
	}
	candidates := make([]map[string]any, 0, len(bundle.Candidates))
	for _, c := range bundle.Candidates {
		candidates = append(candidates, map[string]any{
			"candidateId": c.CandidateID, "subjectId": c.SubjectID, "payload": c.Payload,
			"payloadDigest": c.PayloadDigest, "inferred": c.Inferred, "trust": c.Trust,
			"state": c.State, "expiresAt": c.ExpiresAt, "createdAt": c.CreatedAt,
		})
	}
	traces := make([]map[string]any, 0, len(bundle.Traces))
	for _, t := range bundle.Traces {
		traces = append(traces, map[string]any{
			"id": t.ID, "queryDigest": t.QueryDigest, "hits": t.HitsJSON,
			"reasons": t.ReasonsJSON, "redactions": t.PolicyRedactionsJSON, "createdAt": t.CreatedAt,
		})
	}
	growth := make([]map[string]any, 0, len(bundle.Growth))
	for _, g := range bundle.Growth {
		growth = append(growth, map[string]any{
			"factId": g.FactID, "scopeId": g.ScopeID, "status": g.Status,
			"referenceCount": g.ReferenceCount, "reviewAt": g.ReviewAt,
			"decidedAt": g.DecidedAt, "createdAt": g.CreatedAt,
		})
	}
	flags := make([]map[string]any, 0, len(bundle.Flags))
	for _, f := range bundle.Flags {
		flags = append(flags, map[string]any{
			"factId": f.FactID, "flag": f.Flag, "note": f.Note, "createdAt": f.CreatedAt,
		})
	}
	settings := make([]map[string]any, 0, len(bundle.Settings))
	for _, st := range bundle.Settings {
		settings = append(settings, map[string]any{
			"subjectId": st.SubjectID, "memoryEnabled": st.MemoryEnabled,
			"autoNominate": st.AutoNominate, "growthDays": st.GrowthDays,
		})
	}
	return r.Ok(struct {
		Facts      []map[string]any `json:"facts"`
		Leaves     []map[string]any `json:"leaves"`
		Candidates []map[string]any `json:"candidates"`
		Traces     []map[string]any `json:"traces"`
		Growth     []map[string]any `json:"growth"`
		Flags      []map[string]any `json:"flags"`
		Settings   []map[string]any `json:"settings"`
	}{Facts: facts, Leaves: leaves, Candidates: candidates, Traces: traces, Growth: growth, Flags: flags, Settings: settings})
}

func handleMemoryPurge(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.purge 参数无效", false)
	}
	if e.memoryOps == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆运营服务暂时不可用", true)
	}
	counts, err := e.memoryOps.Purge(ctx)
	if err != nil {
		return memoryOpsFailure(r, err)
	}
	return r.Ok(struct {
		FactsTombstoned int64 `json:"factsTombstoned"`
		Candidates      int64 `json:"candidates"`
		GrowthRows      int64 `json:"growthRows"`
		Flags           int64 `json:"flags"`
		Traces          int64 `json:"traces"`
		Memories        int64 `json:"memories"`
	}{
		FactsTombstoned: counts.FactsTombstoned, Candidates: counts.Candidates,
		GrowthRows: counts.GrowthRows, Flags: counts.Flags,
		Traces: counts.Traces, Memories: counts.Memories,
	})
}

func validFactState(state string) bool {
	return state == "active" || state == "superseded" || state == "tombstoned"
}

type factViewDTO struct {
	FactID      string `json:"factId"`
	ScopeID     string `json:"scopeId"`
	Version     int64  `json:"version"`
	Sensitivity string `json:"sensitivity"`
	State       string `json:"state"`
	CreatedAt   string `json:"createdAt"`
	Pinned      bool   `json:"pinned"`
	Hidden      bool   `json:"hidden"`
	Note        string `json:"note"`
}

type traceDTO struct {
	ID             string `json:"id"`
	QueryDigest    string `json:"queryDigest"`
	HitsJSON       string `json:"hits"`
	ReasonsJSON    string `json:"reasons"`
	RedactionsJSON string `json:"redactions"`
	CreatedAt      string `json:"createdAt"`
}

type growthDTO struct {
	FactID           string `json:"factId"`
	ScopeID          string `json:"scopeId"`
	Status           string `json:"status"`
	ReferenceCount   int64  `json:"referenceCount"`
	LastReferencedAt string `json:"lastReferencedAt"`
	ReviewAt         string `json:"reviewAt"`
	DecidedAt        string `json:"decidedAt"`
	CreatedAt        string `json:"createdAt"`
}

func settingsDTO(st m8core.MemorySettings) struct {
	SubjectID     string `json:"subjectId"`
	MemoryEnabled bool   `json:"memoryEnabled"`
	AutoNominate  bool   `json:"autoNominate"`
	GrowthDays    int    `json:"growthDays"`
	UpdatedAt     string `json:"updatedAt"`
} {
	return struct {
		SubjectID     string `json:"subjectId"`
		MemoryEnabled bool   `json:"memoryEnabled"`
		AutoNominate  bool   `json:"autoNominate"`
		GrowthDays    int    `json:"growthDays"`
		UpdatedAt     string `json:"updatedAt"`
	}{SubjectID: st.SubjectID, MemoryEnabled: st.MemoryEnabled, AutoNominate: st.AutoNominate, GrowthDays: st.GrowthDays, UpdatedAt: st.UpdatedAt}
}

func statsDTO(stats m8app.MemoryOpsStats) struct {
	FactsByState       map[string]int `json:"factsByState"`
	FactsBySensitivity map[string]int `json:"factsBySensitivity"`
	CandidatesByState  map[string]int `json:"candidatesByState"`
	GrowthByStatus     map[string]int `json:"growthByStatus"`
	TracesTotal        int            `json:"tracesTotal"`
	TracesLast7Days    int            `json:"tracesLast7Days"`
	MemoriesTotal      int            `json:"memoriesTotal"`
} {
	return struct {
		FactsByState       map[string]int `json:"factsByState"`
		FactsBySensitivity map[string]int `json:"factsBySensitivity"`
		CandidatesByState  map[string]int `json:"candidatesByState"`
		GrowthByStatus     map[string]int `json:"growthByStatus"`
		TracesTotal        int            `json:"tracesTotal"`
		TracesLast7Days    int            `json:"tracesLast7Days"`
		MemoriesTotal      int            `json:"memoriesTotal"`
	}{
		FactsByState: stats.FactsByState, FactsBySensitivity: stats.FactsBySensitivity,
		CandidatesByState: stats.CandidatesByState, GrowthByStatus: stats.GrowthByStatus,
		TracesTotal: stats.TracesTotal, TracesLast7Days: stats.TracesLast7Days,
		MemoriesTotal: stats.MemoriesTotal,
	}
}

// memoryOpsFailure maps m8app memory-ops errors onto M10-MO-001~005.
func memoryOpsFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrOpsFactNotFound):
		return r.Fail("M10-MO-001", "记忆事实不存在", false)
	case errors.Is(err, m8app.ErrOpsFlagInvalid):
		return r.Fail("M10-MO-002", "事实标记无效", false)
	case errors.Is(err, m8app.ErrOpsGrowthConflict):
		return r.Fail("M10-MO-003", "成长箱条目不存在或已处理", false)
	case errors.Is(err, m8app.ErrOpsSettingsInvalid):
		return r.Fail("M10-MO-004", "记忆设置无效（成长期需 1–90 天）", false)
	case errors.Is(err, m8app.ErrOpsDecisionInvalid):
		return r.Fail("M10-MO-005", "成长箱决定无效", false)
	}
	return r.Fail("STORAGE_UNAVAILABLE", "记忆运营存储暂时不可用", true)
}
