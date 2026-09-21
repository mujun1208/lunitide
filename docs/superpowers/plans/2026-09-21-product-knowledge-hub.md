# Product Knowledge Hub Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 Lunitide 加一个仅管理员可见的「产品总览」模块：本体 + 图谱 + 知识库 + 标签 + 体系；第一次用详细种子清单，之后每次启动按规则对照活源自动生成最新版；并产出自诊断报告。

**Architecture:** 新建 `internal/producthub` 与 `web/src/productHub`。活源（Page / 设置 / Bridge / 媒体枚举 / 注册表）经 `generate-product-catalog` 与采集器进入候选集；与 `seed.v1.json` 按 `stable_key` merge；快照失败保留旧 verified。诊断只读。门禁用 bcrypt，与 `internal/identity` 同款。不复用 `graph_nodes`（0062 是另一套本体）。迁移号 **0167**。

**Tech Stack:** Go, SQLite, Bridge schema + `npm --prefix web run generate:bridge`, React/TS/Vitest, 自研 SVG。

**Spec:**

- [生成原则](../../design/PRD-product-knowledge-hub-generation-principles-2026-09-21.md)
- [初版种子](../../design/catalog-product-knowledge-hub-v1-seed-2026-09-21.md)
- [自净化 PRD](../../design/PRD-product-hub-self-purify-2026-09-21.md)
- 上游 [PRD V3.1](../../design/PRD-product-knowledge-hub-2026-09-18.md)、[UI](../../design/UI-product-knowledge-hub-2026-09-18.md)（主色改黑白）

## Global Constraints

- 迁移必须是 `0167_product_knowledge_hub.sql`。0166 已被 `capability_pack_skip` 占用。
- 禁止手改 `internal/bridge/schema_generated.go`、`web/src/generated/bridge.ts`。改 `api/bridge/v1/*.schema.json` + envelope method enum，再 `generate:bridge` 与 `verify:bridge`。
- 不改 M8 契约、不写业务表、Renderer 不直接碰 SQL/FS/Shell。
- 未解锁：不渲染入口；除 `productHub.auth.status|unlock` 外全部 `PH-012`。
- 密码用 bcrypt cost 10 存哈希；初密不进 git。
- 探针并行，单探针 2s；全量构建 ≤3s。
- LLM 只补 summary/description，标 AI；事实只来自活源。
- 自净化本里程碑只出报告 + `wont_fix`；不实现 `diagnostics.apply`。
- UI token 黑白，绿黄红只表示状态。
- 办公组入口加在会议记录之后，并加 `officeMenu.productHub`（默认 false，引擎门覆盖）。
- 每个任务：红测 → 看失败 → 最小实现 → 绿测。PowerShell 每条命令后检查 `$LASTEXITCODE`。

---

## 文件地图

| 任务 | 新建/改 | 职责 |
|---|---|---|
| 1 | `internal/producthub/merge.go` `merge_test.go` `templates.go` | stable_key merge + 链路模板 |
| 2 | `migrations/0167_product_knowledge_hub.sql`；`store.go`；`internal/storage/sqlite/product_hub_expected_schema.go` | 12 知识表 + auth |
| 3 | `manifest/seed.v1.json` `landscape.json` `rules/tags.json` `manifest.go` | 初版种子机读 |
| 4 | `web/scripts/generate-product-catalog.mjs`；`internal/producthub/generated/catalog.go` | 活源 catalog |
| 5 | `collectors.go` `probes.go` `graph.go` `cards.go` | 采集与探针 |
| 6 | `snapshot.go` `diff.go` `tags.go` `service.go` | 快照状态机 |
| 7 | `diagnose.go` `diagnose_rules.go` | 诊断报告 |
| 8 | `auth.go` | 管理员门 |
| 9 | `api/bridge/v1/productHub.*.schema.json` + envelope + handlers | Bridge |
| 10 | `web/src/app/appTypes.ts` `LaunchSidebar.tsx` `officeMenuSettings.ts` `App.tsx` | 入口 |
| 11 | `web/src/productHub/**` | 页、图谱、卡、链路、诊断、黑白 CSS |
| 12 | `export_html.go` | HTML 导出 |

---

## Task 1：Merge 与模板（先锁「以后不用手改总表」）

- [ ] **1. 红测。** `internal/producthub/merge_test.go`：

```go
func TestMergeAddsLiveOnlyFeature(t *testing.T) {
    seed := []Card{{StableKey: "feature.dialog.music.play", Summary: "播放歌曲", Description: "种子讲解",
        Chain: mustChain()}} // 3-8 步 + success/failure
    live := []Candidate{{StableKey: "feature.office.media.favorite", Name: "收藏", Source: "bridge"}}
    out := Merge(seed, live, nil)
    if !contains(out, "feature.office.media.favorite") {
        t.Fatal("live-only must be generated")
    }
    got := byKey(out, "feature.dialog.music.play")
    if got.Description != "种子讲解" {
        t.Fatal("seed prose must win")
    }
}

func TestMergeRemovesMissingPage(t *testing.T) {
    prev := []Card{{StableKey: "feature.office.page.media"}}
    live := []Candidate{} // catalog 不再有 media
    out := Merge(nil, live, prev)
    if ch := Changelog(prev, out); !hasKind(ch, "feature.office.page.media", "removed") {
        t.Fatal("missing live page must retire")
    }
}

func TestMergeKeepsManualTags(t *testing.T) {
    // 上一快照 manual 标签在新 snapshot 仍在，assigned_by=manual
}
```

- [ ] **2. 跑。** `go test -count=1 ./internal/producthub -run TestMerge` 先失败。
- [ ] **3. 实现。** `Merge` 按原则 §6。`templates.go` 实现 9 个 `chain_class`，每条模板含 success+failure。失败分支必须带 retry 或 fallback。
- [ ] **4. 绿测。** 同上命令。
- [ ] **5. 提交。** `feat(producthub): merge live catalog with seed cards`

---

## Task 2：0167 表

- [ ] **1. 红测。** 空库/从 0166 升级后，下列表存在且 CHECK 有效：`product_snapshots` `product_graph_nodes` `product_graph_edges` `product_feature_cards` `product_chain_steps` `product_graph_index_versions` `product_tags` `product_node_tags` `product_enrichments` `product_change_log` `product_diagnostic_reports` `product_diagnostic_findings` `product_hub_auth`。
- [ ] **2. 跑。** `go test -count=1 ./internal/storage/sqlite -run TestProductHubSchema` 先失败。
- [ ] **3. DDL 要点。** 节点类型 13 个；snapshot state `building|verified|retired`；trigger `boot|registry|manifest|upgrade|manual`；auth 表：

```sql
CREATE TABLE product_hub_auth (
  singleton_id TEXT PRIMARY KEY CHECK (singleton_id = 'default'),
  username TEXT NOT NULL CHECK (username = 'mujun'),
  password_hash TEXT NOT NULL CHECK (length(password_hash) BETWEEN 20 AND 128),
  updated_at TEXT NOT NULL
);
```

首次启动若无行，用 bcrypt(cost 10) 写入初密哈希（常量只存在于 engine，不写进 seed JSON，不进前端）。登记 `store.go` migrations 下一项为 `0167_product_knowledge_hub.sql`，并补 `product_hub_expected_schema.go` 的 `expectedSchemaSQL`。
- [ ] **4. 绿测 + 提交。** `feat(producthub): add 0167 knowledge hub schema`

---

## Task 3：种子机读

- [ ] **1. 红测。** `LoadSeed` 拒绝：重复 stable_key、steps<3、failure 悬空、summary 空。接受 catalog 文档里「详」卡 + 总表其余行（总表行可只有 summary + chain_class）。
- [ ] **2. 实现。** 把种子文档抽成 `seed.v1.json`：`{ "version": "seed.v1", "features": [ ... ] }`。`landscape.json` 放两张槽位卡。
- [ ] **3. 绿测 + 提交。** `feat(producthub): add seed.v1 machine catalog`

---

## Task 4：活源 catalog 生成器

- [ ] **1. 红测。** 生成物含 `pages` 现有 16 个字面量、18 个 settings id、`media` 全部 action、至少包含 `media.asset.open` 的 bridge 方法。
- [ ] **2. 实现。** `web/scripts/generate-product-catalog.mjs`：解析 `appTypes.ts` 的 `Page=` 联合、`settingsNav.ts` 的 `id:`、`officeMenuSettings.ts` 的 `OfficeMenuSettings` 键、全部 `api/bridge/v1/*.schema.json` 的 `x-method`（`x-enabled !== false`）、`public.dto` 或 generated bridge 里 MediaOperationDTO.action。写出 `internal/producthub/generated/catalog.go`（`var Pages []string` 等）和 `web/src/generated/productCatalog.ts`。挂到 `web/package.json` 的 `generate:bridge` 之后或并入同一 npm script。
- [ ] **3. 绿测。** `go test -count=1 ./internal/producthub -run TestCatalogContainsMediaPlay`。
- [ ] **4. 提交。** `feat(producthub): generate live product catalog`

---

## Task 5：采集器 + 探针

采集器接口：

```go
type Collector interface {
    ID() string
    Collect(ctx context.Context, in CollectInput) (CollectOut, error)
}
```

`CollectOut` = candidates + edges。独立失败。

探针接口：

```go
type Probe interface {
    ID() string
    Run(ctx context.Context, g Graph) ProbeResult // 2s timeout wrapper
}
```

七个：`bridgeMethodExists` `featureFlag` `settingsCategoryExists` `entryPageExists` `prerequisitesCheck` `usesAssetsResolve` `settingsCoverage`。

- [ ] **1. 红测。** 假 collector 失败不影响其它；探针超时记 `probe-timeout` 不 panic；`usesAssetsResolve` 对孤儿引用返回 fail。
- [ ] **2. 实现 collectors：** pages/settings/office-menu/bridge/media-actions 读 generated catalog；plugins 读 `m8app.HarnessPlugins`；skills/mcp/experts/commands/automation/memory 接现有只读接口（先允许空实现返回 0，但测试用 stub）。
- [ ] **3. 绿测 + 提交。** `feat(producthub): collect live sources and run probes`

---

## Task 6：快照状态机

```
building → verified（digest 回填，旧 verified → retired）
building 失败 → 丢弃，旧 verified 保留
```

触发：boot 30s、registry 指纹 60s、catalog/seed digest 变化、upgrade 版本号、manual 30s 限流。

- [ ] **1. 红测。** 构建中途错误保留旧 verified；manual 30s 内第二次返回 `PH-005`；第二次成功构建写出 changelog added/updated/removed。
- [ ] **2. 实现 `snapshot.go` `diff.go` `tags.go`。** 影响面 = 一跳反向 `uses|calls|contains`。
- [ ] **3. 绿测 + 提交。** `feat(producthub): snapshot fsm and changelog`

---

## Task 7：诊断报告

- [ ] **1. 红测。** 注入孤儿 skill 引用 → 报告 PH-004 四要素齐全 → 修复种子再构建 → status=fixed 且 resolved_count≥1。诊断 panic → 旧报告仍在。
- [ ] **2. 实现 `diagnose.go` `diagnose_rules.go`。** 规则至少：孤儿引用、悬空分支、模块零卡、种子锚点已删、枚举未出卡、Feature 无 methods。
- [ ] **3. 不实现 apply。**
- [ ] **4. 绿测 + 提交。** `feat(producthub): write diagnostic reports after verify`

---

## Task 8：管理员门

对齐 `internal/identity` 的 bcrypt：

```go
func (s *Auth) Unlock(username, password string) (token string, err error)
func (s *Auth) ChangePassword(token, old, new string) error
func (s *Auth) Check(token string) bool
```

- [ ] **1. 红测。** 错用户名失败；对哈希成功；错密失败；未 Unlock 时 `Authorize` 为 PH-012；改密必须旧密正确。
- [ ] **2. 实现。** 用户名必须 `mujun`。token 内存 map，进程生命期。
- [ ] **3. 绿测 + 提交。** `feat(producthub): admin gate with bcrypt`

---

## Task 9：Bridge

新增 schema（空 payload 用 `type: object additionalProperties false`；有字段按 V3.1）：

`productHub.auth.status` `productHub.auth.unlock` `productHub.auth.changePassword`  
`productHub.status` `productHub.overview` `productHub.graph` `productHub.node`  
`productHub.featureCard` `productHub.tags` `productHub.tagSet` `productHub.changelog`  
`productHub.refresh` `productHub.export` `productHub.diagnostics`

`export.format` = `html|poster|report`。poster 可先返回 `PH-006` 未实现，但 schema 要有。

- [ ] **1. 写入 schema + envelope.schema.json method 枚举（按字母序插入 productHub.*）。**
- [ ] **2.**

```powershell
npm --prefix web run generate:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge generation failed" }
npm --prefix web run verify:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge verification failed" }
```

- [ ] **3. Handler** `defer recover()` → `ENGINE_INTERNAL_ERROR`。除 auth.status/unlock 外先 `Check(token)`。
- [ ] **4. 前端** 按 `createOntologyBridge` 形状加 `createProductHubBridge`（生成器若已生成则只包一层 get/singleton）。
- [ ] **5. 提交。** `feat(producthub): add productHub bridge methods`

---

## Task 10：入口（零侵入其它页）

- [ ] **1. 红测。** `LaunchSidebar.test.tsx`：未解锁不出现「产品总览」；解锁后出现在会议记录之后；`page==='productHub'` 时办公组展开且按钮 active。
- [ ] **2. 改：**

```ts
export type Page = /* 原联合 */ | 'productHub'
```

`officePage` 增加 `productHub`。`OfficeMenuSettings` 增加 `productHub: boolean`，默认 false。解锁成功后本会话显示（不要只靠 localStorage）。
- [ ] **3. `App.tsx` 挂 `ProductHubPage`；深链失败关回 home。**
- [ ] **4. 绿测 + 提交。** `feat(producthub): office sidebar entry behind admin gate`

---

## Task 11：UI（黑白 + 卡 + 链路 + 诊断）

组件：

```
web/src/productHub/
  productHub.css          /* §10 黑白 token */
  ProductHubPage.tsx      /* 页头 + 五标签：全景/图谱/解剖/诊断/图景 */
  UnlockGate.tsx          /* mujun + 密码 + 改密 */
  OverviewTab.tsx
  GraphTab.tsx            /* 五层 SVG */
  FeatureCardPage.tsx     /* A/B/C/D */
  ChainFlowView.tsx       /* 主干+四分支，点击高亮讲解行 */
  AnatomyTab.tsx
  DiagnosticsTab.tsx
  LandscapeTab.tsx
  ChangelogPanel.tsx
  productHubBridge.ts
```

链路节点坐标跟 UI 文档：主干 rect 112×32，成功/失败/降级描边语义色，边类 `.e-m .e-ok .e-f .e-rt .e-dg`。

- [ ] **1. 红测。** Unlock 错密失败；对密进入后能请求 overview。ChainFlowView：给音乐播放卡 fixture，主干 5 点 + success/failure 存在；点第 5 步，对应讲解行带 `.sel`。
- [ ] **2. 实现页面。** 搜索、变更抽屉、刷新、导出下拉。Tab4 徽章 = 未解决 error+warn。
- [ ] **3. 浏览器或组件测：未登录无入口。**
- [ ] **4. 提交。** `feat(producthub): black-white hub ui with cards and chains`

---

## Task 12：HTML 导出

- [ ] **1. 红测。** `export html` 含产品名、域计数、至少一张功能卡的 summary；`export report` 含 finding title + root_cause。
- [ ] **2. 实现 `export_html.go`。** 单文件、无用户数据。poster 可占位。
- [ ] **3. 绿测 + 提交。** `feat(producthub): export html overview and diagnostics`

---

## 验收对照（执行完必须全真）

| 要求 | 任务 |
|---|---|
| 本体/图谱/知识库/标签/体系 | 2–6, 11 |
| 初版详细清单（放歌/开文件等） | 3 + 种子文档 |
| 以后自动增删改 | 1, 4, 5 |
| 每功能说明书 + 链路图 + 逐步讲解 | 1 模板 + 3 详卡 + 11 ChainFlowView |
| 黑白 UI | 11 |
| 管理员 mujun / 可改密 | 8, 10 |
| 诊断报告四要素 + 闭环 | 7, 11 |
| 自净化实施方案 | 已写 PRD，不在本计划写 apply |
| 竞品/前沿槽位 | landscape.json + LandscapeTab |
| 不手改生成物、0167、不复用 0062 表 | 2, 9 |

---

## 自检（写计划时）

- 原则 §7「不必改 Hub」的路径：Task 4 catalog，有测试。
- 种子「详」卡不会在 live 刷新时被模板盖掉：Task 1 `TestMerge` 断言 Description 保留。
- 自净化 apply 未混进 Task 7。
- 无 TBD /「类似 Task N」。
