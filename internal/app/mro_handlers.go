package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mroapp"
)

func handleMROAircraftList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.aircraft.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListAircraft(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	if items == nil {
		items = []mroapp.Aircraft{}
	}
	return r.Ok(map[string]any{"items": items})
}

func handleMROAircraftUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		AircraftID string `json:"aircraftId"`
		TailNo     string `json:"tailNo"`
		MSN        string `json:"msn"`
		Model      string `json:"model"`
		Config     string `json:"config"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.TailNo) == "" || strings.TrimSpace(p.Model) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.aircraft.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	row, err := e.mro.UpsertAircraft(ctx, mroapp.AircraftInput{
		AircraftID: p.AircraftID, TailNo: p.TailNo, MSN: p.MSN, Model: p.Model, Config: p.Config,
	})
	if err != nil {
		return mroFailure(r, err)
	}
	return r.Ok(row)
}

func handleMROManualList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.manual.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListManuals(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	if items == nil {
		items = []mroapp.Manual{}
	}
	return r.Ok(map[string]any{"items": items})
}

func handleMROManualRegister(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Title     string `json:"title"`
		DocType   string `json:"docType"`
		Revision  string `json:"revision"`
		Status    string `json:"status"`
		ATA       string `json:"ata"`
		Documents []struct {
			DocumentID string `json:"documentId"`
			PartNo     int    `json:"partNo"`
		} `json:"documents"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.DocType) == "" || strings.TrimSpace(p.Revision) == "" || len(p.Documents) == 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.manual.register 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	docs := make([]mroapp.ManualDocInput, 0, len(p.Documents))
	ids := make([]string, 0, len(p.Documents))
	for _, d := range p.Documents {
		docs = append(docs, mroapp.ManualDocInput{DocumentID: d.DocumentID, PartNo: d.PartNo})
		ids = append(ids, d.DocumentID)
	}
	if e.m8kb == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "知识库服务暂时不可用", true)
	}
	if err := e.m8kb.DocumentsReady(ctx, ids); err != nil {
		return r.Fail("MRO-DOC-NOT-READY", "手册文档尚未入库或索引失败：请先在专家知识库导入并等待就绪后再登记", false)
	}
	row, err := e.mro.RegisterManual(ctx, mroapp.ManualInput{
		Title: p.Title, DocType: p.DocType, Revision: p.Revision, Status: p.Status, ATA: p.ATA, Documents: docs,
	})
	if err != nil {
		return mroFailure(r, err)
	}
	return r.Ok(row)
}

func handleMROChecklistBuild(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	_ = e
	_ = ctx
	var p struct {
		Steps []string               `json:"steps"`
		Cites []mroapp.CitationBlock `json:"cites"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.checklist.build 参数无效", false)
	}
	var out mroapp.Checklist
	if err := json.Unmarshal(mroapp.BuildChecklistJSON(p.Steps, p.Cites), &out); err != nil {
		return r.Fail("INTERNAL_ERROR", "检查单无法生成", false)
	}
	if out.Steps == nil {
		out.Steps = []mroapp.ChecklistStep{}
	}
	return r.Ok(out)
}

func handleMROAuditList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Limit int `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.audit.list 参数无效", false)
	}
	if e.m8kb == nil {
		return r.Ok(map[string]any{"items": []any{}})
	}
	items, err := e.m8kb.ListRecentKBAudit(ctx, p.Limit)
	if err != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "审计暂时不可用", true)
	}
	if items == nil {
		items = []m8app.AuditRow{}
	}
	return r.Ok(map[string]any{"items": items})
}

func mroFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, mroapp.ErrPayloadInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "机务参数无效", false)
	case errors.Is(err, mroapp.ErrDuplicateTail):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "机尾已存在", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
}
