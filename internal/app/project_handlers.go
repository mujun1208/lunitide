package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
)

const projectMutationActor = "desktop-host"

type projectDTO struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	ProjectCode       string         `json:"projectCode"`
	Type              project.Type   `json:"type"`
	Description       string         `json:"description"`
	Summary           string         `json:"summary"`
	Objective         string         `json:"objective"`
	Client            string         `json:"client"`
	ContractNo        string         `json:"contractNo"`
	Amount            float64        `json:"amount"`
	Budget            float64        `json:"budget"`
	PlanStart         string         `json:"planStart"`
	PlanEnd           string         `json:"planEnd"`
	Remark            string         `json:"remark"`
	CloseReason       string         `json:"closeReason,omitempty"`
	StatusBeforeClose project.Status `json:"statusBeforeClose,omitempty"`
	ReopenReason      string         `json:"reopenReason,omitempty"`
	Status            project.Status `json:"status"`
	OrgID             string         `json:"orgId,omitempty"`
	SpaceID           string         `json:"spaceId,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	Version           int64          `json:"version"`
}

func newProjectDTO(p project.Project) projectDTO {
	p.Status = project.NormalizeStatus(p.Status)
	return projectDTO{
		ID: p.ID, Name: p.Name, ProjectCode: p.ProjectCode, Type: p.Type,
		Description: p.Description, Summary: p.Summary, Objective: p.Objective,
		Client: p.Client, ContractNo: p.ContractNo, Amount: p.Amount, Budget: p.Budget,
		PlanStart: p.PlanStart, PlanEnd: p.PlanEnd, Remark: p.Remark,
		CloseReason: p.CloseReason, StatusBeforeClose: p.StatusBeforeClose,
		ReopenReason: p.ReopenReason, Status: p.Status, OrgID: p.OrgID, SpaceID: p.SpaceID,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Version: p.Version,
	}
}

func (e *Engine) boundOrgID(ctx context.Context) string {
	if e.m9org == nil {
		return ""
	}
	res, err := e.m9org.Summary(ctx)
	if err != nil {
		return ""
	}
	return res.BoundOrgID
}

func projectServiceAvailable(service ProjectService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

type projectCreatePayload struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Summary     string  `json:"summary"`
	Objective   string  `json:"objective"`
	Client      string  `json:"client"`
	ContractNo  string  `json:"contractNo"`
	Amount      float64 `json:"amount"`
	Budget      float64 `json:"budget"`
	PlanStart   string  `json:"planStart"`
	PlanEnd     string  `json:"planEnd"`
	Remark      string  `json:"remark"`
}

func clampText(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if r := []rune(raw); len(r) > max {
		return strings.TrimSpace(string(r[:max]))
	}
	return raw
}

func handleProjectCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectCreatePayload
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
	candidate := project.Project{
		Name: name, Type: project.Type(p.Type),
		Description: clampText(p.Description, 2000), Summary: clampText(p.Summary, 500),
		Objective: clampText(p.Objective, 2000), Client: clampText(p.Client, 200),
		ContractNo: clampText(p.ContractNo, 100), Amount: p.Amount, Budget: p.Budget,
		PlanStart: p.PlanStart, PlanEnd: p.PlanEnd, Remark: clampText(p.Remark, 2000),
		Status: project.StatusCreated, OrgID: e.boundOrgID(ctx),
	}
	if err := project.ValidateCreateBusinessFields(candidate); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	created, err := e.projects.Create(ctx, r.IdempotencyKey, projectMutationActor, p, candidate)
	if err != nil {
		return projectFailure(r, err)
	}
	return bridge.Success(r.ID, newProjectDTO(created))
}

func handleProjectList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Status string `json:"status"`
		Type   string `json:"type"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.list 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	for _, s := range []string{p.Status, p.Type} {
		if s != "" && !validProjectEnum(s) {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.list 参数无效", false)
		}
	}
	items, err := e.projects.List(ctx, project.Filter{Status: project.Status(p.Status), Type: project.Type(p.Type), OrgID: e.boundOrgID(ctx)})
	if err != nil {
		return projectFailure(r, err)
	}
	dtos := make([]projectDTO, 0, len(items))
	for i := range items {
		if p.Status == "" && items[i].Status == project.StatusArchived {
			continue
		}
		dtos = append(dtos, newProjectDTO(items[i]))
	}
	if len(dtos) > 100 {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", false)
	}
	return bridge.Success(r.ID, struct {
		Items []projectDTO `json:"items"`
	}{Items: dtos})
}

func validProjectEnum(s string) bool {
	switch project.Status(s) {
	case project.StatusCreated, project.StatusActive, project.StatusChartered,
		project.StatusReqArchitecture, project.StatusReqAssessment, project.StatusInProgress,
		project.StatusIntegrationTest, project.StatusGoLivePrep, project.StatusLive,
		project.StatusClosed, project.StatusArchived:
		return true
	}
	switch project.Type(s) {
	case project.TypeImplementation, project.TypeOperations, project.TypeEnhancement:
		return true
	}
	return false
}

type projectMutationMeta struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Reason  string `json:"reason"`
}

type projectAdvancePayload struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Phase   int    `json:"phase"`
}

type projectUpdatePayload struct {
	ID          string  `json:"id"`
	Version     int64   `json:"version"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Summary     string  `json:"summary"`
	Objective   string  `json:"objective"`
	Client      string  `json:"client"`
	ContractNo  string  `json:"contractNo"`
	Amount      float64 `json:"amount"`
	Budget      float64 `json:"budget"`
	PlanStart   string  `json:"planStart"`
	PlanEnd     string  `json:"planEnd"`
	Remark      string  `json:"remark"`
}

func handleProjectMutate(e *Engine, ctx context.Context, r bridge.Request, action, id string, version int64, reason string, apply func(*project.Project) error) bridge.Response {
	if !validCanonicalULID(id) || version < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", action+" 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.projects.Mutate(ctx, r.IdempotencyKey, projectMutationActor, action, id, version, func(cur *project.Project) error {
		cur.Status = project.NormalizeStatus(cur.Status)
		switch action {
		case "project.update":
			if !cur.CanEditMutableFields() && !cur.CanEditIdentity() {
				return projectapp.ErrInvalidTransition
			}
		case "project.publish":
			if !cur.CanPublish() {
				return projectapp.ErrInvalidTransition
			}
		case "project.close":
			if !cur.CanClose() {
				return projectapp.ErrInvalidTransition
			}
		case "project.reopen":
			if cur.Status != project.StatusClosed {
				return projectapp.ErrInvalidTransition
			}
		case "project.advanceStatus":
			if !cur.CanEditMutableFields() {
				return projectapp.ErrInvalidTransition
			}
			// applied in handler body
		}
		if err := apply(cur); err != nil {
			return err
		}
		switch action {
		case "project.publish":
			cur.Status = project.StatusChartered
		case "project.close":
			cur.StatusBeforeClose = cur.Status
			cur.CloseReason = clampText(reason, 500)
			if cur.CloseReason == "" {
				return errors.New("close reason is required")
			}
			cur.Status = project.StatusClosed
		case "project.reopen":
			cur.ReopenReason = clampText(reason, 500)
			if cur.ReopenReason == "" {
				return errors.New("reopen reason is required")
			}
			if cur.StatusBeforeClose != "" {
				cur.Status = project.NormalizeStatus(cur.StatusBeforeClose)
			} else {
				cur.Status = project.StatusChartered
			}
			cur.CloseReason = ""
		}
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return bridge.Success(r.ID, newProjectDTO(result))
}

func handleProjectUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var body projectUpdatePayload
	if decodePayload(r.Payload, &body) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.update 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.update", body.ID, body.Version, "", func(cur *project.Project) error {
		if cur.CanEditIdentity() {
			name, err := project.NormalizeName(body.Name)
			if err != nil {
				return err
			}
			cur.Name = name
			if body.Type != "" {
				cur.Type = project.Type(body.Type)
			}
		}
		if !cur.CanEditMutableFields() {
			return nil
		}
		cur.Description = clampText(body.Description, 2000)
		cur.Summary = clampText(body.Summary, 500)
		cur.Objective = clampText(body.Objective, 2000)
		cur.Client = clampText(body.Client, 200)
		cur.ContractNo = clampText(body.ContractNo, 100)
		cur.Amount, cur.Budget = body.Amount, body.Budget
		cur.PlanStart, cur.PlanEnd = body.PlanStart, body.PlanEnd
		cur.Remark = clampText(body.Remark, 2000)
		return nil
	})
}

func handleProjectPublish(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectMutationMeta
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.publish 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.publish", p.ID, p.Version, "", func(*project.Project) error { return nil })
}

func handleProjectClose(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectMutationMeta
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.close 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.close", p.ID, p.Version, p.Reason, func(*project.Project) error { return nil })
}

func handleProjectReopen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectMutationMeta
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.reopen 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.reopen", p.ID, p.Version, p.Reason, func(*project.Project) error { return nil })
}

func handleProjectAdvanceStatus(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectAdvancePayload
	if decodePayload(r.Payload, &p) != nil || p.Phase < 1 || p.Phase > 9 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "project.advanceStatus 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.advanceStatus", p.ID, p.Version, "", func(cur *project.Project) error {
		next, ok := project.AdvanceTarget(cur.Type, p.Phase)
		if !ok {
			return projectapp.ErrInvalidTransition
		}
		cur.Status = next
		return nil
	})
}

func projectFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, projectapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, projectapp.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, projectapp.ErrProjectCapacityReached):
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_CAPACITY_REACHED", "项目数量已达到上限", false)
	case errors.Is(err, projectapp.ErrProjectVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_VERSION_CONFLICT", "项目已被其他操作修改，请刷新后重试", false)
	case errors.Is(err, projectapp.ErrInvalidTransition), errors.Is(err, project.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_INVALID_TRANSITION", "项目状态门禁不允许该操作", false)
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
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	cur, err := e.projects.Get(ctx, p.ID)
	if err != nil {
		return projectFailure(r, err)
	}
	cur.Status = project.NormalizeStatus(cur.Status)
	if !cur.CanDeleteOnlyCreated() {
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_INVALID_TRANSITION", "仅创建态且无产出的项目可删除", false)
	}
	if has, err := e.projects.HasArtifacts(ctx, p.ID); err != nil {
		return projectFailure(r, err)
	} else if has {
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_INVALID_TRANSITION", "项目已有产出，不允许删除", false)
	}
	_, err = e.projects.Mutate(ctx, r.IdempotencyKey, projectMutationActor, "project.delete", p.ID, cur.Version, func(proj *project.Project) error {
		proj.Status = project.StatusArchived
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"deleted": true, "id": p.ID})
}
