# 月伴轮次 / 收口停说 / 专家跟轮 / 会议听写 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Do not commit unless the user asks.

**Goal:** 火山月伴一轮一句且只喂本轮；任务说完就停；同事专家第二轮能做表且不再谎称没配模型；会议三路跟嘴、去重、状态诚实。对照规格综合 **4.9/5**。

**Architecture:** 不重写识别引擎。客户端用 `isolateCurrentUtterance` 把火山 `result_type=full` 切成本句；口播/字幕用 `clipCompanionSpokenTurn` 封顶，**不降** `companionMaxTokens`。People `Complete` 补线程历史 + 错误三分类 + 空文时 `fallbackOfficeGenArgs`。会议 `voice.start` 显式传 `endWindowMs=400`。

**Tech Stack:** Go `go test ./internal/app ./internal/voice/volcsauc ./internal/people ./internal/contract`；前端 `npx vitest run` 指定文件；改 schema 后 `npm run generate:bridge`。PowerShell 用 `;` 不用 `&&`。

**Spec:** [docs/superpowers/specs/2026-09-05-voice-turn-meeting-expert-prd.md](../specs/2026-09-05-voice-turn-meeting-expert-prd.md)（含 §12 复核勘误；执行以改后锁为准）。

## Global Constraints

- 不新 daemon；不改 VERSION / 不打包（另开指令）
- 不改 poison / TTS 音色 / 玉盘 / 星尘 / `AnnotateCapture` / 专家六段
- `companionMaxTokens` 保持 2048；`speech.ts` 280/220 不动
- 月伴 `DefaultEndWindowMS` 保持 1200；只给会议会话传 400
- 不接线 `companionOpeningAck`；不发明第三句「稍等，我去处理。」
- 云端/本机月伴仍只按钮打断
- 图2 只改 People，不改 SessionPage 专家挂载
- 不把 `result_type` 改成 `single`

## File map

| File | Responsibility |
|---|---|
| `web/src/meetings/meetingText.ts` | `isolateCurrentUtterance` |
| `web/src/meetings/meetingText.test.ts` | A2 / D2 切句单测 |
| `web/src/session/companion/speech.ts` | `collapseTandemRepeats` 相邻 ≥8 字 |
| `web/src/session/companion/speech.test.ts` | 图3 循环墙 |
| `web/src/session/companion/volc/volcSpeech.ts` | 月伴路径改用 isolate，停用 absorb 跨句黏 |
| `web/src/session/companion/volc/volcSpeech.test.ts` | 第二句 onFinal 不含第一句 |
| `web/src/session/companion/volc/volcAsr.ts` | `startVolcAsr` 传 `endWindowMs` |
| `web/src/session/companion/companionText.ts` | `clipCompanionSpokenTurn`；火山不排队 |
| `web/src/session/companion/CompanionStage.tsx` | 插话=最新；字幕+TTS 封顶+停流 |
| `web/src/meetings/meetingAsr.ts` | 会议 isolate + `endWindowMs: 400` + merge 400 |
| `web/src/meetings/MeetingPage.tsx` | 连接成功清「正在连接」 |
| `api/bridge/v1/voice.start.schema.json` | 可选 `endWindowMs` |
| `internal/app/voice_handlers.go` | 写入 `Config.EndWindowMS` |
| `internal/app/chat_continue.go` | `media.play` 不续桌面 |
| `internal/app/chat_companion_speech.go` | `companionRedundantMediaSkip` |
| `internal/app/chat_run_stream.go` | 第二次 media.play skip |
| `internal/app/people_agent.go` | 历史 + 错误分类 + excel 补刀 |
| `internal/app/people_agent_test.go` | C1/C2/C3 |

不改：`MoonSphere` / `particleMoon.ts` / `chat.go` 的 `companionMaxTokens` / `speech.ts` 的 280/220。

---

## Task 1: `isolateCurrentUtterance`（A2 / D2 共用）

**Files:** `web/src/meetings/meetingText.ts`, `web/src/meetings/meetingText.test.ts`

导出：

```ts
export function isolateCurrentUtterance(committed: string, incoming: string, lastCurrent = ''): string
```

规则按规格 A2（先 `compactMeetingText`）。`includes(committed原文)` 必须先剥，即使带句号。

- [ ] 在 `meetingText.test.ts` 追加（先红）：

```ts
test('strips prior volc-full sentence even with a period between turns', () => {
  const committed = '今天合肥天气怎么样'
  const incoming = '今天合肥天气怎么样。算了放首歌'
  expect(isolateCurrentUtterance(committed, incoming)).toBe('算了放首歌')
})

test('replaces when the engine starts a fresh clause', () => {
  expect(isolateCurrentUtterance('今天天气怎么样', '帮我放周杰伦')).toBe('帮我放周杰伦')
})

test('returns empty when incoming is only the committed sentence', () => {
  expect(isolateCurrentUtterance('你好', '你好。')).toBe('')
})
```

- [ ] 实现 `isolateCurrentUtterance`。禁止调用 `joinMeetingLines`。
- [ ] `npx vitest run src/meetings/meetingText.test.ts`

---

## Task 2: 相邻 tandem 去重（D3）

**Files:** `web/src/session/companion/speech.ts`, `web/src/session/companion/speech.test.ts`

加强 `collapseTandemBody`：相邻重复且片段 ≥ 8 字只留一次。不要「全文任意出现两次就删」。

- [ ] 先红：

```ts
test('collapses lyric tandem wall from meeting volc full dumps', () => {
  const unit = '虚情假意曾经是我太缺心你是对的'
  const wall = unit.repeat(8)
  const got = collapseTandemRepeats(wall)
  expect(got).toBe(unit)
  expect(Array.from(got).length).toBeLessThan(Array.from(wall).length * 0.3)
})

test('keeps a domain term that appears twice but not tandem', () => {
  expect(collapseTandemRepeats('周转件算法讲完再算周转件库存。')).toContain('周转件算法')
  expect(collapseTandemRepeats('周转件算法讲完再算周转件库存。')).toContain('周转件库存')
})
```

- [ ] 实现相邻切分；`npx vitest run src/session/companion/speech.test.ts`

---

## Task 3: 月伴火山切句（A1 / A3）

**Files:** `web/src/session/companion/volc/volcSpeech.ts`, `web/src/session/companion/volc/volcSpeech.test.ts`

`holdUtterance === false` 时：

1. 增加 `committed = ''`（本条 WS 已定稿拼接）。
2. `onTranscript`：`current = isolateCurrentUtterance(committed, next)`；`onInterim(current)`；不要 `absorbHeldTranscript`。
3. `recycle('final')`：`onFinal(current)` 后 `committed = compact 拼接`（保留原文拼接以便下一轮 `includes`），`resetUtterance()`。
4. `setAssistantPlayback` 仍 `resetUtterance()`，**不要**清 `committed`（同一 WS，她说完用户再说，引擎仍带旧句）。

- [ ] 先红：第二句 `onFinal` 不得包含第一句。

```ts
it('final of turn two does not include turn one after a volc-full dump', async () => {
  const stage = harness()
  asr.commit.mockResolvedValue('今天天气怎么样。算了放首歌')
  await startVolcCompanionSpeech(stage.options, PROVIDER)
  onTranscript('今天天气怎么样', true)
  await vi.advanceTimersByTimeAsync(1300)
  expect(stage.onFinal).toHaveBeenLastCalledWith('今天天气怎么样')
  onTranscript('今天天气怎么样。算了放首歌', false)
  expect(stage.onInterim.mock.calls.at(-1)?.[0]).toBe('算了放首歌')
  onTranscript('今天天气怎么样。算了放首歌', true)
  asr.commit.mockResolvedValue('今天天气怎么样。算了放首歌')
  await vi.advanceTimersByTimeAsync(1300)
  expect(stage.onFinal.mock.calls.at(-1)?.[0]).toBe('算了放首歌')
})
```

定时器以现有 `turnEndWindows()` 为准（月伴不是 220ms）。若现测 220ms 只覆盖短问候，本测用 1300。

- [ ] `npx vitest run src/session/companion/volc/volcSpeech.test.ts`

会议路径（`holdUtterance===true`）不要在 `volcSpeech` 里改 absorb；切句放 Task 8 的 `startMeetingSpeech` 外包一层，避免双切。

---

## Task 4: 火山插话 = 最新一句（A4）

**Files:** `web/src/session/companion/companionText.ts`, `web/src/session/companion/companionText.test.ts`, `web/src/session/companion/CompanionStage.tsx`

- [ ] `shouldQueueBusyUserTranscript`：增加可选 `voicePath?: string`。`voicePath === 'volc'` 时**永远 false**（不排队，走插话）。
- [ ] 单测：volc + speaking + 新句 → false；cloud 保持 true。
- [ ] `CompanionStage` `onInterim` / `beginUserTurn` 传入 `settingsRef.current.voicePath`。火山 thinking/speaking 新 current：`onCancel` + `player.interrupt` + `beginUserTurn(最新)`。
- [ ] `npx vitest run src/session/companion/companionText.test.ts src/session/companion/CompanionStage.a11y.test.tsx`

现有 a11y「下一句已记下」测若针对火山，改成「立即开新轮」。云端测保持排队。

---

## Task 5: 字幕+TTS 封顶（B2）

**Files:** `web/src/session/companion/companionText.ts`, `web/src/session/companion/companionText.test.ts`, `web/src/session/companion/CompanionStage.tsx`

```ts
export function clipCompanionSpokenTurn(text: string, already = ''): { spoken: string; overflow: boolean }
```

同一轮合计最多 2 句或 80 字（先到为准）。`already` 是本轮已出口播（含 lead-in）。

- [ ] 先红：

```ts
expect(clipCompanionSpokenTurn('好，我来播放。已经在播了。你还想听哪一首呢再搜一下。', '').spoken)
  .toBe('好，我来播放。已经在播了。')
expect(clipCompanionSpokenTurn('好，我来播放。已经在播了。你还想听哪一首呢再搜一下。', '').overflow).toBe(true)
expect(Array.from(clipCompanionSpokenTurn('甲'.repeat(90), '').spoken).length).toBe(80)
```

- [ ] 舞台：`setRounds` 助手条、`takeSpeakableChunk` 入队前、`done` 时剩余 `segments`，都先 clip。`overflow` → `onCancel(已出口播)`，不再 enqueue。
- [ ] **不改** `internal/app/chat.go` 的 `companionMaxTokens`。`TestCompanionFastPathCapsTokensAndKeepsVoice` 仍断言 2048。
- [ ] `npx vitest run src/session/companion/companionText.test.ts`

---

## Task 6: `media.play` 一次定生死（B3 / B4 / B5）

**Files:** `internal/app/chat_companion_speech.go`, `internal/app/chat_continue.go`, `internal/app/chat_continue_test.go`, `internal/app/chat_run_stream.go`

```go
func companionRedundantMediaSkip(companion bool, lastTools []string, next string) (string, bool)
```

`companion && next=="media.play" && 本轮已出现过 media.play` → skip，回包：

```
ok:true
已经处理过播放。不要再 media.play，用一两句说结果或「没播成」。
```

`shouldContinueDesktopTurnGoal`：`lastToolName=="media.play"` 直接 `false`。

`unverifiedMediaPlay`：仅当本轮 media.play **次数 == 1** 且输出是「已启动未核验」时续一次；第二次停。

失败口播：`companionToolResultSpeech("media.play", ok:false)` → 「没播成。没找到这首歌，请说出歌名或歌手。」（已有「无法执行」则用它）。

- [ ] 先红（`chat_continue_test.go`）：

```go
if shouldContinueDesktopTurnGoal("这次没有完成。", "放一首复古公路风", 0) && lastToolWouldBeMediaPlay {
    t.Fatal("media.play must not desktop-continue")
}
```

更干净：给 `shouldContinueDesktopTurnGoal` 加 `lastTool string` **会动旧签名**。不要改签名。在函数内无法看见 lastTool。改为在 `pickTurnContinueKind` 里：

```go
if companion && lastToolName(lastTools) == "media.play" {
    return ""
}
```

放在 desktop continue 判断之前。B5 wait 仍可走，但 B3 优先：上一行先截。

- [ ] `chat_run_stream.go` 在派工具前调 `companionRedundantMediaSkip`（对称 `companionRedundantWebSkip`）。
- [ ] `go test ./internal/app -count=1 -filter "Continue|Media|CompanionRedundant"`

Windows：`go test ./internal/app -count=1 -run "Continue|Media|CompanionRedundant"`

---

## Task 7: 同事专家跟轮 + 诚实错误 + 补表（C）

**Files:** `internal/app/people_agent.go`, `internal/app/people_agent_test.go`

### 7a 历史

```go
func peopleAgentHistoryMessages(msgs []people.Message, agentID, currentUserID, currentBody string, limit int) []gateway.Message
```

只要 `kind=="text"`；跳过 `system`；跳过 `Body==currentBody && SenderID==currentUserID`（本轮）；user vs assistant 按 `SenderID==agentID`；最多 8 条；合计 6000 字从最旧丢。

`completePeopleAgentText` / `WithTools`：`ListMessages(ctx, threadID, 40)` → history → 再 append 本轮 user。`completePeopleAgentTurn` 要把 `threadID` 传进这两个函数（现签名没有 threadID — **加上** `threadID string`）。

现签名：`completePeopleAgentWithTools(ctx, agent, sessionID, userText)`。改为加 `threadID`。所有测试调用补参。

### 7b 错误分类

```go
type peopleAgentFailKind int
const (
    peopleFailNoModel peopleAgentFailKind = iota
    peopleFailError
    peopleFailEmpty
)

func classifyPeopleAgentFailure(catalogOK bool, err error, text string) (peopleAgentFailKind, string)
```

- `!catalogOK` → 旧 `peopleAgentNoReplyUserError()`
- `err != nil` → `这轮没做成：` + clip 80 + `。请再发一次。`
- 否则 → `这轮没有生成回复。模型已启用，请换个说法或把数字再发一次。`

`completePeopleAgentTurn` 空回复走分类，禁止一律 `peopleAgentNoReplyUserError`。

### 7c 补表

`completePeopleAgentWithTools` 结束时：`looksLikeExcelTask(userText)` 且本轮 `paths` / tool names 无成功 `excel.gen` 且 text 空或无 `.xlsx`：调用 `fallbackOfficeGenArgs("excel.gen", userText, text)` + `executeUserTool` 一次。成功则 append `officeGenSuccessNotice`；失败走 7b，**不得**写没配模型。

`peopleAgentMaxTokens`：1600 → 2400。

- [ ] 先红：

```go
func TestPeopleAgentEmptyReplyDoesNotClaimMissingModel(t *testing.T) {
    kind, msg := classifyPeopleAgentFailure(true, nil, "")
    if kind != peopleFailEmpty || strings.Contains(msg, "启用一个对话模型") {
        t.Fatalf("got %d %q", kind, msg)
    }
}

func TestPeopleAgentHistorySkipsCurrentUser(t *testing.T) {
    hist := peopleAgentHistoryMessages([]people.Message{
        {Kind: "text", SenderID: "u", Body: "周转件怎么算"},
        {Kind: "text", SenderID: "agent", Body: "先算周转率…"},
        {Kind: "text", SenderID: "u", Body: "做个模拟计算表"},
    }, "agent", "u", "做个模拟计算表", 8)
    if len(hist) != 2 || hist[1].Content != "先算周转率…" {
        t.Fatalf("%+v", hist)
    }
}
```

- [ ] `go test ./internal/app -count=1 -run PeopleAgent`

---

## Task 8: 会议 VAD 400 + 切句 + 状态 + merge（D）

**Files:**

- `api/bridge/v1/voice.start.schema.json` — properties 增加：

```json
"endWindowMs": { "type": "integer", "minimum": 200, "maximum": 3000 }
```

positive 例可加 `{ "backend":"volc", "providerId":"01ARZ3NDEKTSV4RRFFG69G5FAV", "endWindowMs": 400 }`（用仓库已有 ULID 样例 `01ARZ3NDEKTSV4RRFFQ69G5FAV`）。

- [ ] `npm run generate:bridge`
- [ ] `internal/app/voice_handlers.go` `handleVoiceStart` / `startVolcVoice` 读 `EndWindowMS int`，`cfg := ConfigFromSecret(...); cfg.EndWindowMS = p.EndWindowMS`
- [ ] `volcAsr.ts` `startPayload` 带可选 `endWindowMs`
- [ ] `startVolcCompanionSpeech` / `startVolcAsr` 增加可选 `endWindowMs?: number`。月伴不传。
- [ ] `meetingAsr.ts`：`startVolcCompanionSpeech(..., { endWindowMs: 400 })`；`MEETING_MERGE_GAP_MS` 1000→400；`onInterim`/`onFinal` 外包：

```ts
let sessionCommitted = ''
onInterim: text => options.onInterim?.(isolateCurrentUtterance(sessionCommitted, text))
onFinal: text => {
  const current = isolateCurrentUtterance(sessionCommitted, text)
  if (!current) return
  sessionCommitted += current
  buffer.push(current)
}
```

`holdUtterance: true` 仍可留给 endpoint 窗；不要再靠 absorb 拼整场。

- [ ] `MeetingPage.tsx`：`await startMeetingSpeech(...)` **成功之后** `setNotice(prev => prev === '正在连接火山听写…' ? '正在听写' : prev)`。`onInterim` 非空也清掉「正在连接」。
- [ ] 测试：`volcsauc/config_test.go` 默认仍 1200；新增 `TestVolcConfigEndWindowMSHonorsExplicit400`（`cfg.EndWindowMS=400; if cfg.endWindowMS()!=400`）。
- [ ] `meetingAsr.test.ts`：volc 启动带 `endWindowMs: 400`（给 `startVolcCompanionSpeech` mock 断言）。merge 400：改现有 1000 断言。
- [ ] `npx vitest run src/meetings/meetingAsr.test.ts src/meetings/MeetingPage.test.tsx`；`go test ./internal/voice/volcsauc ./internal/contract -count=1`

D5 横幅：本机未就绪时已有 `MEETING_CATCHUP_HINT` / `MEETING_LIVE_UNAVAILABLE_NOTICE`。若文案不含「系统声会在停止后补」，补一句，不要新 notice 体系。

---

## Task 9: 回归门

- [ ] `go test ./internal/app ./internal/voice/volcsauc ./internal/people ./internal/contract -count=1`
- [ ] 覆盖率不低于 49%（现网门）：`go test ./internal/app -coverprofile=cover.out -count=1` 后看 `go tool cover -func=cover.out`
- [ ] `npx vitest run src/meetings src/session/companion/volc src/session/companion/companionText.test.ts src/session/companion/speech.test.ts src/session/companion/CompanionStage.a11y.test.tsx`
- [ ] 不跑 `Test-Install.ps1`；不改 VERSION

人工 §7 清单在作者机，不进本计划自动化。

---

## Spec coverage

| 锁 | Task |
|---|---|
| A1–A3 本句显示+喂模型 | 1, 3 |
| A4 插话最新 | 4 |
| A5 lead-in 复用 | 6（不新造句）；舞台已有 lead-in |
| A6 月伴 1200 | 8 月伴不传 endWindowMs |
| B1 不降 token | 5 明确不改 |
| B2 字幕+TTS+停流 | 5 |
| B3–B5 media 一次 | 6 |
| C1 历史去重本轮 | 7a |
| C2 三分类 | 7b |
| C3 补 excel.gen | 7c |
| D1–D6 会议 | 2, 8 |
| §12 勘误 | 已写入对应 Task |

## 不做（计划外）

- 改 `result_type`
- 云端听系统声
- 汽水/网易云 GUI 自适应
- 升版打包
