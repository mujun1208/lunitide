package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

var capabilityRoleOrder = []string{"chat", "flash", "vision", "embed", "judge", "gui"}

var errJudgeEqChat = errors.New("judge equals chat without allowJudgeEqChat")
var errRoleKindMismatch = errors.New("capability role kind mismatch")

type CapabilityRoleStore interface {
	ListCapabilityRoles(context.Context) ([]sqlite.CapabilityRoleBinding, error)
	ReplaceCapabilityRoles(context.Context, []sqlite.CapabilityRoleBinding) error
}

type capabilityRoleDTO struct {
	Role             string `json:"role"`
	ProviderID       string `json:"providerId,omitempty"`
	ModelID          string `json:"modelId,omitempty"`
	AllowJudgeEqChat bool   `json:"allowJudgeEqChat"`
}

func emptyCapabilityRoles() []capabilityRoleDTO {
	out := make([]capabilityRoleDTO, 0, len(capabilityRoleOrder))
	for _, role := range capabilityRoleOrder {
		out = append(out, capabilityRoleDTO{Role: role})
	}
	return out
}

func mergeCapabilityRoles(rows []sqlite.CapabilityRoleBinding) []capabilityRoleDTO {
	byRole := map[string]sqlite.CapabilityRoleBinding{}
	for _, row := range rows {
		byRole[row.Role] = row
	}
	out := emptyCapabilityRoles()
	for i, role := range capabilityRoleOrder {
		if row, ok := byRole[role]; ok {
			out[i].ProviderID = row.ProviderID
			out[i].ModelID = row.ModelID
			out[i].AllowJudgeEqChat = row.AllowJudgeEqChat
		}
	}
	return out
}

func handleCapabilityRolesGet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if len(request.Payload) > 0 && string(request.Payload) != "{}" && string(request.Payload) != "null" {
		var empty map[string]any
		if decodePayload(request.Payload, &empty) != nil || len(empty) > 0 {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "capability.roles.get 参数无效", false)
		}
	}
	if e.capabilityRoles == nil {
		return request.Ok(map[string]any{"roles": emptyCapabilityRoles()})
	}
	rows, err := e.capabilityRoles.ListCapabilityRoles(ctx)
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "能力路由暂时不可用", true)
	}
	return request.Ok(map[string]any{"roles": mergeCapabilityRoles(rows)})
}

func handleCapabilityRolesSet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload struct {
		Roles []capabilityRoleDTO `json:"roles"`
	}
	if decodePayload(request.Payload, &payload) != nil || len(payload.Roles) != 6 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "capability.roles.set 参数无效", false)
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	seen := map[string]bool{}
	for _, row := range payload.Roles {
		if seen[row.Role] {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "capability.roles.set 参数无效", false)
		}
		seen[row.Role] = true
	}
	for _, role := range capabilityRoleOrder {
		if !seen[role] {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "capability.roles.set 参数无效", false)
		}
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "供应商数据暂时不可用", true)
	}
	if err := validateCapabilityRoleSet(payload.Roles, items); err != nil {
		if errors.Is(err, errJudgeEqChat) {
			return request.Fail("CAPABILITY_ROLE_JUDGE_EQ_CHAT", "judge 与 chat 相同必须勾选允许", false)
		}
		if errors.Is(err, errRoleKindMismatch) {
			return request.Fail("CAPABILITY_ROLE_KIND_MISMATCH", "角色绑定的模型类型不符", false)
		}
		return request.Fail("BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	if e.capabilityRoles == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "能力路由暂时不可用", true)
	}
	now := time.Now().UTC()
	rows := make([]sqlite.CapabilityRoleBinding, 0, 6)
	for _, row := range payload.Roles {
		rows = append(rows, sqlite.CapabilityRoleBinding{
			Role: row.Role, ProviderID: row.ProviderID, ModelID: row.ModelID,
			AllowJudgeEqChat: row.AllowJudgeEqChat, UpdatedAt: now,
		})
	}
	if err := e.capabilityRoles.ReplaceCapabilityRoles(ctx, rows); err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "能力路由写入失败", true)
	}
	return request.Ok(map[string]any{"roles": mergeCapabilityRoles(rows)})
}

func validateCapabilityRoleSet(roles []capabilityRoleDTO, items []provider.Provider) error {
	var chatModel string
	byKey := map[string]provider.Model{}
	for _, p := range items {
		for _, m := range p.Models {
			byKey[p.ID+"\x00"+m.ModelID] = m
		}
	}
	for _, row := range roles {
		bound := row.ProviderID != "" || row.ModelID != ""
		if (row.ProviderID == "") != (row.ModelID == "") {
			return errors.New("providerId and modelId must be set together")
		}
		if !bound {
			continue
		}
		model, ok := byKey[row.ProviderID+"\x00"+row.ModelID]
		if !ok {
			return errors.New("bound model is not in the current catalog")
		}
		if !roleKindAllowed(row.Role, model.EffectiveKind(), model.SupportsVision) {
			return errRoleKindMismatch
		}
		if row.Role == "chat" {
			chatModel = row.ModelID
		}
	}
	for _, row := range roles {
		if row.Role != "judge" || row.ModelID == "" || chatModel == "" {
			continue
		}
		if row.ModelID == chatModel && !row.AllowJudgeEqChat {
			return errJudgeEqChat
		}
	}
	return nil
}

func roleKindAllowed(role string, kind provider.Kind, supportsVision bool) bool {
	switch role {
	case "chat", "flash", "judge":
		return kind == provider.KindLLM
	case "vision":
		return kind == provider.KindVision || (kind == provider.KindLLM && supportsVision)
	case "embed":
		return kind == provider.KindEmbedding
	case "gui":
		return kind == provider.KindGUI
	default:
		return false
	}
}

func (e *Engine) resolveRoleRow(ctx context.Context, role string) (sqlite.CapabilityRoleBinding, bool) {
	if e == nil || e.capabilityRoles == nil {
		return sqlite.CapabilityRoleBinding{}, false
	}
	rows, err := e.capabilityRoles.ListCapabilityRoles(ctx)
	if err != nil {
		return sqlite.CapabilityRoleBinding{}, false
	}
	for _, row := range rows {
		if row.Role == role && row.ProviderID != "" && row.ModelID != "" {
			return row, true
		}
	}
	return sqlite.CapabilityRoleBinding{}, false
}

func (e *Engine) resolveRole(ctx context.Context, role string) (providerID, modelID string) {
	row, ok := e.resolveRoleRow(ctx, role)
	if !ok {
		return "", ""
	}
	return row.ProviderID, row.ModelID
}

func (e *Engine) preferBoundCatalog(ctx context.Context, role string, catalog []provider.CatalogEntry) []provider.CatalogEntry {
	providerID, modelID := e.resolveRole(ctx, role)
	if strings.TrimSpace(modelID) == "" {
		return catalog
	}
	var out []provider.CatalogEntry
	for _, entry := range catalog {
		if (providerID == "" || entry.Provider.ID == providerID) && entry.Model.ModelID == modelID {
			out = append(out, entry)
		}
	}
	return out
}

func classifyTaskRouteWithFlash(goal, raw string) (TaskRoute, map[string]bool) {
	_ = goal
	trimmed := strings.TrimSpace(raw)
	if i := strings.IndexByte(trimmed, '{'); i >= 0 {
		trimmed = trimmed[i:]
	}
	var parsed struct {
		Route string          `json:"route"`
		Allow map[string]bool `json:"allow"`
	}
	if json.Unmarshal([]byte(trimmed), &parsed) != nil {
		return RouteUnspecified, nil
	}
	switch TaskRoute(parsed.Route) {
	case RouteR0, RouteR1, RouteR2, RouteR3, RouteR4:
		return TaskRoute(parsed.Route), parsed.Allow
	default:
		return RouteUnspecified, nil
	}
}

func (e *Engine) tryFlashClassify(ctx context.Context, goal string) (TaskRoute, map[string]bool, bool) {
	providerID, modelID := e.resolveRole(ctx, "flash")
	if strings.TrimSpace(modelID) == "" {
		return RouteUnspecified, nil, false
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return RouteUnspecified, nil, false
	}
	var hit provider.Provider
	found := false
	for _, item := range items {
		if providerID != "" && item.ID != providerID {
			continue
		}
		for _, m := range item.Models {
			if m.ModelID == modelID {
				hit = item
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found || hit.CredentialRef == "" {
		return RouteUnspecified, nil, false
	}
	var raw string
	leaseErr := e.withProviderLease(ctx, hit, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		a, adapterErr := e.adapter(op, hit)
		if adapterErr != nil {
			return adapterErr
		}
		resp, completeErr := a.Complete(op, secret, gateway.Request{
			Model: modelID, MaxTokens: 128, MaxAttempts: 1,
			Messages: []gateway.Message{
				{Role: gateway.RoleSystem, Content: `Classify the user goal into one JSON object {"route":"R0|R1|R2|R3|R4","allow":{}}. No prose.`},
				{Role: gateway.RoleUser, Content: goal},
			},
		})
		if completeErr != nil {
			return completeErr
		}
		raw = resp.Message.Content
		return nil
	})
	if leaseErr != nil {
		return RouteUnspecified, nil, false
	}
	route, allow := classifyTaskRouteWithFlash(goal, raw)
	return route, allow, true
}
