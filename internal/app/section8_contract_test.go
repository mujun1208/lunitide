package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestSection8ManualContracts(t *testing.T) {
	t.Parallel()
	defs := append(engineToolDefinitions(),
		llmadapter.ToolDefinition{Name: "computer.act"},
		llmadapter.ToolDefinition{Name: "browser.act"},
		llmadapter.ToolDefinition{Name: "kb.search"},
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
			goal  string
			route TaskRoute
			must  string
		}{
			{"https://www.bilibili.com/video/BV1xx411c7mD", RouteR1, "video.understand"},
			{"https://v.douyin.com/x", RouteR1, "video.understand"},
			{"https://v.qq.com/x/page/n001.html", RouteR1, "video.understand"},
			{"https://youtu.be/dQw4w9wgGc", RouteR1, "video.understand"},
			{"6.66 复制打开抖音 https://v.douyin.com/x", RouteR1, "video.understand"},
			{"用浏览器打开 https://www.bilibili.com/video/BV1xx", RouteR3, "browser.act"},
		}
		for _, tc := range cases {
			route, allow := classifyTaskRoute(tc.goal, false, true)
			if route != tc.route || !allow[tc.must] {
				t.Fatalf("%q route=%s allow=%v", tc.goal, route, allow)
			}
			// v2 FR-100: the route only feeds prompts and budget — the full
			// surface keeps the must-have family and everything else.
			names := map[string]bool{}
			for _, d := range defs {
				names[d.Name] = true
			}
			if !names[tc.must] {
				t.Fatalf("%q dropped %s", tc.goal, tc.must)
			}
		}
	})
}
