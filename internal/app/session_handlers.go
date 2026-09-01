package app

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

const sessionMutationActor = "desktop-host"

type sessionDraftReclaimer interface {
	ReclaimEmptyDrafts(ctx context.Context, projectID string, need int) (int, error)
}

type sessionDTO struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	Title     string         `json:"title"`
	Pinned    bool           `json:"pinned"`
	Status    session.Status `json:"status"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Version   int64          `json:"version"`
}

func newSessionDTO(v session.Session) sessionDTO {
	return sessionDTO{v.ID, v.ProjectID, v.Title, v.Pinned, v.Status, v.CreatedAt, v.UpdatedAt, v.Version}
}
func sessionServiceAvailable(service SessionService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Pointer || !v.IsNil()
}
func validCanonicalULID(v string) bool {
	u, e := ulid.ParseStrict(v)
	return e == nil && u.String() == v && v[0] <= '7'
}

func handleSessionCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.create 参数无效", false)
	}
	if !sessionServiceAvailable(e.sessions) {
		return r.Fail("STORAGE_UNAVAILABLE", "会话数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	title, err := session.NormalizeTitle(p.Title)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.create 参数无效", false)
	}
	created, err := e.sessions.Create(ctx, r.IdempotencyKey, sessionMutationActor, p, session.Session{ProjectID: p.ProjectID, Title: title})
	if errors.Is(err, sessionapp.ErrSessionCapacityReached) {
		if reclaimer, ok := e.sessions.(sessionDraftReclaimer); ok {
			if n, recErr := reclaimer.ReclaimEmptyDrafts(ctx, p.ProjectID, 100); recErr == nil && n > 0 {
				created, err = e.sessions.Create(ctx, r.IdempotencyKey, sessionMutationActor, p, session.Session{ProjectID: p.ProjectID, Title: title})
			}
		}
	}
	if err != nil {
		return sessionFailure(r, err)
	}
	if _, err := e.sessionOutputDir(created.ID); err != nil {
		// Non-fatal: folder will be created on first tool use.
		_ = err
	}
	return r.Ok(newSessionDTO(created))
}
func handleSessionList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.list 参数无效", false)
	}
	if !sessionServiceAvailable(e.sessions) {
		return r.Fail("STORAGE_UNAVAILABLE", "会话数据暂时不可用", true)
	}
	items, err := e.sessions.List(ctx, session.Filter{ProjectID: p.ProjectID})
	if err != nil {
		return sessionFailure(r, err)
	}
	items = e.ordinaryChatSessions(ctx, items)
	dtos := make([]sessionDTO, len(items))
	for i := range items {
		dtos[i] = newSessionDTO(items[i])
	}
	return r.Ok(struct {
		Items []sessionDTO `json:"items"`
	}{dtos})
}
func handleSessionUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Pinned  bool   `json:"pinned"`
		Version int64  `json:"version"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.update 参数无效", false)
	}
	if !sessionServiceAvailable(e.sessions) {
		return r.Fail("STORAGE_UNAVAILABLE", "会话数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	title, err := session.NormalizeTitle(p.Title)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.update 参数无效", false)
	}
	updated, err := e.sessions.Update(ctx, r.IdempotencyKey, sessionMutationActor, p, p.ID, p.Version, title, p.Pinned)
	if err != nil {
		return sessionFailure(r, err)
	}
	return r.Ok(newSessionDTO(updated))
}
func sessionFailure(r bridge.Request, err error) bridge.Response {

	switch {
	case errors.Is(err, sessionapp.ErrIdempotencyKeyRequired):
		return r.Fail("IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, sessionapp.ErrIdempotencyConflict):
		return r.Fail("IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, sessionapp.ErrProjectNotFound):
		return r.Fail("PROJECT_NOT_FOUND", "项目不存在", false)
	case errors.Is(err, sessionapp.ErrSessionCapacityReached):
		return r.Fail("SESSION_CAPACITY_REACHED", "项目会话数量已达到上限", false)
	case errors.Is(err, sessionapp.ErrSessionNotFound):
		return r.Fail("SESSION_NOT_FOUND", "会话不存在", false)
	case errors.Is(err, sessionapp.ErrSessionVersionConflict):
		return r.Fail("VERSION_CONFLICT", "会话版本冲突", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "会话数据暂时不可用", true)
	}
}

func handleSessionDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.delete 参数无效", false)
	}
	if !sessionServiceAvailable(e.sessions) {
		return r.Fail("STORAGE_UNAVAILABLE", "会话数据暂时不可用", true)
	}
	if err := e.sessions.Delete(ctx, p.ID); err != nil {
		return sessionFailure(r, err)
	}
	return r.Ok(map[string]any{"deleted": true, "id": p.ID})
}

func handleSessionExpertsGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.experts.get 参数无效", false)
	}
	if e.sessionExperts == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "会话专家挂载暂时不可用", true)
	}
	ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, p.SessionID)
	if err != nil {
		return sessionFailure(r, err)
	}
	if ids == nil {
		ids = []string{}
	}
	return r.Ok(map[string]any{"expertIds": ids})
}

func handleSessionExpertsSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string   `json:"sessionId"`
		ExpertIDs []string `json:"expertIds"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) || p.ExpertIDs == nil || len(p.ExpertIDs) > 8 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.experts.set 参数无效", false)
	}
	for _, id := range p.ExpertIDs {
		if !validCanonicalULID(id) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "session.experts.set 参数无效", false)
		}
	}
	if e.sessionExperts == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "会话专家挂载暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.sessionExperts.ReplaceSessionExpertIDs(ctx, p.SessionID, p.ExpertIDs); err != nil {
		return sessionFailure(r, err)
	}
	ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, p.SessionID)
	if err != nil {
		return sessionFailure(r, err)
	}
	if ids == nil {
		ids = []string{}
	}
	return r.Ok(map[string]any{"expertIds": ids})
}
