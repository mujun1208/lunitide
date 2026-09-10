package app

import (
	"context"
	"errors"
	"strings"
	"unicode"

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
	for i := range items {
		items[i] = localizePackRecord(items[i])
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
	return r.Ok(localizePackRecord(record))
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
	return r.Ok(localizePackRecord(record))
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
	return r.Fail(code, packFailureMessage(err), retry)
}

func packFailureMessage(err error) string {
	if err == nil {
		return "能力包操作失败"
	}
	switch {
	case errors.Is(err, capabilitypack.ErrNotFound):
		return "能力包不存在"
	case errors.Is(err, capabilitypack.ErrConflict):
		return "能力包清单或操作已变化，请刷新后再试"
	case errors.Is(err, capabilitypack.ErrUnavailable):
		return "能力包服务暂时不可用"
	}
	return localizePackRecordError(err.Error())
}

func localizePackRecord(record capabilitypack.Record) capabilitypack.Record {
	record.Error = localizePackRecordError(record.Error)
	return record
}

func localizePackRecordError(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	switch {
	case strings.Contains(msg, "is not installed"):
		return "权限开关尚未安装"
	case strings.Contains(msg, "is not built in"):
		return "权限开关不是内置能力"
	case strings.Contains(msg, "template unknown"):
		return "技能模板不存在"
	case strings.Contains(msg, "unknown MCP preset"):
		return "未知的 MCP 预设"
	case strings.HasPrefix(msg, "MCP ") && strings.Contains(msg, " needs "):
		return "MCP 需要先配置参数"
	case strings.Contains(msg, "probe failed"):
		return "能力包组件探测失败"
	case strings.Contains(msg, "capability pack not found"):
		return "能力包不存在"
	case strings.Contains(msg, "manifest or operation changed"):
		return "能力包清单或操作已变化，请刷新后再试"
	case strings.Contains(msg, "service unavailable"):
		return "能力包服务暂时不可用"
	}
	for _, r := range msg {
		if unicode.Is(unicode.Han, r) {
			return msg
		}
	}
	return "能力包操作失败，可继续或撤下"
}
