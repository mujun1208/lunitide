package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/mroapp"
)

func handleMROAircraftList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodeMROPagePayload(r.Payload, &struct{}{}) != nil {
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
	return mroPageResponse(ctx, r, map[string]any{"items": items})
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
	if decodeMROPagePayload(r.Payload, &struct{}{}) != nil {
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
	return mroPageResponse(ctx, r, map[string]any{"items": items})
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
	for _, d := range p.Documents {
		docs = append(docs, mroapp.ManualDocInput{DocumentID: d.DocumentID, PartNo: d.PartNo})
	}
	row, err := e.mro.RegisterManual(ctx, mroapp.ManualInput{
		Title: p.Title, DocType: p.DocType, Revision: p.Revision, Status: p.Status, ATA: p.ATA, Documents: docs,
	})
	if err != nil {
		if errors.Is(err, mroapp.ErrConstraints) {
			return r.Fail("MRO-DOC-NOT-READY", "手册文档尚未就绪或来源已失效，请刷新来源后重试", false)
		}
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
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务审计暂时不可用", true)
	}
	items, err := e.mro.ListAudit(ctx, p.Limit)
	if err != nil {
		return mroFailure(r, err)
	}
	if items == nil {
		items = []mroapp.AuditRow{}
	}
	return r.Ok(map[string]any{"items": items})
}

func mroFailure(r bridge.Request, err error) bridge.Response {
	var blocked *mroapp.CheckoutBlockedError
	switch {
	case errors.Is(err, mroapp.ErrScope):
		return r.Fail("DATA_SCOPE_DENIED", "当前组织无法访问该机务记录", false)
	case errors.Is(err, mroapp.ErrConflict):
		return r.Fail("MRO_CONFLICT", "请求内容或来源证据已变更，请刷新核对", false)
	case errors.Is(err, mroapp.ErrConstraints):
		return r.Fail("MRO_CONSTRAINTS_UNSATISFIED", "当前排程约束或来源证据未通过，请先检查约束与资料版本", false)
	case errors.Is(err, mroapp.ErrCapacity):
		return r.Fail("MRO_CAPACITY_EXCEEDED", "机务记录、关联或来源校验超过容量，请缩小范围或归档后重试", false)
	case errors.As(err, &blocked):
		return r.Fail("BRIDGE_SCHEMA_INVALID", blocked.Reason, false)
	case errors.Is(err, mroapp.ErrCheckoutBlocked):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "校准过期", false)
	case errors.Is(err, mroapp.ErrNotFound):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "机务记录不存在", false)
	case errors.Is(err, mroapp.ErrPayloadInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "机务参数无效", false)
	case errors.Is(err, mroapp.ErrDuplicateTail):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "机尾已存在", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
}
