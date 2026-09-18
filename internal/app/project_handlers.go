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
	"github.com/lunitide/lunitide/internal/projectgen"
	"github.com/lunitide/lunitide/internal/projectroot"
	"github.com/lunitide/lunitide/internal/projectrules"
	"github.com/lunitide/lunitide/internal/projectschema"
)

const projectMutationActor = "desktop-host"

type projectDTO struct {
	ID                  string             `json:"id"`
	Name                string             `json:"name"`
	ProjectCode         string             `json:"projectCode"`
	Type                project.Type       `json:"type"`
	Description         string             `json:"description"`
	Summary             string             `json:"summary"`
	Objective           string             `json:"objective"`
	Client              string             `json:"client"`
	ContractNo          string             `json:"contractNo"`
	Amount              float64            `json:"amount"`
	Budget              float64            `json:"budget"`
	PlanStart           string             `json:"planStart"`
	PlanEnd             string             `json:"planEnd"`
	Remark              string             `json:"remark"`
	CloseReason         string             `json:"closeReason,omitempty"`
	StatusBeforeClose   project.Status     `json:"statusBeforeClose,omitempty"`
	ReopenReason        string             `json:"reopenReason,omitempty"`
	Status              project.Status     `json:"status"`
	OrgID               string             `json:"orgId,omitempty"`
	SpaceID             string             `json:"spaceId,omitempty"`
	RootPath            string             `json:"rootPath,omitempty"`
	TreeStatus          project.TreeStatus `json:"treeStatus,omitempty"`
	TreeDigest          string             `json:"treeDigest,omitempty"`
	TreeGeneratedAt     string             `json:"treeGeneratedAt,omitempty"`
	DefaultExecutor     project.Executor   `json:"defaultExecutor,omitempty"`
	RulesDigest         string             `json:"rulesDigest,omitempty"`
	RulesMaterializedAt string             `json:"rulesMaterializedAt,omitempty"`
	DBStatus            project.DBStatus   `json:"dbStatus,omitempty"`
	DBPath              string             `json:"dbPath,omitempty"`
	DBDigest            string             `json:"dbDigest,omitempty"`
	DBVerifiedAt        string             `json:"dbVerifiedAt,omitempty"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
	Version             int64              `json:"version"`
}

func newProjectDTO(p project.Project) projectDTO {
	p.Status = project.NormalizeStatus(p.Status)
	dto := projectDTO{
		ID: p.ID, Name: p.Name, ProjectCode: p.ProjectCode, Type: p.Type,
		Description: p.Description, Summary: p.Summary, Objective: p.Objective,
		Client: p.Client, ContractNo: p.ContractNo, Amount: p.Amount, Budget: p.Budget,
		PlanStart: p.PlanStart, PlanEnd: p.PlanEnd, Remark: p.Remark,
		CloseReason: p.CloseReason, StatusBeforeClose: p.StatusBeforeClose,
		ReopenReason: p.ReopenReason, Status: p.Status, OrgID: p.OrgID, SpaceID: p.SpaceID,
		RootPath: p.RootPath, TreeStatus: p.TreeStatus, TreeDigest: p.TreeDigest,
		TreeGeneratedAt: p.TreeGeneratedAt, DefaultExecutor: p.DefaultExecutor,
		RulesDigest: p.RulesDigest, RulesMaterializedAt: p.RulesMaterializedAt,
		DBStatus: p.DBStatus, DBPath: p.DBPath, DBDigest: p.DBDigest, DBVerifiedAt: p.DBVerifiedAt,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Version: p.Version,
	}
	if dto.DBStatus == project.DBNone {
		dto.DBStatus = ""
	}
	return dto
}

func (e *Engine) boundOrgID(ctx context.Context) (string, error) {
	id, _, err := e.boundOrgState(ctx)
	return id, err
}

func projectServiceAvailable(service ProjectService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Pointer || !v.IsNil()
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
	RootPath    string  `json:"rootPath"`
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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.create 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	name, err := project.NormalizeName(p.Name)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.create 参数无效", false)
	}
	orgID, err := e.boundOrgID(ctx)
	if err != nil {
		return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认，请重试", true)
	}
	candidate := project.Project{
		Name: name, Type: project.Type(p.Type),
		Description: clampText(p.Description, 2000), Summary: clampText(p.Summary, 500),
		Objective: clampText(p.Objective, 2000), Client: clampText(p.Client, 200),
		ContractNo: clampText(p.ContractNo, 100), Amount: p.Amount, Budget: p.Budget,
		PlanStart: p.PlanStart, PlanEnd: p.PlanEnd, Remark: clampText(p.Remark, 2000),
		RootPath: strings.TrimSpace(p.RootPath), Status: project.StatusCreated, OrgID: orgID,
	}
	// Ordinary and companion chat share this internal storage container. Their
	// existing renderer sends only this reserved name, not a business-project
	// form. Keep the exception exact: a hidden-name prefix or a partially filled
	// business form must not bypass required project fields. Scope, idempotency,
	// storage invariants and audit still apply through the normal creation path.
	if p == (projectCreatePayload{Name: "\u2063月汐·普通对话"}) {
		candidate.Type = project.TypeImplementation
	} else if err := project.ValidateCreateBusinessFields(candidate); err != nil {
		if err.Error() == "project root path is required" {
			return r.Fail("PROJECT_ROOT_REQUIRED", "请选择项目根目录", false)
		}
		return r.Fail("BRIDGE_SCHEMA_INVALID", projectCreateFieldMessage(err), false)
	}
	created, err := e.projects.Create(ctx, r.IdempotencyKey, projectMutationActor, struct {
		OrgID   string
		Payload any
	}{orgID, p}, candidate)
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(newProjectDTO(created))
}

func handleProjectList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Status string `json:"status"`
		Type   string `json:"type"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.list 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	for _, s := range []string{p.Status, p.Type} {
		if s != "" && !validProjectEnum(s) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "project.list 参数无效", false)
		}
	}
	orgID, err := e.boundOrgID(ctx)
	if err != nil {
		return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认，请重试", true)
	}
	items, err := e.projects.List(ctx, project.Filter{Status: project.Status(p.Status), Type: project.Type(p.Type), OrgID: orgID})
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
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", false)
	}
	return r.Ok(struct {
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
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	Phase         int    `json:"phase"`
	EmptyBoardAck bool   `json:"emptyBoardAck"`
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
	RootPath    string  `json:"rootPath"`
}

func handleProjectMutate(e *Engine, ctx context.Context, r bridge.Request, action, id string, version int64, reason string, request any, apply func(*project.Project) error) bridge.Response {
	if !validCanonicalULID(id) || version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", action+" 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.projects.Mutate(ctx, r.IdempotencyKey, projectMutationActor, action, id, version, request, func(cur *project.Project) error {
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
		if msg, ok := projectUserFieldMessage(err); ok {
			return r.Fail("BRIDGE_SCHEMA_INVALID", msg, false)
		}
		return projectFailure(r, err)
	}
	return r.Ok(newProjectDTO(result))
}

func handleProjectUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var body projectUpdatePayload
	if decodePayload(r.Payload, &body) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.update 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.update", body.ID, body.Version, "", body, func(cur *project.Project) error {
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
		if body.RootPath != "" && cur.Status == project.StatusCreated {
			normalized, nerr := projectroot.Normalize(body.RootPath)
			if nerr != nil {
				return projectapp.ErrRootInvalid
			}
			if err := projectroot.Probe(normalized); err != nil {
				return mapRootBindError(err)
			}
			if err := projectroot.Bind(normalized, projectroot.Lock{ProjectID: cur.ID, ProjectCode: cur.ProjectCode, Name: cur.Name}); err != nil {
				return mapRootBindError(err)
			}
			if cur.RootPath != "" && cur.RootPath != normalized {
				_ = projectroot.RemoveLock(cur.RootPath)
			}
			cur.RootPath = normalized
			if cur.TreeStatus == project.TreeReady || cur.TreeStatus == project.TreeFailed || cur.TreeStatus == project.TreePartial {
				cur.TreeStatus = project.TreePending
			}
		}
		return nil
	})
}

func handleProjectPublish(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectMutationMeta
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.publish 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.publish", p.ID, p.Version, "", p, func(cur *project.Project) error {
		if strings.TrimSpace(cur.RootPath) == "" && cur.Name != "\u2063月汐·普通对话" {
			return projectapp.ErrRootRequired
		}
		return nil
	})
}

func handleProjectClose(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectMutationMeta
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.close 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.close", p.ID, p.Version, p.Reason, p, func(*project.Project) error { return nil })
}

func handleProjectReopen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectMutationMeta
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.reopen 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.reopen", p.ID, p.Version, p.Reason, p, func(*project.Project) error { return nil })
}

func handleProjectAdvanceStatus(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p projectAdvancePayload
	if decodePayload(r.Payload, &p) != nil || p.Phase < 1 || p.Phase > 9 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.advanceStatus 参数无效", false)
	}
	return handleProjectMutate(e, ctx, r, "project.advanceStatus", p.ID, p.Version, "", p, func(cur *project.Project) error {
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
		return r.Fail("IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, projectapp.ErrIdempotencyConflict):
		return r.Fail("IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, projectapp.ErrProjectCapacityReached):
		return r.Fail("PROJECT_CAPACITY_REACHED", "项目数量已达到上限", false)
	case errors.Is(err, projectapp.ErrProjectVersionConflict):
		return r.Fail("PROJECT_VERSION_CONFLICT", "项目已被其他操作修改，请刷新后重试", false)
	case errors.Is(err, projectapp.ErrInvalidTransition), errors.Is(err, project.ErrNotFound):
		return r.Fail("PROJECT_INVALID_TRANSITION", "项目状态或阶段门禁不允许该操作，请核对前序阶段和有效交付物", false)
	case errors.Is(err, projectapp.ErrRootRequired):
		return r.Fail("PROJECT_ROOT_REQUIRED", "请选择项目根目录", false)
	case errors.Is(err, projectapp.ErrRootInvalid):
		return r.Fail("PROJECT_ROOT_INVALID", "根目录不存在或不是文件夹", false)
	case errors.Is(err, projectapp.ErrRootBusy):
		return r.Fail("PROJECT_ROOT_BUSY", "该目录已被其他项目占用", false)
	case errors.Is(err, projectapp.ErrRootReadonly):
		return r.Fail("PROJECT_ROOT_READONLY", "无法写入项目根目录，请换可写盘", false)
	case errors.Is(err, projectapp.ErrTreeInvalid):
		return r.Fail("PROJECT_TREE_INVALID", "目录树不合格", false)
	case errors.Is(err, projectapp.ErrTreeFailed):
		return r.Fail("PROJECT_TREE_FAILED", "生成项目目录失败，请检查权限后补生成", false)
	case errors.Is(err, projectapp.ErrTreeRequired):
		return r.Fail("PROJECT_TREE_REQUIRED", "尚未生成项目目录", false)
	case errors.Is(err, projectapp.ErrTaskNotFound):
		return r.Fail("PROJECT_TASK_NOT_FOUND", "清单条目不存在", false)
	case errors.Is(err, projectapp.ErrTaskPhase):
		return r.Fail("PROJECT_TASK_PHASE", "只能对开发清单打开任务", false)
	case errors.Is(err, projectapp.ErrExecutorUnavailable):
		return r.Fail("PROJECT_EXECUTOR_UNAVAILABLE", "所选执行器未检测到，请安装登录或改选月汐", false)
	case errors.Is(err, projectapp.ErrDevIncomplete):
		return r.Fail("PROJECT_DEV_INCOMPLETE", "开发清单尚未全部完成", false)
	case errors.Is(err, projectapp.ErrTestOpen):
		return r.Fail("PROJECT_TEST_OPEN", "测试清单仍有未完成或未通过项", false)
	case errors.Is(err, projectapp.ErrTestReasonRequired):
		return r.Fail("PROJECT_TEST_REASON_REQUIRED", "请填写测试不通过的原因", false)
	case errors.Is(err, projectapp.ErrTestNoSource):
		return r.Fail("PROJECT_TEST_NO_SOURCE", "请先绑定对应的开发条目", false)
	case errors.Is(err, projectgen.ErrInterviewInvalid):
		return r.Fail("PROJECT_INTERVIEW_INVALID", "访谈答案不合格", false)
	case errors.Is(err, projectapp.ErrGenerateEmpty), errors.Is(err, projectgen.ErrGenerateEmpty):
		return r.Fail("PROJECT_GENERATE_EMPTY", "生成结果为空，请换模版或重试", false)
	case errors.Is(err, projectapp.ErrGenerateSkipApproved), errors.Is(err, projectgen.ErrGenerateSkipApproved):
		return r.Fail("PROJECT_GENERATE_SKIP_APPROVED", "已批准的交付物不会覆盖，请先打回", false)
	case errors.Is(err, projectapp.ErrTemplateMissing):
		return r.Fail("PROJECT_TEMPLATE_MISSING", "模版文件不存在，请重选或改用骨架", false)
	case errors.Is(err, projectapp.ErrRulesFailed), errors.Is(err, projectrules.ErrRulesFailed):
		return r.Fail("PROJECT_RULES_FAILED", "规范写入失败，请检查根目录后重试", false)
	case errors.Is(err, projectapp.ErrRulesStale):
		return r.Fail("PROJECT_RULES_STALE", "规范已过期，请重新物化", false)
	case errors.Is(err, projectapp.ErrSchemaInvalid), errors.Is(err, projectschema.ErrSchemaInvalid):
		return r.Fail("PROJECT_SCHEMA_INVALID", "数据库模式不合格", false)
	case errors.Is(err, projectapp.ErrDBBindInvalid), errors.Is(err, projectschema.ErrDBBindInvalid):
		return r.Fail("PROJECT_DB_BIND_INVALID", "请选择项目根内的 sqlite 文件", false)
	case errors.Is(err, projectapp.ErrDBFailed), errors.Is(err, projectschema.ErrDBFailed):
		return r.Fail("PROJECT_DB_FAILED", "建表失败，请检查路径与权限", false)
	case errors.Is(err, projectapp.ErrDBIncomplete), errors.Is(err, projectschema.ErrDBIncomplete):
		return r.Fail("PROJECT_DB_INCOMPLETE", "请先物化并核齐全部数据表", false)
	case errors.Is(err, projectapp.ErrDBRequired):
		return r.Fail("PROJECT_DB_REQUIRED", "先完成库表核齐，才能开始开发任务", false)
	case errors.Is(err, projectapp.ErrInterfaceRequired):
		return r.Fail("PROJECT_INTERFACE_REQUIRED", "先完成接口阶段确认，才能开始开发任务", false)
	case errors.Is(err, projectapp.ErrBoardSourceInvalid):
		return r.Fail("PROJECT_BOARD_SOURCE_INVALID", "源清单无法解析，请改成 JSON 或 OpenAPI", false)
	case errors.Is(err, projectapp.ErrBoardDirty):
		return r.Fail("PROJECT_BOARD_DIRTY", "清单已更新，请先处理待再处理的条目", false)
	case errors.Is(err, projectapp.ErrSelfTestRequired):
		return r.Fail("PROJECT_SELFTEST_REQUIRED", "请先对照详细设计自测并通过", false)
	case errors.Is(err, projectapp.ErrTestKindUnsupported):
		return r.Fail("PROJECT_TEST_KIND_UNSUPPORTED", "该测试类型需要命令或证据", false)
	case errors.Is(err, projectapp.ErrIntegrationNotReady):
		return r.Fail("PROJECT_INTEGRATION_NOT_READY", "场景内单元测试尚未全部通过", false)
	case errors.Is(err, projectapp.ErrSyncInvalid):
		return r.Fail("PROJECT_SYNC_INVALID", "请另选可写的同步目录", false)
	case errors.Is(err, projectapp.ErrSyncRequired):
		return r.Fail("PROJECT_SYNC_REQUIRED", "发布前请先同步到选定目录", false)
	case errors.Is(err, projectapp.ErrAttachmentRequired):
		return r.Fail("PROJECT_ATTACHMENT_REQUIRED", "请先写入不少于 32 字节的附件正文，不能只绑模版", false)
	case errors.Is(err, projectapp.ErrEmptyBoardAck):
		return r.Fail("PROJECT_EMPTY_BOARD_ACK", "空清单需要勾选无任务后再晋级", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
}

func handleProjectDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.delete 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
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
		return r.Fail("PROJECT_INVALID_TRANSITION", "仅创建态且无产出的项目可删除", false)
	}
	if has, err := e.projects.HasArtifacts(ctx, p.ID); err != nil {
		return projectFailure(r, err)
	} else if has {
		return r.Fail("PROJECT_INVALID_TRANSITION", "项目已有产出，不允许删除", false)
	}
	_, err = e.projects.Mutate(ctx, r.IdempotencyKey, projectMutationActor, "project.delete", p.ID, cur.Version, p, func(proj *project.Project) error {
		proj.Status = project.StatusArchived
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"deleted": true, "id": p.ID})
}

func projectCreateFieldMessage(err error) string {
	if msg, ok := projectUserFieldMessage(err); ok {
		return msg
	}
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if peopleUserMessageHasHan(msg) {
			return msg
		}
	}
	return "项目参数无效"
}

func projectUserFieldMessage(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	switch strings.TrimSpace(err.Error()) {
	case "close reason is required":
		return "请填写关闭原因", true
	case "reopen reason is required":
		return "请填写重新打开原因", true
	case "closed project requires a close reason":
		return "关闭项目需要填写原因", true
	case "project name must contain 1 to 200 characters":
		return "项目名称需为 1 到 200 个字符", true
	case "project name is not normalized":
		return "项目名称格式无效", true
	case "project type is required":
		return "请选择项目类型", true
	case "project description is required":
		return "请填写项目描述", true
	case "project client is required":
		return "请填写客户", true
	case "project plan start date is required":
		return "请填写计划开始日期", true
	case "project plan end date is required":
		return "请填写计划结束日期", true
	case "project plan end must not precede plan start":
		return "计划结束日期不能早于开始日期", true
	case "project type is invalid":
		return "项目类型无效", true
	case "project amounts must be non-negative":
		return "项目金额不能为负", true
	case "project plan dates must be YYYY-MM-DD":
		return "计划日期须为 YYYY-MM-DD", true
	}
	return "", false
}
