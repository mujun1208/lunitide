package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// M8 FR-19 handlers (T-8.10.x): expert.create / list / detail / update /
// toggle / archive / mount / mounting.get.
//
// Error mapping follows the M8 wire contract (04 错误矩阵 M8-042~048).

func handleExpertCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Source      string `json:"source"`
		Frontmatter struct {
			Name        string `json:"name"`
			Division    string `json:"division"`
			Description string `json:"description"`
			Semver      string `json:"semver"`
		} `json:"frontmatter"`
		SixSection struct {
			Identity            string `json:"identity"`
			Mission             string `json:"mission"`
			Rules               string `json:"rules"`
			Workflow            string `json:"workflow"`
			DeliverableTemplate string `json:"deliverableTemplate"`
			SuccessMetrics      string `json:"successMetrics"`
		} `json:"sixSection"`
		RequestID string   `json:"requestId"`
		Actor     string   `json:"actor"`
		SkillKeys []string `json:"skillKeys"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Source != m8core.ExpertSourceLocal ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 || len(p.SkillKeys) > 32 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.create 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.Create(ctx, m8app.CreateInput{
		Source: p.Source,
		Frontmatter: m8core.Frontmatter{
			Name: p.Frontmatter.Name, Division: p.Frontmatter.Division,
			Description: p.Frontmatter.Description, Semver: p.Frontmatter.Semver,
		},
		SixSection: m8core.SixSection{
			Identity: p.SixSection.Identity, Mission: p.SixSection.Mission,
			Rules: p.SixSection.Rules, Workflow: p.SixSection.Workflow,
			DeliverableTemplate: p.SixSection.DeliverableTemplate,
			SuccessMetrics:      p.SixSection.SuccessMetrics,
		},
		RequestID: p.RequestID, Actor: p.Actor, SkillKeys: p.SkillKeys,
	})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	e.registerAgentContactForExpert(ctx, res.ExpertID, p.Frontmatter.Name, p.Frontmatter.Division, string(res.State))
	return bridge.Success(r.ID, res)
}

func handleExpertCatalogList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	catalog := m8app.AgencyAgentsCatalog()
	if len(catalog) == 0 {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家市场目录不可用", true)
	}
	expertNames := map[string]bool{}
	if e.m8expert != nil {
		listed, err := e.m8expert.List(ctx, m8app.ExpertFilter{})
		if err == nil {
			for _, row := range listed.Experts {
				if row.State != m8core.ExpertArchived {
					expertNames[row.Name] = true
				}
			}
		}
	}
	var skills m8app.CatalogSkillStore
	if store, ok := e.skills.(m8app.CatalogSkillStore); ok {
		skills = store
	}
	items := make([]m8app.CatalogSummary, 0, len(catalog))
	for _, item := range catalog {
		installed := true
		if item.NeedsProject() {
			installed = expertNames[item.Name]
		}
		if installed && item.NeedsChat() {
			if skills == nil {
				installed = false
			} else if _, err := skills.GetByNameVersion(ctx, item.SkillName(), item.Version); err != nil {
				installed = false
			}
		}
		items = append(items, item.Summary(installed))
	}
	return bridge.Success(r.ID, map[string]any{"items": items})
}

func handleExpertInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ID) < 1 || len(p.ID) > 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.install 参数无效", false)
	}
	var skills m8app.CatalogSkillStore
	if store, ok := e.skills.(m8app.CatalogSkillStore); ok {
		skills = store
	}
	res, err := m8app.InstallAgencyAgent(ctx, e.m8expert, skills, p.ID)
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	if item, ok := m8app.LookupCatalogItem(p.ID); ok && res.ExpertID != "" {
		e.registerAgentContactForExpert(ctx, res.ExpertID, item.Name, item.Division, m8core.ExpertEnabled)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Division  string `json:"division"`
		Source    string `json:"source"`
		State     string `json:"state"`
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.list 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.List(ctx, m8app.ExpertFilter{
		Division: p.Division, Source: p.Source, State: p.State, ProjectID: p.ProjectID,
	})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertDetail(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID  string `json:"expertId"`
		VersionID string `json:"versionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) ||
		(p.VersionID != "" && !validCanonicalULID(p.VersionID)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.detail 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: p.ExpertID, VersionID: p.VersionID})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID          string            `json:"expertId"`
		ExpectedVersionID string            `json:"expectedVersionId"`
		SixSection        map[string]string `json:"sixSection"`
		ChangeNote        string            `json:"changeNote"`
		Actor             string            `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) ||
		!validCanonicalULID(p.ExpectedVersionID) || len(p.SixSection) < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.update 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.Update(ctx, m8app.UpdateInput{
		ExpertID: p.ExpertID, ExpectedVersionID: p.ExpectedVersionID,
		SixSection: p.SixSection, ChangeNote: p.ChangeNote, Actor: p.Actor,
	})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertToggle(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID string `json:"expertId"`
		Enabled  bool   `json:"enabled"`
		Actor    string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.toggle 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.Toggle(ctx, m8app.ExpertToggleInput{
		ExpertID: p.ExpertID, Enabled: p.Enabled, Actor: p.Actor,
	})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertArchive(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID     string `json:"expertId"`
		ConfirmToken string `json:"confirmToken"`
		Actor        string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) ||
		!m8core.ValidHexDigest(p.ConfirmToken) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.archive 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.Archive(ctx, m8app.ArchiveInput{
		ExpertID: p.ExpertID, ConfirmToken: p.ConfirmToken, Actor: p.Actor,
	})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertMount(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		PhaseKey  string `json:"phaseKey"`
		ExpertID  string `json:"expertId"`
		Action    string `json:"action"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) ||
		!validCanonicalULID(p.ExpertID) || !m8core.ValidPhaseKey(p.PhaseKey) ||
		(p.Action != "mount" && p.Action != "unmount" && p.Action != "updateVersion") {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.mount 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.Mount(ctx, m8app.MountInput{
		ProjectID: p.ProjectID, PhaseKey: p.PhaseKey, ExpertID: p.ExpertID,
		Action: p.Action, Actor: p.Actor,
	})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertMountingGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string          `json:"projectId"`
		PhaseKey  string          `json:"phaseKey"`
		Raw       json.RawMessage `json:"-"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) ||
		(p.PhaseKey != "" && !m8core.ValidPhaseKey(p.PhaseKey)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.mounting.get 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	res, err := e.m8expert.MountingGet(ctx, m8app.MountingGetInput{
		ProjectID: p.ProjectID, PhaseKey: p.PhaseKey,
	})
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleExpertSkillsGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID string `json:"expertId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.skills.get 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	keys, err := e.m8expert.ListBoundSkills(ctx, p.ExpertID)
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	if keys == nil {
		keys = []string{}
	}
	return bridge.Success(r.ID, map[string]any{"expertId": p.ExpertID, "skillKeys": keys})
}

func handleExpertSkillsSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID  string   `json:"expertId"`
		SkillKeys []string `json:"skillKeys"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) || p.SkillKeys == nil || len(p.SkillKeys) > 32 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "expert.skills.set 参数无效", false)
	}
	if e.m8expert == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	keys, err := e.m8expert.ReplaceBoundSkills(ctx, p.ExpertID, p.SkillKeys)
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	if keys == nil {
		keys = []string{}
	}
	return bridge.Success(r.ID, map[string]any{"expertId": p.ExpertID, "skillKeys": keys})
}

// m8ExpertFailure maps the FR-19 error family onto the M8 code matrix
// (M8-042~048 plus the shared family).
func m8ExpertFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrExpertSixSectionInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M8-042", "六段式校验失败，零落库", false)
	case errors.Is(err, m8app.ErrExpertVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "M8-043", "版本乐观锁冲突，请刷新后重试", false)
	case errors.Is(err, m8app.ErrMountLimitExceeded):
		return bridge.Failure(r.ID, r.TraceID, "M8-044", "该阶段挂载专家已达上限 4", false)
	case errors.Is(err, m8app.ErrExpertNotMountable):
		return bridge.Failure(r.ID, r.TraceID, "M8-045", "专家已停用或归档，禁止挂载", false)
	case errors.Is(err, m8app.ErrExpertArchiveMounted):
		return bridge.Failure(r.ID, r.TraceID, "M8-048", "存在活跃挂载，须先全部解除再归档", false)
	case errors.Is(err, m8app.ErrExpertBuiltinProtected):
		return bridge.Failure(r.ID, r.TraceID, "EXPERT_BUILTIN_PROTECTED", "内置专家禁止归档", false)
	case errors.Is(err, m8app.ErrExpertNotFound):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_NOT_FOUND", "专家资源不存在", false)
	case errors.Is(err, m8app.ErrExpertStateInvalid):
		return bridge.Failure(r.ID, r.TraceID, "EXPERT_STATE_INVALID", "专家状态不允许该操作", false)
	case errors.Is(err, m8app.ErrCatalogUnknown):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_NOT_FOUND", "目录中没有该专家", false)
	case errors.Is(err, m8app.ErrCatalogInstalled):
		return bridge.Failure(r.ID, r.TraceID, "EXPERT_DUPLICATE", "该目录项已安装", false)
	case errors.Is(err, m8app.ErrExpertDuplicate):
		return bridge.Failure(r.ID, r.TraceID, "EXPERT_DUPLICATE", "同名专家已存在", false)
	case errors.Is(err, m8app.ErrPayloadInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "载荷非法", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "专家服务暂时不可用", true)
	}
}
