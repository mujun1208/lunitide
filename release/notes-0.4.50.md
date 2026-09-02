# Lunitide 0.4.50

在 0.4.49 全模块安全收口的基础上，把两处"看得见但没锁死"的完整性/防重放缺口彻底收口（road-to-4.9 的 W3、W6），并给新链接上运行时闸门。功能面不变，只是审计证据更难被无声篡改、破坏性卸载不再能被伪造/重放。依赖漏洞扫描零可达。

## 修复

- **W3 审计日志防篡改哈希链**：0.4.49 的 `0110` 给 `audit_events` 加了 append-only / no-delete 触发器，但持有触发器之外原始写权限的人（或被替换/编辑过的库文件）仍可无声地拼接、丢行、重排。本包让每一行携带严格递增的 `seq`、上一行的 `event_hash`（`prev_hash`）和对规范化文档计算的 `event_hash`——复用 `m7_audit_events` 账本已在用的同一套 M7 哈希链内核。三个写入口（`audit_helper` / `uow` / `agent_runtime`）统一走 `appendAuditChained` 封链落地；迁移 `0112` 整表重建并逐字节重建两个触发器，schema 漂移守卫仍逐字匹配。迁移前的老行保留 NULL 链列、排在链前（不可追封），链从迁移后第一行开始。
- **W3 接线：审计链纳入生产晋级冻结**：新增的 `Store.VerifyAuditChain` 此前只在测试里被调用，运行时无人触发。本包把它接进既有的 `m7AuditGuard`——每次 `release.promote` 前同时校验 m7 发布账本链与通用 `audit_events` 链，任一被篡改都以 `M7-DR-001` 冻结晋级；读不动账本则退回可重试的 `STORAGE_UNAVAILABLE`。
- **W6 插件卸载服务端单次核销令牌**：`plugin.uninstall` 的确认令牌此前由前端用公开公式 `sha256("plugin.uninstall|"+installId)` 算出，后端只校验是不是合法 hex——可伪造、可重放，"确认"形同虚设。本包新增引擎侧 `plugin.confirmToken`：签发 256-bit 随机、绑定单个 `installId`、2 分钟过期、**首次使用即销毁**的 nonce（进程内 `pluginConfirmVault`，零依赖、无 schema 迁移），`plugin.uninstall` 先核销再执行，伪造/过期/重放一律 `PLUGIN_CONFIRM_REJECTED`。桥产物已重生，前端 `PluginPage`、能力包卸载改为临卸载前实时取服务端令牌。

## 不要指望本包做的

- 不动默认执行模式与 0.4.49 已收口的 7 个 P0 行为；本包只在其之上补完整性/防重放两处
- 不重写 chat.go / engine 上帝对象 / store 的体量（架构分层与 Go↔TS 共享组件库是后续项，不在本包）
- 不声称对答如流、火山 5.0、本地克隆上线；语音主尺不因本包变化

## 验证

- `go build ./...`、全量 `go test ./...`（0 失败）、`internal/app` 与 `cmd/engine` 定向复跑绿
- `golangci-lint run`（app + engine）0 issues；`govulncheck ./...` **0 个可达漏洞**（依赖模块中 2 个已知项，代码从不调用，不可利用）
- 前端 `tsc --noEmit` 通过、`vitest run` 168 files / 1285 tests 全绿；`generate-bridge` 无契约漂移
- 新增单测：`audit_chain_test.go`（跨写入口成链 + 字段篡改/删除检测）、`m8_plugin_confirm_test.go`（单次核销/伪造/错配/过期/唯一性 + 处理器校验）、`m7_audit_guard_test.go`（断链→M7-DR-001 / 不可读→可重试 / 完好→放行）
- 未做桌面 WebView 真机点选；本包为**测试签名占位（未签名开发候选，不可发布）**，正式签名材料到位后重签

## 安装包

- `release/out/Lunitide-Setup-0.4.50-x64.exe`（未签名开发候选）
- `release/out/SHA256SUMS.txt`
- 从 0.4.49 升级
