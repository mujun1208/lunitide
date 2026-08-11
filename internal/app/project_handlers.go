package app

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
)

const projectMutationActor = "desktop-host"

// projectDTO is the sole public projection of the Project domain model.
// Domain fields added later cannot leak through create, list, or replay.
type projectDTO struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Status    project.Status `json:"status"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Version   int64          `json:"version"`
}

func newProjectDTO(p project.Project) projectDTO {
	return projectDTO{ID: p.ID, Name: p.Name, Status: p.Status, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Version: p.Version}
}

func projectServiceAvailable(service ProjectService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

func handleProjectCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name string `json:"name"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.create 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	name, err := project.NormalizeName(p.Name)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.create 参数无效", false)
	}
	created, err := e.projects.Create(ctx, r.IdempotencyKey, projectMutationActor, p, project.Project{Name: name, Status: project.StatusActive})
	if err != nil {
		return projectFailure(r, err)
	}
	return bridge.Success(r.ID, newProjectDTO(created))
}

func handleProjectList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Status project.Status `json:"status"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.Status != "" && p.Status != project.StatusActive && p.Status != project.StatusArchived) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.list 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	items, err := e.projects.List(ctx, project.Filter{Status: p.Status})
	if err != nil {
		return projectFailure(r, err)
	}
	if len(items) > 100 {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", false)
	}
	dtos := make([]projectDTO, len(items))
	for i := range items {
		dtos[i] = newProjectDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []projectDTO `json:"items"`
	}{Items: dtos})
}

func projectFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, projectapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, projectapp.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, projectapp.ErrProjectCapacityReached):
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_CAPACITY_REACHED", "项目数量已达到上限", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
}

func handleProjectDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.delete 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if err := e.projects.Delete(ctx, p.ID); err != nil {
		return projectFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"deleted": true, "id": p.ID})
}
