package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/skillapp"
)

type SkillService interface {
	Get(context.Context, string) (*skill.Skill, error)
	List(context.Context, skill.SkillStatus) ([]skill.Skill, error)
	Match(context.Context, string) ([]skill.SkillMatch, error)
	Create(context.Context, skill.Skill) (skill.Skill, error)
	InstallFromCatalog(context.Context, string) (skill.Skill, error)
	UpdateFields(context.Context, string, *string, *string, *string, *string, []skill.PermissionLevel, *string, int64) (*skill.Skill, error)
	Delete(context.Context, string) error
	Publish(context.Context, string) error
	Deprecate(context.Context, string) error
	Disable(context.Context, string) error
	Invoke(context.Context, string, string, string, string) (skillapp.Invocation, error)
	Execute(context.Context, string, string, bool) (skillapp.Execution, error)
}

func handleSkillInvoke(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{ SkillID, SessionID, Input, ExecutionMode string }
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SkillID) || !validCanonicalULID(p.SessionID) || strings.TrimSpace(p.Input) == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.invoke 参数无效", false)
	}
	inv, err := e.skills.Invoke(ctx, p.SkillID, p.SessionID, p.Input, p.ExecutionMode)
	if err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"invocationId": inv.ID, "skillId": inv.SkillID, "skillVersion": inv.SkillVersion, "inputDigest": inv.InputDigest, "manifestDigest": inv.ManifestDigest, "risk": inv.Risk, "requiresApproval": inv.RequiresApproval, "status": "invoked", "expiresAt": inv.ExpiresAt})
}

func handleSkillExecute(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		InvocationID string `json:"invocationId"`
		SessionID    string `json:"sessionId"`
		Approved     bool   `json:"approved"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.InvocationID) || !validCanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.execute 参数无效", false)
	}
	result, err := e.skills.Execute(ctx, p.InvocationID, p.SessionID, p.Approved)
	if err != nil {
		return skillFailure(r, err)
	}
	if e.messages != nil {
		if _, appendErr := e.messages.AppendAssistant(ctx, result.AuditID, "skill.execute", p.SessionID, "[技能结果 audit:"+result.AuditID+"]\n"+result.Output, messageapp.AssistantUsage{}); appendErr != nil {
			return internalBridgeFailure(r, "SKILL_RESULT_WRITE_FAILED", "技能结果无法持久化", true, appendErr)
		}
	}
	return bridge.Success(r.ID, map[string]any{"invocationId": result.InvocationID, "auditId": result.AuditID, "status": "succeeded", "output": result.Output})
}

type skillDTO struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	DisplayName      string                  `json:"displayName"`
	Description      string                  `json:"description"`
	Version          string                  `json:"version"`
	Status           skill.SkillStatus       `json:"status"`
	Permissions      []skill.PermissionLevel `json:"permissions"`
	EntryPoint       string                  `json:"entryPoint"`
	ManifestJSON     string                  `json:"manifestJson"`
	Signature        *string                 `json:"signature,omitempty"`
	PublisherID      *string                 `json:"publisherId,omitempty"`
	MinEngineVersion *string                 `json:"minEngineVersion,omitempty"`
	CreatedAt        time.Time               `json:"createdAt"`
	UpdatedAt        time.Time               `json:"updatedAt"`
}

type skillMatchDTO struct {
	Skill   skillDTO `json:"skill"`
	Score   float64  `json:"score"`
	Reason  string   `json:"reason"`
	MatchID string   `json:"matchId"`
}

func newSkillDTO(s skill.Skill) skillDTO {
	return skillDTO{
		ID:               s.ID,
		Name:             s.Name,
		DisplayName:      s.DisplayName,
		Description:      s.Description,
		Version:          s.Version,
		Status:           s.Status,
		Permissions:      s.Permissions,
		EntryPoint:       s.EntryPoint,
		ManifestJSON:     s.ManifestJSON,
		Signature:        s.Signature,
		PublisherID:      s.PublisherID,
		MinEngineVersion: s.MinEngineVersion,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func newSkillMatchDTO(m skill.SkillMatch) skillMatchDTO {
	return skillMatchDTO{
		Skill:   newSkillDTO(m.Skill),
		Score:   m.Score,
		Reason:  m.Reason,
		MatchID: m.MatchID,
	}
}

func skillServiceAvailable(service SkillService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

func handleSkillGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.get 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	s, err := e.skills.Get(ctx, p.ID)
	if err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, newSkillDTO(*s))
}

func handleSkillCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name             string                  `json:"name"`
		DisplayName      string                  `json:"displayName"`
		Description      string                  `json:"description"`
		Version          string                  `json:"version"`
		Permissions      []skill.PermissionLevel `json:"permissions"`
		EntryPoint       string                  `json:"entryPoint"`
		ManifestJSON     string                  `json:"manifestJson"`
		MinEngineVersion *string                 `json:"minEngineVersion,omitempty"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.DisplayName) == "" || strings.TrimSpace(p.Version) == "" || len(p.Permissions) == 0 || strings.TrimSpace(p.EntryPoint) == "" || strings.TrimSpace(p.ManifestJSON) == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.create 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	s, err := e.skills.Create(ctx, skill.Skill{
		Name:             p.Name,
		DisplayName:      p.DisplayName,
		Description:      p.Description,
		Version:          p.Version,
		Permissions:      p.Permissions,
		EntryPoint:       p.EntryPoint,
		ManifestJSON:     p.ManifestJSON,
		MinEngineVersion: p.MinEngineVersion,
	})
	if err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, newSkillDTO(s))
}

func handleSkillUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID               string                  `json:"id"`
		DisplayName      *string                 `json:"displayName,omitempty"`
		Description      *string                 `json:"description,omitempty"`
		Permissions      []skill.PermissionLevel `json:"permissions,omitempty"`
		EntryPoint       *string                 `json:"entryPoint,omitempty"`
		ManifestJSON     *string                 `json:"manifestJson,omitempty"`
		MinEngineVersion *string                 `json:"minEngineVersion,omitempty"`
		ExpectedVersion  int64                   `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.update 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	s, err := e.skills.UpdateFields(ctx, p.ID, p.DisplayName, p.Description, p.EntryPoint, p.ManifestJSON, p.Permissions, p.MinEngineVersion, p.ExpectedVersion)
	if err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, newSkillDTO(*s))
}

func handleSkillDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.delete 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	if err := e.skills.Delete(ctx, p.ID); err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"deleted": true})
}

func handleSkillList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Status skill.SkillStatus `json:"status"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.list 参数无效", false)
	}
	if p.Status != "" && p.Status != skill.SkillStatusDraft && p.Status != skill.SkillStatusPublished && p.Status != skill.SkillStatusDeprecated && p.Status != skill.SkillStatusDisabled {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.list 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	items, err := e.skills.List(ctx, p.Status)
	if err != nil {
		return skillFailure(r, err)
	}
	dtos := make([]skillDTO, len(items))
	for i := range items {
		dtos[i] = newSkillDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []skillDTO `json:"items"`
	}{Items: dtos})
}

func handleSkillMatch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Query string `json:"query"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Query) == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.match 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	items, err := e.skills.Match(ctx, p.Query)
	if err != nil {
		return skillFailure(r, err)
	}
	dtos := make([]skillMatchDTO, len(items))
	for i := range items {
		dtos[i] = newSkillMatchDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []skillMatchDTO `json:"items"`
	}{Items: dtos})
}

func handleSkillPublish(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.publish 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	if err := e.skills.Publish(ctx, p.ID); err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"published": true})
}

func handleSkillDeprecate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.deprecate 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	if err := e.skills.Deprecate(ctx, p.ID); err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"deprecated": true})
}

func handleSkillDisable(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.disable 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	if err := e.skills.Disable(ctx, p.ID); err != nil {
		return skillFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"disabled": true})
}

func skillFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, skillapp.ErrInvocationNotFound):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_INVOCATION_NOT_FOUND", "技能调用不存在或不属于当前会话", false)
	case errors.Is(err, skillapp.ErrInvocationConsumed):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_INVOCATION_CONSUMED", "技能调用已消费", false)
	case errors.Is(err, skillapp.ErrInvocationExpired):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_INVOCATION_EXPIRED", "技能调用已过期", false)
	case errors.Is(err, skillapp.ErrInvocationChanged):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_INVOCATION_CHANGED", "技能在调用后已变化", false)
	case errors.Is(err, skillapp.ErrApprovalRequired):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_APPROVAL_REQUIRED", "技能执行需要批准", false)
	case errors.Is(err, skillapp.ErrExecutionForbidden):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_EXECUTION_FORBIDDEN", "当前执行模式禁止技能执行", false)
	case errors.Is(err, skillapp.ErrUnknownEntryPoint):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_ENTRY_POINT_DENIED", "技能入口不在 Engine builtin allowlist", false)
	case errors.Is(err, skillapp.ErrSkillNotFound):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_NOT_FOUND", "技能不存在", false)
	case errors.Is(err, skillapp.ErrInvalidTransition):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_INVALID_TRANSITION", "技能状态转换无效", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
}

// handleSkillCatalogList answers the product-shipped skill template catalog
// (P3-2 local market). Each entry reports whether this name+version is
// already materialized locally so the UI can flip install → installed.
func handleSkillCatalogList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	existing, err := e.skills.List(ctx, "")
	if err != nil {
		return skillFailure(r, err)
	}
	have := make(map[string]bool, len(existing))
	for _, s := range existing {
		have[s.Name+"@"+s.Version] = true
	}
	type entry struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		DisplayName string   `json:"displayName"`
		Description string   `json:"description"`
		Category    string   `json:"category"`
		Version     string   `json:"version"`
		Permissions []string `json:"permissions"`
		Installed   bool     `json:"installed"`
	}
	items := make([]entry, 0, len(skillapp.Catalog()))
	for _, t := range skillapp.Catalog() {
		perms := make([]string, 0, len(t.Permissions))
		for _, p := range t.Permissions {
			perms = append(perms, string(p))
		}
		items = append(items, entry{ID: t.ID, Name: t.Name, DisplayName: t.DisplayName,
			Description: t.Description, Category: t.Category, Version: t.Version,
			Permissions: perms, Installed: have[t.Name+"@"+t.Version]})
	}
	return bridge.Success(r.ID, map[string]any{"items": items})
}

// handleSkillInstall materializes one catalog template as a local draft
// skill through the normal create pipeline (permissions review + publish
// gate still apply).
func handleSkillInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TemplateID string `json:"templateId"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.TemplateID) == "" || len(p.TemplateID) > 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "skill.install 参数无效", false)
	}
	if !skillServiceAvailable(e.skills) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
	s, err := e.skills.InstallFromCatalog(ctx, p.TemplateID)
	switch {
	case err == nil:
		return bridge.Success(r.ID, map[string]any{"skillId": s.ID, "name": s.Name, "status": string(s.Status)})
	case errors.Is(err, skillapp.ErrTemplateUnknown):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_TEMPLATE_NOT_FOUND", "模板不存在", false)
	case errors.Is(err, skillapp.ErrTemplateInstalled):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_TEMPLATE_INSTALLED", "该模板版本已安装", false)
	default:
		return skillFailure(r, err)
	}
}
