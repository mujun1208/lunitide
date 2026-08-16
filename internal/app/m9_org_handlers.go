// M9 slice-1 org-admin bridge handlers (T-9.1.3): org.summary / create /
// switch / activate / suspend + org.space.list / create + org.member.list /
// invite / revoke. The verified org context is derived inside the service
// from the persisted operator binding - payloads never carry an org scope.
package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/org"
)

func handleOrgSummary(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.summary 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.Summary(ctx)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name string `json:"name"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.create 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.CreateOrg(ctx, p.Name)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgSwitch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		OrgID string `json:"orgId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.switch 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.Switch(ctx, p.OrgID)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgActivate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.activate 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.Activate(ctx)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgSuspend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.suspend 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.Suspend(ctx)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgSpaceList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.space.list 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.ListSpaces(ctx)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgSpaceCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name string `json:"name"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.space.create 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.CreateSpace(ctx, p.Name)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgMemberList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.member.list 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.ListMembers(ctx)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgMemberInvite(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		DisplayName string `json:"displayName"`
		ExternalID  string `json:"externalId"`
		IdpIssuer   string `json:"idpIssuer"`
		ExpiresAt   string `json:"expiresAt"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.member.invite 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.Invite(ctx, p.DisplayName, p.ExternalID, p.IdpIssuer, p.ExpiresAt)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleOrgMemberRevoke(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PrincipalID string `json:"principalId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "org.member.revoke 参数无效", false)
	}
	if e.m9org == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
	res, err := e.m9org.Revoke(ctx, p.PrincipalID)
	if err != nil {
		return m9OrgFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

// m9OrgFailure maps the M9 concept taxonomy (M9-001..M9-006) onto the wire
// error contract; anything else fails as storage unavailability.
func m9OrgFailure(r bridge.Request, err error) bridge.Response {
	switch org.Code(err) {
	case "M9-001":
		return bridge.Failure(r.ID, r.TraceID, "M9-001", "组织不存在", false)
	case "M9-002":
		return bridge.Failure(r.ID, r.TraceID, "M9-002", "组织已停用或封闭，写操作被拒绝", false)
	case "M9-003":
		return bridge.Failure(r.ID, r.TraceID, "M9-003", "跨组织访问被拒绝", false)
	case "M9-004":
		return bridge.Failure(r.ID, r.TraceID, "M9-004", "团队空间不存在", false)
	case "M9-005":
		return bridge.Failure(r.ID, r.TraceID, "M9-005", "身份已过期或被吊销", false)
	case "M9-006":
		return bridge.Failure(r.ID, r.TraceID, "M9-006", "角色绑定被拒绝", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "组织管理服务暂时不可用", true)
	}
}
