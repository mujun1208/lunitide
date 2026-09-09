package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const maxTrialSkills = 8

type skillTrialService interface {
	InvokeTrial(context.Context, string, string, string, string) (skillapp.Invocation, error)
}

type skillTrialKey struct{}
type skillTrialScope struct {
	session string
	ids     map[string]bool
}

func withSkillTrials(ctx context.Context, session string, ids []string) context.Context {
	scope := skillTrialScope{session: session, ids: make(map[string]bool, len(ids))}
	for _, id := range ids {
		scope.ids[id] = true
	}
	return context.WithValue(ctx, skillTrialKey{}, scope)
}

func skillTrialsActive(ctx context.Context, session string) bool {
	scope, ok := ctx.Value(skillTrialKey{}).(skillTrialScope)
	return ok && scope.session == session && len(scope.ids) > 0
}

func (e *Engine) prepareSkillTrials(ctx context.Context, session string, ids []string, companion bool) (string, error) {
	if len(ids) == 0 {
		return "", nil
	}
	if companion || !validCanonicalULID(session) || len(ids) > maxTrialSkills {
		return "", errors.New("草稿试用仅适用于已打开的打字会话，每轮最多选择 8 个技能")
	}
	if err := e.CheckCapability(ctx, "skills"); err != nil {
		return "", err
	}
	if !skillServiceAvailable(e.skills) {
		return "", errors.New("技能服务不可用，无法试用草稿")
	}
	if _, ok := e.skills.(skillTrialService); !ok {
		return "", errors.New("当前技能服务不支持草稿试用")
	}
	seen := make(map[string]bool, len(ids))
	var b strings.Builder
	b.WriteString("\n[用户本轮显式选择的草稿试用]\n以下草稿尚未发布；执行本轮工作前，先分别 skill.try 读取完整约定。这里的试用优先于同名已发布目录项。不可用 skill.invoke 代替，不得自动发布；其他未选草稿不允许试用。工具权限仍依当前执行模式。试用输出不代表已安装或已通过质量测试。\n")
	for _, id := range ids {
		if !validCanonicalULID(id) || seen[id] {
			return "", errors.New("trialSkillIds 必须是最多 8 个不重复的技能 ULID")
		}
		seen[id] = true
		sk, err := e.skills.Get(ctx, id)
		if err != nil || sk == nil {
			return "", errors.New("选中的草稿不存在或无法读取，请刷新技能目录")
		}
		if sk.Status != skill.SkillStatusDraft {
			return "", errors.New("选中的技能已不是草稿；请刷新引用，已发布技能使用普通调用，停用技能不能试用")
		}
		fmt.Fprintf(&b, "- %s（skillId=%s，version=%s，revision=%d）\n", truncateUTF8Bytes(skillViewLabel(*sk), 160), sk.ID, sk.Version, sk.Rev)
	}
	return b.String(), nil
}

func skillTrialToolDefinition() llmadapter.ToolDefinition {
	return llmadapter.ToolDefinition{Name: "skill.try", Description: "Try one draft explicitly selected by the user for this chat turn. Load its complete working agreement without publishing or installing it. Only the listed trialSkillIds are permitted; normal operation permissions still apply. Use a concise input instruction referencing the full current request already in chat context.", Schema: []byte(`{"type":"object","properties":{"skillId":{"type":"string","pattern":"^[0-7][0-9A-HJKMNP-TV-Z]{25}$"},"input":{"type":"string","minLength":1,"maxLength":2048}},"required":["skillId","input"],"additionalProperties":false}`)}
}

func (e *Engine) invokeSkillTrialTool(ctx context.Context, mode executionMode, session string, args json.RawMessage) (toolruntime.Result, error) {
	if err := e.CheckCapability(ctx, "skills"); err != nil {
		return toolruntime.Result{}, err
	}
	var a struct {
		SkillID string `json:"skillId"`
		Input   string `json:"input"`
	}
	if json.Unmarshal(args, &a) != nil || !validCanonicalULID(a.SkillID) {
		return toolruntime.Result{}, errors.New("invalid skill.try arguments")
	}
	if err := validateSkillToolInput(a.Input); err != nil {
		return toolruntime.Result{}, err
	}
	scope, ok := ctx.Value(skillTrialKey{}).(skillTrialScope)
	if !ok || scope.session != session || !scope.ids[a.SkillID] {
		return toolruntime.Result{}, errors.New("此草稿未获本轮显式试用授权，请先在对话中选择该草稿；尚未调用或发布")
	}
	svc, ok := e.skills.(skillTrialService)
	if !ok || !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errors.New("技能草稿试用服务不可用")
	}
	inv, err := svc.InvokeTrial(ctx, a.SkillID, session, a.Input, string(mode))
	if err != nil {
		return toolruntime.Result{}, err
	}
	approved := mode == executionModeFullAccess
	if inv.RequiresApproval && !approved {
		return toolruntime.Result{}, fmt.Errorf("草稿试用仍需当前执行权限（risk=%s）；未执行。请在允许的对话执行模式下重新试用", inv.Risk)
	}
	out, err := e.skills.Execute(ctx, inv.ID, session, approved)
	if err != nil {
		return toolruntime.Result{}, err
	}
	if len(out.Output) > skillInvocationMaxBytes {
		return toolruntime.Result{}, errSkillContextBudget
	}
	return toolruntime.Result{Output: fmt.Sprintf("[草稿试用来源 skillId=%s version=%s manifestDigest=%s invocationId=%s]\n%s", inv.SkillID, inv.SkillVersion, inv.ManifestDigest, inv.ID, out.Output)}, nil
}

func isSkillInvocationTool(name string) bool { return name == "skill.invoke" || name == "skill.try" }
