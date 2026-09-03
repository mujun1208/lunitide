# 新增关系引擎验收清单

上架一种引擎必须同时交付：

1. `db_connections.kind` CHECK 增加该值（新迁移，不改 0059 历史文件）。
2. 适配器：DSN 解析、标识符转义、`SELECT 1` 探测语句。
3. Browse SQL 只打 metadata namespace，并通过 `connector.StatementAllowed`。
4. 只读校验：复用 `ValidateReadOnlySQL`，补充该方言的写动词（若与标准 SQL 不同）。
5. 驱动许可与是否 CGO 的说明；月汐生产默认 `CGO_ENABLED=0`，需要 CGO 的引擎不得进默认构建。
6. 单测：探测成功/失败、浏览拒绝业务表、DSN 不落库。
7. UI kind 下拉增加一项。

P2 候选：`sqlserver`、`mariadb`、`sqlite-file`（只读附加文件）。P3 候选：`oracle`。

## 已交付实现

`postgres` 与 `mysql` 已上架，走纯 Go `database/sql` 驱动（`internal/datasourceapp/sqldriver.go`）：

- 驱动：`jackc/pgx/v5/stdlib`（Postgres）、`go-sql-driver/mysql`（MySQL），二者均 `CGO_ENABLED=0` 可编译。
- 探测与查询一律在 `sql.TxOptions{ReadOnly: true}` 只读事务内执行，`SELECT 1` 探活。
- Browse SQL 通过 `browseSQL` 生成，并按方言输出占位符：Postgres 用 `$1,$2…`，MySQL 用 `?`；`database/sql` 不做占位符改写，混用会在真库上报语法错误（见 `TestBrowseSQLUsesDialectPlaceholders`）。
- 已接入 `cmd/engine/main.go` 的 `SetPinger` / `SetQuerier` / `SetProvisioner`。

## 本机零配置接入（固定库名 + 自动建库）

面向"别人装完直接用"的场景，前端只收集账号 + 密码：

- 库名固定为 `FIXED_DATABASE = 'lunitide'`（`web/src/settings/datasourceDsn.ts`），表单不再出现库名字段；主机默认 `127.0.0.1`，端口默认 5432/3306。
- 本机 SSL 默认关闭：Postgres 用 `sslmode=prefer`（有 TLS 走 TLS，本机裸库回落明文），MySQL 用 `tls=preferred` 并带 `allowPublicKeyRetrieval=true`（MySQL 8 `caching_sha2_password` 首连非 TLS 需要）。勾选 SSL 则分别升为 `require` / `tls=true`。
- `SQLProvisioner`（`sqldriver.go`）在 `Probe` 探活之前运行，**仅对本机 host**（localhost/127.0.0.1/::1）执行 `CREATE DATABASE IF NOT EXISTS`（Postgres 先查 `pg_database` 再建，连 `postgres` 维护库）。远程 host 一律 no-op，绝不在客户服务器上建库。库名先过 `reSafeIdent` 白名单再拼接（标识符无法参数化）。
- 建库属 DDL，需账号具备建库权限（如 root）；若失败返回 `ErrProvisionFailed`，前端提示"请使用具有建库权限的账号"。

### 本机可读写 / 远程只读

`Service.Query` 按 `IsLocalDSN(kind, dsn)` 分流（唯一收口，面板与 AI 工具 `datasource.query` 都走它）：

- **远程连接**：`ValidateReadOnlySQL` + `SQLQuerier`（`sql.TxOptions{ReadOnly:true}` 只读事务），写语句返回 `ErrStatementDenied`——客户库永远动不了。
- **本机连接**：跳过只读校验，走 `SQLWriteQuerier`（可写事务并 `Commit`）。行返回语句（`isRowReturning`：SELECT/WITH/SHOW/PRAGMA/EXPLAIN/VALUES/TABLE/DESCRIBE/DESC）走 `Query` 返回结果集；其余走 `Exec` 返回 `rows_affected`。允许本机建表/写数据，风险仅限本机、用户自有库。
- `Browse` 始终只读（仅 metadata 命名空间 + `connector.StatementAllowed`），不受读写分流影响。
- 覆盖：`TestSQLProvisioner*`、`TestProbeRunsProvisionerBeforePing`、`TestProbeProvisionFailureSkipsPingAndVerify`、`TestQueryAllowsWriteOnLocalConnection`、`TestQueryKeepsRemoteReadOnlyEvenWithWriteQuerier`、`TestIsLocalDSNDetectsLocalAndRemote`、`TestIsRowReturningClassifiesStatements`、`TestSQLWriteQuerierRejectsUnknownKind`、`datasourceDsn.test.ts`、`DataSourcePanel.test.tsx`。
