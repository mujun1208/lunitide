# 扩展、MCP、技能与专家整改进度

基线：0997859 / v0.4.67。历史审计探针保留在 `../evidence/extensions/`，其中 PASS 仍代表基线缺陷复现；下面列出的正式测试断言修复后的正确行为。

## 第一批：B02 公共能力门闸、E01 MCP 生命周期

已实现：

- `internal/m8app/plugin_gate.go`：执行前读取已提交的安装状态及活动绑定。显式 disabled、uninstalled、quarantined 或活动绑定缺失均拒绝；数据库错误拒绝。尚未播种内置插件的旧环境沿用已有行为。
- `internal/toolruntime/execution_gate.go`、`runtime.go`：普通/流式/全盘/全盘流式共用实际执行门闸，终端别名先规范化再检查，早于审批、钩子及 IO。计划/委派直达 Runtime 同样受控。
- `internal/app/plugin_execution_gate.go`、`engine.go`、`chat_tool_defs.go`：两种服务注入顺序均接门闸，提供 `Engine.CheckCapability` 给其他实际能力入口复用。Bridge 及浏览器 MCP 入口已接线。
- `internal/app/mcp_lifecycle.go`、`mcp_bridge.go`、M7/M10 MCP handlers：生产设置及市场的探测器调用真实 gateway；未启用的新增配置只探测、不发布可调用工具。每次 Invoke 复核设置库的启停、撤销、隔离与配置目标；更新后旧代失效，运行时接新目标。
- `internal/m7app/mcpruntime.go`、`internal/mcapp/service.go`：停用、卸载、更新的服务方法在提交后使旧运行时失效，覆盖跳过 UI 的调用。Health 写入失败向上返回，写回时检查目标与状态未改变。
- `internal/domain/m7flow/mcpruntime.go`：允许健康观察在 ready/degraded 等同状态刷新，修正原来 ready→ready 非法而被忽略的问题；revoked 保持终态。
- `internal/mcp6/registry.go`、`lifecycle.go`：撤权取消本代在途调用，调用池回收钩子；新代不受旧调用回写影响；失败端点重复注册真实重试；返回深拷贝，调用者无法修改 pin/argv。目录握手失败不再显示 ready。
- `internal/mcp/stdio_pool.go`：容量满且连接忙时等待，等待尊重调用取消；不持全局锁等待某个忙端点；退役忙连接完成前继续计入容量，随后关闭；reaper 在持连接锁后读取状态，启动只生效一次。
- `cmd/engine/main.go`：注册撤权到 stdio 池回收的生产钩子。

正式回归：

- `internal/app/plugin_execution_gate_test.go`：真实 SQLite + 临时工作区，禁用后五入口均拒绝、重启用恢复、库失联拒绝、未播种兼容。
- `internal/app/mcp_lifecycle_test.go`：真实 SQLite + 假传输，禁用新增不发布；UI 卸载后持有旧工具拒绝；直接服务停用也拒绝；故障健康显示 degraded、恢复 ready 且重复健康持久化；更新目标替换并可正常调用。
- `internal/mcp6/lifecycle_test.go`：撤权取消在途调用且触发清池、同 ID 故障重连、快照和输入无法污染授权。
- `internal/mcp/stdio_pool_capacity_test.go`：max=1 忙时同键/异键请求超时取消，池和 dial 数均不超限；忙连接退役完整关闭。
- `internal/mcp6/describe_test.go`：原“目录失败仍 ready”断言改为失败必须 degraded 且不向模型发布。

验证命令与结果（2026-09-06，均退出码 0）：

```powershell
go test ./internal/app ./internal/m7app ./internal/mcapp ./internal/domain/m7flow -run 'TestMcpLifecycle|TestPluginExecutionGate|Mcp|MCP|McConnector' -count=1 -timeout 120s
go test ./internal/app ./internal/mcp6 ./internal/mcp -run 'TestMcpLifecycle|TestPluginExecutionGate|TestStdioPool|TestRevoke|TestRegister|TestDescribe' -count=1 -timeout 120s
go test -race ./internal/mcp6 ./internal/mcp -run 'TestStdioPool|TestRevoke|TestRegister|TestDescribe' -count=1 -timeout 120s
```

仍需后续验收/实现，不能据本批宣称全部完成：

- 根任务继续把 CheckCapability 接入 LLM、STT/TTS、自动记忆注入、agent-loop 和技能等绕过 Runtime 的真实入口。公共工具门闸检查新调用，不承诺撤回已经完成的副作用或取消所有已开始的普通插件操作；MCP 的生命周期取消已实现。
- E02 的认证引用、真实凭据租约、完整 schema pin 持久化/重启对比尚未本批修复。设置层保留兼容的配置摘要；只有实际 gateway 握手成功才判健康，不能将该配置摘要称作完整能力证明。
- 池硬上限适用于该池管理的工具调用会话；独立健康探测仍使用短生命周期会话，统一全引擎子进程预算属于后续资源治理。
- 未访问真实外网端点、凭据，未执行真实桌面动作；真实服务兼容性、长时稳定性和最终整库回归由根任务统一验收。

## 下一批

按根任务授权继续 E04 技能版本冲突与 E06 专家正文/版本/装备一致性；以错误不再被吞、实际已提交数据与界面版本一致为优先验收目标。

## 第二批：E04 技能 rev 与 E06 专家持久性

- 技能 DTO 现在返回真实数字 rev（初始为 0，semver 仍单独保留）。编辑/删除 schema 要求明确 expectedVersion 且允许 0；服务比较客户端 rev，不再以最新读到的版本替换。SQLite 删除将 rev、状态、分类清理、审计放在同一事务。
- SkillPage 编辑时冻结 id/rev，列表刷新不升级旧编辑的版本；删除携带所见 rev。缺失 rev 的旧 DTO 只读，不假设 0。内部目录升级与即时聊天 patch 使用读取所得 rev。
- 专家正文写入错误不再吞掉；不可变正文先完整写成，再提交 catalog/version/初始装备绑定。装备写失败回滚专家与版本。文件正文采用临时文件→同步→原子替换发布，同引用不同内容拒绝；内存 store 并发保护。
- 专家详情核对版本归属，拒绝另一专家的 versionId；校验正文 JSON 和 digest，缺失/损坏返回 EXPERT_BODY_UNAVAILABLE。元数据、版本及当前装备读取于同一事务。专家编辑也冻结打开时的版本 ID。
- 无新增数据库迁移。文件与数据库之间并非同一物理事务：DB 回滚可留下未被引用的完整正文，但不会留下指向写入失败正文的可见版本。历史版本的独立装备快照仍需后续模型，未宣称已完成。

新增回归：`internal/app/skill_revision_test.go`（真实 SQLite 的 DTO 0 版/A 写入/B 旧版编辑与删除冲突/无版本拒绝/当前版删除）；`internal/m8app/expert_consistency_test.go`（磁盘满创建零 catalog、更新不推进版本、装备写失败原子回滚、跨专家版本拒绝、损坏正文拒绝）；SkillPage 前端补编辑中刷新仍持原 rev 和旧 DTO 禁写测试。

定向验证（退出码 0）：

```powershell
go test ./internal/skillapp ./internal/app ./internal/storage/sqlite -run 'TestSkill|TestUpdateFields|TestUpdateSkill|TestDeleteSkill|TestEnsureComposeSkills' -count=1 -timeout 120s
go test ./internal/m8app ./internal/app -run 'TestExpert|TestCreatePersistsCatalog|TestSkillClientRevision' -count=1 -timeout 120s
# cwd = E:\Trae-Work-Projects\lunitide\web
npx --no-install vitest run src/skill/SkillPage.test.tsx src/expert/ExpertCenterPage.test.tsx
```

前端最初从仓库根启动，未读取 web 的测试环境配置，全部以 document is not defined 失败；更正工作目录为 web 后通过，这属于验证命令错误，不是产品回归缺陷。


## 第三批：E07/E08 当前版本检索、引用溯源和失败状态

- SQLite 的 FTS、短词 LIKE、降级 LIKE 和向量候选只接受同 documentId 的最新版本且状态 ready。新版本失败时默认不回退旧内容，历史行仍保留供历史功能使用。
- KB.Cite 现在核对当前 subject 的专家集合、真实 chunkId/documentId、当前版本与 ready 状态；定位信息由存储段落规范化生成，调用者篡改文档、专家、页码、版本、quote 或 revision 均拒绝。
- 解析/投影语义失败先提交 failed 文档和失败审计，再向调用者返回 KB_INDEX_FAILED。数据库写段落失败仍回滚，避免部分段落被提交；失败版本可使用当前 expectedVersion 重试相同内容。
- 本批不覆盖远程来源自动刷新、完整范围权限模型、独立索引 worker、所有失败类型的可恢复队列，也不将全文解析在事务内的问题视为已解决。

正式回归：`internal/m8app/kb_consistency_test.go` 覆盖 FTS/短词/向量不命中旧版、失败行与审计真实持久化及同内容重试、Search→Cite 正向链路与各字段伪造/旧版拒绝；原任意 citation 可通过的测试改为要求拒绝。

```powershell
go test ./internal/m8app ./internal/app ./internal/storage/sqlite -run 'KB|Kb|Cite' -count=1 -timeout 120s
```

结果退出码 0。全部使用隔离 SQLite 与内存假向量，不访问真实内容或外部服务。


## 第四批：E03 标准 SKILL.md 的真实导入链

生产接线已落地，不能再依赖前端声明的 hash、clean 或 eval：

- `internal/skillarchive/source.go`：仅接受公开 GitHub HTTPS 仓库根地址或固定提交的 tree 子目录，SHA 要求完整 40 位小写十六进制；使用既有 networkpolicy 的 DNS/IP 固定连接、重定向校验和下载预算。8 MiB 压缩、32 MiB 展开声明、4096 条目、48 KiB SKILL.md 均有界；最终下载地址必须是固定 codeload 目标。内存读取 ZIP，拒绝路径穿越、反斜线路径、链接、重复大小写路径及无效文本，绝不解压或执行仓库文件。
- 读取标准 SKILL.md 的 YAML name/description 和正文，支持 YAML 折叠/引用等正常标量；拒绝重复键与非字符串必要字段。从正文派生 prompt manifest，无需额外 lunitide manifest.json。使用已有模块缓存中的 yaml.v3 v3.0.1，提升为 go.mod 直接依赖，未联网安装。
- 许可证缺失保存 `unknown`；存在许可证文件则保存其内容摘要标识或仓库声明值，不虚构 MIT、不声称已完成许可证法律鉴定。其他仓库文件只计数并展示，未安装。
- `internal/m6app/skillimport_source.go`：每个新的检查、扫描、审批步骤重新读取固定提交，并比对已持久化 SHA256 与来源摘要。真实调用 `m8core.ScanInjection`，证据写明规则版本、扫描对象、归档摘要、`codeExecuted=false` 和静态覆盖范围；调用者伪造的 scanRefs/clean/evaluationId 不作为生产证据。扫描拒绝保留 inspected 状态并提交 blocked 审计。
- 每个界面步骤的多个状态迁移共用一次 CAS 事务；网络读取在事务外。相同来源的未完成导入可在重启后恢复，不依赖内存缓存。审批重新计算扫描报告并核对已提交记录，客户端 manifest 不能替换真实正文。
- `internal/storage/sqlite/skill_import_tx.go`：审批与当前 `skills` 库草稿写入在同一 M6 事务，初始 rev=0、仅 read_only、默认不启用；candidateId 同时为运行库 skillId。写技能失败时候选审批回滚。撤销在同事务停用运行库技能；`skill_storage.go` 阻止已撤销来源重新发布。
- `internal/app/m6_s5c_handlers.go`、`internal/bootstrap/wire.go`：生产导入服务实际注入来源读取器，Bridge 返回真实名称、描述、许可证、摘要、未导入文件数。六个 skill.import 源 schema 与生成 DTO 同步；新的最小 discover/submit 参数有正式正例，旧 negative example 已按新契约修正。
- `web/src/skill/SkillImportWizard.tsx`：删除全部 MOCK 参数，逐步展示真实结果；提交只发送候选与版本，审批明确导入草稿。界面写明支持范围、大小限制、静态检查局限和脚本不安装；失败不前进，不触发 onApproved，重开可恢复已提交状态。
- 同批补齐 `browser_automation.go` 的 admission 错误日志及 `chat_settings_tools.go` 的真实启用和错误返回，修复 Add 默认禁用却虚构 Enabled:true 的聊天安装旁路。

正式回归（均通过）：

- `internal/skillarchive/source_test.go`：标准 YAML/子目录/真实 hash、来源校验先于网络、重复键/路径穿越/软链/重复大小写文件/大小预算、截断及目标变化、未知许可证与脚本未导入。
- `internal/app/skill_import_source_test.go`：真实 SQLite + 真 ZIP 解析 + 隔离下载替身；Bridge 最小参数→真实报告→重启恢复→审批→运行库草稿→显式发布→Invoke/Execute 读到真实说明→撤销；伪造扫描证据无权威，归档漂移和危险指令拒绝，失败扫描审计落库，运行库写失败整笔回滚，撤销来源不能直接重新发布。
- `web/src/skill/SkillImportWizard.test.tsx`：生产最小载荷、真实摘要/未知许可证展示、只有批准成功才通知、扫描失败不审批、恢复等待审批步骤。

```powershell
npm run generate:bridge
go test ./internal/app ./internal/skillarchive ./internal/skillapp -run 'TestSkillSourceImport|TestSkillImport|TestSkillClientRevision|TestSkillArchive|TestStandardSkillArchive|TestUpdateFields' -count=1 -timeout 120s
go test ./internal/app ./internal/skillarchive ./internal/storage/sqlite ./internal/bootstrap -run 'TestSkillSourceImport|TestSkillImport|TestSkillClientRevision|TestSkillArchive|TestStandardSkillArchive' -count=1 -timeout 120s
# cwd = E:\Trae-Work-Projects\lunitide\web
npx --no-install vitest run src/skill/SkillImportWizard.test.tsx src/skill/SkillPage.test.tsx
```

后两套前端文件共 18 测试通过。新增审计断言最初误查 m7_audit_events，更正为实际 M6 的 audit_events 后通过；新增 rev 测试短暂缺 bridge import 已修复。没有访问真实 GitHub 或执行远程脚本。

仍未宣称完成：私有仓库认证、其他托管站、动态沙盒/恶意代码分析、引用文件与脚本安装、发布者签名验证、离线归档保留与源失效后的离线审批。标准 SKILL.md 正文导入已打通，这些扩展能力需独立验收。E02 的 MCP 认证租约与完整 schema pin 持久化仍未在本批解决；E05 技能执行/依赖的更广治理仍由主 PRD 跟踪。


## 交叉复核修正：E03 回执丢失恢复与编码预算

交叉审查确认：审批事务已提交而回执丢失时，原 expectedVersion 重试会报冲突，重新 discover 又拒绝已批准候选。本批修正并加入正式回归：

- inspect 的原版本 1→已提交 3、submit 的 3→6、approve 的 6→7 允许读取对应已提交步骤；依然校验候选来源摘要与扫描证据，审批重放还核对同一规范化 approval 内容及 candidateId 对应的 runtime 行。
- 已提交结果的确认不重新下载，保证提交成功后断网仍能确认；新的步骤仍需重新获取固定归档和实际扫描。重放不推进状态、不重复写技能、不重复审计。
- 同源 discover 可恢复已批准结果，前提是原 runtime 技能仍存在。技能已经被删除时返回 SKILL_IMPORT_RESULT_MISSING，不假称成功，不自动重建。批准后撤销再重放旧批准仍拒绝。
- Wizard 对同一步骤相同载荷复用 MutationAttempt，重新发现 approved 时显示“技能已导入，可在技能中心查看当前状态”，不误写“草稿”或再次审批。
- 来源摘要在 JSON 编码后再次核对 16 KiB 预算；例如大量 & 字符导致 JSON 转义扩张时，返回明确来源格式错误，避免撞 SQLite 约束后误报存储不可用。

新增/扩充 `TestSkillSourceImportLostAckReplaysCommittedStepsOffline`、`TestSkillSourceImportApprovedRediscoveryRequiresExistingRuntime` 与源预算测试；覆盖同载荷多次重放、断网重开、审计数量不增长、不同 approval/hash、撤权及删除后的负例。Wizard 增加同 attempt 重试与 approved 恢复两个测试。无 schema 变动，无需重新生成。

```powershell
go test ./internal/app ./internal/skillarchive -run 'TestSkillSourceImport|TestSkillArchive|TestStandardSkillArchive' -count=1 -timeout 120s
# cwd = E:\Trae-Work-Projects\lunitide\web
npx --no-install vitest run src/skill/SkillImportWizard.test.tsx src/skill/SkillPage.test.tsx
```

以上均退出码 0，前端共 20/20。2026-09-06 15:33 交回稳定源码，后续整套验证由根任务统一执行。
