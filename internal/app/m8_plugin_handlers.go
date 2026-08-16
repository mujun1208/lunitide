package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// M8 FR-18 handlers (T-8.9.x): plugin.install / plugin.list /
// plugin.toggle / plugin.upgrade / plugin.uninstall / plugin.dev.create /
// plugin.market.search / plugin.market.detail.
//
// Error mapping follows the M8 wire contract (04 错误矩阵 M8-035~041).
// The personal market read-only consumes the public catalogue; with no
// catalogue source wired the local engine answers an empty result set and
// detail answers not-found.

func handlePluginInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Origin          string          `json:"origin"`
		Source          string          `json:"source"`
		PermissionGrant json.RawMessage `json:"permissionGrant"`
		RequestID       string          `json:"requestId"`
		Actor           string          `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		(p.Origin != "market" && p.Origin != "local" && p.Origin != "dev") ||
		len(p.Source) < 1 || len(p.Source) > 512 || len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.install 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	res, err := e.m8plugin.Install(ctx, m8app.InstallInput{
		Origin: p.Origin, Source: p.Source, PermissionGrant: p.PermissionGrant,
		RequestID: p.RequestID, Actor: p.Actor,
	})
	if err != nil {
		return m8PluginFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handlePluginList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Kind  string `json:"kind"`
		State string `json:"state"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		(p.Kind != "" && p.Kind != "mcp" && p.Kind != "skill" && p.Kind != "workflow" &&
			p.Kind != "template" && p.Kind != "tool" && p.Kind != "agent-pack") ||
		(p.State != "" && (len(p.State) < 1 || len(p.State) > 32)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.list 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	res, err := e.m8plugin.List(ctx, p.Kind, p.State)
	if err != nil {
		return m8PluginFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handlePluginToggle(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		InstallID string `json:"installId"`
		Enabled   bool   `json:"enabled"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.InstallID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.toggle 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	res, err := e.m8plugin.Toggle(ctx, m8app.ToggleInput{
		InstallID: p.InstallID, Enabled: p.Enabled, Actor: p.Actor,
	})
	if err != nil {
		return m8PluginFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handlePluginUpgrade(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		InstallID       string          `json:"installId"`
		TargetSemver    string          `json:"targetSemver"`
		PermissionGrant json.RawMessage `json:"permissionGrant"`
		Actor           string          `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.InstallID) ||
		(p.TargetSemver != "" && (len(p.TargetSemver) < 1 || len(p.TargetSemver) > 32)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.upgrade 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	res, err := e.m8plugin.Upgrade(ctx, m8app.UpgradeInput{
		InstallID: p.InstallID, TargetSemver: p.TargetSemver,
		PermissionGrant: p.PermissionGrant, Actor: p.Actor,
	})
	if err != nil {
		return m8PluginFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handlePluginUninstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		InstallID    string `json:"installId"`
		ConfirmToken string `json:"confirmToken"`
		Actor        string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.InstallID) ||
		len(p.ConfirmToken) != 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.uninstall 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	res, err := e.m8plugin.Uninstall(ctx, m8app.UninstallInput{
		InstallID: p.InstallID, ConfirmToken: p.ConfirmToken, Actor: p.Actor,
	})
	if err != nil {
		return m8PluginFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handlePluginDevCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		WorkspaceID string         `json:"workspaceId"`
		Manifest    map[string]any `json:"manifest"`
		Entrypoint  string         `json:"entrypoint"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.WorkspaceID) < 1 || len(p.WorkspaceID) > 128 ||
		p.Manifest == nil || len(p.Entrypoint) < 1 || len(p.Entrypoint) > 512 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.dev.create 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	res, err := e.m8plugin.DevCreate(ctx, m8app.DevCreateInput{
		WorkspaceID: p.WorkspaceID, Manifest: p.Manifest, Entrypoint: p.Entrypoint,
	})
	if err != nil {
		return m8PluginFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handlePluginMarketSearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Query  string `json:"query"`
		Kind   string `json:"kind"`
		Cursor string `json:"cursor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.Query) < 1 || len(p.Query) > 200 ||
		(p.Cursor != "" && len(p.Cursor) > 256) ||
		(p.Kind != "" && !m8core.ValidPluginKind(p.Kind)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.market.search 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	list, err := e.m8plugin.List(ctx, p.Kind, "")
	if err != nil {
		return m8PluginFailure(r, err)
	}
	// Degraded read-only local catalogue (T-8.9.x P3 "不可用降级只读本地"):
	// with no remote market source wired, the search answers from the local
	// install projection so the settings flow stays usable offline.
	needle := strings.ToLower(p.Query)
	items := make([]map[string]any, 0, len(list.Plugins))
	for _, plugin := range list.Plugins {
		haystack := strings.ToLower(plugin.PluginID + " " + plugin.Publisher + " " + plugin.Kind + " " + plugin.Origin)
		if !strings.Contains(haystack, needle) {
			continue
		}
		publisher := plugin.Publisher
		if publisher == "" {
			publisher = "local"
		}
		digestSum := sha256.Sum256([]byte("plugin-market-local\x00" + plugin.InstallID + "\x00" + plugin.PluginID + "\x00" + plugin.Semver))
		items = append(items, map[string]any{
			"itemId":      plugin.InstallID,
			"pluginId":    plugin.PluginID,
			"name":        plugin.PluginID,
			"publisher":   publisher,
			"description": "本地已安装（市场源未接入，只读降级视图）· 状态 " + plugin.State,
			"kind":        plugin.Kind,
			"semver":      plugin.Semver,
			"digest":      hex.EncodeToString(digestSum[:]),
			"signed":      plugin.Origin == "market",
		})
		if len(items) >= 50 {
			break
		}
	}
	return bridge.Success(r.ID, map[string]any{"items": items})
}

func handlePluginMarketDetail(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ItemID string `json:"itemId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ItemID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "plugin.market.detail 参数无效", false)
	}
	if e.m8plugin == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
	list, err := e.m8plugin.List(ctx, "", "")
	if err != nil {
		return m8PluginFailure(r, err)
	}
	// Degraded read-only local detail: the local install projection stands in
	// for the unwired remote catalogue entry.
	for _, plugin := range list.Plugins {
		if plugin.InstallID != p.ItemID {
			continue
		}
		publisher := plugin.Publisher
		if publisher == "" {
			publisher = "local"
		}
		return bridge.Success(r.ID, map[string]any{
			"manifest": map[string]any{
				"pluginId":  plugin.PluginID,
				"semver":    plugin.Semver,
				"kind":      plugin.Kind,
				"origin":    plugin.Origin,
				"publisher": publisher,
			},
			"permissions": []string{},
			"requires":    map[string]any{},
			"signature":   map[string]any{"signed": plugin.Origin == "market", "source": "local-degraded"},
			"downloads":   0,
		})
	}
	return bridge.Failure(r.ID, r.TraceID, "BRIDGE_NOT_FOUND", "市场条目不存在或市场源未接入", false)
}

// m8PluginFailure maps the FR-18 error family onto the M8 code matrix
// (M8-035~041 plus the shared family).
func m8PluginFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrPluginSignatureInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M8-035", "签名或包校验无效，已隔离零注册", false)
	case errors.Is(err, m8app.ErrPluginManifestInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M8-036", "插件 manifest 非法", false)
	case errors.Is(err, m8app.ErrPluginPermissionDenied):
		return bridge.Failure(r.ID, r.TraceID, "M8-037", "插件权限超出授权白名单", false)
	case errors.Is(err, m8app.ErrPluginProbeFailed):
		return bridge.Failure(r.ID, r.TraceID, "M8-038", "插件探活失败，可重试", true)
	case errors.Is(err, m8app.ErrPluginPermissionExpansion):
		return bridge.Failure(r.ID, r.TraceID, "M8-039", "升级权限扩张，已隔离待审查", false)
	case errors.Is(err, m8app.ErrBindingInactive):
		return bridge.Failure(r.ID, r.TraceID, "M8-040", "能力绑定非 active，拒绝且零副作用", false)
	case errors.Is(err, m8app.ErrPluginUninstallConflict):
		return bridge.Failure(r.ID, r.TraceID, "M8-041", "卸载链失败，整体回滚", false)
	case errors.Is(err, m8app.ErrInstallNotFound),
		errors.Is(err, m8app.ErrBundleNotFound):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_NOT_FOUND", "插件资源不存在", false)
	case errors.Is(err, m8app.ErrInstallStateInvalid):
		return bridge.Failure(r.ID, r.TraceID, "PLUGIN_STATE_INVALID", "插件安装状态不允许该操作", false)
	case errors.Is(err, m8app.ErrPayloadInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "载荷非法", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "插件服务暂时不可用", true)
	}
}
