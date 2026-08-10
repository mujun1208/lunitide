package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
)

type SkillService interface {
	Get(context.Context, string) (*skill.Skill, error)
	List(context.Context, skill.SkillStatus) ([]skill.Skill, error)
	Match(context.Context, string) ([]skill.SkillMatch, error)
	Publish(context.Context, string) error
	Deprecate(context.Context, string) error
	Disable(context.Context, string) error
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
	case errors.Is(err, skillapp.ErrSkillNotFound):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_NOT_FOUND", "技能不存在", false)
	case errors.Is(err, skillapp.ErrInvalidTransition):
		return bridge.Failure(r.ID, r.TraceID, "SKILL_INVALID_TRANSITION", "技能状态转换无效", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "技能数据暂时不可用", true)
	}
}
