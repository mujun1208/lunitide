# 能力补齐设计：角色路由 + 传感器先验证

日期：2026-09-04  
状态：**superseded。** 角色 + 传感器方向仍有效；「不新增 kind=gui / GUI 绑 vision」已作废。开工只认 [2026-09-04-daily-ops-capability-prd.md](./2026-09-04-daily-ops-capability-prd.md)。  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  
对照：[八类模型现码盘点](../../../C:/Users/mujun/.cursor/projects/e-Trae-Work-Projects-lunitide/canvases/eight-model-slots.canvas.tsx)（画布）；OpenClaw / Hermes 插槽纪要（用户提供）

---

## 0. 分类与目标

这是**架构方案**，不是在现有 kind 上各加一个模型。

**目标：** 用 2026 年桌面 Agent 里真正有效的做法，补齐 Embedding / Reranker / Judge / OCR / GUI 视觉闭环 / Key 池，且不拆掉现有六种 kind、不新开 daemon、内核仍是 SQLite。

**非目标：** 不做成「用户必须配齐八个模型才能用」；不引入 Qdrant / Mem0 必选依赖；不把电脑控制改成纯 GUI 基座模型；不改 poison / 说完再答 / 月伴 TTS 音色。

---

## 1. 三条路与选择

| 路线 | 做法 | 代价 | 结论 |
|---|---|---|---|
| A. Kind 爆炸 | 加 `embedding` `rerank` `judge` `ocr` `gui` 五种 kind + 五套后台页 | 配置税高；Judge/OCR/GUI 本质不是独立协议 | 否 |
| B. 外挂平台 | Mem0 + 向量库 + UI-TARS 常驻 | 新进程、新故障域，违背「内核永远 SQLite」 | 否 |
| **C. 角色路由 + 传感器先验证** | 能力是 **Role**，绑到已有 kind；检索先混合算法；桌面先 UIA/前台传感器，视觉只做兜底；Key 是供应商密文列表 | 只加一个 embedding kind（API 形状不同）；其余不增协议 | **采用** |

2026 年先进且适合月汐的不是「八个模型都接上」，而是：

1. **Role 不是 Kind。** Kind 描述 API 形状（chat / embeddings / speech）。Role 描述这次调用干什么（chat / flash / vision / embed / judge）。
2. **先观察再裁判。** `desktop.open` 刚证明：工具自报成功不可信。Judge 的第一层是传感器，不是再问同一个 LLM。
3. **混合检索，模型重排最后上。** 个人/专家库规模下，FTS + 向量 + 新近度 + MMR 比上一个 Reranker 模型更稳、更便宜。
4. **GUI 以 UIA 树为主、截图为辅。** Windows 上纯视觉点选仍脆；先进做法是可访问性树 + Set-of-Mark，失败才叫现有 vision 槽。

---

## 2. 角色表（产品配置面）

在现有供应商目录之上加一层 **Capability Role**，设置里叫「能力路由」，不叫「第八个模型页」。

| Role | 绑到 Kind | 缺省绑定 | 独立降级 |
|---|---|---|---|
| `chat` | `llm` | 会话/首页当前模型 | 已有 `chat.prefer` → `kindDefault` |
| `flash` | `llm` | 现 `pickCompanionFlashModel` 显式化，可手选 | 回落到 `chat` |
| `vision` | `vision` 或 `llm+supportsVision` | 现 `VisionDescribeCatalog` | 已有顺序试 |
| `embed` | **新 kind=`embedding`** | 未配置则检索只走 FTS（合法降级） | 本地/兼容 OpenAI embeddings |
| `judge` | `llm`（角色，不是新 kind） | 默认 `flash`，可指定更便宜的验证模型 | 传感器通过则不调用 |
| `listen` / `speak` | `asr` / `tts` + 本机引擎 | 已有三路听、多引擎说 | 已有 |
| `image` / `video` | `image` / `video` | 已有工具 | 已有 Catalog 顺序试 |

**不新增的 kind（本文原结论，已被日常任务 PRD 修正）：** `judge`、`ocr`、`rerank` 仍不新增。**`gui` 改为独立 kind**，见日常任务 PRD §3.2。OCR 继续并进 `vision`；Rerank 先算法。

**Key 池：** 每个 Provider 从「一把钥匙」改为「密文列表」。`429` / 认证失败轮下一把。不新开进程。仍走 Windows 凭据 + `secretlease`。

---

## 3. 分能力怎么补

### 3.1 Embedding（要加 kind，但检索仍以 FTS 为底）

现码：`kb_chunks.embedding` 空列、`memories.embedding_id` 空引用、`memory.search` 关键词、专家库 FTS5。

**做法：**

- 供应商目录加 `kind=embedding`（OpenAI 兼容 `/embeddings`，可接火山/Voyage/本机 bge）。
- **写入时**给专家库 chunk、确认后的个人记忆打向量；旧行后台补齐，失败不影响 FTS。
- **查询时**并行：FTS5（已有）+ 向量余弦（有 embed 才跑）。合并分 = `α·fts + β·dense + γ·recency`，再 MMR 去冗余（λ≈0.7，抄 OpenClaw 的「查询零模型」）。
- 未配 embedding：**产品完整可用**，只是语义近邻弱。这是合法降级，不是残缺。

机务手册（ATA 章节）更吃关键词和定位器，所以 FTS 必须留下。

### 3.2 Reranker（先不上模型）

个人库和专家库在桌面规模下，交叉编码器收益小于配置税。

**P0：** 确定性融合 + MMR + 新近度。  
**P2（仅当单库 chunk > 约 2 万且用户叫「搜不准」）：** 可选 `kind=rerank` 或复用小 LLM 列表重排 Top-20。默认关。

### 3.3 Judge（两层，禁止「同一模型自嗨」）

现码：`planVerifyPrompt` 仍喂**当前聊天模型**，只看执行日志，不看桌面事实。

| 层 | 谁 | 何时调用 | 例子 |
|---|---|---|---|
| **L0 传感器** | 无模型 | 每个会改变世界的工具返回后 | 前台进程/标题是否命中（已有 `openedWindowConfirmed`）；文件是否存在；KB 引用是否在命中 chunk 里；浏览器 URL 是否变化 |
| **L1 角色** | `judge` → 便宜 LLM，**禁止默认等于 `chat`** | 仅 L0 不确定，或 `plan.run` 多步收口 | 把 L0 观察（前台快照、工具 stderr）塞进 verifier，输出 `{verified,gaps}` |

桌面打开的验收句式已经锁过：「前置后读前台命中才算」。把这套 **L0 推广到 `cc.*` / `media.play` / `browser.act`**，比先接一个 Judge 模型更能消灭「无法执行」假因。

MRO 继续：正则门禁是 L0；不把机务放行交给 Judge 模型。

### 3.4 OCR（不单独立 kind）

现码视觉提示词已要求转写可见文字。

- 会话截图 / 月伴看屏：继续 `vision` 槽。
- 专家库 PDF/Office：解析管线已有；扫描件在 **没有 vision** 时才走本机 OCR（例如 RapidOCR 作为 ingest 库，不是用户可见 kind）。
- 不在设置里出现「OCR 模型」页。

### 3.5 GUI 视觉闭环（月汐差异化，也是最该补的）

两家开源壳都空着；月汐已经有 Win32/UIA + CDP + 前台校验。

**不要**第一期上 UI-TARS 常驻。Windows 上先进且稳的顺序：

1. **L0：** UIA/Win32 树 + 现有 `ActivateWindowMatching` / 前台命中。
2. **L0.5 Set-of-Mark：** 把可点控件编号画在截图上（来自 UIA，不是模型猜框）。
3. **L1：** 仅当树缺失或点了没生效，把 SoM 截图交给现有 `vision` 槽：「点编号 N / 读到的字是什么」。
4. **L0 再读一次前台或控件状态。** 失败才对用户说「无法执行」，并带 `code · correlationId`。

观察闭环仍要。用户已指定 **GUI 单独成 kind 作兜底**，不再绑 `vision`、不再列为 P2 可选项。以日常任务 PRD §3.2 为准。

### 3.6 Guardrail（保持策略，不模型化）

执行模式、hooks、电脑控制开关、危险操作审批继续当门。模型 Guardrail（LlamaGuard 类）只做 P2 可选，挂在 hooks 的 `block` 之前，默认关——航空场景更信名单和人审，不信分类器放行。

### 3.7 Key 池

`Provider` 凭据从单 ref 改为有序列表。Call 遇 `429` / `insufficient_quota` / 部分 `401` 换下一把，同一次用户动作最多轮 N 次。lease 语义不变。UI：供应商页「添加备用 Key」，不做成独立产品。

---

## 4. 架构落点（仍是现有进程）

```
设置「能力路由」
    chat / flash / vision / embed / judge
         │
         ▼
  CatalogForKind + RoleBinding
         │
    ┌────┴────┐
    │ Engine  │  无新 daemon
    └────┬────┘
         │
  工具返回 ──► L0 传感器 ──► 过则结算
                    │不确定
                    ▼
              Role=judge（便宜 LLM）
                    │
              仍不确定 → 对用户说无法执行 + 观察
```

检索：

```
query ──► FTS5 ──┐
         embed? ─┴► 融合 + 新近度 + MMR ──► 注入上下文
```

电脑控制：

```
UIA/打开 ──► L0 前台/控件 ──► 成功
                │失败
                ▼
         SoM 截图 + vision ──► 再 L0
```

---

## 5. 分期（按收益，不按「缺哪个 kind」）

### P0 — 先消灭假成功 / 假失败（不配新模型也能变准）

1. 把 `openedWindowConfirmed` 的前台传感器推广到 `cc.*`、`media.play`、关键 `browser.act`。
2. `plan.verify` 必须附带 L0 观察；`judge` 角色默认 `flash`，禁止与 `chat` 同一模型除非用户显式勾选。
3. 检索融合：FTS + 新近度 + MMR（此时可以没有向量）。
4. 供应商备用 Key 列表 + 429 轮转。

### P1 — 语义检索与视觉兜底

1. `kind=embedding` + 写入专家库/已确认记忆。
2. 混合检索（FTS ∥ 向量）。
3. 电脑控制 SoM + 现有 vision 槽兜底。
4. 设置页「能力路由」：chat / flash / vision / embed / judge 五个下拉，空 = 自动。

### P2 — 按痛点加模型，默认关

1. 库很大时可选 rerank。
2. 扫描件 ingest 本机 OCR。
3. 可选本机 GUI 视觉模型绑到 `vision`。
4. 可选文本 Guard 模型挂 hooks。

---

## 6. 验收（方案被批准后写进实施计划）

- 未配 embedding：记忆/专家搜索行为不比现在差，设置不报残缺。
- 配了 embedding：同义问（「起落架」vs「landing gear」）能命中，FTS 关键词问不回退。
- `desktop.open` / 播放：前台未命中不得 `ok`；L1 judge 不得在无观察时改口为成功。
- `plan.run` 验证请求的模型 id ≠ chat 模型 id（除非用户显式「验证用主模型」）。
- 主 LLM 429：备用 Key 轮一次后同一轮对话能继续；视觉预识别不跟 chat 共一把正在限流的 Key（已分供应商则自然隔离）。
- 不新开 daemon；不强制新 npm 依赖进安装包（本机 OCR/GUI 模型若上 P2，走已有本机模型安装器）。

---

## 7. 风险

- 把 Judge 做成新 kind：用户配不起，验证仍和主模型共故障域。
- 先上向量、丢掉 FTS：机务手册定位器会退步。
- 先上 GUI 基座模型：Windows 上点偏，且和现有 UIA 双源打架。
- Key 池轮转若把 401 一律当「换 Key」：会掩盖真配错，需只对配额类错误轮转。
