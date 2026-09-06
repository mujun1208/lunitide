# 第二阶段：扩展、MCP、专家与知识库

本记录以实际产品接线和正式回归为验收依据；第一阶段历史审计探针保留，不能作为修复后的通过证据。真机、真实外部服务与凭据由用户验证。

## B02：通用能力在途撤权

已实现 `PluginService.AcquireCapability`：每项能力独立 epoch，数据库授权读与订阅之间发生撤权会重查；已提交安装、升级、启停、卸载及内置挂载改变 epoch，取消旧代调用。事务失败不取消，重新授权的调用不受旧代 release 影响。多能力组合任一撤权均取消。

生产入口：Engine 同步 Bridge、ToolRuntime 通用执行入口、对话工具入口、MCP Invoke 均获取同一个能力作用域。执行器忽略取消而迟交成功结果时，结果丢弃；已取消进度不继续发送。已完成的外部副作用无法回滚，取消能力取决于执行适配器；长生命周期 chat/talk/ASR/meeting 由对话代理补全。

正式回归：`internal/m8app/plugin_scope_test.go` 覆盖跨能力隔离、旧代/新代隔离、失败事务、授权读与订阅竞态；`internal/app/plugin_scope_test.go` 通过实际 Runtime/WebFetcher 与 chat hook 验证取消及晚到成功丢弃。

验证：`go test -race ./internal/m8app ./internal/app -run 'TestPluginCapabilityEpoch|TestPluginScope|TestPluginExecutionGate' -count=1 -timeout 120s` 通过；`mcp6/toolruntime` 相关生命周期定向用例通过。独立 MCP 在途撤权/晚到成功丢弃已补 `TestMcpScopeRevocationDropsLateSuccess`，race 通过；注册、健康检查也获取同一 scope。长生命周期生产接线由对话代理负责。

## E02：认证、身份/schema pin 与版本锁

已接生产 `cmd/engine/main.go → mcp_security.go → mcp6.Registry`；删除旧的空凭据网关适配器。认证使用真实 `secret.Service`，绑定端点 ID、协议和 HTTPS origin。每次握手/调用在 30 秒凭据租约内执行，HTTPS 请求附 Bearer；stdio 只接显式许可的凭据环境变量，拒绝覆盖 PATH/运行时注入变量，带凭据进程退出后才释放租约，不进入长驻池。

实际初始化身份和完整工具定义规范化后生成 pin。每次 stdio 调用在同一个连接读取工具清单并验证身份/schema，HTTPS GET 适配器验证 TLS 目标 URL 身份及实时清单。工具增加、删除、schema/描述变化均撤权；401 使用结构化 HTTP 状态识别并撤权，不按文本包含“401”猜测。状态持久化失败会向调用者报告，不作为成功。

新增迁移 `0131_mcp_endpoint_security.sql` 保存引用、pin、锁定启动参数和 CAS 版本；旧记录版本为真实 0。首次握手保存 pin，重启沿用；裸 npx/uvx 包在首次执行前解析为精确版本，后续无需再查询 latest；node 入口脚本按内容及参数计算摘要，在启动/调用前验证。Node 依赖树仍由该项目的锁文件管理，此入口摘要不声称覆盖全部依赖文件。

用户链路：MCP 清单的“凭据” → Host 原生目标确认 → 受保护目录持久操作记录 → Secret Store → 私有绑定/CAS → 取消旧连接。保存、轮换、撤销不会将凭据发给引擎或模型，也不在清单返回 secretRef。响应丢失重放同一个请求返回已提交状态，内容不同拒绝；原请求已被后续轮换替换时拒绝。后台恢复清理过期未绑定及已退休凭据，清理失败保留记录重试。密码输入提交后清空。

漂移恢复：“复核变更”读取当前工具集合/摘要 → 用户确认 → 后端重新连接比较预览摘要 → 原子保存新 pin 和审计 → 用户重新连接。预览后再次漂移、旧版本确认均拒绝；提交成功后 ACK 丢失允许按持久 pin 返回已提交结果，不重复授权。`mcp.credential.set`（Host）及 `mcp.security.review`（Engine）源 schema、注册表、生成器白名单和生成物已同步；生成器保留根对象与 oneOf 的交集，避免丢失公共字段。

正式回归与结果：

- `internal/mcp6/security_test.go`、`credential_lease_test.go`：认证代际、401、完整 pin、JSON 顺序等价、身份/新增工具漂移、租约期限/清零、跨端点借用拒绝、在途取消；通过。
- `cmd/engine/mcp_security_test.go`：本地 TLS 隔离服务上直接运行生产适配器，验证探测/列表/调用真实 Bearer、401 无执行、schema 漂移不触达 tools/call；通过。
- `internal/app/mcp_security_test.go`：真实 SQLite first-pin、轮换、持久隔离、重启、复核时再漂移、确认 ACK 重放、确认后重新 Invoke；通过。
- `internal/credentialsubmission/mcp_windows_test.go`：Host 拒绝无写入、ACK 恢复不重复绑定、同请求不同 secret 拒绝、轮换/撤销旧 key GC、孤立 key 恢复、日志无凭据；通过。
- `internal/mcp/launch_lock_test.go`、`node_lock_test.go`：精确版本锁不反复解析、非法源拒绝、本地文件内容变更识别，未执行测试文件；通过。
- `go test ./internal/mcp ./internal/mcp6 ./internal/credentialsubmission -count=1 -timeout 120s` 通过；app/MCP、Host 与 Registry 定向 race 通过。
- 新 UI 安全对话框 3 项与既有 MCP 页 12 项通过；全前端 TypeScript 检查通过。

边界：保留项目现有远程 HTTPS GET 工具协议，界面明确其支持范围；不能将它称为标准 Streamable HTTP MCP。外部厂商、原生确认窗口和真实第三方包启动的真机验证由用户负责。版本锁不是上游包签名或整棵 Node 依赖树的内容认证。

## 后续与验收口径

B02、E02、E05、E06 的已实现链路和正式回归见下文。E07/E08 已交由会议/知识代理独占续建，记录在 `phase2-knowledge.md`；不将尚未实施或真机待验事项写作完成。自动化通过不能代替厂商/真机验收，也不能直接声称达到 4.9 分。

## E05：服务端能力包引用图与可恢复装卸（2026-09-06 第二阶段）

新增 `0134_capability_pack_operations.sql` 与 `internal/capabilitypack/service.go`，将清单、操作版本、逐组件进度、资源归属及共享引用写入 SQLite。`plugin.pack.list/install/uninstall`、聊天 `plugin.create` 的非空组合清单和启动 wiring 共用服务端执行器。真实调用现有技能发布、MCP 认证/目录复核准入、插件门闸；任一步失败返回错误并留下可继续的状态，完成全部组件后才挂载卡片。装卸均按存储进度恢复，卸载失败不删除账本。历史卡片仅展示“依赖待核验”，不把旧 localStorage 当归属证据。

门闸及 MCP 停用在实际组件写事务内重查共享引用；手动接管的组件不会被后来卸载包关闭。技能保留；同名模板草稿仅在正文/入口/权限与目录一致且版本 CAS 成功后发布。UI 完全移除逐组件安装/卸载和 localStorage 授权，改为读取服务端版本，提供继续操作和复核修复；文案如实提示 MCP 会启动本地进程。

定向验收：`go test ./internal/app -run TestCapabilityPack -count=1 -timeout 90s` 已通过共享引用、重启恢复、装卸故障恢复、ACK 重放、不同清单拒绝、手动接管保留；随后增补“释放检查之后出现第二引用”的实际事务竞态用例并运行 race。前端 `npx vitest run src/plugin/capabilityPacks.test.ts src/plugin/PluginPage.test.tsx` 共 13 项通过。无真实 MCP 包进程或外网执行，后端 MCP 使用隔离目录/认证适配器。

## E06：专家版本装备与孤立正文回收（2026-09-06 第二阶段）

`0135_expert_equipment_snapshots.sql` 新增不可更新/删除的版本装备快照。创建、正文更新、装备修改以及内置目录补齐均在既有专家事务内保存对应快照；装备变化追加专家版本，保留旧项目挂载版本。`expert.skills.get` 可读指定版本，返回实际 `versionId/known`；写入必须提交 `expectedVersionId`。前端从已加载详情冻结版本，冲突时保留编辑内容并提示刷新。

升级前历史版本缺少装备证据时记录 `known=false`，不把升级时的当前装备伪装成旧版本内容；升级时的当前版本用实际绑定回填。详情返回 `boundSkillsKnown/equipmentVersionId`。新增 `expert_persona_gc.go`：正文发布和 GC 共用 SQLite writer 事务边界，读取每个候选的全部历史版本引用后，才删除超过 24 小时的孤立正文或临时文件。每批至多 256 条、上下文 5 秒、每分钟调度，流式遍历分片并记录遍历进度；退出等待维护协程，保留所有仍被历史版本引用的正文。

正式回归：专家全定向、存储装备、Bridge 写入版本、两项新增历史快照/真实文件 GC 用例通过。`go test -race ./internal/m8app ./internal/app -run 'TestExpertEquipmentSnapshots|TestExpertPersonaGC|TestExpertSkillsGetSet|TestMcpLifecycle|TestMcpSecurityDurable' -count=1 -timeout 120s` 通过（33/36 秒）；专家页面 15 项通过。

## S07 / E02 补充：连接器市场统一身份门闸与失败传播

复核 `mc.*` 已接共享 `settingsGatewayProber`，补齐其遗漏：市场安装保存环境凭据引用并执行相同名字/SecretRef 校验，更新事务推进安全版本并保留原身份 pin，旧在途调用立即失效；启动参数或身份变化需显式目录复核。`mcapp.reprobe` 不再吞探测/读取/持久化错误，不会把真实 prober 已提交的 quarantined 状态覆盖为 degraded/ready。错误映射明确区分认证失效、版本冲突、身份漂移。市场配置 DNS 检查受调用取消与 3 秒预算约束。

MCP 页移除安装后 toggle 失败的吞错路径，刷新实际端点并呈现连接错误。新增真实 SQLite + 安全 Registry 正式回归：市场安装确实持久化身份 pin；更换目标产生隔离及安全版本递增且旧工具不可执行；401 安装返回失败/degraded；探测后数据库读取失败不得返回旧 ready。隔离适配器不会建立真实 HTTP 或启动外部包进程。

## 记忆到期、身份与设置并发补充（2026-09-06）

`memoryapp` 缺失读/写依赖返回明确错误；到期采用 `expiresAt <= now`，只读依赖也能安全处理过期记录。实际 SQLite 列表和搜索在 LIMIT 前排除到期记录，并按 RFC3339Nano 的固定精度比较保留未来 1ns 的记录。清理每批 256 条、30 秒总预算、失败返回已提交计数和错误；审计与删除共享事务。兼容旧存储适配器无法证明超过 100 条已全部扫描时返回 incomplete，不再假报清理完成。

M8 显式确认在一个事务中绑定引擎可信 identity、候选与 token；前端不能靠他人 token 确认或拒绝。候选确认期限等号即到期，过期状态与审计成功提交后才对外返回过期错误。编辑正文不得改候选 scope。提名入口校验身份，列表实际 SQL 按身份过滤后限制条数，撤回事务内检查候选所有者。确认期限仅用于 pending 候选，未误用为已确认事实的保留期限。

`memory.settings.get/update` 返回完整持久 profile 的版本哈希；写入必须提交已加载的 expectedVersion，真实 SQLite 同事务校验并更新纳秒单调 updatedAt，包含时钟回退、相同值重写及 ABA。旧内部 Upsert 也经 CAS；identity legacy 重绑定使用解析后的时间并保证后继版本。前端读取失败禁止默认值覆盖，冲突保留草稿并显示“读取最新版本并保留草稿”，用户核对最新已保存内容后才可再次保存。响应直接来自已提交行，避免二次读取返回另一次写入。

验证：memoryapp/m8app/app/sqlite 的记忆、确认、提名、到期、清理、搜索正式定向通过；新增 700 条跨批清理/未来 1ns/他项目保留/删除失败回滚、身份与 scope 拒绝/精确过期持久化、设置并发仅一方成功/ABA/倒时钟/审计失败回滚/Bridge 真实版本与权限。前端 MemoryOpsPanel 16 项通过（含冲突草稿与加载失败禁写）。没有访问真实服务或用户记忆库。

## E02 最终补充：旧 mcp6.register 合入生产持久链

复核发现 bootstrap 的旧 m6 EndpointService 实际未接，旧 register 仅写内存；旧投影又缺 command/args。此次没有新建第三套生命周期：生产 `mcp6.register` 先持有真实 capability scope，通过 Registry.CheckEndpoint 做完整认证/pin/版本锁核验且不发布可调用对象，随后 `McpRuntimeService.ImportGatewayEndpoint` 在现有 M7 配置+安全表同事务写入完整目标、原始参数、实际锁定参数、pin、SecretRef、状态与审计，最后 Health 准入。已有设置/Host 批准的同目标凭据身份被复用，避免创造第二个不匹配的凭据身份；独立注册以不可变请求摘要生成稳定 ID。

生产工具执行找不到持久 grant 时拒绝；旧接口撤权先提交终态、再取消 Registry/池中在途 IO。旧请求不能复活停用/撤销/隔离记录。启动恢复统一调用实际 Health，因此离线恢复成功同步刷新 DB ready，身份漂移则持久 quarantine。数据库或审计失败时没有可调用 ghost。旧 M6 投影仅留给明确未接 M7 的嵌入式测试/兼容服务，不作为生产持久化证据。

正式回归测试：
- `TestLegacyMcpPersistsLockedDescriptorAndRevocationAcrossRestart`：完整 stdio descriptor/版本锁、重启调用、ACK 重放、在途取消、重启和旧请求不能复活撤销。
- `TestLegacyMcpPersistenceFailureHasNoCallableGhost`：注入真实事务审计失败，配置与安全记录回滚且 Registry 无可执行端点。
- `TestLegacyMcpConcurrentRegistrationCreatesOneGrant`：并发注册仅一个持久 grant。
- `TestLegacyMcpDegradedRecoveryAndRestartPinDrift`：离线注册失败保留 degraded，重启恢复 ready；再次改变服务身份后重启不能重新批准。
- `TestLegacyMcpUsesExistingApprovedCredentialIdentity`：旧入口复用真实 M7/Host 凭据身份，不创建重复端点。

最终验证已通过：`go test -race ./internal/app -run TestLegacyMcp -count=1 -timeout=120s`（59.031 秒）；`go test ./internal/m7app ./internal/mcp ./internal/mcp6 -count=1 -timeout=120s`（m7app 无独立测试，服务由 app 实际 SQLite 用例覆盖；mcp 3.669 秒、mcp6 0.456 秒）；`go test ./internal/app -run 'TestLegacyMcp|TestMcp6|TestMcpLifecycle|TestMcpSecurityDurable|TestCapabilityPack' -count=1 -timeout=120s`（2.845 秒）。输出见 [短 race](../evidence/phase2-legacy-mcp-race.log)、[连接包回归](../evidence/phase2-legacy-mcp-packages.log)、[受影响入口回归](../evidence/phase2-legacy-mcp-targeted.log)。测试全部使用隔离 SQLite 与假认证/工具适配器，没有启动第三方 MCP 包或连接真实服务；本次短 race 补充最终旧入口接线，不替代根任务的全库验收记录。

## E03/E04 第一阶段完成证据的对应位置

固定源SKILL.md导入/发布/实际调用与客户端所见rev更新/删除CAS在第一阶段已实现，本阶段沿用并通过最终整库回归复验。实现和正式测试入口见[extensions-progress.md](extensions-progress.md)的E03/E04章节；该文件其余未完成描述属于历史，后续状态以本文件和最终台账为准。当前导入范围为固定GitHub提交ZIP内的prompt正文，支持脚本不自动安装，不宣称通用任意代码技能运行环境。
