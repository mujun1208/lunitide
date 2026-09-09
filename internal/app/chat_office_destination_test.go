package app

import (
	"encoding/json"
	"testing"
)

func TestOfficeDesktopDestinationExcludesNegation(t *testing.T) {
	for _, goal := range []string{
		"生成 Word 周报，保存到会话目录，不放桌面。",
		"生成 Word 周报，不要保存到桌面。",
		"生成 Word 周报，无需复制到桌面。",
		"Generate a Word report, do not save it to the desktop.",
		"Generate a Word report, don't put it on the desktop.",
	} {
		t.Run(goal, func(t *testing.T) {
			if wantsOfficeFileOnDesktop(goal) {
				t.Fatal("negated destination interpreted as desktop output")
			}
			var args struct {
				Desktop bool `json:"desktop"`
			}
			if err := json.Unmarshal(fallbackOfficeGenArgs("html.gen", goal, ""), &args); err != nil {
				t.Fatal(err)
			}
			if args.Desktop {
				t.Fatal("fallback escaped to desktop")
			}
		})
	}
	for _, goal := range []string{
		"生成 Word 周报，放到桌面。",
		"Generate a Word report on the desktop.",
		"不要生成 PDF，生成 Word 周报放到桌面。",
	} {
		if !wantsOfficeFileOnDesktop(goal) {
			t.Errorf("explicit desktop destination lost: %s", goal)
		}
	}
}

func TestOfficeGenerationDoesNotTriggerDesktopFallback(t *testing.T) {
	for _, goal := range []string{
		"生成一份可打开的 Word 周报，保存到会话目录。",
		"生成Word周报，保存到会话目录weekly-report-test.docx，不放桌面，不启动其他应用。",
		"生成Word文档，不要打开其他文件。",
		"生成 Word 周报，文件生成后只告诉我路径。",
	} {
		if got := fallbackDesktopOpenArgs(goal); len(got) != 0 {
			t.Errorf("unrequested open for %q: %s", goal, got)
		}
		if looksLikeDesktopObserveTurn(goal) {
			t.Errorf("unrequested screen capture for %q", goal)
		}
	}
	for goal, want := range map[string]string{
		"帮我打开桌面上的企业AI智能助手.txt": "企业AI智能助手.txt",
		"不启动微信，请打开汽水音乐":        "汽水音乐",
		"先完成报告，然后打开汽水音乐":       "汽水音乐",
	} {
		if got, ok := desktopOpenTargetFromGoal(goal); !ok || got != want {
			t.Errorf("explicit open lost for %q: %q, %v", goal, got, ok)
		}
	}
}
