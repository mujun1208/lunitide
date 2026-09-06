# 电脑控制、项目管理、资产与计划工作流审计

审计日期：2026-09-06。基线：`09978597dc026bfa40ab8d690d64f9bcaef476f3`，`v0.4.67`。本文件来自本次源码追踪与隔离探针，不把历史 PRD、代码注释中的“已完成”“安全”“原子”等表述当作验收结果。

## 1. 结论与证据边界

当前不是可以按 4.9/5 验收的状态。已经定位到会影响正常业务的断链：同一项目第二次更新失败、项目阶段可绕过门禁直接上线、工作计划首次创建根节点失败、失败计划被覆盖为完成、模板重试造成重复和文件内容变化。电脑控制还存在动作执行前取消/急停已经生效但动作仍执行、目标窗口黑名单可绕过、成功动作审计失败被吞掉的问题。

本次额外运行 **13 个隔离探针，13 个期望安全/正确行为的断言失败，证实对应缺陷**。这不是原有测试套件的通过率；探针是为复现遗漏边界新增的只读审计材料。产品代码没有修改。Go overlay 仅在测试进程中加入虚拟测试文件，数据库位于各测试 `t.TempDir()`，电脑控制使用 Fake Host，未移动鼠标、未发送键盘、未更改真实剪贴板、未执行模型生成命令、未访问生产数据库。

证据文件：

- `evidence/computer-projects-assets-overlay.json`：可重复执行的 Go overlay。
- `evidence/computer_projects_assets_probe_test.go.txt`：项目、模板、计划探针源码。
- `evidence/computer_control_probe_test.go.txt`：Fake Host 电脑控制探针源码。
- `evidence/computer-projects-assets-probe.log`：6 个项目/模板探针结果。
- `evidence/planning-probe.log`：3 个真实临时 SQLite 计划探针结果。
- `evidence/computer-control-probe.log`：4 个电脑控制探针结果。

复核命令（全部从仓库根目录执行）：

```text
go test -overlay docs/audits/2026-09-06-system-review/evidence/computer-projects-assets-overlay.json ./internal/app -run '^TestAudit(Project|Template|Planning)' -count=1 -v
go test -overlay docs/audits/2026-09-06-system-review/evidence/computer-projects-assets-overlay.json ./internal/ccapp -run '^TestAuditCc' -count=1 -v
```

原有 Go/前端全量测试与构建由总审计执行，不在本子审计重复。真实 Win32/UIA、ConPTY、WebView2、DPI/多显示器、受限账户、模型输出质量、网络与异常关机需要在隔离 Windows 测试机完成，未运行的不能计作通过。

## 2. 实际模块链路与已有优势

| 模块 | 已追踪到的实际链路 | 已有基础与优势 |
| --- | --- | --- |
| 电脑控制 | 聊天工具调用 → `toolruntime.runCcTool` → `ccapp.ExecuteTool` → `MapComputerAct`/过滤/风险/前台检查 → `runHost` → Windows Host → 截图/审计 | `computer.act` 复用 `cc.*`；有开关、时限授权、急停锁存、速率限制、风险审批、坐标与 frameId、UIA 命名目标与操作后截图；Host 可替换，适合无副作用测试。 |
| 模型命令 | `command.run`/`run_terminal_cmd` → `toolruntime.Runtime.execute` → 审批、allowlist、hardline → `exec.CommandContext` → 输出/产物 | 环境变量 allowlist、相同审批入口、超时、命令别名归一、会话级全磁盘确认；但资源管理与下一条链路不同。 |
| 持久命令任务 | bridge `command.review.request`/`command.start`/`command.cancel` → `agentrunapp` → 签名参数/授权/审批摘要 → `commandworker` → SQLite job/effect/event/audit | 事务、CAS、幂等、执行回执与结果未知状态；worker 有持续排空输出、上限、Windows Job Object、启动目录 guard；应作为统一执行适配的候选基础。 |
| 人工终端 | `TerminalPanel` → terminal bridge → `terminal_handlers` → `terminalruntime` → Windows ConPTY | 显式启动、会话数量限制、环境净化、Job Object、输出摘要审计；人工输入不等同于模型审批。界面的审批徽标来自聊天模式，需避免误导。 |
| 浏览器 | 独立浏览器 `browserapp.Manager` → WebView2；远程浏览器 `brapp` → CDP/LocalHost；抓取还有 `browser`/`networkpolicy` | 主窗口与浏览器 profile 分离；初始 URL gate、独立生命周期；`brapp` 有连接状态、导航限制；不同网络入口需要统一验收范围。 |
| 项目管理 | `ProjectPage`/`ProjectWorkbenchShell` → typed project bridge → `project_handlers` → `projectapp` → SQLite UoW | ULID、字段规范化、业务状态、创建幂等、版本锁、事务内审计；前端有错误显示、防双击、保留创建/修改重试 attempt。 |
| 交付物/阶段 | `DeliverablePanel` → deliverable bridge/附件 bridge → 交付物与附件存储；finalize 再调用 project.advanceStatus 与 stage.update | 交付物、模板引用、检查清单、冻结与阶段记录实际落盘；存在前端证据要求；然而最终推进没有一个服务器原子边界。 |
| 计划工作流 | `ProjectPlanPanel` → plan/node bridge → `planningapp`/SQLite；plan.run.* → `agentorchestration.Coordinator`/repository；workflow.* → `m7app.WorkflowService`/SQLite | 有计划、节点、协调运行、版本化阶段工作流、审计与重启协调恢复。**plan.run.start 目前仅改变协调状态，明确返回 executionStarted:false。** |
| 资产 | `AssetManagerPage` → template bridge → 暂存分片/模板 handler → `asset_templates` + template files → 项目交付物引用 | 类型校验、10 MiB 限制、ULID 文件引用、状态机、CAS、引用保护、模板变更审计；文件与元数据的持久事务尚未统一。 |

接线证据：`internal/bootstrap/wire.go:129`（协调恢复）、`:150`（workflow）、`:223`（CC）、`:425`/`:429`（toolruntime/CC）、`:504`/`:509`（独立 terminal 根目录与 runtime）。当前终端使用公共 `terminals` 目录，`projectId/sessionId` 用于启动合法性校验，没有映射成项目目录；若产品期待“当前项目终端”，需要明确修正。

## 3. 已复现的确定问题

以下 P1 表示应在发布承诺“稳定可用”前修复的核心业务/权限问题；P2 为正常边界的一致性和可维护性问题。没有把所有风险都夸大为远程安全漏洞。

### CPA-01 / P1：同一项目第二次同类修改事务失败

- 证据：`internal/projectapp/service.go:200`，审计 ID 是 `sha256(action + projectID)`；`internal/storage/sqlite/audit_chain.go:59` 用普通 `INSERT`，`audit_events.id` 唯一；`internal/storage/sqlite/uow.go:168` 更新与审计处于同一事务。
- 触发：正常创建项目，独立保存两次；第二次用新的幂等键、最新 version。也影响第二次 `project.advanceStatus`、关闭/重开后再次关闭等重复 action。
- 实测：第二次 update 报 `UNIQUE constraint failed: audit_events.id (1555)`，数据库仍为第一次保存的 summary/version=2。不是幂等重试冲突。
- 整改：审计事件每次独立成功 mutation 分配唯一 ID，幂等记录负责复用已完成结果；不要为每个“项目+动作”永久复用一个事件 ID。
- 验收：同一项目连续更新 100 次、顺序推进全部阶段、close/reopen 循环 20 次，无主键冲突，审计条数与真实成功 mutation 一致；重复原始请求不新增审计。

### CPA-02 / P1：项目幂等摘要缺少实际修改内容

- 证据：`internal/projectapp/service.go:185`、`:208`、`:211` 只绑定 action/id/version。闭包里的 summary、reason、phase 等不进入 digest。
- 触发：同 key、同项目、同 version，第二次更改字段或阶段。
- 实测：本应 `IDEMPOTENCY_CONFLICT` 的第二个请求回成功，返回第一次 summary。前端正常会更换 attempt，但服务端仍不满足幂等契约。
- 整改：传入规范化完整 mutation command，摘要绑定 actor/org/project/action/expectedVersion/业务 payload；不依靠闭包隐含请求内容。
- 验收：同载荷重放完全一致；任一业务字段、目标阶段或 reason 变化均拒绝为冲突；重放不再依赖当前版本。
- 同组静态问题：`projectReplayDTO`（`:143`、`:166`）漏掉 OrgID/SpaceID/StatusBeforeClose/ReopenReason，重放 DTO 与首次响应可能不同；需扩展重放完整性用例。

### CPA-03 / P1：项目阶段可以跳过发布与所有交付门禁

- 证据：`internal/app/project_handlers.go:239` 只检查可编辑；`:338`–`:343` 根据外部 `phase` 直接赋目标状态。`internal/domain/project/project_fsm.go` 的 `AdvanceTarget` 不接收当前状态，也不验证交付物/阶段证据。
- 实测：created 项目直接请求 `phase:8`，bridge 返回 `status:live`，没有发布、阶段记录或门禁证据。
- 正常路径另一问题：`web/src/project/DeliverablePanel.tsx:89`–`:99` 分别冻结交付物、推进项目、完成 stage，任一步失败会留下部分状态；没有一个服务器端整体提交。
- 整改：新增服务器端阶段完成用例，事务内校验当前阶段、前置状态、全部必需交付物及证据版本、权限、版本锁，再统一写冻结/阶段/项目/审计/幂等。旧 advanceStatus 委托该用例。
- 验收：任意跳步、回退、空证据、已关闭项目、旧 version 均拒绝且零写入；在每个落盘点故障注入后不出现“项目已上线、阶段未完成”状态。

### CPA-04 / P1：非末分片重试会改变模板文件

- 证据：`internal/app/template_staging.go:58` 接收 index，`:102` 直接追加而从未使用 index；`web/src/bridge/client.ts:1116` 自动重试 staging 请求。
- 实测：发送 index=0 的 `FIRST-`，重复一次同片，再发送 index=1 的 `LAST`，最终是 `FIRST-FIRST-LAST`。
- 整改：按 uploadId/index/offset/digest 持久确认；同片相同摘要重放返回已确认，异摘要冲突；拒绝乱序/缺片；最终比较 totalSize 与 sha256。前端仅重试服务端可重放的操作。
- 验收：每个分片在“已写入但 ACK 丢失”位置注入故障，所有重试仍字节级等于源文件；含末片、非末片、乱序、同 index 异内容用例。

### CPA-05 / P1：未完成模板暂存也可作为成品消费

- 证据：`internal/app/template_staging.go:117`–`:131` 仅凭 uploadId 路径读文件，没有检查 last/ready、总片数/长度/摘要；`:106` 忽略 ctx。
- 实测：仅写入 `last:false` 的 PARTIAL，consume 已返回 PARTIAL 成功。
- 整改：上传状态机 `receiving → ready → committing → committed/aborted/expired`；create 只引用 ready 状态；租约、总量上限、过期清理、重启恢复与关闭文件句柄明确落地。
- 验收：未完成/被取消/过期/不同所属用户或组织的 uploadId 不能消费；中断不会留下无限量临时文件或打开句柄。

### CPA-06 / P1：模板创建没有服务端幂等，自动重试会重复创建

- 证据：`internal/app/asset_handlers.go:94`–`:183` 不读 IdempotencyKey，每次生成新 fileRef 并新建模板；`web/src/bridge/client.ts:1117` 对 create 自动重试；`internal/app/engine.go:1198` 仅限制 key 长度，没有通用去重中间件。
- 实测：同 key/同 payload 经真实 handler 与内存 FileStorage/模板存储执行两次，生成两份文件、两个模板 ID。该探针验证 handler 缺少幂等；SQLite CreateAssetTemplate 也没有幂等参数/记录。
- 上传 ID 模式：首次成功会清理暂存，ACK 丢失后的重试则可能报“上传未完成或已过期”，界面看失败而数据已创建。
- 整改：资产创建统一 UoW 幂等记录；文件写入与数据库采用可恢复 prepare/commit 流程；成功回执与 uploadId→templateId 映射可重放；失败清理交由可重试 GC。
- 验收：base64/分片两种输入的超时重试各 100 次只得一个模板/文件；DB 失败、文件失败、进程崩溃都能对账，不静默吞掉删除失败。

### CPA-07 / P1：工作计划首次建根节点违反后端约束

- 证据：`web/src/project/ProjectPlanPanel.tsx:62` 发送 `sequence:0`；`internal/planningapp/service.go:132` 放行 0；`internal/domain/planning/plan.go:193` 与 `migrations/0012_planning.sql:26` 要求 `sequence > 0`。
- 实测：与 UI 相同的根节点载荷经真实 planningapp/SQLite 返回 `node sequence must be positive`。
- 影响：已有 plan 可能已经激活，但根节点创建失败，“从清单同步”依赖 nodeId，后续无法执行。
- 整改：统一 schema、TS 生成类型、domain、SQL 的正整数约束；服务器端提供 ensurePhasePlan 原子用例，创建 plan+root 后激活。用 projectId+stageId/phase 的稳定键绑定，不用 name.includes 或回退第一个 plan。
- 验收：全新项目首次进入每个阶段工作计划成功；重复挂载/切换不会重复创建或使用另一阶段计划；只读项目进入页面零写入。

### CPA-08 / P1：计划暂停后仍能启动节点，父子状态无原子约束

- 证据：`internal/planningapp/service.go:227`–`:257` 只检查节点转换/审批，未读 plan.status 或检查父节点完成；`internal/storage/sqlite/planning.go:114`/`:252` 的状态 UPDATE 没有 expected version/status 条件。
- 实测：持久 ready 节点所在 plan 已 paused，StartNode 仍成功变 running。
- 整改：plan/node 的状态转换用单一 UoW；启动时检查 plan active、依赖完成、审批绑定、expectedVersion；Pause/Cancel 还要定义在途动作如何停止及确认。当前 pending→ready 没有找到正式生产更新入口，需补全而不是由 UI 随意写状态。
- 验收：pause 与 start 竞态始终只有一种合法结果；暂停提交后不再发新动作；父依赖未完成无法启动；重复完成/失败的终态不能反复覆盖。

### CPA-09 / P1：失败计划可被另一个节点完成覆盖成 completed

- 证据：`internal/planningapp/service.go:371`–`:384` 仅判断 `IsTerminal()`，failed/cancelled 也算 terminal，没有检查 plan 当前状态/失败节点；`CompletePlan` 与自动 checkPlanCompletion 规则不同。
- 实测：两个 running 节点，A FailNode 令 plan failed，B CompleteNode 后 plan 变 completed，A 仍 failed。
- 整改：从节点集合计算统一聚合状态，failed/cancelled 的优先级及重试政策明确；节点转换、计划聚合、审计事务内更新；查询不能仅截取前 100 个节点来判全量完成。
- 验收：所有节点终态组合、完成与失败并发、超过 100 个节点、重试分支下的计划状态正确；带失败节点的计划不能展示全部完成。

### CPA-10 / P1：动作前取消或急停已经成功，电脑动作仍执行

- 证据：`internal/ccapp/service.go:434`/`:445` 只取一次设置，`:529` 执行无 context 的 runHost；`internal/ccapp/runhost.go:26` 不接受 context；`internal/ccapp/emergency.go:20`–`:43` 更新锁存并释放键，但没有取消在途动作。
- 实测一：读配置后、Host 实际写入前 cancel context，Fake ClipboardSet 仍执行一次、返回 nil。
- 实测二：同一位置 EmergencyStop 成功提交 `EmergencyStopped=true`，Fake ClipboardSet 仍执行一次、返回 nil。
- 整改：所有执行入口共享会话/运行 cancel token 和授权版本号；动作串行化/桌面操作租约；每个不可逆外部动作前校验权限与取消；Host 支持可中断等待、释放 modifier、有限超时。急停响应不能只表示配置已保存，应给出在途停止确认/无法中止的明确状态。
- 验收：配置读取后、焦点切换后、粘贴前、按键间、拖动中、窗口调用阻塞中注入取消；急停确认后不再有新增副作用；规定停止时延目标并在隔离 Windows 机测量，不承诺已完成的 OS 动作能撤销。

### CPA-11 / P1：目标窗口切换绕过进程黑名单

- 证据：`internal/ccapp/service.go:515` 检查旧前台；`:587` focusIfNamed 不复核新进程；`internal/ccapp/runhost.go:143`/`:151` 先切窗口后输入；`internal/ccapp/risk.go:147` 只对 window_action/app_quit 额外检查目标。
- 实测：Fake 当前 notepad，阻止列表含 powershell.exe，`cc.keyboard_type` 指定 window=PowerShell，实际写入次数为 1。探针刻意不截图，随后才返回截图错误，写入已经发生。
- 整改：所有面向 window/id/name 的动作解析并固定目标 HWND/进程身份，焦点切换后复查；未知/权限不足/目标丢失时拒绝；OS 输入前最后复查。与 CPA-10 使用同一个 DesktopActionContext。
- 验收：被阻止的窗口在初始前台、被命名切换、操作中抢焦点三种情况下均不能输入/粘贴/按键；ActiveWindow/ListWindows 查询失败时也拒绝高影响动作。

### CPA-12 / P1：成功的电脑动作可以没有审计记录

- 证据：`internal/ccapp/service.go:547` 无返回值地调用 writeAudit；`internal/ccapp/audit.go:66` 丢弃整个事务错误。注释说“拒绝后的 best effort”，实际成功执行也走同一路径。
- 实测：注入 AppendCcAudit 失败，Fake 写入 1 次，审计 0 条，ExecuteTool 返回成功。
- 整改：副作用前 durable intent/prepared，执行后回执；审计存储不可用时阻止新高影响动作，已经发生的动作落为“执行已发生/回执待对账”，不鼓励自动重复；审计保留明确 actor、session/run/action、审批摘要与授权版本。
- 验收：审计磁盘满、SQLite busy、请求超时、执行后崩溃都有可查询的 pending/outcome_unknown 或完成回执，不能无记录地报成功。

## 4. 其他源码确定问题与待验风险（未混入 13 个实测探针）

| 编号/级别 | 源码证据与确定行为 | 下一步/验收 |
| --- | --- | --- |
| CPA-13 / P1 | `toolruntime/runtime.go:581` 使用无界 bytes.Buffer，直到命令结束才截 64 KiB；流式 `:540` Scanner 单行上限 256 KiB 且未处理 Err，超长行停止排空。`command_job_windows.go:24`–`:53` Job Object 失败继续运行，且进程已启动后才绑定。 | 统一 bounded sink，截断后持续读；超时先杀完整树再等待管道关闭；worker 启动前挂 Job。用输出洪泛/超长行/孙进程/Job失败测试，禁止在用户机器用真实破坏性命令复现。 |
| CPA-14 / P2 | `terminalruntime/runtime.go:259`–`:298` output/exit 共用非阻塞队列，满时默认丢弃；累计输出达到 MaxOutputBytes 后永久静默，UI不获截断通知。terminal 关闭/Shutdown 在 Start 还没填充 s.p 时直接调用 s.p.close，有空值竞态。 | 终态专用可靠通道或可查询状态；输出明确 truncation/gap；生命周期枚举替代“map存在即ready”；Fake Platform deterministically 注入启动/关闭竞态。 |
| CPA-15 / P2 | `terminal_handlers.go:113` ownsTerminal 只检查整个 Engine map 是否有 ID，不绑定当前 renderer/session/owner；`TerminalPanel.tsx:46` 注册 window.resize 没有对应 remove。前端 bridge 在 start 响应后才注册流 listener，有早期输出/退出事件丢失窗口。 | owner/generation绑定；订阅握手或可重放 cursor；React start完成时检查仍挂载/仍属当前session；打开关闭与切项目 100 次不泄漏 listener/终端。 |
| CPA-16 / P1（组织隔离承诺成立时） | `project_handlers.go:56` 组织查询失败返回空串；`store_project.go:16` 空Org不筛选；项目 mutate/get 无Org条件；模板表/API无Org字段，只有“org-level”文件注释。 | 明确本机个人数据与组织数据分区；带AuthContext查询/写入；组织解析失败不能退回全量；两组织同机测试已知ID访问和切换中的在途写入。没有声称已复现远程越权。 |
| CPA-17 / P2 | `asset_storage.go:134` 固定 LIMIT100，无游标/total/hasMore，创建无100上限，旧资产超过100后无法从列表发现；`uow.go:125`项目容量统计包含archived，删除只改archived，累计创建100次后即使界面已清空也不能新增。 | 资产分页/搜索；项目容量明确统计活跃还是历史，保证删除后用户可按产品规则继续创建；101+资产与100次创建删除测试。 |
| CPA-18 / P2 | `asset_handlers.go:166`文件先写、元数据后写；失败清理/删除均吞错。`template_staging.go:30`状态懒初始化未同步，无过期时间/总上传数，清理依赖成功create。 | 文件存储与DB的恢复协议、GC、磁盘预算、TTL、启动对账、并发初始化；故障点测试无孤儿文件和无限占用。 |
| CPA-19 / 能力缺口 | `plan_run_handlers.go:40` 固定 executionStarted:false；`:74`只调用Coordinator.Start。`ProjectPlanPanel`从清单创建todo并回写状态，没有真正 worker 执行桥接。 | PRD不得把协调状态记账当作已经完成自动执行；选择暂时明确显示“计划记录”，或按下述任务接执行和结果验证。 |
| CPA-20 / P2 | `ProjectPlanPanel.tsx:51`找不到当前阶段plan时回退listed.items[0]；`:69`挂载就ensure/create/activate，未以readOnly阻止。项目切换/阶段切换中异步请求无generation guard。 | 稳定阶段主键；只读页禁止创建；响应只更新发起它的项目/阶段；延迟/乱序回包用例。 |
| CPA-21 / 待实机验证 | browserapp初始地址、WebView2导航、brapp/CDP、web.fetch各有不同URL/DNS约束；浏览器资源/重定向/DNS rebinding是否全覆盖不能只凭初始CheckURL认定安全。 | 隔离代理记录真实连接；私网域名解析、跳转、子资源、非标准端口、CDP关闭/重连与profile清理测试。 |
| CPA-22 / 待执行边界验证 | agent.run.cancel (`agentrunapp/service.go:373`) 只调用通用状态转换，子 command 有自己的 cancel map；尚未证明取消父Run会停止所属命令/子任务。 | 使用 Fake CommandRunner 确认父取消→所有child cancelled→无后续输出/落盘，重启由effect journal对账；UI若只取消会话流也必须同样验证。 |
| CPA-23 / 待文件系统验证 | `toolruntime/paths.go:123` mkdir父目录发生在canonical containment检查前；普通路径字符串检查与随后文件操作之间没有handle pin，junction/符号链接交换需要实测。 | 只在临时目录做reparse/junction竞态；拒绝路径应零副作用；统一复用成熟workspace handle guard，绝不直接对用户目录执行测试。 |

上述列表把可见代码行为与尚待实测的风险分开。特别是人工终端本身作为用户显式打开的本地 shell，不应简单定性成“任意命令漏洞”；问题在于界面审批模式、项目目录、所属会话与实际能力的关系没有统一。

## 5. 当前评分（每点满分 5）

评分锚点：5=明确契约下完整验收且有稳定性证据；4=主要链路闭合，少量低影响缺口；3=可运行但重要边界或恢复不完整；2=核心流程存在确定断链；1=主要仍为占位。以下是本子审计的工程质量评分，不是已经测得的生产可用率。综合为前五项等权平均；未完成实机验收不授予高分。

| 模块 | 功能链路 | 数据一致性 | 取消/恢复/稳定 | 权限与审计 | 复用/可维护/测试 | 综合/5 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 电脑控制（CC/UIA/截图） | 3.4 | 2.6 | 2.0 | 1.8 | 3.5 | **2.66** |
| 命令执行与人工终端 | 3.2 | 3.0 | 2.5 | 2.8 | 3.0 | **2.90** |
| 浏览器控制（有限源码证据） | 3.2 | 3.0 | 3.0 | 3.0 | 3.5 | **3.14** |
| 项目管理与阶段交付 | 2.0 | 2.0 | 2.5 | 2.0 | 3.2 | **2.34** |
| 计划/协调运行/工作流整合 | 1.8 | 2.0 | 2.0 | 2.5 | 3.0 | **2.26** |
| 资产管理/模板/文件引用 | 3.0 | 2.0 | 2.0 | 2.8 | 3.0 | **2.56** |

主要扣分来自可重复证实的异常，不是因为目录多或代码行数多。已有 domain/app/storage 分层、typed bridge、CAS、审计链、Fake Host 和 migration 测试值得保留，适合逐步补齐；不建议用全盘重写来掩盖当前缺陷。

## 6. 可执行升级 PRD 子任务

### 范围与禁止漂移原则

目标是修复现有模块闭环与一致性，不在整改中新增产品中心、模型供应商或无关交互。先定义契约与验收，再修改现有入口；保留已运行的 bridge 方法，通过内部委托迁移。每个任务必须交付错误码、幂等语义、权限边界、状态机、日志/指标、迁移/回滚和对应测试。不得只改 UI 禁用按钮当作修复后端门禁。

| 任务 | 优先级/依赖 | 具体交付 | 核心验收 |
| --- | --- | --- | --- |
| CPA-T01 项目审计ID与完整幂等 | P1，独立先行 | 修CPA-01/02；唯一审计事件、完整 command摘要、完整replay DTO；现存审计不改写。 | 两个project探针转绿；100次保存/20次关闭重开；冲突载荷必须拒绝。 |
| CPA-T02 后端阶段完成事务 | P1，依赖T01 | `CompleteProjectPhase` service；校验项目/阶段/交付物/证据/审批/版本并原子冻结推进；bridge向后兼容委托。 | CPA-03转绿；中途故障全部回滚；端到端走完每种项目类型所有阶段。 |
| CPA-T03 计划契约修正 | P1，独立先行 | sequence约束统一；稳定phasePlanKey；只读零写；创建plan/root/activate原子；每阶段隔离。 | 首次进入、重复挂载、只读、切阶段乱序回包测试。 |
| CPA-T04 计划状态内核 | P1，依赖T03 | plan/node版本CAS、pending→ready→running正规调度、父依赖检查、统一失败聚合、暂停/取消语义。 | CPA-08/09转绿；全部状态组合与并发竞态；>100节点。 |
| CPA-T05 模板上传与创建事务 | P1，独立先行 | 分片offset/index/hash，上传租约与状态机，创建幂等、ready校验、恢复记录、文件GC。 | CPA-04/05/06全部转绿；ACK丢失、取消、重启、磁盘满、DB失败；源/成品hash一致。 |
| CPA-T06 桌面动作上下文与急停 | P1，独立先行 | run/session取消、权限epoch、独占桌面租约、每动作前复核、context-aware Host等待与modifier释放。 | CPA-10转绿；隔离机急停/切焦/超时组合，定义并测停止P95/P99。 |
| CPA-T07 目标窗口安全检查 | P1，依赖T06 | 目标HWND/进程身份固定，切焦后复核、未知状态拒绝，统一type/paste/key/menu/window行为。 | CPA-11转绿；被阻止进程任何目标方式都无法输入。 |
| CPA-T08 电脑动作持久回执 | P1，依赖T06 | prepared→executed/failed/outcome_unknown；审批/权限/证据绑定；审计故障停止新高影响动作；后台对账。 | CPA-12转绿；执行前/后崩溃均可解释，不盲重放。 |
| CPA-T09 统一命令执行适配 | P1，依赖T06定义取消契约 | 从commandworker抽复用执行器，toolruntime接适配；统一输出、Job、deadline、进程树取消；保留人工终端独立交互。 | 输出洪泛内存有界，长行不中断排空，子孙进程无残留，Job失败拒绝启动。 |
| CPA-T10 终端生命周期与数据流 | P2，独立 | Start/Close原子状态，owner/generation，可靠终态，输出截断标志，订阅握手，前端清理。 | 100次打开/关闭/切项目、启动中取消、退出先于ACK、队列满。 |
| CPA-T11 组织/项目作用域 | P1，依赖统一AuthContext | 所有读写服务显式owner/org/project；本机个人数据明确单独域；组织解析错误不扩大范围；资产作用域迁移。 | 组织A/B已知ID访问、切换中并发写、撤权后回放；无跨域数据。 |
| CPA-T12 分页与容量 | P2，独立 | 模板/计划/节点分页+total/hasMore；业务运算查询完整集合；项目容量不由历史墓碑意外耗尽。 | 101资产、>100节点、累计100次创建删除；所有历史数据可发现。 |
| CPA-T13 计划执行接线（明确产品边界） | P2，依赖T04/T08/T09/T11 | 若继续承诺自动执行：把协调run绑定持久execution run/worker，完成由回执/验收证据驱动；否则保持“计划记录”状态并明确显示未执行。 | 点启动后确有执行或明确未启动；取消父任务停止子执行；重启不重复副作用。 |
| CPA-T14 浏览器与文件边界验证 | P1验证，必要修复 | 合并不同浏览器/HTTP入口的策略矩阵；私网/DNS/redirect/subresource、junction/reparse检查与handle guard。 | 网络抓包证明被拒请求未连接；被拒文件路径零写入；异常浏览器回收。 |

建议排期按依赖分四批：第一批T01/T03/T05/T06/T07处理明确断链；第二批T02/T04/T08补服务器事务/状态机；第三批T09/T10/T11/T12整合边界；第四批T13/T14及全场景实机验收。具体人日需团队根据熟悉度估算，不以未经确认的固定日期承诺完工。

### 4.9 分升级验收条件

- 当前13个故障探针全部转绿，新增的权限/取消/事务探针不能通过删除断言、改为跳过或放宽错误定义“解决”。
- 每种项目类型从创建到交付/上线/关闭/重开端到端可重复；首次使用、非空历史库、旧版本迁移均纳入。
- 模板上传重试不变字节、不重复资产；所有mutation相同幂等键相同payload回放一致，不同payload确定冲突。
- 暂停、撤权、取消、急停有可观测的效果状态；外部副作用与数据库事实可以对账，结果未知必须明确显示。
- Windows隔离机完成Win32/UIA/ConPTY/WebView2组合测试；多显示器/缩放/锁屏/权限不足/窗口关闭/断网/引擎重启/磁盘不足有验收记录。
- 无未关闭P0/P1；P2如果保留，需不影响核心路径且有明确产品限制与负责人。稳定性指标、样本数、观察时长、环境列明，不把“若干测试通过”表述成永不崩溃或100%可用。
- 评分按验收结果重打，每个维度至少4.9才能宣称达到目标。当前没有这些运行证据，整改计划本身不等于达标。

