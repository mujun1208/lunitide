package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/videounderstand"
)

// TaskRoute is the daily-ops shrink class. Empty means "do not shrink".
type TaskRoute string

const (
	RouteUnspecified TaskRoute = ""
	RouteR0          TaskRoute = "R0"
	RouteR1          TaskRoute = "R1"
	RouteR2          TaskRoute = "R2"
	RouteR3          TaskRoute = "R3"
	RouteR4          TaskRoute = "R4"
)

var (
	infoQueryHints = []string{
		"天气", "气温", "股价", "金价", "新闻", "几点", "汇率",
		"weather", "temperature", "stock",
	}
	namedLocalAppHints = []string{
		"汽水", "网易云", "记事本", "notepad", "微信", "钉钉", "wps",
	}
	browserLookupHints = []string{
		"打开浏览器", "上网查", "用浏览器查", "用浏览器搜",
	}
	siteHints = []string{
		"http://", "https://", "网站", "网页",
		"12306", "知乎", "淘宝", "taobao", "bilibili", "B站", "youtube", "抖音",
		"weixin.qq.com", "微信视频号", "视频号",
	}
	siteActHints = []string{"登录", "登陆", "点"}
	genHints     = []string{
		"写一份", "写个报告", "写份", "写周报", "生成ppt", "生成 ppt", "生成PPT",
		"做一份", "画一张", "做视频", "生成表格", "生成报告", "生成文档",
		"生成 Word", "生成Word", "生成word",
	}
	openHints       = []string{"打开", "启动", "把开"}
	openNegations   = []string{"不打开", "别打开", "不要打开", "无需打开", "不用打开", "不必打开", "不需要打开", "请勿打开", "don't open", "do not open", "dont open", "without opening"}
	playHints       = []string{"播放", "暂停", "下一首", "上一首", "放首歌", "放歌", "播歌", "听歌", "来一首", "放一首"}
	desktopActHints = []string{
		"点击", "点一下", "点开", "点进", "第一条", "帮我点", "点按钮", "截图", "输入", "打字", "填写", "填表",
		"回车", "按一下", "按回车", "粘贴", "快捷键", "热键", "ctrl+",
		"点确定", "点保存", "点取消",
	}
	officeComputerHints = []string{
		"打开word", "打开 word", "在word", "在 word", "用word", "用 word",
		"电脑操作", "用电脑写", "用电脑打开",
	}
	browserAppHints = []string{"chrome", "edge", "firefox", "浏览器"}
)

// classifyTaskRoute picks a shrink class from the user goal.
// companion does not change the route. ccEnabled only adds computer.act on R2.
// Unmatched / empty goals return ("", nil) so today's full surface stays.
func classifyTaskRoute(goal string, companion, ccEnabled bool) (TaskRoute, map[string]bool) {
	_ = companion
	route := detectTaskRoute(goal)
	if route == RouteUnspecified {
		return RouteUnspecified, nil
	}
	allow := routeAllow(route, ccEnabled)
	// A route is an optimization, not a capability boundary. Compound goals
	// must retain the tools for every requested step (query -> write -> send).
	lower := strings.ToLower(goal)
	merge := func(extra map[string]bool) {
		for name, enabled := range extra {
			allow[name] = enabled
		}
	}
	if containsAnyFold(goal, lower, infoQueryHints) {
		merge(routeAllow(RouteR1, ccEnabled))
	}
	if !refusesOfficeGen(goal) && (containsAnyFold(goal, lower, genHints) || wantsOfficeGen(goal) || mediaGenerationKind(goal) != "") {
		merge(routeAllow(RouteR4, ccEnabled))
	}
	if containsAnyFold(goal, lower, []string{"发送", "发给", "发消息", "告诉", "回复", "转发", "send", "message"}) {
		allow["im.send"] = true
		if containsAnyFold(goal, lower, namedLocalAppHints) {
			merge(routeAllow(RouteR2, ccEnabled))
		}
	}
	if containsAnyFold(goal, lower, []string{"命令", "终端", "脚本", "编译", "测试", "shell", "terminal", "command", "script"}) {
		merge(toolProfileAllow(toolProfileCoding))
	}
	return route, allow
}

func detectTaskRoute(goal string) TaskRoute {
	t := strings.TrimSpace(goal)
	if t == "" {
		return RouteUnspecified
	}
	lower := strings.ToLower(t)
	info := containsAnyFold(t, lower, infoQueryHints)
	namedApp := containsAnyFold(t, lower, namedLocalAppHints)
	browserLookup := containsAnyFold(t, lower, browserLookupHints)
	site := containsAnyFold(t, lower, siteHints)
	gen := !refusesOfficeGen(t) && (containsAnyFold(t, lower, genHints) || wantsOfficeGen(t) || mediaGenerationKind(t) != "")
	play := containsAnyFold(t, lower, playHints)
	open := hasOpenIntent(t, lower)
	if open && containsAnyFold(t, lower, browserAppHints) && containsAnyFold(t, lower, []string{"桌面", "默认浏览器", "default browser", "desktop browser"}) {
		return RouteR2
	}
	if namedApp && containsAnyFold(t, lower, []string{"退出", "关闭", "quit", "exit"}) {
		return RouteR2
	}

	if websiteFirstResultGoal(t) {
		return RouteR3
	}
	if info && namedApp {
		return RouteR2
	}
	if info && open && (containsAnyFold(t, lower, browserAppHints) || containsAnyFold(t, lower, []string{"桌面", "本机", "文件", "文档"})) {
		return RouteR2
	}
	if info && browserLookup {
		return RouteR1
	}
	if info && containsAnyFold(t, lower, desktopActHints) {
		return RouteR2
	}
	if info {
		return RouteR1
	}
	if _, _, ok := videounderstand.DetectShareURL(t); ok {
		if explicitBrowserIntent(t, lower) {
			return RouteR3
		}
		return RouteR1
	}
	if site && (open || containsAnyFold(t, lower, siteActHints) || browserLookup || containsAnyFold(t, lower, browserAppHints)) {
		return RouteR3
	}
	if gen && (containsAnyFold(t, lower, officeComputerHints) || (open && containsAnyFold(t, lower, []string{"word", "wps"}))) {
		return RouteR2
	}
	if gen {
		return RouteR4
	}
	if play {
		return RouteR2
	}
	if containsAnyFold(t, lower, desktopActHints) {
		return RouteR2
	}
	if open && namedApp {
		return RouteR2
	}
	if open && containsAnyFold(t, lower, browserAppHints) && !site {
		return RouteR2
	}
	if autoToolProfile(t) == toolProfileMinimal {
		return RouteR0
	}
	return RouteUnspecified
}

func routeAllow(route TaskRoute, ccEnabled bool) map[string]bool {
	switch route {
	case RouteR0:
		return copyAllow(toolProfileAllow(toolProfileMinimal))
	case RouteR1:
		return map[string]bool{
			"web.search": true, "web.fetch": true, "weather.get": true, "video.understand": true,
			"memory.search": true, "memory.get": true,
			"user.ask": true,
		}
	case RouteR2:
		allow := map[string]bool{
			"desktop.open": true, "desktop.type": true, "desktop.quit": true, "desktop.browse": true, "media.play": true,
			"excel.parse": true, "excel.gen": true, "docx.gen": true,
			"pptx.gen": true, "pdf.gen": true, "html.gen": true,
			"office.generate": true, "office.inspect": true, "office.patch": true, "office.range.patch": true,
			"office.image.replace": true, "office.chart.patch": true, "office.cache.refresh": true, "office.deliver": true,
			"user.ask": true,
		}
		mergeAllow(allow, officeStudioAllow())
		if ccEnabled {
			allow["computer.act"] = true
		}
		return allow
	case RouteR3:
		return map[string]bool{
			"browser.act": true, "web.fetch": true, "user.ask": true,
		}
	case RouteR4:
		allow := map[string]bool{
			"excel.gen": true, "excel.parse": true, "docx.gen": true,
			"pptx.gen": true, "pdf.gen": true, "html.gen": true,
			"office.generate": true, "office.inspect": true, "office.patch": true, "office.range.patch": true,
			"office.image.replace": true, "office.chart.patch": true, "office.cache.refresh": true, "office.deliver": true,
			"workspace.list": true, "workspace.read": true, "workspace.write": true,
			"workspace.search": true, "workspace.edit": true,
			"image.generate": true, "video.generate": true, "audio.generate": true,
			"web.search": true, "web.fetch": true, "weather.get": true,
			"user.ask": true,
		}
		mergeAllow(allow, officeStudioAllow())
		return allow
	default:
		return nil
	}
}

// applyTaskRoute shrinks defs to allow. nil allow leaves defs unchanged.
// user.ask is always kept. kb.search / kb.cite / graph.expand already on
// defs stay (expert mount); they are never added by the allow map.
func applyTaskRoute(defs []llmadapter.ToolDefinition, route TaskRoute, allow map[string]bool) []llmadapter.ToolDefinition {
	_ = route
	if allow == nil {
		return defs
	}
	keep := copyAllow(allow)
	keep["user.ask"] = true
	for _, d := range defs {
		if strings.HasPrefix(d.Name, mcpToolPrefix) || d.Name == "mcp.search" || d.Name == "mcp.call" {
			keep[d.Name] = true
		}
		switch d.Name {
		case "kb.search", "kb.cite", "graph.expand",
			"skill.invoke", "skill.try", "skill.view", "skill.create", "skill.manage",
			"skill.catalog.list", "skill.list", "skill.install", "skill.publish",
			"plan.run":
			keep[d.Name] = true
		}
	}
	return filterToolDefs(defs, keep)
}

// assembleRoutedTools mirrors chat.start: profile → companion deny → route.
func assembleRoutedTools(defs []llmadapter.ToolDefinition, goal string, companion, ccEnabled bool) []llmadapter.ToolDefinition {
	profile := toolProfileDefault
	if !companion {
		profile = autoToolProfile(goal)
	}
	out := applyToolProfile(defs, profile)
	if companion {
		out = filterCompanionDefaultTools(out)
	}
	if profile == toolProfileDefault || profile == toolProfileMinimal {
		route, allow := classifyTaskRoute(goal, companion, ccEnabled)
		out = applyTaskRoute(out, route, allow)
	}
	return out
}

func explicitBrowserIntent(orig, lower string) bool {
	if strings.Contains(orig, "登录后看") || strings.Contains(orig, "登陆后看") {
		return false
	}
	if containsAnyFold(orig, lower, []string{"打开浏览器", "用浏览器", "在浏览器", "上网打开"}) {
		return true
	}
	if containsAnyFold(orig, lower, browserAppHints) && hasOpenIntent(orig, lower) {
		return true
	}
	return containsAnyFold(orig, lower, []string{"登录", "登陆"})
}

func officeStudioAllow() map[string]bool {
	return map[string]bool{
		"office.generate":      true,
		"office.inspect":       true,
		"office.patch":         true,
		"office.range.patch":   true,
		"office.image.replace": true,
		"office.chart.patch":   true,
		"office.cache.refresh": true,
		"office.deliver":       true,
	}
}

func mergeAllow(dst, extra map[string]bool) {
	for name, enabled := range extra {
		dst[name] = enabled
	}
}

var officeGenNegations = []string{
	"不要生成 office", "不要生成office", "不要 office 文档", "不要office文档",
	"不要生成 word", "不要生成word", "不要生成办公文档",
	"do not generate office", "don't generate office", "dont generate office",
}

func refusesOfficeGen(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return containsAnyFold(text, lower, officeGenNegations)
}

func hasOpenIntent(orig, lower string) bool {
	if !containsAnyFold(orig, lower, openHints) {
		return false
	}
	stripped := orig
	for _, phrase := range openNegations {
		stripped = strings.ReplaceAll(stripped, phrase, " ")
		if folded := strings.ToLower(phrase); folded != phrase {
			stripped = strings.ReplaceAll(stripped, folded, " ")
		}
	}
	return containsAnyFold(stripped, strings.ToLower(stripped), openHints)
}

func containsAnyFold(orig, lower string, hints []string) bool {
	for _, kw := range hints {
		if kw == "" {
			continue
		}
		if strings.Contains(orig, kw) || strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func copyAllow(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
