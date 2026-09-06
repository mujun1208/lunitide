# 日常任务能力整改 · P1 复盘落地 PRD

日期：2026-09-05  
版本：复盘落地版（对照现码重写）  
状态：已批准并落地（2026-09-05，`feat/daily-ops-p1`）。不装 sqlite-vec；不改 VERSION / `v0.4.62`。  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  

**P1 本文是唯一事实源。** 取代同日上一份 P1 稿里所有未复盘的步骤。  
P0 锁与已完成项只认 [2026-09-04-daily-ops-executable-prd.md](./2026-09-04-daily-ops-executable-prd.md)。  
superseded：`2026-09-04-daily-ops-capability-prd.md`、`2026-09-04-capability-slots-design.md`。  
P0 计划：`docs/superpowers/plans/2026-09-04-daily-ops-capability.md`。P1 按本文 §5 直接落地，不再另写计划。

沿用冻结：不新 daemon；内核 SQLite；不改 poison / 说完再答 / 月伴 TTS / 专家表；`desktop.open` 成功=前置后前台命中；电脑控制不自开；保护会话不可删改名；不假成功 `ENGINE_UNAVAILABLE`；不移动 `v0.4.62`；不重写 `AnnotateCapture` / `assignNodeIDs`；不重写凭据提交协议（Windows + `secretlease` + `submissionId` + Host `Mutate`），只**加第三条分支**；同一人不要同时改 `SessionPage.tsx` 与 `CompanionStage.tsx`。

---

## 0. 产品现状（2026-09-05 现码，复盘基准）

月汐是本机助手：Go Engine + React WebView2 + 单文件 SQLite。对话工具面由引擎组装，桌面只有一只手 `computer.act`。技能、MCP、专家库是另外三层货，不是电脑控制的货架。

### 0.1 P0 已落地（禁止当缺口重做）

| 能力 | 现码 | 测试 |
|---|---|---|
| 五路收面 | `task_route.go` `classifyTaskRoute` / `applyTaskRoute`；`chat.go` 在 `filterCompanionDefaultTools` 之后接线；`state.taskRoute` | D-A1–A7 |
| flash / judge 启发式 | `flash_model.go` `pickJudgeModelID` | D-C1 |
| plan.verify + l0 | `chat_plan.go` `extractL0` / `decidePlanVerify` | D-C1–C3 |
| 先 observe 再 xy | `requireObserveBeforeXY`；空树生产路径**禁止**裸 xy（`allowGUIPixels` 仅测试） | D-I1–I2 |
| 无名只点 ID | `resolveNamedTarget` / `InvokeUI` | D-J1/J3 |
| observe truncated | `observeUIPayload` | D-K1–K2 |
| verifyAfter 700ms | `mutateSettleWait` | D-L1–L2 |
| user.ask reason + 登录墙/UAC/另存为停车 | `chat_tool_defs.go`；`parkUACAsk` / `parkBrowserWallAsk` / `parkFilePickerAsk` | D-G* |
| CJK → paste | `PreferPasteText` | D-O1 |
| desktop.type 回读 / media L0 / 拒点月汐窗 | `verifyDesktopTyped` / `attachMediaL0` / `refuseSelfWindowPixels` | 部分有测 |
| 纯打开 settle | `companionGoalIsOpenOnly` | D-H1 |
| 子代理无 CC | `readOnlyEngineToolDefinitions`；`expertWriteToolDenied` | 现网 |

`classifyTaskRouteWithFlash` **不存在**（P0 正确）。

### 0.2 现码形状（P1 必须接，禁止平行实现）

```
用户句
  → autoToolProfile → 挂 plan/mcp/cc/skill/expert
  → filterCompanionDefaultTools
  → classifyTaskRoute + applyTaskRoute → state.taskRoute（只活在本次 chat.start）
       白名单视频分享链（抖音/腾讯视频/B站/YouTube）且非显式浏览器意图 → **仍走 R1**
  → withProviderLease(p.CredentialRef) 包住整段 Stream/工具环
       工具失败 → ok:false 写回 messages，环继续
       特殊失败 → parkUACAsk / parkFilePickerAsk / parkBrowserWallAsk，然后 return
  → 无 GUI 兜底钩子
```

观察帧图已经在成功工具结果上：`Result.VisionData` → `appendCaptureVision(req.Images)`。  
L0 是工具输出末行 JSON：`{"l0":{kind,passed,uncertain,detail}}`。

### 0.3 技能 / MCP（对照，P1 不改）

| 层 | 现码 | 含义 |
|---|---|---|
| 技能目录 | `skillCatalogInjection` 只注入名称+触发词+一句；`skill.match` 词面；`skill.catalog.list` 捆绑市场 | 已是「目录 ≠ 正文 ≠ 调用」 |
| MCP 进货 | `mcp.presets` / `mcp.install` / `mcp.add`；装后 `mcp6.DescribeFunc` | 渠道在 |
| MCP 市场 | `mcp_market_items` LIKE + 签名缓存；按需拉，非官方 Registry 日更 | 清单不用装，但不是 ETL |
| 专家库 | `kb.search` 只 FTS（`IndexVersion=fts5-trigram`）；`PutKBChunks` 写 `embedding=NULL` | P1 只在这里加向量 |
| 记忆 | `memories.embedding_id` 是 ULID 指针；`memory_inject` 词面+FTS | P1 不改公式 |

外来「工具层做成 sqlite-vec 搜索引擎」对 **`computer.act` 无帮助**（缺的是路由和传感器）。对技能/MCP 目录同构，**P2 开门**，见 §12。

### 0.4 Kind / 凭据 / Gateway（P1 会踩的现状）

- `NormalizeKind` 未知 → **llm**。`gui`/`embedding` 漏写 case = 目录污染。  
- `persistKind` 未知 → **llm**。前端同样会塌。  
- `Adapter` = Complete/Stream/Discover。可选接口先例：`ImageGenerator`（`model_catalog.go` type-assert）。  
- `withProviderLease` **永远**用 `p.CredentialRef`。  
- `HostHandler.Mutate` 只分 `provider.create` / 否则当 `provider.update`。  
- `provider.credential.submit` 的 token 契约写明只给 create/update；digest 绑的是那份 request。  
- `IsCredentialReferenceAdopted` 只认 `providers.credential_ref`。  
- `allowGUIPixels` 只有 `SetAllowGUIPixelsForTest`。  
- `KBChunk` **没有** Embedding 字段；`GetKBChunk` 不读 blob。  
- 知识入库 `PutKBChunks` 在 **SQLite 事务内**（`m8app/kb.go`）。事务里打 HTTP Embed 会锁库。  
- 新 Bridge 方法必须改：schema、`envelope.schema.json`、`web/scripts/generate-bridge.mjs` **硬编码 enabled 清单**、生成三件套、`handlers_registry.go`；写方法还要 `MutationMethod` + `mutationMethods`。  
- 最新迁移 `0118`。加列必须同步 `store.go` 的 `expectedSchemaSQL` + `expectedColumns` + List/Get/Insert/Update SQL。

### 0.5 对话贴视频链（现码缺口，P1-8 补）

用户在打字/月伴里贴抖音、腾讯视频、B站、YouTube 链接，期望的是**解读、总结、反馈**，不是打开浏览器代播。

| 现码 | 结果 |
|---|---|
| `web.fetch` | 只 `webfetch.ExtractText`（跳过 `script`）。视频页多是 SPA，正文几乎没有字幕 |
| `video.generate` | **生成**视频，不是理解。`kind=video` 仍是生视频 |
| `gateway.Request` | 只有 `Images`，无视频帧；附件 MIME 仅 png/jpeg/webp |
| `siteHints` 含 `http`/`bilibili`/`youtube` | 「打开 + 站点」→ **R3 `browser.act`** |
| 单独一条分享链 | 无「天气/打开」→ **RouteUnspecified**，全工具面，模型可能 `browser.act` / `computer.act` |
| 抖音分享口令 | 文案常带「复制**打开**抖音」→ 现规则会当成 R3 |
| `youtube-transcript-mcp` | 只是可选 preset，未默认安装，也不覆盖 B站/抖音/腾讯 |

P1 **不**下载成片、不接 yt-dlp/ffmpeg、不新 daemon、不把正片送进模型。能落地的是：白名单链 → SSRF 钉死拉取 → 公开标题/简介/字幕（有才用）→ 诚实总结。

---

## 1. 复盘勘误（上一份 P1 稿不能直接开工）

| ID | 上一稿问题 | 落地锁死 |
|---|---|---|
| R-P1-1 | 只加 Kind 常量 | `NormalizeKind`/`ValidKind`/`persistKind`/`modelKind` 四面一起改。D-D1/D-D10 先红。 |
| R-P1-2 | 「改 chat_run_stream 只接函数」 | 现码工具失败只写回 messages。必须**新加控制流**：每轮最多一次 `tryGUIFallback`，仿 `parkUACAsk`。纯函数不够。 |
| R-P1-3 | 空树 GUI 坐标 | 生产 `requireObserveBeforeXY` 在 `count==0` 仍拒绝。P1 要生产 API `SetAllowGUIPixels(bool)`，**仅**在 GUI 空树动作前后打开，defer 关。模型调不到。 |
| R-P1-4 | 路由可从回合恢复 | `taskRoute` 只在 `streamState`。P1 读 `state.taskRoute`。恢复/续跑从 `turn.Goal` **重算** `classifyTaskRoute`，不持久化路由列。 |
| R-P1-5 | 备份 Key 只写引擎 Handler | `submitCredential` digest 绑 create/update；`Mutate` 无第三分支；adoption 不认备份 ref。必须扩 Host 链，见 §4.4。 |
| R-P1-6 | 429 在 `classifyStreamError` 后换把 | 整段 Stream 已在一个 lease 里。必须在 lease **回调内**、**尚未发出 EventDelta** 时换 ref 重入 `withProviderLeaseRef`。已流出字则不换，回现网 `PROVIDER_RATE_LIMITED`。 |
| R-P1-7 | 入库时顺手 Embed | 入库在 SQLite 事务里。Embed **事务外**，成功后再 `UPDATE kb_chunks.embedding`。失败留 NULL。 |
| R-P1-8 | 漏改生成器硬清单 | `generate-bridge.mjs` 第 53 行起有一份 enabled 数组，必须按字母序插入。漏一行 `verify:bridge` 红。 |
| R-P1-9 | 漏改 `publicProvider` | DTO 字段要同时改 `engine.go` `providerDTO` + `publicProvider` + schema。 |
| R-P1-10 | `capability.list` 当角色表 | 那是 M4 manifest。新方法 `capability.roles.get/set`。 |
| R-P1-11 | flash 分类默认开 | 仅当 flash 角色**显式**有 `model_id` 才跑。空 = P0 只规则。 |
| R-P1-12 | GUI 当模型工具 | 模型看不见 GUI。运行时兜底。子代理 / R0/R1/R4 不进。 |
| R-P1-13 | SoM 整数 / 散文抠号 | 只收单个 JSON。`{"markId":"B1"}`。 |
| R-P1-14 | drag 已能 id | `MapComputerAct` drag 仍要四坐标。P1 加 `id`/`id2`。schema 加 `id2`。 |
| R-P1-15 | 现成 UIPI 文案 | 没有。把 access denied / elevated / 跨完整性写进 `looksLikeUACToolResult`，复用 `parkUACAsk`。 |
| R-P1-16 | memories 开向量库 | 不。只 `kb_chunks.embedding`。`memory_inject` 不改。 |
| R-P1-17 | Embed 焊进 Adapter | 可选 `Embedder`，仿 `ImageGenerator`。仅 `openai_compatible`。 |
| R-P1-18 | sqlite-vec / Registry 日更 / CUA MCP | **不进 P1。** §12。 |
| R-P1-19 | 加列就过 | `0119` + `store.go` 三处快照 + 全部 providers SQL。新表同样进 dump。 |
| R-P1-20 | volc 挂 embedding | `Validate` 已拒非语音。保存必须失败。 |
| R-P1-21 | `maybeDescribeImages` 兼读号 | 那是 OCR/描述。SoM 用新 `guiSomPickPrompt`，禁止复用 `visionDescribePrompt`。 |
| R-P1-22 | 用 `web.fetch` 当看视频 | `ExtractText` 丢掉 `script`，拿不到 `__INITIAL_STATE__` / 字幕 JSON。新包 `videounderstand`，**禁止**改 `skipContentTags`。 |
| R-P1-23 | 「打开」一律 R3 | 抖音口令自带「复制打开抖音」。有白名单视频链时默认 **R1 + `video.understand`**；只有显式浏览器意图才 R3。 |
| R-P1-24 | 播放+抖音链走 `media.play` | `media.play` 是本机播放器。白名单视频链 +「播放」仍 R1，诚实说不能代播。 |
| R-P1-25 | 新 `kind=video-understand` / 正片进网关 | `kind=video` 仍是生成。不扩 `gateway.Request` 视频字段。不装 yt-dlp。 |

---

## 2. 目标与非目标

**目标：** 两个新 kind 一次改对；无 GUI 也能 SoM 读号；有 GUI 只兜底；专家库可配向量且 FTS 不撤；主模型在**尚未吐字**时遇 429 能换备用 Key；六个角色可配；`drag` 能从 id 起手；拒附/UIPI 停给人；对话里贴白名单视频分享链能**解读/总结**并诚实标明材料来源。

**非目标（反向用例）：**

- 不新开 UI-TARS Desktop / vLLM / 向量进程；Setup 不夹带 GUI/embedding 权重。  
- 不装 sqlite-vec；不做官方 MCP Registry 日更；不把 Computer-Use MCP 搜进对话工具表。  
- 不改 `memory_inject`；不新 Mem0。  
- 不重写凭据协议，只加 backup 分支。  
- 不让子代理 / 同事画像 / 机务专家拿到 `computer.act` 或 GUI。  
- 不改 poison / TTS / `CompanionStage` 视觉 / VERSION / `v0.4.62`。  
- 不把「截屏」当主干。  
- 不下载成片、不接 yt-dlp/ffmpeg、不新视频理解 daemon、不把正片/音频送进网关。  
- 不默认安装 `youtube-transcript-mcp`；不把「看视频」做成 `browser.act` 代播。  
- 不扩小红书/快手等未点名站点（P2）。  
- 不说「我看完了」——没有公开字幕就只能根据页面简介。

---

## 3. 目标架构

```
P0（保持）：
  句 → 规则路由 → 收面 → 模型选工具 → l0 → 败则观察 → 有树只点 id

P1 接上（同一 chat.start）：
  1) 规则未命中且 flash 角色显式绑定
        → classifyTaskRouteWithFlash（只用 flash；非法 JSON → nil allow）
  2) 工具环里 desktop.type / computer.act 结构化失败
        且未 park UAC/文件/登录墙
        且本轮还没跑过 GUI 兜底
        且 route∈{R2,R3} 且 ccOn 且非子代理
        → 若本轮还没有成功 observe：先内部 observe（不另问模型）
        → pickGUIFallback
              none → 维持 ok:false，环继续
              gui/vision + 有节点 → Complete+Images，只许 {"markId":"B1"}
              gui + 空树 → {"x","y","frameId"} ∈[0,1] 或 0–1000
        → 校验后由运行时调 computer.act（模型不再选）
        → 再 L0
  3) kb.search：有 embed 目录 → FTS∥向量；无目录/失败 → 现网 FTS
  4) Stream 第一次失败且 0 delta 且 429/quota → 换下一把 CredentialRef 再 lease
        最多换 3 把；401 不换；成功不持久化晋升
  5) 句中检出白名单视频分享链（且非显式浏览器意图）
        → 仍标 R1（不新路由常量）
        → allow 增 `video.understand`；禁止 browser.act / computer.act / media.play
        → 工具：SSRF Fetch → 解析 og/JSON-LD/播放器状态 → 有公开字幕再拉字幕
        → 结果带 source=captions|page_meta|mixed|empty；模型按此说话
```

角色（空 `model_id` = 自动）：

| Role | 允许 kind | 空 | 绑定后 |
|---|---|---|---|
| chat | llm | 会话选择器赢；只作「无会话模型」缺省，不中途替换 | 仅缺省 |
| flash | llm | `pickFlashModelID`；**不**开 flash 分类 | plan.verify + 显式绑定才分类 |
| vision | vision 或 llm+`supportsVision` | `VisionDescribeCatalog` 第一项 | describe 与 SoM |
| embed | embedding | 不 Embed | 事务外写向量 + kb 混合 |
| judge | llm | `pickJudgeModelID` | 验证 Complete；=chat 必须勾选 |
| gui | gui | SoM（有 vision）或停 | 只兜底 |

`allow_judge_eq_chat` 只对 judge 有意义。

---

## 4. 契约（按现码改过）

### 4.1 Kind

`ModelDTO.kind` 增加 `embedding`、`gui`。

同步一次做完（P1-1）：

1. `api/bridge/v1/public.dto.schema.json`  
2. `npx`/`npm run generate:bridge`（生成 `schema_generated.go`、`schema_generated_test.go`、`web/src/generated/bridge.ts`）  
3. `kind.go`：`KindEmbedding` `KindGUI`；`NormalizeKind`/`ValidKind` 加 case  
4. `modelKind.ts`：

```ts
export type ModelKind = 'llm' | 'vision' | 'image' | 'video' | 'voice' | 'embedding' | 'gui'
export type PersistKind = 'llm' | 'vision' | 'image' | 'video' | 'asr' | 'tts' | 'embedding' | 'gui'
```

`persistKind('embedding'|'gui')` 原样。`modelKind`：asr/tts→voice，其余=persist。  
文案：向量模型 / GUI 模型。`MODEL_KINDS` 加这两项。volc 编辑器仍只 `VOICE_ROLES`。  
`Provider.Validate`：volc 拒绝非语音；每种 EffectiveKind 至多一个 kindDefault。

### 4.2 能力路由

迁移 `0119_capability_roles_and_key_backups.sql`：

```sql
CREATE TABLE capability_role_bindings (
  role TEXT PRIMARY KEY CHECK (role IN ('chat','flash','vision','embed','judge','gui')),
  provider_id TEXT,
  model_id TEXT,
  allow_judge_eq_chat INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

ALTER TABLE providers ADD COLUMN credential_ref_backups TEXT
  NOT NULL DEFAULT '[]'
  CHECK (json_valid(credential_ref_backups));
```

空表合法。`provider_id`/`model_id` 同时空或同时非空。绑定必须能在当前目录解析且 kind 符合 §3。

`capability.roles.get` payload `{}`。result 永远 6 行（缺行当自动）。  
`capability.roles.set` 整表替换 6 行。`additionalProperties:false`。  
judge 的 modelId 等于 chat/当前会话模型且 `allowJudgeEqChat!=true` → 业务码拒绝（不要用 schema 卡死）。

`x-owner: engine`。set 加入 `MutationMethod` + `mutationMethods`。

UI：`SettingsPage` 的 `category==='providers'` 改成片段：先 `CapabilityRouting`，再 `ProviderApp`。不新 `SettingsCategory`。`settingsNav` providers keywords 加「能力路由 向量 GUI judge」。

解析：`resolveRole(role) (providerId, modelId)`。plan.verify 改走 resolveRole(judge)，空则 `pickJudgeModelID`。

### 4.3 Embedder 与 kb.search

```go
type Embedder interface {
    Embed(ctx context.Context, secret []byte, model string, texts []string) ([][]float32, error)
}
```

`internal/gateway/openai_embed.go`：`POST {base}/embeddings`。Anthropic / volc 不实现。

BLOB：`uint32le dim | float32le[dim]`。维数 0 或不一致 → 该行跳过向量支路。

**写入时序（锁死）：**

1. 现网入库事务照旧，`PutKBChunks` 仍可写 NULL。  
2. 事务提交后，若 embed 角色可解析且协议是 openai_compatible：lease（`OperationChat` 或 `OperationProviderTest`，不新 enum）→ Embed(bodies) → **第二笔**短事务 `UPDATE kb_chunks SET embedding=? WHERE chunk_id=?`。  
3. 任一步失败：chunk 可读，embedding 仍 NULL。不回滚正文。  
4. 禁止在 `TransactKB` 里打 HTTP。

`KBChunk` 增加可选 `Embedding []byte`。`GetKBChunk`（cite）可以继续不读 blob。新方法 `ListKBChunkEmbeddings(scope)` 供检索。`SearchKBChunkFTS` SQL **不改**。

检索（`m8app/kb_search.go`）：

- 无目录 / 全 NULL：现网 FTS，`IndexVersion=fts5-trigram`。  
- 有向量：FTS 与向量并行；融合 `0.5*fts + 0.4*cos + 0.1*recency`（`created_at` 按年衰减）；MMR λ=0.7。  
- `IndexVersion=fts5+dense-v1`，`explanation.Reasons` 仍含 `fts5 body match`。

`provider.test`：若 `modelId` 的 kind 是 embedding，type-assert Embedder，短文本一条；不要 Complete。

Discover/同步：不自动标 embedding。用户手改 kind。

### 4.4 Key 备份（Host 链必须扩）

`credential_ref_backups`：JSON 字符串数组，0–4，与第一把不重复。禁止明文出网。

`ProviderDTO` / `providerDTO` / `publicProvider` 只加 `credentialBackupCount`（0–4）。

**提交协议（扩，不重写）：**

1. UI 调现网 `provider.credential.submit`：  
   - `scope.providerId` 已有供应商  
   - `request` = **即将调用的** `provider.credential.backup.add` 公共 payload（无 submissionId）  
   - digest 绑这份 request，不是 provider.update  
2. 再调 `provider.credential.backup.add`：`{providerId, credentialSubmissionId, expectedVersion}`  
3. Host `Mutate` **第三分支**（显式判断 method，禁止落入 update）：  
   - create → 现网  
   - update → 现网  
   - `provider.credential.backup.add` → `internal.provider.backup.with-credential`  
4. 内部 handler：append ref 到 backups，**不改** `CredentialRef`，adoption 写入，version++。  
5. `IsCredentialReferenceAdopted`：第一把 **或** backups JSON 含该 ref 都算已采用。  
6. `provider.credential.backup.remove`：`{providerId, index, expectedVersion}`。只删备份。引擎方法即可（无新 secret）。  
7. 满 4 拒绝。  
8. `generate-bridge.mjs` 的 owner 断言：`backup.add` 与 `provider.update` 一样走 **engine + Host 拦截**（和 create/update 同一桌面入口），`submit` 仍是 host。  
9. `provider.credential.submit` schema 的 token 说明改为：create / update / **backup.add**。  
10. `ProviderBridge` 加 `backupAdd` / `backupRemove`。两者进 `MutationMethod`。

**轮转：**

```go
func (e *Engine) withProviderLeaseRef(ctx, p, ref, op, fn) error
// ref 空则 p.CredentialRef
```

顺序：第一把 → backups[0]…  
同一次 `chat.start` 最多换 3 把（最多 4 次尝试）。  
仅当：错误匹配 `429` / `insufficient_quota` / `\bquota\b`（大小写不敏感）**且**尚未 `send(EventDelta)` **且** assistant/thinking 缓冲为空。  
401 → 现网 `PROVIDER_AUTHENTICATION_FAILED`，不换，不改 `credential_state`。  
成功不改第一把。  
plan.verify / 专家 council / 子代理 Complete：共用本轮计数器，不每步重置；同样遵守「已吐字不换」。

### 4.5 GUI / SoM

```go
type guiExecutor string // none | gui | vision

func pickGUIFallback(in guiFallbackIn) guiExecutor
```

输入：`ccOn, isSubagent, route, nodeCount, desktopTypeL0Passed, guiCatalog, visionCatalog`。

| 条件 | 结果 |
|---|---|
| !ccOn / isSubagent / R0/R1/R4 / 本轮已兜底 | none |
| desktopType 本轮 L0.passed | none |
| nodeCount>0 && guiCatalog | gui |
| nodeCount>0 && vision | vision |
| nodeCount==0 && gui | gui（坐标） |
| else | none |

接线（`chat_run_stream.go` 工具 for 循环之后、park 之前）：

1. 本轮 `guiFallbackUsed` 标志，最多一次。  
2. 触发：刚完成的工具是 `desktop.type` 或 `computer.act`，且 `toolErr!=nil` 或 summary 含 `ok:false`，且 L0 非 passed，且未 park。  
3. 若 `observedFrameID==""`：运行时 `computer.act action=observe`（内部，不经模型）。失败 → none。  
4. `Complete`+`Images`（最近一帧 VisionData）。提示：`guiSomPickPrompt`（选择题，禁止描述散文）。  
5. 只解析**一个** JSON 对象。有树：`markId` 字符串且 `lookupHit`。空树：`x,y,frameId`；`x,y`∈[0,1] 或 (1,1000] 当 0–1000；`frameId` 必须等于本轮 `observedFrameID`。  
6. 运行时执行 `computer.act`。空树坐标前后 `SetAllowGUIPixels(true)`，defer false。  
7. 再走 L0。成功则把结果当 ToolCompleted 发给模型。失败维持无法执行+观察。

页空态：GUI 是兜底。草稿「接入本机 UI-TARS」预填 `http://127.0.0.1:1234/v1` 或 `:8000/v1`，kind=gui，Key=`lm-studio`。写明不带权重。  
向量页：要 OpenAI 兼容 `/embeddings`；未配继续 FTS。

### 4.6 drag / UIPI

`computer.act` schema 增加 `id2`（string, maxLength 8）。  
drag：`id` 起点（`lookupHit`）；终点 `id2` 或 (x2,y2)。有 `id` 可省 x1,y1。无 observe 与裸 xy 同失败。不新工具名。scroll 后再 observe：补回归。

UIPI：host 附加/Invoke 失败且像 access denied / elevated / 跨完整性 → `ErrCcRiskBlocked` + 现网 UAC 句式。扩展 `looksLikeUACToolResult`，走 `parkUACAsk`。`user.ask` reason 已有 `uac`。不新事件。

### 4.7 视频链接解读（P1-8）

**不新路由常量。** 复用 `RouteR1`。GUI 兜底对 R1 已是 none（D-D5）。`kind=video` / `video.generate` 不动。

**工具名：** `video.understand`（引擎工具，**不**加 Bridge 方法，**不**写 `fetch.html` 产物，避免工作区浏览器空页）。

```json
{"type":"object","properties":{"url":{"type":"string","maxLength":2048}},"required":["url"],"additionalProperties":false}
```

**分享链检出** `detectVideoShareURL(goal) (canonical, platform, ok)`：

用户句里找 `http(s)://` **或**无 scheme 的短链。平台锁死（P1 只这四家）：

| platform | 分享 Host（用户可贴） |
|---|---|
| bilibili | `bilibili.com` `www.bilibili.com` `m.bilibili.com` `b23.tv` |
| douyin | `douyin.com` `www.douyin.com` `v.douyin.com` `iesdouyin.com` `www.iesdouyin.com` |
| tencent | `v.qq.com` `video.qq.com` `m.v.qq.com` |
| youtube | `youtube.com` `www.youtube.com` `m.youtube.com` `youtu.be` |

子域匹配用 **精确 host 或 `www.`/`m.`/`v.` 前缀表**，禁止 `*.qq.com` 这种过宽后缀。

**路由（写进 `detectTaskRoute`，顺序锁死）：**

1. 现网：info+namedApp → R2；info → R1。  
2. **`detectVideoShareURL` 命中：**  
   - 显式浏览器意图 → R3。意图只认：`打开浏览器` / `用浏览器` / `在浏览器` / `上网打开` / chrome|edge|firefox + 打开 / `登录`/`登陆`（且不是「登录后看」这种总结句）。  
   - **「打开」单独不算。** 「复制打开抖音」「打开抖音看看」「打开B站」+ 分享链 → **R1**。  
   - 「播放/暂停」+ 分享链 → **R1**，不是 R2。  
   - 其余（光贴链、解读/总结/分析/讲了什么/什么内容/帮我看看）→ **R1**。  
3. 现网：site+打开（无视频链）→ R3；gen → R4；play → R2；…  

「打开B站」**无 URL** 仍走现网 R3。P0「打开 Chrome 上 12306」不变。

**R1 allow 加一行：** `video.understand: true`。`web.search`/`web.fetch` 仍在。禁止 `browser.act` / `computer.act` / `media.play`。`applyTaskRoute` 不把 `video.understand` 当专家挂载例外。

R0 / R2 / R3 / R4 **不加**此工具。R0 误伤防护：`autoProfileTaskHints` 已含 `http`；P1 再加 `b23.tv` `v.douyin.com` `youtu.be` `bilibili.com` `v.qq.com`，避免无 scheme 短链被收成闲聊。

**管道（`internal/videounderstand`，网络只走 `networkpolicy.Fetch`）：**

1. 解析 url；分享 Host 不在表 → `ok:false reason=unsupported_host`。  
2. Fetch HTML（建议 `MaxBodyBytes=2MiB`）。每跳再验分享 Host。重定向出表或进内网 → 失败（现网 SSRF）。  
3. **新解析器**读 og / twitter / JSON-LD `VideoObject` / 已知播放器状态（B站 `__INITIAL_STATE__`、YouTube `ytInitialPlayerResponse`）。**禁止**改 `webfetch.skipContentTags`。  
4. 若页面给出公开字幕 URL：第二跳 Fetch **仅**字幕。字幕 Host 白名单与分享表分开：  
   - B站：host 后缀 `.hdslb.com`（仍走 pinned Fetch，禁私网）  
   - YouTube：仍在 youtube.com（`api/timedtext`）  
   - 抖音/腾讯：P1 不猜私有签名 API；没有公开字幕就 `page_meta`  
5. 字幕正文 cap 与 `webfetch.MaxTextBytes` 同级（32KiB），超了标 `captionsTruncated`。  
6. 登录墙/验证码/空壳 SPA：`ok:false`（`login_wall` / `captcha` / `empty_page`）。**禁止**改 `browser.act` 点过去。模型可 `user.ask` 让用户贴文案。  
7. 可选：有 vision 目录且 `og:image` 拉到了，可 `Complete+Images` 描述**封面**，结果里必须写 `coverDescribed=true`，文案当封面不是正片。  
8. 工具回写形状（模型必须按 `source` 说话）：

```
ok: true|false
platform: bilibili|douyin|tencent|youtube
source: captions|page_meta|mixed|empty
reason: 失败码（仅失败）
title: ...
author: ...
finalUrl: ...
disclaimer: 这不是逐帧看完视频。根据公开字幕/页面简介整理。
```

`source=page_meta|empty` 时，用户可见回复禁止「我看完了」「我看了整段」。应说「根据标题和简介」。

**月伴：** `companionToolLeadIn("video.understand")` = 「好，我先看下这个链接。」  
`companionPersonaToolsInstruction` 加一条：贴了抖音/B站/腾讯视频/YouTube 链必须 `video.understand`，不要 `browser.act`，不要 `media.play` 代播；没字幕就按简介说。  
`companionRedundantWebSkip` **不要**把 `video.understand` 当第二次天气搜索跳过。

**子代理 / 专家：** 不把 `video.understand` 加进只读研究面，除非本轮 R1 已经收面带上。禁止研究子代理用它绕过浏览器墙去点登录。

---

## 5. 详细步骤（TDD，按包，可直接做）

每步：先写失败测试 → 跑红 → 最小实现 → 跑绿。不要跨包夹带。改 schema 后立刻 `npm run generate:bridge` 再 `npm run verify:bridge`。

### P1-1 kind + schema

**文件：** `public.dto.schema.json`；`kind.go` `provider.go` `provider_test.go`；生成三件套。

1. 在 `provider_test.go` 加 `TestNormalizeKindGUIAndEmbedding`：断言 `NormalizeKind("gui")==KindGUI`、`embedding` 同理、未知仍 llm、`ValidKind` 真、`CatalogForKind(gui)` 在只有 llm/vision 夹具下为空、`VisionDescribeCatalog` 不含 gui 行（D-D1/D-D2）。跑红。  
2. 改 `kind.go` case。跑绿。  
3. 改 schema 枚举。`npm run generate:bridge`。`verify:bridge` 绿。  
4. `volc` 夹具保存 embedding → Validate 失败（D-D11）。  
5. `go test ./internal/domain/provider ./internal/contract`。

### P1-2 页签

**文件：** `modelKind.ts` `modelKind.test.ts`；`ProviderApp.tsx` `ProviderApp.test.tsx`。

1. 测 `persistKind`/`modelKind` 对 gui/embedding 不塌 llm（D-D10）。红。  
2. 改类型与函数。`MODEL_KINDS` 加上。文案 D-D3。  
3. `llmReadyProviders` 夹具含 gui/embedding 不得入选（D-D4）。现网 `modelKind.test.ts` 全绿。  
4. ProviderApp：空态文案（GUI / 向量）；类型下拉非 volc 含新 kind。  
5. `npx tsc --noEmit`（web）。

### P1-3 GUI 兜底

**文件：** 新 `gui_fallback.go` `_test.go`；`chat_run_stream.go`；`chat_tool_defs.go`；`ccapp/service.go`（生产 `SetAllowGUIPixels`）；不改 AnnotateCapture。

1. 表测 `pickGUIFallback`：D-D5/D-D6/D-D8/D-D12/D-N1。红。  
2. 实现纯函数。绿。  
3. 解析测：只认 `{"markId":"B1"}`；整数失败；空树坐标+frameId（D-D7/D-D9）。  
4. `SetAllowGUIPixels` 生产方法；测：false 时空树 xy 仍失败，true 时过，调用后能关。  
5. `chat_run_stream`：加 `guiFallbackUsed`；失败钩子；内部 observe；Complete+Images；运行时 click。测：R1 不进；子代理不进；本轮只一次。  
6. `guiSomPickPrompt` 常量，禁止抄 `visionDescribePrompt`。  
7. 回归 `./internal/app` `./internal/ccapp`（C7–C9、observe_gate、UAC park）。

### P1-4 Embedder + kb

**文件：** `gateway/types.go`；新 `openai_embed.go` `_test.go`；`m8core/kb.go`（可选 Embedding）；`m8_kb.go`；`m8app/kb.go`（事务后 embed）；`kb_search.go`；`kb_search_handlers.go`；`provider_diagnostics.go` test 分支。

1. 假 HTTP：embeddings 请求形状、维数头编解码。  
2. D-E1：无目录时现网 `kb_search_handlers_test` + `m8app` FTS 绿（先跑，作门禁）。  
3. D-E3：Embed 失败仍 FTS。  
4. D-E4：Anthropic 绑 embed → 跳过写。  
5. D-E2：夹具两 chunk（关键词 ATA vs 同义句）融合后两路都在。  
6. 断言入库事务内没有 HTTP（测：projector 路径提交后 embedding 仍可异步/二次事务写入）。  
7. `provider.test` embedding 行走 Embed。  
8. `go test ./internal/gateway ./internal/m8app ./internal/storage/sqlite ./internal/app`（kb）。

### P1-5 Key 备份

**文件：** `0119_…sql`；`store.go` `uow.go`；`public.dto.schema.json`（count）；新 backup.add/remove schema；`envelope.schema.json`；`generate-bridge.mjs` enabled 清单；`provider.credential.submit.schema.json` 文案；`hosthandler_windows.go` Mutate 第三分支；`credential_internal_handlers.go` 或新 backup internal；`IsCredentialReferenceAdopted`；`providerDTO`/`publicProvider`；`chat_run_stream.go` + `withProviderLeaseRef`；`ProviderApp.tsx`；`client.ts` MutationMethod / ProviderBridge；handlers_registry。

1. 迁移 + dump：空库 OpenTemplated 绿。  
2. D-F3：list/get JSON 无 ref。  
3. D-F4：第 5 把拒绝。  
4. Host：submit(request=backup.add) → Mutate 走 backup 内部方法，第一把不变，count=1。误把 backup.add 当 update 的测试必须红。  
5. adoption：备份 ref `IsCredentialReferenceAdopted==true`。  
6. D-F1：假 Adapter 第一次 Stream 在 0 delta 时 429，第二把成功；同一次 start。  
7. D-F2：401 不换。  
8. 已发送 EventDelta 后 429 不换。  
9. UI：备用 Key 行，走 submit+backup.add。  
10. `verify:bridge` + `go test ./internal/app ./internal/credentialsubmission ./internal/storage/sqlite`。

**0119 与 P1-6 同文件。** 若拆 PR：P1-5 先加列，P1-6 再加角色表；或一次迁。禁止两个 0119。

### P1-6 能力路由

**文件：** 0119 表；`capability.roles.get/set` schema；生成器清单（`capability.roles.get` 紧挨 `capability.list`）；`capability_roles.go` `_test.go`；`flash_model.go`/`chat_plan.go`/`task_route.go`（可选 flash 分类）；`CapabilityRouting.tsx`+test；`SettingsPage.tsx`；`settingsNav.ts`+test；`client.ts` roles.set 为 mutation。

1. D-P2：空库 get 6 行自动。  
2. D-P1：set judge=chat 未勾选拒绝。  
3. 绑定 kind 不符拒绝（gui 角色绑 llm）。  
4. D-H3b：flash 空 → 不调模型分类。  
5. D-H3：flash 显式绑定 → 分类 Complete 的 model 是该 id。  
6. plan.verify 走 judge 绑定。  
7. UI：六个下拉，空=自动；不新 nav id（D-P3）。  
8. 回归 D-C1–C3。

### P1-7 drag + UIPI

**文件：** `computer_act.go` `_test.go`；`chat_tool_defs.go` schema `id2`；`runhost.go`；host 错误映射；`chat_file_picker.go` `looksLikeUACToolResult`。

1. D-Q1/D-Q2。  
2. schema 含 id2。  
3. D-M1：拒附文案触发 `looksLikeUACToolResult` → parkUACAsk。  
4. 现网 UAC / 另存为 / observe_gate 绿。

### P1-8 视频链接解读

**文件：** 新 `internal/videounderstand/*.go` `_test.go`（纯解析 + host 表，无网）；`task_route.go` `_test.go`；`chat_tool_profile.go`（短链 task hint）；`chat_tool_defs.go`；`internal/toolruntime/runtime.go` 新 case；`chat.go` `toolStartedSummary`；`chat_companion_speech.go`；夹具 HTML 放 `internal/videounderstand/testdata/`。不改 `webfetch/extract.go` 的 skip 表。不加 Bridge schema。

1. `TestDetectVideoShareURL`：四平台长链 + `b23.tv` / `v.douyin.com` / `youtu.be` 无 scheme；`example.com` 否；`news.qq.com` 否（D-V1）。红。  
2. 实现 host 表。绿。  
3. `TestClassifyTaskRoute` 加行：  
   - 光贴 BV / 抖音口令「复制打开抖音…https://v.douyin.com/x」→ R1，must `video.understand`，forbid `browser.act` `computer.act` `media.play`（D-V2/D-V3）  
   - 「用浏览器打开」+ BV → R3，无 `video.understand`（D-V4）  
   - 「播放」+ 抖音链 → R1 不是 R2（D-V5）  
   - 「解读/总结」+ `v.qq.com` → R1（D-V6）  
   - 「打开B站」无 URL → 仍 R3（P0 回归）  
   - 「打开 Chrome 上 12306」仍 R3（P0）  
   - 「播放七里香」仍 R2（P0）  
4. R1 allow 含 `video.understand`；`applyTaskRoute` 夹具 defs 含该工具时 R1 保留、R2/R3 丢掉（D-V7）。  
5. 解析夹具：只有 og → `source=page_meta` 有 title（D-V8）；含字幕 JSON → `source=captions`（D-V9）；空 SPA → `empty_page`（D-V10）；登录墙标记 → `login_wall`（D-V11）。  
6. 假 Fetch：分享链 302 到 `169.254.169.254` / `127.0.0.1` → SSRF 失败（D-V12）；302 到 `example.com` → `unsupported_host`（D-V13）。字幕第二跳 host 不在字幕白名单 → 不跟，降为 `page_meta`。  
7. `toolruntime` case：`video.understand` 调解析器；不写 `Artifact` html。  
8. 月伴 lead-in + 工具指令句；`companionRedundantWebSkip` 对 `video.understand` 不跳（D-V14）。  
9. 回归 `./internal/app`（D-A*）`./internal/toolruntime` `./internal/videounderstand`。

---

## 6. 分期

```
P1-1 kind+schema                 无依赖
P1-2 页签                        ← P1-1
P1-3 gui_fallback+接线           ← P1-2 + P0 observe
P1-4 Embedder+kb                 可与 P1-3 并行
P1-5 Key 备份                    可并行；先测 Host 再改 lease
P1-6 角色表+UI                   ← P1-1；与 P1-5 同 0119 或先 0119 列再表
P1-7 drag/UIPI                   可并行；少碰 SessionPage
P1-8 video.understand            可并行；只碰 task_route / toolruntime / 月伴指令，不碰 SessionPage
```

同一 PR 不改 SessionPage + CompanionStage。不夹带视觉、poison、omni、VERSION。

---

## 7. 验收

先红后绿。P0 D-A…D-O / C7–C9 必须绿。

| ID | 断言 |
|---|---|
| D-D1 | NormalizeKind/ValidKind(gui\|embedding) |
| D-D2 | CatalogForKind(gui) 无 llm/vision；VisionDescribe 无 gui |
| D-D3 | 页签文案 |
| D-D4 | 会话选择器只有 llm |
| D-D5 | R1 → fallback none |
| D-D6 | desktop.type L0 过 → 不进 |
| D-D7 | 无 gui 有 vision 有树 → 只 markId 字符串 |
| D-D8 | 无 gui 无 vision 或空树无 gui → 无法执行+观察 |
| D-D9 | 空树+gui → [0,1] 或 0–1000 + 本帧 frameId |
| D-D10 | persist/modelKind 不塌 llm |
| D-D11 | volc 拒 embedding/gui |
| D-D12 | 子代理不调用 fallback |
| D-N1 | CC 关：无 computer.act；fallback none |
| D-E1 | 无 embed：kb/memory 现网绿；IndexVersion=fts5-trigram |
| D-E2 | 有 embed：同义进向量；ATA FTS 仍在 |
| D-E3 | Embed 失败 NULL，FTS 可读 |
| D-E4 | Anthropic 当 embed → 跳过写 |
| D-E5 | 入库事务内无 HTTP Embed |
| D-F1 | 0 delta + 429 + 备份 → 同轮续；≤3 换 |
| D-F2 | 401 不换；PROVIDER_AUTHENTICATION_FAILED |
| D-F3 | DTO 无 ref，只有 count |
| D-F4 | 第 5 把拒绝 |
| D-F5 | 已 EventDelta 后 429 不换 |
| D-F6 | Mutate(backup.add) 不走 update 分支；第一把不变 |
| D-P1 | judge=chat 未勾选拒绝 |
| D-P2 | get 永远 6 行 |
| D-P3 | 无新 SettingsCategory |
| D-H3 | flash 分类用 flash 绑定 |
| D-H3b | flash 空：不分类 |
| D-Q1 | drag 无 observe+id 失败 |
| D-Q2 | observe 后 drag id 走 rememberHits |
| D-M1 | 拒附 → parkUACAsk，无像素 |
| D-X1 | 工具表不因市场检索出现 CUA MCP；路由不被检索替换 |
| D-V1 | 四平台 + 短链检出；非白名单否 |
| D-V2 | 光贴分享链 → R1 + `video.understand`，无 browser/computer/media.play |
| D-V3 | 「复制打开抖音」口令仍 R1，不因「打开」变 R3 |
| D-V4 | 「用浏览器打开」+ 视频链 → R3 |
| D-V5 | 「播放」+ 抖音链 → R1，不是 R2 |
| D-V6 | 「总结/解读」+ 腾讯视频链 → R1 |
| D-V7 | R1 保留 `video.understand`；R2/R3 不带 |
| D-V8 | og 夹具 → `page_meta` + title |
| D-V9 | 字幕夹具 → `captions` |
| D-V10 | 空壳 → `empty_page`，禁止「我看完了」 |
| D-V11 | 登录墙 → `login_wall`，不调 browser.act |
| D-V12 | 重定向内网 → SSRF 失败 |
| D-V13 | 重定向出白名单 → `unsupported_host` |
| D-V14 | 月伴不把 `video.understand` 当天气二次搜索跳过 |

**每 PR 回归：**  
`go test ./internal/app ./internal/toolruntime ./internal/videounderstand ./internal/ccapp ./internal/domain/provider ./internal/gateway ./internal/m8app ./internal/storage/sqlite ./internal/credentialsubmission`  
C7-6 C8-2 C9-8、UAC park、汽水前台、kb FTS、skill catalog、mcp presets。  
Web：`modelKind` `userAsk` `ProviderApp` companion a11y。  
`npx tsc --noEmit`；改 schema 则 `npm run generate:bridge && npm run verify:bridge`。

---

## 8. 手工 20 分钟

1. 不配 GUI：北京天气只 web.search。  
2. 打开汽水到前台。  
3. CC 开：observe 见 B1 再点。  
4. 只配视觉：无名钮读号能点。  
5. 视觉关、空树：失败带观察。  
6. GUI 草稿 127.0.0.1，安装包不增。  
7. 主模型 429（未吐字）+ 备份能续；401 不换。  
8. 设置→模型与供应商顶部六角色；judge 不勾不能=主模型。  
9. 专家库：未配向量关键词在；配了同义能中、ATA 不丢。  
10. 登录 / UAC / 拒附：决策卡，不盲点。  
11. 对话只贴一条 B站/抖音/腾讯视频/YouTube 链：走解读，不弹出工作区浏览器代播；回复标明「根据简介」或「根据字幕」。  
12. 抖音口令（带「复制打开抖音」）：仍总结，不 `browser.act`。  
13. 「用浏览器打开这条B站」：才走浏览器。  
14. 无字幕/登录墙：诚实说看不到正片，请用户贴文案；不假装看完。

---

## 9. 文件所有权

```
P1-1  public.dto.schema.json；kind.go provider.go provider_test.go；生成三件套
P1-2  modelKind.ts/.test.ts；ProviderApp.tsx/.test.tsx
P1-3  新 gui_fallback.go/_test.go；chat_run_stream.go；chat_tool_defs.go
      ccapp/service.go（SetAllowGUIPixels）
P1-4  gateway/types.go；新 openai_embed.go/_test.go
      m8core/kb.go；m8_kb.go；m8app/kb.go kb_search.go
      kb_search_handlers.go；provider_diagnostics.go
P1-5  0119 sql；store.go uow.go
      新 provider.credential.backup.add/remove.schema.json
      envelope.schema.json；generate-bridge.mjs
      provider.credential.submit.schema.json（文案）
      hosthandler_windows.go；credential internal backup handler
      store IsCredentialReferenceAdopted
      engine.go providerDTO/publicProvider
      新 provider_backup_handlers.go
      chat_run_stream.go；provider_diagnostics.go（withProviderLeaseRef）
      handlers_registry.go
      client.ts；ProviderApp.tsx
P1-6  0119 表；capability.roles.get/set.schema.json
      新 capability_roles.go/_test.go
      flash_model.go chat_plan.go task_route.go（仅显式 flash 分类）
      新 CapabilityRouting.tsx+test
      SettingsPage.tsx settingsNav.ts/.test.ts
      client.ts MutationMethod
P1-7  computer_act.go/_test.go；chat_tool_defs.go
      runhost.go；host_windows_*；chat_file_picker.go
P1-8  新 internal/videounderstand + testdata
      task_route.go/_test.go；chat_tool_profile.go
      chat_tool_defs.go；toolruntime/runtime.go
      chat.go toolStartedSummary；chat_companion_speech.go
```

不碰：CompanionStage 视觉、poison、omni、VERSION、v0.4.62、memory_inject 计分、chat_skill_catalog.go、mcp_market ETL、AnnotateCapture、assignNodeIDs、`webfetch` skip 表、`video.generate`、gateway 视频字段。

---

## 10. 风险

| 风险 | 对策 |
|---|---|
| NormalizeKind 漏 case | D-D1/D-D2 门禁 |
| 生成器硬清单漏方法 | P1-1/5/6 立刻 verify:bridge |
| 备份走成 update 换第一把 | D-F6；Mutate 显式三分支 |
| 429 重试重复吐字 | D-F5：有 delta 不换 |
| 事务内 Embed 锁库 | D-E5；事务外 UPDATE |
| 空树 xy 泄漏给模型 | SetAllowGUIPixels 仅兜底前后；defer 关 |
| 视觉乱点 | 只 markId；R1/R4/子代理 none |
| store dump | 0119 与快照同 PR |
| 工具货架进电脑控制 | D-X1 |
| 抖音口令「打开」误 R3 | D-V3；打开单独不算浏览器意图 |
| 声称看完视频 | D-V8/D-V10；`source` 强制 |
| 改 ExtractText 漏 script | 禁止；独立解析器 |
| 字幕 CDN 变 SSRF | 第二跳仍 pinned；出表不跟 |

单包不合只回退该包。

---

## 11. 批准

1. 本文是 P1 唯一事实源；P0 仍认 09-04 父稿。  
2. 上一份 P1 稿里「只接函数 / 只写引擎备份 Handler / 入库事务里 Embed」作废，以本文为准。  
3. `NormalizeKind("gui")` 必须是 `gui`。  
4. 空树坐标只许 GUI，且生产开关用完即关。  
5. SoM 是 `"B1"`。  
6. 429 只在 0 delta 时换备份；401 不换；不晋升第一把。  
7. 备份必须走 Host Mutate 第三分支。  
8. 工具层搜索引擎不对电脑控制开工；技能/MCP 只开门到 P2。  
9. 视频链默认解读（R1），不代播；没有字幕就承认只看到简介；不接 yt-dlp。

已批准。按 §5 直接做。迁移文件在仓库根 `migrations/`（不是 `internal/storage/sqlite/migrations`）。`video.understand` 加入 `engineToolDefinitions` 后必须从 `readOnlyEngineToolDefinitions` 排除，否则研究子代理会带上。

---

## 12. P2 门（不开工）

天气适配器、rerank、OCR、文本 Guard、技能/MCP 目录向量、sqlite-vec、官方 Registry 日更。  
成片/音频进多模态、yt-dlp、小红书/快手等未点名站点、默认安装 YouTube MCP。

若做目录检索：复用 P1 Embedder；语料=名称+描述+触发词/参数/示例；未配向量时词面/LIKE 必须绿；检索≠安装≠调用；sqlite-vec 可选且失败回退 BLOB 余弦；不把 Computer-Use MCP 搜进 `computer.act` 表。
