# 意图路由与状态闸口重构 v2.0（能力默认全集 + 状态护栏）

> 文档版本：v2.0（可开发）
> 状态：Ready for Implementation Review
> 日期：2026-10-19
> 基线：`trae/docs/Design` @ `8b42162c` / 0.18.3（意图识别三修复已发布）
> 性质：产品 + 工程合同。按本文改造 `internal/app` 现有路由与工具装配链，不新开对话引擎。
> 前序：`PRD-chat-task-lanes-v1.md` v1.4（lane 体系原始合同）。本文是其架构级修订，非补丁。
> 冻结（继承 v1.4 全部冻结项）：`token_ledger`、普通聊天/项目分离、统一 Runtime、S-01/S-02、`STREAM_INCOMPLETE` 残流不放工具；不接邮箱/共享日历；`html.gen` 仅三模板；不编节省百分比。
> 冻结（产品红线，本版新增重申）：不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方；不改审批模式语义；不删任何 git tag；R2 桌面能力、R3 浏览器能力、L4 代码/电脑控制能力**不收**。

---

## 0. 相对 v1.4 的改口（必须先读）

v1.4 的性能节写明「禁止：静默剥工具、默认换小模型」，但实现落地后 lane 体系恰恰成为每轮静默剥工具的机制。本版把这条被违背的原则升格为架构第一原则，并重排全部责任边界。

| v1.4 合同 | v2.0 合同 | 原因 |
|---|---|---|
| lane 按意图预测剥工具表（L1 只读白名单、L2 剥搜索、R2/R3 route 白名单） | **任何意图预测都无剥工具权**。工具表 = profile 全集；剥除仅来自状态闸口与用户显式配置 | 意图预测天然不可靠（0.18.3 三事故全链）；误判代价只能是浪费预算，不能是丢失能力 |
| flash 分类器结论直接决定 route 白名单（可剥 web.search） | flash 仅输出**提示词引导 + 预算基线**；返回 NONE/低置信 → 按全能力处理 | 64-token 单次调用不该持有剥除权限（RC2：「查询啊」被误判 R2 剥掉搜索，4 分 26 秒零结果） |
| 护栏绑在档位上（判「这句话像写周报」→ L2 剥搜索） | 护栏绑在**任务状态**上（检测「无材料 + 请求生成」→ 闸口拦生成、放行追问） | v1.4 的事故全是模型行为问题（无材料硬搜假周报），状态检测可靠、意图预测不可靠 |
| 四层串联过滤器：profile → `applyTaskRoute` → `applyLaneTools` → `restoreFileLandingTools` | **一层能力面**（全集 + 显式剥除）+ **一层预算面**（档位只管步数/模型/催促）+ **闸口在运行时逐次判定** | 四层白名单互不知情，`applyLaneOverrides` 已堆 9 个救援 if，补丁驱动不可持续 |
| 6 档 lane（L0/L1/L2/L2-ask/L3/L4） | **3 档预算档**（Chat/Task/Agent），档位只改预算不改工具表 | L2/L2-ask 差异并入状态闸口；L3/L4 契约已相同；边界样本减半 |
| 一次性分类，错档整轮报废 | **分类-验证-升级闭环**：预算将耗尽且无实质产出 → 自动升档续跑 | 错误半径从「整轮」缩到「几步」 |
| 路由规则无回归集（每次事故靠 PRD 附录记录） | **意图路由评测矩阵**：动路由代码必跑全量三层用例集 | Codex/Claude Code 路由稳定靠 evals 持续锁定，不靠一次性设计正确 |

**优先级链（继承 v1.4，冲突时按此裁）：** 听指令 > 准确 > 稳定 > 能力 > 性能 > 速度。

---

## 1. 背景：三事故与深层根因

### 1.1 直接触发（0.18.3 已修但只到症状层）

| ID | 事故 | 0.18.3 的修复 | 残留问题 |
|---|---|---|---|
| RC1 | 裸公司名「北京支点互动科技有限公司」无动词 → 判 L1 闲聊，无 web.search、1 步预算，33 秒只回「找到可用的搜索工具了」 | `bareTopicLookup` 裸实体识别 + lane 救援接入 | 又一个关键词特判；下一个裸实体形态仍会漏 |
| RC2 | 催促句「查询啊」被 flash 误判 R2（桌面操作）→ route 白名单剥掉 web.search → 4 分 26 秒 / 4.1 万 token 全走被封 MCP 通道零结果 | `lookupFollowUpOverridesRoute` 压制 R2/R3 → R1 | 压制规则只覆盖 lookup 形态；flash 误判其他形态仍会剥工具 |
| RC3 | `capability.discover` 发现类调用成功 → 解除自动补搜梯子 → 明暗两条搜索通道同时断 | 梯子重写：只有真实 web.search 收据才解除 | 梯子是被「分类不可恢复」逼出来的运行时补丁 |

### 1.2 深层根因（本版要根治的四条）

1. **A/B 类混淆**。能力路由（判断意图 → 配工具）与行为护栏（模型在特定状态会做坏事 → 机制拦截）被塞进同一个「意图分类 → 剥工具」机制。护栏的触发依据本该是可靠的状态（有无材料、挂载人数、上一步动作），却绑定在不可靠的意图预测上。
2. **弱分类器持强权限**。关键词正则与 64-token flash 调用，两者都能剥掉全部搜索工具。
3. **四层串联过滤器失配**。`chat.go:1106-1160`：工具表依次过 `applyTaskRoute`（route 白名单）→ `applyLaneTools`（lane 白名单）→ `restoreFileLandingTools`（补回）；每层独立白名单、互不知情，`applyLaneOverrides`（`chat_lane.go:444-495`）9 个救援 if 是层间失配的止痛药。
4. **一次性分类不可恢复**。档位入口定死，错档在预算耗尽后才暴露，整轮报废。

### 1.3 对标结论（调研 2026-10，四家无一例外）

| 产品 | 入口意图分类？ | 剥/限能力的依据 | 错误半径 |
|---|---|---|---|
| Codex CLI | 无。单一 agent loop，模型自主发工具调用 | suggest/auto-edit/full-auto 审批档 + sandbox，**用户配置**；Auto-review 用第二 agent 事后审查 | 单次调用 |
| Claude Code | 无前置意图分类 | permission modes（用户切换）；Plan mode **机制性**收写权限（用户显式进入状态）；auto classifier 在**每次工具调用前**判审批 | 单次调用 |
| Roo Code | 无前置 | Code/Architect/Ask/Debug/Orchestrator 模式，`switch_mode` 是**给模型的工具**，可观测可纠正 | 单次调用 |
| Cline | 无前置 | Plan/Act 两态，用户切换 | 单次调用 |

共识：**没有任何主流产品用「意图预测」做前置剥工具**。档位普遍存在，但全部站在权限/审批维度（用户显式控制）或状态维度（Plan mode 进入状态后机制性收权），错误半径都是单次调用。

---

## 2. 目标与非目标

### 2.1 目标（全部为可观测行为）

- 输入任意裸实体名（公司/机构/术语/缩写）或任意催促短句，**当轮必出现真实搜索动作或明确说明**，不再出现「整轮无搜索通道」。
- flash 分类器无论判什么，工具表不再因此变少；误判的可见代价上限 = 多花几步预算，随后被升级闭环纠正。
- v1.4 记录的**每一起事故**（假周报、会商卡死、打开后继续写、余票死锁、续播去搜索）在新架构下有对应回归用例且不复发。
- 闲聊轮的响应速度与 token 成本不劣化：Chat 档仍默认关长推理、走快模型、低步数预算。
- 任何一次误分类可回放、可归因：路由评测矩阵全量跑通是合入前置条件。

### 2.2 非目标

- 不做第二套对话系统；不动 `chat.start` 以外的入口。
- 不做自动切小模型（多模型分级仍按档位配置，不按单轮动态切换）。
- 不动 `token_ledger`、审批模式语义、已连接 MCP 配置、会商挂载语义（v1.4 已定：挂载 ≥2 即开会）。
- 不在本版引入 `mode.switch` 模型自报工具（列入 M3 评估，非承诺）。
- 不追求路由准确率 100% 的承诺；追求**误判代价趋近于零 + 可回归验证**。

---

## 3. 核心架构：两个平面 + 一条闭环

### 3.1 第一原则

> **剥除工具的依据只能是「任务状态」或「用户显式动作」，永远不能是「意图预测」。**
> 意图预测（关键词、flash、未来任何分类器）只允许产生两种输出：提示词引导、预算基线。

### 3.2 平面划分

```
chat.start
  │
  ├─【意图层】classifyChatLane(v2) + flash 分类      ──输出──▶ 预算档 + 提示词引导
  │   （可错，错了只影响预算与引导词）                        （不再触碰工具表）
  │
  ├─【能力面】req.Tools = profile 全集                 ──不变──▶ 唯一剥除来源：
  │                                                          ① 用户显式配置（隐私/审批模式）
  │                                                          ② 状态闸口（运行时逐次判定）
  │
  ├─【预算面】LaneContract v2（Chat/Task/Agent 三档）
  │   只管：模型分级（DisableReasoning/快模型）、MaxMainToolSteps 基线、催促策略
  │
  └─【运行时】run loop
        ├─ 每次工具调用过 ToolGate（状态闸口，B 类护栏）
        └─ 轮内升级检测（A 类兜底闭环）
```

### 3.3 三平面职责对照

| 平面 | 回答的问题 | 输入 | 输出 | 允许的失败模式 |
|---|---|---|---|---|
| 意图层 | 这大概是什么类型的对话？ | goal 文本、会话上下文 | 预算档、系统提示引导词 | 档位偏差 → 多花/少花几步预算 |
| 能力面 | 本轮允许哪些工具？ | profile、用户显式配置、状态信号 | 工具表（默认全集） | 无（不依赖预测） |
| 运行时闸口 | **此刻这次调用**该放行吗？ | 任务状态信号 | 放行 / 拦截+替代动作 | 误拦一次 → 有明确解除条件，用户可纠正 |

---

## 4. 数据模型与契约

### 4.1 档位枚举 v2（6 档 → 3 档）

```go
type ChatLane string
const (
    LaneChat  ChatLane = "chat"   // 闲聊 / 就地文字 / 寒暄
    LaneTask  ChatLane = "task"   // 单轮可完成的任务 / 交付物生成
    LaneAgent ChatLane = "agent"  // 多步代理轮（继续未完成工作 / 代码 / 电脑控制 / 媒体中心首步）
)
```

**旧 → 新映射（迁移唯一真源）：**

| v1.4 档 | v2 档 | 迁移说明 |
|---|---|---|
| L0（空输入/寒暄） | chat | 零步预算规则保留在 LaneChat 契约内（见 4.2），不再独立成档 |
| L1（闲聊/就地润色） | chat | 工具表不再剥；预算保持低档 |
| L2（有材料办公产物） | task | 剥搜索的职能移交「无材料生成闸」（FR-301） |
| L2-ask（模糊任务追问） | task | 独立档取消；「只许问不许做」行为移交状态闸（FR-302） |
| L3（完整任务） | task | 与 L2 合并（v1.4 时 L2/L3 契约已高度趋同） |
| L4（代理轮/继续/媒体中心） | agent | 语义不变 |

映射必须是**纯函数**：`migrateLaneV1(old ChatLane) ChatLane`，供会话恢复、journal 回放、灰度开关三处共用，禁止各自实现。

### 4.2 LaneContract v2 字段契约

```go
type LaneContract struct {
    Lane             ChatLane
    Route            TaskRoute        // 仅用于提示词引导与统计上报，不再驱动工具表
    DisableReasoning bool             // 模型分级：chat 档默认 true；用户说「深度思考」翻 false（继承 laneWantsDeepThink）
    FastModelPreferred bool           // 模型分级：chat 档 true，走 flash 角色绑定的快模型
    MaxMainToolSteps int              // 预算基线，见下表
    ContinueNudges   bool             // 催促策略
    Council          CouncilOverlay   // 会商叠加层（预算自管，继承 v1.4）
}
```

**删除的字段（及去向）：** `AllowWebSearch`、`AllowOfficeGen`、`KeepSpecialistTools`、`SkipOfficeResearchPipeline` —— 全部移交状态闸口或删除（`SkipOfficeResearchPipeline` 由 FR-304 接管）。

**预算基线（唯一保留的档位差异）：**

| 档 | MaxMainToolSteps 基线 | DisableReasoning 默认 | 追加规则 |
|---|---|---|---|
| chat | 4（寒暄/空输入 0） | true | 无工具时也允许 0 步直接回复 |
| task | 12 | false | 无材料生成闸触发时首个 gen 调用不计入步数 |
| agent | 24 | false | 「继续未完成的工作」永远进本档（继承 looksLikeFinishOpenWork） |

预算在轮内可升不可降（FR-500），升档后 `MaxMainToolSteps` 取新档基线与当前值较大者。

### 4.3 状态信号（StateSignal）——闸口的唯一输入

状态信号是**可观测任务状态**的窄集合，每个信号有明确数据源与判定时机。禁止在信号定义里掺入文本意图猜测（如「这句话像不像写周报」），只允许描述状态。

| 信号 | 类型 | 数据源 | 判定时机 |
|---|---|---|---|
| `HasMaterials` | bool | `LaneInput.HasTurnMaterials \|\| hasTurnMaterials(goal,…)`（现有函数复用） | 轮开始 + 用户每次补充材料后 |
| `GenerationRequested` | bool | 本轮工具调用流中首次出现 `*.gen` / `office.*` 写类 / `desktop.open` 后的写类调用 | 每次工具调用前（闸口判定时） |
| `CouncilMountedGE2` | bool | 会话挂载专家数 ≥2（v1.4 语义） | 轮开始 |
| `FileJustOpened` | bool | `desktop.open` 在本轮成功返回且尚无后续写调用 | 每次工具调用后更新 |
| `MediaContinueAction` | bool | media session 动作（play 空 query / pause / next / stop） | media 工具调用参数解析 |
| `TicketQueryNoSource` | bool | 行情/余票类工具白名单外无可用 MCP 源（fail-closed 判定，复用现有 inventoryLookupBlocksPublicWeb 判据） | 轮开始 |
| `BudgetNearExhausted` | bool | `step >= MaxMainToolSteps - 1` 且 `SuccessfulTools == 0` | 每 step 后 |
| `LookupIntentLowConfidence` | bool | flash 返回 NONE / 未能定档（RouteUnspecified） | 轮开始 |

### 4.4 ToolGate 闸口规则表（B 类护栏唯一所在）

闸口在 run loop 每次工具调用前执行，规则表**穷举、有序、可插拔**：

| Gate ID | 触发（信号组合） | 动作 | 解除条件 |
|---|---|---|---|
| G-301 无材料生成闸 | `!HasMaterials && GenerationRequested` | 拦截 gen 类调用；向模型注入一次性系统消息：「材料未就绪，先用自然语言向用户确认缺什么，一次问清即停」；放行 `user.ask` | 用户提供材料（`HasMaterials` 翻 true）→ 自动放行并**不重置**当前步数 |
| G-302 模糊任务闸 | 预算档 task && 轮内前 2 步无任何工具调用 && 模型输出为问句 | 放行回复不催促（「问一次就停」，继承 v1.4 L2-ask 行为） | 本轮结束 |
| G-303 文件打开即停闸 | `FileJustOpened && 写类调用` | 拦截写调用；注入：「打开已完成即达成目标，不要读取后再生成一份」 | 用户明说「转换/另存/修改」→ 翻 false |
| G-304 调研流水线闸 | `!HasMaterials && GenerationRequested && 任务为报告类`（判定复用 `looksLikeReportTask`，这是**任务类型**判定不是意图剥权） | 拦截 `*.gen`、放行 `web.search` 与 `user.ask`（**与 v1.4 方向相反：材料不足先允许查证或追问，禁止直接生成**） | `HasMaterials` 翻 true |
| G-305 余票 fail-closed 闸 | `TicketQueryNoSource` | 工具表收窄为 `mcp.search`（最多 2 步），说明未接入即停（继承 v1.4 §12.7，此项 v1.4 已是状态实现，原样迁入规则表统一管理） | 本轮结束 |
| G-306 媒体续播闸 | `MediaContinueAction` | `action=play` 空 query 直接走 media session，禁止抽歌名去 `web.search`（继承 v1.4） | 本轮结束 |
| G-307 会商预算闸 | `CouncilMountedGE2` | 会商步数按预算档封顶（chat 1 步/task 2 步/agent 6 步，继承 v1.4），会商与主循环预算分离 | 本轮结束 |

**闸口通用契约：**
- 闸口**只拦调用，不剥工具表**——模型看得见工具，调用被拦时收到结构化原因，模型可换合法路径。
- 闸口拦截必须产生**用户可见的轮内事件**（复用现有工具拦截提示 UI），禁止静默。
- 每次拦截写入 journal：`gate_id / signal_snapshot / tool_name / step`，供评测与归因。
- 规则表按 Gate ID 升序评估，首个命中即执行（规则间已设计为互斥，冲突视为实现 bug）。

### 4.5 flash 分类器契约 v2（降权后）

```go
// 输出结构不变：route + allow map。变化的是消费方式。
// 允许的消费者：① 系统提示引导词（"用户想操作桌面应用"→ 注入桌面执行契约）
//               ② 预算基线偏移（R2/R3/R4 → task/agent 档起步）
// 禁止的消费者：applyTaskRoute 白名单（该函数在 M1 删除剥除逻辑）
// NONE / RouteUnspecified → 一律按 agent 档预算 + 全能力处理（低置信 = 保守 = 全能力）
```

flash 自身调用参数不变（64 token / 1 次尝试 / 禁思考，`capability_roles.go:308`）。`lookupFollowUpOverridesRoute`（0.18.3）在 M2 末期随白名单一起删除——它保护的场景已不存在。

---

## 5. 功能需求（FR）

### FR-100 能力面收敛（M1 核心，纯减法）

- **触发**：所有走工具环的非 companion 轮。
- **处理规则**：
  1. `applyTaskRoute` 撤销白名单剥除，仅保留 allow map 透传给提示词层；R2 桌面执行契约注入逻辑保留（`chat.go:1122-1127`）。
  2. `applyLaneTools` 保留函数壳但 L1/L2/L2-ask 白名单分支删除，全部档位返回输入全集（L0 零工具特例移交 LaneChat 零步预算——工具在、步数为 0，语义等价且实现更简）。
  3. `restoreFileLandingTools` 删除（其补回的对象不再被剥）。
  4. `applyLaneOverrides` 中与剥除相关的救援分支（含 0.18.3 的 R1 救援、`lookupOptedOut` 剥搜索）删除；保留档位修正类分支（capabilityWork → task 档等）。
- **结果**：工具表从轮开始到结束只受显式配置与闸口影响。
- **验收**：AC-101 起始工具表 == profile 全集，对任意 goal 断言；AC-102 `grep` 确认 `applyTaskRoute`/`applyLaneTools` 无 keep/continue 白名单残留；AC-103 全量闸门绿（见 §7 M1）。

### FR-200 flash 降权与置信度门控（M1）

- **处理规则**：见 4.5。另：flash 返回 R1-R4 时预算基线按映射表偏移（R1→task、R2/R3→agent、R4→task），返回 NONE → agent 档全能力。
- **异常**：flash 调用失败/超时（现状返回 `used=false`）→ 与 NONE 同路径。
- **验收**：AC-201 「查询啊」在 flash 误判 R2 的注入测试下，工具表仍含 web.search（0.18.3 RC2 回放）；AC-202 flash 不可用时路由不降级。

### FR-300 状态闸口（M2 核心，逐条迁移）

- **处理规则**：按 4.4 规则表实现 `ToolGate`；每个 Gate 独立单测 + journal 断言。
- **迁移合同（v1.4 事故 → Gate 回归）**：假周报→G-301/G-304；会商卡死→G-307；打开后继续写→G-303；余票死锁→G-305；续播去搜索→G-306。
- **验收**：AC-301~306 每条 Gate 的触发/动作/解除三态测试；AC-307 v1.4 §1 全部事故形态在新架构回放不复发。

### FR-400 预算档（M2）

- **处理规则**：按 4.1/4.2 实现 3 档与迁移映射；`migrateLaneV1` 单测覆盖 6 旧档全枚举。
- **兼容**：会话恢复/journal 回放读到旧档字符串 → 过 `migrateLaneV1`；旧 journal 不重写。
- **验收**：AC-401 迁移映射表逐行测试；AC-402 闲聊轮 token 消耗与 0.18.3 基线对比不劣化（评测矩阵跑 20 条闲聊用例取均值）。

### FR-500 升级闭环（M2）

- **触发**：`BudgetNearExhausted` 信号为 true。
- **处理规则**：预算档升一级（chat→task→agent），`MaxMainToolSteps` 补至新档基线，注入一次性系统消息：「预算已升级，继续完成用户目标」；同轮最多升 2 次（防振荡）。
- **与 0.18.3 梯子的关系**：`chat_run_loop.go:746-783` 的补搜梯子是本 FR 的特例实现，M2 重构为升级闭环的首个消费者（升级时自动执行一次补搜判定），不另起炉灶。
- **验收**：AC-501 RC1 场景回放：裸公司名误入 chat 档，第 4 步前完成升档并出现真实 web.search；AC-502 升档不重复计入已耗步骤。

### FR-600 意图路由评测矩阵（M2，合入前置条件）

- **用例集三层**：
  - **种子层**：0.18.3 的 5 个测试原样迁入（裸实体/催促压制/lane 救援/梯子契约/查询词提取）。
  - **事故层**：v1.4 §0/§1 全部事故逐条转用例（含「打开文件后说继续」「挂载两人只润色」「余票无源」等）。
  - **对抗层**：每条 Gate 与每个档位判定函数 ≥3 正例 + ≥3 反例 + ≥2 边界（边界必须包含 0.18.3 三 RC 的原始语句）。
- **运行契约**：`go test ./internal/app/ -run TestRouteEvalMatrix` 单命令全量；矩阵用例表驱动（`route_eval_matrix_test.go`，一行一用例：输入/期望档/期望 Gate 序列/期望首轮工具可见性）。
- **合入门禁**：任何触碰 `classifyChatLane`/`tryFlashClassify`/ToolGate/预算表的 PR，CI 必跑矩阵且零红。
- **验收**：AC-601 矩阵 ≥60 用例全绿；AC-602 注入测试：故意翻转一个档位判定，矩阵必须红。

### FR-700 总开关与灰度（M1 起全程）

- `LUNITIDE_CHAT_LANES=off` 语义更新：关闭 v2 全链（闸口+预算档+升级闭环），回到「profile 全集 + 无预算分级」最简模式（不是回 v1.4——v1.4 剥除代码已删）。
- 灰度：`LUNITIDE_STATE_GATES=off` 单独关闸口（预算档保留），用于隔离闸口引入的问题。
- **验收**：AC-701 两开关组合（on/off × on/off）四种形态启动冒烟通过。

---

## 6. 处理链路：改造前后

**现状（0.18.3，四层过滤器）：**
```
classifyTaskRouteApps(关键词) → [Unspecified? flash 补] → lookupFollowUpOverridesRoute(补丁)
  → applyTaskRoute(route 白名单剥) → applyLaneOverrides(9 个救援 if)
  → applyLaneTools(lane 白名单剥) → restoreFileLandingTools(补回)
  → run loop(补搜梯子兜底)
```

**v2（两平面 + 闭环）：**
```
classifyChatLane v2(3 档预算) + flash(引导词/预算偏移，NONE→agent 全能力)
  → req.Tools = profile 全集 (能力面，唯一剥除=显式配置)
  → LaneContract v2(预算面)
  → run loop:
       每 step: ToolGate(规则表逐次判定，拦调用不剥表，拦截可见+落 journal)
       每 step 后: BudgetNearExhausted? → 升级闭环(升档/补搜判定)
```

---

## 7. 里程碑与验收标准

每期独立可合入、可回滚；期与期之间无硬依赖阻塞（M2 依赖 M1 的能力面收敛完成）。

### M1 · 能力面收敛（纯减法，风险最低）

| 项 | 内容 |
|---|---|
| 范围 | FR-100、FR-200、FR-700 |
| 删除 | route/lane 白名单剥除、`restoreFileLandingTools`、`lookupFollowUpOverridesRoute`、剥除类救援分支 |
| 交付 | 上述代码删除 + 对应测试改写 + 两开关 |
| 验收命令 | `go vet ./... && go build ./... && go test ./internal/app/... && ./release/Check-Coverage.ps1 -Floor 51 -Timeout 35m` |
| 验收标准 | AC-101/102/103/201/202/701 全绿；0.18.3 五测试种子层全绿；`golangci-lint run ./...` 零 issue |
| 回滚 | 单 commit revert；`LUNITIDE_CHAT_LANES=off` 兜底 |

### M2 · 状态闸口 + 预算档 + 升级闭环 + 评测矩阵

| 项 | 内容 |
|---|---|
| 范围 | FR-300、FR-400、FR-500、FR-600 |
| 交付 | `ToolGate` 规则表 + `StateSignal` + 3 档 `LaneContract` v2 + `migrateLaneV1` + 升级闭环 + `route_eval_matrix_test.go` |
| 验收命令 | `go test ./internal/app/ -run TestRouteEvalMatrix -count=1` + M1 全量命令 |
| 验收标准 | AC-301~307、AC-401/402、AC-501/502、AC-601/602 全绿；矩阵 ≥60 用例；闲聊 token 基线不劣化 |
| 回滚 | `LUNITIDE_STATE_GATES=off` 关闸口；预算档可独立保留 |

### M3 · 评估项（非承诺，另立 PRD）

`mode.switch` 模型自报工具（Roo 路线）、路由准确率线上统计（journal 聚合）、flash prompt 调优迭代。

---

## 8. 风险与缓解

| 风险 | 等级 | 缓解 |
|---|---|---|
| 全能力后闲聊轮多花 token（模型见到全集 schema 会想调工具） | 中 | 预算档仍限步数（chat 4 步）；系统提示按档注入「本轮以直接回答为主」；AC-402 基线盯防 |
| 闸口拦截过多打断任务流畅性 | 中 | 每个 Gate 有解除条件；拦截必可见；`LUNITIDE_STATE_GATES=off` 独立开关；评测矩阵对抗层含「不该拦」反例 |
| G-304 与 0.18.3 已修行为的方向冲突（v1.4 禁搜 vs v2 允许查证） | 低 | v2 方向经设计评审确认：无材料时「查证后生成」与「追问后生成」都合法，只有「无材料直接生成」被拦；矩阵事故层用例双形态覆盖 |
| 旧会话恢复读到 v1 档字符串 | 低 | `migrateLaneV1` 纯函数 + 全枚举单测；journal 不重写 |
| 评测矩阵成为合入瓶颈 | 低 | 表驱动单文件，新增用例 = 加一行；CI 并行跑 |

---

## 9. 开放问题

| # | 问题 | 状态 |
|---|---|---|
| Q-1 | chat 档 4 步预算是否足够容纳「升级闭环触发前的浪费」？ | [TBD] M1 上线后按 journal 数据定，可在 M2 调基线 |
| Q-2 | G-301 的「材料」判定是否需要扩展到「用户贴了三条要点但没说附件」形态（v1.4 已知缺口）？ | [Assumption, TBD] M2 复用现有 `hasTurnMaterials`，扩展形态进 M3 |
| Q-3 | `FastModelPreferred` 是否需要新的角色绑定（现有 flash/main 之外）？ | [TBD] M2 评审时按模型配置现状定 |

---

*本文与 `PRD-chat-task-lanes-v1.md` v1.4 的关系：v1.4 的听指令缺口修复（§1.1/1.2）、会商语义、media 状态机条款全部有效继续执行；被本版显式替换的仅 §0 表中所列条目。冲突时以本文为准。*
