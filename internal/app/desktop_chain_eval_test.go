package app

import (
	"runtime"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// Local desktop capability eval: phrasing → route → native launch / paste /
// GUI grammar / verifier, without driving a live screen. Failures here are
// product regressions, not flaky UI timing.

func TestDesktopCapabilityEvalSet(t *testing.T) {
	apps := toolruntime.KnownLaunchAppNames()

	t.Run("settings_page_is_r2", func(t *testing.T) {
		goal := "打开蓝牙设置"
		route, allow := classifyTaskRouteApps(goal, false, true, apps)
		if route != RouteR2 || !allow["desktop.open"] {
			t.Fatalf("route=%q allow.open=%v", route, allow["desktop.open"])
		}
		for _, want := range []string{"蓝牙", "壁纸", "锁屏", "蓝牙设置"} {
			found := false
			for _, n := range apps {
				if n == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("routing vocabulary must include native alias %q", want)
			}
		}
		if route, _ := classifyTaskRouteApps("打开蓝牙", false, true, apps); route != RouteR2 {
			t.Fatal("short spoken form 打开蓝牙 must be R2")
		}
		if route, _ := classifyTaskRouteApps("换壁纸", false, true, apps); route != RouteR2 {
			t.Fatal("imperative alias 换壁纸 must be R2")
		}
	})

	t.Run("how_to_question_is_not_desktop_work", func(t *testing.T) {
		if route, _ := classifyTaskRouteApps("蓝牙设置在哪里能找到", false, true, apps); route == RouteR2 {
			t.Fatal("asking where a page lives is not R2")
		}
	})

	t.Run("named_app_act_is_r2", func(t *testing.T) {
		if got := detectAppActRoute("在飞书里给运营群发一句今晚上线", []string{"飞书"}); got != RouteR2 {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("computer_act_run_budget_is_eight", func(t *testing.T) {
		if ccapp.MaxComputerActSteps != 8 {
			t.Fatalf("MaxComputerActSteps=%d", ccapp.MaxComputerActSteps)
		}
		def := computerActToolDefinition()
		if !strings.Contains(def.Description, "2–8 named") {
			t.Fatalf("description still advertises the old budget: %s", def.Description)
		}
		if !strings.Contains(string(def.Schema), `"maxItems":8`) {
			t.Fatalf("schema maxItems drifted: %s", def.Schema)
		}
	})

	t.Run("han_and_long_text_paste", func(t *testing.T) {
		if !ccapp.PreferPasteText("你好，这是一段要粘贴的中文") {
			t.Fatal("CJK must paste")
		}
		if ccapp.PreferPasteText("ok") {
			t.Fatal("short latin still types")
		}
	})

	t.Run("gui_loop_and_verifier_grammar", func(t *testing.T) {
		act, err := parseGUILoopAction(`{"action":"click","markId":"B1","frameId":"frm"}`, false, "frm")
		if err != nil || act.Action != "click" || act.MarkID != "B1" {
			t.Fatalf("gui action: %+v err=%v", act, err)
		}
		v, ok := parseDesktopVerdict(`{"verdict":"done","reason":"设置页已在前台"}`)
		if !ok || v.Verdict != verdictDone {
			t.Fatalf("verdict: %+v ok=%v", v, ok)
		}
	})

	t.Run("empty_observe_tree_is_the_gui_loop_trigger", func(t *testing.T) {
		if !observeReturnedEmptyTree(`{"count":0,"nodes":[]}`) {
			t.Fatal("empty tree must trigger the visual loop")
		}
		if observeReturnedEmptyTree(`{"count":3,"nodes":[{"id":"B1"}]}`) {
			t.Fatal("a real tree must not")
		}
	})

	t.Run("windows_native_pages_stay_on_this_os", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("native Settings URIs are Windows-only")
		}
		if !strings.Contains(strings.Join(apps, "|"), "回收站") {
			t.Fatal("shell folders must be in the launch vocabulary")
		}
	})
}
