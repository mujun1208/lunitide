package app

import (
	"context"
	"errors"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/capabilitypack"
)

func handlePluginPackList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.capabilityPacks == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "能力包服务暂时不可用", true)
	}
	items, err := e.capabilityPacks.List(ctx)
	if err != nil {
		return packFailure(r, err)
	}
	return r.Ok(map[string]any{"items": items})
}
func handlePluginPackInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Spec      capabilitypack.Spec `json:"spec"`
		Repair    bool                `json:"repair"`
		Confirmed bool                `json:"confirmed"`
	}
	if decodePayload(r.Payload, &p) != nil || !p.Confirmed || p.Spec.Validate() != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "请确认能力包的技能、MCP 与权限开关清单", false)
	}
	if e.capabilityPacks == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "能力包服务暂时不可用", true)
	}
	record, err := e.capabilityPacks.Install(ctx, p.Spec, p.Repair)
	if err != nil {
		return packFailure(r, err)
	}
	return r.Ok(record)
}
func handlePluginPackUninstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PackID          string `json:"packId"`
		ExpectedVersion int64  `json:"expectedVersion"`
		Confirmed       bool   `json:"confirmed"`
	}
	if decodePayload(r.Payload, &p) != nil || p.PackID == "" || len(p.PackID) > 128 || p.ExpectedVersion < 1 || !p.Confirmed {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "撤下能力包需要确认及当前版本", false)
	}
	if e.capabilityPacks == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "能力包服务暂时不可用", true)
	}
	record, err := e.capabilityPacks.Uninstall(ctx, p.PackID, p.ExpectedVersion)
	if err != nil {
		return packFailure(r, err)
	}
	return r.Ok(record)
}
func packFailure(r bridge.Request, err error) bridge.Response {
	code := "PACK_OPERATION_FAILED"
	retry := true
	if errors.Is(err, capabilitypack.ErrConflict) {
		code = "PACK_CONFLICT"
		retry = false
	}
	if errors.Is(err, capabilitypack.ErrNotFound) {
		code = "PACK_NOT_FOUND"
		retry = false
	}
	return r.Fail(code, err.Error(), retry)
}
