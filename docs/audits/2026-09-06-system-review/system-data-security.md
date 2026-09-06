# 系统、数据、同事聊天与工程门禁复核

基线：2026-09-06，v0.4.67，`09978597dc026bfa40ab8d690d64f9bcaef476f3`。本文件与三份专项笔记共同支撑主 PRD。此次只增加审计资料，没有修改产品实现、安装包或用户数据。

## 1. 架构事实与优势

生产链路为 React/TypeScript → 顶层来源校验的 WebView2 Bridge → Go Desktop Host → 带握手/PID 验证的 Named Pipe → Go Engine → SQLite/文件/外部适配器。Go 1.26.6，当前 Node 24.16.0（CI 用 Node 22，存在验证环境差异）。不是 Electron 应用。

- 核心数据使用 SQLite，`internal/storage/sqlite/store.go:67` 的 OpenSecure 接受受控目录 capability，单库连接数为 1（`:98`）；有 WAL、迁移校验、schema fingerprint、quick_check 与外键检查。
- 123 个 SQL 迁移；467 个 Bridge schema 文件；生产契约可以生成并检查漂移。契约有定义不等于按钮/执行/落库全部贯通。
- `internal/bootstrap/wire.go:108` 将身份私钥接入 DPAPI；模型凭据有 Host/lease/绑定机制。模型凭据与用户内容数据库的保护边界不同，不能宣称 SQLite 全库加密或抵抗同用户恶意程序。
- UoW、版本锁、幂等、审计/outbox、持久 agent run 和恢复扫描已有复用基础；具体模块仍存在绕开这些设施的写入。
- 桌面 watchdog 能区分进程死亡与 RPC 断链，并尝试重连；有页面错误边界和 WebView 恢复处理。它们不能替代业务写入健康检查，也不能证明永不闪退。
- `CreateBackup` 使用 VACUUM INTO、完整性验证、fsync；`RestoreBackup` 采用暂存和失败恢复，恢复后调用者必须重新打开 Store。

## 2. 已复现问题

### SYS-01 / P1：事务 panic 后连接没有回滚，后续写入持续失败

证据：`internal/storage/sqlite/uow.go:53-78` 手工 BEGIN IMMEDIATE，defer 仅在命名返回值 resultErr 非空时 ROLLBACK；callback panic 时该值仍是 nil。`internal/ipc/session.go:279` 有外围 recover，进程能够继续，但数据库连接已经残留事务。

隔离复现：临时 SQLite，事务 callback 注入 panic，外层 recover 后第二次普通事务返回 `cannot start a transaction within a transaction`。这是故障注入结果，不表示正常代码已经自然触发 panic。证据 `evidence/transaction_probe_test.go.txt`、`evidence/probe-tests.log`。

整改：只在 commit 成功后置 committed=true，其余退出路径无条件回滚；回滚失败丢弃连接并上报 degraded，不把毒化连接还回池。panic 记录脱敏关联 ID 后按边界传播/恢复。验收：callback panic、commit error、cancel、rollback error 后下一请求要么正常执行，要么返回明确可恢复故障，绝不“引擎 ready 但所有修改永久失败”。

### SYS-02 / P1：PostgreSQL 实际连接地址与本地写权限判定不一致

证据：`internal/datasourceapp/sqldriver.go:240-257` 用 net/url.Hostname 判本地，实际连接由 pgx 解析；`service.go:349-366` 据此绕过只读检查并切到 writeQuerier；`bootstrap/wire.go:248` 生产确实注入写执行器。

隔离复现：只解析 `postgres://audit@127.0.0.1/audit?host=192.0.2.10&sslmode=disable`，pgx 有效 Host 是文档保留地址 192.0.2.10，IsLocalDSN 却返回 true。没有联网或写远程数据库。证据 `evidence/datasource_probe_test.go.txt`、`evidence/probe-tests.log`。

整改：只用实际驱动解析后的规范连接配置进行授权；校验所有 host/fallback/hostaddr/service 参数。把“是否本地”“是否允许写”“允许写哪个库/表”分开存储，不从 URL 字面值隐式授权。外部源默认只读、只读账号和只读事务共同约束；本机受管库也需显式写权限。验收包括 query host 覆盖、多 host/fallback、IPv6、连接代理和服务参数，拒绝的写操作调用计数为 0。

### SYS-03 / P1：数据库写工具跳过手动审批模式

证据：`internal/app/chat_tool_defs.go:65-67` 在进入通用审批 runtime 前，直接执行 datasource.query；`internal/app/datasource_query.go:18-41` 不接收审批/actor；`datasource_handlers.go:145-167` 同样无写幂等与业务审计。只读查询与可写 SQL 共用一个工具名。

隔离复现：真实 Engine 分发+数据源 Service+临时 SQLite，外部写执行器替换为计数器；executionModeApproval 下 UPDATE 被执行一次、err=nil。未连接数据库服务器。证据 `evidence/datasource_approval_probe_test.go.txt`、`evidence/datasource-approval-probe.log`。

整改：拆成只读 Query 与显式 Mutate command，后者必须有当前主体、目标库/表/语句摘要、授权、幂等与持久审计；语音和无人值守同事入口不能因工具名字为 query 就绕开写策略。验收覆盖每种执行模式和入口，审批拒绝/撤销/过期、同 key 异 SQL、执行后 ACK 丢失均不重复写。

### SYS-04 / P1：同事消息没有绑定认证发送者与群成员范围

证据：`internal/people/p2p.go:487-511` 直接复制 wire Message 的 SenderID；`:613-615` 对已存在 group 只按 ID 取群，不检查 from 是否成员。底层 `internal/storage/sqlite/people.go:181-188` 接受传入 sender。握手认证真实存在，但没有补上消息级主体约束。

两个无网络、无真实联系人探针：一个已信任 peer 的帧冒充本机 sender 被存储；一个已信任但非群成员的 peer 向已知 group ID 注入消息被存储。Fake Store 只替代持久接口，不替代真实 handleFrame/receiveMessage/ensureRemoteThread 判定。证据 `evidence/people_probe_test.go.txt`、`evidence/people-probe-tests.log`。

整改：sender 必须来自认证连接；若协议支持转发，显式保留 originalSender 与验签证据，不接受任意覆盖。每条群消息、已读回执、文件帧必须检查当前群成员/被屏蔽/撤权状态；文件分片绑定 sender+connection+offer。验收：正常配对不退化，冒充/非成员/撤权后/旧连接/跨 offer 等矩阵零越界写入。

## 3. 代码确认的系统缺口

| ID | 发现、范围与证据 | 落地要求 |
|---|---|---|
| SYS-05 / P1 | 同事 Send 先本地落库，再启动 goroutine 投递，`service.go:301-305`；`p2p.go:339` 丢弃 push 错误；Message DTO 没有 delivery 状态。远端离线/ACK 失败不形成可靠重试闭环。 | sender outbox、接收持久 ACK、每收件人 queued/sent/delivered/failed；同 messageId 去重，重启恢复，手动重试。已读与送达分开。 |
| SYS-06 / P1 | 本次 npm audit 命中 9 个受影响包条目：2 high、7 moderate。Mermaid 用于模型 Markdown 自动绘图，锁文件版本命中 DoS 公告；`MermaidBlock.tsx:49-50` 在页面渲染。 | 升级并锁定安全依赖；CI 加 npm audit/公告审查；限制图形复杂度、隔离解析/渲染和设置中断预算；不能靠 try/catch 阻止同线程无限循环。 |
| SYS-07 / P2 | `engine.go:1237-1241` system.health 固定返回 ready；`cmd/desktop/watchdog.go:44-78` 监视进程/RPC，不监测业务写健康。 | 保留 liveness；新增按能力的 readiness，包括 DB、磁盘、任务队列、设备、MCP、provider 上次真实探测。不得每次健康检查都触发付费推理。 |
| SYS-08 / P2（复核更正） | 可见备份/恢复闭环仍未验证。原文关于 Store.VerifyAuditChain 未接线的判断错误：基线 bootstrap/wire.go 已调用 SetAuditChainVerifier(store)，app/m7_update_handlers.go 的 m7AuditGuard 同时验证 M7 与通用审计链，发布晋级受保护。 | 保留既有发布保护；补可见备份/恢复及启动/定期自检，不重复开发现有接线。备份覆盖 DB+引用文件+录音+索引清单；DPAPI 跨机恢复需重录凭据；定期检查带 checkpoint。 |
| SYS-09 / P2 | 数据源状态文案 `DataSourcePanel.tsx:119` 对所有已探测连接标“只读”，与同页面本地可写事实冲突；readonlyVerified 只证明某次 SELECT 1。 | 显示实际 read/write policy、范围、探测时间、连接状态；区分连接健康与当前业务数据新鲜度。 |
| SYS-10 / P2 | 历史验收文档 P0-P1-status 仍引用 0.3.0、32 个前端测试，README 部分 Electron 迁移表述与移除历史并存。release-candidate 的 quality 不等同主 quality（未统一 lint/扫描/coverage 规则）。 | 一份版本化发布证据索引、同一可复用 CI；代码/安装包/签名/测试/版本 tag 绑定 commit，旧记录明确历史。 |

依赖结论来自本次锁文件扫描，不等于 9 个漏洞均已证明可以攻击本应用。高风险传递依赖需要做可达性分析。Go govulncheck 的退出码为 0，含模块级公告记录，不得写成“所有 Go 依赖都没有漏洞”；未报告可达受影响符号。

已核对上游原始资料：[Mermaid XY 图无限循环公告](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-2v8p-3f2j-5mp7)、[Mermaid 状态图注入公告](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-ghcm-xqfw-q4vr)、[pgx 连接解析文档](https://pkg.go.dev/github.com/jackc/pgx/v5/pgconn#ParseConfig)。XY 图问题的 11.x 修复版本为 11.16.1；升级时仍应重新核对当时全部公告，而不是仅满足一个版本下限。

## 4. 覆盖范围较浅的其他模块

自动化/组织/MRO/消息连接器/个人记忆纳入路由、存储与全量原有测试复核；未逐一增加故障探针或用外部服务逐按钮验收。相关路径为 `web/src/automation`、`internal/m8app/automation.go`、`internal/scheduler`、`internal/org`/`m9app`、`internal/mroapp`、`internal/imapp`、`internal/memoryapp`/`m8app/memory*`。这些模块的现状分是较低置信工程评价，不能以未找到问题表示没有问题。

- 组织切换必须分别验证资源身份、项目列表和已知 ID 的读写；个人本机数据与组织数据的边界不能混用，参见 CPA-16。
- 调度任务需要覆盖重启补跑、重复 tick、时区/DST、停用与在途任务；外部消息真实发送与收到/已投递回执使用专用测试身份。
- 个人记忆、专家 KB、MCP Memory 是三种不同存储，不应通过名称相似宣称已经同步；查新要定义来源与当前有效版本。
- MRO 相关手册/库存/工卡关联只评价软件状态与来源完整性，本审计不是业务放行资质或航空安全认证。

## 5. 测试事实与限制

详细统计和每项跳过用例在 `evidence/test-summary.json`，完整原始日志留存。首个 Go 命令被 PowerShell 将未引用的 coverage 文件扩展名误拆为额外 `.out` 包；这是审计命令错误，已引用完整参数重跑并通过，不列为产品缺陷。

最终全量 Go 日志当时包含 1 个新增会议审计包、4 个缺陷复现测试。原有套件统计必须从结果中排除该包：**136 个包返回通过、2 个包返回无测试、3357 个测试/子测试通过（2648 个顶层测试）、22 项跳过、0 个原有失败**。包PASS不等于该包具有独立Test事件；初版“有测试包”的措辞已纠正。审计包已经加 `auditprobe` 标签，之后不会被默认套件收集。

Go 语句覆盖率 **51.0%**，SQLite 包 **35.1%**。前端 **202 个测试文件、1553 项通过**；Vitest JSON 的 421 suites 是 describe 层级数，不是 421 文件。Bridge、类型、Go vet、Go build、golangci-lint、前端生产构建均通过。构建仍有主入口 JS 约 1797 kB、gzip 540 kB 的包体积提示，这不是崩溃实测。

选择性 race：8 个包、315 个测试/子测试通过，7 项跳过；范围为 ipc、voice 及其子包、meetings、people、ccapp、mcp6、engineclient。它没有覆盖整库、真实 MCP stdio pool、Windows UIA/ConPTY 全生命周期或全部业务交错。已测 race 通过与逻辑竞态反例可以同时成立。

没有执行真实模型付费调用、真实麦克风/系统音频、用户桌面键鼠操作、真实同事发送、生产数据库连接、长时运行/断电/安装卸载和签名发布验收；22 个 Go skip 主要覆盖这些外部环境以及文件系统权限条件。不能称“全链路所有功能实机验收完成”。

## 6. 根审计探针重跑

```powershell
go test -overlay docs/audits/2026-09-06-system-review/evidence/probe-overlay.json -count=1 -run '^TestAudit' -timeout 2m ./internal/storage/sqlite ./internal/datasourceapp ./internal/people ./internal/app
```

这 5 项断言期待安全正确行为，目前应失败。整改后再将它们转为正式回归测试；不能为“全绿”删除断言或反转产品正确性要求。overlay 使用当前工作区绝对路径，跨机器重跑需按新根目录重建 Replace 映射。
