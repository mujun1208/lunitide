package app

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateTurnFailureNotice(t *testing.T) {
	if got := createTurnFailureNotice([]string{"desktop.open"}, ""); !strings.Contains(got, "还没开始播放") {
		t.Fatalf("desktop-only failure = %q", got)
	}
	if got := createTurnFailureNotice([]string{"media.play"}, ""); !strings.Contains(got, "没能开始播放") {
		t.Fatalf("media failure = %q", got)
	}
	if got := createTurnFailureNotice([]string{"cc.screen_capture"}, ""); !strings.Contains(got, "media.play") {
		t.Fatalf("vision failure = %q", got)
	}
	if got := createTurnFailureNotice([]string{"desktop.open"}, "已完成播放。"); got != "" {
		t.Fatalf("done text must not add failure notice: %q", got)
	}
	if got := createTurnFailureNotice([]string{"excel.gen"}, ""); !strings.Contains(got, "desktop=true") {
		t.Fatalf("excel.gen failure = %q", got)
	}
	if got := createTurnFailureNotice([]string{"docx.gen"}, ""); !strings.Contains(got, "desktop=true") {
		t.Fatalf("docx.gen failure = %q", got)
	}
}

func TestCreateTurnClosingNotice(t *testing.T) {
	if got := createTurnClosingNotice([]string{"workspace.write", "skill.create"}, ""); !strings.Contains(got, "技能中心") {
		t.Fatalf("skill.create notice = %q", got)
	}
	if got := createTurnClosingNotice([]string{"plugin.create"}, ""); !strings.Contains(got, "插件") {
		t.Fatalf("plugin.create notice = %q", got)
	}
	if got := createTurnClosingNotice([]string{"workspace.read"}, ""); got != "" {
		t.Fatalf("unrelated tools should not close the turn: %q", got)
	}
	if got := createTurnClosingNotice([]string{"workspace.write"}, ""); got != "" {
		t.Fatalf("acting tool must not say 我已经做完了: %q", got)
	}
	if got := createTurnClosingNotice([]string{"command.run", "web.fetch"}, "文件已写入，已完成。"); got != "" {
		t.Fatalf("already-done text must not spam: %q", got)
	}
	if got := createTurnClosingNotice([]string{"html.gen"}, ""); got != "" {
		t.Fatalf("html.gen must not say 我已经做完了: %q", got)
	}
	if got := createTurnClosingNotice([]string{"desktop.open"}, ""); got != "" {
		t.Fatalf("desktop.open must not say 我已经做完了: %q", got)
	}
	if got := createTurnClosingNotice([]string{"web.search"}, ""); got != "" {
		t.Fatalf("web.search must not say 我已经做完了: %q", got)
	}
}

func TestCompanionPersonaForbidsTaskDonePhrases(t *testing.T) {
	got := companionPersonaInstruction()
	for _, phrase := range []string{"我做完了", "我已经做完了", "任务已完成", "禁止内部思考", "我想想", "无法执行", "想聊点什么", "desktop.type"} {
		if !strings.Contains(got, phrase) {
			t.Fatalf("persona must mention %q", phrase)
		}
	}
}

func TestTurnOutcomeNotice(t *testing.T) {
	if got := turnOutcomeNotice(true, errors.New("upstream")); got != turnInterruptNotice {
		t.Fatalf("stop must win over error: %q", got)
	}
	if got := turnOutcomeNotice(false, errors.New("upstream")); !strings.HasPrefix(got, turnErrorNotice) || !strings.Contains(got, "desktop=true") {
		t.Fatalf("failed notice = %q", got)
	}
	if strings.Contains(turnOutcomeNotice(false, errors.New("upstream")), "模型请求失败") {
		t.Fatal("generic upstream must not leak 模型请求失败")
	}
	if got := turnOutcomeNotice(false, nil); got != "" {
		t.Fatalf("success must not add outcome notice: %q", got)
	}
	next, delta := appendAssistantNotice("正在写文件", turnInterruptNotice)
	if !strings.Contains(next, "终止打断了") || delta == "" {
		t.Fatalf("append interrupt = %q %q", next, delta)
	}
	again, empty := appendAssistantNotice(next, turnInterruptNotice)
	if empty != "" || again != next {
		t.Fatalf("duplicate notice leaked: %q %q", again, empty)
	}
}

func TestDuplicateToolSkipSummary(t *testing.T) {
	done := map[string]string{"abc": "wrote file"}
	if summary, skip := duplicateToolSkipSummary("abc", done); !skip || summary != duplicateToolResult {
		t.Fatalf("skip = %v %q", skip, summary)
	}
	if _, skip := duplicateToolSkipSummary("new", done); skip {
		t.Fatal("unseen digest must execute")
	}
}

func TestExpertPersonaHeaderAndClip(t *testing.T) {
	single := expertPersonaHeader(1)
	if strings.Contains(single, "专家理事会") || strings.Contains(single, "5–6 轮") {
		t.Fatalf("single expert must stay persona: %q", single)
	}
	council := expertPersonaHeader(3)
	if !strings.Contains(council, "会议主席") || !strings.Contains(council, "5–6 轮") || !strings.Contains(council, "思考") {
		t.Fatalf("council prompt = %q", council)
	}
	if !strings.Contains(council, "不要把每位专家的发言拆成多条助手消息") {
		t.Fatalf("council must forbid extra bubbles: %q", council)
	}
	for _, prompt := range []string{single, council} {
		for _, needle := range []string{"skill.invoke", "web.search", "mermaid", "docx.gen", "pptx.gen", "desktop=true"} {
			if !strings.Contains(prompt, needle) {
				t.Fatalf("persona header missing %q: %q", needle, prompt)
			}
		}
	}
	short := clipExpertBody([]byte("岗位说明"))
	if short != "岗位说明" {
		t.Fatalf("short clip = %q", short)
	}
	long := []rune(strings.Repeat("专", expertSectionMaxRunes+80))
	clipped := clipExpertBody([]byte(string(long)))
	if !strings.Contains(clipped, "已截断") {
		t.Fatalf("expected truncation, got len=%d", len([]rune(clipped)))
	}
	if n := len([]rune(clipped)); n > expertSectionMaxRunes+20 {
		t.Fatalf("clipped too large: %d", n)
	}
}

func TestSkipExpertCouncilOnSimpleComputerUse(t *testing.T) {
	if !skipExpertCouncil("帮我在桌面创建一个文件夹，名字叫小宝") {
		t.Fatal("create-folder must skip council")
	}
	if !skipExpertCouncil("帮我设计一个点球大战的网页小游戏，在桌面可以直接试玩") {
		t.Fatal("desktop html game must skip council")
	}
	if !skipExpertCouncil("打开网站播放音乐") {
		t.Fatal("open website and play music must skip council")
	}
	if skipExpertCouncil("请三位专家一起评审这份架构方案") {
		t.Fatal("architecture review must keep council")
	}
}

func TestCollectExpertIDsPrefersMountedPack(t *testing.T) {
	const mounted = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	const extra = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	got := collectExpertIDs([]string{mounted}, "[引用专家 安全|"+extra+"]\n[引用专家 重复|"+mounted+"]")
	if len(got) != 2 || got[0] != mounted || got[1] != extra {
		t.Fatalf("got %#v", got)
	}
}
