# 对话任务分档与质量门 v1.4

> 文档版本：v1.4（可开发）  
> 状态：Ready for Implementation Review  
> 日期：2026-09-11  
> 基线：`feat/prd-v7-s1-continuity` @ `1326fb24` / 0.4.75 + 本工作区办公台接线 WIP + 余票 fail-closed 工作区改动  
> 性质：产品 + 工程合同。实现按本文改现有 `internal/app` 循环，不新开对话引擎。  
> 冻结：`token_ledger`、普通聊天/项目分离、统一 Runtime、S-01/S-02、`STREAM_INCOMPLETE` 残流不放工具。不接邮箱/共享日历。`html.gen` 仅三模板。不编节省百分比。

## 0. 相对前版的改口（必须先读）

| 旧合同 | 现合同 | 原因 |
|---|---|---|
| v1.0：L1 即使 @ 两位也不开会 | 必须开会，档不升 L3 | @ 了还不讨论是违令 |
| **v1.1：挂载≠会商，必须再 @ / 芯片才开会** | **会话挂载 ≥2 即本轮会商名单。@ / 芯片不再当第二道门。** 卸载才退出 | 对话里已经挂上，再 @ 一遍是多此一举 |
| 会商=现网 8×6 Complete + 主席逼搜网/出文件 | 会商是**档上叠加层**：L1–L2 每位 1 步、无工具；主席提示跟档，禁止把润色劫持成调研/出文件 | 准 + 稳；避免 10 分钟预算被会商吃光后主循环卡死 |
| L2 用 `DisableReasoning=true` 躲开报告流水线 | **禁止**拿推理开关当流水线闸。L2 走与办公台相同的「跳过调研流水线」门；`DisableReasoning` 只关思考输出 | 现码 `startDocxWorkflow` 见 `DisableReasoning` 直接 return；`shouldContinueDocxTurn` 又无视它（FR-502）。混用会卡壳或漏催 |
| 「写周报」R4 = 工具表原样含搜索 | **路由标签仍是 R4**（不扩成全集、不收成 R0）。L2/L2-ask **从允许表扣掉** `web.search`/`web.fetch` | R4 是能力边界收缩，不是「必须搜两轮」 |
| 实现另开含糊计划 | 本文给出类型、落点、验收 ID，可按 TDD 直接开干 | 业主要可落地 |
| 月伴打开桌面文件走 workspace/command | **只许 `desktop.open`，打开成功即停**；禁止读完再写一份 | 2026-09-11 真机：断句后误当成「增补文档」，Administrator 桌面列目录失败，第二次读到文件后还继续写 |
| 再点播放抽成歌名去搜 | 续播/再点 = `action=play` 空 query；暂停/下一首/停止走 media session | 汽水「started playing」仍被环当成未验证 |
| 高铁/车票自动 `web.search` + 刮 12306 | **无余票 MCP 则说明尚未接入并停**；禁止 fetch/browser；最多 2 步 | 2026-09-11：10 次工具、界面「执行中」像死锁 |
| sherpa 垃圾字也 `chat.start` | 幻觉听写不得开轮；提示没听清 | 图1：`hellolo ty ty` + `COMPANION_REPLY_STALL` |
| 口头「不说了/撤了」当新任务 | 只撤/取消 = 打断，不新开 `chat.start` | 图2：撤了之后停在「正在接，还没开口」 |

**优先级（冲突时按此裁，不得为后面的项牺牲前面的项）：**

1. **听指令** — 用户原话、**已挂载的专家**、「不要搜」「深度思考」胜过默认档。挂载就是点名  
2. **准确** — 无材料不编本周事实；工具 400 不装已执行；**挂载 ≥2 = 会商**，不要第二道 @
3. **稳定** — 残流/无人值守/空稿门/隔离 Mermaid 不回退  
4. **能力** — L3/L4 与办公生成、桌面、代码保持现网  
5. **性能** — 少轮次、少 schema、会商按档封顶  
6. **速度** — 首字流式；禁止为约 200ms 撤 Mermaid 隔离；禁止用静默剥工具换快

## 1. 整条链路（现码）与卡壳点

```
chat.start
  ├─ 装配信封 AssembleEnvelope；失败可 explicit fallback（打字仅序列错误）
  ├─ 高水位同步压缩（打字挡首字；语音跳过）
  ├─ buildExpertCouncilConfig（现码：无 @ 的多挂载 = 不开会 → v1.2 改为挂载≥2 即开会）
  ├─ 工具：profile → route(R0–R4) → flash 补洞
  └─ go runStream
        ├─ applyExpertCouncil  【挡首字；Complete 计入 10 分钟整轮预算】
        ├─ OfficeTask 空：startPpt/startDocx（DisableReasoning 则根本不启动）
        ├─ OfficeTask 非空：强制关掉 Ppt/DocxActive（办公台已离线条款）
        └─ for step < 24→48
              Stream → 工具 / 自动补搜(仅行情类) / 流水线拦 *.gen / 继续催促
```

### 1.1 听指令缺口（本版要堵）

| 现象 | 代码 | 用户感受 |
|---|---|---|
| 只挂载多人、未 @，不开会 | `selectedTurnExpertIDs` 无引用且挂载≠1 → nil | UI 已挂上，引擎当没点名。**v1.2 改口**：挂载名单就是会商名单，再 @ 是多此一举 |
| 本轮芯片与挂载不一致时芯片赢 | `selectedTurnExpertIDs` 先扫 `[引用专家]` | 会把已挂未 @ 的人排除。**v1.2**：不以芯片剔除挂载；卸载才退出 |
| 项目管理静默 `session.experts.set` 前几名 | 旧测「芯片打败过期挂载」 | 禁止引擎替用户静默改挂载。过期人请用户卸，不要靠 @ 过滤 |
| 「请三位一起评」但会话 0/1 人 | `buildExpertCouncilConfig` 静默 nil | 可见「请先挂载至少两位专家」，不开会、不装已评 |
| 主席提示「必须搜网 + *.gen 做完交付」 | `councilChairInstruction` | 已挂两位只想润色 → 被劫持成出 Word。**听指令失败** |

### 1.2 准确缺口

| 现象 | 代码 |
|---|---|
| 「写周报」无材料仍强制两轮搜 | `looksLikeReportTask` → `startDocxWorkflow` 注入 `reportPipelineInstruction`；`docxGenBlocked` 未齐则 `reportGenBlockedMsg` |
| `officeMaterialReview` 只覆盖「分析/审阅+附件」 | 光说写周报不走离线条款 |
| `referenceOnlyOfficeTurn` 要「已有/附件/转为」等词 | 用户贴了三条要点但没说「附件」→ 仍调研 |
| 办公台已写「来源变换不强制旧流水线」 | `office_context.go`；**打字对话另一条路** |
| 交互 400 已有「切纯对话」提示 | 后续模型仍可能写「已写入文件」。提示必须钉在正文首段 |
| 专家会商自带 `web.search`/`*.gen`（最多 6 步） | 无材料专家会「搜出」假周报 |

### 1.3 稳定 / 卡死

| 现象 | 会不会卡 | 处理 |
|---|---|---|
| 会商与主循环**共用** `turnGenerationBudget`（10 分钟 / 512KiB / 131072 token） | 8 人 × 6 步搜网可在主席开口前耗尽，主循环直接 `errTurnGenerationBudget` | L0–L2 会商 1 步无工具；L3 2 步；L4 保持 6。并行仍 3 |
| `DisableReasoning` 阻止 `startDocxWorkflow`，但已激活流水线的催促无视它 | L2 若只靠关推理：新开周报不进流水线（碰巧好），续跑旧 checkpoint 仍 nudge 搜网（坏） | 档位布尔 `SkipOfficeResearchPipeline`，与推理开关解耦 |
| `workflowDisciplineClause`「不要停下来问」vs 无材料应问 | 模型不调用 `user.ask`、空转或硬搜 | L2-ask **扣掉生成与搜索**，只留 `user.ask`；覆盖纪律为「问一次就停」 |
| `continueNudgeText` 在已用工具后追问时再催 | L2-ask 若误留 `docx.gen`，空稿拒绝 → 再催 → 再搜 | L2-ask 不催促完成文件 |
| 打字 `user.ask` 走审批卡，语音才就地停 | 问材料可能弹出审批而不是结束 | L2-ask 优先自然语言问句；若调用 `user.ask`，本轮在发出问题后停，不把「未出文件」当失败 |
| 高水位压缩同步挡首字 | 长会话第一次像死机 | 不改压缩合同；L0/L1 不额外再挡 |
| 办公归档 `go` 不挡完成 | 中间栏曾空（本工作区已修） | 合入门，不重做 |
| Handler panic 只 `fmt.Errorf` 无栈 | 难查 | P1 打 `debug.Stack()`，不挡 P0 |
| Mermaid 回主页面 | 主线程卡死风险 | 隔离 worker 硬约束 |
| 高铁余票无接口仍刮 12306 | `web.fetch` 10–15s × 多 URL；环 24→48；Playwright 未就绪 | 主机 fail-closed：只 `mcp.search`，最多 2 步；见 §12.7 |
| 听写垃圾开轮后无首 token | 渲染进程 6s/12s `COMPANION_REPLY_STALL`；引擎可能仍在跑上一轮 | 拒收幻觉字；stall 必须真正 `onCancel` 掉引擎轮；见 §12.8 |
| 口头撤了仍 `beginUserTurn` | 本机 sherpa「说完再答」，打断只有按钮；「撤了」被当成新 Goal | 纯取消口语 = `cancelReply`；见 §12.9 |
| 引擎日志不记工具 | `%LocalAppData%\Lunitide\logs\engine-*.log` 只有 preturn/guidance/失败 | 每步 `tool start name=`；卡死先对工具名再对 STALL |

### 1.4 能力（不要误伤）

- R2 打开 Word、R3 浏览器、L4 代码/电脑控制：**不收**。  
- `computer.act` 不承诺任意应用一次成功；L0–L2 不允许表不得带它。  
- 小说/需要查证的行业报告走 L3，现网流水线合法。  
- 「打开 Word 写周报」保持 R2，不改成 R4。

### 1.5 性能 / 速度（排在听指令与准确之后）

主因是**轮次**：推理默认开 + 报告 6 次 nudge + 工具环 24→48，不是 React，也不是 `bufferReply`（电脑/播歌/语音带工具）。无损 Token 层去的是空白，不减 Stream 次数。  
收益顺序：未挂载或仅 1 人则零会商步 → 写稿少工具 schema → L2 去掉调研环 → 关长推理。挂了 ≥2 人就开会，用档位封顶换速度，不用「再 @ 一次」换速度。  
禁止：静默剥工具、默认换小模型、为 200ms 撤 Mermaid 隔离。

## 2. 目标与非目标

### 2.1 目标

- 闲聊 / 润色 / 写一段 / 写一篇不要文件：一轮主模型；默认关长推理；不搜。  
- **会话挂载 ≥2（或本轮芯片并入后 ≥2）：先开会，再按原任务交付。不必再 @。**  
- 「写周报」：`detectTaskRoute` **仍为 R4**。有材料则读后生成；无材料问缺，**零次** `web.search`。  
- 用户要查证 / 控电脑 / 改代码：能力不降。  
- 办公台卡片与交付文件同一归档。

### 2.2 非目标

- 第二套对话系统、自动切小模型、动 `token_ledger`、邮箱日历、VERSION/打包（除非另令）。  
- 把「写周报」改成技能空跑或跳过 LLM。  
- 「换一种方式」回放回声。  
- 语音 TTS 另轨。

## 3. 档位 + 会商叠加（实现合同）

档看**任务**。会商看**本会话已挂载的专家**。二者正交：L1 + 会话挂了两人 = 仍是 L1，先开会再一次成文。`@` / `[引用专家]` 只表示「把这些人挂进本会话」，与侧栏挂载同一名单，不是第二道开关。

### 3.1 类型（落在 `internal/app/chat_lane.go`）

```go
type ChatLane string

const (
    LaneL0    ChatLane = "L0"     // 寒暄
    LaneL1    ChatLane = "L1"     // 一次成文，不要办公文件
    LaneL2    ChatLane = "L2"     // 有材料出文件（含有要点的周报）
    LaneL2Ask ChatLane = "L2-ask" // 要出文件但本轮没有可用材料
    LaneL3    ChatLane = "L3"     // 用户明确要查证后再写
    LaneL4    ChatLane = "L4"     // 代理 / 桌面 / 代码
)

type LaneInput struct {
    Goal               string // 必须已经过 chatRoutingText，禁止用附件正文选题
    HasTurnMaterials   bool   // 见 §3.3
    Companion          bool
    OfficeTaskID       string
}

type CouncilOverlay struct {
    Run           bool
    ExpertIDs     []string
    MaxSteps      int  // 每位
    Tools         bool
    DisableReasoning bool
}

type LaneContract struct {
    Lane                       ChatLane
    Route                      TaskRoute // 写周报必须仍是 R4
    DisableReasoning           bool
    SkipOfficeResearchPipeline bool
    AllowWebSearch             bool
    AllowOfficeGen             bool
    MaxMainToolSteps           int
    ContinueNudges             bool
    Council                    CouncilOverlay
}
```

`classifyChatLane(in LaneInput) ChatLane` 纯函数。  
`buildLaneContract(lane, route TaskRoute, overlay CouncilOverlay) LaneContract` 纯函数。  
`applyLaneTools(defs, c LaneContract) []llmadapter.ToolDefinition` 只减不加（不得加回 `computer.act`）。

### 3.2 分档表（只看 Goal + 材料，不看专家）

路由文本 = `chatRoutingText`。`「继续」` 先换成 checkpoint 的 `turn.Goal` 再分档（现 `looksLikeResume`）。

| 档 | 进入 | 排除 |
|---|---|---|
| L0 | 现 `isShortIdleGreeting` 或 `autoToolProfile==minimal` 且无任务线索 | 帮我/写/生成/打开/搜索。寒暄即使已挂 ≥2 人**仍不开会**（没有可讨论的任务）；从 L1 起挂载 ≥2 即开会 |
| L1 | 写/润色/总结/扩写/翻译；**未**要求 Word/PPT/Excel/PDF/桌面文件；**未**要求上网查 | 「写周报/报告/做PPT」不是 L1 |
| L2 | 要出办公文件或「写周报/报告」，且 `HasTurnMaterials` | 用户写了「去网上查/检索/调研/找资料」→ L3 |
| L2-ask | 同上但无材料 | 用户说「你先搜」→ L3 |
| L3 | 明确查证/检索/调研，或 L2-ask 之后用户说先搜 | — |
| L4 | 代码/终端/电脑控制/桌面打开/播控/浏览器/装技能，或现网 R2/R3 | 纯写稿 |

含糊「帮我做个任务」且无宾语：按 L2-ask 处理（只问一句），**不要**当 L4 做完。有明确宾语的任务仍连续做完。

用户显式覆盖（听指令，写在 `applyLaneOverrides`）：

- 「不要搜/离线/禁止联网」→ `lookupOptedOut` 已有；强制 `AllowWebSearch=false`，L3 降为 L2 或 L1。  
- 「深度思考/认真想」→ `DisableReasoning=false`，档不变。  
- 「先搜索/上网查」→ 升 L3（写稿/出文件）或给当前档打开搜索（查询类）。

### 3.3 本轮材料（禁止扫全年历史）

`HasTurnMaterials == true` 当且仅当任一成立：

1. 本轮 `contextRefs` 含 attachment，或 `OfficeTaskID` 任务目录里已有用户文件（不是空任务）；  
2. 用户消息在扣掉寒暄后，存在 ≥2 条要点（换行 `-`/`1.` 或至少两个 `；`/`。` 分隔的事实句），或粘贴正文 ≥ 80 字；  
3. 用户点名了工作区/桌面路径或「见附件/如上/以下材料」。

不把「周报」两个字当材料。不读附件正文来改档（正文只进信封给模型）。

### 3.4 各档运行时

| 档 | 推理 | 主循环工具 | 流水线 | 催促 | 主循环步数 |
|---|---|---|---|---|---|
| L0 | 关 | 现 minimal/R0 | 不开 | 否 | 1 |
| L1 | 关（用户要深度思考则开） | 无工具；有附件则仅 `workspace.read`（办公台用 `office.inspect`） | 不开；mermaid 不强制 | 否 | 1 |
| L2 | 关 | R4 **减去** search/fetch；保留 gens + workspace + `user.ask` | **跳过调研**；注入 `referenceOfficeInstruction` | 只催「未读材料 / 未调用生成」 | 8 |
| L2-ask | 关 | **仅 `user.ask`**（MCP/技能/kb 若已在 defs 可保留只读检索类？**选定：不保留 web.search**；kb 可留） | 不开 | 否 | 1 |
| L3 | 开 | R4 含搜索 | 现报告/PPT 流水线可用 | 现网 | 24→48 |
| L4 | 开 | 现路由 | 现纪律 | 现网 | 24→48 |

Office Studio（`officeTaskContextID != ""`）：继续不 `startPpt/startDocx`。L2 打字必须与此对齐，禁止一条路离线、一条路逼搜。

`officeGenWorkflowClause`：L2/L2-ask **不得**注入「至少两轮 web.search」句；改注入离线条款。`selectWorkflowClauses` 对「写周报」勿因「查」字误加 `workflowResearchClause`（「查」太宽，本版在 L2 直接不选调研条款）。

### 3.5 会商叠加（听指令：挂载即入会）

**名单（同一份，封顶 8，不并 13 人目录）：**

```
roster = unique(ListSessionExpertIDs ∪ extractExpertRefIDs(本轮文本))
```

侧栏挂载与本轮 `@` / `[引用专家 name|id]` 都是写入这份名单的动作。芯片若尚未落入 `session.experts`，本轮仍并入 roster（即时挂载）。**不得用本轮芯片从已挂载名单里剔除任何人。** 退出会商的唯一产品动作是**卸载**。

项目管理、换阶段、换任务时，引擎**禁止**静默 `session.experts.set` 一批用户没点过的专家。过期挂载由用户卸，不要靠「再 @ 一次」过滤。

**开会条件（同时满足，且非语音）：**

1. `len(roster) ≥ 2`  
2. 本轮不是 L0 寒暄  
3. 不是 `skipExpertCouncil`（建文件夹 / 播歌 / 打开网站——操作任务不被会商绑架）

单挂载 1 人：保持现网人格注入，不开会（一个人不是理事会）。  
`len(roster) < 2` 且用户写了「请两位一起评」：主回复第一句「请先挂载至少两位专家」，`Council.Run=false`，不装已评。

`selectedTurnExpertIDs` **改口**：挂载 ≥2 时返回全部挂载（加本轮芯片并集），不再因为没有 `@` 返回 nil。  
**改测：** `TestSelectedTurnExpertIDsUsesMountedSubsetOnly` 现期望「两挂载无 @ → 空」**改为**「两挂载 → 两人」。`TestSelectedTurnExpertIDsTurnRefsBeatStalePMMounts` **改为**：芯片不得踢掉仍挂载的人；另测「引擎不得在用户未点名时静默改挂载」。

| 主档 | 每位步数 | 专家工具 | 专家推理 | 主席提示 |
|---|---|---|---|---|
| L0/L1/L2/L2-ask | 1 | 无 | 关 | 综合后**只完成用户原任务**。L1=对话里成文；L2=用已有材料生成；L2-ask=只列出缺什么。禁止要求 web.search / 禁止要求未点名的 *.gen |
| L3 | 2 | 现 specialist（可搜） | 开 | 现网结构 + 允许按用户要求调研 |
| L4 | 6（现网） | 现网 | 开 | 现网「综合后把交付做完」 |

并行仍 `councilParallelExperts=3`。共享总线旗标逻辑不改。  
会商仍走 `turnBudgetAdapter`。L1/L2 无工具 1 步，避免预算饿死主循环。  
测试改口：L1 + 会话挂载两人（无 @）→ `buildExpertCouncilConfig != nil`；**删除/不得再写**「挂载≠会商」「必须再 @」。

## 4. 周报 / 文章叙事

- 只发「写周报」：L2-ask，R4 标签，工具无 search、无 `docx.gen`，问本周完成/风险/下周或请贴笔记。  
- 「写周报」+ 要点/附件：L2，读材料 → `docx.gen`（点名 Word/桌面）或先对话交付（未点名文件时允许）。禁止补编未给的数字。  
- 「写一篇介绍光合作用，不要文件」：L1，一轮，默认不搜。  
- 「根据网上公开资料写行业报告」：L3。  
- 「打开 Word 写周报」：R2/L4，质量门仍拒空稿。  
- 「润色这段」且会话已挂两位（不必再 @）：会商 1 步无工具 → 主席出润色稿，不生成 Word。

## 5. 闭口顺序（防止自己卡自己）

`handleChatStart` / `runStream` 固定顺序：

1. `goal = chatRoutingText`；若 `looksLikeResume` 则 `goal = prev.Goal`。  
2. `route, allow = classifyTaskRoute`（写周报必须 R4）；flash 仅在 `RouteUnspecified` 时补，**不得**把 L2 改回带搜索。  
3. `lane = classifyChatLane`；`overrides`；`contract = buildLaneContract`。  
4. 会商配置按 §3.5；写入 `state.council` 与 `contract.Council`。  
5. `applyTaskRoute` 后立刻 `applyLaneTools`（档扣工具）。  
6. `DisableReasoning = contract.DisableReasoning || companion || 寒暄`。  
7. `runStream`：先会商（若 Run），再：  
   - `SkipOfficeResearchPipeline || OfficeTaskID != ""` → 不 `startPpt/startDocx` 调研支路；L2 注入 `referenceOfficeInstruction`。  
   - 否则保持现网 start。  
8. 主循环：`MaxMainToolSteps`；`ContinueNudges=false` 时 `pickTurnContinueKind` 对 ask/wait/docx nudge 返回空（桌面 incomplete 仍可在 L4）。  
9. `docxGenBlocked`：`SkipOfficeResearchPipeline` 时视为就绪（仍走空稿/无标题硬拒绝）。  
10. `looksLikeCurrentLookupTurn` 自动补搜：L2/L2-ask/`AllowWebSearch=false` 时不触发（写周报本就不命中行情表，双保险）。

回退：`LUNITIDE_CHAT_LANES=off` 只关分档与会商步数封顶，恢复旧流水线；不影响 Token 效率总闸。

## 6. 质量门（诊断仍有效的部分）

- 办公台：生成后左栏+中间预览；右侧产物可点；reference 不占中间；`officeTaskId` 点卡片不打开 Workspace。已有测试必须绿。  
- 400：交互提示保留且为正文首段；无人值守不得剥工具装完成；`STREAM_INCOMPLETE` 不放工具。  
- 上下文：失败可见；日志记 durable / explicit / checkpoint / fallback。不新造 `capability.readiness.get`。  
- Mermaid：未闭合不渲染；隔离 worker。  
- S-01/S-02、0.4.75 可见失败：不重开。覆盖率/单次日志数字不进验收。

## 7. 文件落点（按此改，勿扩 scope）

| 文件 | 改动 |
|---|---|
| **新建** `chat_lane.go` + `chat_lane_test.go` | `classifyChatLane` / `hasTurnMaterials` / `councilInvited` / `buildLaneContract` / `applyLaneTools` / `applyLaneOverrides` |
| `chat.go` | start 里算 contract；resume 用旧 Goal；邀请不足两人写可见句 |
| `chat_run_stream.go` | 用 contract 短路 start 流水线、补搜、步数、nudge；**不要**再把新流水线塞进函数中部 |
| `chat_docx_workflow.go` / `chat_ppt_workflow.go` | `docxGenBlocked`/`pptGenBlocked` 尊重 Skip；L2 注入离线条款 |
| `chat_workflows.go` | L2/L2-ask 不选调研条款；L2-ask 覆盖纪律 |
| `chat_expert_council.go` | `selectedTurnExpertIDs` 改为挂载∪本轮芯片；overlay 步数/工具/主席文案 |
| `chat_expert_council_test.go` | 两挂载无 @ 必须开会；芯片不得踢掉仍挂载的人 |
| `chat_continue.go` | `ContinueNudges=false` 短路（经 contract 传入 `pickTurnContinueKind` 或外层） |
| `task_route.go` | **尽量不改** R4 定义；收缩放 `applyLaneTools` |
| 办公台已有测试 | 只回归，不重写 |

`runStream` 已很长：contract 在 lease 回调开头读一次，后续只读布尔。

## 8. 阶段

**P0（可合并一个实现计划，按测试红灯顺序）：**  
分档纯函数 → 工具收缩（周报 R4 且无 search）→ L2 跳过调研 / L2-ask 只问 → 会商叠加与主席分档文案 → 办公台回归 + 400 回归。

**P1：** 上下文路径日志；panic 栈；审 Mermaid WIP（隔离硬留）。  
**P2：** 只读档位字；「深度思考/先搜索」芯片（可复用执行模式旁，不新设置页）。  
**延后：** 语音、拆 `runStream` 文件、自动小模型。

## 9. 验收（先写失败测试）

| ID | 给定 | 期望 |
|---|---|---|
| T01 | 「你好」 | L0，关推理，不会商，不催促 |
| T02 | 「把这段话润色得更顺：…」（无搜无文件） | L1，主循环 1 次 Stream，`web.search` 0 |
| T03 | 「写周报」无材料 | `detectTaskRoute==R4`；lane L2-ask；search 0；`docx.gen` 不在 defs；回复在问材料 |
| T04 | 「写周报」+ 三条要点 | L2；无调研拦截；允许 `docx.gen`；不得出现用户未给的量化业绩 |
| T05 | 「写一篇文章介绍光合作用」 | L1；无 `computer.act`；默认不 search |
| T06 | 「根据网上公开资料写一份行业报告」 | L3；允许流水线与搜索 |
| T07–T08 | 办公台 `officeTaskId` / 仅 reference | 与现网办公接线测试相同 |
| T09–T10 | 工具 400 交互 / 无人值守 | 提示可见；无人值守不 completed |
| **T11** | 会话挂载两人、用户只发「润色这段」、无 @、无邀请句 | `selectedTurnExpertIDs` 为这两人；**会商 Run=true** |
| T12 | L1 文本 + `[引用专家 A\|id][引用专家 B\|id]`（即使 session 尚未写入） | 会商 Run=true（芯片=即时挂载）；每位 ≤1 Complete；专家 defs 无 search/gen；主席产出润色/正文，**不**调用 `docx.gen` |
| T13 | 未闭合 mermaid | 不启动渲染 |
| T14 | 挂载 A、B，本轮只 @ A | 会商仍含 A **和** B（芯片不剔除已挂载） |
| T15 | 「请两位一起评」+ 挂载 0 + 无芯片 | 不会商；可见「请先挂载至少两位专家」 |
| T16 | L2 + `DisableReasoning=true` | 仍可 `docx.gen`（不被 `startDocx` 的推理闸误伤；空稿仍拒） |
| T17 | 「写周报」且 defs 曾含 search | `applyLaneTools` 后无 search；`looksLikeCurrentLookupTurn` 自动补搜不触发 |
| T18 | 用户发「继续」且上轮 Goal=有要点写周报 | 按 L2 而非 L0 |
| T19 | L1 + 会话挂载八人（无 @） | 最多 8 次专家 Complete、无工具；其后主循环仍能 Stream（预算未耗尽） |
| T21 | 「你好」+ 会话挂载两人 | L0，**不会商** |
| T20 | 「写周报，不要联网」+ 无材料 | 保持 L2-ask，search 0 |

禁止：节省 x%、无来源周报数字、空成功占位、「供应商已通过」。

## 10. 风险

- 依赖「写周报自动搜网凑篇」：视为错误能力，用环境闸回退。  
- L1 关推理变浅：用户说深度思考即开。  
- 材料探测误报：只认本轮，不扫历史。  
- 办公 WIP 与分档：先合已测接线，再合 lane（两个 PR 亦可）。

## 11. 自检

- 无 TBD。  
- 挂载 ≥2 即开会与「寒暄不开会、操作任务 skipCouncil」不矛盾：挂载是名单，L0/播歌仍不必开会。  
- `@` 与侧栏挂载同一名单，实现时芯片并入 roster，不用芯片做减法。  
- R4 标签与 L2 扣搜索不矛盾：标签防扩面，档位防假调研。  
- 推理开关与流水线解耦，避免 FR-502 卡壳。  
- 与 Token 效率 PRD、Office Studio、09-08 残流结论不冲突。  
- P0 一份实现计划可消化。  
- §12 月伴打开即停、听写不吞字、播控、余票 fail-closed、拒收幻觉听写、口头撤了与分档正交，不新开对话引擎。

## 12. 月伴语音：听写、打开即停、播控、空转（2026-09-11 真机）

三种听写（火山 seed-asr / 本机 sherpa / 系统 Web Speech）共用 `pickTranscriptRevision` 与 `looksIncompleteUtterance`。打字分档（L0–L4）不改变本节；月伴仍关长推理。

同一下午的三组真机要一起看：**余票刮网卡在执行中**、**图1 垃圾听写 + STALL**、**图2 口头撤了仍在接**。前一轮没停干净，后两轮就会叠在「正在接 / 没有及时回应」上。

### 12.1 现场两问

**问 1：第一次是不是断句所以打不开文档、还报错？**

是。不是桌面上没有那份文件。

用户要的是「打开桌面上的《日常操作功能增补》一类文档」。听写若在「打开」或后半文件名之前提交，引擎收到的是残句，例如「桌面的日常操作功能，增补文档」。

| 环节 | 现码 | 后果 |
|---|---|---|
| 不完整句判定 | `looksIncompleteUtterance` 已把「桌面上的…文档」当未说完 | 本应加长静音再交 |
| 硬天花板 | `shouldForceCommitUtterance`：文字不再涨且 ≥ `INCOMPLETE_HARD_MS`（4.2s）仍会交未完成句 | 说话稍停就被当成一轮 |
| 收尾短尾巴 | `pickTranscriptRevision` 仅当尾巴 < 整句 60% 才保住原文 | 「成功，再点击播放一下」会盖掉「播放没有成功，再点击播放一下」 |
| 打开目标 | `desktopOpenTargetFromGoal` **必须**句首有「打开/启动/运行」 | 残句没有「打开」→ 主机不注入 `desktop.open` |
| 模型误读 | 「增补文档」像写作 | 去 `workspace.list` `C:\Users\Administrator\Desktop`（进程身份不是你的桌面）→ 失败；再 `command.run`/`workspace.search` → 再失败 → 「无法执行」 |

所以第一次报错是 **听写截断改变了意图**（打开 → 去写/去搜工作区），再加上 **用错桌面路径**，不是文件不存在。

**问 2：第二次打开了，为什么还继续执行好几步才停？**

第二次轨迹是 `workspace.read`（读到 `C:\Users\mujun\Desktop` 上的稿），**不是** `desktop.open`。`companionGoalIsOpenOnly` 只在「打开成功且最后一工具是 desktop.open」时停环。读文件成功后纪律变成「连续做完」，模型开始「按日常操作功能补成一份完整文档」，再撞上 `STREAM_INCOMPLETE`（「模型返回格式不完整」）。

用户指令是打开已有文件。打开窗口后必须停，一句话报文件名。禁止读完再写、禁止 command.run 猜路径。

### 12.2 听写合同（三模式同一套）

1. `pickTranscriptRevision`：incoming 是已听原文的前缀或后缀（去标点后）时，**必须保留更长的已听原文**。三种听写的 commit/final 都走此函数。真正换了一句短指令（「打开汽水」→「暂停」）才替换。  
2. `在点击` → `再点击`；`点击放一下` → `点击播放一下`。写在 `cleanUserTranscript`，三模式提交后都经过它。  
3. 「打开桌面上的…文档/文件」在 `looksIncompleteUtterance==true` 时，**禁止**因 `INCOMPLETE_HARD_MS` 单独开轮。必须实际静音达到不完整句窗口，或用户点停。  
4. 残句没有打开动词、也拼不出完整文件名：口头问「要打开桌面上哪一份？」然后停。禁止对残句 `workspace.list` / `command.run`。

### 12.3 打开桌面文件（写死）

判定「只要打开」：含打开/启动/把开，且没有「然后写/填写/播放/搜索/生成」等后续动作。文件名里的「增补文档」**不是**写作指令。

主机侧（对标已有 `companionAutoMediaPlayArgs`）：

- `desktopOpenTargetFromGoal` 能抽出名字 → 只注入一次 `desktop.open`，`name=用户原话里的文件名`。  
- 成功回执 `opened …`：**结束本轮**，一句话报已打开。`pickTurnContinueKind` 已有此门，但前提是最后工具必须是 `desktop.open`。  
- 本轮目标是「只要打开」时，**禁止** `workspace.list` / `workspace.search` / `workspace.read` / `workspace.write` / `command.run`。桌面文件不在会话工作区，也不在 Administrator 配置文件。  
- `desktop.open` 继续只用 `userDesktopDir()`（当前用户桌面），不得改成 `C:\Users\Administrator\Desktop`。

「打开并写/填」才走打开之后的第二步。用户没说写，读到正文也不得开写。

### 12.4 播控（写死）

现场：「播放没有成功，再点击播放一下。」工具两次 `media.play` 汽水都回 `started playing`，月伴仍说没确认。另：`companionExtractMusicQuery` 会把「没有成功再点击一下」当成歌名去搜。

| 用户说 | 工具 |
|---|---|
| 再点/再点击播放/没成功再播/再播一下 | `media.play` action=play，**query 空**，app=当前播放器 |
| 暂停 | action=pause |
| 下一首 / 切歌；在播歌语境下的「下一周」 | action=next |
| 上一首 | action=prev |
| 停止播放 / 别放了 | action=stop |
| 退出/关掉汽水（或当前播放器） | `desktop.quit` name=播放器，不是再搜歌 |

顺序：Windows 媒体会话 → 失败则前台媒体键。回执 `verified *` 或 `started playing` 且 L0 passed、非 uncertain → **停环**，禁止再 `computer.act` 补点。`unverifiedMediaPlay` 不得把这类回执当成未完成。

### 12.5 实现落点（语音专节）

| 文件 | 改动 |
|---|---|
| `web/.../transcriptRevision.ts` | 前缀/后缀保整句（三模式） |
| `web/.../companionText.ts` | 在点击 / 点击放一下 |
| `internal/app/companion_context.go` | `companionMediaCommand`；续播空 query；打开只注入 desktop.open |
| `internal/app/chat_continue.go` | `started playing`+passed L0 停环；只要打开且已 `opened` 停环 |
| `internal/app/chat_run_stream.go` | 只要打开：禁止 workspace/command 补洞 |
| `internal/app/chat_intent.go` | 「增补文档」文件名不挡 open-only |

### 12.6 验收（语音）

| ID | 给定 | 期望 |
|---|---|---|
| T22 | 已听「播放没有成功，再点击播放一下。」收尾变成「成功，再点击播放一下。」 | 提交仍是整句 |
| T23 | 「播放没有成功，再点击播放一下。」+ 当前汽水 | `media.play` play、query 空、app=汽水；不是搜「没有成功」 |
| T24 | `started playing in 汽水音乐` + L0 foreground passed | `unverifiedMediaPlay==false`，不再催促 |
| T25 | 「暂停」「下一首」 | action=pause / next，不搜歌 |
| T26 | 未说完「打开桌面上的日常操作功能」 | 不开轮、不 workspace.list |
| T27 | 「打开桌面上的日常操作功能增补文档」 | 仅一次 `desktop.open`；成功后无 workspace/command；一轮结束 |
| T28 | 只要打开的目标含「增补文档」 | `companionGoalIsOpenOnly==true` |
| T29 | 残句无「打开」 | 口头问文件名，零工具 |
| T30 | 打开成功后模型还想 workspace.read/写 | 主机拒绝或不再给这些工具 |

### 12.7 余票 / 高铁（执行中像死锁）

现场：听成「今天上海虹桥到合肥南高铁票有哪些/多少钱」。工具轨迹约 10 步：`web.search` ×2 → `browser.act`（`BROWSER_MCP_NOT_READY`）→ 一串 `web.fetch`（12306 404、高铁页 404、trip.com、DNS）。界面「执行中」；月伴说网上没用、要上 12306。

**不是 UI 死锁。** `web.fetch` 连接 10s + 等头 15s；环 24 可扩到 48；纪律是「继续直到完成」。12306 要登录、反爬，抓页报不了实时余票。Playwright 没就绪时 `browser.act` 是空转。

现有条款已写「没有接口则说明尚未接入」，但被三处顶掉：`looksLikeCurrentLookupTurn` 自动补 `web.search`；调研条款因「查/火车」再塞搜索；月伴提示「需要补充就继续查询」。

**写死：**

- 只问火车/高铁/机票/余票、又没说「打开 12306 / 网上查」：只许一次 `mcp.search`。没有专用接口 → 口头「尚未接入实时余票，不能报今天还有哪些票」，**立刻结束**。
- 禁止 `web.search` / `web.fetch` / `browser.act` / `desktop.browse` 刷 12306。主机 `guardCurrentTurnTool` 拒绝，不只靠提示词。
- 这种轮 **最多 2 步**，禁止 `extendToolLoopLimit` 扩到 24/48。
- 用户明确说打开某网站才打开页面，仍不得编造余票。
- 日志：`%LocalAppData%\Lunitide\logs\engine-*.log`（UTC）。卡死先看 `chat stream … tool start name=`，再看界面工具轨迹。旧日志只有 preturn/guidance/模型失败，对不上「执行中」。

本工作区已按此改主机（需重编重启才进 0.4.75）。验收仍以 T31–T32 为准。

### 12.8 图1：幻觉听写 + `COMPANION_REPLY_STALL`

现场（本机 sherpa · 说完再答）：

1. 字幕出现无意义串：`hellolo 哦嘀嘀嘀 ty ty ty of的的的一的在的一yely`（解码器在有能量、无稳定中文时的幻觉，不是用户原话）。
2. 同时提示「听到你的声音了，但识别没有出字，已重新连接，请再说一遍」——**和字幕有字矛盾**。聋识别恢复（`RECOGNIZER_DEAF_MS`：有能量、interim 空）会 `stop` 再开识别；flush/重连会把残留 token 当成定稿。
3. `shouldAcceptUserTranscript` 只挡回声/忙碌/空/人设标签，**不挡幻觉**。`beginUserTurn` 把垃圾当 Goal，进入 thinking。
4. 上一轮余票若还在刮网，或垃圾 Goal 让模型一直不吐首字：渲染进程在 thinking 且无新鲜助手字时，6s（connecting）/ 12s（streaming）触发 `COMPANION_REPLY_STALL`（「月汐没有及时回应，请再说一次」），`onCancel` 后回聆听。工具执行中 stall 定时器会让路（`companionToolsExecuting`），所以余票刮网不会走这条横幅，只会停在「执行中」；**垃圾开轮、引擎没真正出字**才会 STALL。

现码定时：`COMPANION_FIRST_TOKEN_CONNECTING_MS=6000`，`COMPANION_FIRST_TOKEN_STREAMING_MS=12000`。thinking 满 2s 无助手字还会出「正在接，还没开口」（图2同一条 hint）。

**写死：**

1. `looksLikeAsrHallucination`（三模式 commit 前）：无有效中文任务词，且出现重复拉丁碎片（`ty ty`）、`hellolo`/`hello`+无问候对象、连续「嘀」、或只有「的的的/一的在的」这类虚词串 → **不得** `beginUserTurn`。提示「没听清，请再说一遍」。不要为这种字重启识别。
2. `hello` / `嗨` 后接「月汐/在吗」仍是合法寒暄（已有 wake）。只有幻觉形态才拒。
3. interim 或定稿**已经有字**时，禁止再走「没出字 → 重新连接」。有字要么是有效句（提交），要么是幻觉（丢弃），不能又提示没出字。
4. `COMPANION_REPLY_STALL` 必须：`onCancel` 取消**引擎**当前 stream（含还在跑的工具）；清掉本轮幻觉用户字幕；回聆听。禁止只改前端状态、引擎继续 fetch。
5. 不把 stall 超时收到 3s（旧流利度合同：闪模型 TTFT 仍可能 5–8s）。先靠拒收幻觉减少误开轮，不靠更短自杀。
6. 真任务开轮后 2s 内应有可听垫字「嗯」或首 token；「正在接」只是等待说明，不能代替开口，也不能在已 STALL 后还挂着。

### 12.9 图2：口头撤了 + 「正在接，还没开口」

现场：用户说「我说我不说了你直接撤了吧」。舞台停在 thinking，字幕「正在接，还没开口」。本机 sherpa 合同是**说完再答、打断用按钮**，麦克风不是打断通道。

因果：图1 stall 或余票轮之后回到聆听；用户用口语取消；`beginUserTurn` 把整句当新 Goal；新 `chat.start` 等首 token；满 2s 出 connecting hint。若上一轮引擎没被 cancel 干净，新轮会堵在「正在接」直到再次 STALL。

「算了放首歌」已有听写光标测试，是**换任务**，不是纯取消。不得把所有「算了」都当打断。

**写死：**

| 用户说 | 主机 |
|---|---|
| 不说了 / 我说完了你撤了 / 你直接撤了 / 撤了吧 / 停下 / 别查了 / 取消 / 不用查了 / 算了不用了（无新动词） | `companionSpokenCancel=cancel`：与点「打断」相同，`cancelReply` + 取消引擎 stream。**零次**新 `chat.start`。口头「好，已停下」 |
| 算了放首歌 / 算了查天气 / 不要这个，改打开… | `cancel-and`：先取消上一轮，再对剩余指令开一轮 |
| 暂停 / 下一首 / 别放了 | 仍走 §12.4 播控，不是对话取消 |

本机路径在 thinking 时默认关麦。纯取消必须在**聆听**里认出口语（图2这种），或用户点打断。不得要求用户只会点 Tab。

同一会话只允许一条活着的 companion stream。新口令到来时：取消类 → 干掉旧的；非取消且旧的还在跑 → 不新开轮，口头「上一句还在做，要停下就说撤了或点打断」。

### 12.10 实现落点（本批追加）

| 文件 | 改动 |
|---|---|
| `internal/app/chat_task_result.go` | `inventoryLookupBlocksPublicWeb`；余票拒 scrape |
| `internal/app/chat_intent.go` | 余票不 `fallbackWebSearchArgs`；可补一次 `mcp.search` |
| `internal/app/chat_run_stream.go` | 余票环封顶 2；`tool start` 日志 |
| `internal/app/chat_companion_speech.go` / `chat_workflows.go` | 去掉「继续查询」与余票调研互殴 |
| `web/.../companionText.ts` | `looksLikeAsrHallucination`；`companionSpokenCancel` |
| `web/.../CompanionStage.tsx` | commit 前拒幻觉；取消口语走 `cancelReply`；stall 必须 cancel 引擎；有字不聋重连 |

| ID | 给定 | 期望 |
|---|---|---|
| T31 | 「今天上海到合肥高铁票有哪些」且无车票 MCP | 至多一次 `mcp.search`；0 次 `web.search`/`web.fetch`/`browser.act`；口头「尚未接入」；≤2 步结束 |
| T32 | 用户明确「打开12306查高铁」 | 允许打开页面；仍不得编造余票 |
| T33 | sherpa 定稿 `hellolo 哦嘀嘀嘀 ty ty ty of的的的一的在的一yely` | `looksLikeAsrHallucination==true`；不 `beginUserTurn`；提示没听清；无 `COMPANION_REPLY_STALL` |
| T34 | 已有 interim 字时能量仍在 | 不得走「识别没有出字 → 重新连接」 |
| T35 | thinking 且无首 token 满 stall | `onCancel` 取消引擎轮；清掉本轮垃圾用户字幕；回聆听；禁止引擎继续跑工具 |
| T36 | 「我说我不说了你直接撤了吧」 | `companionSpokenCancel==cancel`；`cancelReply`；0 次新 `chat.start`；口头「好，已停下」 |
| T37 | 「算了放首歌」 | 取消上一轮后按播歌执行（不是纯取消） |
| T38 | 上一轮工具还在跑时用户说撤了 | 先取消在跑的 stream，再停；不得再开一轮空 thinking |

---

请评审本文。说「按 v1.4 开发」后按 TDD 落地（语音测试已有红灯草稿：`transcriptRevision.test.ts`、续播与 `unverifiedMediaPlay`；余票 fail-closed 测试已在本工作区）。图1/图2 未改运行时前，不要当作已修好。未批准整份 v1.4 前，不要再改月伴状态机（余票主机合同除外，已按上一问落地）。
