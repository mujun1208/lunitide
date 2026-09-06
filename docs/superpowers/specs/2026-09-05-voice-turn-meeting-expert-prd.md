# 月伴轮次 / 收口停说 / 专家跟轮 / 会议听写 — 可落地 PRD

日期：2026-09-05  
版本：定稿。四条独立轨道，一份事实源。  
状态：2026-09-05 已按改后锁落地，随 **0.4.65** 发布。  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  
对照安装包：`v0.4.64`（星尘 + 执行收口已上船后的实测回归）。

**本文是本波唯一事实源。** 与 [2026-09-05-companion-execute-followthrough-prd.md](./2026-09-05-companion-execute-followthrough-prd.md) 冲突处，以本文为准，且只解锁下文点名的锁。

不改：`VERSION`（落地后另开版本）。不新 daemon。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方 / `AnnotateCapture` / `assignNodeIDs` / 专家六段身份表。不改 `speech.ts` 的 280/220 云端 cascade 窗。

---

## 0. 打分

| 口径 | 分 | 含义 |
|---|---|---|
| 当前（0.4.64 实测 + 现码） | **1.8 / 5** | 四条都是可复现缺陷，不是体验微调 |
| 按本文落地并过验收 | **4.9 / 5** | 契约可测；剩 0.1 是火山云端 VAD 细节与歌词回灌，不假装关掉 |

分项（当前 → 目标）：

| 轨 | 当前 | 目标 | 为什么扣 0.1 |
|---|---|---|---|
| A 月伴火山轮次 | 2.0 | 4.9 | 极端回声仍可能闪一下再丢掉 |
| B 月伴收口停说 | 1.5 | 4.9 | 播放器 UI 各异，一次失败后不再盲试，可能要用户说歌名 |
| C 同事专家跟轮 | 1.5 | 4.9 | 超长第一轮会被裁到定额，不是全书回放 |
| D 会议三路听写 | 1.5 | 4.9 | 云端永远听不到系统声（浏览器引擎限制），只做到诚实 + 更快上屏 |

4.9 不是「感觉顺了」。每轨有可跑测试 + 一条人工 3 分钟清单。任一轨验收失败，整波不得自称 4.9。

---

## 1. 用户原话锁成产品句

1. **一轮一句。** 我说完第一句，再开第二句：字幕只显示第二句，上一句从当前条消失。喂给模型的也只有这一轮。他答这一轮。处理中先说「稍等」，做完再说结果，然后结束本轮。我插话：按**最新一句**答，旧轮取消。
2. **说完就停。** 火山月伴处理任务时简短。做完或失败就结束，不许无限复读「我去播 / 我暂停 / 你要听哪首」。
3. **专家第二轮必须还能做。** 图2：第一轮已经长文回答，第二轮「做个模拟计算表」报「请先启用对话模型」是错的。模型已配置。要做表或诚实说为什么这轮没做成。
4. **会议听写三路都要能用。** 图3 火山：要跟嘴出字，禁止同一句贴十几遍。图4 云端：不许 silently 丢大段；慢可以，丢不行；状态字必须是真的。本机：保持「比较全」，出字再跟嘴一点。

---

## 2. 现码根因（对照 0.4.64，不是猜）

### A. 月伴火山：第一句黏第二句，且会进模型

- 火山 SAUC 握手写死 `result_type: "full"`（`internal/voice/volcsauc/backend.go`）。同一条 websocket 上，每次回调都是**从开流到现在的全文**，不是本句增量。
- 0.4.64 为了避免她说完后冷启动，**故意不关 websocket**（`volcSpeech.ts` `setAssistantPlayback`）。于是第二句的 `full` 文本 = 第一句 + 第二句。
- `absorbHeldTranscript` 在 `next.includes(prev)` 时直接返回 `next`（全文）。`onInterim` / `onFinal` 拿到的就是黏好的墙。
- `CompanionStage.beginUserTurn` 会 `setRounds([{ role:'user', text }])`（整轮替换），但 `text` 已经是黏墙。`onSend(text)` 把墙喂给模型。
- 用户担心「会不会追加喂给模型」：**会。**

`DefaultEndWindowMS = 1200` 不是本条主因。它只让定稿更晚。主因是 `full` + 长连接 + 客户端当增量拼。

### B. 月伴说个没完

图1：`media.play` `ok:false`（没核验到在播 / 找不到列表项）后，又一次 `media.play` 发了暂停/播放拨动。字幕循环「打开汽水 / 搜复古公路风 / 我暂停 / 你要听哪首」。

现码合力：

- `companionMaxTokens = 2048`，语音轨允许写长文。
- `media.play` 算桌面手。`shouldContinueDesktopTurn` 在失败后仍续跑（最多 5 次桌面 nudge）。
- `companionToolResultSpeech` 失败时说「这次没有完成。」成功路径中间工具说「还在处理。」模型把这些当继续指令。
- 执行收口的 wait/leadin（最多 3 次）在「稍等」场景是对的；套在播歌失败上会逼她再试。

### C. 图2 专家第二轮「没配模型」

报错原文在 `peopleAgentNoReplyUserError()`：空回复一律写成「请先在设置 → 模型与供应商里启用一个对话模型」。

- 图2 是**同事专家 1 对 1**（People），不是工作台 `SessionPage`。
- `completePeopleAgentText` / `WithTools` 的 messages **只有 system + 本轮 user**，不带线程里上一轮问答。
- `peopleCompanionMemoryHint` 是检索槽，不是上一轮正文。用户说「好，做个模拟计算表」时，模型手里没有周转件算法。
- `excel.gen` 允许且应当被调用；缺数字 / 工具失败 / 步数用尽后 content 为空 → 走同一句「没配模型」。
- 第一轮已经长文回答，证明 `resolvePreferredChatModel` 当时是成功的。第二轮再报没模型，是**诊断撒谎**。

`resolveChatModel` 在目录非空时会回退 `catalog[0]`。真正「没模型」只有 `KindLLM` 目录为空。空回复 ≠ 没模型。

### D. 会议听写

- 会议复用 `startVolcCompanionSpeech`，`holdUtterance: true`，再套 `createMeetingLineBuffer`。
- 火山同一条 `full` 流 + `absorbHeldTranscript` 把歌词墙黏在一起；`collapseTandemRepeats` 只吃「整句首尾相接」，吃不掉略变体循环。
- `DefaultEndWindowMS = 1200` **月伴和会议共用**。会议要跟嘴，却等约 1.2s 才定稿，再一次性倒墙。这就是图3「先卡、后重复」。
- `MeetingPage` 在 `startMeetingSpeech` 返回前一直挂「正在连接火山听写…」。字已经上屏时状态仍可能停在连接（图3）。
- 云端 Web Speech **不能注入系统声 PCM**（现码注释已写死）。图4 丢内容 + 慢，一半是引擎限制，一半是 `MEETING_MERGE_GAP_MS = 1000` 把 interim 攒成段才出。
- 本机 sherpa 听混音，所以「比较全」；出字慢同样是 hold + merge 窗。

---

## 3. 三条路径与推荐

**推荐：路径 1 — 按面拆契约，共用两个纯函数，不重写识别引擎。**

| 路径 | 做什么 | 代价 | 4.9？ |
|---|---|---|---|
| **1 按面拆（推荐）** | 客户端把 `full` 切成「本句」；会议 / 月伴各写 `EndWindowMS`；专家补线程历史 + 诚实错误；播歌失败即停 | 改动面清楚，可单测，不赌火山改 `result_type` | 能 |
| 2 统一成一台「话语状态机」 | 月伴 + 会议 + 云端共用一个新 ASR 内核 | 重写面大，回归周期长，和 0.4.64 收口抢同一批文件 | 晚，且容易带回切半句 |
| 3 只改提示词 / 文案 | 告诉模型「别黏、别啰嗦」 | 图1/图3 是识别缓冲，提示词碰不到 | 不能 |

否决路径 2、3。路径 1 的共享面只许两个纯函数：

1. `isolateCurrentUtterance(committed, incoming) → current` — 从火山 `full` 文本取出本句。
2. `classifyPeopleAgentFailure(err, text, catalogOK) → {code, userText}` — 专家空回复不得再写「没配模型」。

---

## 4. 四轨产品锁

### 轨 A — 月伴火山轮次隔离

**A1. 显示本轮，喂模型也是本轮。**  
火山 `onInterim` / `onFinal` 在进舞台之前必须经过 `isolateCurrentUtterance`。`beginUserTurn` 收到的字符串不得包含上一轮已 `onFinal` 的正文（去空白后不得 `includes` 上一轮 committed）。

**A2. 切句规则（锁死，便于单测）。**

设 `committed` = 本条 websocket 上已经交给舞台的定稿拼接（去空白）。`incoming` = 引擎这次 `full` 文本。

比较前先对双方做 `compactMeetingText`（去标点空白）。顺序只能是：

| 关系 | 输出 |
|---|---|
| `incoming` 空 | 保持上一帧 current，不 emit |
| compact 相等 | 空 current（本句已提交，等下一句） |
| compact(incoming) 以 compact(committed) 为前缀 | 去掉该前缀（按原文切片），再 `collapseTandemRepeats` |
| compact(committed) 以 compact(incoming) 为前缀且 incoming ≥ 4 字 | 修订中：后缀空则保持上一帧 current |
| `suffixPrefixOverlap(compact)` ≥ min(8, floor(len/2)) | 只取重叠之后的新字 |
| 以上都不成立 | **新句替换**：current = collapse(incoming)。**禁止** `joinMeetingLines`。若 `incoming.includes(committed原文)` 仍必须先剥 committed，不得把两句当新句 |

禁止在月伴路径（`holdUtterance === false`）再调用 `absorbHeldTranscript` 做跨句黏连。会议路径见轨 D。

**A3. 定稿后清字幕条。**  
`onFinal(current)` 之后：舞台 `setRounds([{role:'user', text: current}])`，`setInterimText('')`。下一句 interim 只画新 current。旧用户句从**当前条**消失。历史不进月伴舞台（月伴本来就只画当前轮）。

**A4. 插话 = 最新一句。**  
她在 thinking/speaking：新的非回声 current 视为插话。取消旧流、打断 TTS、`onSend(最新 current)`。废弃「下一句已记下，这轮说完就回」对火山路径的排队（云端 / 本机按钮打断保持原样）。`looksLikePlaybackEcho` 仍先滤回声。

**A5. 处理中话术。**  
本轮已派出工具、模型还没给结果：只口播**现有** `companionToolLeadIn`（「好，我来播放。」/「好，我帮你查一下。」等），然后禁声直到工具回包。回包后口播结果（轨 B），结束本轮。禁止再发明第三句「稍等，我去处理。」禁止在等工具时继续生成闲聊。

**A6. 不改。**  
月伴 `DefaultEndWindowMS` 仍是 **1200**（上一版用户锁：换气不得切半句）。`speech.ts` 280/220 不动。云端 / 本机月伴不接对麦插话。

### 轨 B — 月伴任务收口停说

**B1. 模型 token 不降。**  
`companionMaxTokens` **保持 2048**。降到 512 会截断 `desktop.type` / `computer.act` / 工具 JSON，填表轨会坏。啰嗦不靠砍 completion budget。

**B2. 可见+可听硬顶。**  
新纯函数 `clipCompanionSpokenTurn(text) → {spoken, overflow}`：同一轮最多 **2 句或 80 字**（先到为准），按。？！切。lead-in 算第 1 句，结果算第 2 句。

必须同时作用：

1. `setRounds` 助手条（图1 大气泡就是这里没封顶）
2. TTS `enqueue` / `takeSpeakableChunk` 之后
3. overflow 为真时 `onCancel` 停流，不再入队剩余 delta

只封 TTS、不封字幕，验收过不了图1。

**B3. `media.play` 一次定生死。**  
本轮已调用过 `media.play`（不论 ok）：

- `ok:true` 或输出含「正在播」→ 口播「已经在播了。」**立刻结束**，禁止第二次 `media.play`，禁止桌面 continue。
- `ok:false` → 口播「没播成。」+ 一行原因（已有「无法执行：…」则用它；否则「没找到这首歌，请说出歌名或歌手。」）**立刻结束**。禁止再搜、再暂停拨动、再问「你要听哪首」。

实现：`companionRedundantMediaSkip`（对称现有 `companionRedundantWebSkip`）。`unverifiedMediaPlay` 只在「已启动但未核验」且本轮 **0 次** play 回包时续一次；第二次必须停。

**B4. 桌面续跑不得套播歌。**  
`isDesktopControlTool` 仍含 `media.play` 以便权限。`shouldContinueDesktopTurnGoal`：若 `lastTool == media.play`，返回 false（成功失败都停）。填表 / 点控件 / `computer.act` 续跑规则不动。

**B5. 稍等收口仍在。**  
`looksLikeCompanionWaitPromise` / lead-in：仍最多 3 次 nudge。用尽说「无法执行：这一轮没有完成查询。」然后停。与 B3 同时成立时 **B3 优先**。

**B6. 不改。**  
不接线 `companionOpeningAck`。不改 TTS 音色。不改 idle 门（R0 无 needle 仍不挂工具）。

### 轨 C — 同事专家跟轮 + 诚实错误

**C1. 跟轮必须带线程。**  
`completePeopleAgentText` / `WithTools` 的 messages：

1. system（人设，已有）
2. 本线程最近 **最多 8 条** 非 system 文本消息（user / 本专家 assistant），**去掉本轮正在处理的那条 user**（`ListMessages` 里已有它，禁止再拼一次），按时间正序，合计裁到 **6000 字**（从最旧往下丢）
3. 本轮 user（只出现一次）

禁止只发「好，做个模拟计算表」而不带上一轮航材算法正文。记忆槽仍可附在 system，但不能替代线程。`localBrainPrompt` 已有摘录；本锁只补 **BrainLunitide** 的 `Complete` messages（图2 走这条）。

**C2. 错误三分类。** 禁止再用 `peopleAgentNoReplyUserError` 兜一切。

| 条件 | 用户可见 |
|---|---|
| `KindLLM` 目录为空，或 `resolvePreferredChatModel` 失败且目录为空 | 「这轮没有生成回复。请先在设置 → 模型与供应商里启用一个对话模型，再发一次。」 |
| 目录有模型，工具/补全报错 | 「这轮没做成：{裁到 80 字的真原因}。请再发一次。」 |
| 目录有模型，调用成功但正文空 | 「这轮没有生成回复。模型已启用，请换个说法或把数字再发一次。」 |

旧函数只留给第一行。测试必须锁：第一轮成功后第二轮空回复 **不得** 再出现「启用一个对话模型」。

**C3. 做表。**  
用户本轮要表（复用已有 `looksLikeExcelTask` / 「计算表 / 模拟表 / 表格」）且本轮工具列表**没有**成功的 `excel.gen`：在步数用尽或模型只吐空文时，走现成 `fallbackOfficeGenArgs("excel.gen", …)` **自动补一次**（工作台已有这条，同事通道没有）。缺数字导致 gen 失败：用 C2 第二/三行，禁止假装没模型。`peopleAgentMaxTokens`：**1600 → 2400**。步数 8 不动。

**C4. 不改。**  
不改专家六段身份。不把同事通道改成 `executionModeFullAccess`。不恢复 `command.run` / `computer.act`。图2 文案与 `PeoplePage.tsx`「在线 · 一对一 · 局域网投递，文件需对方确认」对得上，**本轨就是 People**，不是工作台 SessionPage。

### 轨 D — 会议听写三路

**D1. VAD 按面拆。**  
`Config.EndWindowMS` 已存在，但 `ConfigFromSecret` / `voice.start` **从未赋值**，所以会议也是 1200。必须打通：`voice.start.schema.json` 增加可选 `endWindowMs`（200–3000）→ `startVolcVoice` 写入 `Config.EndWindowMS` → `volcAsr.startVolcAsr({ endWindowMs })`。会议传 **400**，月伴不传（默认 1200）。

| 面 | `EndWindowMS` | 理由 |
|---|---|---|
| 月伴火山 | **1200**（默认，A6） | 换气不切半句 |
| 会议火山 | **400** | 跟嘴出字；定稿仍走 `meetingLineDelta` |

`TestDefaultEndWindowMSMatchesSherpa` 改语义：默认常量仍 1200；另加 `TestMeetingVolcEndWindowMSIs400`。禁止把月伴默认改回 400。

**D2. 会议不得跨句黏 `full`。**  
`startMeetingSpeech` 的 `onTranscript` 在进 `createMeetingLineBuffer` 之前：

1. `isolateCurrentUtterance(sessionCommitted, incoming)` 得到 current
2. `collapseTandemRepeats(current)`
3. 只把 **delta**（相对已 emit）推进 buffer

`holdUtterance: true` 只表示「会议不抢答、2s 内换气还算同一句」，**不是**把整场会议拼成一段。禁止对会议再 `joinMeetingLines` 无重叠的两段歌词。

**D3. 重复墙。**  
`collapseTandemRepeats` 增加：连续出现 ≥2 次、长度 ≥ 8 字的子串，只留 1 次（现有整句相接不够）。单测用图3 那种「虚情假意…」循环墙，输出不得长于原文的 1.3 倍，且循环体只出现一次。

**D4. 状态字要诚实。**  
`startMeetingSpeech` **resolve 成功**后立刻清掉「正在连接火山听写…」，改为空或「正在听写」。`sawRealCaption` 只用于 fallback 看门狗，不再卡住连接文案。interim 非空时禁止仍显示「正在连接…」。

**D5. 云端（图4）。**  
- 运行时行必须继续写清：`音源：浏览器听写`（麦克风 only）。不得暗示在录系统声。
- `MEETING_MERGE_GAP_MS`：**1000 → 400**（只影响会议 buffer；月伴不走这个 buffer）。
- 已有句号且 ≥ 8 字：80ms 上屏（现码已有，保持）。
- 停止后补转写仍走本机 sherpa（已有）。云端 live 丢的系统声，停录后必须出现在补转写里；若本机未就绪，横幅说明「系统声会在停止后补，本机识别未就绪则没有」。

**D6. 本机。**  
不改 sherpa 1.2s 引擎终点。只把会议 buffer 的 merge 窗改到 400ms（D5），让「比较全」的字更快上屏。不把本机改成火山。

**D7. 不改。**  
不把会议火山改成 `result_type=single`（不赌云端协议）。不重写 `meetingCapture` / WASAPI。不把云端 Web Speech 强行灌 PCM（做不到）。

---

## 5. 数据流（按轨）

### A/B 月伴火山一轮

```
mic PCM → 长连接 SAUC (result_type=full, end_window=1200)
       → isolateCurrentUtterance(committed, full)
       → interim: 只画 current
       → 静音 1200 + 文本稳定 → onFinal(current)
       → committed += current
       → beginUserTurn(current) → onSend(current)
       → 若要工具: 口播现有 companionToolLeadIn → 工具
       → 结果 ≤2 句/80 字 → 结束
       → 插话: interrupt + onSend(最新 current)
```

### C 同事专家一轮

```
people message
  → 读线程最近 ≤8 条 + 本轮
  → resolvePreferredChatModel
      目录空 → 没配模型（唯一合法说这句话的地方）
      有模型 → tools 环（可 excel.gen）或 text
  → 空/错 → classifyPeopleAgentFailure（诚实）
  → 成功 → 写入线程 + recordPeopleTurnMemory
```

### D 会议一句

```
混音 PCM → 火山 end_window=400 / 本机 sherpa / 云端 Web Speech
        → isolate + collapseTandemRepeats
        → meetingLineDelta → live 行 + meetings.append
        → 连接成功即不再写「正在连接」
        → 停止 → sherpa 补转写
```

---

## 6. 文件与测试（派工边界）

只许为这四轨改这些面。禁止顺手改玉盘 / 星尘 / poison。

| 轨 | 主文件 | 必须新增或改的测试 |
|---|---|---|
| A | `web/src/meetings/meetingText.ts`（`isolateCurrentUtterance`）`web/src/session/companion/volc/volcSpeech.ts` `web/src/session/companion/CompanionStage.tsx`（火山插话不再排队） | `isolateCurrentUtterance`：带句号的 full 黏两句 → 只出第二句；`volcSpeech.test.ts`：第二句 onFinal 不含第一句 |
| B | `internal/app/chat_continue.go` `internal/app/chat_companion_speech.go` `internal/app/chat_run_stream.go` `web/src/session/companion/companionText.ts` 舞台入队处 | **不改** `companionMaxTokens`；`media.play` 失败后不再 continue；第二次 `media.play` 被 skip；`clipCompanionSpokenTurn` 截字幕+TTS |
| C | `internal/app/people_agent.go` + 读线程的 people store | 第二轮空回复 ≠ 没配模型文案；messages 含上一轮 user/assistant；「做个表」且历史有数字时 tools 含 `excel.gen` |
| D | `internal/voice/volcsauc` 开会话时填 `EndWindowMS`；`meetingAsr.ts` `MeetingPage.tsx` `speech.ts` `collapseTandemRepeats` | 会议 volc 400；月伴默认仍 1200；歌词墙 collapse；连接成功后 notice 不含「正在连接火山」 |

覆盖率：本波新增 Go 语句覆盖不得把仓库总覆盖压到 49% 以下（现网门）。前端相关 vitest 必须绿。

---

## 7. 人工 15 分钟清单（装包后，作者机）

1. **月伴火山。** 说「今天合肥天气怎么样」，等她开口；再说「算了，放首歌」。字幕只有第二句；她按第二句走，不得再答天气。
2. **插话。** 她说话中说「停，改放周杰伦」。旧轮停，只答最新句。
3. **播歌失败。** 故意说一个不存在的歌名。她最多两句，然后静音。工具轨不得出现第二次 `media.play`。
4. **同事专家。** 航空航材专家：第一轮问周转件算法（应有长文）；第二轮「做个模拟计算表」。必须出表或诚实缺数字，**禁止**红字「启用一个对话模型」。
5. **会议火山。** 放一段 40 秒歌或说话。10 秒内应有字，不得整墙同一句重复 ≥3 次。状态不得停在「正在连接」超过 websocket 已通之后。
6. **会议云端。** 只说话（麦克风）。字应在约 1 秒内出现；停录后若有系统声缺口，横幅或补转写说明。运行时行写「浏览器听写」。
7. **会议本机。** 同段内容应不少于云端；出字间隔肉眼短于 0.4.64。

---

## 8. 明确不做（本波结束后仍是 0.1）

- 火山云端真实 VAD 细节、把 `result_type` 改成 `single`。
- 云端听系统声（浏览器做不到）。
- 播放器 GUI 自适应（汽水 / 网易云控件各异）：失败即停并请用户说歌名，不盲点。
- 贾维斯 / Three / 新手势 / 新 daemon。
- 工作台 SessionPage 专家挂载（图2 已核实是 People）。

---

## 9. 落地顺序

1. 纯函数 `isolateCurrentUtterance` + `collapseTandemRepeats` 加强 + 单测（A/D 共用）。
2. `volcSpeech` 月伴切句 + 舞台插话改最新句（A）。
3. 播歌一次定生死 + token/口播硬顶（B）。
4. 专家线程历史 + 错误三分类（C）。
5. 会议 `EndWindowMS=400` + notice + merge 400ms（D）。
6. 人工清单。另开版本号与安装包（需单独开工令）。

---

## 10. 与上一版 PRD 的解锁声明

上一版锁：`DefaultEndWindowMS` 400→1200，全进程一个数。

本文解锁为：**常量默认仍 1200（月伴）；会议会话显式传 400。** 不是把月伴改回 400。`speech.ts` 280/220 仍冻。

上一版锁：说了稍等必须跑完。本文不撤。B3 只禁止「失败后继续盲试播歌」，不禁止天气查询跑完再说。

---

## 11. 自检

- 无 TBD / TODO。
- 四轨接口清楚：两个纯函数 + 按面填 `EndWindowMS` + people messages 加历史。
- 「没配模型」只有目录为空时能出现。
- 黏句问题同时锁了显示和 `onSend`。
- 范围是四轨修复，不是新子系统。一份计划可派工。

---

## 12. 复核勘误（2026-09-05 晚，原话 × 现码 × 初稿）

初稿作为规格 **3.6 / 5**：根因对，但有 7 处按原文落地会修偏或修不完。本节已写回 §4，执行以改后的锁为准。

| # | 初稿问题 | 现码证据 | 改后 |
|---|---|---|---|
| 1 | A2「无前后缀 → 整段当新句」 | 火山 `result_type=full` 常带句号：`今天天气怎么样。算了放首歌`，`startsWith` 失败会把两句再喂给模型 | 先 `compactMeetingText` 再切；`includes(committed)` 必须先剥 |
| 2 | B1 `companionMaxTokens` 2048→512 | `chat.start` 的 MaxTokens 含工具 JSON；填表 `desktop.type` 会被截 | **不降**；啰嗦用 `clipCompanionSpokenTurn` |
| 3 | B2 只封 TTS | 图1 大气泡是 `setRounds` 累加 `assistantText`；只封 TTS 画面仍循环 | 字幕 + TTS + 停流三条一起封 |
| 4 | A5 新造「稍等，我去处理。」 | 已有 `companionToolLeadIn` | 复用，不第三句 |
| 5 | C1 历史 8 条 + 再拼本轮 | `ListMessages` 已含本轮 user | 历史去掉本轮再拼一次 |
| 6 | C3「必须 excel.gen」只靠模型自觉 | 工作台有 `fallbackOfficeGenArgs`，People 没有 | 空文/步数用尽补一次 gen |
| 7 | D1 以为填 `EndWindowMS` 即可 | `voice.start` schema 无此字段；`ConfigFromSecret` 永不赋值 | 打通 schema → handler → volcAsr |
| 8 | D3「出现两次就删」过宽 | 会吃掉正常术语重复 | 只去相邻 tandem |
| 9 | C4 还在猜图2 是不是工作台 | `PeoplePage.tsx` 551 行文案与截图一致 | 锁 People |

规格改定后作为文档 **4.7 / 5**。产品 4.9 仍取决于按改后锁做完 §7 清单。
