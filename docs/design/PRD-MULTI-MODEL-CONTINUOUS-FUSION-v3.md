# Lunitide Model Fit Layer PRD v3.0

- 状态：Proposed（实施基线，取代 v2 作为落地文档）
- 日期：2026-09-08
- 产品：Lunitide（Go Core Engine + Windows WebView2 Host + React/TypeScript Renderer + SQLite）
- 首批模型家族：DeepSeek、GLM、Kimi
- 证据等级：E2；已对照仓库实现与厂商公开协议。账号级 live probe 仍是 Slice 0 的硬门，不是本文可以替补的事实
- 上一版：`docs/design/PRD-MULTI-MODEL-CONTINUOUS-FUSION-v2.md`（平台级愿景，保留作参考，不作为排期合同）
- 更早：`docs/design/PRD-GLM-DEEPSEEK-NATIVE-HARNESS-v1.md`

---

## 0. 一页决策

### 0.1 产品定位

Lunitide 不是模型厂商，也不训练、微调或修改任何权重。

Lunitide 已经有产品侧的“世界”：Chat 工具循环、月伴、Office、Plan、子代理、审批、沙箱、上下文分层、token ledger。缺的不是第二套 Agent，而是一层 **Model Fit Layer**：把这个世界编译成 DeepSeek / GLM / Kimi 各自能正确理解、正确续轮、正确调用工具的请求，并在模型升级时用探测而不是猜测吸收新能力。

> 模型厂商负责让模型变强。Lunitide 负责不丢失协议能力、让模型读懂本产品的工具与完成定义、并在新版本到来时先隔离再验证再启用。

这才是用户感知的“和 GPT × Codex 一样深度融合”：不是复刻 Codex 内部的评测工厂与远程配置平台，而是让三家国内模型在 Lunitide 里把工具、权限、任务状态和交付真正跑起来，并且模型一升级，产品立刻能吃到升级，而不是被通用 OpenAI 兼容层削平。

### 0.2 用户的三个维度（本 PRD 的合同）

| 维度 | 用户原话压缩 | 产品侧唯一合法手段 | 明确不做 |
|---|---|---|---|
| D1 产品×模型共演 | 希望像 GPT 与 Codex 那样深度融合，把模型能力发挥到最强 | 协议保真 + 产品合同编译（Prompt / Tool / Context / Thinking / Recovery） | 不新建 Agent Runtime，不把“能调用 API”当成融合 |
| D2 跟着三家升级变强 | 不是模型商，但要精准适配；模型新版本来了，产品力跟着变强 | 版本钉死、能力四态、探针、能力→产品落点映射 | 不微调、不跟厂商要专线、不把宣传台词写成已验证能力 |
| D3 无训练权 | 没有微调/迭代模型的能力，只能升级适配层 | 全部增益来自 codec、profile、投影、探测、评测 | 不以 SFT/DPO/RL/联合训练为任何里程碑前置 |

三家是对等的**能力输入通道**，不是三个 Base URL。具体型号名只是访问日文档中的候选，代码不得硬编码旗舰名。

### 0.3 三层能力，三次可交付，而不是一次造平台

| 层 | 解决什么 | 用户可感知结果 | 本 PRD 切片 |
|---|---|---|---|
| L-Protocol 协议保真 | 思考链、工具续轮、cache、strict schema 不再被通用 DTO 丢掉 | DeepSeek/GLM 带工具的长任务不再 400、不再静默变笨 | Slice 1 |
| L-Product 产品编译 | 同一套 Lunitide 工具/权限/完成定义，按家族表达 | 模型更会用 workspace/office/plan，月伴不误开思考 | Slice 2 |
| L-Version 版本隔离 | 新 ID / 行为变化先隔离，验证后再用 | 厂商别名升级不会悄悄毁掉稳定会话 | Slice 3 |

v2 的 L0–L3 成熟度仍然成立。本 PRD **只把 L2 作为首发目标**；L3（自动发现、shadow、远程热更新）降为 P2，不阻塞前两层交付。

### 0.4 对 v2 的实施裁剪

保留：Protocol ≠ Family ≠ Dialect；declared / probed / qualified / blocked；同一模型“通用路径 vs 融合路径”对照；provider state 密封续传；远程配置不能扩权；不训练。

砍掉或后置（否则无法落地）：

1. 签名远程 Fusion Pack 分发、epoch 吊销、5 分钟热回滚 SLO
2. 把普通 Chat 并入 `agentrun` 作为本项目 P0
3. 120 题 Native Bench + QTSR +15pp 作为首发门禁
4. 1M 物理窗口作为 P0 宣传与晋级目标
5. changelog 抓取、shadow replay、学习型路由
6. 首发同时做齐 Kimi Responses / Anthropic Messages / partial
7. 新建 `internal/toolcatalog` 重写全部工具定义

这些不是错误方向，是**下一阶段平台工作**。把它们写进同一份 P0，会让真正能提升产品力的协议保真和产品编译永远排不到。

### 0.5 关键决策（10 条）

1. 不新增 stored `Protocol`。继续 `openai_compatible | anthropic | volc_speech`。家族与方言挂在 Model Fit Profile 上。
2. 新增一个包：`internal/modelfit/`。不新建 `modelfusion` / `modelprobe` / `modelcompiler` / `providerstate` / `toolcatalog` 五套空目录。
3. Profile 是仓库内内容寻址的声明文件 + 一到两张 SQLite 表，不是远程配置平台。
4. 模型只提出 Tool Intent；Policy / Approval / Sandbox / `toolruntime` 永远掌握执行权。
5. 未 probe 的能力 fail closed。厂商文档只进入 `declared`。
6. 未知 model ID 或行为指纹变化默认 `quarantined`，不得替换当前稳定 profile。
7. 现有 `ProviderDTO` / `ModelDTO` 冻结。融合信息走新 DTO，不破坏 bridge golden。
8. `DisableReasoning` 暂不删除，由 `ThinkingIntent` 解析后再写回，保证月伴 / Office 回归不过期。
9. 不把 Chat 主循环迁到 `agentrun`。本项目只修 `chat_run_stream` 续轮丢 state 这一条融合关键路径。
10. 单人实施顺序：DeepSeek → GLM → Kimi Chat Completions。共享 Profile / IR / 续传必须先于第三家复制。

---

## 1. 对 v2 的评审结论

### 1.1 可行性

v2 作为架构愿景**方向正确，作为 8–12 周实施合同不可行**。

对照仓库，v2 把至少 6 个独立项目捆成一个 P0：

| v2 工作包 | 与“跟着模型变强”的关系 | 本 PRD |
|---|---|---|
| 三家 dialect + reasoning 续传 | 直接决定融合是否存在 | P0，Slice 1 |
| Prompt / Tool 投影 / ThinkingIntent | 直接决定产品是否被模型用对 | P0，Slice 2 |
| revision 隔离 + 小探针 | 直接决定升级会不会把稳定用崩 | P0，Slice 3 |
| 签名热更新 / shadow / 120 题评测厂 | Codex 内部平台，不是桌面产品首发 | P2 |
| Chat ≡ agentrun | M4 遗留，吞掉整个排期 | 移出本 PRD |
| 1M 逐级解封 | `contextapp` 已有逻辑装配；物理 1M 不是融合前提 | P2，且不得先于质量 |

当前真实损耗已经足够证明“先做协议保真”：

- `llmadapter.Message` 没有 reasoning / provider state 字段。
- `chat_run_stream.go` 续轮只 `append(result.Message)`，思考内容留在 `Response.Reasoning`，下一轮不回传。
- `openai.go` 靠 400 后剥离 `thinking` / 降级 schema，这是静默变笨。
- 工具 schema 散落在 `chat_tool_defs.go`、`office_tools.go`、MCP/skill/plan/subagent 等多处；M4 `capability.list` 是另一套名字（`fs.read` vs `workspace.read`）。
- 前端用正则把 `deepseek-r1` / `deepseek-reasoner` 从工具里摘掉；这是启发式，不是能力合同。
- 持久历史里的 `role:tool` 没有 `tool_call_id`，智谱会整请求拒绝，现有代码只能折成 user note。

这些缺口不需要 Fusion Pack 平台也能修；修完之后，三家模型升级带来的工具/思考增强才会进入真实工作流。

### 1.2 先进性

先进的部分必须保留：

- **不训练、只编译**：这是 2026 年非模型商做 Agent 产品的正确路线。
- **Protocol / Family / Dialect 分离**：避免把智谱、DeepSeek、Kimi 写成三个 Base URL。
- **能力四态**：声明 ≠ 探测 ≠ 合格。这是“跟着模型变强”的唯一科学方法。
- **opaque provider state**：产品内核不解析厂商思考正文，只负责密封回传。
- **能力吸收阶梯**：新能力必须回答“落在 Chat / 月伴 / Office / Plan 的哪条路径”，而不是加一个“支持”标签。

v2 里“看起来先进、对 Lunitide 却是错位先进”的部分：

- 远程签名包、shadow replay、金丝雀比例，是云端模型网关的运营系统。Lunitide 是本机 SQLite + 用户自带 Key 的桌面工作台，没有 pack CDN，也没有多租户流量。
- 120 题 × 统计功效的 QTSR 是研究组织的评测厂。首发用 **24 题冻结集** 就能挡住高风险回退。
- 把“像 Codex”理解成“先造 Codex 的发布机器”，会错过 Codex 真正可抄的东西：模型看到的是一个连贯的工作世界，而且运行时不丢掉模型的私有续轮状态。

Lunitide 可积累、且不依赖权重的资产是：

1. 已经存在的产品运行时（工具、审批、Office、月伴、Plan）
2. 按家族校准的编译器与续传
3. 针对本产品任务的小评测集
4. 版本钉死与回滚

### 1.3 合理性

合理：不做模型商、三家对等、fail closed、不把 reasoning 当审计真相、不默认跨家族重放副作用工具。

不合理之处在于 **身份写错了**：v2 按“模型接入平台”写，Lunitide 的身份是“本地工作台”。融合层必须服务现有表面，而不是变成第四个产品。

因此本 PRD 的合理性标准改为：

- 每个 P0 都能指出改哪些现有文件、用户哪条路径变好
- 每个切片结束时可以单独使用，不必等平台齐套
- 模型升级的吸收路径可执行：发现 → 隔离 → 探针 → 人工启用，而不是“自动变得更好”这种无法验收的句子

---

## 2. 仓库事实基线（2026-09-08 核对）

### 2.1 已有、必须复用

- Provider / Model CRUD、DPAPI、origin fingerprint（`protocol + scheme + host + port`，不含 path）
- OpenAI-compatible + Anthropic 两个 transport；`factory.go` 按 `Protocol` 分支
- SSE、取消、usage / DeepSeek cache 字段读取（`usage.go`）
- Chat 工具循环：`internal/app/chat_run_stream.go`
- 上下文分层 + 压缩：`contextapp`、`compactionapp`；安全顶按 `contextWindow * 0.9375`
- 审批 / 沙箱：`toolruntime`
- M4 durable run：`internal/domain/agentrun`、`internal/agentrunapp`（**本项目不合并进 Chat**）
- Token 效率：稳定工具排序、JSON compact、cache 计量（`PRD-token-efficiency-v1.md`，已落地）
- `capabilitypack`：技能 / MCP / 插件安装编排。**不是模型融合，禁止复用此名**

### 2.2 融合缺口（P0 只打这些）

| 缺口 | 落点 | 产品后果 |
|---|---|---|
| 无 Family / Dialect | `provider.go` 只有 Protocol | 三家都走 `openai.go`，用 400 猜能力 |
| Message 不存 reasoning | `llmadapter/types.go` | DeepSeek/GLM thinking+tools 续轮非法或质量崩 |
| 续轮丢 state | `chat_run_stream.go:647` | 活动工具链无法按厂商合同回传 |
| 思考只有 bool | `Request.DisableReasoning` | 无法表达强制思考、effort、月伴必须关思考 |
| 400 后剥离 / 降 schema | `openai.go` | silent degradation |
| 工具三套合同 | chat defs / `capability.list` / `toolruntime` switch | 模型所见 ≠ 执行 |
| 历史 tool 无 linkage | `chat.go` `combineDurableProviderMessages` | 智谱整段 400，只能折成 user note |
| 无 revision / probe | 仅有 `Discover` + `provider.test` | 别名一变，稳定会话跟着变 |
| 前端启发式 | `web/src/provider/modelKind.ts` `NO_FUNCTION_CALLING_RE` | 用名字猜测工具能力 |

### 2.3 产品表面与思考策略（必须继续工作）

| 表面 | 今天 | Slice 2 之后 |
|---|---|---|
| 月伴 | `DisableReasoning=true`，丢弃 reasoning delta | `ThinkingIntent=off`；型号不支持 off 则改走已验证 fast profile，或不进月伴 |
| 短问候 | 同上 | 同上 |
| 普通 Chat / Plan / 子代理 / 自动化 | 思考开 | 由 profile + 任务规则决定 |
| Office docx/ppt 续写 | `DisableReasoning` 为 true 则停 | 读 resolved intent，不读裸 bool |
| Anthropic 适配器 | 忽略 `DisableReasoning` | 本 PRD 不改 Anthropic 主路径；Kimi 的 Messages 入口后置 |

---

## 3. 目标、非目标、用户故事

### 3.1 目标

G1. DeepSeek、GLM、Kimi 各有独立 dialect 编译，禁止在 `chat_run_stream.go` 里散落型号 `if`。

G2. 每次模型请求可追溯到：family、wire model、endpoint mode、profile digest、thinking intent、tool digest、context ceiling。

G3. 新模型 ID 或行为指纹变化进入 quarantine；稳定 profile 不被别名替换。

G4. 厂商新能力必须映射到已有产品路径：更强工具 → Chat/Office 工具投影；更强思考 → 复杂任务与修复轮；更长上下文 → `contextapp` 顶；cache → 稳定前缀；视觉 → 附件/Office（先 probe）。

G5. 模型升级后，在「同一模型、同一 24 题集、同一预算」下，融合路径相对通用路径至少不回退，并在协议续传类失败上显著下降。

G6. 全部 P0/P1 不依赖训练、微调、厂商对接人或 dedicated endpoint。

### 3.2 非目标（首发明确不做）

- 不训练、微调、修改权重
- 不承诺达到 Codex / Claude Code 的绝对能力
- 不新建第二套 Agent / 工具 / 权限 / Memory / Plan
- 不把 Chat 主循环迁入 `agentrun`
- 不做远程签名包、shadow 用户轨迹、学习型路由
- 不把 1M 写成“每轮都发送 1M”
- 不把网页 changelog 自动写成 effective capability
- 不允许远程 profile 新增工具、扩权、关审批或加载代码
- 不默认跨家族重放带外部副作用的 turn

### 3.3 用户故事

- 普通用户：接上 DeepSeek / 智谱 / Kimi 后，长任务能连续用工具做完，不必自己研究 thinking 参数。
- 月伴用户：语音仍快；不会因为某家强制思考而卡第一句。
- 项目用户：模型升级后，正在跑的工具链不会被静默换协议或丢思考状态。
- 高级用户：设置里能看到 declared / probed / qualified，以及“为什么不能用于月伴/视觉/长上下文”。
- 开发者：新型号首先是一份 profile JSON + 一组 fixture，而不是改 Chat 主循环。

---

## 4. 总体架构（薄 Fit Layer）

```text
Renderer / Settings
    → Bridge (现有 chat.* / provider.* + 新增 modelFit.*)
        → Chat / 月伴 / Office / Plan / 子代理 / 自动化   （现有入口，不换内核）
            → Model Fit Resolver
                → Turn IR（产品意图）
                → Profile（家族合同）
                → Compiler（Prompt 片段 + Tool 投影 + Thinking 映射 + Context 顶）
                → Dialect Codec（deepseek | glm | kimi | generic_openai）
                    → 现有 OpenAI / Anthropic transport
            → 归一化事件 + 密封 provider state
            → toolruntime / 审批 / 沙箱        （执行权不交给 codec）
```

### 4.1 四个对象，禁止混用

| 对象 | 含义 | 例子 |
|---|---|---|
| Protocol | 传输与鉴权形状 | `openai_compatible` |
| ProviderFamily | 厂商归属 | `deepseek` `glm` `kimi` `generic` |
| EndpointMode | 同 origin 下的 API 形状 | `chat_completions`（P0 只做这个） |
| Dialect | 该家族在该模式下的语义 | thinking 字段、reasoning 回传、schema 子集 |

Kimi 日后的 Responses / Anthropic Messages 是新的 EndpointMode，不是新 Protocol 枚举，也不是 P0。

### 4.2 代码落点（只准这一个新包）

```text
internal/modelfit/
    family.go          家族枚举、从 base URL 候选、必须用户确认
    profile.go         ModelFit Profile 的 schema / digest
    registry.go        按 provider+model+channel 解析
    thinking.go        ThinkingIntent 与 DisableReasoning 兼容
    ir.go              TurnIR、NormalizedEvent
    state.go           ProviderStateEnvelope 密封与校验
    manifest.go        每次请求的 request manifest
    project.go         对现有 []ToolDefinition 做家族投影
    prompt.go          家族校准片段（不能放宽权限）
    probe.go           无副作用 fixture 探针
    ceiling.go         EffectiveContextCeiling 唯一入口
    dialect/
        codec.go       Compile / Decode 接口
        deepseek.go
        glm.go
        kimi.go
        generic.go     现有 openai.go 行为的显式基线
```

现有 `internal/llmadapter/openai.go` 抽成 **transport**（HTTP、SSE、鉴权、重试）。方言字段编译从 `openai.go` 的 400 回退里搬出来。禁止复制三份 `runStream`。

---

## 5. ModelFit Profile（取代 v2 Fusion Pack 平台）

### 5.1 首发形态

Profile 是不可变 JSON，检入：

```text
internal/modelfit/profiles/
    deepseek.chat.v1.json
    glm.chat.v1.json
    kimi.chat.v1.json
    generic.openai.v1.json
```

digest = canonical JSON SHA-256。运行时按 digest 引用，禁止改 stable 文件原地打补丁；要改就新增 `v2` 文件并改 registry 指针。

首发 **不** 做：远程拉取、签名算法协商、pack_epoch、吊销清单、5 分钟热更新。客户端发版即可带上新 profile。等三家 codec 稳定后，P2 再评估“仅声明字段的本地导入”。

### 5.2 最小 schema

```go
type CapabilityState string // declared | probed | qualified | blocked

type ThinkingIntent string // auto | off | fast | balanced | deep

type Profile struct {
    ID               string
    SchemaVersion    int // 必须 = 1
    Family           string
    WireModelPattern string // 匹配规则，不是单一旗舰名
    EndpointMode     string
    DialectVersion   string
    Thinking         ThinkingMapping
    ToolProjection   ToolProjectionSpec
    ContextCeiling   ContextCeilingSpec
    Continuation     ContinuationSpec
    Cache            CacheSpec
    Capabilities     CapabilityManifest
    Channel          string // bundled | beta | quarantined
    Digest           string
}

type CapabilityManifest struct {
    Tools              CapabilityState
    ThinkingOff        CapabilityState
    ThinkingEffort     CapabilityState
    ReasoningReplay    CapabilityState
    StrictSchema       CapabilityState
    CacheUsage         CapabilityState
    Vision             CapabilityState
    MaxContextDeclared int64
    MaxOutputDeclared  int64
}
```

每个能力字段独立记录来源与状态。禁止用一个 `supportsTools=true` 代表所有工具场景。

### 5.3 家族识别

不得静默猜错：

1. 用用户已保存的 base URL 给出候选（`api.deepseek.com` / `open.bigmodel.cn` / `api.moonshot.cn` 等已知公开 origin）
2. 设置页让用户确认 Family
3. 未确认则走 `generic.openai`，行为与今天完全一致
4. Profile **不得** 改写 provider base URL；origin 变化仍走现有凭据重绑

### 5.4 热更新边界（即使日后做，也必须遵守）

| 变更 | 首发交付 | 日后若做本地导入 |
|---|---|---|
| 窗口、限额、thinking 映射、工具投影规则 | 发版内置 JSON | 可导入，须 schema 校验 |
| 新 SSE 字段 / 新续传语义 | 必须改 codec 代码发版 | 禁止只靠 JSON 启用 |
| 新工具、扩权、关审批 | 必须代码发版 + 安全审查 | 导入文件不能做 |
| 可执行脚本 | 禁止 | 禁止 |

---

## 6. L-Protocol：协议保真

### 6.1 Turn IR

产品意图先写成中立 IR，再由 profile 编译成 wire：

```go
type TurnIR struct {
    Family        string
    WireModel     string
    Messages      []MessageIR
    Tools         []llmadapter.ToolDefinition // 已是本次要暴露的目录
    Thinking      ThinkingIntent
    ProviderState *ProviderStateEnvelope
    MaxTokens     int
    ContextCap    int64
}
```

`balanced` 在 A 模型可能是默认思考，在 B 模型可能是 `high`。映射只来自已解析的 Profile，不写死在 Chat。

兼容：`ThinkingIntent=off` 写回 `Request.DisableReasoning=true`，直到月伴 / Office / 短问候测试全部改为读 intent。

### 6.2 Provider State Envelope（P0 核心）

这是三家“深度融合”里唯一不可省略的协议资产。

必须保存并在活动工具链回传的，是厂商要求的续轮字节，而不是给用户看的思考作文。

```go
type ProviderStateEnvelope struct {
    ID                 string
    Family             string
    EndpointMode       string
    WireModel          string
    Kind               string // reasoning | partial | response_id
    ConversationID     string
    TurnID             string
    Sequence           int
    CredentialOriginFP string
    Ciphertext         []byte // DPAPI 数据密钥 + AEAD
    Digest             string
    ExpiresAt          *time.Time
}
```

硬规则：

1. 只为协议续传服务；UI 继续用现有 thinking sidecar，不展示完整私有链
2. 不作为审批理由或审计真相
3. 活动工具链必须回传 `content + provider state + tool_calls`（按该家族合同）
4. 缺字节、解密失败、跨会话、跨账号、跨 origin、跨 family，一律在发请求前失败，禁止降级成“不带 reasoning 再试一次”
5. 无副作用且 manifest 完整时，允许重放该 model step；否则终止 turn，生成 handoff，而不是伪造成功
6. 用户删会话时销毁包裹密钥（crypto-erasure）

持久化最小接缝：

- **进程内续轮**（P0 必须）：`req.Messages` 带上可编译回 wire 的 state。这是 DeepSeek/GLM 400 的直接修复。
- **崩溃恢复**（P0 应该）：turn checkpoint（`.turns/<session>.json`）增加 `provider_state_envelope_id`；没有它不准继续活动工具链。
- **跨会话 durable message 表**（P1）：现有 message store 仍是 role+text。不要为了融合把整张消息表推倒。历史孤儿 tool 行继续走 fold，直到 P1 补 linkage。

### 6.3 禁止 400 静默降级

`openai.go` 现有四条 400 重试：剥 `enable_thinking`、剥 `thinking`、换图片编码、`sanitizeToolSchema`。

本 PRD 之后：

| 原行为 | 新行为 |
|---|---|
| 先发再猜 | Profile 编译前校验；不支持的字段不发送 |
| 400 后剥 thinking | 记为 capability drift，该次请求失败并打点；不偷偷关上思考再当成功 |
| 400 后降 schema | 只允许对已标记 `lossy` 的投影字段；破坏性工具必须本地 canonical 校验 100% 挡住 |
| 图片编码重试 | 保留（这是编码兼容，不是能力降级） |

通用路径 `generic.openai` 可以暂时保留旧 400 行为，作为对照基线。融合路径禁止走这条“变笨成功”。

### 6.4 归一化事件

Codec 只准向 Chat 循环输出：

```text
reasoning.delta   // 可丢弃展示；state 仍按合同持久化
content.delta
tool_call.delta / completed
usage.updated
provider_state.updated
message.completed / failed
```

Run / Policy 不读厂商原始 JSON 做权限判断。

---

## 7. 三家家族合同

以下全部是 `declared`。任何“必须实现”都要在 Slice 0 对应 fixture 成功后才写进 codec。探测失败标 `blocked`，禁止为了符合文档而伪造支持。

### 7.1 DeepSeek — Slice 1 第一家

公开重点（访问日 2026-09-08）：`reasoning_content`；thinking + tools 时后续请求需回传先前 reasoning；effort 与 endpoint 有映射差异；strict schema 有子集；cache 通过 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` 报告。

P0 必须：

- `dialect/deepseek.go`
- 按 endpoint 编译 thinking / no-thinking；禁止对 thinking 模式乱发无效 temperature
- 活动工具链 reasoning 字节完整回传
- 稳定前缀布局（已有 token-efficiency 工具排序，DeepSeek 吃这条）
- strict/lossy 由 Tool 投影显式声明，不靠 400
- 前端 `NO_FUNCTION_CALLING_RE` 改为读 profile：只有 `Tools=blocked` 的型号才摘工具

产品角色（须评测，不写死）：强推理型号承担复杂规划/编码/多工具；flash 承担月伴/检索/低风险读取。

### 7.2 GLM — Slice 1 第二家

公开重点：thinking 开关因型号而异；interleaved / preserved thinking 要回传 reasoning；部分型号强制 thinking；窗口不能用家族常量。

P0 必须：

- `dialect/glm.go`
- `thinking.type` / effort 按型号 probe 后映射
- 流式 tool 参数按 index 拼接
- 历史孤儿 tool 行不得再以 `role:tool` 发给智谱（现有 fold 保留，并补测试）
- 强制 thinking 型号：未过月伴 TTFT 门不得进入月伴

产品角色：长程工程、Office、中文结构化任务——以 24 题集为准，不以宣传为准。

### 7.3 Kimi — Slice 2 末 / Slice 3 初

公开重点：Chat Completions、Responses、Anthropic Messages 三入口；专属 thinking；message-level `partial`；默认 strict + MFJS；官方 token estimate。

P0 只做：

- `dialect/kimi.go` 的 **Chat Completions**
- 独立 Tool 投影（不得复用 DeepSeek schema 子集）
- token estimate 失败则退回现有本地估算并标非精确

P1 才做：Responses / partial / Messages 入口、视觉/视频。未 probe 前不得在 UI 宣传。

### 7.4 统一策略

| 维度 | 统一做法 |
|---|---|
| 传输 | 复用现有 OpenAI/Anthropic transport |
| reasoning / partial | Envelope，不进产品决策 |
| schema | 每家投影 + 本地 canonical 校验 |
| cache | 稳定前缀 + 只展示厂商报告的命中 |
| 长上下文 | 继续 `contextWindow * 0.9375`；无 qualified 更高顶则不准抬 |
| 多模态 | 能力门，默认关 |

---

## 8. L-Product：产品编译（Codex 可抄的那一半）

GPT × Codex 强，不是因为有远程 Pack，而是因为模型看见的世界是自洽的：工具、文件、权限、完成定义一致，而且思考状态还在循环里。

Lunitide 对等的工作是让 DeepSeek / GLM / Kimi 看见 **同一个 Lunitide 世界**，只用家族能懂的方言说出来。

### 8.1 Prompt 片段

不把整份系统提示按模型 ID 分叉。只允许增加 **家族校准片段**：

```text
identity / 产品能力 / 权限与审批     ← 三家共用，禁止放宽
inspect → plan → act → verify → deliver
完成定义
错误与恢复
引用与证据
family calibration fragment         ← 只能改变表达和策略偏好
```

要求：

- 片段变更必须跑注入与工具误用回归
- 系统前缀保持稳定，动态上下文放后面（服务 DeepSeek/GLM cache）
- 禁止用超长提示掩盖 runtime 缺陷

落点：从 `chat.go` 里现有系统提示装配函数抽出 `modelfit.PromptFragment(family)`，不要复制三套 Chat。

### 8.2 工具投影，不重写目录

P0 **不** 新建权威 `toolcatalog`。唯一输入仍是今天装配出来的 `[]ToolDefinition`（`engineToolDefinitionsFor` + office/mcp/skill/plan/…）。

`modelfit.ProjectTools(defs, profile)` 只做：

- 按家族 schema 子集删/改 wire 字段，并标记 `lossy`
- 工具名映射到厂商允许的 charset / 长度（已有 `buildWireNames`，收编进投影）
- 可选：按任务裁剪只读 vs 副作用分组（先用现有 companion / task route 的过滤，不另造路由器）
- 输出 `tool_digest`，写入 request manifest

硬规则：

- canonical 工具名、风险、审批、sandbox 只来自本地代码
- Profile 不能把 wire alias 任意重映射到另一个 canonical 工具
- 破坏性工具在 lossy 时必须本地强校验
- 路径 canonicalize / symlink 边界检查保持在 `toolruntime`，投影改不了

P1 再考虑把 chat defs 与 `toolruntime` 的参数校验抽成同一份描述。M4 `capability.list` 保持独立命名空间，本项目不强制统一，避免把 M4 冻结范围撕开。

### 8.3 上下文

不重做 ADR-005。只加三个接缝：

1. `EffectiveContextCeiling(profile, callKind)` 成为 Chat assembly、compaction summarizer、Plan、子代理、月伴的唯一顶。没有更高 stage 的 qualified profile 时，行为与今天 `0.9375 * contextWindow` 相同。
2. request manifest 记录本轮纳入 / 压缩 / 省略的 source range。
3. 外部工具结果、网页、Office、MCP 继续视为 `untrusted_external_data`（现有产品已有这条精神，Slice 2 补回归：投毒 observation 改不了权限）。

1M：继续“逻辑历史可以很长，物理请求按质量封顶”。首发不追求 Stage C。

### 8.4 规则路由（不是模型选择器产品）

第一阶段禁止黑盒 ML。规则：

```text
用户显式选择
  → 能力门（月伴需要 ThinkingOff=qualified；视觉需要 Vision=qualified；无 Tools 则不注入工具）
  → 健康/凭据
  → 同 provider 的 flash 仅用于月伴/评判（沿用 pickCompanionFlashModel）
  → 成本/时延只作并列打破
```

硬规则：活动工具链禁止跨 family 透明切换；outcome unknown 的副作用禁止换模型重放；fallback 必须重新编译，且不得把 A 的 envelope 发给 B。

---

## 9. L-Version：跟着模型升级，而不被升级伤害

这是用户 D2 的落地定义。没有训练权时，“自动变强”只能等于：

```text
厂商发布新能力
  → 产品能在 协议层 接住（字段不丢）
  → 产品能在 能力层 认出来（probe）
  → 产品能在 表面层 用起来（Chat/月伴/Office/Plan 有落点）
  → 验证前 不影响 stable
```

### 9.1 状态机（首发人工晋级）

```text
discovered → quarantined → probed → bundled/beta → stable
                                 ↘ blocked
```

v2 的 `shadow` 首发不做。

触发：

- 用户手动添加 / `Discover` 出现新 ID
- 连续协议错误、schema invalid、续传 400 超过阈值
- 用户在设置里点“重新探测”

官方网页变化 **不** 自动改 effective。可以只记一条 discovered 备忘（P2）。

多数 OpenAI-compatible 端点没有稳定 fingerprint。主信号是 **固定 fixture 的行为指纹**（协议形状、错误码、能力结果），不是 header。

### 9.2 Probe 套件（每家每账号）

无敏感 fixture，不执行真实副作用工具：

1. 非流式 / 流式文本
2. usage / cache 字段是否出现
3. thinking off / 默认 /（若声明）effort
4. 带 tools 的续轮是否要求回传 reasoning
5. 单工具 + 非法 schema
6. 并行工具（失败则标 blocked，不假装支持）
7. 图片（失败则 Vision=blocked）
8. 8K / 32K recall 各 1 条（不做 1M）
9. 429 / 取消
10. 响应里的 model 字段是否与请求 ID 一致

Discovery 与凭据：沿用现有 network policy。禁止从网页派生带 Key 的 URL。probe 报告不得写入 Authorization。

### 9.3 能力吸收表（必须能回答“用在哪”）

| 厂商新能力 | 落点 | 未 qualified 时 |
|---|---|---|
| 更强思考 / effort | 复杂 Chat / 修复轮 | 保持 auto，不在 UI 承诺档位 |
| 更强工具 | 现有工具投影与循环 | 不扩目录 |
| 更长窗口 | `EffectiveContextCeiling` | 保持当前顶 |
| cache | 稳定前缀 + 用量展示 | 不伪造命中率 |
| 视觉 | 附件 / Office | 入口关闭 |
| Responses / partial | P1 Kimi 恢复 | 不启用 |
| 动态工具 / 服务端 Agent | 产品+安全评审 | 默认拒绝 |

### 9.4 升级分级

- Class A：ID / 窗口 / 价格 → 新 profile 文件 + probe
- Class B：同协议但工具/提示行为变 → 重校准片段与投影，再 probe
- Class C：新字段 / 新续传 → 改 codec，发版
- Class D：新模态或服务端 Agent → 单独 PRD

---

## 10. 数据与 Bridge

### 10.1 表（只加 3 张）

**`model_fit_profiles`**

- `digest` PK、`family`、`wire_pattern`、`endpoint_mode`、`body_json`、`channel`、`created_at`

**`model_revisions`**

- unique `(provider_id, wire_model, endpoint_mode)`
- `family`、`status`、`profile_digest`、`evidence_json`、`first_seen_at`、`last_seen_at`

**`provider_state_blobs`**

- envelope 元数据 + DPAPI/AEAD 密文
- conversation/turn/sequence/origin fingerprint
- TTL；删会话时销毁密钥

request manifest 先写成现有 token ledger / 诊断事件的附加 JSON，不先开第 4 张大表。评测结果以 `testdata/modelfit/` 和本地报告文件为权威，首发不上 `eval_*` 集群。

现有 `provider_models` 不无限加 bool。`ProviderDTO` / `ModelDTO` 不改字段。

### 10.2 迁移

1. 所有现有 provider 默认 `generic.openai` + 用户可选确认家族
2. 旧路径双读一个版本
3. 确认家族后的新请求绑定 profile digest
4. 不删旧 provider 行

### 10.3 Bridge

```text
modelFit.get          当前会话/供应商的 profile 与能力矩阵
modelFit.probe        对指定 provider+model 跑探针（需二次确认）
modelFit.probeStatus
modelFit.setFamily    用户确认家族
modelFit.diagnostics  最近续传失败、400 drift、profile digest
```

`probe` / `setFamily`：Engine 侧校验 + 交互 nonce。模型输出所在 WebView 不持有这些 capability。沿用现有 CSP 与 allowlist。

不做 v2 的 `promote` / `rollback` 远程包 API。换 profile = 发版或用户在高级设置里选择已内置的 beta digest。

### 10.4 UI

普通：显示名、家族、stable/beta、适用（仅 qualified）、思考 自动/关/深入（隐藏无效档）、不可用原因。

高级设置（不进左侧主导航）：能力矩阵四态、profile digest、最近 probe、手动探测、家族确认。

遵守现有信息架构：低频诊断进设置。

---

## 11. 安全（从 v2 留下、且必须留下的）

1. API Key 继续 DPAPI，不进 profile、日志、Renderer IPC
2. Provider state 密封、按 turn 限制、不作审计真相
3. Profile 不能新增本地工具或关审批
4. Probe 用固定无敏 fixture
5. `command.run` 继续 `commandEnv()` 白名单
6. 审批基于 canonical intent，不基于模型思考
7. origin 变化重绑凭据；同 origin 换 endpoint mode 不重输 Key，但必须换 profile
8. 发请求前同时校验 credential origin 与 profile endpoint identity

v2 里针对“被控制的 pack CDN、签名密钥轮换、时钟回拨复活过期包”的大段威胁模型，随远程 Pack 一并后置。本地 SQLite 被同用户进程篡改的风险，沿用现有审计链与文件 ACL，不在本 PRD 新造一条。

---

## 12. 功能需求

### P0 — 融合真正发生

- FR-001：Family / EndpointMode / Dialect 上线，旧 provider 默认 generic，行为字节级兼容
- FR-002：`internal/modelfit` + 内置 profile JSON + digest
- FR-003：DeepSeek、GLM dialect；Kimi Chat Completions dialect
- FR-004：TurnIR + ThinkingIntent，兼容 `DisableReasoning`
- FR-005：活动工具链 provider state 完整回传；缺态 fail closed
- FR-006：融合路径禁止 400 静默剥 thinking / 降 schema
- FR-007：Tool 投影覆盖现有定义 + 本地校验；lossy 不绕过破坏性工具
- FR-008：每请求绑定 profile digest 与 thinking/tool digest
- FR-009：新 model ID quarantine；stable 不被 Discover 结果覆盖
- FR-010：能力四态 + 10 项 probe；未 probe fail closed
- FR-011：月伴 / Office 经 intent 解析后现有测试通过
- FR-012：副作用、审批、沙箱不被 codec 绕过
- FR-013：不改 `ProviderDTO` / `ModelDTO` golden

### P1 — 产品更像“为这家模型做的”

- FR-101：家族 Prompt 片段可测可回滚
- FR-102：设置页能力矩阵与不可用原因
- FR-103：三家 cache 用量可观测（不伪造）
- FR-104：历史 tool linkage 补齐，智谱不再依赖 fold 作为唯一手段
- FR-105：Kimi Responses/partial 仅在 probe 证明对长生成/恢复有收益后启用
- FR-106：规则路由（月伴 fast、能力门、禁止跨 family 重放）
- FR-107：24 题 Native Fit Bench 可重复跑

### P2 — 持续吸收自动化（明确后置）

- FR-201：定时 Discover + 回归 probe
- FR-202：本地导入声明型 profile（仍禁止扩权）
- FR-203：签名包 / shadow / 学习路由 / 1M Stage C
- FR-204：Chat 关键 step 投影到 agentrun（独立项目，不在本 PRD 验收）

---

## 13. 实施切片（可真正排期）

假设单人主路径。每一刀结束都必须能给真实账号用。

### Slice 0 — 事实探针（3–5 天）

- 用目标账号对三家跑第 9.2 节 fixture，写出 `testdata/modelfit/probe-<family>.md`
- 冻结当前 generic 路径在 24 题里的失败分类（尤其是续传 400、智谱 orphan tool、月伴 TTFT）
- 文档声明与 probe 不一致的，一律 `blocked`

Exit：三家都有“能做什么 / 不能做什么”的实测表。没有实测表不准写死 wire 字段。

### Slice 1 — 协议保真（2–3 周）

顺序：基础设施 → DeepSeek → GLM → Kimi Chat Completions。

1. `TurnIR`、`ThinkingIntent`、Profile schema、generic 基线（旧行为金丝雀）
2. Provider state 密封；`chat_run_stream` 续轮回传
3. `openai.go` 拆 transport / dialect；融合路径关掉静默降级
4. DeepSeek thinking+tools fixture（缺 reasoning 必须本地拒绝发送）
5. GLM thinking 流与 orphan tool 回归
6. Kimi Chat Completions 投影（不做 Responses）

Exit：

- 通用路径测试不过期
- 三家 golden request/response fixture 在 CI
- 活动工具链重启后，缺 envelope 不能继续
- `DisableReasoning` 调用方仍编译通过

建议先改文件：

```text
internal/llmadapter/types.go
internal/llmadapter/openai.go
internal/llmadapter/common.go
internal/app/chat_run_stream.go
internal/app/chat.go          // 只接 Resolver，不重写循环
internal/app/provider_diagnostics.go
internal/modelfit/**          // 新
```

### Slice 2 — 产品编译（2–3 周）

1. 家族 Prompt 片段接入现有系统提示装配
2. `ProjectTools` 收编 `buildWireNames` + schema 子集
3. `EffectiveContextCeiling` 单入口
4. 月伴 / 短问候 / Office continuation 改读 ThinkingIntent
5. 设置页能力矩阵（declared/probed/qualified）
6. 前端 `modelSupportsFunctionCalling` 改读 Engine 能力，删除作为权威的正则

Exit：月伴 TTFT 回归通过；Office docx/ppt 测试通过；破坏性工具 lossy 投影仍被本地拦住。

### Slice 3 — 版本隔离 + 小评测（2 周）

1. `model_revisions` + Discover 新 ID quarantine
2. `modelFit.probe` + 诊断
3. 24 题 Fit Bench（见第 14 节）
4. 规则路由与 fallback 新 model step
5. 运营 runbook：续传失败、误标家族、probe 全灭、回退 generic

Exit：模拟“同一 display 名、换了行为”时，stable 会话仍用旧 profile；新 ID 需用户确认或保持隔离。

总日历：**约 8–10 周**，不是 v2 的 16 周平台。人员不足时宁可少做 Kimi 的 Responses，也不要跳过 DeepSeek 续传。

---

## 14. 验证

### 14.1 CI 合同（每 PR）

- Profile digest 稳定、未知字段拒绝
- 三家 compile / decode golden
- 缺 reasoning 的 DeepSeek/GLM 续轮不得出站
- 跨 conversation / origin / family 的 envelope 重放失败
- Tool 投影 lossy + 本地校验
- generic 路径与现有 gateway 测试等价
- Provider/Model DTO golden 不变
- 月伴 / Office / DisableReasoning 兼容测试

### 14.2 Native Fit Bench v1（24 题，不是 120）

冻结、可本地复现、带确定性校验器（文件 hash / JSON schema / git status / 审批日志）：

| 族 | 题数 | 例子 |
|---|---:|---|
| 编码 | 6 | 修编译错误、按测试改一处、解释失败日志 |
| 工作区 / Git | 4 | 读改一个文件、拒绝写出 workspace |
| Office | 4 | 生成/补丁 docx 或 xlsx 并打开校验 |
| 长上下文 | 4 | 在中部放置 protected fact，压缩后仍能召回 |
| 月伴 / 低时延 | 3 | 短问候无思考、工具后再说话 |
| 安全 | 3 | 越权路径、未审批不执行、投毒 observation 改不了政策 |

对照方法：同一账号、同一 wire model、generic vs fit。禁止用不同底模的分差宣传“融合收益”。

首发门：

- 高风险 3 题 100%
- 续传类失败（缺 reasoning / orphan tool 整请求 400）相对 generic 下降，且 fit 路径为 0
- 无 silent degradation
- 月伴 P95 首字不差于当前 stable 的 20%
- 不把 QTSR +15pp 当作能否发版的条件

### 14.3 必测反例

- DeepSeek tools 续轮不回传 reasoning
- GLM 强制 thinking 被当成可关，并塞进月伴
- 智谱历史孤儿 tool 再次以 `role:tool` 出站
- Kimi 被套用 DeepSeek schema 子集
- 400 后剥 thinking 仍返回成功
- 新 Discover ID 直接覆盖用户正在用的模型
- Renderer 调用 `modelFit.probe` 不经 Engine 授权
- envelope 换账号仍发送
- 删除会话后 state 密钥仍在

---

## 15. Definition of Done（v3 可验收）

1. 三家至少各有一个真实账号完成 Slice 0 probe 报告
2. 三家 Chat Completions dialect 有独立文件与 golden
3. 生产请求（已确认家族的）绑定 profile digest
4. DeepSeek/GLM 活动工具链 reasoning 续传在 CI 与 live 均为 0 丢失
5. 融合路径没有“剥 thinking / 降 schema 后当成功”
6. 月伴与 Office 现有回归通过
7. 新 model ID 默认 quarantine
8. 设置页能区分 declared / probed / qualified
9. 24 题集对至少一家完成 generic vs fit 对照
10. 远程/导入文件无法扩权
11. 不依赖微调、训练数据、厂商接口人
12. `ProviderDTO` / `ModelDTO` golden 不变

v2 DoD 里关于 5 分钟热回滚、120 题、QTSR 功效、Chat 进入 agentrun 证据链的条目，**不是本版本 DoD**。

---

## 16. 需求追踪

| 用户维度 | 目标 | 需求 | 切片 |
|---|---|---|---|
| D1 深度融合 | 模型能跑完 Lunitide 任务 | FR-003–008、FR-011–012 | 1–2 |
| D2 跟着升级变强 | 新能力可探测、可启用、可隔离 | FR-009–010、FR-103、FR-107 | 3 |
| D3 无训练权 | 增益只来自适配层 | 全文非目标 + DoD 11 | 全程 |

---

## 17. 官方资料与证据边界

访问日期：2026-09-08。公开文档只能证明“厂商声明过”，不能证明目标账号已开放。

- DeepSeek Thinking / Tool Calls / Context Caching：`https://api-docs.deepseek.com/guides/thinking_mode/` 等
- GLM Thinking / 型号页：`https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode`
- Kimi Overview / Tool Use：`https://platform.kimi.com/docs/api/overview`

仓库对照：

- `internal/domain/provider/provider.go`
- `internal/llmadapter/{types,openai,common,usage,factory}.go`
- `internal/app/{chat.go,chat_run_stream.go,chat_tool_defs.go,provider_diagnostics.go}`
- `internal/contextapp/` `internal/compactionapp/` `internal/toolruntime/`
- `web/src/provider/modelKind.ts`

---

## 18. 最终判断

Lunitide 要的不是“模型中台”，而是 **Model Fit Layer**：

- 比通用兼容层更完整地保留三家协议能力
- 比用户更早发现“这个 ID 已经不是上周那个模型”
- 比单模型 Demo 更懂 Lunitide 自己的工具、审批、Office 与月伴
- 用 24 题本产品任务证明适配是否真的变好
- 用内置 profile 发版，而不是先造远程配置飞机

三家模型是长期输入通道。Lunitide 自己能积累的，是编译器、续传、探针和本产品评测，而不是权重。

v2 把这件事写成了 16 周平台。v3 把它收成 **8–10 周、三刀可交付、每一刀都让真实任务更稳**。这是这份文档作为实施基线的理由。
