# Lunitide 0.4.59

航空机务（MRO）方向的一次深度改造整体落地：机务工作台、机务专家、可引用清单、非受控手册闸门、审计回放，配套「专家知识库」（PDF/Word/Excel/PPT 纯 Go 解析入库）与「关系型数据源中心」（PostgreSQL / MySQL 纯 Go 驱动）。本次额外把数据源接入做到**本机零配置**：只填账号密码，库名固定、不存在即自动创建；本机连接可读写、远程连接严格只读。安装即用，无需额外运行时。

## 1. 机务工作台（MRO Workbench）

- 独立的机务工作台页面：机型/尾号上下文写入 `sessions.metadata_json`（`mroContext`），对话与检索据此过滤。
- **机务专家卡**（航空机务专家）与阶段专家一致的挂载/切换体验。
- **引用门**：机务回答区分「建议（advisory）」与「未落地引用（ungrounded）」两类 chip，未грounded 不污染记忆。
- **可引用清单导出**：从已引用步骤一键生成 JSON 清单（`mro.checklist.build`），带无障碍下载。
- **非受控手册导入**：reference-only 手册需用户显式确认后才纳入，避免误当受控资料引用。
- **审计回放**：工作台内展示 `m7_audit_events`。
- **黄金样例**：`testdata/mro/golden_p0.json` + `mroapp/golden_test.go` 锁定检索接地与引用门核心行为。

## 2. 专家知识库（Expert Knowledge）

- 新增 `expert.knowledge.ingest`：把文档正式导入某专家的独立知识库，导入后对话只在该库检索。
- **纯 Go 文本抽取**（新增 `internal/doctext`）：DOCX / PPTX / XLSX（`xuri/excelize`）/ PDF（`ledongthuc/pdf`）全部 `CGO_ENABLED=0` 可编译；加 `maxBuildBytes` 内存护栏防御异常大/畸形文件。
- 前端「专家知识库」面板支持 PDF / Word / Excel / PPT / Markdown，展示解析预览与失败原因；文件名 → mediaType 兜底路由（`File.type` 为空也能正确识别）。
- **就绪闸门**：`KBService.DocumentsReady` + `ErrKBDocumentNotReady`，机务手册注册前校验文档确已入库且 `KBIndexReady`，杜绝引用尚未建索引的文档。

## 3. 关系型数据源中心（Datasource Center）

- **纯 Go 驱动**（`internal/datasourceapp/sqldriver.go`）：Postgres 走 `jackc/pgx/v5/stdlib`、MySQL 走 `go-sql-driver/mysql`，均 `CGO_ENABLED=0`。探测/查询按方言输出占位符（Postgres `$1,$2…`、MySQL `?`）。
- **凭据不落主库**：DSN（含密码）只存本机 `datasource-secrets.json`（`0600`，`FileSecrets`），列表/详情永不回显。
- **本机零配置接入**：表单只收账号 + 密码；主机默认 `127.0.0.1`、端口默认 5432/3306、库名固定为 `lunitide`。本机 SSL 默认关（Postgres `sslmode=prefer`、MySQL `tls=preferred` + `allowPublicKeyRetrieval=true`），勾选后升级为 `require` / `tls=true`。
- **不存在即建库**：`SQLProvisioner` 在探测前对**本机 host**执行 `CREATE DATABASE IF NOT EXISTS`（Postgres 先查 `pg_database`），远程 host 一律 no-op，绝不在客户服务器建库；库名先过白名单防注入。建库失败提示「请用有建库权限的账号（如 root）」。
- **本机可读写 / 远程只读**：`Service.Query` 按 `IsLocalDSN` 分流（面板与 AI 工具 `datasource.query` 同一收口）。远程连接 `ValidateReadOnlySQL` + 只读事务，写语句拒绝；本机连接走 `SQLWriteQuerier`（可写事务，行返回走 Query、其余走 Exec 返回 `rows_affected`）。`Browse` 始终只读。

## 验证

- Go：`go build ./...`（CGO=0）干净；`go vet ./...` 干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` **0 calls**；`go test ./...` **全绿**（覆盖率 50.2% ≥ 49% 闸）。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **185 files / 1387 tests** 全绿；`vite build` 成功。
- 打包契约：`release/Test-OmniExcluded.ps1` 通过（Setup 不夹带 Omni/Comni/MiniCPM-o 运行时）。

## 安装包

- `release/out/Lunitide-Setup-0.4.59-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.58 升级
