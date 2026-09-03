package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/datasourceapp"
)

func handleDatasourceList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.list 参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	items, err := e.datasource.List(ctx)
	if err != nil {
		return datasourceFailure(r, err)
	}
	if items == nil {
		items = []datasourceapp.ConnectionPublic{}
	}
	return r.Ok(map[string]any{"items": items})
}

func handleDatasourceCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		DSN  string `json:"dsn"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.DSN) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.create 参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	row, err := e.datasource.Create(ctx, datasourceapp.CreateInput{Name: p.Name, Kind: p.Kind, DSN: p.DSN, Actor: "local-user"})
	if err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(row)
}

func handleDatasourceProbe(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || len(strings.TrimSpace(p.ID)) != 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.probe 参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.datasource.Probe(ctx, p.ID); err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(map[string]any{"id": p.ID, "readonlyVerified": true})
}

func handleDatasourceBrowse(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID     string `json:"id"`
		Scope  string `json:"scope"`
		Schema string `json:"schema"`
		Table  string `json:"table"`
	}
	if decodePayload(r.Payload, &p) != nil || len(strings.TrimSpace(p.ID)) != 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.browse 参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	items, err := e.datasource.Browse(ctx, datasourceapp.BrowseInput{
		ConnectionID: p.ID, Scope: p.Scope, Schema: p.Schema, Table: p.Table,
	})
	if err != nil {
		return datasourceFailure(r, err)
	}
	if items == nil {
		items = []datasourceapp.BrowseItem{}
	}
	return r.Ok(map[string]any{"items": items})
}

func handleDatasourceBind(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		OwnerType    string         `json:"ownerType"`
		OwnerID      string         `json:"ownerId"`
		ConnectionID string         `json:"connectionId"`
		Purpose      string         `json:"purpose"`
		TableMap     map[string]any `json:"tableMap"`
	}
	if decodePayload(r.Payload, &p) != nil || p.TableMap == nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.bind 参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	raw, err := json.Marshal(p.TableMap)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.bind 参数无效", false)
	}
	row, err := e.datasource.Bind(ctx, datasourceapp.BindInput{
		OwnerType: p.OwnerType, OwnerID: p.OwnerID, ConnectionID: p.ConnectionID,
		Purpose: p.Purpose, TableMapJSON: string(raw),
	})
	if err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(row)
}

func handleDatasourceDisable(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || len(strings.TrimSpace(p.ID)) != 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.disable 参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.datasource.Disable(ctx, p.ID); err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(map[string]any{"id": p.ID, "state": "disabled"})
}

func handleDatasourceQuery(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ConnectionID string `json:"connectionId"`
		BindingID    string `json:"bindingId"`
		SQL          string `json:"sql"`
		MaxRows      int    `json:"maxRows"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.SQL) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "datasource.query 参数无效", false)
	}
	if e.datasource == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
	res, err := e.datasource.Query(ctx, datasourceapp.QueryInput{
		ConnectionID: p.ConnectionID, BindingID: p.BindingID, SQL: p.SQL, MaxRows: p.MaxRows,
	})
	if err != nil {
		return datasourceFailure(r, err)
	}
	return r.Ok(res)
}

func datasourceFailure(r bridge.Request, err error) bridge.Response {
	msg := datasourceapp.RedactError(err)
	switch {
	case errors.Is(err, datasourceapp.ErrPayloadInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "数据源参数无效", false)
	case errors.Is(err, datasourceapp.ErrDuplicateName):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "数据源名称已存在", false)
	case errors.Is(err, datasourceapp.ErrDuplicateBinding):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "该用途已绑定", false)
	case errors.Is(err, datasourceapp.ErrNotFound):
		return r.Fail("NOT_FOUND", "数据源不存在", false)
	case errors.Is(err, datasourceapp.ErrNotProbed), errors.Is(err, datasourceapp.ErrDatasourceNotVerified):
		return r.Fail("FORBIDDEN", "连接未探测", false)
	case errors.Is(err, datasourceapp.ErrDisabled):
		return r.Fail("FORBIDDEN", "连接已禁用", false)
	case errors.Is(err, datasourceapp.ErrStatementDenied), errors.Is(err, datasourceapp.ErrStockBindingRequired):
		return r.Fail("FORBIDDEN", msg, false)
	case errors.Is(err, datasourceapp.ErrProvisionFailed):
		return r.Fail("UNAVAILABLE", "无法自动创建数据库：请使用具有建库权限的账号（如 root），或先手动建库后重试", true)
	case errors.Is(err, datasourceapp.ErrProbeUnavailable):
		return r.Fail("UNAVAILABLE", "探测驱动未编译", true)
	case errors.Is(err, datasourceapp.ErrProbeFailed):
		return r.Fail("UNAVAILABLE", msg, true)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "数据源中心暂时不可用", true)
	}
}
