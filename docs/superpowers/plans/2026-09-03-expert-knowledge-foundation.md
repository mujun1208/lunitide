# 专家知识底座 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给每个同事专家（及 3 张 PM 内置专家）建立可检索的知识库、成长路径和解释性 `kb.search`，替换 stub 索引器。

**Architecture:** 领域包只做 schema 校验。KB 正文进 `kb_chunks.body` + FTS5。专家集合 `scope_id=expert:{expertId}`。检索对标 `recall.query` 的 hits + explanation。不引入向量库，不改 `0062` 的 `node_type` CHECK。

**Tech Stack:** Go（`internal/m8app`, `internal/app`, `internal/storage/sqlite`）+ React（`web/src/expert`）+ SQLite 迁移 `0113`–`0114` + Bridge schema。

**Spec:** [docs/superpowers/specs/2026-09-03-mro-expert-and-expert-foundation-design.md](../specs/2026-09-03-mro-expert-and-expert-foundation-design.md)

## Global Constraints

- 产品内核保持 SQLite；禁止 PostgreSQL / 本地 MySQL 当第二内核。
- 禁止生产路径依赖 Python / Electron / LightRAG / Qdrant / Neo4j / Redis。
- `kb_chunks.embedding` 保持 NULL；向量默认关。
- 不重写 `0062` `graph_nodes.node_type` CHECK；种类走 payload `pack` + `kind`。
- stub 空 body 的索引不算验收通过。
- 市场人设卡默认不建 KB collection。
- UI 只用现有 CSS 变量；中英双语。
- Bridge：`api/bridge/v1/<method>.schema.json` → envelope 枚举排序一致 → `npm run generate:bridge` → handler → `handlers_registry.go` → `web/src/bridge/client.ts`。
- 迁移登记 `internal/storage/sqlite/store.go` 清单 SHA-256；FTS 虚表不进 `expectedSchemaSQL`（同 0107）。
- 改 `kb_chunks` 表形后必须同步 `store.go` 的 `"table:kb_chunks"` 期望 DDL。

---

## File map

| File | Responsibility |
|------|----------------|
| `internal/m8app/ontologypacks/software.v1.json` | 软件工程包声明 |
| `internal/m8app/ontologypacks/mro.v1.json` | 机务包声明（本计划只加载校验，机务卡在另一计划启用） |
| `internal/m8app/ontologypack.go` | 加载、校验 payload |
| `migrations/0113_kb_chunk_body.sql` | `kb_chunks.body` |
| `migrations/0114_expert_foundation.sql` | FTS、成长路径、专家集合索引 |
| `internal/m8app/kb_index.go` | 真切块索引器 |
| `internal/m8app/kb_search.go` | `Search` / `Cite` / `EnsureExpertCollection` |
| `internal/m8app/expert_growth.go` | 成长路径读写 |
| `internal/app/kb_search_handlers.go` | Bridge handlers |
| `internal/app/chat_kb_inject.go` | 挂载专家时注入检索摘要 |
| `web/src/expert/ExpertKnowledgePanel.tsx` | 知识 / 路径页签 |

---

## Task 1: 领域本体包加载与校验

**Files:**
- Create: `internal/m8app/ontologypacks/software.v1.json`
- Create: `internal/m8app/ontologypacks/mro.v1.json`
- Create: `internal/m8app/ontologypack.go`
- Test: `internal/m8app/ontologypack_test.go`

- [ ] **Step 1: 写失败测试** `internal/m8app/ontologypack_test.go`

```go
package m8app_test

import (
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestOntologyPacksLoadSoftwareAndMRO(t *testing.T) {
	sw, err := m8app.LoadOntologyPack("software.v1")
	if err != nil || sw.ID != "software.v1" {
		t.Fatalf("software: %+v %v", sw, err)
	}
	if !sw.HasKind("Class") {
		t.Fatal("software.v1 must declare Class")
	}
	mro, err := m8app.LoadOntologyPack("mro.v1")
	if err != nil {
		t.Fatal(err)
	}
	if !mro.HasKind("Fault") || !mro.HasLink("APPLIES_TO") || !mro.HasAction("propose_remediation") {
		t.Fatalf("mro pack incomplete: %+v", mro)
	}
}

func TestOntologyPackValidatePayload(t *testing.T) {
	mro, err := m8app.LoadOntologyPack("mro.v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := mro.ValidateNodePayload(`{"pack":"mro.v1","kind":"Fault","code":"32-00","title":"EGT"}`); err != nil {
		t.Fatal(err)
	}
	if err := mro.ValidateNodePayload(`{"pack":"mro.v1","kind":"Spaceship"}`); err == nil {
		t.Fatal("unknown kind must fail")
	}
}

func TestOntologyPackUnknownID(t *testing.T) {
	if _, err := m8app.LoadOntologyPack("legal.v1"); err == nil {
		t.Fatal("missing pack must fail")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/m8app/ -run TestOntologyPack -count=1`

Expect: `undefined: m8app.LoadOntologyPack`

- [ ] **Step 3: 写入包 JSON 与加载器**

`software.v1.json`：

```json
{
  "id": "software.v1",
  "objects": [
    {"kind": "File"}, {"kind": "Document"}, {"kind": "Artifact"},
    {"kind": "Requirement"}, {"kind": "Decision"}, {"kind": "Module"},
    {"kind": "Class"}, {"kind": "Function"}, {"kind": "Interface"},
    {"kind": "Table"}, {"kind": "Field"}, {"kind": "UseCase"},
    {"kind": "TestCase"}, {"kind": "Task"}, {"kind": "Release"}
  ],
  "links": [{"name": "contains"}, {"name": "depends_on"}, {"name": "references"}],
  "actions": []
}
```

`mro.v1.json`：

```json
{
  "id": "mro.v1",
  "objects": [
    {"kind": "Aircraft"}, {"kind": "Document"}, {"kind": "Section"},
    {"kind": "Component"}, {"kind": "PartNumber"}, {"kind": "Fault"},
    {"kind": "Task"}, {"kind": "DefectRecord"}, {"kind": "WorkOrder"},
    {"kind": "Technician"}
  ],
  "links": [
    {"name": "APPLIES_TO"}, {"name": "CONTAINS"}, {"name": "SUPERSEDES"},
    {"name": "PART_OF"}, {"name": "HAS_PART"}, {"name": "SYMPTOM_OF"},
    {"name": "HAS_CAUSE"}, {"name": "FIXED_BY"}, {"name": "REQUIRES"},
    {"name": "INVOLVES"}, {"name": "PERFORMED_ON"}
  ],
  "actions": [
    {"name": "propose_remediation", "mode": "read"},
    {"name": "query_stock", "mode": "read"},
    {"name": "log_defect", "mode": "write"},
    {"name": "create_work_order", "mode": "write"},
    {"name": "audit_step", "mode": "write"}
  ]
}
```

`ontologypack.go` 要点：`//go:embed ontologypacks/*.json`；`type OntologyPack struct { ID string; Objects []struct{ Kind string `json:"kind"` }; Links []struct{ Name string `json:"name"` }; Actions []struct{ Name, Mode string } }`；`LoadOntologyPack` 按 id 查找；`HasKind`/`HasLink`/`HasAction`；`ValidateNodePayload` 解析 JSON，要求 `pack` 等于包 id 且 `kind` 在 objects 内。

物理入图时 `graph_nodes.node_type` 仍用 `Artifact` 或 `Document`（0062 CHECK 内），`payload` 带 `pack`/`kind`。本任务不写图。

- [ ] **Step 4: 再跑测试**

Run: `go test ./internal/m8app/ -run TestOntologyPack -count=1`

Expect: PASS

- [ ] **Step 5: Commit** `feat(kb): domain ontology packs software.v1 and mro.v1`

---

## Task 2: 迁移 0113 / 0114

**Files:**
- Create: `migrations/0113_kb_chunk_body.sql`
- Create: `migrations/0114_expert_foundation.sql`
- Modify: `internal/storage/sqlite/store.go`（manifest + `"table:kb_chunks"` 期望 DDL）

- [ ] **Step 1: 写 0113**

```sql
-- 0113 KB chunk body: searchable text. embedding stays NULL (vector off).
ALTER TABLE kb_chunks ADD COLUMN body TEXT NOT NULL DEFAULT '';
```

- [ ] **Step 2: 写 0114**

```sql
-- 0114 Expert knowledge foundation: FTS over chunk body, growth paths,
-- unique collection scope. FTS objects are skipped by expectedSchemaSQL
-- (same rule as 0107).
CREATE UNIQUE INDEX IF NOT EXISTS ux_kb_collections_scope
    ON kb_collections(scope_id);

CREATE TABLE expert_growth_paths (
    expert_id TEXT PRIMARY KEY CHECK (length(expert_id) = 26 AND substr(expert_id, 1, 1) GLOB '[0-7]' AND expert_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    mission_snapshot TEXT NOT NULL CHECK (length(mission_snapshot) BETWEEN 1 AND 4096),
    ladder_json TEXT NOT NULL CHECK (length(ladder_json) >= 2),
    coverage_json TEXT NOT NULL CHECK (length(coverage_json) >= 2),
    updated_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS kb_chunk_fts USING fts5(
    chunk_id UNINDEXED,
    body,
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS trg_kb_chunk_fts_ai AFTER INSERT ON kb_chunks BEGIN
  INSERT INTO kb_chunk_fts(chunk_id, body)
  SELECT new.chunk_id, new.body WHERE length(new.body) > 0;
END;

CREATE TRIGGER IF NOT EXISTS trg_kb_chunk_fts_au AFTER UPDATE OF body ON kb_chunks BEGIN
  DELETE FROM kb_chunk_fts WHERE chunk_id = old.chunk_id;
  INSERT INTO kb_chunk_fts(chunk_id, body)
  SELECT new.chunk_id, new.body WHERE length(new.body) > 0;
END;

CREATE TRIGGER IF NOT EXISTS trg_kb_chunk_fts_ad AFTER DELETE ON kb_chunks BEGIN
  DELETE FROM kb_chunk_fts WHERE chunk_id = old.chunk_id;
END;
```

- [ ] **Step 3: 登记 manifest 并更新期望 DDL**

Run: `Get-FileHash -Algorithm SHA256 migrations/0113_kb_chunk_body.sql` 与 `0114_expert_foundation.sql`

在 `store.go` 清单末尾追加两行。把 `"table:kb_chunks"` 的 CREATE 文本在 `embedding BLOB` 后增加 `body TEXT NOT NULL DEFAULT ''`（与迁移后 `sqlite_schema` 一致）。`kb_chunk_fts` 与 trigger **不要**写入 `expectedSchemaSQL`。

- [ ] **Step 4: 扩展 `m8core.KBChunk` 增加 `Body string`；`PutKBChunks` INSERT 增加 `body` 列。**

`internal/domain/m8core/kb.go` 的 `KBChunk` 增加 `Body string`。`internal/storage/sqlite/m8_kb.go` 的 INSERT 改为：

```sql
INSERT INTO kb_chunks
 (chunk_id,document_id,document_version,ordinal,content_digest,locator_json,embedding,created_at,body)
 VALUES(?,?,?,?,?,?,NULL,?,?)
```

最后一个绑定 `c.Body`。

- [ ] **Step 5: 跑迁移测试**

Run: `go test ./internal/storage/sqlite/ -count=1`

Expect: PASS（若 `TestExpectedSchema` 失败，按实际 `sqlite_schema` 对齐 `kb_chunks` DDL 字符串，不要改 0062 文件）。

- [ ] **Step 6: Commit** `feat(kb): chunk body column, FTS, expert growth paths`

---

## Task 3: 真切块索引器

**Files:**
- Create: `internal/m8app/kb_index.go`
- Test: `internal/m8app/kb_index_test.go`
- Modify: `internal/m8app/kb.go`（`NewKBService` 默认 indexer、`BuildChunkProjection` 写入 body）

现有 `KBIndexer` 只返回 chunk ID。扩展为能带正文：

```go
type IndexedChunk struct {
	ID      string
	Body    string
	Locator string // JSON
}

type KBBodyIndexer func(ctx context.Context, doc m8core.KBDocument) ([]IndexedChunk, error)
```

保留旧 `KBIndexer` 会破坏调用方。做法：在 `KBService` 增加 `bodyIndexer KBBodyIndexer`；`UpsertDocument` 若 `bodyIndexer != nil` 走新路径，把 `chunk.Body` 填进 `BuildChunkProjection` 的结果。

更小改动：扩展 `BuildChunkProjection` 接受 `[]IndexedChunk`。

- [ ] **Step 1: 写失败测试**

```go
func TestParseBodyIndexerSplitsMarkdownAndRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "amm.md")
	body := "# ATA 32\n\nGear retraction fault isolation.\n\n# ATA 33\n\nLights."
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := m8core.KBDocument{
		DocumentID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1,
		MediaType: "text/markdown", ContentRef: path,
		SHA256: strings.Repeat("ab", 32), SourceLocator: path,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	chunks, err := m8app.ParseBodyIndexer(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("want heading split, got %d", len(chunks))
	}
	if chunks[0].Body == "" {
		t.Fatal("body must not be empty")
	}
}

func TestParseBodyIndexerEmptyFileFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(path, []byte("   "), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := m8core.KBDocument{DocumentID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1, MediaType: "text/markdown", ContentRef: path, SHA256: strings.Repeat("cd", 32), SourceLocator: path, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if _, err := m8app.ParseBodyIndexer(context.Background(), doc); err == nil {
		t.Fatal("empty body must fail index")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/m8app/ -run TestParseBodyIndexer -count=1`

- [ ] **Step 3: 实现 `ParseBodyIndexer`**

规则：

- `content_ref` 必须是本地绝对路径；读文件失败 → 返回 `ErrKBIndexFailed`。
- `text/markdown` / `text/plain`：按二级标题或每 1200 字切块；每块 `Locator` 为 `{"documentId","version","ordinal","page":1,"quote":前 80 字}`。
- `application/pdf` / docx / xlsx：若 service 注入了 `ParseFn`，调用它把 `ParseBlock.Text` 按页成块；未注入则把整个文件当文本失败并返回明确错误 `parse function not configured`（测试用 markdown 路径）。
- 0 个非空块 → `ErrKBIndexFailed`。
- chunk 数 > `m8core.MaxKBChunksPerVersion` → 返回错误（调用方应拆 document，本函数不静默截断冒充成功）。

`NewKBService` 设置 `bodyIndexer: ParseBodyIndexer`。`DefaultKBIndexer` 保留给旧单测；`UpsertDocument` 优先 `bodyIndexer`。

更新 `UpsertDocument`：投影时把 `IndexedChunk.Body` 写入 `KBChunk.Body`；`content_digest` 改为 `sha256(doc.SHA256 + "|" + body + "|" + ordinal)`，保证正文变化会变 digest。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/m8app/ -run "TestParseBodyIndexer|TestKB" -count=1`

修复因 digest 算法变化而失败的旧 KB 单测：它们若仍用 stub indexer，继续走 `SetIndexer` 旧路径。

- [ ] **Step 5: Commit** `feat(kb): parse body indexer rejects empty stubs`

---

## Task 4: `EnsureExpertCollection` + `Search` + `Cite`

**Files:**
- Create: `internal/m8app/kb_search.go`
- Test: `internal/m8app/kb_search_test.go`
- Modify: `internal/storage/sqlite/m8_kb.go`（ListChunksByScope、GetCollectionByScope、SearchChunkFTS）

扩展 `KBTx`：

```go
GetKBCollectionByScope(scopeID string) (KBCollection, bool, error)
ListKBDocumentsByCollection(collectionID string) ([]m8core.KBDocument, error)
SearchKBChunkFTS(scopeID, query string, limit int) ([]KBSearchHit, error)
GetKBChunk(chunkID string) (m8core.KBChunk, error)
```

```go
type KBSearchHit struct {
	Chunk     m8core.KBChunk
	Document  m8core.KBDocument
	Score     float64
	Dropped   string // effectivity reason or empty
}

type KBSearchInput struct {
	ExpertID string
	Query    string
	TopK     int
	TailNo   string
	AsOf     string // RFC3339 date or empty
	DocType  string
}

type KBSearchResult struct {
	TraceID     string
	Hits        []KBCitedHit
	Explanation KBExplanation
	IndexVersion string
}

type KBCitedHit struct {
	ExpertID  string
	DocID     string
	Revision  string
	Locator   string
	Quote     string
	Score     float64
}

type KBExplanation struct {
	Reasons     []string
	Redactions  []string
	NotAdopted  []string
	Missing     bool
}
```

`ExpertScopeID(expertID string) string` 返回 `"expert:"+expertID`。

`EnsureExpertCollection(ctx, expertID)`：若无 collection 则 `PutKBCollectionIfAbsent`，`collection_id` 新 ULID，`scope_id=expert:{id}`，`auth_policy=local-owner`。同事专家与 PM 内置才调用；本函数不判断 kind，由 bootstrap 决定谁调用。

`Search`：

1. `TopK` 默认 6，最大 50。
2. 无 collection → 空 hits，`Missing=true`，reason `no collection for expert`。
3. FTS `kb_chunk_fts MATCH` 查询词（trigram；查询串去掉引号）。
4. 读取 locator：若 `tails` 非空且输入 `TailNo` 非空且不匹配 → 丢到 `NotAdopted`（`effectivity tail`）。
5. 若 `effFrom`/`effTo` 与 `AsOf` 不交 → `NotAdopted`。
6. 若 `DocType` 过滤不匹配 → `NotAdopted`。
7. 命中写入 `Hits`，`Quote` 取 body 前 240 字，`Revision` 取 locator.revision 否则 ""。
8. 0 命中 → `Missing=true`，reason `no controlled chunk matched`。

`Cite(hit)` 原样返回 `KBCitedHit`（用于工具层，禁止改 quote）。

- [ ] **Step 1: 写失败测试**（Ensure 幂等、Search 命中 markdown、effectivity 丢掉错误机尾、空库 Missing）

用 `sqlite.OpenTemplated` + `NewKBService` + `ParseBodyIndexer` upsert 一篇含 `locator` tails 的文档。测试里可先 `EnsureCollection` 再 upsert，再手工 `UPDATE kb_chunks SET locator_json=...` 若 indexer 尚未写 tails。索引器对 markdown 默认 `tails:[]`；本测试在 Search 输入 `TailNo="B-0000"` 时不应丢掉无 tails 的块。另写一条 locator 含 `"tails":["B-1234"]` 的 chunk，搜索 `TailNo="B-9999"` 必须进 NotAdopted。

- [ ] **Step 2: 实现 storage 方法与 Search**
- [ ] **Step 3:** `go test ./internal/m8app/ -run TestKBSearch -count=1` 与 `go test ./internal/storage/sqlite/ -run TestKB -count=1`
- [ ] **Step 4: Commit** `feat(kb): expert-scoped search with effectivity drop reasons`

---

## Task 5: 成长路径服务

**Files:**
- Create: `internal/m8app/expert_growth.go`
- Test: `internal/m8app/expert_growth_test.go`

```go
type GrowthPath struct {
	ExpertID        string
	MissionSnapshot string
	LadderJSON      string // [{"name":"手册检索","state":"have|learning|next"}]
	CoverageJSON    string // {"docTypes":["AMM"],"gaps":["MEL"]}
	UpdatedAt       string
}

func (s *GrowthService) GetOrInit(ctx, expertID, mission string) (GrowthPath, error)
func (s *GrowthService) RefreshCoverage(ctx, expertID string) (GrowthPath, error)
```

`GetOrInit`：无行则插入 `ladder_json=[{"name":"知识积累","state":"next"}]`，`coverage_json={"docTypes":[],"gaps":[]}`，`mission_snapshot` 截取 mission 前 4096 字。  
`RefreshCoverage`：扫描该专家 collection 下 ready 文档的 locator `docType`，写入 `docTypes`；机务包对象作为可选 gaps 仅当 collection 绑定 `mro.v1` 时计算（本任务可用「docTypes 为空则 gaps 含 文档」）。

- [ ] **Step 1: 写失败测试** GetOrInit 幂等、RefreshCoverage 统计 AMM
- [ ] **Step 2: 实现 + storage Put/Get growth**
- [ ] **Step 3:** `go test ./internal/m8app/ -run TestGrowth -count=1`
- [ ] **Step 4: Commit** `feat(expert): growth path get-or-init and coverage refresh`

---

## Task 6: Bootstrap 为同事专家建集合

**Files:**
- Modify: `internal/m8app/bootstrap.go`
- Test: `internal/m8app/conversation_experts_test.go`（扩展现有 `TestEnsureBuiltinExperts*`）

在 `EnsureBuiltinExperts` 成功 seed 每个专家后调用 `kb.EnsureExpertCollection` 与 `growth.GetOrInit(expertID, six.Mission)`。需要把 `KBService` 与 `GrowthService` 注入 bootstrap，或新增 `EnsureExpertFoundations(ctx, svc, kb, growth)`。

只处理：`ConversationExperts()` + `builtinExpertSpecs` 对应已存在 catalog 行。不要遍历市场人设卡。

- [ ] **Step 1: 扩展测试** `EnsureBuiltinExperts` 之后 `GetKBCollectionByScope("expert:"+id)` 为真；随机一个 agency 市场 id 没有 collection。
- [ ] **Step 2: 实现**
- [ ] **Step 3:** `go test ./internal/m8app/ -run TestEnsureBuiltinExperts -count=1`
- [ ] **Step 4: 在 `cmd/engine/main.go` 专家 bootstrap 之后调用 foundation ensure（对照现有 `EnsureBuiltinExperts` 调用点）。**
- [ ] **Step 5: Commit** `feat(expert): seed KB collections for colleague experts`

---

## Task 7: Bridge `kb.search` / `kb.cite` / `expert.knowledge.get` / `expert.growth.get`

**Files:**
- Create: `api/bridge/v1/kb.search.schema.json`
- Create: `api/bridge/v1/kb.cite.schema.json`
- Create: `api/bridge/v1/expert.knowledge.get.schema.json`
- Create: `api/bridge/v1/expert.growth.get.schema.json`
- Modify: `api/bridge/v1/envelope.schema.json`（方法名按字母序插入：`expert.growth.get` 在 `expert.detail` 与 `expert.install` 之间；`expert.knowledge.get` 在 `expert.install` 与 `expert.list` 之间；`kb.cite` / `kb.search` 在 `kb.upsertDocument` 旁）
- Create: `internal/app/kb_search_handlers.go`
- Modify: `internal/app/handlers_registry.go`
- Modify: `web/src/bridge/client.ts`（查询方法，非 mutation，除 cite 外）
- Modify: `web/scripts/generate-bridge.mjs` 的 x-enabled 列表（若断言失败）

`kb.search` payload：`expertId`（ULID）、`query`（1..2048）、`topK`（1..50 可选）、`tailNo`、`asOf`、`docType` 可选。  
result：对齐 spec FR-A6/A7：`traceId`, `hits[]`（expertId, docId, revision, locator, quote, score）, `explanation`（reasons, redactions, notAdopted, missing）, `indexVersion`。

`kb.cite` payload：同一 hit 对象；result 原样回传（服务端再校验 quote 是某 chunk body 前缀，否则 `KB_CITE_INVALID`）。

`expert.knowledge.get` payload：`expertId`。result：`documentCount`, `readyCount`, `chunkCount`, `nodeCount`, `memoryCount`（该专家 key 前缀的 memories + 已确认 facts 计数，数不到则 0）。

`expert.growth.get` payload：`expertId`。result：`missionSnapshot`, `ladder`（array）, `coverage`（object）, `scenarios`（`[{title,phaseKey}]` 调已有 scenario list）。

错误映射：collection 无 → 仍 200 + missing（不要 404，方便 UI）；cite 失败 → `BRIDGE_SCHEMA_INVALID`。

- [ ] **Step 1: 写 4 个 schema（含 x-examples 正反例）**
- [ ] **Step 2:** `npm run generate:bridge` 与 `npm run verify:bridge`
- [ ] **Step 3: handlers + 注册 + engine 字段 `m8kbsearch` / `m8growth`**
- [ ] **Step 4:** `go test ./internal/app/ -run TestKBSearchHandler -count=1`（新建 handler 测试：decode 空 query 失败；有服务返回 missing）
- [ ] **Step 5: Commit** `feat(bridge): kb.search cite and expert knowledge/growth`

---

## Task 8: 对话注入与工具允许

**Files:**
- Create: `internal/app/chat_kb_inject.go`
- Test: `internal/app/chat_kb_inject_test.go`
- Modify: `internal/app/chat_memory.go` 的 `prepareChatMemory`（或现有注入点）在已挂载专家时追加一段「专家知识」evidence
- Modify: `internal/app/chat_expert_tools.go`：同事专家允许 `kb.search`、`kb.cite`、`graph.expand`

`graph.expand` P0：handler 返回空 hits + `explanation.missing=true` + reason `graph unused`（满足降级原则）。本任务同时加 `api/bridge/v1/graph.expand.schema.json`（payload：`expertId`, `chunkId` 或 `nodeId`；result 同 search 形但 hits 可空）。

注入文本格式：

```
[专家知识 航空机务专家]
修订: 42  ATA: 32  引用: Gear retraction...
```

无命中不注入假依据。

- [ ] **Step 1: 写测试** 有 chunk 则 prepare 后的 evidence 含 quote；无 collection 则不含「修订」
- [ ] **Step 2: 实现注入 + 空 graph.expand**
- [ ] **Step 3:** `go test ./internal/app/ -run "TestChatKB|TestExpertTools" -count=1`
- [ ] **Step 4: Commit** `feat(chat): inject expert KB hits and allow kb.search tools`

---

## Task 9: 专家中心「知识」「路径」页签

**Files:**
- Create: `web/src/expert/ExpertKnowledgePanel.tsx`
- Test: `web/src/expert/ExpertKnowledgePanel.test.tsx`
- Modify: `web/src/expert/ExpertCenterPage.tsx`（详情区在情景卡旁加两个 tab：知识 / 路径）
- Modify: `web/src/bridge/client.ts` 已在 Task 7 加方法

页签「知识」：显示 `documentCount` / `readyCount` / `chunkCount` / `nodeCount` / `memoryCount`；按钮「把文件交给此专家」打开 `<input type="file">`，选文件后：计算 sha256（Web Crypto）→ 调现有附件或 workspace 写入 → `kb.upsertDocument`（collection 由 `expert.knowledge.get` 一并返回 `collectionId`——Task 7 的 knowledge.get 需增加 `collectionId` 字段）。若 knowledge.get 无 collectionId，先不要在 UI 创建，显示「此专家尚未建立知识库」（市场人设卡）。

页签「路径」：渲染 ladder 三态（会 / 正在补 / 下一步）、coverage.docTypes、coverage.gaps、scenarios 标题列表。

中英：`知识`/`Knowledge`，`路径`/`Path`。

- [ ] **Step 1: 写 vitest** 加载 knowledge.get 数字；人设卡无 collection 显示尚未建立；路径渲染「正在补」
- [ ] **Step 2: 实现面板并接入 ExpertCenterPage**
- [ ] **Step 3:** `npm --prefix web run test -- ExpertKnowledgePanel ExpertCenterPage`
- [ ] **Step 4: Commit** `feat(ui): expert center knowledge and growth tabs`

---

## Task 10: 底座验收

- [ ] **Step 1:** `go test ./internal/m8app/ ./internal/app/ ./internal/storage/sqlite/ -count=1`
- [ ] **Step 2:** `npm run verify:bridge`
- [ ] **Step 3:** 对照 spec FR-A1..A7：A1 bootstrap；A2 UI 数字；A3 真切块；A4 路径页；A5 注入；A6 cite 结构；A7 explanation。缺口只允许 `graph.expand` 空实现（spec 允许 P0）。
- [ ] **Step 4: Commit** 仅当有修复时 `fix(kb): foundation acceptance gaps`

---

## Spec coverage

- FR-A1..A7：Task 4–9
- 领域包：Task 1
- 真切块：Task 3
- 不建市场人设库：Task 6
- `graph.expand` 降级：Task 8
- 机务专家卡 / 工作台 / 数据源：不在本计划
