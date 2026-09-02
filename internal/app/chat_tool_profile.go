package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
)

type toolProfile string

const (
	toolProfileDefault   toolProfile = ""
	toolProfileMinimal   toolProfile = "minimal"
	toolProfileCoding    toolProfile = "coding"
	toolProfileColleague toolProfile = "colleague"
)

func parseToolProfile(raw string) toolProfile {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "minimal":
		return toolProfileMinimal
	case "coding":
		return toolProfileCoding
	case "colleague":
		return toolProfileColleague
	default:
		return toolProfileDefault
	}
}

// autoProfileTaskHints are substrings that mark a turn as carrying an
// actionable task. Any hit keeps the full default tool surface — narrowing is
// never allowed to strip a tool the turn plausibly needs.
var autoProfileTaskHints = []string{
	"文件", "代码", "运行", "执行", "打开", "点击", "截图", "帮我", "编译", "测试",
	"报错", "安装", "部署", "生成", "播放", "发送", "邮件", "网页", "浏览", "电脑",
	"桌面", "会议", "修改", "重构", "调试", "命令", "终端", "项目", "文档", "表格",
	"改一下", "写一个", "写个", "查一下", "查询", "搜索",
	"file", "code", "run", "open", "click", "search", "fix", "build", "test",
	"error", "install", "deploy", "http", ".go", ".ts", ".py", ".md", "git",
	"todo", "refactor", "debug", "terminal", "command",
}

// autoProfileChatHints are positive signals that a short turn is pure
// conversation. A narrow profile is only chosen when one of these is present
// AND no task hint is — an ambiguous turn always stays on the full surface.
var autoProfileChatHints = []string{
	"你好", "您好", "嗨", "哈喽", "在吗", "早上好", "中午好", "下午好", "晚上好", "晚安",
	"谢谢", "感谢", "多谢", "辛苦了", "你是谁", "你叫什么", "聊聊", "聊天", "无聊",
	"讲个笑话", "陪我", "好无聊", "在干嘛", "在忙吗", "怎么样", "最近",
	"hi", "hello", "hey", "thanks", "thank you", "how are you", "good morning",
	"good night", "who are you", "what's up", "nice to meet",
}

// autoToolProfile picks a tool surface when the client sent no explicit
// profile (S1 token trim). It narrows to `minimal` only for high-confidence
// pure-conversation turns; minimal still keeps web.search/fetch, memory and
// user.ask, so even a misread degrades gracefully instead of stranding the
// model. Anything with an actionable intent — or any turn long enough to
// likely carry a task — stays on the full default surface.
func autoToolProfile(goal string) toolProfile {
	t := strings.TrimSpace(goal)
	if t == "" {
		return toolProfileDefault
	}
	if len([]rune(t)) > 40 {
		return toolProfileDefault
	}
	lower := strings.ToLower(t)
	for _, kw := range autoProfileTaskHints {
		if strings.Contains(t, kw) || strings.Contains(lower, kw) {
			return toolProfileDefault
		}
	}
	for _, kw := range autoProfileChatHints {
		if strings.Contains(t, kw) || strings.Contains(lower, kw) {
			return toolProfileMinimal
		}
	}
	return toolProfileDefault
}

func toolProfileAllow(profile toolProfile) map[string]bool {
	switch profile {
	case toolProfileMinimal:
		return map[string]bool{
			"web.search": true, "web.fetch": true,
			"memory.search": true, "memory.get": true,
			"user.ask": true,
		}
	case toolProfileCoding:
		return map[string]bool{
			"workspace.list": true, "workspace.read": true, "workspace.write": true,
			"workspace.search": true, "workspace.edit": true,
			"command.run": true,
			"web.search":  true, "web.fetch": true,
			"memory.search": true, "memory.get": true,
			"skill.invoke": true, "skill.view": true,
			"todo.write": true,
		}
	case toolProfileColleague:
		allow := map[string]bool{}
		for name := range specialistToolAllow {
			allow[name] = true
		}
		allow["memory.search"] = true
		allow["memory.get"] = true
		return allow
	default:
		return nil
	}
}

func applyToolProfile(defs []gateway.ToolDefinition, profile toolProfile) []gateway.ToolDefinition {
	allow := toolProfileAllow(profile)
	if allow == nil {
		return defs
	}
	return filterToolDefs(defs, allow)
}

// companionDefaultDeniedTool is the P1-PROF high-risk set: 月伴 may open
// desktop/media and (when CC is on) computer.act, but must not advertise
// or run a shell / outbound IM / raw cc.* ladder.
func companionDefaultDeniedTool(name string) bool {
	n := strings.TrimSpace(name)
	if strings.HasPrefix(n, "cc.") {
		return true
	}
	switch n {
	case "command.run", "im.send":
		return true
	default:
		return false
	}
}

func filterCompanionDefaultTools(defs []gateway.ToolDefinition) []gateway.ToolDefinition {
	out := make([]gateway.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		if companionDefaultDeniedTool(d.Name) {
			continue
		}
		out = append(out, d)
	}
	return out
}
