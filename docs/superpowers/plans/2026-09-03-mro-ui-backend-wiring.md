# 机务专家 UI 与后台线路 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按复盘规格把第 14 张专家卡、专家知识/成长页、条件机务工作台、对话引用芯片和设置数据源接到现有 Bridge，而不污染非机务用户的侧栏。

**Architecture:** 产品代码服从 `docs/superpowers/specs/2026-09-03-mro-prd-replay-ui-and-wiring.md` §3。底座检索与表结构仍按 `2026-09-03-expert-knowledge-foundation.md`；机务卡与表按 `2026-09-03-mro-expert-and-workbench.md`（引用门按 H3 改）；连接按 `2026-09-03-relational-datasource-center.md`。本计划只写界面、身份解析、会话上下文和屏幕-API 接线。

**Tech Stack:** React/TypeScript（`web/src/expert` `web/src/mro` `web/src/settings` `web/src/session`）+ 现有 Bridge + 现有 CSS 令牌。

**Spec:** [docs/superpowers/specs/2026-09-03-mro-prd-replay-ui-and-wiring.md](../specs/2026-09-03-mro-prd-replay-ui-and-wiring.md)

## Global Constraints

- 不 Fork WingFix；内核 SQLite；不装本地 MySQL；向量默认关。
- UI 只用 `--bg --bg2 --bg3 --ink --muted --rule --line --tide1 --tide2 --glow --ok --warn --err` 与现有 `skill-center` / `settings-shell` / `pending-memory-banner`。禁止新组件库。
- Bridge 的 `expertId` 永远是 26 位 ULID。`catalogItemId`（如 `mro-expert`）只用于寻址与显隐。禁止 `scope_id=expert:mro-expert`。
- 侧栏「机务工作台」仅当已安装且 `state=enabled` 且 `catalogItemId=mro-expert` 时渲染。
- 引用门禁止整段替换成功回答；无 cite 的确定件号只改该句并挂芯片。
- 手册注入默认 locator + quote≤240；全文 PDF 不进 prompt。
- 路径页文案「这位专家的成长」。无文档不画假阶梯。
- 中英双语。页眉「辅助建议，不构成放行」。
- 市场人设卡不建知识库。

**Prerequisite:** 先完成 foundation 计划 Task 1–8（真切块、`kb.search`、`expert.knowledge.get` 含 `collectionId`、`expert.growth.get`）。本计划 Task 从接线开始。若那些 Task 未做，本计划的 UI 测试用 mock Bridge。

---

## File map

| File | Responsibility |
|------|----------------|
| `web/src/expert/expertIds.ts` | `catalogItemId` ↔ 已安装 ULID |
| `web/src/expert/conversationExperts.ts` | 增加 `mro-expert`；`ConversationExpertDivision` 含 `operations` |
| `web/src/expert/ExpertDetailTabs.tsx` | 概览 / 知识 / 路径页签壳 |
| `web/src/expert/ExpertKnowledgePanel.tsx` | 知识页 |
| `web/src/expert/ExpertGrowthPanel.tsx` | 成长页 |
| `web/src/expert/ExpertCenterPage.tsx` | 接入页签；机务显示「打开工作台」 |
| `web/src/mro/mroContext.ts` | `MroSessionContext` 类型与读写 |
| `web/src/mro/MroWorkbenchPage.tsx` | 顶栏 + 左轨 + 手册 |
| `web/src/mro/MroAskButton.tsx` | 问月汐：建会话、挂载、写入 context |
| `web/src/session/MroCiteList.tsx` | 引用芯片 |
| `web/src/session/PendingMemoryBanner.tsx` | 复用；机务确认同一句式 |
| `web/src/settings/DataSourcePanel.tsx` | 设置数据源 |
| `web/src/settings/settingsNav.ts` | `datasources` |
| `web/src/App.tsx` | `Page` 加 `mro`；条件侧栏 |
| `web/src/styles.css` | 仅追加 `mro-*` / `expert-detail-tabs` 必要规则 |
| `internal/app/chat_citation_gate.go` | 按 H3：不整段吞答 |
| `internal/app/chat_kb_inject.go` | 读 `session.mroContext`；只注入 240 字 |

---

## Task 1: 身份解析（catalogItemId ↔ ULID）

**Files:**
- Create: `web/src/expert/expertIds.ts`
- Test: `web/src/expert/expertIds.test.ts`
- Modify: `web/src/expert/conversationExperts.ts`（类型与 division）
- Test: `web/src/expert/conversationExperts.test.ts`

```ts
export function findInstalledExpert<T extends {expertId: string; catalogItemId?: string; name?: string; state?: string}>(
  items: readonly T[],
  catalogItemId: string,
): T | undefined {
  const key = catalogItemId.trim()
  return items.find(item => (item.catalogItemId ?? '') === key)
}

export function isEnabledMroExpert(items: readonly {catalogItemId?: string; state?: string}[]): boolean {
  return items.some(item => item.catalogItemId === 'mro-expert' && item.state === 'enabled')
}
```

`conversationExperts.ts`：

- `CONVERSATION_EXPERTS` 追加 `{id:'mro-expert', name:'航空机务专家'}`
- `ConversationExpertDivision` 改为 `'product' | 'data' | 'design' | 'engineering' | 'testing' | 'operations'`
- `conversationExpertDivision('mro-expert')` 返回 `'operations'`
- preferred skills / required tools 与规格附录一致；**不要**给机务卡加 `web.search`

- [ ] **Step 1: 写失败测试** `expertIds.test.ts`：列表含 `catalogItemId:'mro-expert', expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAA', state:'enabled'` 时 `findInstalledExpert` 返回该 ULID；`state:'disabled'` 时 `isEnabledMroExpert` 为 false。`conversationExperts.test.ts`：`toHaveLength(14)`；`conversationExpertDivision('mro-expert') === 'operations'`。

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web run test -- expertIds conversationExperts`

- [ ] **Step 3: 写最小实现**（上列函数 + 花名册一行 + division 分支）

- [ ] **Step 4: 跑测试确认通过**

- [ ] **Step 5: Commit** `feat(expert): resolve mro catalog id to installed ULID`

---

## Task 2: 会话机务上下文

**Files:**
- Create: `web/src/mro/mroContext.ts`
- Test: `web/src/mro/mroContext.test.ts`
- Modify: `internal/app/chat_kb_inject.go`（若 foundation Task 8 已存在则改它）：`Search` 缺省 `TailNo`/`AsOf` 来自 session meta `mroContext`

```ts
export type MroSessionContext = {
  tailNo: string
  asOf: string
  manualIds: string[]
  pack: 'mro.v1'
  scenario?: 'manual' | 'fault' | 'checklist'
}

export const MRO_CONTEXT_KEY = 'mroContext'

export function parseMroContext(raw: unknown): MroSessionContext | undefined {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return undefined
  const row = raw as Record<string, unknown>
  const tailNo = String(row.tailNo ?? '').trim()
  const asOf = String(row.asOf ?? '').trim()
  if (tailNo.length < 1 || tailNo.length > 32) return undefined
  if (!/^\d{4}-\d{2}-\d{2}$/.test(asOf)) return undefined
  const manualIds = Array.isArray(row.manualIds) ? row.manualIds.filter((id): id is string => typeof id === 'string' && id.length === 26) : []
  return {tailNo, asOf, manualIds, pack: 'mro.v1', scenario: row.scenario === 'fault' || row.scenario === 'checklist' ? row.scenario : 'manual'}
}
```

Go 侧 session meta 已有 JSON 袋则复用；没有则把 context 写进该会话工作记忆 key `session:{id}:mroContext`（只存结构化 JSON，不进长期记忆、不走确认条）。

注入规则：`len(quote) > 240` 则截断。`sendManualExcerpts` 未开（默认）时 prompt 只含 `docType revision ata page`，不含 quote。

- [ ] **Step 1: 写失败测试** 合法 `{tailNo:'B-0000', asOf:'2026-09-03'}` 解析成功；缺日期返回 undefined；`kb inject` 测试：session 带 tail B-0000 时 Search 收到 TailNo（foundation 测试扩一条）。

- [ ] **Step 2: 实现 parse + inject 读 context**

- [ ] **Step 3:** `npm --prefix web run test -- mroContext` 与 `go test ./internal/app/ -run TestChatKB -count=1`

- [ ] **Step 4: Commit** `feat(mro): session context carries tail and date into kb.search`

---

## Task 3: 专家详情三页签 + 知识 / 成长

**Files:**
- Create: `web/src/expert/ExpertDetailTabs.tsx`
- Create: `web/src/expert/ExpertKnowledgePanel.tsx`
- Create: `web/src/expert/ExpertGrowthPanel.tsx`
- Test: `web/src/expert/ExpertKnowledgePanel.test.tsx`
- Test: `web/src/expert/ExpertGrowthPanel.test.tsx`
- Modify: `web/src/expert/ExpertCenterPage.tsx`：详情栏在名片与主操作之下渲染 `<ExpertDetailTabs/>`；概览放入现有运行时/六段/情景；**不要**把知识数字直接追加在六段后面。
- Modify: `web/src/styles.css` 追加：

```css
.expert-detail-tabs{display:flex;padding:4px;border:1px solid var(--rule);border-radius:10px;background:var(--bg2);margin:10px 0}
.expert-detail-tabs button{flex:1;padding:7px 8px;border:0;background:transparent;color:var(--muted);border-radius:8px}
.expert-detail-tabs button[aria-selected="true"]{background:var(--bg);color:var(--ink)}
.expert-knowledge-stats{display:flex;flex-wrap:wrap;gap:8px 16px;color:var(--muted);font-size:12px;margin:0 0 12px}
.expert-knowledge-table{width:100%;border-collapse:collapse;font-size:12px}
.expert-knowledge-table th,.expert-knowledge-table td{padding:8px 6px;border-bottom:1px solid var(--rule);text-align:left}
.expert-growth-empty{color:var(--muted);font-size:12px;line-height:1.55}
```

知识页行为：

- 调 `expert.knowledge.get({expertId})`（ULID）。
- 无 `collectionId`：文案「人设卡不建知识库。升级为同事专家后再说。」无文件按钮。
- 有集合：显示 `documentCount readyCount chunkCount nodeCount memoryCount`。按钮「把文件交给此专家」→ `<input type="file" accept=".pdf,.docx,.md,.txt,application/pdf">`。
- 上传：沿用附件/工作区已有 sha256 写入，再 `kb.upsertDocument`。`index_state=failed` 行 class `is-failed`，文案用返回 error 或「无法抽出正文」。
- 中文标签：知识 / Knowledge 用 `useZh()`。

成长页：

- `expert.growth.get`。
- 标题「这位专家的成长」/ `This expert's path`。
- `coverage.docTypes.length===0`：只渲染使命 +「把文件交给此专家后，这里会列出已覆盖的类型。」**断言页面不出现「正在补」三态条。**
- 有 docTypes：列出已覆盖与 gaps（仅当 gaps 非空）。

机务概览主操作增加：`catalogItemId==='mro-expert' && state==='enabled'` 时按钮「打开工作台」，`onOpenWorkbench?.()`。

- [ ] **Step 1: 写 vitest**
  - knowledge：mock get 返回 counts 与 collectionId，看见「480」或 chunkCount。
  - knowledge：无人设 collectionId 时无「把文件交给此专家」。
  - growth：空 coverage 不含「正在补」。
  - growth：文案含「这位专家的成长」。

- [ ] **Step 2: 实现三组件并改 ExpertCenterPage**（详情栏用 tabs state `'overview'|'knowledge'|'growth'`）

- [ ] **Step 3:** `npm --prefix web run test -- ExpertKnowledgePanel ExpertGrowthPanel ExpertDetailTabs`

- [ ] **Step 4: Commit** `feat(ui): expert detail tabs for knowledge and growth`

---

## Task 4: 引用门按 H3 + 对话芯片

**Files:**
- Modify: `internal/app/chat_citation_gate.go`（若尚未创建则按 mro 计划 Task 4 创建，但规则换成下面）
- Test: `internal/app/chat_citation_gate_test.go`
- Create: `web/src/session/MroCiteList.tsx`
- Test: `web/src/session/MroCiteList.test.tsx`
- Modify: `web/src/session/SessionPage.tsx`：当本轮结果含 `cites` 或 `mroGate` 时在该助手气泡下渲染芯片；有 `mroContext` 时消息列顶渲染微条。

```go
func GateMROAnswer(text string, cites []CitationBlock) (out string, chips []GateChip) {
    out = text
    if !containsAdvisory(text) {
        out = "辅助建议，不构成放行。\n" + text
        chips = append(chips, GateChip{Kind: "advisory"})
    }
    if hasDefinitePNOrTorque(text) && len(cites) == 0 {
        out = rewriteDefiniteClaims(out)
        chips = append(chips, GateChip{Kind: "ungrounded", Detail: "未找到受控依据，未给出确定件号"})
    }
    return out, chips
}

func RestoreCouncilCitations(draft string, cites []CitationBlock) (string, bool) {
    restored := false
    for _, c := range cites {
        if c.DocID != "" && strings.Contains(draft, c.DocID) {
            continue
        }
        q := c.Quote
        if len([]rune(q)) > 40 {
            q = string([]rune(q)[:40])
        }
        if q != "" && strings.Contains(draft, q) {
            continue
        }
        draft += "\n\n" + formatCiteAppendix(c)
        restored = true
    }
    return draft, restored
}
```

禁止：`return fixedRefusal, nil` 整段丢掉模型其余叙述。

`MroCiteList` props：`cites: Array<{docType?: string; revision: string; locator: string; quote: string; expertName: string}>`；`discarded?: number`；`gate?: string`；`restored?: boolean`。

渲染：每条 `article.mro-cite` 左边框 `2px solid var(--tide1)`。丢弃行 muted。「已补回机务引用」在 `restored` 时出现。

```css
.mro-cite-list{display:flex;flex-direction:column;gap:8px;margin:8px 0 0}
.mro-cite{border-left:2px solid var(--tide1);padding:8px 10px;background:var(--bg2);border-radius:0 8px 8px 0}
.mro-cite small{color:var(--muted)}
.mro-context-strip{font-size:12px;color:var(--muted);padding:6px 0}
```

- [ ] **Step 1: 写 Go 测试** 无 cite + 文本含 `件号 NAS1149` → 输出仍含其它叙述（若有）且含「未找到受控依据」，**不等于**仅固定三行拒答；有 cite 不改写件号；综合稿无 DocID → `RestoreCouncilCitations` 附录且 restored=true。

- [ ] **Step 2: 写 vitest** 芯片显示 `修订 42` 与专家名；`discarded=3` 显示「3 块因机尾不适用已丢弃」。

- [ ] **Step 3: 接到 SessionPage**（从 chat 结果 DTO 读 cites；字段名与 handler 对齐，若 DTO 尚无则在 foundation inject 结果上加 `cites` 数组）

- [ ] **Step 4:** `go test ./internal/app/ -run TestGateMRO -count=1` 与 `npm --prefix web run test -- MroCiteList`

- [ ] **Step 5: Commit** `feat(chat): citation chips and non-destructive MRO gate`

---

## Task 5: 条件侧栏 + 工作台壳 + 手册

**Files:**
- Create: `web/src/mro/MroWorkbenchPage.tsx`
- Test: `web/src/mro/MroWorkbenchPage.test.tsx`
- Modify: `web/src/App.tsx`：`type Page` 增加 `'mro'`；`LaunchSidebar` 项目组在资产管理之后条件渲染机务按钮；`page==='mro'` 渲染工作台。
- Create: `api/bridge/v1/mro.aircraft.list.schema.json` 等（若 mro 计划 Task 6 未做，本 Task 一并做最小 list/upsert/manual list/register）

工作台 JSX 结构（不要六个顶 Tab）：

```tsx
<main className="skill-center mro-workbench-page">
  <header className="mro-top">
    <div>
      <h1 className="view-title">{zh ? '机务工作台' : 'MRO workbench'}</h1>
    </div>
    <small>{zh ? '辅助建议，不构成放行' : 'Advisory only. Not a release to service.'}</small>
  </header>
  <div className="mro-context-bar">
    {/* 机尾 select、日期 input、手册 select、MroAskButton */}
  </div>
  <div className="mro-body">
    <nav className="mro-rail" aria-label={zh ? '机务分区' : 'MRO sections'}>
      <button aria-selected={rail==='manuals'}>手册</button>
      <button aria-selected={rail==='fault'}>排故</button>
      <button aria-expanded={moreOpen}>更多</button>
    </nav>
    <section>{/* 手册表或空态 */}</section>
  </div>
</main>
```

空态文案必须是「从一本手册或一个机尾开始」，不能是「先建机尾或先导入一本手册」的命令腔与本文不一致——以复盘规格 §8.2 为准。

P0 更多 → 检查单 / 数据源 / 审计 各一页诚实空态，例如数据源：「先在设置 → 数据源探测连接」。禁止假表。

侧栏按钮：

```tsx
{mroEnabled && <button className={page==='mro'?'active':''} onClick={()=>setPage('mro')} aria-label={zh?'机务工作台':'MRO workbench'}><span>✈</span>{zh?'机务工作台':'MRO workbench'}</button>}
```

`mroEnabled` 来自 Task 1 `isEnabledMroExpert`。`App` 在 load 专家列表后传入 sidebar。测试：专家列表无 mro 或 disabled 时容器 `queryByLabelText('机务工作台')` 为 null。

- [ ] **Step 1: 写 vitest** 空态文案；页眉声明；`mroEnabled=false` 侧栏无按钮（抽 `LaunchSidebar` 可测 props 或对 `project-nav-list` mock experts）。

- [ ] **Step 2: 实现页面 + App 条件入口 + `page==='mro'`**

- [ ] **Step 3:** `npm --prefix web run test -- MroWorkbenchPage`

- [ ] **Step 4: Commit** `feat(ui): gated MRO workbench with fleet context bar`

---

## Task 6: 问月汐

**Files:**
- Create: `web/src/mro/MroAskButton.tsx`
- Test: `web/src/mro/MroAskButton.test.tsx`
- Modify: `web/src/App.tsx` 或工作台 props：`onOpenChat(target)` 复用现有 `setTarget`

```ts
export async function openMroChat(input: {
  sessions: SessionBridge
  projects: ProjectBridge
  experts: ExpertBridge
  mroExpertId: string
  context: MroSessionContext
  prompt?: string
}): Promise<{sessionId: string}> {
  const project = await ensurePersonalProject(input.projects)
  const session = await input.sessions.create({projectId: project.id, title: input.prompt?.slice(0, 80) || (input.context.scenario === 'fault' ? '排故' : '机务手册')})
  // persist context: session.update meta or dedicated bridge session.meta.set
  await input.sessions.update({id: session.id, version: session.version, title: session.title, pinned: session.pinned, /* meta: {mroContext: input.context} 若 DTO 已有 */})
  if (input.experts.mount) {
    await input.experts.mount({projectId: project.id, phaseKey: 'RESEARCH_EVIDENCE', expertId: input.mroExpertId, action: 'mount'})
  }
  return {sessionId: session.id}
}
```

若 `session.update` 尚无 meta 字段：把 JSON 写入 `localStorage` key `lunitide:mro-context:{sessionId}`，inject 侧 WebView 读不到则 **同时** 用 `todo.write` 禁止——改为工作记忆 upsert：Bridge 若无现成 session meta，加最小 `session.context.set` schema（payload: `sessionId`, `mroContext` object；result: `{}`）。优先走这一小 Bridge，不要只靠 localStorage（引擎检索必须看见 tail）。

- [ ] **Step 1: schema `session.context.set` + `session.context.get`**（若已有通用 session extra 则复用，禁止第二套）

- [ ] **Step 2: 写测试** Ask 调用 create 一次、context.set 带 `tailNo`、mount `mro` ULID。无 `mroExpertId` 按钮 disabled，title「先启用航空机务专家」。

- [ ] **Step 3: 实现按钮；排故页把症状作为 `prompt`**

- [ ] **Step 4:** `npm --prefix web run test -- MroAskButton` 与 `npm run verify:bridge`

- [ ] **Step 5: Commit** `feat(mro): ask Lunitide opens mounted chat with tail context`

---

## Task 7: 设置数据源面板

**Files:**
- Modify: `web/src/settings/settingsNav.ts`
- Test: `web/src/settings/settingsNav.test.ts`
- Create: `web/src/settings/DataSourcePanel.tsx`
- Test: `web/src/settings/DataSourcePanel.test.tsx`
- Modify: `web/src/settings/SettingsPage.tsx`：`category==='datasources'` 渲染面板
- 后端按 datasource 计划 Task 1–4；本 Task 只接线。DSN 字段：`host, port, database, user, password, ssl`，提交后 mock/list **不得**再含 password 或 dsn。

`SETTINGS_CATEGORIES` 追加：

```ts
{ id: 'datasources', icon: '▣', label: '数据源', labelEn: 'Data sources', keywords: 'postgres mysql 数据库 只读 连接 dsn 数据源 datasource' }
```

`SETTINGS_NAV_GROUPS` 的能力组 ids 在 `security` 后插入 `datasources`。

面板：列表 + Dialog 添加 + 探测 + 禁用 ConfirmDialog。浏览只显示名称树。

- [ ] **Step 1: settingsNav 测试** 搜索 `postgres` 命中数据源；能力组含该 id。

- [ ] **Step 2: DataSourcePanel 测试** list 两项；探测按钮存在；渲染结果 JSON 用 fixture **断言不出现** `postgres://` 与 `password`。

- [ ] **Step 3: 接入 SettingsPage**

- [ ] **Step 4:** `npm --prefix web run test -- settingsNav DataSourcePanel`

- [ ] **Step 5: Commit** `feat(settings): readonly datasource center without echoing DSN`

---

## Task 8: 未受控 / 缺陷确认条

**Files:**
- Modify: `web/src/session/pendingMemory.ts` 或新建 `web/src/session/pendingConfirm.ts` 共用 preview
- Test: `web/src/session/PendingMemoryBanner.test.tsx` 增补
- Modify: 工作台导入未受控：先 `ConfirmDialog`（规格 §8.2），对话用到未受控块时发与记忆相同的 pending 项：`content` 前缀 `待确认：将使用未受控手册回答`

按钮文案保持「确认沉淀」「以后再说」，不要「同意放行」。

- [ ] **Step 1: 测试** banner 对机务 pending 仍显示那两个按钮，文案不含「放行」。

- [ ] **Step 2: 接入导入 Dialog + 对话 pending 类型扩展（`kind: 'memory'|'mro-uncontrolled'|'mro-defect'` 若需要；UI 仍一个 Banner）**

- [ ] **Step 3: Commit** `feat(mro): reuse memory confirm language for uncontrolled manuals`

---

## Task 9: 切片验收

对照复盘规格 §10 切片 2–3 与本文 Task 1–8。

- [ ] **Step 1:** `rg "mro-expert" web/src/expert/conversationExperts.ts internal/m8app/catalog/conversation_experts.json` 有命中
- [ ] **Step 2:** `rg "page==='mro'" web/src/App.tsx` 有命中；侧栏按钮在 `mroEnabled &&`
- [ ] **Step 3:** `rg "整段|fixedRefusal|未找到受控依据。请提供机尾" internal/app/chat_citation_gate.go` 不得把整段固定拒答当作唯一返回（允许 rewrite 函数）
- [ ] **Step 4:** `npm --prefix web run test -- expertIds conversationExperts ExpertKnowledgePanel ExpertGrowthPanel MroWorkbenchPage MroCiteList MroAskButton DataSourcePanel`
- [ ] **Step 5:** `go test ./internal/app/ -run "TestGateMRO|TestChatKB" -count=1`
- [ ] **Step 6:** `npm run verify:bridge`
- [ ] **Step 7:** 手工对照：停用机务专家 → 侧栏入口消失；知识页人设卡无交文件；成长空态无「正在补」

不要为了绿而造 50 条黄金命中率。

---

## Spec coverage

- H1 条件侧栏：Task 5
- H2 身份：Task 1
- H3 引用门：Task 4
- H4 出站摘录：Task 2 inject
- H5 P0 收窄：本计划无六 Tab、无 50 条指标
- H6 三页签：Task 3
- H7 mroContext：Task 2、6
- H8 手册聚合：工作台按 `manual_id` 列表（mro 计划表 + Task 5）
- H9 数据源：Task 7；专家工具白名单在 mro 卡 requiredTools
- H10 pack：foundation 检索过滤，本计划知识页只展示本专家计数
- H12 确认语言：Task 8
- FR-A2/A3/A4 UI：Task 3
- FR-B1 上架：prerequisite mro 计划 Task 1 + 本 Task 1
- FR-C1 空态与入口：Task 5
- FR-D1 设置：Task 7
- FR-E1 页眉：Task 5
- 打开工作台按钮：Task 3
