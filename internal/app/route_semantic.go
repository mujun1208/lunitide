package app

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

// Semantic routing layer on top of the keyword tables in task_route.go.
//
// Keyword tables are the zero-latency fast path. They fail on any app the
// table does not know ("在飞书里发一句" → L1, one tool step, no computer.act).
// This file adds two cheap signals that close that gap without a model call:
//
//  1. a live app vocabulary — the built-in launch table, Start Menu shortcuts
//     on this PC, and the processes / titles of windows open right now;
//  2. an "act inside an app" verb list that turns (app mention + act verb)
//     into R2 while leaving questions about apps ("微信是哪年发布的") alone.
//
// The flash classifier (capability_roles.go) runs only after both miss.

// appActVerbs mark acting inside a desktop app. Generic on their own; they
// only count when the sentence also names an app from the vocabulary.
var appActVerbs = []string{
	"打开", "启动", "运行", "进入", "切到", "切换到", "切换", "关闭", "关掉", "退出", "最小化", "最大化",
	"发", "发送", "发给", "发条", "发个", "回复", "回一下", "转发", "撤回", "艾特", "@",
	"点", "点击", "点开", "点一下", "输入", "打字", "粘贴", "填", "填写", "选", "勾",
	"搜", "搜索", "查找", "查一下", "登录", "登陆", "签到", "打卡",
	"截图", "录屏", "保存", "另存", "导出", "导入", "下载", "上传", "安装", "更新",
	"拨", "呼叫", "打电话", "语音通话", "视频通话", "开会", "入会", "加入会议",
	"置顶", "收藏", "删除", "清空", "标记", "已读",
	"说一声", "说一下", "问一下", "问问", "通知", "留言", "催一下", "提醒",
	"帮我在", "帮我用", "帮我去",
	"open", "launch", "switch to", "close", "quit", "send", "reply", "forward",
	"click", "type", "paste", "search", "log in", "login", "sign in", "download", "install",
}

// strongActVerbs override the how-to guard: "怎么在微信里发文件" is still a
// question, but "打开微信怎么还没打开" is an action follow-up.
var strongActVerbs = []string{
	"打开", "启动", "进入", "点开", "点一下", "点击", "发送", "发给", "帮我发", "帮我点", "帮我打开",
	"切到", "切换到", "关掉", "退出", "截图", "帮我在",
}

var howToMarkers = []string{
	"怎么", "如何", "为什么", "为啥", "是什么", "是哪", "什么时候", "多少", "哪个好", "区别",
	"how to", "how do", "what is", "why", "when did", "which is better",
}

// routingAppVocabulary is the live app list for one turn. Order: known
// launch table, installed Start Menu names, currently open windows.
func (e *Engine) routingAppVocabulary(ctx context.Context) []string {
	_ = ctx
	seen := map[string]bool{}
	var out []string
	add := func(names []string) {
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			key := strings.ToLower(n)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, n)
		}
	}
	add(toolruntime.KnownLaunchAppNames())
	add(toolruntime.InstalledAppNames())
	if e != nil && e.ccctrl != nil {
		add(e.ccctrl.OpenWindowHints())
	}
	return out
}

// matchAppMention returns the vocabulary entry the goal mentions, or "".
// Latin names need token boundaries so "word" does not match "password";
// CJK names use plain containment. Longer names win ties so "企业微信"
// beats "微信".
func matchAppMention(goal, lower string, apps []string) string {
	best := ""
	for _, app := range apps {
		a := strings.TrimSpace(app)
		if a == "" {
			continue
		}
		if !mentionsToken(goal, lower, a) {
			continue
		}
		if len([]rune(a)) > len([]rune(best)) {
			best = a
		}
	}
	return best
}

func mentionsToken(goal, lower, token string) bool {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return false
	}
	ascii := true
	for _, r := range t {
		if r >= 0x80 {
			ascii = false
			break
		}
	}
	if !ascii {
		return strings.Contains(goal, token) || strings.Contains(lower, t)
	}
	idx := 0
	for {
		i := strings.Index(lower[idx:], t)
		if i < 0 {
			return false
		}
		i += idx
		before := i == 0 || !isLatinWordRune(rune(lower[i-1]))
		after := i+len(t) >= len(lower) || !isLatinWordRune(rune(lower[i+len(t)]))
		if before && after {
			return true
		}
		idx = i + len(t)
	}
}

func isLatinWordRune(r rune) bool {
	return r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// detectAppActRoute is the vocabulary fast path: app mention + act verb → R2.
func detectAppActRoute(goal string, apps []string) TaskRoute {
	t := strings.TrimSpace(goal)
	if t == "" || len(apps) == 0 {
		return RouteUnspecified
	}
	// Drop negated phrases first ("不要打开微信，只告诉我步骤") so the
	// verb inside the negation cannot count as an action.
	for _, phrase := range openNegations {
		t = strings.ReplaceAll(t, phrase, " ")
	}
	lower := strings.ToLower(t)
	app := matchAppMention(t, lower, apps)
	if app == "" {
		return RouteUnspecified
	}
	if containsAnyFold(t, lower, howToMarkers) && !containsAnyFold(t, lower, strongActVerbs) {
		return RouteUnspecified
	}
	if containsAnyFold(t, lower, appActVerbs) {
		return RouteR2
	}
	// "在飞书里…" / "飞书上…" / "用飞书…" frame the app as the place where
	// something happens, which is acting inside it even without a listed verb.
	if actInsideAppFrame(t, lower, app) {
		return RouteR2
	}
	// A Settings/shell alias that *is* the whole request ("换壁纸", "帮我蓝牙")
	// is the page to open; it has no separate act verb because the alias
	// itself is the imperative.
	if nativePageGoal(t, app) {
		return RouteR2
	}
	return RouteUnspecified
}

func nativePageGoal(goal, app string) bool {
	if !toolruntime.IsNativePageName(app) {
		return false
	}
	g := strings.TrimSpace(goal)
	a := strings.TrimSpace(app)
	if strings.EqualFold(g, a) {
		return true
	}
	lower := strings.ToLower(g)
	al := strings.ToLower(a)
	for _, p := range []string{"帮我", "请帮我", "请", "麻烦", "帮忙"} {
		if strings.TrimSpace(strings.TrimPrefix(lower, p)) == al {
			return true
		}
	}
	return false
}

func actInsideAppFrame(goal, lower, app string) bool {
	a := strings.ToLower(app)
	for _, pat := range []string{"在" + a, "用" + a, "去" + a, "到" + a, "上" + a} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	for _, suf := range []string{"里", "里面", "上", "中", "内", "那边"} {
		if strings.Contains(lower, a+suf) {
			return true
		}
	}
	_ = goal
	return false
}

// classifyTaskRouteApps is classifyTaskRoute with a live vocabulary.
func classifyTaskRouteApps(goal string, companion, ccEnabled bool, apps []string) (TaskRoute, map[string]bool) {
	route, allow := classifyTaskRoute(goal, companion, ccEnabled)
	lower := strings.ToLower(goal)
	appRoute := detectAppActRoute(goal, apps)
	switch {
	case route == RouteUnspecified && appRoute == RouteR2:
		route = RouteR2
		allow = routeAllow(RouteR2, ccEnabled)
	case route == RouteR1 && appRoute == RouteR2:
		// "在微信里查一下天气" — info query inside a named app is desktop work.
		route = RouteR2
		merged := routeAllow(RouteR2, ccEnabled)
		mergeAllow(merged, routeAllow(RouteR1, ccEnabled))
		allow = merged
	case route == RouteR4 && appRoute == RouteR2:
		// Generation that must land inside an app keeps both tool families.
		mergeAllow(allow, routeAllow(RouteR2, ccEnabled))
	case appRoute == RouteR2 && allow != nil:
		mergeAllow(allow, routeAllow(RouteR2, ccEnabled))
	}
	if route == RouteR2 && allow != nil && containsAnyFold(goal, lower, sendIntentHints) {
		allow["im.send"] = true
	}
	return route, allow
}

// sendIntentHints mark "deliver a message to someone" so im.send stays
// available next to the desktop tools that would type it into an app.
var sendIntentHints = []string{
	"发送", "发给", "发消息", "发一句", "发条", "发个", "发一条", "发一下", "告诉", "回复", "回一下",
	"转发", "说一声", "说一下", "通知", "留言", "催一下", "提醒", "@", "艾特",
	"send", "message", "reply", "forward", "tell",
}

// flashRouteVocabulary trims the vocabulary to what fits a 128-token
// classifier prompt: known table first, then whatever is open right now.
func flashRouteVocabulary(apps []string, limit int) string {
	if limit <= 0 || len(apps) == 0 {
		return ""
	}
	names := make([]string, 0, limit)
	for _, a := range apps {
		if len(names) >= limit {
			break
		}
		names = append(names, a)
	}
	sort.Strings(names)
	return strings.Join(names, "、")
}
