package app

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/stageapp"
)

const stageMutationActor = "desktop-host"

type stageDTO struct {
	ID        string      `json:"id"`
	ProjectID string      `json:"projectId"`
	Phase     int         `json:"phase"`
	Title     string      `json:"title"`
	Status    stage.Status `json:"status"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
	Version   int64       `json:"version"`
}

func newStageDTO(v stage.Stage) stageDTO {
	return stageDTO{v.ID, v.ProjectID, v.Phase, v.Title, v.Status, v.CreatedAt, v.UpdatedAt, v.Version}
}

func stageServiceAvailable(service StageService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

func validStageStatus(status stage.Status) bool {
	switch status {
	case stage.StatusNotStarted, stage.StatusInProgress, stage.StatusWaitingReview, stage.StatusApproved, stage.StatusCompleted,
		stage.StatusRejected, stage.StatusStale, stage.StatusPaused, stage.StatusBlocked, stage.StatusCancelled:
		return true
	default:
		return false
	}
}

func handleStageCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Phase     int    `json:"phase"`
		Title     string `json:"title"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || p.Phase < 1 || p.Phase > 9 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "stage.create 参数无效", false)
	}
	if !stageServiceAvailable(e.stages) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "阶段数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	title, err := stage.NormalizeTitle(p.Title)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "stage.create 参数无效", false)
	}
	created, err := e.stages.Create(ctx, r.IdempotencyKey, stageMutationActor, p, stage.Stage{ProjectID: p.ProjectID, Phase: p.Phase, Title: title})
	if err != nil {
		return stageFailure(r, err)
	}
	return bridge.Success(r.ID, newStageDTO(created))
}

func handleStageList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "stage.list 参数无效", false)
	}
	if !stageServiceAvailable(e.stages) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "阶段数据暂时不可用", true)
	}
	items, err := e.stages.List(ctx, stage.Filter{ProjectID: p.ProjectID})
	if err != nil {
		return stageFailure(r, err)
	}
	dtos := make([]stageDTO, len(items))
	for i := range items {
		dtos[i] = newStageDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []stageDTO `json:"items"`
	}{dtos})
}

func handleStageUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID       string       `json:"projectId"`
		ID              string       `json:"id"`
		Status          stage.Status `json:"status"`
		ExpectedVersion int64        `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 || !validStageStatus(p.Status) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "stage.update 参数无效", false)
	}
	if !stageServiceAvailable(e.stages) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "阶段数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	updated, err := e.stages.Update(ctx, r.IdempotencyKey, stageMutationActor, p, stageapp.UpdateInput{
		ProjectID: p.ProjectID, ID: p.ID, Status: p.Status, ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		return stageFailure(r, err)
	}
	return bridge.Success(r.ID, newStageDTO(updated))
}

func stageFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, stageapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, stageapp.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, stageapp.ErrProjectNotFound):
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_NOT_FOUND", "项目不存在", false)
	case errors.Is(err, stageapp.ErrStagePhaseConflict):
		return bridge.Failure(r.ID, r.TraceID, "STAGE_PHASE_CONFLICT", "阶段已存在", false)
	case errors.Is(err, stageapp.ErrStageNotFound):
		return bridge.Failure(r.ID, r.TraceID, "STAGE_NOT_FOUND", "阶段不存在", false)
	case errors.Is(err, stageapp.ErrStageVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "STAGE_VERSION_CONFLICT", "阶段已被其他操作修改，请刷新后重试", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "阶段数据暂时不可用", true)
	}
}
