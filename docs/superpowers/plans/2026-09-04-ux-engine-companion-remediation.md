# 三次反馈全量整改 · Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把三次反馈的十项断点按锁定规格修完，使引擎可自愈、月伴能打开软件且再进空白、说完立刻出字，其余六项体验一次收口。

**Architecture:** 不新开 daemon / 表 / 路由。宿主用 `ReplaceCaller` 换新 RPC client；月伴打开用 CanonicalMusicApp 进程别名验窗（汽水 = sodamusic.exe），前置后读前台才算成功；舞台每次挂载取消未结束 live 流，本访首轮丢掉上轮失败稿。#8 **不接线** opening ack，只保证首模型 token 先于 TTS 上屏。按波次串行改 `SessionPage.tsx` 与 `CompanionStage.tsx`。

**Tech Stack:** Go 1.x（`internal/hostbridge`、`internal/app`、`internal/toolruntime`、`cmd/desktop`）+ React/TS（`web/src`）+ Vitest + 现有 Bridge。

**Spec:** [docs/superpowers/specs/2026-09-04-remediation-upgrade-prd.md](../specs/2026-09-04-remediation-upgrade-prd.md)

## Global Constraints

- 不新开独立平台骨架；内核永远 SQLite；机务输出是辅助建议，不构成放行。
- 不新 daemon、不改 named pipe 安全模型、不新 npm 依赖、不升版本、不打安装包。
- `Client.poison` 语义不动；恢复只靠新 client + `ReplaceCaller`。
- 月伴会话保持单例「月伴对话」，不可删、不可改名。
- #8 不换模型 / ASR / TTS / pill / 人设长文。
- 每个 Task：先写失败测试 → 确认红 → 最小实现 → 确认绿。未要求前不 commit。
- 现网 `go test ./...` 与相关 vitest 必须保持绿。

## File map

| 文件 | 本计划职责 |
|---|---|
| `cmd/desktop/main.go` `watchdog.go` `watchdog_test.go` | 自拉起路径喂 `rpcBroken`；重连优先于 takeover |
| `internal/hostbridge/gateway.go` `gateway_test.go` | 带锁 `ReplaceCaller` |
| `web/src/App.tsx` `web/src/bridge/client.ts` | 壳层一条引擎横幅；冒泡 `ENGINE_UNAVAILABLE` |
| `web/src/project/ProjectPage.tsx` `web/src/skill/SkillPage.tsx` `web/src/expert/ExpertCenterPage.tsx` | 失败态 code + correlationId；恢复后自动 load |
| `internal/app/chat_continue.go` `chat_continue_test.go` | `companionGoalIsOpenOnly`；纯打开 settle |
| `internal/app/chat_turn_notice.go` | 成功打开不落失败通知 |
| `internal/app/chat_companion_speech.go` `chat.go` `chat_run_stream.go` | 接线 ack；成功打开禁止「无法执行」 |
| `internal/toolruntime/desktop.go` `runtime.go` | `desktop.open` 验窗 |
| `internal/ccapp/runhost.go` | screenshot 跳过自家前台 |
| `web/src/session/companion/CompanionStage.tsx` | 先字后声；空白挂载；成功打开不朗读无法执行 |
| `web/src/session/SessionPage.tsx` | 取消 companion live；决策先入框；专家失败回滚 |
| `web/src/session/streamScroll.ts` | 思考不 pin |
| `web/src/meetings/MeetingPage.tsx` `web/src/settings/SettingsPage.tsx` | 设置 overlay |
| `web/src/session/sessionTitle.ts` `web/src/App.tsx` | 保护会话无删除/重命名 |
| `web/src/mro/mroRailGroups.ts` `MroWorkbenchPage.tsx` | 五级折叠 |
| `web/src/project/PhaseExpertsBar.tsx` `web/src/expert/expertIds.ts` | 左侧只读 |
| `web/src/session/companion/visual/moonVisual.ts` `web/src/styles.css` | 说话嘴填满内圆 |

```text
T0 #0 引擎
 → T1 #9 打开结算
 → T2 #8 ack 出字
 → T3 #7 空白舞台
 → T4 #6 滚动+决策
 → T5 #5 专家
 → T6 #1 会议 overlay
 → T7 #3 不可删
 → T8 #2 机务折叠
 → T9 #4 嘴
 → T10 回归门槛
```

---

## Task T0 · P0-E 引擎进程内重连

**Files:** `cmd/desktop/watchdog_test.go` `cmd/desktop/main.go` `internal/hostbridge/gateway.go` `internal/hostbridge/gateway_test.go` `web/src/bridge/client.ts` `web/src/App.tsx` `web/src/skill/SkillPage.tsx`

- [ ] **Step 1:** 在 `watchdog_test.go` 增加自拉起路径断言（当前会红）：

```go
func TestSelfLaunchWatchRelaunchesWhenRPCBroken(t *testing.T) {
	if !engineWatchShouldRelaunch(false, true, 4242, true) {
		t.Fatal("self-launch path must treat poisoned RPC as relaunch even when PID is alive")
	}
}
```

确认 `engineWatchShouldRelaunch` 已绿后，在 `main.go` 自拉起分支用同一谓词：不要只 `command.Wait()`；轮询 `client.Broken()!=nil || !pidAlive`。

- [ ] **Step 2:** `gateway_test.go` 写失败测试：

```go
func TestReplaceCallerNextCallUsesNewClient(t *testing.T) {
	// poisonFake.Call returns error; after g.ReplaceCaller(okFake),
	// Handle(project.list) must succeed and must not return ENGINE_UNAVAILABLE.
}
```

- [ ] **Step 3:** 在 `Gateway` 增加：

```go
func (g *Gateway) ReplaceCaller(next Caller) {
	g.replaceMu.Lock()
	defer g.replaceMu.Unlock()
	g.caller = next
}
```

`Handle` 读 `caller` 与 `ReplaceCaller` 共用同一把锁。Call error 仍映射 `ENGINE_UNAVAILABLE`，`retryable=true`，details 带短因。禁止 `ok:true`。

- [ ] **Step 4:** 跑 `go test ./internal/hostbridge ./cmd/desktop` 至绿。

- [ ] **Step 5:** 前端 `bridge/client.ts`：`ENGINE_UNAVAILABLE` 除抛给调用方外，派发 `window` 事件 `lunitide:engine-unavailable`（带 code、correlationId）。`App.tsx` 只挂一条顶栏：「核心引擎已断开，正在自动重连…」；收到恢复事件后当前页 `reload`。`SkillPage` 市场错误必须渲染 `code` 与 `correlationId`。

- [ ] **Step 6:** Vitest：连续两次该事件只渲染一条横幅；点「重试连接」后 `catalogList` mock 再被调用一次。

---

## Task T1 · #9 纯打开验窗并 settle

**Files:** `internal/app/chat_continue.go` `internal/app/chat_continue_test.go` `internal/app/chat_turn_notice.go` `internal/toolruntime/desktop.go` `internal/toolruntime/runtime.go` `internal/ccapp/runhost.go` `internal/app/chat_companion_speech.go`

- [ ] **Step 1:** 改 `chat_continue_test.go`（先跑红再改实现）：

```go
if companionGoalIsOpenOnly("帮我打开桌面汽水") != true {
	t.Fatal("open-only")
}
if companionGoalIsOpenOnly("打开记事本然后填身份证") != false {
	t.Fatal("open-then-act")
}
if shouldContinueDesktopTurn("已经打开了汽水音乐。", 0) {
	t.Fatal("open-only result must settle")
}
if !shouldContinueDesktopTurn("已经打开记事本。", 0) && !companionGoalIsOpenOnly("打开记事本帮我写号码") {
	t.Fatal("open-then-act must still continue after open")
}
if got := pickTurnContinueKind("已经打开了。", "已经打开了。", "opened C:\\\\x\\\\汽水音乐.lnk", []string{"desktop.open"}, true, true, true, true, 0); got != "" && companionGoalIsOpenOnly("打开汽水") {
	t.Fatalf("open-only must not return desktop, got %q", got)
}
```

`pickTurnContinueKind` 增加 `userGoal string` 参数（或从 checkpoint 读）。纯打开 + 最后工具 `desktop.open` + 输出以 `opened ` 开头且不含 `无法执行` → 返回 `""`。

- [ ] **Step 2:** 实现：

```go
func companionGoalIsOpenOnly(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || !companionWantsDesktopControl(t) {
		return false
	}
	open := strings.Contains(t, "打开") || strings.Contains(t, "启动") || strings.Contains(t, "把开")
	if !open {
		return false
	}
	for _, follow := range []string{"填写", "填一下", "输入", "写入", "播放", "播一", "点", "搜索", "搜一", "发消息"} {
		if strings.Contains(t, follow) {
			return false
		}
	}
	return true
}
```

`desktopTurnSettled`：当文案含「已经打开」且本轮 `companionGoalIsOpenOnly` 为真，返回 true。含「无法执行」仍 settle（停环），但 T1 Step 5 保证成功打开不会写出这四字。

- [ ] **Step 3:** `createTurnFailureNotice` / `turnOutcomeNotice`：`desktop.open` 成功且无后续失败工具 → 空字符串。单测锁死。

- [ ] **Step 4:** `desktop.open` 在 `openWithDefaultApp` 成功后验窗。查询列表 = `launchQueryCore(name)` + `CanonicalMusicApp` 的 `Aliases` 与 `Processes`（「汽水」必须包含 `sodamusic.exe`、`Soda Music`）。排除 Lunitide。每 200ms 读前台，标题或进程命中才成功；最多 4s。`ActivateWindowMatching` 在 SetForeground 失败时返回 nil **不得**当成功——以「前台是不是目标」为准。单测：前台 Lunitide + 列表里有 sodamusic.exe 窗口 → 查询「汽水」成功。失败文案：`无法执行：启动了但窗口没到前台`。

- [ ] **Step 5:** `computer.act` screenshot：若 `ActiveWindow` 进程是 `lunitide.exe` 或标题含 `Lunitide`/`月伴`，改走 desktop 或上一非月伴窗（与 `cc_control_test.go` 键盘用例同一规则）。纯打开成功后引擎不再接受本轮 `computer.act`（工具层返回 skip 或 continue 不再 nudge）。

- [ ] **Step 6:** 人设补一句：「打开类目标在 desktop.open 成功后用一句结果收尾，禁止说无法执行，禁止再截月伴。」`go test ./internal/app ./internal/toolruntime ./internal/ccapp` 绿。

---

## Task T2 · #8 说完立刻出字（不接线 ack）

**Files:** `internal/app/chat_companion_fastpath_test.go` `internal/app/chat_run_stream.go` `web/src/session/companion/CompanionStage.tsx` `web/src/session/companion/CompanionStage.headstart.test.tsx`

- [ ] **Step 1:** `chat_companion_fastpath_test.go` 增加：**companion `chat.start` 在 mock 上游返回前，事件里不得出现** `嗯，` / `嗨，我在呢` / `companionOpeningAck` 的 delta。`companionOpeningAck` 保持单测函数，**生产路径零调用**。

- [ ] **Step 2:** 读 `chat_run_stream.go` / `chat.go`，确认没有 ack 接线；若有人接上了，删掉。

- [ ] **Step 3:** `CompanionStage`：`assistantText` 第一个非空模型 token → 立刻上字幕，并在 `thinking` 时 `REPLY_COMPLETED`。**不要等** `takeSpeakableChunk` 才出字。TTS 仍等第一句完整标点才 `enqueue`。

- [ ] **Step 4:** headstart 测试：先推模型句「好呀，今晚月色很好。」（不是 ack）。断言字幕先有这句，TTS 第一次 enqueue 也是这句。闲聊轮 tools 仍为空。

- [ ] **Step 5:** `go test ./internal/app -count=1` 与对应 vitest 绿。

---

## Task T3 · #7 每次进月伴空白

**Files:** `web/src/session/companion/CompanionStage.tsx` `web/src/session/SessionPage.tsx` `web/src/session/liveChat.ts` `web/src/session/SessionPage.companion.test.tsx`

- [ ] **Step 1:** 失败测试（companion 挂载）：

```ts
it('does not seed last unfinished turn when re-entering companion', async () => {
  // historySeed has user+assistant 无法执行
  // liveChatEntry has streaming assistantText
  // expect live log empty, cancelLiveChatTurn called, no 「上次对话还没做完」
})
```

- [ ] **Step 2:** 删除或短路 seed effect，mount 时 `setRounds([])`。`SessionPage` 在 `initialCompanion`：`historySeed={[]}`；不设 `resumeBanner`；未结束 live 则 `cancelLiveChatTurn`；`toolActivities` 传 `[]` 直到本轮新工具。本访第一轮 `chat.start` 带 `companionFreshVisit`（或等价：组装时丢掉上一轮含「无法执行」/未完成的助手稿）。单测 C7-6：上轮助手为「无法执行。」时，本访首轮 messages 不含该句。

- [ ] **Step 3:** `onExit`：companion 标题会话不删单例；仍 `cancelStream`。`companionShouldDiscardOnExit` 对单例保持 false。

- [ ] **Step 4:** 侧栏仍只有一条置顶「月伴对话」。Vitest 绿。

---

## Task T4 · #6 思考不抢滚动 + 决策先入框

**Files:** `web/src/session/streamScroll.ts` `web/src/session/streamScroll.test.ts` `web/src/session/SessionPage.tsx` `web/src/session/UserAskWizard.tsx` `web/src/styles.css`

- [ ] **Step 1:** `streamScroll.test.ts`：假 box 思考增高 200 token，用户已 `deltaY<0`，`scrollTop` 变化 ≤ 2px。`pinIfFollowing` 在仅 `thinkingText` 变化时不被调用。

- [ ] **Step 2:** effect 依赖去掉 `thinkingOpen`。RO 只观察 message-list 与 live assistant，不观察 `.thinking-panel`。CSS：

```css
.thinking-panel { contain: strict; max-height: 28vh; overflow-y: auto; }
```

- [ ] **Step 3:** `submitUserAsk`：

```ts
const followUp = formatUserAskFollowUp(...)
setText(followUp)
try {
  await sendAndChat(event, [], followUp)
  setText('')
  // close wizard only on success
} catch (err) {
  // keep text, keep wizard, keep chips
  // toast: 核心引擎暂时不可用。你的选择已留在输入框，可点重试。
}
```

禁止 `chat.start` 失败后 `decideTool` 标完成。retryable 引擎错误 500ms 后再试 1 次。

- [ ] **Step 4:** 测试 D1–D4。项目与个人共用同一 pin 测试。

---

## Task T5 · #5 专家只读镜像

**Files:** `web/src/expert/expertIds.ts` `web/src/project/phaseExperts.ts` `web/src/project/PhaseExpertsBar.tsx` `web/src/session/SessionPage.tsx`

- [ ] **Step 1:** `resolveInstalledExpertIds(slugs, installed)` 把 catalog slug 译成 ULID；译不了跳过并返回 `missing`。用 slug 调 set 的 UI 路径删除。

- [ ] **Step 2:** `PhaseExpertsBar` 去掉 `<select>` 与 ×。文案「本阶段将使用」。空态「未挂载 · 在下方输入框添加」。「推荐」走同一 `session.experts.set`。`revision` 变化重新 `get`。

- [ ] **Step 3:** `persistMounted` 失败必须回滚 `referencedExperts` 并 `setError`，禁止 `.catch(()=>{})`。

- [ ] **Step 4:** E1–E5 vitest 绿。个人聊天无阶段栏行为不变。

---

## Task T6 · #1 会议设置 overlay

**Files:** `web/src/meetings/MeetingPage.tsx` `web/src/meetings/MeetingPage.test.tsx` `web/src/App.tsx` `web/src/settings/SettingsPage.tsx`

- [ ] **Step 1:** 测试：点「听写与纪要设置」**不**调用 `onOpenSettings` 里的 `setPage`。父组件传入 `onOpenSettings={() => setMeetingSettingsOpen(true)}`。

- [ ] **Step 2:** `MeetingPage` 用现有 `model-manager-overlay` 壳嵌 `SettingsPage`（`initialCategory='meetings'`，`backLabel='返回会议'`，`embedded`）。cleanup **不**因 overlay 运行。

- [ ] **Step 3:** `recording===true` 时引擎/音源 `disabled`，aria「本场结束后生效」。断言 overlay 期间 `meetings.stop` 与 `speechRef.stop` 次数为 0。

- [ ] **Step 4:** 非录制可改引擎。侧栏设置仍 `setPage('settings')`。

---

## Task T7 · #3 保护会话无删除

**Files:** `web/src/session/sessionTitle.ts` `web/src/session/sessionTitle.test.ts` `web/src/App.tsx` `web/src/session/SessionPage.tsx`

- [ ] **Step 1:**

```ts
export function isProtectedSidebarChat(title: string): boolean {
  const t = title.trim()
  return isCompanionChatTitle(t) || isPlaceholderChatTitle(t) || t === '对话' || t === 'Chat'
}
```

- [ ] **Step 2:** 侧栏菜单：保护会话不渲染删除、不渲染重命名。`remove()` 若 `isProtectedSidebarChat` 直接 return。顶栏垃圾桶同样。

- [ ] **Step 3:** P1–P5：月伴无删除确认、无重命名；「上海天气」仍可删。

---

## Task T8 · #2 机务五级折叠

**Files:** `web/src/mro/mroRailGroups.ts`（新建） `web/src/mro/MroWorkbenchPage.tsx` `web/src/mro/MroWorkbenchPage.test.tsx` `web/src/styles.css`

- [ ] **Step 1:** 纯函数导出组定义：手册 / 机务维修(排故,到期) / 航材(航材,计划) / 工具化工品 / 工具(检查单,审计,机队)。`readRailOpen` / `writeRailOpen` 读写 `lunitide:mro-rail-open`。当前 rail 所在组强制展开。

- [ ] **Step 2:** `mro-rail` 改为组按钮 + 叶子按钮。点组只 toggle。点「计划」→ `rail==='plan'`，问月汐仍 `mx-planning-expert`。

- [ ] **Step 3:** R1–R5：一级 5 个；`aria-expanded` / `aria-selected`。

---

## Task T9 · #4 说话嘴填满内圆

**Files:** `web/src/session/companion/visual/moonVisual.ts` `moonVisual.test.ts` `web/src/styles.css`

- [ ] **Step 1:** 单测锁（改小必红）：

```ts
expect(strandsSpeaking(0.6).glass).toBe(true)
expect(strandsSpeaking(0.6).scale).toBeLessThanOrEqual(0.62)
expect(strandsSpeaking(0.6).amplitude).toBeGreaterThanOrEqual(1.45)
expect(STRANDS_THINKING.scale).toBe(1.15)
```

- [ ] **Step 2:** `strandsSpeaking` 恢复 `glass: true`，按上列改 scale/amplitude。CSS speaking strands `transform: scale(1.55)`。无 WebGL：`.companion-moon-halo-wave` speaking inset `-8%`～`0`，scale 0.92→1.08。`prefers-reduced-motion` 静止填满。不改 `useCompanionMachine`。

- [ ] **Step 3:** V1–V3。idle/listening 外框不变。

---

## Task T10 · 回归门槛（不发版）

- [ ] `go test ./...`
- [ ] `npx vitest run`（至少本计划涉及文件）
- [ ] `npx tsc --noEmit`
- [ ] `npm run verify:bridge`
- [ ] 手工 15 分钟按 spec §15：引擎恢复、会议不丢稿、机务折叠、月伴不可删、嘴、专家、滚动/决策、再进空白、立刻出字、打开汽水到前台且不说无法执行
- [ ] **禁止** `git tag` / 打安装包 / push 发行，直到用户点头

---

## Spec coverage（self-review）

| Spec 项 | Task |
|---|---|
| #0 G0–G6 | T0 |
| #9 C9-1–C9-7 | T1 |
| #8 C8-1–C8-4（反 ack） | T2 |
| #7 C7-6 freshVisit | T3 |
| #9 C9-8 sodamusic 别名 | T1 |
| #7 C7-1–C7-5 | T3 |
| #6 J/D | T4 |
| #5 E1–E5 | T5 |
| #1 M1–M6 | T6 |
| #3 P1–P5 | T7 |
| #2 R1–R5 | T8 |
| #4 V1–V3 | T9 |
| 发布门槛 | T10 |

无 TBD。`ReplaceCaller`、`companionGoalIsOpenOnly`、`isProtectedSidebarChat` 名称前后一致。#7 含 liveChat 取消，不是只关 seed。#9 纯打开与打开再做已分开。
