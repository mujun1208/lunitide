package app

import (
	"github.com/lunitide/lunitide/internal/llmadapter"
	"regexp"
	"strings"
)

var skillAuthoringIntent = regexp.MustCompile(`(?i)(创建|新建|编写|制作|生成|改进|优化|修改|更新|封装|保存|整理成|转成|做一个|做个|写一个|写个|create|build|author|improve|update|design)[^\n。！？!?；;]{0,100}(技能|\bskills?\b)`)
var expertAuthoringIntent = regexp.MustCompile(`(?i)(?:^|[，,。！？；;\n]|请|帮我|我要|我想|想要|需要|先|再)(创建|新建|制作|生成|改进|优化|修改|更新|保存|做一个|做个|create|build|author|improve|update|design)[^\n。！？!?；;]{0,100}(专家|\bexpert\b)`)
var capabilityTrialIntent = regexp.MustCompile(`(?i)(?:^|[，,。！？；;\n]|请|帮我|我要|我想|想要|需要|先|再)(试用|测试|评估)(一下|下|这个|该|新建的|刚才的|新创建的|技能|专家)|(?:技能|专家)[^\n。！？!?；;]{0,40}(试用|测试一下|做测试|进行测试|评估一下)|\b(test|trial|evaluate)\b.{0,40}\b(skill|expert)\b`)

func looksLikeExpertAuthoringTask(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(t, "[引用技能 expert-manager|") || expertAuthoringIntent.MatchString(capabilityRequestBody(t))
}

func capabilityRequestBody(text string) string {
	t := strings.TrimSpace(text)
	for strings.HasPrefix(t, "[引用技能 ") || strings.HasPrefix(t, "[引用专家 ") {
		_, rest, ok := strings.Cut(t, "]")
		if !ok {
			break
		}
		t = strings.TrimSpace(rest)
	}
	return t
}

func capabilityWorkTask(text string) bool {
	if looksLikeSkillAuthoringTask(text) || looksLikeExpertAuthoringTask(text) {
		return true
	}
	t := strings.ToLower(capabilityRequestBody(text))
	subject := strings.Contains(t, "技能") || strings.Contains(t, "专家") || strings.Contains(t, "skill") || strings.Contains(t, "expert")
	return subject && capabilityTrialIntent.MatchString(t)
}

func capabilityWorkRequest(req llmadapter.Request) bool {
	current := lastUserChatText(req.Messages)
	if capabilityWorkTask(current) {
		return true
	}
	if !(looksLikeResume(current) || looksLikeStatusFollowUp(current) || strings.Contains(current, "刚才") || strings.Contains(current, "决策提交")) {
		return false
	}
	skipped := false
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != llmadapter.RoleUser {
			continue
		}
		if !skipped {
			skipped = true
			continue
		}
		return capabilityWorkTask(m.Content)
	}
	return false
}

// A reusable skill's subject (weekly report/PPT/Excel) is not a request to
// produce that document now. Explicitly selecting the creator also keeps the
// user's following requirements on the skill-authoring path.
func looksLikeSkillAuthoringTask(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	for _, cancel := range []string{"不要创建技能", "不用创建技能", "别创建技能", "不需要创建技能", "don't create a skill", "do not create a skill"} {
		if strings.Contains(t, cancel) {
			return false
		}
	}
	remaining := t
	for strings.HasPrefix(remaining, "[引用技能 ") || strings.HasPrefix(remaining, "[引用专家 ") {
		ref, tail, ok := strings.Cut(remaining, "]")
		if !ok {
			break
		}
		if strings.HasPrefix(ref, "[引用技能 ") {
			label, _, _ := strings.Cut(strings.TrimPrefix(ref, "[引用技能 "), "|")
			if label == "skill-creator" || label == "技能创建器" || label == "技能创建助手" {
				return true
			}
		}
		remaining = strings.TrimSpace(tail)
	}
	return skillAuthoringIntent.MatchString(remaining)
}

func skillAuthoringInstruction(text string) string {
	if looksLikeExpertAuthoringTask(text) {
		return "\n[本轮专家创建] 用户要创建或改进专家岗位说明书。先读取 expert-manager，再调用 expert.create 保存真实专家。本轮不受「操作结果一到三句 / 不要展开核对表」限制，也不要追加「下一步建议」。必须把用户描述充分拆进六段卡片，对照可用技能目录和已装 MCP 做匹配，并把匹配项写入 skillKeys（技能用目录 key，MCP 用 mcp:<id>）。对话里用两张 GFM 表交代：①岗位说明书（身份/使命/规则/流程/交付模板/成败标准）②装备匹配（类型/名称/理由/是否已关联）。模型工具参数是平铺的 name/division/description/semver/identity/mission/rules/workflow/deliverableTemplate/successMetrics/skillKeys，不是 source/frontmatter/sixSection/requestId。只有 expert.create 返回成功和 expertId 后才能报告创建成功，并保留上述表格。小说、报告、Word 是专家的业务范围，不是这轮要生成的文档。不要触发文档流水线或要求确认文件生成。\n"
	}
	if !looksLikeSkillAuthoringTask(text) {
		return ""
	}
	return "\n[本轮技能创建] 用户要创建或改进可复用技能。优先 skill.invoke 一次读完整约定；不要为同一份 SKILL.md 反复 skill.view 分页或 workspace.read。再用 skill.create / skill.manage 保存真实技能；保存成功后直接汇报技能 ID，不要再继续工具循环。流程图必须在同一条消息里闭合 ```mermaid 围栏。周报、PPT、表格是技能处理的主题，不是本轮必须生成的文件。不要启动文档生成流水线或把一份文档当成技能交付；只有工具返回保存成功与技能 ID 才能报告创建成功。\n"
}
