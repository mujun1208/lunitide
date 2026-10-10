# Lunitide 能力自补（Capability Bootstrap）设计

> 对照 OpenAI Codex 的实现方式，回答一个问题：**当用户要做的任务超出当前挂载的工具时，agent 应该"自己去查找并补上能力，然后继续执行"，而不是回答"我没有这个东西"。**

- 日期：2026-10-10
- 触发事件：用户实测「帮我查询中航材利顿航空科技股份有限公司，相关信息」，模型回复 `Not found. Searching tools.` 后终止，右侧浏览器面板却自动弹出一个空的百度搜索页。

---

## 0. 事件复盘：模型其实做对了，是系统没接住

数据库 `protocol_message_groups`（rowid 3385）里留着模型的原话：

> "Available tools listed: mcp_search, mcp_call, user_ask. No direct web.search tool in the function list! Only mcp tools. So I need to mcp_search for a web search tool."

这句话说明两件事：

1. **模型的自补意识是对的**：它发现缺 web.search，主动去 mcp.search 找一个搜索工具——这正是用户想要的「缺能力 → 自己找 → 补上 → 再执行」。
2. **系统没有接住这个意图**：mcp.search 只搜索**已连接端点**的工具（见 `chat_mcp_tools.go` 的 `mcpGatewayToolDefinitions`：`"Search the %d connected MCP tools by name or description"`），根本没有"可安装能力目录"的概念。模型在一个只有测试服务器的池子里找搜索工具，当然找不到。

### 根因链（七步）

1. `looksLikeCurrentLookupTurn` 关键词表是封闭白名单（天气/火车/股价…），**不含「查询 / 搜索 / 查一下 / 资料」**——这句检索意图没有被识别。
2. `toolsForThisTask` 所有分支未命中 → 返回 nil。
3. `wideFallbackKeep` 对该车道只给 `{user.ask}`。
4. `pickTaskTools` 强制保留全部 MCP 工具（`mcp_` 前缀 + `mcp.search`/`mcp.call` 永不剔除）。
5. 模型拿到的工具清单 = `[mcp.search, mcp.call, user.ask]`。
6. mcp.search 只搜已连接端点 → 无结果 → 模型放弃。
7. 兜底救援 `fallbackWebSearchArgs` 想注入 web.search，但被 `toolDefinitionsHave(req.Tools, "web.search")` 门控拦住——**工具不在清单，连救援都无法注入**。

另外网络层实测：DuckDuckGo 被墙（10s 超时），cn.bing.com 0.3s 可用。`toolruntime.searchWeb` 的三级回退（DDG → BingCN → Bing）本身是好的——**问题纯粹是工具没挂载，不是搜索实现坏了**。

---

## 1. Codex 是怎么做的

研究 openai/codex 仓库得出的五个关键机制：

### 1.1 搜索是服务端常驻能力，不参与逐轮工具清单

Codex 的 web_search 是 Responses API 的**服务端工具**，配置项是 `web_search = "cached" | "live" | "disabled"`。模型侧永远"有"这个能力——可用性由**配置声明**决定，而不是由某一轮的工具清单决定。泳道/画像再怎么收窄，也不存在"这一轮没有搜索"的状态。

> 对照 Lunitide：搜索是模型侧 function tool，会被泳道收窄剔除。这是本次事故的结构性原因。

### 1.2 一切工具归一为同一种形态

内置工具、MCP 工具、插件工具统一为 `ToolSpec::Function` + JSON Schema，MCP 工具归一成普通 function。模型不需要区分"这是内置的还是外部的"，也没有"外部工具要去另一个入口找"的心智负担。

> 对照 Lunitide：`mcp.search`/`mcp.call` 网关 + `mcp_` 前缀直挂（>12 个时折叠为网关）——归一已经做了，这是好底子。

### 1.3 Agent Skills：能力即文档，渐进披露

SKILL.md（agentskills.io 开放标准）：一个能力 = 一份 markdown（指令+脚本入口）。系统只把**名字+描述**放进上下文（约 2% 预算），模型用到时才读正文。能力的边际成本极低，所以可以预置几百个而不撑爆上下文。

> 对照 Lunitide：已有完整 skill 市场（skill.invoke / skill.try / skill.create），这一层不缺。

### 1.4 能力自举：发现缺口 → 列出可装 → 用户批准 → 安装 → 继续

Codex 有两个专门工具：

- `list_available_plugins_to_install`：列出**可安装**的插件（不是已安装的）
- `request_plugin_install`：请求安装，**走用户批准**，批准后 skill 声明的 MCP 依赖自动安装

这是"能力自补"的核心闭环：**agent 永远有一个明确的下一步动作**（去查可装目录），而不是终止。

> 对照 Lunitide：完全没有"可安装能力"的概念。mcp.search 的语义被模型高估了（它以为是市场，实际只是已连接端点的索引）。

### 1.5 AGENTS.md：项目级记忆

项目根的 AGENTS.md 声明约定/偏好，agent 每次进入项目自动读取。补装的能力、用户的工具偏好可以沉淀在项目级。

> 对照 Lunitide：已有 memory 体系，缺的是"能力偏好"这一类记忆。

---

## 2. 设计目标（来自用户原话）

> "如果我需要做一下查询搜索，或者需要执行一些任务，不能直接就告诉我没有这个东西。是不是要自己去查找然后补充上这个工具也好技能也好，补充完整他的能力，然后再去执行我的任务。"

拆成四条原则：

| # | 原则 | 含义 |
|---|------|------|
| 1 | **永不裸奔** | 任何任务意图下，基础能力（搜索/抓取/询问）不可被泳道收窄完全剔除 |
| 2 | **缺口可发现** | 模型发现缺工具时，有明确出口（检索可补能力目录），而不是终止 |
| 3 | **补能有批准** | 安装/连接新能力必须用户批准（Codex 同款红线） |
| 4 | **补能可沉淀** | 本轮补的能力要记住，下轮同类任务直接可用 |

---

## 3. 方案：四层递进

### 第一层：内置能力常驻（P0/P1，已完成）

**已实施（2026-10-10，P0）：**

1. `chat_intent.go` `looksLikeCurrentLookupTurn` 关键词表扩容：加入「查询 / 查一下 / 查一查 / 查查 / 搜一下 / 搜索 / 搜搜 / 查找 / 资讯 / 资料 / search / look up」→ 检索回合被正确识别，L1 车道 `AllowWebSearch=true`，`toolsForThisTask` 挂上 web.search/web.fetch。
2. `chat_lane.go` `wideFallbackKeep`：L3/L4（全工具面车道）未识别任务的兜底清单加入 web.search/web.fetch——**agent 在全能力车道上永远保有公共网页检索**。L1/L2 保持窄面（其车道过滤器本来就会再剥一次 web，语义不变）。
3. 前端 `browserOpenAsk` + SessionMessagePanel 门控：搜索类指令不再自动弹出右侧浏览器面板（详见第 5 节）。

**已实施（2026-10-10，P1）：**

- **统一双搜索实现**：回退阶梯下沉为 `webfetch` 共享助手（`SearchAttempts` / `ParseSearchSource` / `ChallengePage`）；`agentrunapp.WebSearch`（证据链路）改为与 `toolruntime.searchWeb` 相同的 DDG→BingCN→Bing 三级回退（每源 8s 超时、挑战页与非 2xx 视为该源失败、按源解析、证据摘要取获胜页面 body、SourceURI 优先 FinalURL），消除"证据链路被墙就断"。toolruntime 同步改用共享助手，删除本地 `searchChallengePage` 副本。
- **修正 mcp.search 的描述**：明确告知模型"只搜索已连接端点，不是可安装工具市场；无匹配时说明缺什么能力并引导用户去设置连接 MCP 端点"。
- **执行契约加"缺能力出口"**：`capabilityExitInstruction()`（[能力缺口约定]）在 `wantsTools` 的回合注入——模型缺工具时先查已挂载工具（含 MCP），确实缺少就告知用户缺什么、如何补，不得裸回"没有这个能力"。

### 第二层：能力目录 `capability.discover`（P2）

新增内置工具（或扩展 mcp.search 语义为 `mcp.discover`）：

```
输入：{ "need": "搜索某公司公开信息" }
输出：三类来源的匹配结果——
  1. 内置工具目录：web.search（当前会话可用/需开启）、browser.act…
  2. 技能市场：已有 skill 体系里匹配的技能
  3. MCP 端点目录（新）：预置的公共 MCP 服务器注册表
     （高德地图、必应搜索、天气、飞书…），
     每项标注：功能、所需权限、是否需要凭证
```

关键变化：模型问「我能不能搜 XX」时，得到的是**「可以，有这些途径」**，而不是空结果。配套系统提示词（执行契约加一条）：

> 「如果任务需要的工具不在当前清单里，先调用 capability.discover 查找可用能力，不要直接回答没有这个能力。」

**已实施（2026-10-10，P2）：**

- **`capability.discover` 工具落地**（`internal/app/capability_discover.go`）：输入 `{"need": "一句话能力描述"}`（1-400 字），输出四源匹配 JSON——`connectedMcp`（已连接端点工具，`ReadyToolSnapshot`）、`builtin`（内置工具目录 + 中文关键词映射 `builtinToolKeywords`）、`skills`（已安装 published 技能）、`installablePresets`（复用 `mcp6.Presets()` 18 项免费预置目录，含 needsCredential/argHint 等标注）+ `guidance`（有匹配→可直接用/可 mcp.install 安装；无匹配→明确告知缺什么，不得虚构工具）。
- **跨语言匹配**：`capabilityScore` 用 CJK 二元组（+3，停用词表 `capabilityStopBigrams` 滤掉「帮我/这个/什么」类泛化干扰）+ 概念组（+20）+ ASCII 词（+2），阈值 3；中文关键词映射桥接中文需求→英文工具描述。
- **网关工具永保**：`isGatewayTool`（mcp_ 前缀 / mcp.search / mcp.call / capability.discover / mcp.presets / mcp.install）统一替换 `chat_tool_profile.go`、`chat_lane.go`（pickTaskTools）、`task_route.go` 三处的散落判断——minimal/coding profile、任务路由收窄后这些"能力出入口"仍在清单上。
- **分发接入**：`chat_run_loop.go` settings-plane 分支 + `chat_settings_tools.go` 注册 def 与 invoke case；零服务 Engine 也挂载（自包含、nil 安全）。
- **契约升级**：`capabilityExitInstruction` 从"缺能力出口"升级为四步约定——查已挂载工具（含 mcp.search）→ capability.discover 四源查找 → 征得同意后 mcp.install（审批模式下引导设置页）→ 确实无匹配才告知缺口与补齐途径。
- 测试：`capability_discover_test.go` 8 项（评分/网关/内置命中/预置命中/无匹配/输入校验/零服务挂载/收窄永保），全量 `internal/app` 回归通过。

### 第三层：补装流 `capability.request`（P3）

对标 Codex 的 `request_plugin_install`：

```
模型调用 capability.request({ "capability": "bing-search-mcp" })
  → 引擎发 approval_required（复用现有审批 UI，注明权限与数据流向）
  → 用户批准 → 连接 MCP 端点 / 安装技能 / 引导配置凭证
  → 工具热挂载进本轮会话 → 模型继续原任务
  → 用户拒绝 → 模型收到明确拒绝信号，向用户转述并给替代方案
```

实现要点：

- **审批复用**：走现有 approval 流（scope: once/session/always），"always" 即等价于沉淀。
- **凭证走 vault**：需要 key 的服务在批准后由用户在设置页配置，密钥不进对话上下文。
- **热挂载先做简版**：工具清单变更与会话状态有一致性成本，第一版可以「安装完成 → 本轮以文字告知 → 下一轮生效」，跑稳后再做本轮内热插拔。

**已实施（2026-10-10，P3）：**

- **capability.request 落在既有 `mcp.install` 上**（不新造工具名，P2 契约与 capability.discover 的 guidance 均已指向它）：照 `SetOfficeExecutor` 先例给 toolruntime 注入 `SetMcpInstaller` 回调（引擎侧 `installMcpPresetViaRuntime` 包装原安装管线），`mcp.install` 加入 runtime 的 mutating 列表并进 execute switch——**安装从"引擎自行分发、审批模式一拒了之"改为走标准审批门**。
- **审批全链路复用**：手动审批模式下 `ErrApprovalRequired` → 现有 `EventApprovalRequired` 卡片 → `chat.tool.approve`（scope once/session/always）→ `DecideScoped` 执行安装 → 同流恢复继续原任务；拒绝则模型收到 `ok:false 用户拒绝了这次操作` 并转述。审批卡摘要新增 `mcpInstallApprovalSummary`：写明预置名、功能、以什么命令在本机启动第三方服务端、是否需要密钥及密钥去向（设置页，不进对话）。
- **语音轮不静默自动批**：`approvalProfileDangerous` 加入 mcp.install——月伴轮次同样弹卡（安装即信任，不能 companion 预授权）；FullAccess 仍按会话授权直行，AutoEdit 语义不变，无人值守（同事自动回复）维持拒绝。
- **简版热挂载**：安装成功输出与契约均注明「新工具从下一轮对话开始生效，本轮如实报告安装结果」；需密钥预置仍走 ConfigureOnly + 设置页配置（密钥不进对话上下文）。
- **收死第二执行路径**：`invokeSettingsPlaneTool` 移除 mcp.install case、`ungatedEngineToolDenied` 移除其拒绝分支——今后该名字只有 toolruntime 一条执行路径，审批门不可能被绕过。
- 测试：toolruntime 4 项（审批门/已批执行/无安装器失败关闭/Prepare-Decide 往返恰好执行一次）+ app 侧（ungated 家族更新、审批卡摘要、危险名单、安装器无服务失败关闭）；toolruntime 与 app 全量回归通过。

### 第四层：沉淀（P4）

- 补装成功写入 memory：「用户批准了 XX 端点，用于 YY 类任务」。
- 下轮同类任务：`autoToolProfile` / 泳道判断时优先挂载已批准能力。
- 项目级对照 AGENTS.md：后续可在项目根支持 `LUNITIDE.md` 声明工具偏好。

**已实施（2026-10-10，P4）：**

- **安装批准沉淀为语义记忆**（`internal/app/capability_memory.go`）：`installMcpPresetViaRuntime` 成功后调用 `recordApprovedMcpPreset`，向项目语义层（LayerSemantic / ScopeProject，key=`mcp-preset-approval:<presetId>`，upsert 不重复）写入「用户批准安装并连接了 MCP 预置 <名称>：<描述>。同类任务可直接使用其 mcp_ 前缀工具」；需密钥预置写「待配置密钥并连接」。门控用 `AllowExplicitSave`（审批卡是显式用户动作，手动记忆模式也写、关闭模式不写），失败仅记日志不影响安装结果。后续轮次经记忆召回（Search 命中语义层）即可知道"用户已为此类任务批准过该端点"。
- **优先挂载已由 P0–P2 结构性保证**：已连接端点的 mcp_ 工具经 `isGatewayTool` 在 minimal/coding profile、任务路由收窄、泳道收窄后全部保留——「已批准能力优先挂载」无需新增判断，本层只补"知道批准过"的记忆面。
- **安装幂等 + 重连引导**：`invokeMcpInstallPreset` 对持久映射（endpointID→presetId，跨重启存活）中仍存活于注册表的端点直接返回 `already_installed`（不重复 Add）；`capability.discover` 对安装过的预置把 How 改为「此前已安装过；若工具未挂载请在设置中重新连接，无需重复安装」。
- **项目级 `LUNITIDE.md`**：`repo_guidance.go` 的仓库约定链在每级目录同时读 AGENTS.md 与 LUNITIDE.md（各 4KB 上限、共享 12KB 预算、远处先丢），LUNITIDE.md 作为月汐专属的工具与能力偏好声明（如常用 MCP、检索口径）与 AGENTS.md 并列注入。
- 测试：`capability_memory_test.go` 3 项（语义层写入与 upsert/无服务失败关闭/持久映射识别）+ discover 重连标注 + LUNITIDE.md 注入（并列与单独存在两种形态）；app 全量回归通过（179s）。

---

## 4. 为什么不直接"把所有工具永远挂上"

反方向的最粗暴解法是取消泳道收窄、永远全量挂载。不做，理由：

1. **L1 的价值就是快**：闲聊/一步问答车道 DisableReasoning + 1 步预算，全量工具会让模型在不需要工具时规划工具调用，变慢变贵。
2. **窄清单是防幻觉护栏**：现有设计里"工具不在清单"本身就是一种引导（逼模型走 user.ask 而不是瞎执行）。我们要修的是「**基础检索能力**不可缺席」，不是「所有能力常驻」。
3. Codex 的启示恰恰是**分层**：搜索这类基础设施=服务端常驻；长尾能力=渐进披露的 Skills；新能力=批准制安装。三层各管一段。

---

## 5. 附：浏览器面板自动弹出的修复（问题一）

**根因**：`browserPage.ts` 的 `browserFollowURL` 对任何含「搜索/搜一下/查一下/查一查/帮我查/查找」的指令都生成百度搜索 URL；SessionMessagePanel 发送时拿到该 URL 就 `setWorkspaceOpen(true)` 弹出右侧浏览器面板。「帮我**查**询中航材利顿…」命中「帮我查」→ 弹面板。

**修复**（保留已打开面板的导航辅助，只取消自动弹出）：

- 新增 `browserOpenAsk(text)`：仅识别**明确的打开意图**（粘贴裸 URL / 「打开 https://…」/「打开百度」）。
- SessionMessagePanel：`browserFollow` 仍派发 `lunitide:browser-open`（面板已开时导航过去，搜索辅助保留），但**弹出面板**只在 `userWantsBrowserPanel(basePrompt) || browserOpenAsk(basePrompt)` 时发生。
- 效果：「搜索一下竞品」「帮我查询 XX 公司」→ 不弹面板；web.search 的结果地址照常进地址条，用户点浏览器标签或点产物卡片时才看到页面。「打开这个网页」「粘贴 URL」→ 照常弹出（用户明确要求看页面）。

---

## 6. 落地节奏

| 阶段 | 内容 | 状态 |
|------|------|------|
| P0 | 关键词扩容 + L3/L4 泳道兜底 + 面板弹出门控 | ✅ 2026-10-10 已完成并过测试 |
| P1 | 双搜索实现统一；mcp.search 描述修正；执行契约加"缺能力出口" | ✅ 2026-10-10 已完成并过测试 |
| P2 | `capability.discover` + 预置 MCP 端点目录 | ✅ 2026-10-10 已完成并过测试（复用 mcp6.Presets 目录） |
| P3 | `capability.request` 审批流 + 挂载 | ✅ 2026-10-10 已完成并过测试（落在 mcp.install，走 toolruntime 审批门） |
| P4 | 能力偏好记忆 + 项目级 LUNITIDE.md | ✅ 2026-10-10 已完成并过测试（语义记忆 + 安装幂等 + 重连引导） |

## 7. 风险与红线

- **安装即信任**：引入 MCP 端点 = 供应链风险。必须用户批准 + 权限/数据流向标注（对标 Codex 用户批准制），预置目录里的端点要有准入审核。
- **凭证安全**：API key 只进 vault，不进对话、不进模型上下文。
- **描述即契约**：工具 Description 必须与真实语义一致（本次 mcp.search 被模型高估就是描述与能力的错位）。
- **热挂载一致性**：工具清单变更需与会话状态、审批 scope、泳道缓存对齐；先做下轮生效的简版。
