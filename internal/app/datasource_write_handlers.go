package app

import (
	"context"
	"github.com/lunitide/lunitide/internal/bridge"
)

func handleDatasourceWritePrepare(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ConnectionID string `json:"connectionId"`
		SQL          string `json:"sql"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "写入参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	if fail := requireIdempotency(r); fail != nil {
		return *fail
	}
	op, err := e.datasource.PrepareWrite(ctx, p.ConnectionID, p.SQL, r.IdempotencyKey)
	if err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(op)
}
func handleDatasourceWriteCommit(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID     string `json:"id"`
		Digest string `json:"digest"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "确认参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	if fail := requireIdempotency(r); fail != nil {
		return *fail
	}
	op, err := e.datasource.CommitWrite(ctx, p.ID, p.Digest)
	if err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(op)
}
func handleDatasourceWriteGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "查询参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	op, err := e.datasource.GetWrite(ctx, p.ID)
	if err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(op)
}
func handleDatasourceWriteList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ConnectionID string `json:"connectionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "查询参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	items, err := e.datasource.ListWrites(ctx, p.ConnectionID)
	if err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(map[string]any{"items": items})
}
