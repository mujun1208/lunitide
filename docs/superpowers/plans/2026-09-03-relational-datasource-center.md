# 关系数据库连接中心 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在设置里让用户配置、探测并只读浏览 PostgreSQL / MySQL，把连接绑定到专家或机务工作台；产品内核仍是 SQLite。

**Architecture:** 复用已有 `db_connections` + `db.query` + `internal/connector` 元数据白名单。新增 Bridge `datasource.*` 与绑定表。DSN 只进 secret 存储，Bridge 永不回传连接串。未探测成功的连接禁止查询。

**Tech Stack:** Go（`internal/datasourceapp` 或 `internal/m7app` 扩展）+ 现有 secret 经纪 + React 设置页 + 迁移 `0115`。

**Spec:** [docs/superpowers/specs/2026-09-03-mro-expert-and-expert-foundation-design.md](../specs/2026-09-03-mro-expert-and-expert-foundation-design.md) §5、FR-D1..D5

## Global Constraints

- 产品内核保持 SQLite；禁止安装本地 MySQL 服务；禁止把月汐库迁到 PostgreSQL。
- `kind` P1 仅 `postgres` | `mysql`。P2 方言扩展不在本计划实现，但 Task 7 必须写下验收清单文件。
- 默认只读。`datasource.query` / `db.query` 在 `readonly_verified_at IS NULL` 时拒绝（现有 `DBQuery` 已检查）。
- DSN 不进 snapshot、日志、Bridge DTO、Renderer（ADR-004）。
- 浏览只允许 `information_schema` / `pg_catalog` / `performance_schema` / `sys` 等 `connector.MetadataNamespaces`。禁止默认 `SELECT *` 业务表。
- UI 只用现有令牌；设置分类文案中英双语。
- 迁移从 `0115` 编号，登记 `store.go` 清单。

---

## File map

| File | Responsibility |
|------|----------------|
| `migrations/0115_datasource_bindings.sql` | 绑定表 |
| `internal/datasourceapp/service.go` | list/create/probe/browse/bind/disable |
| `internal/app/datasource_handlers.go` | Bridge |
| `web/src/settings/DataSourcePanel.tsx` | 设置 UI |
| `web/src/settings/settingsNav.ts` | 新分类 `datasources` |
| `docs/superpowers/specs/2026-09-03-datasource-dialect-checklist.md` | FR-D5 清单 |

---

## Task 1: 列出与禁用连接（storage）

**Files:**
- Modify: `internal/storage/sqlite/m7_toolgap.go`
- Modify: `internal/m7app/toolgap.go`（`ToolgapTx` 加 List/Disable）
- Test: `internal/storage/sqlite/m7_toolgap_test.go`（若无则新建 `datasource_test.go`）

现有只有 `GetDBConnection` / `PutDBConnection`。新增：

```go
ListDBConnections() ([]m7flow.DBConnection, error)
SetDBConnectionVerified(id, verifiedAt string) error
DisableDBConnection(id string) error
```

P1 不删行：`Disable` 把 `readonly_verified_at` 置 NULL，并写审计 `datasource.disabled`。若表无 disabled 列，用 `readonly_verified_at IS NULL` 表示不可用；另加列更清晰：

`0115` 同时加：

```sql
ALTER TABLE db_connections ADD COLUMN state TEXT NOT NULL DEFAULT 'active'
    CHECK (state IN ('active','disabled'));
```

并更新 `store.go` 的 `"table:db_connections"` 期望 DDL（在 `created_by` 前或后加 `state`，以实际 ALTER 后 `sqlite_schema` 为准）。

- [ ] **Step 1: 写失败测试** Put 一条 postgres 连接，List 长度为 1 且 DSNSecretRef 非空、测试断言 **没有** DSN 明文列；Disable 后 state=disabled；Get 仍在。
- [ ] **Step 2: 写 0115 前半（state 列）+ 登记 hash + 实现 List/Disable**
- [ ] **Step 3:** `go test ./internal/storage/sqlite/ -run TestDBConnection -count=1`
- [ ] **Step 4: Commit** `feat(db): list and disable db_connections without exposing DSN`

---

## Task 2: 绑定表

**Files:**
- Modify: `migrations/0115_datasource_bindings.sql`（可与 Task 1 同一文件：先 state 列，再 bindings 表）
- Create: `internal/datasourceapp/bind.go`
- Test: `internal/datasourceapp/bind_test.go`

```sql
CREATE TABLE datasource_bindings (
    binding_id TEXT PRIMARY KEY CHECK (length(binding_id) = 26 AND substr(binding_id, 1, 1) GLOB '[0-7]' AND binding_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_type TEXT NOT NULL CHECK (owner_type IN ('expert','mro')),
    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 64),
    connection_id TEXT NOT NULL REFERENCES db_connections(id),
    purpose TEXT NOT NULL CHECK (purpose IN ('stock','workorder','generic')),
    table_map_json TEXT NOT NULL CHECK (length(table_map_json) >= 2),
    created_at TEXT NOT NULL,
    UNIQUE (owner_type, owner_id, purpose)
);
```

`table_map_json` 例：`{"schema":"inv","table":"stock","pnColumn":"part_no","stationColumn":"station","qtyColumn":"qty"}`。

服务：

```go
type BindInput struct {
	OwnerType, OwnerID, ConnectionID, Purpose, TableMapJSON, Actor string
}
func (s *Service) Bind(ctx, BindInput) (bindingID string, error)
func (s *Service) ListBindings(ctx, ownerType, ownerID string) ([]Binding, error)
```

规则：connection 必须 `state=active` 且 `ReadOnlyVerifiedAt != nil`，否则 `ErrDatasourceNotVerified`。`table_map_json` 必须是对象，key 仅 `schema,table,pnColumn,stationColumn,qtyColumn,woColumn,tailColumn` 的子集，字符串值 1..128。

- [ ] **Step 1: 写失败测试** 未探测连接 Bind 失败；探测后 Bind 成功；重复 purpose 409
- [ ] **Step 2: 实现**
- [ ] **Step 3:** `go test ./internal/datasourceapp/ -run TestBind -count=1`
- [ ] **Step 4: Commit** `feat(db): datasource bindings require verified connections`

---

## Task 3: Probe（只读探测）

**Files:**
- Create: `internal/datasourceapp/probe.go`
- Test: `internal/datasourceapp/probe_test.go`

```go
func (s *Service) Probe(ctx context.Context, id string) error
```

步骤：

1. 取 connection；`kind` 必须是 `postgres` 或 `mysql`。
2. 用 secret 经纪按 `dsn_secret_ref` 取 DSN（测试注入 `SecretGet func(ref string) (string, error)` 返回 `postgres://` 或内存替身）。
3. 打开只读驱动。P1 实现：`database/sql` + 纯 Go 驱动（若仓库已有 `github.com/jackc/pgx` 或 `github.com/go-sql-driver/mysql` 则用已有；**没有则不要为了本任务引入 CGO**）。若生产尚未引入驱动：Probe 在 `LUNITIDE_DATASOURCE_PROBE=stub` 测试时用 fake ping；正式路径调用可注入 `Pinger`：

```go
type Pinger func(ctx context.Context, kind, dsn string) error
```

测试用 fake：kind=postgres 且 dsn 含 `readonly` 则成功，否则失败。

4. 成功则 `SetDBConnectionVerified(id, now)`。
5. 只执行 `SELECT 1`（postgres）或 `SELECT 1`（mysql）。禁止用户自定义 SQL。

- [ ] **Step 1: 写测试** stub ping 成功写 verified_at；失败保持 NULL
- [ ] **Step 2: 实现 + 注入 Pinger**
- [ ] **Step 3:** `go test ./internal/datasourceapp/ -run TestProbe -count=1`
- [ ] **Step 4: Commit** `feat(db): readonly probe stamps verified_at`

---

## Task 4: Browse metadata

**Files:**
- Create: `internal/datasourceapp/browse.go`
- Test: `internal/datasourceapp/browse_test.go`

复用 `connector.StatementAllowed`。根据 `kind` 选只读 metadata SQL：

- postgres `schema` scope：`SELECT schema_name FROM information_schema.schemata`
- postgres `table`：`SELECT table_schema, table_name FROM information_schema.tables WHERE table_schema = $1`
- mysql 对应 `information_schema.TABLES`

任何业务表名直接进 FROM 必须被 `StatementAllowed` 拒绝。Browse 入参 `scope` 仅 `catalog|schema|table|column`。未 verified 拒绝。

返回 `[]{name, schema}`，最多 200 行。

- [ ] **Step 1: 写测试** 用 fake Querier 返回 information_schema 行；传入 `SELECT * FROM inventory` 必须 `ErrStatementDenied`
- [ ] **Step 2: 实现**（组装 SQL，先 `connector.StatementAllowed`，再 Query）
- [ ] **Step 3:** `go test ./internal/datasourceapp/ -run TestBrowse -count=1`
- [ ] **Step 4: Commit** `feat(db): metadata browse stays on information_schema allowlist`

---

## Task 5: Create 连接（密钥）

**Files:**
- Create: `internal/datasourceapp/create.go`
- Test: `internal/datasourceapp/create_test.go`

```go
type CreateInput struct {
	Name, Kind, DSN, Actor string
}
```

规则：`name` 1..128 唯一；`kind` ∈ postgres|mysql；DSN 非空且不含换行。生成 ULID。`dsn_secret_ref = "dbconn:"+id`。通过注入 `SecretPut(ref, dsn)` 写入；**DSN 不写 SQLite 除 ref 外任何列**。`state=active`，`readonly_verified_at` 空。审计 `datasource.created`（资源 id=connection id，无 DSN）。

测试 spy：`SecretPut` 被调用一次且 SQLite 行的 `dsn_secret_ref` 为 `dbconn:{id}`；扫描表内容不得出现 DSN 明文。

- [ ] **Step 1: 写失败测试**
- [ ] **Step 2: 实现**
- [ ] **Step 3:** `go test ./internal/datasourceapp/ -run TestCreate -count=1`
- [ ] **Step 4: Commit** `feat(db): create connection stores DSN only in secret ref`

---

## Task 6: Bridge + 设置 UI

**Files:**
- Create: `api/bridge/v1/datasource.list.schema.json`
- Create: `api/bridge/v1/datasource.create.schema.json`
- Create: `api/bridge/v1/datasource.probe.schema.json`
- Create: `api/bridge/v1/datasource.browse.schema.json`
- Create: `api/bridge/v1/datasource.bind.schema.json`
- Create: `api/bridge/v1/datasource.disable.schema.json`
- Modify: `api/bridge/v1/envelope.schema.json`（`datasource.*` 按字母序插在 `devTask.transition` 与 `diagnostics.export` 之间）
- Create: `internal/app/datasource_handlers.go`
- Modify: `internal/app/handlers_registry.go`
- Modify: `web/src/bridge/client.ts`（create/probe/bind/disable 为 mutation）
- Create: `web/src/settings/DataSourcePanel.tsx`
- Test: `web/src/settings/DataSourcePanel.test.tsx`
- Modify: `web/src/settings/settingsNav.ts`：`SettingsCategory` 增加 `'datasources'`；智能分组加入；keywords `数据库 PostgreSQL MySQL 数据源 DSN`
- Modify: `web/src/settings/SettingsPage.tsx`：该分类渲染 `DataSourcePanel`

list 结果字段：`id, name, kind, state, readonlyVerified, createdAt`。**禁止** `dsn` / `dsnSecretRef`。

create payload：`name, kind, dsn, requestId`。dsn 仅此一次上行，响应不含 dsn。

UI：列表（名称、引擎、已探测/未探测、禁用）；表单 kind 下拉 postgres/mysql + 名称 + DSN 密码框；按钮「探测」「浏览 schema」「禁用」。浏览结果只显示 schema/表名。绑定 UI 可本任务只做 expert/mro owner 的下拉（机务工作台 P1 再接具体映射表单）。

- [ ] **Step 1: schema + generate:bridge + verify:bridge**
- [ ] **Step 2: handler 测试** list 剥除 secret；create 空 dsn 400
- [ ] **Step 3: DataSourcePanel 测试** 不渲染 DSN 回显；未探测显示「不可查询」
- [ ] **Step 4:** `npm --prefix web run test -- DataSourcePanel`
- [ ] **Step 5: Commit** `feat(ui): settings data source center for postgres and mysql`

---

## Task 7: `datasource.query` 工具 + FR-D5 清单

**Files:**
- Modify: `internal/app/chat_expert_tools.go` 允许工具名 `datasource.query`
- Create: `internal/app/datasource_query.go`（包装 `ToolgapService.DBQuery`：必须有 binding；SQL 再过 `m7flow.ValidateReadOnlySQL`；`ConnID=binding.ConnectionID`；未 verified 返回明确错误「连接未探测」）
- Create: `docs/superpowers/specs/2026-09-03-datasource-dialect-checklist.md`

清单文件正文（不得 TBD）：

```markdown
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
```

`query_stock` 要有 stock binding 才允许（机务计划调用）；本任务只实现 generic `datasource.query`：payload `connectionId` 或 `bindingId` + `sql` + `maxRows<=200`。

- [ ] **Step 1: 写测试** 未 verified 拒绝；`DELETE FROM x` 拒绝；verified 后 SELECT 走注入 Querier
- [ ] **Step 2: 实现包装 + 清单文件**
- [ ] **Step 3:** `go test ./internal/app/ -run TestDatasourceQuery -count=1`
- [ ] **Step 4: Commit** `feat(db): datasource.query wrapper and dialect checklist`

---

## Spec coverage

- FR-D1：Task 5–6
- FR-D2：Task 3 + query 拒绝
- FR-D3：Task 4
- FR-D4：绑定 purpose=stock（Task 2）；工作台映射表单在机务计划
- FR-D5：Task 7 清单
- 不换 SQLite 内核：全程无新守护进程
