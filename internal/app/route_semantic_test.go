package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestDetectAppActRouteUsesLiveVocabulary(t *testing.T) {
	apps := []string{"飞书", "Obsidian", "企业微信", "微信", "Word"}
	cases := []struct {
		goal string
		want TaskRoute
	}{
		{"在飞书里给运营群发一句今晚上线", RouteR2},
		{"切到 Obsidian 把今天的笔记保存一下", RouteR2},
		{"帮我在企业微信里@张三催一下报表", RouteR2},
		{"飞书上回一下李四", RouteR2},
		{"用飞书给张三说一声我晚点到", RouteR2},
		{"open obsidian and paste this", RouteR2},
		// Questions about an app are not desktop work.
		{"飞书是哪年发布的", RouteUnspecified},
		{"微信怎么导出聊天记录", RouteUnspecified},
		{"Obsidian 和 Notion 哪个好", RouteUnspecified},
		// Latin names need word boundaries: "password" must not match Word.
		{"reset my password please", RouteUnspecified},
		// No verb, no frame → plain chat.
		{"微信真好用", RouteUnspecified},
		{"不要打开微信，只告诉我步骤", RouteUnspecified},
		{"", RouteUnspecified},
	}
	for _, tc := range cases {
		if got := detectAppActRoute(tc.goal, apps); got != tc.want {
			t.Fatalf("goal=%q got=%q want=%q", tc.goal, got, tc.want)
		}
	}
	if got := detectAppActRoute("在飞书里发一句", nil); got != RouteUnspecified {
		t.Fatalf("empty vocabulary must not route: %q", got)
	}
}

func TestNativeSettingsPagesRouteAsDesktopWork(t *testing.T) {
	apps := toolruntime.KnownLaunchAppNames()
	for _, goal := range []string{"打开蓝牙设置", "打开蓝牙", "换壁纸", "帮我换壁纸", "打开锁屏", "帮我打开回收站", "把 Windows 更新页面打开", "进入控制面板"} {
		if route, allow := classifyTaskRouteApps(goal, false, true, apps); route != RouteR2 || !allow["desktop.open"] {
			t.Fatalf("%q: route=%q allow.desktop.open=%v; native pages must be R2 with desktop.open", goal, route, allow["desktop.open"])
		}
	}
	if route, _ := classifyTaskRouteApps("蓝牙设置在哪里能找到", false, true, apps); route == RouteR2 {
		t.Fatal("asking where a page lives is a question, not desktop work")
	}
}

func TestClassifyTaskRouteAppsPromotesUnknownAppToR2(t *testing.T) {
	apps := []string{"飞书"}
	goal := "在飞书里给运营群发一句今晚上线"
	if r, _ := classifyTaskRoute(goal, false, true); r != RouteUnspecified {
		t.Fatalf("static tables should miss 飞书 today: %q", r)
	}
	route, allow := classifyTaskRouteApps(goal, false, true, apps)
	if route != RouteR2 {
		t.Fatalf("route=%q want R2", route)
	}
	for _, name := range []string{"computer.act", "desktop.open", "desktop.type", "im.send", "user.ask"} {
		if !allow[name] {
			t.Fatalf("allow missing %s: %v", name, allow)
		}
	}
	if _, allowOff := classifyTaskRouteApps(goal, false, false, apps); allowOff["computer.act"] {
		t.Fatalf("computer.act must stay off when computer control is disabled")
	}
}

func TestClassifyTaskRouteAppsInfoQueryInsideAppKeepsBothFamilies(t *testing.T) {
	route, allow := classifyTaskRouteApps("在飞书里查一下明天天气然后发给张三", false, true, []string{"飞书"})
	if route != RouteR2 {
		t.Fatalf("route=%q", route)
	}
	if !allow["web.search"] || !allow["computer.act"] || !allow["im.send"] {
		t.Fatalf("allow=%v", allow)
	}
}

func TestClassifyTaskRouteAppsLeavesStaticRoutesAlone(t *testing.T) {
	route, _ := classifyTaskRouteApps("今天北京天气怎么样", false, true, []string{"飞书", "Obsidian"})
	if route != RouteR1 {
		t.Fatalf("route=%q want R1", route)
	}
	route, _ = classifyTaskRouteApps("你好", false, true, []string{"飞书"})
	if route != RouteR0 {
		t.Fatalf("route=%q want R0", route)
	}
}

func TestFlashClassifyIgnoresModelAllowMap(t *testing.T) {
	route, allow := classifyTaskRouteWithFlashCC("x", `{"route":"R2","allow":{}}`, true)
	if route != RouteR2 {
		t.Fatalf("route=%q", route)
	}
	if !allow["computer.act"] || !allow["desktop.open"] {
		t.Fatalf("allow must come from routeAllow, got %v", allow)
	}
	route, allow = classifyTaskRouteWithFlashCC("x", "Sure! {\"route\":\"none\"} hope that helps", true)
	if route != RouteUnspecified || allow != nil {
		t.Fatalf("NONE must stay unspecified: %q %v", route, allow)
	}
	route, _ = classifyTaskRouteWithFlashCC("x", "```json\n{\"route\":\"r3\"}\n```", false)
	if route != RouteR3 {
		t.Fatalf("fenced lowercase route: %q", route)
	}
}

func TestFlashRouteSystemPromptDefinesRoutes(t *testing.T) {
	p := flashRouteSystemPrompt("飞书、微信", true)
	for _, needle := range []string{"R0 =", "R1 =", "R2 =", "R3 =", "R4 =", "NONE", "飞书、微信", "never R2"} {
		if !containsAnyFold(p, p, []string{needle}) {
			t.Fatalf("prompt missing %q:\n%s", needle, p)
		}
	}
}

func TestWindowVocabularyExtractsAppNames(t *testing.T) {
	got := ccapp.WindowVocabulary([]ccapp.WindowInfo{
		{Title: "文档1 - Word", Process: "WINWORD.EXE"},
		{Title: "GitHub - Google Chrome", Process: "chrome.exe"},
		{Title: "微信", Process: "Weixin.exe"},
		{Title: "Program Manager", Process: "explorer.exe"},
	})
	want := map[string]bool{"winword": true, "Word": true, "chrome": true, "Google Chrome": true, "weixin": true, "微信": true}
	for _, v := range got {
		if !want[v] {
			t.Fatalf("unexpected vocabulary %q in %v", v, got)
		}
		delete(want, v)
	}
	if len(want) != 0 {
		t.Fatalf("missing %v from %v", want, got)
	}
}
