package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestSection8ManualContracts(t *testing.T) {
	t.Parallel()
	defs := append(engineToolDefinitions(),
		gateway.ToolDefinition{Name: "computer.act"},
		gateway.ToolDefinition{Name: "browser.act"},
		gateway.ToolDefinition{Name: "kb.search"},
	)

	t.Run("1-weather-web-search-only", func(t *testing.T) {
		route, allow := classifyTaskRoute("北京明天天气", false, false)
		if route != RouteR1 || !allow["web.search"] || allow["computer.act"] || allow["browser.act"] {
			t.Fatalf("§8.1 route=%s allow=%v", route, allow)
		}
	})
	t.Run("2-open-soda-foreground-route", func(t *testing.T) {
		route, allow := classifyTaskRoute("打开汽水", false, false)
		if route != RouteR2 || !allow["desktop.open"] || allow["computer.act"] {
			t.Fatalf("§8.2 cc-off 汽水 route=%s allow=%v", route, allow)
		}
	})
	t.Run("3-cc-adds-computer-act-on-r2", func(t *testing.T) {
		route, allow := classifyTaskRoute("打开记事本", false, true)
		if route != RouteR2 || !allow["computer.act"] {
			t.Fatalf("§8.3 route=%s allow=%v", route, allow)
		}
	})
	t.Run("11-14-video-share-honesty", func(t *testing.T) {
		cases := []struct {
			goal   string
			route  TaskRoute
			must   string
			forbid []string
		}{
			{"https://www.bilibili.com/video/BV1xx411c7mD", RouteR1, "video.understand", []string{"browser.act", "media.play"}},
			{"https://v.douyin.com/x", RouteR1, "video.understand", []string{"browser.act", "media.play"}},
			{"https://v.qq.com/x/page/n001.html", RouteR1, "video.understand", []string{"browser.act"}},
			{"https://youtu.be/dQw4w9wgGc", RouteR1, "video.understand", []string{"browser.act", "media.play"}},
			{"6.66 复制打开抖音 https://v.douyin.com/x", RouteR1, "video.understand", []string{"browser.act"}},
			{"用浏览器打开 https://www.bilibili.com/video/BV1xx", RouteR3, "browser.act", []string{"video.understand"}},
		}
		for _, tc := range cases {
			route, allow := classifyTaskRoute(tc.goal, false, true)
			if route != tc.route || !allow[tc.must] {
				t.Fatalf("%q route=%s allow=%v", tc.goal, route, allow)
			}
			kept := applyTaskRoute(defs, route, allow)
			names := map[string]bool{}
			for _, d := range kept {
				names[d.Name] = true
			}
			if !names[tc.must] {
				t.Fatalf("%q dropped %s", tc.goal, tc.must)
			}
			for _, name := range tc.forbid {
				if names[name] {
					t.Fatalf("%q leaked %s", tc.goal, name)
				}
			}
		}
	})
}
