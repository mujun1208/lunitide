# 扩展能力与专家知识专项审计

审计日期：2026-09-06。基线：HEAD `0997859`，v0.4.67。范围：技能中心、能力包/插件中心、MCP、专家中心，以及关联知识库、记忆召回、撤权和状态一致性。本文以当前源代码、真实 SQLite 夹具和隔离复现为证据，没有把旧 PRD、代码注释、测试名字或历史验收报告当作实现已完成的证据。

只读检查产品实现；未修改产品代码、用户数据库或凭据，未安装或启动任何外部 MCP/插件，未运行付费模型调用。新增的缺陷复现通过 Go overlay 注入审计测试源码，测试数据位于各测试的临时目录；探针源码、overlay、完整运行日志与摘要清单已持久保存在 `evidence/extensions/`。全量测试由主审计任务负责，本专项没有重复运行全量套件。

## 1. 专项结论

当前已经有可用的技能模板、专家资料/版本/挂载、MCP stdio 传输和 SQLite 审计基础，不能评价为“全空壳”。但界面状态和实际执行权之间存在断点，尚不具备 4.9/5 的验收条件。

最先修复两项：**停用能力门闸后真实工具仍可执行**、**MCP 页面删除端点后同进程聊天仍能调用该端点**。两者均已用真实服务和临时 SQLite 复现。另有技能 GitHub 导入固定失败、专家正文落盘失败被忽略、知识检索仍召回被新版替代的旧内容、引用校验可直接接受不存在的材料、MCP schema pin 没有验证实际漂移等确定问题。

“满分”必须由验收门槛约束，不能承诺所有环境、所有模型、所有外部服务永不失败。合理目标是：失败有边界、有可见状态、可恢复；撤权立即生效；资料版本可追溯；已承诺入口真正可走通。

## 2. 真实链路与支持范围

下列路径均相对于仓库根目录 `E:\Trae-Work-Projects\lunitide`。

| 模块 | 界面/调用入口 | 实际后端和存储 | 已实现与实际边界 |
|---|---|---|---|
| 技能中心 | `web/src/skill/SkillPage.tsx` → `skillBridge` → `internal/app/skill_handlers.go` | `internal/skillapp/service.go` / `catalog.go` → `internal/storage/sqlite/skill_storage.go`、`skill_invocation_storage.go` → `skills` / `skill_invocations` | 捆绑模板安装、创建、状态管理、匹配、短期调用提案、执行前重验、持久化单次消费。执行器只接受白名单 builtin 入口；大部分目录技能返回工作约定，随后使用现有工具，并非运行任意外部技能代码。 |
| GitHub 技能导入 | `SkillImportWizard.tsx` → `skill.import.*` → `internal/app/m6_s5c_handlers.go` | `internal/m6app/skillimport.go` → `internal/storage/sqlite/m6_s8skill.go` → `m6_import_candidate`、`m6_skill` / version / dependency / install | 候选状态机和 CAS/审计已实现，但 UI 填假证据且无法完成；该旧表链与日常 `skills` 运行库也没有发现完整同步。 |
| 插件/能力包 | `web/src/plugin/PluginPage.tsx` / `capabilityPacks.ts` → `plugin.*` | `internal/app/m8_plugin_handlers.go` → `internal/m8app/plugin.go` / `harness_plugins.go` → `plugin_bundles` / `plugin_installs` / `plugin_capability_bindings` | 当前 UI 已把它如实标为捆绑能力包与内置开关。通用 resolver/prober/registrar/revoker 在生产未注入；默认 registrar 只生成绑定描述。不能据此宣称通用外部插件安装/执行/升级已实现。 |
| MCP 设置、删除与聊天 | `McpPage.tsx` → `mcp.add/list/toggle/health`，删除走 `mc.connector.uninstall` | 设置：`m7app.McpRuntimeService`；市场生命周期：`mcapp.Service`；数据库：`mcp_endpoint_settings` / `mcp_market_items`；运行：`internal/app/mcp_bridge.go` → `mcp6.Registry` → `cmd/engine/mcpgateway.go` → `internal/mcp` | 三个对象各自持有生命周期职责，只有部分转移被同步。stdio 真正调用 tools/list、tools/call，有超时、行大小限制、进程隔离和池；HTTPS 当前为专用 GET `/tools` / 调用封装，生产凭证为空，不等同于完整通用远程 MCP 客户端。 |
| 专家中心 | `ExpertCenterPage.tsx` / `ExpertDetailTabs.tsx` → `expert.*`；聊天从 `chat_turn_equipment.go` / `chat_expert_compose.go` 装配 | `m8app.ExpertService`、SQLite expert catalog/version/mounting、`session_expert_mounts` / `expert_skill_bindings`，正文为 `FilePersonaStore` | 六段人设校验、版本追加、版本冲突、项目阶段挂载上限、会话挂载、技能绑定、同引擎专家协作。专家是同引擎的人设/装备，不是独立进程/自治运行时。 |
| 专家知识与数据更新 | 专家面板目前走 `kb.upsertDocument`；另有 `expert.knowledge.ingest`；聊天 `chat_kb_inject.go` → `KBService.Search` | `m8app/kb*.go`、`internal/storage/sqlite/m8_kb.go`、KB 文档版本/块/FTS/可选向量 | 能解析正文、建块、FTS 检索、按专家 scope 检索、适用日期/机号筛选、生成引用。版本更新、失败持久化、引用归属和原生 UI 文件选择有缺口。 |
| 关联记忆召回 | `internal/app/chat_memory.go` | `memoryapp`、`m8app.MemoryService.RecallForInject`、SQLite `memory_fts` / `memory_fact_fts` | 确认候选过滤、敏感项过滤、项目/专家隔离、注入预算和召回 trace 已有实现。MCP Memory 是独立服务器图谱，预置文案已明确不是产品记忆中心。 |

## 3. 当前评分建议（每项满分 5）

尺度：1=关键入口或边界失效；2=有基本功能但主要链路有确定断点；3=正常路径基本可用，恢复与一致性有缺口；4=主要真实链路和故障测试有证据；4.9=本文验收门槛全部满足且有持续观察证据。分数是本次工程审计评价，不是测量到小数位的成功率。

| 模块 | 功能闭环 | 状态/数据一致性 | 权限与撤权 | 失败恢复/稳定性 | 可验证性/可维护性 | 模块建议分 |
|---|---:|---:|---:|---:|---:|---:|
| 技能中心 | 2.5 | 2.5 | 3.0 | 3.0 | 3.0 | **2.8** |
| 插件/能力包中心 | 2.0 | 1.5 | 1.0 | 2.0 | 2.5 | **1.8** |
| MCP | 2.5 | 1.5 | 1.5 | 2.5 | 3.0 | **2.2** |
| 专家中心 | 3.0 | 2.0 | 2.5 | 2.0 | 3.0 | **2.5** |
| 专家知识/资料查新 | 2.5 | 1.5 | 2.0 | 2.0 | 2.5 | **2.1** |

模块分为上列五维等权平均。它们不能替代全项目加权评分；不可用单元测试数量把撤权和真实性缺陷平均掉。产品记忆中心本次只查关联路径，没有对其整个模块独立给总分。

## 4. 确定问题、修复与验收

### EXT-01 / P0：内置能力“停用门闸”没有进入真实工具执行判定【已复现】

- 证据：`internal/bootstrap/wire.go:256` 仅构造插件服务；`internal/m8app/plugin.go:467` 的 Toggle 只改 install/bindings；`plugin.go:880` 的 CheckBinding 全仓没有生产调用点。实际聊天工具执行在 `internal/app/chat_tool_defs.go:61-81` 直接进入 toolruntime，未读取插件状态。
- 触发：在能力包页停用 workspace，然后继续调用 workspace.write。隔离测试按相同后端链路禁用门闸后，真实工具仍写出临时 `audit-proof.txt`。
- 影响：用户看到已停用但执行权仍在。此问题不表示底层 workspace/full-disk 授权完全失效，而是产品承诺的这道撤权门闸没有生效。
- 修复：用一个 CapabilityPolicy/CapabilityResolver 把 `pluginId → tool families` 映射和 active binding 校验接到每条执行路径；定义 UI 开关、聊天、专家、语音、子任务、计划运行的同一判定。绑定不可读时按拒绝处理，不能继续默认开放。
- 验收：分别关闭 workspace、shell、web、browser 后，从普通聊天/专家/语音/子任务/直接 bridge 发起对应动作均被明确拒绝，零副作用；重新开启后只恢复该能力。测试必须调用真实工具分发器，不能只断言数据库 state。

### EXT-02 / P0：MCP 页面删除后聊天运行时仍保留调用权【已复现】

- 证据：`web/src/mcp/McpPage.tsx:112-120` 删除走 mcBridge；`internal/app/m10_mc_handlers.go:161-181` 仅 `e.mcmarket.Uninstall`；`internal/mcapp/service.go:412-454` 只将数据库端点 disabled/revoked。对照 `internal/app/m7_mcp_handlers.go:183-210`，只有 mcp.toggle 调用 dropSettingsMcp；删除/更新链路没有同步运行时。
- 触发：mcp.add → mcp.toggle(true) → 实际 UI 对应 mc.connector.uninstall → 同进程 mcp6 invoke。
- 结果：真实 SQLite 中端点 revoked，但运行时工具仍成功下发。测试使用假传输，不启动外部程序。
- 影响：已删除工具在本次进程生命周期内仍可调用；更新也可能继续调用旧 URL/旧参数。重启排除 revoked 行不能补偿当前进程的撤权缺口。
- 修复：生命周期由一个服务负责。先将 revoke epoch/disabled 标志置为执行阻断，再取消在途授权/调用、驱逐进程池、持久化并发布状态；失败要保留可重试补偿记录。更新应撤销旧代连接后重新探测，新代 ready 才接流量。
- 验收：删除成功返回之后，旧工具快照、缓存 toolId、直接 invoke、排队调用均拒绝；进程池没有对应子进程；重启状态一致；更新后不能再访问旧目标。

### EXT-03 / P1：GitHub 技能导入第三步固定失败，第四步亦缺材料【已复现第三步】

- 证据：`web/src/skill/SkillImportWizard.tsx:6,23-26` 使用固定 a×64 摘要、MIT、github-import、`mock-scan-ref`、`clean`、`mock-eval`。`internal/app/m6_s5c_handlers.go:188-193` 要求 scanRefs 是 JSON 数组串、injectionScan 是 JSON 对象串，UI 当前值必被拒绝。approve 未传 manifest，而 `internal/m6app/skillimport.go:228-235` 必须 ParseManifest。
- 触发：正常填写 GitHub URL 与 commit，完成发现、检查，再点提交扫描。
- 影响：用户无法完成界面承诺的导入；候选表还写入虚构来源证据。修正字段格式不能等同于补完真实获取/哈希/许可证/扫描/审批。
- 修复：后端导入 job 获取被固定 commit 的内容、由后端计算哈希、读取许可证和 manifest，输出真实扫描记录与评估结果。界面只提交来源坐标与审批决定。统一进入日常 `skills` 运行库，避免导入成功但会话目录不可见。实现完成前该入口明确标为暂不可用，不再生成假证据。
- 验收：本地受控 HTTP/git 夹具导入成功且可 `skill.list → invoke`；错误 commit、哈希不符、缺许可证、损坏清单、恶意路径均拒绝；检查记录可从 artifact digest 追溯；扫描未跑不得标 clean。

### EXT-04 / P1：技能编辑/删除的客户端版本没有参与并发约束【代码确证】

- 证据：`web/src/skill/SkillPage.tsx:46,49` 固定 expectedVersion=1；`internal/skillapp/service.go:398-405` 明确忽略 expectedVersion，使用请求到达时刚读出的 Rev；`internal/app/skill_handlers.go:244-257` 删除只校验 expectedVersion≥1 后调用不带版本的 Delete。
- 触发：两个视图先后加载同一技能；A 保存之后，B 用旧内容保存/删除。
- 影响：服务内部的读写间 CAS 能捕获极窄竞争窗口，但不能捕获用户从旧页面提交的编辑，仍会覆盖 A 的修改。不是“第二次保存必报错”，而是旧客户端版本被忽略。
- 修复：DTO 返回 rev；UI 带加载时 rev；UPDATE/DELETE 直接比较客户端 rev；冲突返回新版本和可合并字段；审计记录 before/after digest。
- 验收：A/B 同读 r1，A 写成 r2 后，B 的旧编辑和旧删除均得到冲突，数据维持 r2；刷新后重试成功。

### EXT-05 / P1：MCP 健康、重连状态来自配置摘要，不是实际连接【代码确证】

- 证据：`internal/m7app/mcpruntime.go:86-87` LocalMcpProber 只 hash endpoint descriptor；生产 `internal/bootstrap/wire.go:176` 未替换；`internal/app/m7_mcp_handlers.go:213-235` 健康按钮只用该服务；`internal/app/mcp_bridge.go:37-78` 真实网关注册失败仅记日志；`web/src/mcp/McpPage.tsx:95,126,130` 吞掉 enable 失败。
- 触发：添加不存在的 stdio 包或不可达 HTTPS；或者真实注册失败后点重新连接。
- 影响：设置页面可以显示已连接，但聊天运行时 degraded/缺工具。当前健康按钮也不会重新 Probe degraded 的 mcp6 运行时；Register 发现已有非 revoked ID 时直接返回，不能替代真正重连。
- 修复：只有真实 initialize+tools/list 成功才 ready；设置视图展示该运行时快照及探测时间。重连必须驱动同一运行时；DB/运行时持久化失败不可吞掉（Health 中多个 `_ = TransactMcp` 亦应改正）。
- 验收：离线/缺 npx/404/超时/协议错误统一显示可解释失败；网络恢复后重连真实成功且工具列表出现；失败状态不会被纯配置 hash 洗成 ready。

### EXT-06 / P1：MCP schema pin 没有验证实时漂移【已复现】

- 证据：`internal/mcp6/registry.go:259-278` refreshToolbox 会替换实际 schema cache；`registry.go:500-505` Invoke 只检查已有 pin 是否存在/符合 64 字符格式，没有与现有 schema digest 比较；`cmd/engine/mcpgateway.go:116-161` 实际适配器直接下发，也不比较 schema。ServerIdentityDigest 也未用于真实服务身份验证。
- 触发：首次描述工具 schema A，后续 Probe 变成同名工具 schema B，再 Invoke。
- 结果：隔离测试仍成功下发 B 的参数，无漂移拒绝。
- 修复：对授权时的规范化 schema+描述/权限和服务身份计算快照；每次重新 describe 和重新连接比较；差异进入待审查状态。执行时携带授权代号，缓存旧快照不能绕过撤权。
- 验收：改参数、增工具、工具重命名、身份变更均按明确策略阻断/重新批准；未变更不会误报；重启保留授权 pin，不能用最新返回自动覆盖原授权。

### EXT-07 / P1：有凭证 MCP 配置没有贯通执行层【代码确证】

- 证据：`web/src/mcp/McpPage.tsx:31-48` 手动 JSON 解析只保留 command/args/url，不保留 env/auth；`internal/m7app/mcpruntime.go:131,186` EnvSecretRefs 仅做校验，McpEndpointConfig 无此字段；`cmd/engine/mcpgateway.go:37` extraEnv=nil；`mcpgateway.go:116,164-170` invoke 忽略 auth，mcpEmptyLease 永远传 nil。
- 触发：用户粘贴依赖 env token 的 mcpServers JSON，或连接需要 Authorization 的 HTTPS 服务。
- 影响：配置被接受但执行拿不到凭证，不能实现通用带认证 MCP。这里只确认凭证丢失链路，没有读取用户凭据。
- 修复：明确支持的认证方式；存储 secretref 与参数映射；执行时短租约解析并仅注入目标进程/请求，旋转后驱逐旧连接；日志/错误/工具结果脱敏。界面未知字段拒绝或明确提示，不静默丢弃。
- 验收：用本地假认证服务测试正确密钥、轮换、撤销、401、过期和错误映射；DB/日志/导出中无明文密钥；不支持的认证类型安装前就解释失败。

### EXT-08 / P1：能力包共享依赖引用、删除恢复和安装真值不完整【代码确证】

- 证据：`web/src/plugin/capabilityPacks.ts:155-167` 账本在 localStorage，写失败被吞；`:259-262` 复用 MCP 不记录引用，`:291-294` 复用 gate 不记录引用；`:350-376` 删除通过其他包“新增/打开过”的列表来判断占用；`:390-391` 即使停用失败也删除账本并返回 ok=true。启用失败在 `:274` 被吞。`mergedPackLedger:181-190` 不能从服务端恢复依赖清单。
- 触发：A 包新增 Playwright，B 包复用同一端点，卸载 A；或者删除时 bridge 断连；或者更换/清空 WebView 数据目录。
- 影响：A 删除会停用仍被 B 需要的端点；部分失败失去重试账本；页面显示已安装并不保证依赖 enabled/ready。预置依赖复用还会跳过已停用 MCP 的启用动作。
- 修复：包/依赖/引用计数/安装步骤全部由后端事务+可恢复 job 管理；记录“依赖”与“谁创建”两种不同关系；引入 pending/ready/partial_failed/removing/remove_failed。前端只展示后端真值；失败保留操作游标与补偿信息。
- 验收：A/B 共享同一 MCP 时删任一个均保留另一个功能；删最后引用才撤销包拥有的实例；用户预存实例保持用户控制；中途断网/重启后重试幂等；localStorage 清空不改变真实安装关系。

### EXT-09 / P1：专家正文落盘失败被吞掉，数据库与人设文件非原子【已复现】

- 证据：`internal/m8app/expert.go:120-123` 丢弃 PersonaStore.Put 错误；`:182-220` 创建先提交 DB 再落盘；`:604-682` 更新同样先推进版本指针再写正文；`:383-386` 找不到正文时返回 `{}`。Create 在 DB 提交后才写 skill bindings（`:221-228`），绑定失败可形成接口报错但专家已创建的半状态。
- 触发：磁盘写满/权限拒绝/文件写中断；或者技能绑定失败。
- 结果：模拟磁盘满 PersonaStore，Create 返回成功，Detail 正文为 `{}`。
- 修复：优先把六段正文作为不可变 blob 放入同一 SQLite 事务；若继续用文件，采用 staging+fsync+rename 后提交引用，维护补偿/孤儿回收。正文校验摘要不符或缺失时显示明确损坏，不退回空人设。技能绑定与 create 一起提交或采用显式可恢复创建状态。
- 验收：文件写失败不得返回成功/推进 currentVersion；任意故障点重启后引用正文可用且摘要一致；重复创建幂等；绑定失败不会留下不可见半专家。

### EXT-10 / P1：知识库新版替代后旧版仍进入默认检索【已复现】

- 证据：`internal/m8app/kb.go:177-179` 声明仅最新 ready 可搜索，但 `internal/storage/sqlite/m8_kb.go:101-107,172-207` 的 FTS 与向量查询仅 `index_state='ready'`，未限定当前版本。统计也按版本计文档（`:232`）。
- 触发：同一 documentId 先写 v1，再写 v2 删除旧指令，随后搜索只出现在 v1 的词。
- 结果：隔离 SQLite 测试仍返回 v1 原文。
- 影响：专家可能用失效资料回答，数据查新失败；新资料并不自动排除旧证据。入口重复导入还总生成新 documentId（`expert_kb_ingest.go:77,103`，面板 `ExpertKnowledgePanel.tsx:96`），同源材料容易并存。
- 修复：定义 document identity/source key、current_version_id 与 superseded/deleted/effective 状态；默认检索只用当前受控版本，历史查询显式指定 asOf/version。FTS、向量、缓存、引用校验和计数统一读同一可见性视图；导入更新用 source+hash 幂等映射。
- 验收：v2 替代后 v1 专有词在默认查询中零命中；历史模式可精确取 v1；删除/撤销/过期文档不命中；FTS 与向量结果一致；同源重复导入不增重复文档。

### EXT-11 / P1：引用校验没有验证材料存在与归属【已复现】

- 证据：`internal/m8app/kb_search.go:238-267`，locator 没有 chunkId 即直接返回成功（`:251-252`）；有 chunkId 只验证 quote 前缀，不校验 expertId/docId/version/collection。handler 只校验 ID 形状和非空字段。
- 触发：任意形状正确但不存在的 expertId/docId，locator=`{}`，quote 任意文本。
- 结果：Cite 返回成功。另一专家真实 chunk 也缺少归属核对。
- 影响：界面“引用校验成功”可能误导用户以为存在可信来源；专家隔离不能依靠传入的标签。
- 修复：服务端根据 chunkId 联表获取实际 expert scope、document、版本、hash 和可见状态；引用使用后端签发/可重算的证据键；客户端传入内容仅作为待核验文本。
- 验收：不存在/跨专家/跨文档/旧失效版本/改写 quote 均拒绝；合法引用回链到准确版本与正文位置；缺少 chunkId 不能视为已核验。

### EXT-12 / P1：知识解析失败返回 failed，但失败记录和审计被回滚【已复现】

- 证据：`internal/m8app/kb.go:272-291` 在事务中写 failed 行与审计后返回 ErrKBIndexFailed；`internal/storage/sqlite/m8_kb.go:19` 调用共享 Transact；`internal/storage/sqlite/agent_runtime.go:25-42` callback 非 nil 错误即 rollback。
- 触发：解析器报错、扫描 PDF 无正文、chunk 数超限等。
- 结果：Upsert 返回 failed+ErrKBIndexFailed，但 KnowledgeGet 文档数是 0，刚写的失败记录没有保存。
- 影响：用户无从查看具体失败条目/重试；“已隔离失败文档”注释和前端反馈与实际 DB 不一致。
- 修复：把可预期解析失败作为需要提交的业务结果，事务返回 nil，提交后再映射错误或返回 typed failed payload；存储错误继续 rollback。保留 original source/hash/reason/attempt/retry state。
- 验收：解析失败后列表中有可解释的 failed 文档、零 searchable chunks、有审计；重启后仍可重试；数据库失败不产生伪成功。

### EXT-13 / P1：MCP 连接池忙时突破配置上限【已复现】

- 证据：`internal/mcp/stdio_pool.go:82-116` 达到 max 后尝试驱逐空闲项；没有空闲项时仍创建新 entry，没有等待或拒绝。
- 触发：所有已有端点正在调用，同时请求新端点。
- 结果：隔离测试 max=1，第一连接忙，第二连接被接受，池长度=2。当前默认 max=8 因而不是硬性资源上限。
- 影响：高并发、多端点可超出预期子进程/内存额度。另 `:74-77` 在持有全局锁时等待单端点锁，会让其他端点排队；`:161-164` 在取得 entry 锁前读 entry.conn/lastUsed 有竞争风险，未用 race 工具验证，见第 5 节。
- 修复：硬容量 semaphore+有截止时间的等待队列；全局锁内仅查找条目，不等待某端点 I/O 锁；每条目的所有共享字段由同一锁保护；撤权能定向 cancel/evict。
- 验收：超过上限时只排队或明确 busy，活进程数≤配置；等待可取消；A 的长调用不阻塞 B 的已有连接；连接全部 busy 时关机/撤权可在预算内完成。

### EXT-14 / P2：通用外部插件生命周期接口存在，但生产适配器缺失【代码确证，范围问题】

- 证据：`internal/bootstrap/wire.go:256-257` 无 Resolver/Prober/Registrar/Revoker 注入；`internal/m8app/plugin.go:163-176` 默认 source 只能命中 bundleId；`:221-230` 默认注册只造 BindingSpec；`internal/app/chat_settings_tools.go:32,175` 却把 source 描述为 roster id 并直接传 Install，不能按此 ID 解析 bundle。
- 影响：技能/内置开关/组合清单可用不等于安装通用 npm/Cordis/TS 插件可用；聊天 plugin.install 提示中的 roster id 与服务解析规则不匹配。升级接口以已存在 bundle 为输入，不能宣称在线查新或自动升级下载完成。
- 修复：本轮 PRD 首先冻结产品范围为“能力包+已注册内置能力”，修正接口命名/参数和提示；通用可执行扩展另立 ADR、签名信任根、包哈希、隔离、依赖解析、升级回滚任务。不要为了拿分贸然开启当前没有治理的外部执行。
- 验收：UI/工具说明/bridge schema/后端 source 类型一致；每一种支持类型至少一条真实安装→调用→禁用→卸载链路；未支持类型提前明确拒绝。

### EXT-15 / P2：MCP 内置种子自动下载执行，版本未锁定【代码确证】

- 证据：`internal/bootstrap/wire.go:463-465` 启动时 seed 并 hydrate；`internal/app/mcp_bridge.go:124-157` 自动 RiskConfirmed=true；`internal/mcp6/presets.go:121-140` memory/sequentialthinking 使用 `npx -y <包名>`，未固定版本。stdlib 进程 Job Object/环境隔离已有实现，但不是包内容签名或版本固定。
- 影响：首次启动或缓存变化时依赖外部包分发，用户的稳定运行基线会随上游变化；“不会执行外部脚本”的能力包宽泛文案与安装 stdio server 后实际启动程序不一致。
- 修复：维护受评审 package+version+integrity 清单；安装过程明确显示外部程序来源、版本和执行权限；下载与启用分阶段，离线不拖慢主入口。内置等价能力不必自动安装同名外部 MCP。
- 验收：清缓存后仍解析到同一包内容；hash 不符拒绝；更新有差异和回滚；离线首次启动正常进入主界面，MCP 独立显示等待安装。

### EXT-16 / P2：资料读取输入内存没有前置上限【代码确证，未做破坏性压测】

- 证据：`internal/app/expert_kb_ingest.go:57`、`internal/app/kb_search_handlers.go:186`、`internal/m8app/kb_index.go:33` 在输入上直接 os.ReadFile；前端 `ExpertKnowledgePanel.tsx:90-92` 对整文件 arrayBuffer+digest。doctext 虽限制提取结果大小（`internal/doctext/doctext.go:27-34`），发生在整文件已进入内存后。
- 影响：大文件导入可能使 WebView/engine 显著增大内存；多个导入并发会放大。没有用用户文件或超大文件实际压垮系统。
- 修复：入口先 stat/媒体检查，按可配置文件上限拒绝；流式 hash、分片上传/解析、每任务内存/并发预算；文档解析移出 SQLite 写事务以免阻塞其他模块；超限报具体原因。
- 验收：边界大小±1、多个并发、取消、超大稀疏文件、压缩炸弹夹具均受控；主聊天/设置操作延迟在预算内；拒绝时零半成品。

## 5. 疑点与未验证项（不得当作实测故障）

1. **专家知识面板原生选文件接线**：`ExpertKnowledgePanel.tsx:82-86` 依赖标准 File 没有声明的 `.path`，没有 native picker/attachment token 回退；相关测试 `ExpertKnowledgePanel.test.tsx:84-90` 人工 defineProperty(path)，掩盖了真实浏览器 File 的行为。当前宿主为 WebView2，仓库已有 `desktop.files.pick` 能力但该面板未用。本专项未启动桌面实际点击文件选择，因此把“发布版此入口必失败”保留为高置信待实机复核，不冒充 GUI 实测。整改优先接统一原生选取/上传链路，再用不注入 path 的真实 File 测试。
2. **停用/归档专家是否仍在所有聊天入口装配**：项目 Mount 明确检查 enabled；但 `chat_turn_equipment.go:73-111` 读取 Detail 后取 name，没有 state 检查；`m8app/expert.go:504-549` ComposeSkillsForNames 列表未过滤 disabled/archived，且名称/意图匹配还能回退目录。需要 root 对普通聊天/同事聊天/自动意图/会议/语音逐入口复核，不能从项目挂载的正确行为推断所有入口正确。
3. **MCP 并发 race**：Registry.Register 在 map 公布对象后 `registry.go:360-370` 不持锁写 State；pool 的 reaper 读取 entry 字段早于 entry 锁。静态有不一致锁保护；本专项没有跑 race，不能报告“竞态已触发”或声称已经造成宕机。
4. **同源文档自动查新**：本轮只找到显式导入和版本 upsert，没有验证后台 watcher/远端资料更新时间校验、证据过期告警和批准替换闭环。现有 asOf/effectivity 筛选是已存元数据筛选，不等于主动去上游查新。
5. **记忆召回规模**：`m8app/memory_inject.go:89` 先取最多 200 个 confirmed candidates 后在内存中打分；FTS 结果只是辅助匹配，没有把第 201 条及以后候选按 hit id 读回。这会限制大库召回候选，尚未做 10 万条规模质量与耗时测试。应建有 200/201/1000/10 万条的相关性、隔离、延迟评估。
6. 本专项未测试在线目录可达率、任何付费 LLM、真实 npx/uvx 包、外部 OAuth、真实多设备同步、不同 Windows/WebView2 版本。外部目录名称与所谓“官方/成熟”没有联网验证，不能当作已认证供应链。

## 6. 已运行的针对性复现与证据解释

运行方式：用 Go `-overlay` 将审计目录中 4 个 `.go.txt` 探针映射到各包虚拟 `audit_review_repro_test.go`；产品源码与用户数据不变，常规 `go test ./...` 不会将 `.go.txt` 作为代码纳入。固定基线最后一次合并复跑覆盖 4 个包、9 个审计断言，全部复现，exit code=0。

在仓库根目录执行：

```powershell
go test -overlay E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-06-system-review/evidence/extensions/overlay.json ./internal/m8app ./internal/mcp6 ./internal/app ./internal/mcp -run TestAuditReview -count=1 -v -timeout 120s
```

持久证据：[复核说明](E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-06-system-review/evidence/extensions/README.md)、[完整复现日志](E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-06-system-review/evidence/extensions/reproduction.log)、[overlay 映射](E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-06-system-review/evidence/extensions/overlay.json)、[证据 SHA-256 清单](E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-06-system-review/evidence/extensions/sha256.json)。

| 测试（仅审计夹具） | 真正执行的部分 | 观察结果 |
|---|---|---|
| TestAuditReviewExpertBodyFailureReportsSuccess | 真实 ExpertService + 临时 SQLite；PersonaStore 注入磁盘满错误 | Create 成功；Detail.sixSection 为 `{}` |
| TestAuditReviewKBHistoricalVersionsRemainSearchable | 真实 KBService、SQLite、FTS；同 ID 两个版本 | 新版后仍找到旧版专有词及旧原文 |
| TestAuditReviewKBFailedIngestRecordRollsBack | 真实 KBService/SQLite；解析器注入失败 | 返回 failed/error，持久化文档数 0 |
| TestAuditReviewKBCiteAcceptsUnverifiableText | 真实 KBService；空库、不存在 ID | 任意 quote 和 locator={} 被接受 |
| TestAuditReviewSchemaDriftStillInvokes | 真实 mcp6.Registry；假 probe/describe/invoke | 同名 schema A→B 后工具仍下发 |
| TestAuditReviewMCPUninstallDoesNotRevokeChat | 真实 mcp.toggle/mc.connector.uninstall handlers、服务、SQLite、Registry；假外部端口 | SQLite revoked，聊天调用成功 |
| TestAuditReviewDisabledPluginStillExecutesWorkspace | 真实插件 seed/toggle、executeUserTool、toolruntime | disabled workspace 后仍写临时文件成功 |
| TestAuditReviewSkillWizardScanPayloadRejected | 真实 handleSkillImportSubmit，传界面原样 payload | BRIDGE_SCHEMA_INVALID |
| TestAuditReviewPoolExceedsCapacityWhenBusy | 真实 StdioPool；假无外部进程连接 | max=1 且已有连接忙时，第二连接获准，Len=2 |

以上 9 项审计断言均确认当前缺陷存在；测试输出 PASS **不是产品功能已验收通过**。其中 workspace 的初版夹具使用非法非 ULID session，被正确拒绝；改为合法 ULID 后才复现真实门闸缺陷。没有把该夹具失败计为产品缺陷。测试只覆盖表中条件，不能外推所有模块已全链路测试。

复现源码已存为 `evidence/extensions/app_audit_test.go.txt`、`m8_audit_test.go.txt`、`mcp_audit_test.go.txt`、`pool_audit_test.go.txt`，不依赖 `%TEMP%`。PRD 落地时应将这些“确认缺陷存在”的探针改为“要求安全行为”的正式回归断言；修复后本次审计探针应相应不再通过。

## 7. 可落地 PRD 工作包与依赖

### 阶段 A：先恢复撤权与状态可信（发布阻断项）

| 任务 | 范围/交付物 | 依赖 | 退出条件 |
|---|---|---|---|
| EX-A1 统一能力执行判定 | capabilityId/tool family/subject/session/revision；所有真实 dispatch 入口查同一判定；替换“只有卡片变灰”的开关 | 无 | EXT-01 回归通过；逐入口矩阵有实测证据 |
| EX-A2 统一 MCP 生命周期 | m7 settings、mc lifecycle、mcp6 registry 通过同一协调器；revoke epoch；定向池驱逐；可恢复 outbox | A1 的判定接口 | EXT-02/05/06 通过；删除成功后旧调用零下发 |
| EX-A3 关闭虚假完成路径 | GitHub 导入、MCP 连接、包安装/删除失败改为可解释状态；清除 mock 证据；产品支持矩阵与界面一致 | 无，可与 A1 并行 | UI 不会将未完成当成功；无生产 mock-scan/mock-eval |
| EX-A4 资源与取消预算 | 进程池硬上限、跨端点隔离排队、文档大小/并发上限、取消与关闭预算 | A2 | EXT-13/16 通过；过载可控、正常聊天保持可用 |

### 阶段 B：补齐数据一致性和知识证据

| 任务 | 范围/交付物 | 依赖 | 退出条件 |
|---|---|---|---|
| EX-B1 专家不可变正文一致提交 | 正文 blob、版本、绑定同一提交边界；文件方案需 staging/校验/补偿；启动损坏检测 | A3 | EXT-09 所有故障点通过 |
| EX-B2 资料当前版本与溯源 | source identity、current pointer、effective state、hash；统一 FTS/向量可见视图；严格 citation key | B1 可并行设计 | EXT-10/11 通过；旧版/跨专家引用零误接纳 |
| EX-B3 解析任务与重试 | queued/parsing/indexing/ready/failed/cancelled；失败记录单独提交；幂等导入；统一原生 picker/上传 | B2 的文档标识 | EXT-12、File.path 真机入口验证通过 |
| EX-B4 技能运行库与导入库统一 | 确认 canonical skills/version/rev；迁移旧 m6 skill 链；后端导入 job；真实扫描材料；UI rev CAS | A3 | EXT-03/04 通过；新旧技能均可调用；无丢字段迁移 |
| EX-B5 能力包服务端依赖图 | pack instance/desired deps/resolved deps/owner/reference/job attempts；安装与删除均可恢复 | A1/A2/B4 | EXT-08 共享依赖、失败重试、重启恢复通过 |

### 阶段 C：认证、查新、运行质量

| 任务 | 范围/交付物 | 依赖 | 退出条件 |
|---|---|---|---|
| EX-C1 MCP 认证与供应链 | 类型化 auth、secretref lease、包固定 version+integrity、升级差异/回滚 | A2 | EXT-07/15 通过；凭证未泄露、撤销即时 |
| EX-C2 资料查新与适用性 | source fetchedAt/checkedAt/contentHash/effectiveFrom/To/TTL；手工刷新及可选定时检查；新版待确认替换 | B2/B3 | 修改源资料后能发现变更；旧源不可达显示 stale；不得把新检查时间当新版本 |
| EX-C3 专家统一装配解析器 | 普通对话、同事、阶段、会议、语音都按 enabled/state/version/skills/MCP 一次解析并输出可见装备快照 | A1/B1 | 停用/归档专家不得被静默复活；版本和装备快照可追踪 |
| EX-C4 检索质量与可观测性 | 标注数据集、规模曲线、trace/证据/授权/版本关联；把模型质量与系统故障分开计量 | B2/B4/C3 | 召回覆盖和耗时达 agreed 基线；错误可定位、可重放 |

避免漂移规则：一个缺陷/工作包对应一个可独立回滚 PR；先明确 canonical 状态所有者和 schema，再改 UI；同一种动作不能再增加第四套 MCP/第二套能力包逻辑；迁移先备份、校验数量与 digest、跑干净库和旧库升级测试；旧入口通过适配器迁移，在所有调用者切换前保留兼容性；不以“更换模型/增加提示词”替代撤权、版本和事务修复。

## 8. 达到 4.9 的专项验收门槛

1. EXT-01/02 无任何开放路径；所有 P0/P1 已关闭，回归测试使用真实 bridge+service+临时 SQLite+实际执行计数，覆盖普通聊天/专家/语音/子任务/计划的适用路径。
2. 每个入口支持的状态转移都有成功、失败、重复、超时、取消、重启恢复证据。任何返回“成功”的安装/停用/删除/更新都与数据库和实际执行状态相符。
3. 所有知识回答可把引用回链到实际 expert/document/version/chunk/hash；默认检索不会命中已替换/撤销资料；失效、未知和缺失资料会明确标注。
4. 断网、磁盘满、SQLite busy、服务崩溃、WebView 重载、后台恢复、9+ MCP 并发、超大文档和损坏包都能受控失败且保留重试信息。资源上限是硬约束，不是注释。
5. 集成环境至少跑完整场景矩阵和 72 小时 soak；记录观察的任务数、崩溃数、失败分类、恢复耗时与资源峰值。将“0 个观测崩溃”写成观察结果，不能据此承诺绝对永不崩溃。
6. 4.9 是上述门槛通过后的复评分目标。现阶段未跑真实桌面/外部模型/长期 soak 的项目不能提前打 4.9；必须把未验证项列入发布前清单并补齐证据。

