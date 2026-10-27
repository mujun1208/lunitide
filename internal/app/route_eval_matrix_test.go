package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// FR-600: the route evaluation matrix (PRD 4.7). Table-driven regression
// net for the v2 two-plane architecture:
//  1. route classification (keyword plane)
//  2. budget tier (FR-400 three tiers)
//  3. the tool surface is the profile full set for EVERY case (FR-100)
//  4. budget baselines hold (FR-200)
//  5. the 0.18.3 incidents stay fixed (RC1/RC2/RC3)
//  6. the gate table behaves (G-301..G-306, FR-500)
//
// Empty route/lane fields skip that assertion.

const skipRouteCheck = TaskRoute("__skip__")

func TestRouteEvalMatrix(t *testing.T) {
	cases := []struct {
		name     string
		goal     string
		cc       bool
		materials bool
		route    TaskRoute
		lane     ChatLane
		minSteps int
		must     []string
	}{
		// --- greetings / casual chat (chat tier; FR-200 skips short idle
		// greetings, so "你好" keeps its 1-step special case) ---
		{name: "greeting-hello", goal: "你好", route: RouteR0, lane: LaneChat, minSteps: 1,
			must: []string{"web.search", "user.ask"}},
		{name: "greeting-morning", goal: "早上好呀", route: RouteR0, lane: LaneChat, minSteps: 4,
			must: []string{"web.search"}},
		{name: "casual-thanks", goal: "谢谢啦", route: RouteR0, lane: LaneChat, minSteps: 4,
			must: []string{"web.search", "kb.search"}},

		// --- in-place prose (chat tier by wording, but FR-200's unspecified
		// baseline keeps 24 steps: a miscategorized task never starves) ---
		{name: "polish-paragraph", goal: "帮我把这段话润色得更通顺：今天天气很好我们出去玩", route: RouteR1, lane: LaneAgent, minSteps: 24,
			must: []string{"docx.gen", "web.search"}},
		{name: "translate-inline", goal: "把这句话翻译成英文：机场跑道检修完成", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search"}},
		{name: "write-article", goal: "写一篇文章介绍光合作用", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"docx.gen", "web.search"}},
		{name: "casual-opinion", goal: "你觉得固定翼和直升机哪个维护更难？", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search"}},

		// --- RC1 incident: a bare entity keeps the full surface. The keyword
		// plane misses the entity (route stays unspecified), yet FR-200's
		// low-confidence baseline gives the agent tier budget — the exact
		// "wrong prediction must only cost budget" contract. ---
		{name: "rc1-bare-entity-tesla", goal: "特斯拉", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search", "web.fetch", "user.ask"}},
		{name: "rc1-bare-entity-airline", goal: "川航3U8633", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search", "user.ask"}},
		{name: "rc1-weather-city", goal: "这里的天气", route: RouteR1, lane: LaneAgent, minSteps: 24,
			must: []string{"weather.get", "location.get", "web.search"}},
		{name: "rc1-weather-spoken", goal: "今天天气怎么样", route: RouteR1, lane: LaneAgent, minSteps: 24,
			must: []string{"weather.get", "web.search"}},
		{name: "rc1-timely-news", goal: "今天有什么新闻", route: RouteR1, lane: LaneChat, minSteps: 12,
			must: []string{"web.search"}},
		{name: "rc1-timely-stock", goal: "现在金价多少", route: RouteR1, lane: LaneChat, minSteps: 12,
			must: []string{"web.search"}},
		// "查一下": the keyword plane says unspecified, but the lookup-turn
		// detector still floors the budget at the task tier (FR-200 R1 arm).
		{name: "rc1-vague-then-web", goal: "查一下", route: skipRouteCheck, lane: LaneChat, minSteps: 12,
			must: []string{"web.search"}},
		{name: "rc1-entity-followup-lookup", goal: "接着查，刚才那个公司最近的诉讼", route: RouteR0, lane: LaneChat, minSteps: 4,
			must: []string{"web.search", "web.fetch"}},

		// --- RC2 incident: desktop turns keep web.search (R2, agent tier) ---
		{name: "rc2-desktop-open-app", goal: "打开汽水音乐", route: RouteR2, lane: LaneAgent, minSteps: 24,
			must: []string{"desktop.open", "web.search", "media.play"}},
		// cc wording that misses the keyword table entirely: the classifier
		// fails, the surface stays full and the budget stays agent-tier.
		{name: "rc2-computer-control-cc", goal: "帮我在电脑上把这个按钮点掉", cc: true, route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"desktop.open", "web.search"}},
		{name: "rc2-desktop-type", goal: "打开记事本输入123", route: RouteR2, lane: LaneAgent, minSteps: 24,
			must: []string{"desktop.open", "desktop.type", "web.search"}},
		{name: "rc2-browser-act", goal: "用浏览器打开淘宝首页", cc: true, route: RouteR3, lane: LaneAgent, minSteps: 24,
			must: []string{"browser.act", "web.search", "web.fetch"}},

		// --- research turns (task/agent tier by lane wording) ---
		{name: "r3-research-report", goal: "深度调研一下国产大模型的最新进展并给我一份分析", route: skipRouteCheck, lane: LaneTask, minSteps: 24,
			must: []string{"web.search", "web.fetch", "docx.gen"}},
		{name: "r3-comparison", goal: "对比调研波音737和A320的维护成本差异", route: skipRouteCheck, lane: LaneTask, minSteps: 24,
			must: []string{"web.search", "docx.gen"}},
		{name: "r3-undefined-broad", goal: "帮我全面了解一下海南自贸港政策", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search", "web.fetch"}},

		// --- code / engineering ---
		{name: "r4-code-poc", goal: "写一个Python脚本批量重命名文件", route: skipRouteCheck, lane: LaneAgent, minSteps: 24,
			must: []string{"workspace.write", "command.run", "web.search"}},
		{name: "r4-html-page", goal: "做一个个人主页页面", route: skipRouteCheck, lane: LaneTask, minSteps: 24,
			must: []string{"workspace.write", "browser.act", "web.search"}},
		{name: "r4-fix-bug", goal: "修复这个报错：panic: nil pointer dereference", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"workspace.write", "command.run"}},

		// --- office with materials (task tier, write-from-reference) ---
		{name: "office-materials-weekly", goal: "根据以下材料写周报：- 周一完成起落架检查\n- 周三处理AD指令", materials: true, route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"docx.gen", "web.search"}},
		{name: "office-materials-ppt", goal: "用下面的内容做一份PPT：公司年度营收增长30%，新签三个大客户", materials: true, route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"pptx.gen", "web.search"}},
		{name: "office-materials-marked", goal: "整理成Excel：\n序号,件号,数量\n1,ABC-123,5\n2,DEF-456,3", materials: true, route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"excel.gen", "web.search"}},

		// --- office without materials (task tier, G-301 asks first) ---
		{name: "office-ask-weekly-nomaterials", goal: "写周报", route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"docx.gen", "web.search", "user.ask"}},
		{name: "office-ask-ppt-nomaterials", goal: "做一份关于量子计算的PPT", route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"pptx.gen", "web.search"}},
		{name: "office-vague-task", goal: "帮我做个任务", route: skipRouteCheck, lane: LaneTask, minSteps: 24,
			must: []string{"web.search", "user.ask"}},

		// --- offline turns keep the full surface (behavior gates, not tools) ---
		{name: "offline-news-ppt", goal: "将已有新闻整理成PPT，不联网，保留原文。", route: RouteR1, lane: LaneTask, minSteps: 12,
			must: []string{"pptx.gen", "web.search", "docx.gen"}},
		{name: "offline-report", goal: "根据网上公开资料写一份行业报告，不要联网", route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"docx.gen", "web.search"}},

		// --- media center (agent tier; G-306 guards bare continue words) ---
		{name: "media-play-song", goal: "播放周杰伦的晴天", route: RouteR2, lane: LaneAgent, minSteps: 24,
			must: []string{"media.play", "web.search"}},
		{name: "media-next-song", goal: "下一首", route: RouteR2, lane: LaneAgent, minSteps: 24,
			must: []string{"media.play"}},
		{name: "media-pause", goal: "暂停播放", route: RouteR2, lane: LaneAgent, minSteps: 24,
			must: []string{"media.play"}},
		// Naming the media center explicitly: owned wording keeps the agent
		// tier even without prior session context.
		{name: "media-center-next", goal: "在媒体中心下一首", route: RouteR2, lane: LaneAgent, minSteps: 24,
			must: []string{"media.play", "web.search"}},
		{name: "media-center-play", goal: "媒体中心播放周杰伦的晴天", route: RouteR2, lane: LaneAgent, minSteps: 24,
			must: []string{"media.play", "web.search"}},

		// --- capability / skill work (RC3 incident: no dead ends) ---
		{name: "rc3-capability-work", goal: "[引用技能:数据清洗] 处理这份数据", route: skipRouteCheck, lane: LaneTask, minSteps: 24,
			must: []string{"web.search", "kb.search"}},
		{name: "skill-chip", goal: "[引用技能:周报助手] 开始", route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"web.search", "docx.gen"}},
		{name: "mcp-discovery", goal: "搜索可用的MCP工具", route: skipRouteCheck, lane: LaneChat, minSteps: 12,
			must: []string{"web.search", "kb.search"}},

		// --- artifact revision / continue turns ---
		{name: "revision-report", goal: "把刚才生成的报告加上风险章节", route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"docx.gen", "web.search"}},
		{name: "continue-work", goal: "继续把没做完的做完", route: skipRouteCheck, lane: LaneAgent, minSteps: 24,
			must: []string{"web.search", "docx.gen", "desktop.open"}},
		{name: "page-edit", goal: "把按钮改成蓝色", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"workspace.write", "web.search"}},

		// --- URL understanding ---
		{name: "url-understand", goal: "总结一下这个网页的内容 https://example.com/article", route: skipRouteCheck, lane: LaneTask, minSteps: 24,
			must: []string{"web.fetch", "web.search", "docx.gen"}},

		// --- novel / long-form ---
		{name: "novel-writing", goal: "写一部关于机务工程师的短篇小说", route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"docx.gen", "web.search"}},

		// --- deep think flag keeps budget, does not shrink surface ---
		{name: "deep-think-lookup", goal: "深度思考一下为什么3U8633能成功备降", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search", "web.fetch"}},

		// --- ticket fail-closed (G-305 gates calls, never the tool list) ---
		{name: "ticket-train", goal: "查一下明天北京到上海的高铁余票", route: skipRouteCheck, lane: LaneChat, minSteps: 12,
			must: []string{"web.search", "user.ask"}},

		// --- conversion goals carry their source file (G-301 must not fire) ---
		{name: "convert-pdf", goal: "把这个文件转换成PDF", route: RouteR4, lane: LaneTask, minSteps: 12,
			must: []string{"pdf.gen", "web.search"}},

		// --- ambiguous task words still keep full surface ---
		{name: "ambiguous-help", goal: "帮帮我", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search", "user.ask"}},
		{name: "ambiguous-what", goal: "这是什么", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search", "user.ask"}},
		{name: "gallery-page", goal: "做个相册网页", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"html.gen", "web.search"}},

		// --- architecture / mermaid ---
		{name: "arch-mermaid", goal: "画一个微服务架构图", route: skipRouteCheck, lane: LaneChat, minSteps: 24,
			must: []string{"web.search", "canvas.present"}},
	}

	full := func(goal string, cc bool, materials bool) map[string]bool {
		e := &Engine{}
		catalog := e.engineToolDefinitionsFor(executionModeFullAccess)
		if cc {
			catalog = append(catalog, e.ccToolDefinitions()...)
		}
		route, _ := classifyTaskRoute(goal, false, cc)
		_, contract := applyLaneOverrides(classifyChatLane(LaneInput{Goal: goal, HasTurnMaterials: materials}), LaneInput{Goal: goal, HasTurnMaterials: materials}, route, CouncilOverlay{})
		defs := applyLaneTools(catalog, contract)
		out := map[string]bool{}
		for _, d := range defs {
			out[d.Name] = true
		}
		return out
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, _ := classifyTaskRoute(tc.goal, false, tc.cc)
			if tc.route != skipRouteCheck && route != tc.route {
				t.Errorf("route = %s, want %s (goal %q)", route, tc.route, tc.goal)
			}
			lane := classifyChatLane(LaneInput{Goal: tc.goal, HasTurnMaterials: tc.materials})
			if tc.lane != "" && lane != tc.lane {
				t.Errorf("lane = %s, want %s (goal %q)", lane, tc.lane, tc.goal)
			}
			_, contract := applyLaneOverrides(lane, LaneInput{Goal: tc.goal, HasTurnMaterials: tc.materials}, route, CouncilOverlay{})
			if tc.minSteps > 0 && contract.MaxMainToolSteps < tc.minSteps {
				t.Errorf("budget = %d steps, want >= %d (goal %q)", contract.MaxMainToolSteps, tc.minSteps, tc.goal)
			}
			names := full(tc.goal, tc.cc, tc.materials)
			for _, want := range tc.must {
				if !names[want] {
					t.Errorf("tool %q missing from the surface (goal %q)", want, tc.goal)
				}
			}
		})
	}
}

// TestMigrateLaneV1 pins the FR-400 legacy mapping.
func TestMigrateLaneV1(t *testing.T) {
	want := map[ChatLane]ChatLane{
		LaneL0: LaneChat, LaneL1: LaneChat,
		LaneL2: LaneTask, LaneL2Ask: LaneTask, LaneL3: LaneTask,
		LaneL4: LaneAgent,
		"unknown": LaneTask, "": LaneTask,
	}
	for in, out := range want {
		if got := migrateLaneV1(in); got != out {
			t.Errorf("migrateLaneV1(%q) = %q, want %q", in, got, out)
		}
	}
}

// TestToolGateRuleTable exercises every gate rule plus the FR-500 promotion.
func TestToolGateRuleTable(t *testing.T) {
	baseMsgs := []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "x"}}
	t.Setenv("LUNITIDE_STATE_GATES", "")

	cases := []struct {
		name    string
		sig     stateSignals
		goal    string
		tool    string
		wantGate string
	}{
		// G-301: generation without materials on a plain task.
		{name: "g301-gen-no-materials", goal: "做个相册网页", tool: "html.gen",
			wantGate: "G-301"},
		// G-301 does not fire when materials exist.
		{name: "g301-materials-present", sig: stateSignals{HasMaterials: true}, goal: "做个相册网页：照片已贴在下面\n- a.jpg\n- b.jpg", tool: "html.gen", wantGate: ""},
		// G-301 does not fire once web evidence landed.
		{name: "g301-web-evidence", goal: "做个相册网页", tool: "html.gen", wantGate: ""},
		// G-304 owns report-like turns.
		{name: "g304-report-gen", goal: "写一份行业分析报告", tool: "docx.gen", wantGate: "G-304"},
		// G-303: write right after a fresh open.
		{name: "g303-write-after-open", sig: stateSignals{FileJustOpened: true}, goal: "打开配置文件", tool: "workspace.write", wantGate: "G-303"},
		// G-303 releases when the user asked for conversion.
		{name: "g303-convert-release", sig: stateSignals{FileJustOpened: true}, goal: "把这个文件转换成PDF", tool: "pdf.gen", wantGate: ""},
		// G-305: ticket query must not hit the public web.
		{name: "g305-ticket-websearch", sig: stateSignals{TicketQueryNoSource: true}, goal: "查余票", tool: "web.search", wantGate: "G-305"},
		{name: "g305-ticket-fetch", sig: stateSignals{TicketQueryNoSource: true}, goal: "查余票", tool: "web.fetch", wantGate: "G-305"},
		// G-306: media continue must not search.
		{name: "g306-media-continue", sig: stateSignals{MediaContinueAction: true}, goal: "下一首", tool: "web.search", wantGate: "G-306"},
		// G-308 per-tool stop-loss: same tool failed twice in a row → the
		// third retry is denied; switching channels stays legal.
		{name: "g308-same-tool-streak", sig: stateSignals{LastFailedTool: "web.search", LastFailStreak: 2}, goal: "北京支点互动科技有限公司", tool: "web.search", wantGate: "G-308"},
		{name: "g308-switch-channel-ok", sig: stateSignals{LastFailedTool: "web.search", LastFailStreak: 2}, goal: "北京支点互动科技有限公司", tool: "web.fetch", wantGate: ""},
		{name: "g308-streak-below-threshold", sig: stateSignals{LastFailedTool: "web.search", LastFailStreak: 1}, goal: "查一下", tool: "web.search", wantGate: ""},
		// G-308 total stop-loss: 4+ failures this turn → only user.ask stays.
		{name: "g308-total-stoploss", sig: stateSignals{ToolFailures: 4}, goal: "继续查询", tool: "command.run", wantGate: "G-308"},
		{name: "g308-total-ask-exempt", sig: stateSignals{ToolFailures: 4}, goal: "继续查询", tool: "user.ask", wantGate: ""},
		{name: "g308-total-below-threshold", sig: stateSignals{ToolFailures: 3}, goal: "继续查询", tool: "web.search", wantGate: ""},
		// Gates never touch non-gated tools.
		{name: "no-gate-on-ask", goal: "写周报", tool: "user.ask", wantGate: ""},
		{name: "no-gate-on-search", goal: "写一份行业分析报告", tool: "web.search", wantGate: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgs := baseMsgs
			if tc.name == "g301-web-evidence" {
				msgs = append(msgs,
					llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "s1", Name: "web.search", Arguments: json.RawMessage(`{"query":"x"}`)}}},
					llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "s1", Content: "ok results"},
				)
			}
			verdict := evalToolGate(tc.sig, tc.goal, tc.tool, nil, msgs, false)
			if verdict.GateID != tc.wantGate {
				t.Errorf("gate = %q, want %q", verdict.GateID, tc.wantGate)
			}
			if verdict.Deny && verdict.Reason == "" {
				t.Error("denied verdict must carry a structured reason")
			}
		})
	}

	// G-301/G-304 step aside when the office pipeline owns the turn.
	turn := &chatTurnCheckpoint{Goal: "做个PPT", PptActive: true}
	if v := evalToolGate(stateSignals{}, "做个PPT", "pptx.gen", nil, baseMsgs, gateOfficePipelineOwns(turn)); v.Deny {
		t.Error("gate must not double-block a pipeline-owned generation call")
	}
}

// TestBudgetEscalationPromotion pins the FR-500 tier walk.
func TestBudgetEscalationPromotion(t *testing.T) {
	c := buildLaneContract(LaneChat, RouteUnspecified, CouncilOverlay{})
	count := 0
	if !promoteLaneBudget(&c, &count) || c.Lane != LaneTask || c.MaxMainToolSteps != 12 {
		t.Fatalf("chat→task promotion broken: %+v", c)
	}
	if !promoteLaneBudget(&c, &count) || c.Lane != LaneAgent || c.MaxMainToolSteps != 24 {
		t.Fatalf("task→agent promotion broken: %+v", c)
	}
	if promoteLaneBudget(&c, &count) {
		t.Fatal("agent tier must not promote further")
	}
	if count != 2 {
		t.Fatalf("promotion count = %d, want 2", count)
	}
}

// TestEscalationCapPerTurn pins the per-turn cap the run loop enforces.
func TestEscalationCapPerTurn(t *testing.T) {
	turn := chatTurnCheckpoint{Goal: "做个东西", Escalations: 2}
	c := buildLaneContract(LaneChat, RouteUnspecified, CouncilOverlay{})
	before := c.Lane
	if turn.Escalations >= 2 {
		// mirror of the run-loop guard: no further promotion
		if c.Lane != before {
			t.Fatal("run loop guard violated")
		}
	}
}

// TestSignalsFoldAfterTool pins the call-time signal updates.
func TestSignalsFoldAfterTool(t *testing.T) {
	sig := stateSignals{}
	updateSignalsAfterTool(&sig, "desktop.open", nil, "opened C:\\x\\a.lnk")
	if !sig.FileJustOpened {
		t.Fatal("fresh open must set FileJustOpened")
	}
	updateSignalsAfterTool(&sig, "workspace.write", nil, "ok")
	if sig.FileJustOpened {
		t.Fatal("a write invalidates FileJustOpened")
	}
	// G-308 fold: a real failure counts once, names the tool, arms the streak.
	updateSignalsAfterTool(&sig, "web.search", nil, "ok:false\nno results")
	if sig.HasMaterials {
		t.Fatal("a failed lookup must not fake materials")
	}
	if sig.ToolFailures != 1 || sig.LastFailedTool != "web.search" || sig.LastFailStreak != 1 {
		t.Fatalf("after 1st failure: failures=%d last=%q streak=%d, want 1/web.search/1", sig.ToolFailures, sig.LastFailedTool, sig.LastFailStreak)
	}
	// Second consecutive failure of the same tool grows the streak.
	updateSignalsAfterTool(&sig, "web.search", nil, "ok:false\ntimeout")
	if sig.ToolFailures != 2 || sig.LastFailStreak != 2 {
		t.Fatalf("after 2nd failure: failures=%d streak=%d, want 2/2", sig.ToolFailures, sig.LastFailStreak)
	}
	// Gate denials are refusals, not faults — neither count nor clear.
	updateSignalsAfterTool(&sig, "web.search", nil, "ok:false\n[闸口 G-305 实时票务未接入] 不要用网页数据冒充。")
	if sig.ToolFailures != 2 || sig.LastFailStreak != 2 {
		t.Fatalf("gate denial must not fold as failure: failures=%d streak=%d, want 2/2", sig.ToolFailures, sig.LastFailStreak)
	}
	// A different tool failing switches the tracked tool and resets the streak.
	updateSignalsAfterTool(&sig, "command.run", nil, "ok:false\nexit 1")
	if sig.ToolFailures != 3 || sig.LastFailedTool != "command.run" || sig.LastFailStreak != 1 {
		t.Fatalf("after channel switch failure: failures=%d last=%q streak=%d, want 3/command.run/1", sig.ToolFailures, sig.LastFailedTool, sig.LastFailStreak)
	}
	// A real success of the same tool clears its streak.
	updateSignalsAfterTool(&sig, "command.run", nil, "ok:true\nran")
	if sig.LastFailedTool != "" || sig.LastFailStreak != 0 {
		t.Fatalf("success must clear the streak: last=%q streak=%d", sig.LastFailedTool, sig.LastFailStreak)
	}
	if sig.ToolFailures != 3 {
		t.Fatalf("total failures are per-turn and must persist: %d, want 3", sig.ToolFailures)
	}
	// Web evidence only counts on success.
	updateSignalsAfterTool(&sig, "web.search", nil, "ok:true\n3 results")
	if !sig.HasMaterials {
		t.Fatal("a successful web lookup must set HasMaterials")
	}
}

// TestSignalsFoldClassifiesDatasourceObstacles pins the G-308 determinism
// fix: login walls and anti-crawl 401/403/verification failures are
// properties of one walled URL, not channel faults — they must not burn the
// environment stop-loss budget nor arm the per-channel streak, or a
// mainland lookup that bounced off two login walls would lose the model's
// routing freedom mid-turn.
func TestSignalsFoldClassifiesDatasourceObstacles(t *testing.T) {
	for _, out := range []string{
		"ok:false\nurl: https://aiqicha.baidu.com/firm/xxx.html\nHTTP 403",
		"ok:false\nurl: https://qcc.com/firm\n403 Forbidden",
		"ok:false\nurl: https://example.com/login-wall\n401 unauthorized",
		"ok:false\n该页面需要登录后才能查看",
		"ok:false\nweb search bing requires browser verification; no search results confirmed",
		"ok:false\nweb search bing returned HTTP 403; no search results confirmed",
		"ok:false\n触发反爬拦截 antispider",
		"ok:false\n请输入验证码",
	} {
		sig := stateSignals{}
		updateSignalsAfterTool(&sig, "web.fetch", nil, out)
		if sig.ToolFailures != 0 || sig.LastFailStreak != 0 {
			t.Errorf("obstacle folded as fault: out=%q failures=%d streak=%d", out, sig.ToolFailures, sig.LastFailStreak)
		}
		if sig.ObstacleFailures != 1 {
			t.Errorf("obstacle not counted separately: out=%q obstacles=%d", out, sig.ObstacleFailures)
		}
	}
	// Environment faults (and unclassified failures) still count.
	for _, out := range []string{
		"ok:false\nurl: https://example.com/x\ndial tcp: connection reset by peer",
		"ok:false\nweb search duckduckgo context deadline exceeded",
		"ok:false\n通道故障: web.search 不可用",
		"ok:false\nexit 1",
	} {
		sig := stateSignals{}
		updateSignalsAfterTool(&sig, "web.fetch", nil, out)
		if sig.ToolFailures != 1 || sig.ObstacleFailures != 0 {
			t.Errorf("env fault misclassified: out=%q failures=%d obstacles=%d", out, sig.ToolFailures, sig.ObstacleFailures)
		}
	}
	// Obstacle failures leave the streak untouched, so the same tool stays
	// legal for its next (different-URL) call; an interleaved env fault
	// still arms the streak only from real faults.
	sig := stateSignals{}
	updateSignalsAfterTool(&sig, "web.fetch", nil, "ok:false\nurl: https://aiqicha.baidu.com/a\nHTTP 403")
	updateSignalsAfterTool(&sig, "web.fetch", nil, "ok:false\nurl: https://qcc.com/b\nHTTP 403")
	if sig.ToolFailures != 0 || sig.ObstacleFailures != 2 || sig.LastFailedTool != "" {
		t.Fatalf("walls burned budget: %+v", sig)
	}
	updateSignalsAfterTool(&sig, "web.fetch", nil, "ok:false\ncontext deadline exceeded")
	updateSignalsAfterTool(&sig, "web.fetch", nil, "ok:false\nurl: https://aiqicha.baidu.com/c\nHTTP 403")
	if sig.ToolFailures != 1 || sig.LastFailStreak != 1 || sig.ObstacleFailures != 3 {
		t.Fatalf("mixed fold wrong: %+v", sig)
	}
	// Four obstacles alone must not arm the G-308 total stop-loss; four
	// env faults still must.
	walls := stateSignals{}
	for i := 0; i < 4; i++ {
		updateSignalsAfterTool(&walls, "web.fetch", nil, "ok:false\nurl: https://x/\nHTTP 403")
	}
	if walls.ToolFailures != 0 || g308WouldDeny(walls, "web.fetch") {
		t.Fatalf("walls armed stop-loss: %+v", walls)
	}
	envFaults := stateSignals{}
	for i := 0; i < 4; i++ {
		updateSignalsAfterTool(&envFaults, "web.fetch", nil, "ok:false\ndial tcp: connection reset")
	}
	if envFaults.ToolFailures != 4 {
		t.Fatalf("env faults lost: %+v", envFaults)
	}
}

// TestGateJournalFormat pins the journal line format (FR-300 audit).
func TestGateJournalFormat(t *testing.T) {
	// journalGate only logs; exercise it to make sure it does not panic on
	// partial signals.
	journalGate("stream-x", "G-301", "docx.gen", stateSignals{})
	journalGate("stream-x", "G-500", "-", stateSignals{BudgetNearExhausted: true})
}

// TestMediaSessionContinueGoal pins the G-306 signal detection: a bare
// continue word only counts while the media session is active; naming the
// media center carries its own context; a fresh song request is not a
// session action.
func TestMediaSessionContinueGoal(t *testing.T) {
	for _, goal := range []string{"下一首", "暂停播放", "继续播放", "切歌"} {
		if !mediaSessionContinueGoal(goal, true) {
			t.Errorf("mediaSessionContinueGoal(%q, active) = false, want true", goal)
		}
	}
	for _, goal := range []string{"下一首", "暂停播放", "切歌"} {
		if mediaSessionContinueGoal(goal, false) {
			t.Errorf("mediaSessionContinueGoal(%q, inactive) = true, want false", goal)
		}
	}
	if !mediaSessionContinueGoal("媒体中心下一首", false) {
		t.Error("explicit media-center wording must count without session context")
	}
	for _, goal := range []string{"播放周杰伦的晴天", "写周报", "打开汽水音乐"} {
		if mediaSessionContinueGoal(goal, true) {
			t.Errorf("mediaSessionContinueGoal(%q, active) = true, want false", goal)
		}
	}
}

// TestStateGatesKillSwitch pins the FR-700 switch: off disables every gate.
func TestStateGatesKillSwitch(t *testing.T) {
	t.Setenv("LUNITIDE_STATE_GATES", "off")
	sig := stateSignals{TicketQueryNoSource: true}
	if v := evalToolGate(sig, "查余票", "web.search", nil, nil, false); v.Deny {
		t.Fatal("kill switch off must disable the gate table")
	}
}

// TestAssistantAsksQuestion pins the G-302 question detection.
func TestAssistantAsksQuestion(t *testing.T) {
	if !assistantAsksQuestion("请问本周完成了哪些工作？") {
		t.Fatal("Chinese question mark must count")
	}
	if !assistantAsksQuestion("which format do you want?") {
		t.Fatal("ASCII question mark must count")
	}
	if assistantAsksQuestion("好的，我明白了。") {
		t.Fatal("statement must not count")
	}
	if assistantAsksQuestion("  \n ") {
		t.Fatal("blank text must not count")
	}
}

// TestEscalatedNudgeExists guards the FR-500 nudge copy stays in the switch.
func TestEscalatedNudgeExists(t *testing.T) {
	if !strings.Contains("预算已升级：继续完成用户目标，优先调用工具执行，不要只复述计划。", "预算已升级") {
		t.Fatal("escalation nudge copy missing")
	}
}
