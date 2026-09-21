package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/producthub"
)

func (e *Engine) Consult(ctx context.Context, prompt string) (producthub.ConsultResult, error) {
	out := producthub.ConsultResult{}
	if e == nil || !skillServiceAvailable(e.skills) {
		out.Output = "未挂载技能服务。已使用中枢本地方案。可在技能中心发布 debugger / code-reviewer / implement 后重试。"
		return out, nil
	}
	items, err := e.skills.List(ctx, skill.SkillStatusPublished)
	if err != nil || len(items) == 0 {
		out.Output = "没有已发布技能。已使用中枢本地方案。发布 debugger / code-reviewer / implement 后，「执行净化」会自动绑定。"
		return out, nil
	}
	pick := pickPurifySkill(items)
	name := strings.TrimSpace(pick.DisplayName)
	if name == "" {
		name = pick.Name
	}
	out.SkillID = pick.ID
	out.SkillName = name
	out.Output = fmt.Sprintf("已绑定内部技能「%s」（%s）。任务书已写入净化记录。请在对话里 skill.invoke 该技能落地补丁；中枢已完成本地目录修复（标签/入口方法/状态），不自动改写 Go/TS。\n\n%s", name, pick.ID, prompt)
	return out, nil
}

func pickPurifySkill(items []skill.Skill) skill.Skill {
	best, score := items[0], -1
	for _, item := range items {
		n := strings.ToLower(item.Name + " " + item.DisplayName + " " + item.Description)
		s := 0
		switch {
		case strings.Contains(n, "debugger") || strings.Contains(n, "调试"):
			s = 5
		case strings.Contains(n, "implement") || strings.Contains(n, "实现"):
			s = 4
		case strings.Contains(n, "code-reviewer") || strings.Contains(n, "review") || strings.Contains(n, "审查"):
			s = 3
		case strings.Contains(n, "diagnose") || strings.Contains(n, "诊断"):
			s = 2
		}
		if s > score {
			best, score = item, s
		}
	}
	return best
}
