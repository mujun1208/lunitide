package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
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
	}
	siteActHints = []string{"登录", "登陆", "点"}
	genHints     = []string{
		"写一份", "写个报告", "写份", "生成ppt", "生成 ppt", "生成PPT",
		"做一份", "画一张", "做视频", "生成表格", "生成报告", "生成文档",
	}
	openHints = []string{"打开", "启动", "把开"}
	playHints = []string{"播放", "暂停", "下一首", "上一首"}
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
	return route, routeAllow(route, ccEnabled)
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
	gen := containsAnyFold(t, lower, genHints)
	play := containsAnyFold(t, lower, playHints)
	open := containsAnyFold(t, lower, openHints)

	if info && namedApp {
		return RouteR2
	}
	if info && browserLookup {
		return RouteR1
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
	if gen {
		return RouteR4
	}
	if play {
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
			"web.search": true, "web.fetch": true, "video.understand": true,
			"memory.search": true, "memory.get": true,
			"user.ask": true,
		}
	case RouteR2:
		allow := map[string]bool{
			"desktop.open": true, "desktop.type": true, "media.play": true,
			"excel.parse": true, "excel.gen": true, "docx.gen": true,
			"pptx.gen": true, "pdf.gen": true, "html.gen": true,
			"user.ask": true,
		}
		if ccEnabled {
			allow["computer.act"] = true
		}
		return allow
	case RouteR3:
		return map[string]bool{
			"browser.act": true, "web.fetch": true, "user.ask": true,
		}
	case RouteR4:
		return map[string]bool{
			"excel.gen": true, "excel.parse": true, "docx.gen": true,
			"pptx.gen": true, "pdf.gen": true, "html.gen": true,
			"workspace.list": true, "workspace.read": true, "workspace.write": true,
			"workspace.search": true, "workspace.edit": true,
			"image.generate": true, "video.generate": true,
			"web.search": true, "web.fetch": true,
			"user.ask": true,
		}
	default:
		return nil
	}
}

// applyTaskRoute shrinks defs to allow. nil allow leaves defs unchanged.
// user.ask is always kept. kb.search / kb.cite / graph.expand already on
// defs stay (expert mount); they are never added by the allow map.
func applyTaskRoute(defs []gateway.ToolDefinition, route TaskRoute, allow map[string]bool) []gateway.ToolDefinition {
	_ = route
	if allow == nil {
		return defs
	}
	keep := copyAllow(allow)
	keep["user.ask"] = true
	for _, d := range defs {
		switch d.Name {
		case "kb.search", "kb.cite", "graph.expand",
			"skill.invoke", "skill.view", "skill.create", "skill.manage",
			"plan.run":
			keep[d.Name] = true
		}
	}
	return filterToolDefs(defs, keep)
}

// assembleRoutedTools mirrors chat.start: profile → companion deny → route.
func assembleRoutedTools(defs []gateway.ToolDefinition, goal string, companion, ccEnabled bool) []gateway.ToolDefinition {
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
	if containsAnyFold(orig, lower, browserAppHints) && containsAnyFold(orig, lower, openHints) {
		return true
	}
	return containsAnyFold(orig, lower, []string{"登录", "登陆"})
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
