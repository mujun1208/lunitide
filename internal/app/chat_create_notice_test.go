package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
)

func TestCreateTurnFailureNotice(t *testing.T) {
	if got := createTurnFailureNotice([]string{"desktop.open"}, ""); got != "" {
		t.Fatalf("successful open-only must not look like failure: %q", got)
	}
	if got := createTurnFailureNotice([]string{"media.play"}, ""); !strings.Contains(got, "没能开始播放") {
		t.Fatalf("media failure = %q", got)
	}
	if got := createTurnFailureNotice([]string{"cc.screen_capture"}, ""); !strings.Contains(got, "电脑操作未能完成") || strings.Contains(got, "media.play") {
		t.Fatalf("vision failure = %q", got)
	}
	if got := createTurnFailureNotice([]string{"desktop.open"}, "已完成播放。"); got != "" {
		t.Fatalf("done text must not add failure notice: %q", got)
	}
	if got := createTurnFailureNotice([]string{"excel.gen"}, ""); !strings.Contains(got, "生成失败") {
		t.Fatalf("excel.gen failure = %q", got)
	}
	if got := createTurnFailureNotice([]string{"docx.gen"}, ""); !strings.Contains(got, "生成失败") {
		t.Fatalf("docx.gen failure = %q", got)
	}
}

func TestCreateTurnClosingNotice(t *testing.T) {
	if got := createTurnClosingNotice([]string{"workspace.write", "skill.create"}, ""); !strings.Contains(got, "技能中心") {
		t.Fatalf("skill.create notice = %q", got)
	}
	if got := createTurnClosingNotice([]string{"plugin.create"}, ""); !strings.Contains(got, "能力包") {
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
	if got := createTurnClosingNotice([]string{"excel.gen", "skill.invoke"}, ""); !strings.Contains(got, "技能草稿") {
		t.Fatalf("complex office turn should offer skill draft: %q", got)
	}
}

func TestCompanionPersonaForbidsTaskDonePhrases(t *testing.T) {
	got := companionPersonaChatInstruction()
	for _, phrase := range []string{"我做完了", "我已经做完了", "任务已完成", "禁止内部思考", "我想想", "无法执行", "想聊点什么"} {
		if !strings.Contains(got, phrase) {
			t.Fatalf("chat persona must mention %q", phrase)
		}
	}
	if strings.Contains(got, "desktop.open") || strings.Contains(got, "desktop.type") {
		t.Fatal("idle chat persona must not inject the desktop cookbook")
	}
	tools := companionPersonaToolsInstruction()
	for _, phrase := range []string{"desktop.type", "desktop.open", "media.play"} {
		if !strings.Contains(tools, phrase) {
			t.Fatalf("tools persona must mention %q", phrase)
		}
	}
	if !strings.Contains(tools, "command.run") || !strings.Contains(tools, "桌面") || !strings.Contains(tools, "mkdir") {
		t.Fatal("desktop folder create must tell the model to mkdir on the real Desktop via command.run")
	}
}

func TestCompanionIdleChatOmitsDesktopCookbook(t *testing.T) {
	got := companionPersonaChatInstruction()
	if !strings.Contains(got, "闲聊立刻回答") {
		t.Fatal("idle chat must still say to answer immediately")
	}
	if strings.Contains(got, "desktop.open") {
		t.Fatal("你好-style idle chat must not carry desktop.open instructions")
	}
}

func TestClipCancelledCompanionPersist(t *testing.T) {
	if got := clipCancelledCompanionPersist("今晚月色很好，适合出门。后半句还没读"); got != "今晚月色很好，适合出门。" {
		t.Fatalf("clip = %q", got)
	}
	if got := clipCancelledCompanionPersist("还没有标点"); got != "" {
		t.Fatalf("unspoken stream must not persist: %q", got)
	}
	if got := clipCancelledCompanionPersistToSpoken("今晚月色很好，适合出门。后半句还没读", "今晚月色很好，"); got != "今晚月色很好，" {
		t.Fatalf("spoken prefix clip = %q", got)
	}
}

func TestTurnOutcomeNotice(t *testing.T) {
	if got := turnOutcomeNotice(true, errors.New("upstream"), "", nil); got != turnInterruptNotice {
		t.Fatalf("stop must win over error: %q", got)
	}
	got := turnOutcomeNotice(false, errors.New("upstream"), "", nil)
	if !strings.HasPrefix(got, turnErrorNotice) {
		t.Fatalf("failed notice = %q", got)
	}
	for _, leak := range []string{"写到桌面请用", "desktop=true", "*.gen", "不要用 command.run", "模型请求失败"} {
		if strings.Contains(got, leak) {
			t.Fatalf("failed notice leaked %q: %q", leak, got)
		}
	}
	if got := turnOutcomeNotice(false, nil, "", nil); got != "" {
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
	single := expertPersonaHeader(1, "PPT专家")
	if strings.Contains(single, "专家理事会") || strings.Contains(single, "5–6 轮") || strings.Contains(single, "你是月汐主编排") {
		t.Fatalf("single expert must stay persona: %q", single)
	}
	if !strings.Contains(single, "你就是「PPT专家」") {
		t.Fatalf("single expert must be the named person: %q", single)
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
	if !skipExpertCouncil("帮我删掉桌面上的可可") {
		t.Fatal("desktop delete must skip council")
	}
	if !skipExpertCouncil("copy this file to my desktop") {
		t.Fatal("desktop copy must skip council")
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

func TestBundledWorkflowInjectionTrimsByIntent(t *testing.T) {
	idle := bundledWorkflowInjection("")
	if !strings.Contains(idle, "[内置工作流]") {
		t.Fatalf("idle turn should keep a one-line workflow header: %q", idle)
	}
	if strings.Contains(idle, "html.gen") || strings.Contains(idle, "九步流水线") || strings.Contains(idle, "cc.screen_capture") {
		t.Fatalf("idle turn must not dump the full blob: %q", idle)
	}
	if got := bundledWorkflowInjection("你好"); strings.Contains(got, "cc.screen_capture") || strings.Contains(got, "九步流水线") {
		t.Fatalf("idle chat leaked desktop/office blob: %q", got)
	}
	play := bundledWorkflowInjection("随便播一首歌")
	if !strings.Contains(play, "media.play") {
		t.Fatalf("play turn missing media.play: %q", play)
	}
	if strings.Contains(play, "cc.screen_capture") || strings.Contains(play, "九步流水线") {
		t.Fatalf("play turn leaked desktop/office blob: %q", play)
	}
	weather := bundledWorkflowInjection("查北京明天天气")
	if !strings.Contains(weather, "web.search") {
		t.Fatalf("weather turn missing search: %q", weather)
	}
	if strings.Contains(weather, "computer.act") || strings.Contains(weather, "九步流水线") || strings.Contains(weather, "cc.screen_capture") {
		t.Fatalf("weather turn leaked desktop/office workflow: %q", weather)
	}
}

func TestShouldOfferSkillDraft(t *testing.T) {
	if shouldOfferSkillDraft([]string{"web.search"}) {
		t.Fatal("single lookup must not offer a skill draft")
	}
	if !shouldOfferSkillDraft([]string{"workspace.edit", "command.run", "workspace.write"}) {
		t.Fatal("multi-step mutating turn should offer a skill draft")
	}
	if !shouldOfferSkillDraft([]string{"desktop.open", "computer.act", "desktop.type"}) {
		t.Fatal("desktop open+act+type is a reusable trajectory")
	}
	if shouldOfferSkillDraft([]string{"workspace.edit", "command.run", "workspace.write", "skill.create"}) {
		t.Fatal("already created a skill — do not offer again")
	}
}

func TestShouldOfferDesktopSkillDraft(t *testing.T) {
	if shouldOfferDesktopSkillDraft([]string{"computer.act"}, 1, verdictNotDone) {
		t.Fatal("failed screen must not become a skill")
	}
	if shouldOfferDesktopSkillDraft([]string{"computer.act"}, 1, verdictBlocked) {
		t.Fatal("blocked screen must not become a skill")
	}
	if !shouldOfferDesktopSkillDraft([]string{"computer.act"}, 1, verdictDone) {
		t.Fatal("a finished GUI loop is already a multi-step recipe")
	}
	if !shouldOfferDesktopSkillDraft([]string{"desktop.open", "computer.act"}, 0, verdictDone) {
		t.Fatal("verified native-open then act should be saved")
	}
	if shouldOfferDesktopSkillDraft([]string{"desktop.open"}, 0, verdictDone) {
		t.Fatal("a single launch is not a recipe")
	}
	if shouldOfferDesktopSkillDraft([]string{"computer.act", "skill.create"}, 2, verdictDone) {
		t.Fatal("already created a skill")
	}
}

func TestShouldOfferAnySkillDraft(t *testing.T) {
	if offer, _ := shouldOfferAnySkillDraft([]string{"computer.act", "desktop.type", "desktop.open"}, 0, verdictNotDone, false); offer {
		t.Fatal("failed desktop must not fall through to a generic skill draft")
	}
	if offer, _ := shouldOfferAnySkillDraft([]string{"workspace.edit", "command.run", "workspace.write"}, 0, verdictBlocked, false); offer {
		t.Fatal("blocked desktop must not offer a skill")
	}
	if offer, _ := shouldOfferAnySkillDraft([]string{"computer.act"}, 1, verdictDone, true); offer {
		t.Fatal("companion turns never offer a skill draft")
	}
	offer, desktop := shouldOfferAnySkillDraft([]string{"computer.act"}, 1, verdictDone, false)
	if !offer || !desktop {
		t.Fatal("finished GUI loop should offer a desktop skill")
	}
	offer, desktop = shouldOfferAnySkillDraft([]string{"workspace.edit", "command.run", "workspace.write"}, 0, "", false)
	if !offer || desktop {
		t.Fatal("generic mutating turn should still offer a skill")
	}
}

func TestDesktopSkillRecipeKeepsDesktopSteps(t *testing.T) {
	recipe := formatDesktopSkillRecipe("打开蓝牙设置", []turnToolReceipt{
		{Name: "desktop.open", Args: json.RawMessage(`{"name":"蓝牙设置"}`)},
		{Name: "web.search", Args: json.RawMessage(`{"q":"x"}`)},
	})
	if !strings.Contains(recipe, "desktop.open") || strings.Contains(recipe, "web.search") {
		t.Fatalf("recipe should keep desktop steps only: %q", recipe)
	}
	msg := desktopSkillDraftOfferMessage("打开蓝牙设置", []turnToolReceipt{{Name: "desktop.open", Args: json.RawMessage(`{"name":"蓝牙设置"}`)}})
	if !strings.Contains(msg.Content, "skill.create") || !strings.Contains(msg.Content, "蓝牙设置") {
		t.Fatalf("offer: %q", msg.Content)
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

func TestClipExpertBodyToTokens(t *testing.T) {
	// A short body under budget is returned verbatim.
	if got := clipExpertBodyToTokens("岗位说明", 100); got != "岗位说明" {
		t.Fatalf("short body must pass through: %q", got)
	}
	// maxTokens<=0 means no cap.
	long := strings.Repeat("专", 5000)
	if got := clipExpertBodyToTokens(long, 0); got != long {
		t.Fatalf("zero budget must not clip")
	}
	// A body over budget is trimmed to fit the token ceiling.
	clipped := clipExpertBodyToTokens(long, 200)
	if token.EstimateTokens(clipped) > 200+token.EstimateTokens("\n…（岗位说明书已按上下文预算精简）") {
		t.Fatalf("clipped body exceeds token budget: %d", token.EstimateTokens(clipped))
	}
	if !strings.Contains(clipped, "按上下文预算精简") {
		t.Fatalf("clipped body must note the trim: %q", clipped)
	}
}

func TestExpertInjectionTokenBudget(t *testing.T) {
	// Unknown window falls back to the default and applies the ratio.
	p := provider.Provider{}
	got := expertInjectionTokenBudget(p, "missing")
	want := int64(float64(defaultExpertBudgetContextWindow) * expertInjectionCeilingRatio)
	if got != want {
		t.Fatalf("fallback budget = %d, want %d", got, want)
	}
	// A known small window still keeps the per-expert floor.
	small := provider.Provider{Models: []provider.Model{{ModelID: "m", ContextWindow: 100}}}
	if got := expertInjectionTokenBudget(small, "m"); got < expertPersonaMinPerExpertTokens {
		t.Fatalf("budget must not drop below the floor: %d", got)
	}
	// A large window scales by the ratio.
	big := provider.Provider{Models: []provider.Model{{ModelID: "m", ContextWindow: 200000}}}
	if got := expertInjectionTokenBudget(big, "m"); got != int64(200000*expertInjectionCeilingRatio) {
		t.Fatalf("large window budget = %d", got)
	}
}
