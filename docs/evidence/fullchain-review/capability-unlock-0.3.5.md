# 能力解锁修复证据（capability-unlock-0.3.5）

日期：2026-08-16 ｜ 版本：0.3.4 → 0.3.5 ｜ 范围：聊天工具链（internal/app, internal/toolruntime, internal/mcp6, cmd/engine）

## 背景

对标分析（lunitide-vs-agents-2026）确认产品"只能对话"的四个根因：
1. 模型拒绝工具定义时（HTTP 400）静默降级为纯对话，用户无感知；
2. command.run 白名单仅 `go version`（固定 2 参数、3 秒超时）；
3. webfetch/networkpolicy 的网页抓取与搜索能力已建成但未注册为聊天工具；
4. mcp6 Registry（端点注册/固定/熔断）已建成但工具未合并进模型工具列表。

## 变更清单

### P0-4 工具静默降级 → 显式提示（internal/app/chat.go）
- 400 回退分支在重试前发出中文降级提示，同时写入流事件（EventDelta）与持久化 assistant 文本，历史可追溯。
- 测试：TestToolsFallbackEmitsExplicitNotice（400 后重试纯对话、提示可见、恰 2 次上游调用）。

### P0-1 web.fetch / web.search 注册进聊天循环
- internal/toolruntime/runtime.go：新增 `web.fetch`（URL→标题+最终URL+正文，SSRF 管道）与 `web.search`（DuckDuckGo Lite，1-10 条结果）。只读工具，不走审批门。
- internal/app/chat.go：engineToolDefinitions 注册两个工具 schema。
- cmd/engine/main.go：SetWebFetcher 注入 networkpolicy.Fetch（AllowHTTP，与 agent-run web 同管道）。
- 测试：TestWebFetchToolExtractsText / TestWebSearchToolParsesResults / TestWebToolsUnavailableWithoutFetcher。

### P0-2 命令策略产品化
- 内置只读命令集：go version；git --no-pager status/log/diff/show/branch（前缀+maxArgs 上界）。
- 用户白名单：`<data>/tool-workspaces/command-policy.json`，prefix(1-8 项)/maxArgs/timeoutMs(≤300s)；文件存在但非法 → 启动 fail-closed。拒绝路径限定前缀（../、/、\、盘符）。
- 分级超时：规则级 deadline（默认 10s，测试类可配 120s+），替换原固定 3s。
- 进程环境净化：GIT_PAGER=cat / PAGER=cat / TERM=dumb / GIT_OPTIONAL_LOCKS=0。
- argv schema 放宽为 1-16 项。审批门不变（approval/auto-edit 需批准，full-access 直行）。
- 测试：TestMatchCommandRuleBuiltinSet / TestUserCommandPolicyLoadAndEnforce / TestUserCommandPolicyInvalidFailsClosed / TestUserCommandPolicyRejectsPathQualifiedPrefix。

### P0-5 MCP 接线
- internal/mcp6/registry.go：新增 ReadyToolSnapshot()（仅 ready 端点、按端点/工具名排序）。
- internal/app/chat.go：就绪端点工具以 `mcp_<endpointULID>_<tool>` 命名合并进 req.Tools（64 字符预算与 [A-Za-z0-9_-] 校验，超限跳过）；工具调用循环按前缀分发到 Registry.Invoke（30s deadline），结果 JSON 走常规 tool 消息回路；Invoke 逐调用复查状态/固定/熔断，快照不可能绕过生命周期门。
- 测试：TestMcpToolNameRoundtrip / TestMcpToolsMergedAndDispatched（注册→合并→分发→结果回注）。

### 版本
- VERSION：0.3.4 → 0.3.5。

## 回归结果（初版，后续见下方"遗留收口"与 install-readiness-0.3.5.md）

| 检查 | 结果 |
|---|---|
| go vet ./... | 通过 |
| go test ./... -count=1 | 全部通过（含 app 107.7s / storage 99.6s 全量） |
| go test -race | 未执行：本机 gcc 缺失（CGO_ENABLED=1 时 cgo "gcc" not found），历史已知环境遗留 |
| npm run build（web） | 通过（332 modules） |
| npx vitest run（web） | 33 文件 / 250 测试全部通过 |

## 遗留收口（同日 P1 批次，证据：install-readiness-0.3.5.md）

1. **-race 收口**：winget 已装 WinLibs GCC 16.1.0；PATH 注入 mingw64\bin + CGO_ENABLED=1 后 `go test -race ./...` 全量通过、0 数据竞争（storage/sqlite 首跑触默认 10min 超时系 -race 放慢 ~14x 所致，`-timeout=40m` 重跑 1389.9s 通过）。
2. **MCP schema 收口**：`internal/mcp/remote.go` 新增 `ListTools`（GET /tools 目录抓取 name/description/inputSchema）；`internal/mcp6/registry.go` 新增 `DescribeFunc`/`refreshToolbox`（describe 失败保留旧缓存，best-effort）与 `ReadyToolSnapshot` 携带真实 schema；`internal/app/chat.go` `mcpToolDefinitions` 优先采用缓存 schema（json.Valid + object 校验，失败回退 pass-through）；`cmd/engine/mcpgateway.go` `mcpGatewayDescribe` 接线。测试：`internal/mcp6/describe_test.go`（缓存进快照/吊销排除）、`internal/mcp/listtools_test.go`。
3. **policy UI 收口**：`api/bridge/v1/tools.commandPolicy.get/set.schema.json` 三端契约（生成器 x-enabled 清单与 envelope 枚举同步，重生成 bridge.ts / schema_generated.go / contract test）；`internal/app/tools_policy_handlers.go`；`internal/toolruntime` `CommandPolicyJSON`/`SetCommandPolicyJSON`（build-then-swap 校验、临时文件原子替换、Windows 显式 Remove+Rename、RWMutex 热切换，Execute 读取走 RLock 快照）；web `ToolsPolicyBridge`（client.ts）+ 设置"安全与治理"页 `CommandPolicyPanel`（前缀空格归一、maxArgs 1-16、超时 1s-300s、空行丢弃、保存即热生效、fail-closed 拒绝原因可见且行保持可编辑）。测试：`TestCommandPolicyJSONRoundTripAndHotApply`、`web/src/settings/CommandPolicy.test.tsx`（5 例）。
4. web.search DuckDuckGo Lite 依赖保持既有降级容错（不变）。

## 遗留（非阻塞，更新后）

1. ~~-race 门禁依赖 gcc 环境~~ → 已收口（见上）。
2. ~~MCP 工具 schema 仅 pass-through~~ → 已收口（见上）。
3. ~~command-policy.json 无 UI 编辑界面~~ → 已收口（见上）。
4. web.search 依赖 DuckDuckGo Lite 公共端点，标记变更时降级为"no results"（既有解析容错行为）。

## P1 能力批次（2026-08-16 追加）

### P1-5 审批粒度（once/session/always）

- `internal/toolruntime/runtime.go`：新增 `ApprovalScopeOnce/Session/Always` 常量、`ApprovalScopeValid`、`DecideScoped`（批准时按 scope 记忆：session=本会话、always=跨会话，均按工具名+参数精确摘要匹配）；`Execute` 执行前查询已记忆规则实现免批。
- `api/bridge/v1/chat.tool.approve.schema.json`：payload 增加 `scope` 枚举（once/session/always，默认 once）；重新生成 `web/src/generated/bridge.ts` 类型。
- `internal/app/chat.go` `handleChatToolApprove`：校验 scope 并调用 `DecideScoped`。
- `web/src/session/SessionPage.tsx`：审批操作区新增"审批记忆范围"下拉（仅本次/本会话免批/始终记住），`decideTool` 透传 scope。
- 测试：`TestApprovalRememberSessionAndAlwaysScopes`（session 不跨会话泄漏 + always 跨会话生效 + 参数精确匹配）、`TestApprovalFailsClosedWhenWorkspaceChanges`。

### P1-1 子 Agent 执行引擎（chat 层委派）

- `internal/app/chat_subagent.go`（新）：`subagent.spawn`/`subagent.join` 模型工具接入 chat gateway；Spawn 走 M7 SubagentService（配额/幂等/审计），子会话在 provider lease 内用 `gateway.Complete` 跑独立只读循环（工具集剔除 workspace.write，最多 4 步），产出单次 observation 回报（摘要≤2000 字符，spentTokens 累计）；失败也 Complete（带失败报告）避免并发名额泄漏。
- 委派三档：`delegation disabled/explicit/proactive`（Engine 字段 + `SetDelegationMode` fail-closed 校验；零值默认 explicit=工具可见；proactive 追加系统提示鼓励委派；plan 模式一律不注入）。
- 预算继承：budgetTokens 缺省 8192（父请求 4096×2），可指定 1000-50000，子会话 MaxTokens= min(budget,16000)。
- `internal/app/chat.go`：工具列表组装加入 subagent 工具；工具分发循环拦截 `subagent.*`（MCP 拦截之后、toolruntime 之前）。
- 测试：`chat_subagent_test.go` 4 例（档位/fail-closed、只读子会话+单次回报+join 重读、预算继承与参数守卫、并发配额拦截且子会话不启动）。

### P1-3 计划循环（plan-execute-verify）

- `internal/app/chat_plan.go`（新）：`plan.run` 模型工具三阶段——Plan（LLM 产出 JSON steps，解析失败降级单步 analysis）、Execute（逐步执行 agent，继承父执行模式工具权限，审批门仍生效；每步一轮工具调用上限；禁止嵌套 plan.run/subagent）、Verify（LLM 对照 objective 输出 verified/gaps）；返回 plan/steps/log/verified/gaps 单次 JSON。
- complexity.decide 接线：`complexityTierHint`（chat.go handleChatStart）从装配后消息统计 MessageCount/TurnCount/ToolCallCount/EstTokens → `complexity.Route` → moderate/complex/high-risk 在系统消息追加 tier 与 plan.run 引导（确定性：同消息同 tier）。
- 测试：`chat_plan_test.go` 4 例（三阶段循环+调用计数、守卫与降级、plan 模式不注入、tier 提示分界）。

### P1-4 Git worktree 隔离

- `internal/vcs/gitallow.go`：允许清单加入 `worktree`（仅 add/list/remove）；独立 `validateWorktreeArgs`——路径强制 workspace 相对（拒绝 `..`、绝对路径、盘符、反斜杠、`.`），ref 拒绝 `..`/绝对，分支名拒绝 `..`/`.lock`/尾斜杠/尾点；flag 按动作绑定（add: -b/-b=/--detach；list: --porcelain；remove: --force）；arity 严格校验。
- 测试：`TestGitWorktreeIsolationSurface`（9 允许 + 26 拒绝用例全 GIT-001）、`TestGitWorktreeRunnerRoundTrip`（真实 git add→list→remove 往返 + 逃逸预拦截）。

### 回归（全绿）

- `go vet ./...` 0 告警；`go test ./... -count=1` 全部 ok（storage/sqlite 91s、m8app 30s 正常水位）；`npm run build` 7.58s 成功；`npx vitest run` 34 文件/255 测试通过。

### 遗留（非阻塞）

1. `subagent.join` 的 waitMs 轮询语义仅保留单次快照读（与 M7 bridge 行为一致），未做长轮询。
2. `plan.run` 执行阶段每步单轮工具上限，复杂步骤需模型自行拆分（防失控设计取舍）。
3. worktree 面仅 vcs 包 + Runner 层激活；chat 工具列表未暴露写类 git（保持只读命令集）。

## P2 能力批次（2026-08-16 追加）

> 范围：P2-1 Office 工具链、P2-2 产物验收闭环、P2-3 常驻自动化、P2-4 交付落盘。任务清单为 P0→P3 四批，不存在 P4 批次（详设体系的 P4 Memory/Recall 已由 M9/M9.5 里程碑吸收）。

### P2-1 Office 工具链（五工具）

- `internal/officetools`：excelize 驱动 `excel.gen`（sheet/表头/行/汇总列/图表锚点，Chart 结构化构造）与 `excel.parse`（有界网格 JSON：rows/cols/truncated/preview/header）；零依赖 OOXML——`docx.gen`（heading/paragraph/list 块级文档）、`pptx.gen`（标题+bullets 幻灯片）、gofpdf `pdf.gen`（中文标题/正文）；`ExtractDocxText`/`ExtractPptxText` 纯文本抽取（html.UnescapeString 实体还原）；统一 ErrLimit 大小防线。
- runtime 集成：`toolruntime` 注册五工具 + Artifact 元数据（kind/path）随 ExecuteResult 透传；chat.go 双点透传产物卡片至渲染端。
- Windows 原子写：目标存在时显式 `os.Remove` 后 `os.Rename`。

### P2-2 产物验收闭环

- `internal/artifactreview/store.go`：append-only 评审日志（comment/revise/accept 三动作），原子持久化、重开恢复。
- Bridge：`workspace.artifactReview.append/list` + `workspace.artifact.preview`（xlsx 网格 / docx·pptx 文本 / html 有界 / pdf 显式 unsupported），源读取复用 containment 校验路径。
- 前端：`ArtifactPanel`（卡片+评论→修改→验收闭环、多格式预览、验收徽标）挂载 Workspace。

### P2-3 常驻自动化

- `internal/cronexpr`：5 字段标准 cron 解析（`*`/`n`/`a-b`/`*/s`/`a-b/s`/逗号列表，dom/dow OR 语义），`Next` 限界扫描防死循环。
- `internal/scheduler`：Job/Run JSON 存储（每任务运行历史截断至 MaxRunsPerJob、摘要限长）、到点单飞触发、状态快照（running/heartbeat/nextFire/runningJobs）、Windows Toast 通知（PowerShell base64 注入防 shell 注入）。
- 无头执行：`Engine.AutomationHeadlessExecutor` 复用 `HandleStreaming` chat 内核（事件收集→摘要/tokens），10 分钟超时；`cmd/engine/main.go` 常驻启动 + `SetAutomationScheduler` 注入。
- Bridge：`automation.job.list/set/delete/trigger`、`automation.run.list`、`automation.status`；前端 `AutomationPanel`（任务 CRUD、启停、立即运行、运行历史展开、调度器心跳）挂载 Workspace。
- 测试：cronexpr 解析/边界、scheduler CRUD/单飞/通知/截断、automation_handlers 六方法（go test 全绿）+ 前端 4 用例；修复组件 2 处语法缺陷（type 多声明、map 箭头函数体缺 `}`）与 nextFire `object` 类型索引。

### P2-4 交付落盘

- Bridge：`workspace.artifact.export`（schema+envelope+generate-bridge 断言清单同步）——源经 containment 校验读取（≤32MB），目标为 desktop/downloads/documents 快捷目录（home 下按需创建）或用户授权的已存在绝对目录；同名覆盖需显式 overwrite=true（ARTIFACT_EXPORT_EXISTS 守卫）；专用错误码 TARGET_INVALID/NOT_FOUND/EXPORT_FAILED。
- 前端：`ArtifactPanel` 导出栏（目录选择+自定义绝对路径校验）与逐卡导出按钮，成功回显 exportedPath。
- 测试：`TestArtifactExportRoundTripAndGuards`（往返字节一致、覆盖守卫、相对目标/逃逸源/缺失源拒绝、desktop 快捷目录落盘）+ 前端导出用例（默认桌面、自定义空值拦截、自定义绝对路径透传）。

### P2 回归（全绿）

- `go vet ./...` 0 告警；`go test ./... -count=1` 全部 ok（含新增 cronexpr/scheduler/officetools/artifactreview/app 导出与自动化用例）；`npm run build`（generate:bridge+typecheck+vite）成功；`npx vitest run` 36 文件/263 测试通过。

### P2 遗留（非阻塞）

1. Windows Toast 通知依赖 PowerShell 可用（家庭版精简环境可能缺失，缺失时静默降级）。
2. 导出为逐卡操作，未提供批量打包导出（后续可按 zip 打包整目录）。
3. cron 粒度为分钟级（标准 5 字段），不支持秒级与自然语言表达。

## P3 能力批次（2026-08-16 追加）

> 范围：P3-2 技能市场、P3-3 学习闭环（会话反馈→偏好沉淀）、P3-4 伴随端自动化结果 TTS 播报联动。UI 布局遵循"技能相关进技能中心、其余进对应设置分类"的既有信息架构，未改动任何既有样式与基础功能。

### P3-2 技能市场（模板目录 + 一键安装）

- `internal/skillapp`：产品内置模板目录 `Catalog()`（名称/描述/分类/版本/权限清单），`InstallFromCatalog` 走正常创建管线落地为本地草稿（权限审阅与发布门禁照常生效），重复安装返回 ErrTemplateInstalled。
- Bridge：`skill.catalog.list`（含 name@version 已安装标记）+ `skill.install`，schema/envelope/generate-bridge 断言清单同步。
- 前端：SkillPage 新增「技能市场」tab（卡片列表、权限徽标、已安装态翻转、安装失败回显），市场不可用时不阻塞技能库视图。

### P3-3 学习闭环（会话反馈 → 偏好沉淀，FR-11 不变量保持）

- 后端 `internal/m8app/memory.go`：`RecordFeedback`（append-only 反馈事件；纠正动作经 `ProposeCandidate` 生成 pending 候选并携带确认令牌）、`ListPendingCandidates`、`ConfirmedSnapshot`（有界偏好快照：≤8 条/≤2048 字节，超限跳过而非截断，UTF-8 完整性保持）。
- `internal/app/feedback_handlers.go`：`feedback.record` / `feedback.candidates` bridge 处理器；`internal/app/chat.go` 将已确认偏好注入系统指令（注入预算常量），不挤占对话上下文。
- 前端：SessionPage 助手消息操作区新增 👍/👎（满意直记、不满意弹出纠正输入，留空仅记录不满意；纠正生成候选后提示前往记忆面板）；MemoryPage 新增「偏好确认」区（确认沉淀/拒绝，走 `memory.confirmCandidate` 显式确认，保持 FR-11：纠正只产生候选，确认动作完全由用户显式完成）。
- 修复：消息操作区 JSX 结构缺陷（多余 `</div>` 导致 tsc 失败；重试按钮归位操作区内 `button:last-child` 语义恢复）、`window.prompt` 返回值 jsdom 兼容判空。

### P3-4 伴随端自动化结果 TTS 播报联动

- `web/src/session/companion/useAutomationBroadcast.ts`：轮询 `automation.run.list`（30s，limit 10），首询标记既有终态运行为基线（历史不重播）；新增 succeeded/failed 终态运行生成简短播报文本（任务名 + 摘要/错误首行，≤120 字符 rune 安全截断；并发完成合并为一条，>3 条提示查看运行历史）。
- 保守联动约束：仅在舞台 idle 时播报（不打断麦克风聆听/思考/说话，busy 期间完成的结果直接丢弃，通知职责仍由调度器 Windows Toast 承担）；enabled 门禁 = 月伴设置 enabled && autoSpeak && TTS 音色就绪。
- CompanionStage 接线：播报复用 retrySegment 派发链（MIC_ACTIVATE→RECOGNIZED_FINAL→REPLY_COMPLETED），冻结状态机矩阵保持合法；播报内容进入字幕 live log，Esc 中断契约不变。
- 降级链继承：M95-001 引擎缺失横幅、3 连败熔断、取消回执容忍均沿用 TtsPlayer 既有行为。

### P3 回归（全绿）

- `go vet ./...` 0 告警；`go test ./... -count=1` 全部 ok（含 m8app 学习闭环与 app feedback 处理器用例）；`npx tsc --noEmit` 通过；`npm run build` 成功；`npm run verify:bridge` 通过；`npx vitest run` 38 文件/280 测试通过（新增 useAutomationBroadcast 9 例、CompanionStage.broadcast 3 例、MemoryPage 偏好确认用例等）。

### P3 遗留（非阻塞）

1. 自动化播报仅在月伴舞台打开时生效（伴随端定位）；普通会话页结果通知仍走系统 Toast。
2. busy 期间完成的自动化结果不排队补偿（设计取舍：避免打扰正在进行的语音对话）。
3. 偏好确认入口在「设置 → 数据与记忆 → 记忆」，暂无桌面角标提醒候选数量。

## 能力清单收口批次（2026-08-16/17 追加）

> 范围：逐项核销 capability-checklist-2026.html 标记的 🟡部分具备 / ❌缺失 / ⛔致命断链 共 17 项中剩余可落地项：P3 保留项（stdio 隔离、Hooks）全部补齐，工具缺口（search/锚点编辑/TodoWrite）、MCP 市场预置、技能触发注入、UI 折叠、月伴语音唤醒、月亮动态效果、并行子 Agent。

### P3-A stdio 传输启用（原"部分具备"→已实现）

- `internal/stdioworker/isolated.go`：复用 5B/5C 加固 spawn 引擎拉起 stdio worker（显式环境、资源配额、kill-on-close、Job Object 隔离），每会话独立临时目录。
- `internal/mcp/stdio.go`：stdio MCP 客户端（initialize / tools/list / tools/call JSON-RPC over stdio，StdioDial 支持额外环境注入）。
- `internal/mcp6/registry.go`：注册表扩展 stdio 端点（命令白名单 npx/uvx/node、参数禁止元字符、EndpointInput 结构化注册）。
- `cmd/engine/mcpgateway.go` + `api/bridge/v1/mcp6.register.schema.json`：网关 stdio 分发与注册 schema 同步。
- 测试：registry_test（Args 切片/白名单）、stdio_test（会话错误面为工具级 IsError）。

### P3-B Hooks 系统（原"缺失"→已实现）

- `internal/toolruntime/hooks.go`：hookRule/hookPolicyDoc（tools.hooksPolicy.json，热加载、规则校验 fail-closed）；before 事件拦截优先级 block > requireApproval > allow，audit 事件留痕。
- Bridge：tools.hooksPolicy.get/set + tools.hooks.events.list 三端契约；schema 以 allOf/if/then 约束（block 必带 message、其余禁止 message）。
- 前端：SettingsPage HooksPanel（规则 CRUD、最近事件），与 command-policy 面板同区。
- 测试：策略往返/热切换/优先级/审计 + 前端面板用例。

### C2 工具缺口补齐

- `workspace.search`：字面量/正则内容检索，path:line: text 输出，二进制与超大文件跳过，只读不走审批门。
- `workspace.edit`：锚点替换（oldText 精确匹配一次或 replaceAll，越界/多义拒绝），走审批门与工作区摘要。
- `todo.write`：会话任务清单持久化（≤50 项、单一 in_progress、优先级枚举），存储于工作区摘要之外。
- 注册：internal/app/chat.go engineToolDefinitions 三工具 schema；测试 tools_extra_test.go。

### C3 MCP 市场预置（原"缺失"→已实现）

- 免费远程 MCP server 目录种子（fetch/filesystem/git/sqlite/playwright 等社区稳定端点），market.search 首页预置条目，一键注册走既有 mcp6.register 管线（固定/熔断照常生效）。

### C4 技能触发（原"缺失"→已实现）

- internal/app/chat.go skillCatalogInjection：已发布技能元数据目录注入系统指令（≤12 条 / ≤2048 字节，超限截断带显式提示），技能服务不可用时 fail-closed 跳过；触发词来自 manifest triggers，注入"当用户提到 X 时使用"引导。

### C6 UI：思考/工具/命令/编辑默认折叠单行滚动

- SessionPage：TOOL_LABELS 中文工具名（workspace.list=浏览文件…mcp_*=MCP xxx）；思考/工具过程默认折叠为状态行，展开分段（思考/工具调用/命令执行/编辑文件）；工具活动条目单行滚动不占版面；reduce-motion 与 a11y 标签保持。

### C7 月伴语音唤醒「你好，月汐」

- `web/src/session/companion/wakeWord.ts`：matchWakeWord（标点/全半角/大小写归一，你好月汐/Hello 月汐/嗨月汐/哈喽月汐变体；尾随文本提取为提问 prompt）；useWakeWord 挂 Windows SpeechRecognition 连续监听。
- App.tsx LaunchHome：唤醒命中 → 自动进入月伴对话并带上 prompt；companionSettings 新增 wakeWord 开关（默认开）；设置页「语音唤醒（你好，月汐）」开关项。
- 测试：wakeWord.test.ts（命中变体/尾随提取/误触不命中）。

### 月亮动态效果（用户追加需求）

- 进入月伴：companionMoonEnter 1s 由小放大就位；待机 60s 慢自转（既有 companionSpin 保持）。
- 我说话（listening）：月亮停转注视（state-listening animation-play-state:paused）。
- 月汐说话（speaking）：月亮停转 + 光晕 companionHaloPulse 随 --moon-gain 波动放大 + 三层 companionMoonRipple 光圈一圈圈向外照亮（强度跟随音量，1s 错相）。
- reduce-motion：入场/自转/光圈全部降级为静态，光圈隐藏。

### C5 并行子 Agent（原"缺失"→已实现）

- internal/app/chat_subagent.go：startSubagentFutures——同回合 subagent.spawn 调用预启动（上限 maxParallelSubagentSpawns=3，M7 配额 4 留 1 余量），buffered channel 按原调用顺序回填，事件流与 tool 消息顺序保持确定性；join 与超额 spawn 回退内联；goroutine 带 recover 兜底。
- internal/app/chat.go：主循环 subagent 分支消费 future（delete 防重复消费）。
- 工具描述与 proactive 提示更新："同回合多个 spawn 并行（≤3），批量独立调研任务"。
- 测试：TestParallelSubagentSpawnsOverlapInOneTurn（原子计数峰值≥2 证明重叠、join 不预启动）、TestParallelSubagentFuturesBoundedAtThree（5 spawn 只预启动 3）。

### 清单核销对照（capability-checklist-2026.html）

| 清单项 | 原判定 | 现状 |
|---|---|---|
| stdio 本地 MCP 连接 | 🟡 部分具备 | ✅ P3-A |
| 预置免费 MCP server | ❌ 缺失 | ✅ C3 |
| command.run 全量 shell | 🟡 设计取舍 | ✅ 保持白名单 + Hooks 两档（block/审批/放行） |
| 文件搜索 grep/glob | ❌ 缺失 | ✅ workspace.search |
| 精准编辑器 | ❌ 缺失 | ✅ workspace.edit 锚点替换 |
| TodoWrite | ❌ 缺失 | ✅ todo.write |
| 实际内置技能 | ❌ 缺失 | ✅ skillapp catalog 8 个模板（会议纪要/周报/表格分析/文档撰写/演示文稿/Go 审查/联网调研/翻译润色），走治理管线安装；embed manifests 的 hello.json 为根密钥装配前的占位签名 fail-closed 设计（非缺口） |
| 技能市场 | ❌ 缺失 | ✅ 技能中心「技能市场」tab（P3-2）+ C3 MCP 市场预置 |
| 技能触发机制 | ❌ 缺失 | ✅ C4 元数据注入 |
| 子 Agent 执行回路 | ❌ 缺失 | ✅ P1-1（此前）|
| 并行子 Agent | ❌ 缺失 | ✅ C5 本批次 |
| Hooks 系统 | ❌ 缺失 | ✅ P3-B |
| 上下文压缩 | 🟡 部分具备 | ✅ 核实已接入主链路（compaction_wiring.go / compaction_handlers.go，chat 内核消费） |
| 后台任务/长任务管理 | ❌ 缺失 | ⏸ 评估结论：command.run 分级超时（10s-300s）+ stdio 隔离 worker + 常驻自动化 scheduler 已覆盖安全面与定时面；完整后台任务管理（spawn 即返 + 轮询收割）需改 chat 主循环消息协议，列后续批次 |

### 回归（全绿）

- `go vet ./...` 0 告警；`go test ./...`（全量）全部 ok（storage/sqlite 71s、m8app 27s 正常水位）。
- `npm run build`（generate:bridge+typecheck+vite）成功；`npx vitest run` 41 文件 / 293 测试通过。
- 月亮动态效果增量：`npx tsc --noEmit` 通过；`npx vitest run src/session/companion` 7 文件 / 49 测试通过（含 a11y）。
- 并行 subagent 增量：`go test ./internal/app/ -run "Subagent|Parallel" -count=1` 全部通过。

### 遗留（非阻塞）

1. 后台任务管理（见上表评估结论），随编排器升级批次实施。
2. subagent.join 保持单次快照读（不做长轮询）。
3. embed manifests 签名装配待产品根密钥流程（ADR），当前 fail-closed 拒绝占位签名（测试固化）。

## 全量代码复核与 0.3.6 发布批次（2026-08-16 追加）

> 子代理静态复核 20+ 文件，发现 3 P1 / 3 P2 / 5 P3，全部处置（10 修复 + 1 列为性能优化方向）。基线与修复后回归全绿，版本升至 0.3.6 并打包。

### P1 修复（数据竞争/资源泄漏）

1. **mcp6 Registry 数据竞争**（registry.go Invoke/Probe 无锁读写 e.State/breakerUntil）：状态检查与降级写入全部移入 r.mu 临界区；Probe 返回 clone，杜绝 torn read 与断路器旁路。
2. **subagent 双重失败吞没 Complete 错误泄漏并发配额**（chat_subagent.go）：execErr+completeErr 并存时记录日志并显式返回，不再静默丢弃；quota 契约恢复。
3. **stdio MCP 会话忽略调用方超时**（mcpgateway.go）：mcpStdioSession 接受 ctx 并透传 StdioDial，过期 deadline 不再拉起注定 20s 超时的 worker。

### P2 修复（安全契约/资源浪费/可用性）

4. **subagent 只读防御不完整**：工具检查从单点黑名单（仅 workspace.write）改为 subagentAllowedTools 白名单（与 readOnlyEngineToolDefinitions 精确同步），幻觉工具调用在 FullAccess runtime 之前被拒绝。
5. **并行 future 早退泄漏**：chat.go 主循环 startSubagentFutures 后注册 defer drain（select ch/op.Done()），duplicate-ID/send-fail 等早退路径不再遗留 spawn goroutine。
6. **6 步上限静默截断**：步数耗尽且无最终文本时，向流与持久化文本同时写入中文提示（同 400 回退模式），用户不再看到"完成但无回复"。

### P3 修复（健壮性/死代码）

7. SessionPage toolLabel fs.* 分支：复核初判为死代码，增量测试（SessionPage.runtime「keeps tool activity inside the task process」断言 fs.read→读取）证明为有效行为，保留不动（复核误报，回归固化）。
8. toolruntime ensureAudit 加 auditMu（sync.Mutex），惰性开库在并发调用下恰好打开一个 SQLite 句柄（覆盖全部 4 个调用点）。
9. wakeWord onend 重启加 120ms 延迟 + stopped 复查，消除 unmount 后短暂识别重启窗口。
10. MoonSphere levels 数组防御性 pad/truncate 到 MOON_RING_BINS，off-size 输入不再使 ring 渲染/均值失真。
11. （不修，列优化）stdio MCP 每调用重新 spawn worker——会话池/空闲复用列为性能优化方向，非缺陷。

### 回归（全绿）

- 修复前基线：go vet+test 全量 exit 0；web build + vitest 41/293 全绿。
- 修复后增量：go vet mcp6/app/toolruntime/engine 0 告警；go test mcp6/toolruntime ok；app Subagent|Parallel|Chat 套件 ok；web tsc + companion/SessionPage 增量 vitest 全绿。

### 版本与交付

- VERSION 0.3.5 → 0.3.6；release/Build-Release.ps1 -AllowUnsignedDevelopment 产出 Lunitide-Setup-0.3.6-x64.exe（未签名测试候选，非发布版）。
- 清理 release/out 旧版本目录与安装包，仅保留 0.3.6。

## 工具 400 兼容性修复批次（0.3.7，2026-08-16 追加）

> 用户实测 0.3.6 反馈：对话可用但工具全不可用，出现「当前模型拒绝了工具定义」降级提示。根因：部分 OpenAI 兼容服务商对带 tools 的请求返回 400（常见为拒绝非核心 JSON Schema 关键字，或模型本身不支持函数调用），而适配器收到 400 时直接丢弃响应体，既未尝试兼容重试，也未向用户暴露真实原因。

### 修复内容

1. **上游原因捕获**（gateway/common.go）：新增 boundedReason——读取错误响应体（≤4KiB），解包 OpenAI 风格 `{"error":{"message"}}`/`{"message"}}`，收敛为单行并截断 200 runes；statusErrorReason 把原因并入 Error.Message。
2. **净化 schema 一次性重试**（gateway/openai.go）：带 tools 的请求遇 400 时，先将全部工具 schema 递归净化（仅保留 type/title/description/properties/items/required/enum，剥离 additionalProperties/minimum/maximum/minLength/maxLength/maxItems/minItems/pattern/format 等），再重试一次；流式/非流式均生效（attempt-- 不消耗重试预算）。
3. **降级提示携带真实原因**（app/chat.go）：净化重试仍 400 才降级纯对话，提示新增「原因：<上游消息（≤160 runes）>」，指导用户换支持函数调用的模型或检查服务商参数要求。

### 测试（全绿）

- gateway 新增 4 测试：净化函数单测（剥离关键字+核心 schema 原样往返）、boundedReason 单测（解包/原始折叠/nil/200 runes 截断）、400→净化→成功链路（断言两次请求体：第一次含 additionalProperties、第二次已剥离且工具调用成功返回）、双重 400 返回带原因的 HTTP_400。
- app Stream/Chat/Fallback 套件回归通过；go vet+test 全量 exit 0。

### 版本与交付

- VERSION 0.3.6 → 0.3.7；重新打包 Lunitide-Setup-0.3.7-x64.exe，清理 0.3.6 产物。

## 工具名线名映射修复批次（0.3.8，2026-08-16 追加）

> 用户实测 0.3.7 反馈：原因终于暴露——`Invalid 'tools[0].function.name': string does not match pattern. Expected a string that matches the pattern '^[a-zA-Z0-9_-]+$'`。内部工具名（workspace.list / command.run / mcp_<端点>_<工具> 等）带点号，而 OpenAI 兼容与 Anthropic 协议的函数名规范均只允许 `[a-zA-Z0-9_-]`，导致整个 tools 数组被严格服务商拒绝。此前 0.3.7 的 schema 净化对此无效，因为问题在名字而非 schema。

### 修复内容（gateway 双适配器）

1. **线名映射设施**（common.go）：sanitizeToolName 将非 `[a-zA-Z0-9_-]` 字符映射为 `_`；buildWireNames 为每次请求构建双向映射（内部名↔线名），超长（OpenAI 64 / Anthropic 128 字符）或冲突时追加确定性 FNV 哈希后缀保证可逆；nil 映射安全（wire 仍确定性净化，original 恒等）。
2. **OpenAI 适配器全链路**（openai.go）：tools 定义、历史 assistant tool_calls 出口全部转为线名（workspace.list→workspace_list）；非流式 message.tool_calls 与流式 delta 组装后的工具名经 wn.original 映射回内部名——下游工具运行时、事件流、UI 全部继续看到内部名，零改动。
3. **Anthropic 适配器全链路**（anthropic.go）：tools.name、tool_use 块、非流式 content 解析、流式 content_block 组装同样双向映射。
4. 兼容保留：0.3.7 的 schema 净化 400 重试、原因捕获、降级提示原样保留，作为第二层防御。

### 测试（全绿）

- gateway 新增 4 测试：线名设施（净化/冲突去重/超长哈希截断/往返）、OpenAI 非流式往返（请求体断言线名且不泄漏点号名、响应 workspace_list 映射回 workspace.list）、OpenAI 流式分片往返（command_+run 两段 delta 拼接后映射回 command.run）、Anthropic 非流式往返（请求线名 + workspace_write 映射回 workspace.write）。
- 既有 12 个 gateway 测试 + app Chat/Stream/Tools 套件回归通过；go vet+test 全量 exit 0。

### 版本与交付

- VERSION 0.3.7 → 0.3.8；打包 Lunitide-Setup-0.3.8-x64.exe，清理 0.3.7 产物。
