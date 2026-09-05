package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestClassifyTaskRoute(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id        string
		goal      string
		companion bool
		cc        bool
		route     TaskRoute
		must      []string
		forbid    []string
		nilAllow  bool
	}{
		{
			id: "D-A1", goal: "北京明天天气",
			route: RouteR1,
			must:  []string{"web.search", "web.fetch", "memory.search", "memory.get", "user.ask"},
			forbid: []string{"desktop.open", "desktop.type", "browser.act", "computer.act", "kb.search"},
		},
		{
			id: "D-A2", goal: "打开汽水",
			cc: false, route: RouteR2,
			must:   []string{"desktop.open", "media.play", "user.ask"},
			forbid: []string{"computer.act", "browser.act"},
		},
		{
			id: "D-A2-cc", goal: "打开汽水",
			cc: true, route: RouteR2,
			must: []string{"desktop.open", "computer.act"},
		},
		{
			id: "D-A3", goal: "打开浏览器查天气",
			cc: true, route: RouteR1,
			must:   []string{"web.search", "user.ask"},
			forbid: []string{"browser.act", "desktop.open", "computer.act"},
		},
		{
			id:     "D-A3b",
			goal:   "上网查天气",
			route:  RouteR1,
			forbid: []string{"browser.act", "computer.act"},
		},
		{
			id: "D-A4", goal: "写一份半月财报到桌面",
			cc: true, route: RouteR4,
			must:   []string{"excel.gen", "docx.gen", "user.ask", "workspace.write"},
			forbid: []string{"computer.act", "browser.act", "desktop.open"},
		},
		{
			id: "D-A5", goal: "你好",
			route: RouteR0,
			must:  []string{"web.search", "web.fetch", "memory.search", "memory.get", "user.ask"},
			forbid: []string{"desktop.open", "command.run", "computer.act", "plan.run"},
		},
		{
			id: "D-A7", goal: "查一下今天金价",
			route:  RouteR1,
			forbid: []string{"kb.search", "kb.cite"},
		},
		{
			id: "conflict-named-app", goal: "用汽水查天气",
			route: RouteR2,
			must:  []string{"desktop.open", "media.play"},
		},
		{
			id: "site-chrome", goal: "打开 Chrome 上 12306",
			route: RouteR3,
			must:  []string{"browser.act", "web.fetch", "user.ask"},
			forbid: []string{"desktop.open", "computer.act", "web.search"},
		},
		{
			id: "play-next", goal: "播放七里香",
			route: RouteR2,
			must:  []string{"media.play", "desktop.open"},
		},
		{
			id:       "empty",
			goal:     "",
			nilAllow: true,
		},
		{
			id:       "unmatched-long",
			goal:     strings.Repeat("你好", 21),
			nilAllow: true,
		},
		{
			id:    "D-V2",
			goal:  "https://www.bilibili.com/video/BV1xx411c7mD",
			route: RouteR1,
			must:  []string{"video.understand", "web.fetch", "user.ask"},
			forbid: []string{"browser.act", "computer.act", "media.play"},
		},
		{
			id:    "D-V3",
			goal:  "6.66 复制打开抖音，看看【月食】https://v.douyin.com/ieFxxxx/",
			route: RouteR1,
			must:  []string{"video.understand"},
			forbid: []string{"browser.act", "computer.act", "media.play"},
		},
		{
			id:    "D-V4",
			goal:  "用浏览器打开 https://www.bilibili.com/video/BV1xx411c7mD",
			route: RouteR3,
			must:  []string{"browser.act", "web.fetch"},
			forbid: []string{"video.understand", "computer.act"},
		},
		{
			id:    "D-V5",
			goal:  "播放 https://v.douyin.com/ieFxxxx/",
			route: RouteR1,
			must:  []string{"video.understand"},
			forbid: []string{"media.play", "desktop.open"},
		},
		{
			id:    "D-V6",
			goal:  "解读总结一下 https://v.qq.com/x/cover/mzc00200abc/n0044xyz.html",
			route: RouteR1,
			must:  []string{"video.understand"},
			forbid: []string{"browser.act"},
		},
		{
			id:    "open-bilibili-no-url",
			goal:  "打开B站",
			route: RouteR3,
			must:  []string{"browser.act"},
			forbid: []string{"video.understand"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			route, allow := classifyTaskRoute(tc.goal, tc.companion, tc.cc)
			if tc.nilAllow {
				if route != RouteUnspecified || allow != nil {
					t.Fatalf("want unspecified/nil, got %q allow=%v", route, allow)
				}
				return
			}
			if route != tc.route {
				t.Fatalf("route=%q want %q", route, tc.route)
			}
			if allow == nil {
				t.Fatal("allow is nil")
			}
			for _, name := range tc.must {
				if !allow[name] {
					t.Fatalf("allow missing %s: %v", name, keysOf(allow))
				}
			}
			for _, name := range tc.forbid {
				if allow[name] {
					t.Fatalf("allow leaked %s", name)
				}
			}
			if !allow["user.ask"] {
				t.Fatal("user.ask must stay on every matched route")
			}
		})
	}
}

func TestClassifyTaskRouteDoesNotEnableCC(t *testing.T) {
	t.Parallel()
	// D-A6: ccEnabled is an input; classification never flips it and
	// only the R2 allow map may mention computer.act.
	cc := false
	route, allow := classifyTaskRoute("打开汽水", false, cc)
	if cc {
		t.Fatal("classify must not set the caller's cc flag")
	}
	if route != RouteR2 {
		t.Fatalf("route=%q", route)
	}
	if allow["computer.act"] {
		t.Fatal("ccEnabled=false must omit computer.act")
	}
	routeOn, allowOn := classifyTaskRoute("打开汽水", true, true)
	if routeOn != RouteR2 {
		t.Fatalf("companion+cc route=%q", routeOn)
	}
	if !allowOn["computer.act"] {
		t.Fatal("ccEnabled=true must add computer.act on R2 only")
	}
	_, r1 := classifyTaskRoute("北京明天天气", false, true)
	if r1["computer.act"] {
		t.Fatal("R1 must never gain computer.act even when CC is on")
	}
}

func TestApplyTaskRoute(t *testing.T) {
	t.Parallel()
	all := append(engineToolDefinitions(),
		gateway.ToolDefinition{Name: "computer.act"},
		gateway.ToolDefinition{Name: "kb.search"},
		gateway.ToolDefinition{Name: "kb.cite"},
		gateway.ToolDefinition{Name: "plan.run"},
	)
	route, allow := classifyTaskRoute("北京明天天气", false, true)
	got := applyTaskRoute(all, route, allow)
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.Name] = true
	}
	if seen["desktop.open"] || seen["computer.act"] || seen["browser.act"] {
		t.Fatalf("R1 leaked mutating tools: %v", seen)
	}
	if !seen["web.search"] || !seen["user.ask"] || !seen["video.understand"] {
		t.Fatalf("R1 dropped search/ask/video.understand: %v", seen)
	}
	routePlay, allowPlay := classifyTaskRoute("播放七里香", false, false)
	if routePlay != RouteR2 || allowPlay["video.understand"] {
		t.Fatalf("R2 must not carry video.understand: route=%q allow=%v", routePlay, allowPlay)
	}
	routeBrowse, allowBrowse := classifyTaskRoute("打开 Chrome 上 12306", false, false)
	if routeBrowse != RouteR3 || allowBrowse["video.understand"] {
		t.Fatalf("R3 must not carry video.understand: route=%q allow=%v", routeBrowse, allowBrowse)
	}
	// Expert tools already on the table stay; classify itself never adds kb.search.
	if !seen["kb.search"] || !seen["kb.cite"] {
		t.Fatal("applyTaskRoute must preserve already-mounted kb.*")
	}
	if allow["kb.search"] {
		t.Fatal("D-A7: R1 allow map must not contain kb.search")
	}

	if got := applyTaskRoute(all, RouteUnspecified, nil); len(got) != len(all) {
		t.Fatalf("nil allow must keep the full surface: %d vs %d", len(got), len(all))
	}
}

func TestApplyTaskRouteR0MatchesMinimal(t *testing.T) {
	t.Parallel()
	all := engineToolDefinitions()
	route, allow := classifyTaskRoute("你好", false, false)
	if route != RouteR0 {
		t.Fatalf("route=%q", route)
	}
	routed := applyTaskRoute(all, route, allow)
	minimal := applyToolProfile(all, toolProfileMinimal)
	if len(routed) != len(minimal) {
		t.Fatalf("R0=%d minimal=%d", len(routed), len(minimal))
	}
	want := map[string]bool{}
	for _, d := range minimal {
		want[d.Name] = true
	}
	for _, d := range routed {
		if !want[d.Name] {
			t.Fatalf("R0 extra %s", d.Name)
		}
	}
}

func TestAssembleRoutedToolsCompanionWeather(t *testing.T) {
	t.Parallel()
	all := append(engineToolDefinitions(), gateway.ToolDefinition{Name: "computer.act"}, gateway.ToolDefinition{Name: "kb.search"})
	got := assembleRoutedTools(all, "北京明天天气", true, true)
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.Name] = true
	}
	if seen["command.run"] || seen["im.send"] {
		t.Fatal("companion deny must still apply")
	}
	if seen["computer.act"] || seen["desktop.open"] {
		t.Fatal("companion R1 must not keep desktop/computer.act")
	}
	if !seen["web.search"] || !seen["kb.search"] {
		t.Fatalf("expected search + mounted kb: %v", seen)
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}
