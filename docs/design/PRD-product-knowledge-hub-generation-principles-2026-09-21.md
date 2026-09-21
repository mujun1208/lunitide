# 产品知识中枢 · 生成原则（写一次，后续只演进规则）

- 日期：2026-09-21
- 状态：已确认（方案 2 + 种子/自动迭代）
- 配套：`docs/design/catalog-product-knowledge-hub-v1-seed-2026-09-21.md`（初版详细清单）
- 自净化：`docs/design/PRD-product-hub-self-purify-2026-09-21.md`（诊断报告 + 落地实施 PRD）
- 实施计划：`docs/superpowers/plans/2026-09-21-product-knowledge-hub.md`
- 上游契约：`docs/design/PRD-product-knowledge-hub-2026-09-18.md` V3.1、`docs/design/UI-product-knowledge-hub-2026-09-18.md`（色调改黑白）

---

## 0. 一句话

**第一次**交出一份足够细的种子清单（含「放歌 / 暂停 / 下一曲 / 打开文件 / 打开网易云」这类原子功能）。  
**以后每一次**产品启动或重建，引擎对照「活的产品源 ∪ 初版种子 ∪ 本文件规则」自动生成最新快照：新增补卡、仍在的保留并刷新探针、消失的退役。  
**不要**每改一版产品就手写一整份功能表。

```
seed.v1（人工，只写一次）     live catalog（每次扫描）
        \                       /
         \                     /
          merge(stable_key) + chain templates + overlays
                         |
                  snapshot N（verified）
                         |
              diagnose report（四要素）
```

---

## 1. 三层知识，谁能改谁

| 层 | 文件/位置 | 谁维护 | 何时改 |
|---|---|---|---|
| **P 原则** | 本文件 + `internal/producthub/rules/` | 人 | 只在新增**源类型 / 节点类型 / 链路模板类 / 诊断规则类**时改 |
| **S 种子** | `internal/producthub/manifest/seed.v1.json`（初版见 catalog 文档） | 人写一次 | 只允许**补深度讲解**（更完整的 chain / 中文说明书），禁止当「当前功能全集」 |
| **L 活源** | 页面联合类型、设置 18 分类、Bridge `x-method`、媒体 action 枚举、HarnessPlugins、技能/MCP/命令/自动化/记忆计数 | 产品自己 | 每次 `boot / registry / upgrade / manual refresh` |
| **O 覆盖** | `product_node_tags`（manual）、`product_enrichments`、seed 里同 `stable_key` 的富文本 | 人/LLM | 重建永不丢；LLM 只补 `summary/description` 并标 AI |

事实（计数、关系、探针、枚举动作）只来自 L。  
文案深度（说明书、逐步讲解、成功/失败/重试/降级）优先来自 S/O，没有则用 P 的链路模板当场生成。

---

## 2. 活源（产品自己加载，不手抄）

每次构建，采集器并行读取下列源。任一源失败记 `PH-002`，不拖死其它源。

| 源 ID | 读什么 | 吐什么 |
|---|---|---|
| `pages` | `Page` 联合类型（生成进 catalog） | Module 入口 + `feature.<domain>.page.<page>` |
| `settings` | `SETTINGS_CATEGORIES` 18 项 | Module 解剖 + `feature.foundation.settings.<id>` |
| `office-menu` | `OfficeMenuSettings` 键 | 办公组可见入口（含媒体中心） |
| `bridge` | `api/bridge/v1/*.schema.json` 的 `x-method` 且 `x-enabled=true` | 每个公开方法一张原子 Feature |
| `media-actions` | `MediaOperationDTO.action` 枚举 | `feature.office.media.<action>`（play/pause/next…） |
| `plugins` | `HarnessPlugins()` | Plugin 节点 + 对应 Feature |
| `skills` | `skillapp` 已安装清单 | Skill 节点 + `feature.assets.skill.<name>` |
| `mcp` | `mcp6` Endpoint + ReadyToolSnapshot | Mcp / McpTool + invoke Feature |
| `experts` | `m8app` ExpertTx | Expert 节点 |
| `commands` | `command.Manifest.Specs` | `feature.execution.command.<id>` |
| `automation` | `automation.go` Bundle | Chain 节点 |
| `memory` | `memoryapp` fact 计数 | 计数，不造假节点 |
| `seed` | `seed.v1.json` | 富卡 + 示范链路；只 merge，不覆盖活源 identity |

**生成 catalog 的方式（避免引擎读 TS）：**  
`web/scripts/generate-product-catalog.mjs` 与 `generate:bridge` 同批跑，写出：

- `web/src/generated/productCatalog.ts`
- `internal/producthub/generated/catalog.go`

内容：Page 字面量、设置分类、办公菜单键、全部 `x-method`、媒体 action。  
**产品加一个页面 / 一个设置项 / 一个 Bridge 方法 / 一个媒体动作 → 下次生成 catalog → 下次快照自动多一张卡。** Hub 业务代码零改动。

---

## 3. stable_key（跨版本身份，merge 主键）

```
product.lunitide
domain.<dialog|office|assets|execution|foundation>
module.<domain>.<name>
feature.<family>.<verb>          例 feature.office.media.play
page.<page>                      例 page.media
settings.<category>              例 settings.computer
bridge.<method>                  例 bridge.media.asset.open
plugin.<id> / skill.<name> / expert.<catalog>.<name>
mcp.<name> / mcp.<name>.tool.<tool>
command.<id>
capability.<area>.<name>
chain.<feature>
landscape.competitor.<slug>
landscape.frontier.<slug>
```

规则：

1. 只允许 `[a-z0-9._-]`，一段一名。
2. 活源 identity 由**源自己的稳定 ID**推导（`x-method`、plugin ID、settings id），不由中文名推导。
3. 种子若与活源同指一事，必须用**同一个** stable_key。初版已对齐。
4. 改中文名不算新功能；改 key 才算新功能（旧 key 走退役）。

---

## 4. 原子功能粒度（「放歌」「打开文件」必须单独成卡）

一张 Feature = **用户可单独说出或点到的一个动词**，不是整个模块。

| 对 | 错 |
|---|---|
| 播放 / 暂停 / 下一曲 / 上一曲 / 停 / 搜歌 / 开网易云 | 「音乐」一张大卡了事 |
| 打开文件 / 选文件 / 预览 / 保存 | 「文件工作区」一张了事 |
| 打开应用 / 截图 / 点击 | 「电脑控制」一张了事 |

派生规则：

- 枚举源（媒体 action、设置分类、Page、Bridge method）→ **一枚一卡**。
- 种子示范卡（开网易云、语音连续指令）可以比枚举更「场景化」，但必须 `uses` 到枚举卡（`calls` 边）。
- 禁止把两个可独立失败的动词合成一张卡。

---

## 5. 链路模板类（没有种子讲解时自动生成）

每个原子功能必须带主干 3–8 步，且至少一条 `success` + 一条 `failure`（失败必须声明 retry 或 fallback，禁止悬空）。

| class | 适用 | 主干骨架 |
|---|---|---|
| `intent-control` | 语音/打字驱动的本机动作（放歌、开应用） | 输入 →（可选 ASR）→ 意图 → 绑定工具 → 执行 → 核验（SMTC/窗口/owned runtime） |
| `media-transport` | 媒体中心已有会话上的 play/pause/next… | 选会话 → 发 operation → 核验 snapshot → UI 刷新 |
| `file-open` | 打开/选择/预览文件 | 入口 → 选路径或 assetId → 权限 → 打开 → 核验句柄/预览 |
| `crud-bridge` | 普通 Bridge 方法 | 请求 → 校验 → 持久化/调用 → 回执 |
| `settings-toggle` | 设置分类项 | 打开设置 → 定位分类 → 改值 → 持久化 |
| `page-enter` | 进入某个 Page | 侧栏/深链 → 门禁 → 渲染 → 首屏数据 |
| `asset-invoke` | 技能/MCP/专家/命令 | 选择资产 → 授权 → 调用 → 回执 |
| `meeting-pipeline` | 会议开始/转写/纪要 | 拾音 → ASR → 落库 → 摘要/待办 |
| `diagnose-only` | 诊断与自检 | 扫描 → 规则 → 报告 →（不自动改码） |

种子可覆盖整条 `chain`。merge 时：种子有完整 chain 则保留；活源只刷新 `attributes` / `probe` / `version`。

---

## 6. Merge 算法（每次自动生成最新版）

输入：`seed.v1`、本次 live candidates、上一份 `verified` 快照、manual 标签、enrichments。

对每个 `stable_key`：

| 情况 | 行为 | changelog |
|---|---|---|
| 仅 live | 用模板生成卡；文案用标题+源 description；标 `provenance=registry\|probe` | `added` |
| 仅 seed | 若活源应存在却没有 → `warn` + 卡标弃用候选；landscape 类保留 | `removed` 或保留（landscape） |
| 两边都有 | **identity/attributes/probe 用 live**；**summary/description/chain/methods 用 seed（若更完整）**；manual 标签并入 | `updated`（digest 变）或跳过 |
| 仅上一快照有、live/seed 都无 | 退役节点，影响面一跳反依 | `removed` |

完整度判定（seed 胜出条件，全满足才覆盖文案）：

- `summary` 非空且 ≤128
- `description` 非空
- `steps` 3–8
- 至少 success + failure，failure 闭合

否则用模板，并标「待补说明书」。

Digest：`sha256(canonical(card without probe timestamps))`。未变则该卡不写 change_log。

并发：manual refresh 与 registry 轮询用 manifest+catalog digest CAS；building 失败丢弃，旧 verified 保留。

---

## 7. 什么时候必须改 Hub 代码

只在这些情况改 `internal/producthub` / 原则文件：

1. 新增**源类型**（例如将来有「手机端注册表」）
2. 新增**节点类型**（第 14 类）
3. 新增**链路模板类**
4. 新增**诊断规则类**
5. 改 Bridge 契约或表结构

不改 Hub 的情况（由 catalog + 采集器自动吃进去）：

- 新 Page、新设置分类、新办公菜单项
- 新 Bridge 方法、新媒体 action
- 新技能 / 插件 / MCP / 命令 / 专家
- 改某功能中文名或默认开关

---

## 8. 标签

四类词表：`scenario` / `capability` / `entry` / `status`。  
三级：registry 直映 → rule 关键词 → LLM 建议（必须落在词表内）→ **manual 最高，跨快照按 stable_key 保留**。

自动打标规则写在 `internal/producthub/rules/tags.json`，属 P 层。

---

## 9. 管理员门（只你可见）

- 用户名固定 `mujun`（大小写敏感）。
- 初密 `1234567890`：**只作为首次写入的明文输入**，落库用现有 `identity` 同款 bcrypt（cost 10），禁止明文、禁止进 git。
- 未解锁：侧栏不渲染「产品总览」；`page=productHub` 深链失败关闭；除 `productHub.auth.status|unlock` 外全部 `PH-012 UNAUTHORIZED`。
- 解锁后会话令牌（内存，进程内）；改密必须先验旧密。
- `officeMenu.productHub` 默认 false，且被引擎门覆盖（前端藏按钮不够）。

此密码已出现在对话里，首次进入后立刻改掉。

---

## 10. UI 色调（覆盖 UI 文档紫/青主色）

知识页仍固定深色、不跟主题。Token 改黑白：

```
--ph-bg: #0A0A0A
--ph-card: #141414
--ph-card-2: #1C1C1C
--ph-text: #F5F5F5
--ph-dim: #A3A3A3
--ph-line: rgba(255,255,255,0.12)
--ph-brand: #FFFFFF          /* 主干/选中用白，不用紫 */
--ph-brand-2: #D4D4D4
--ph-cyan: #E5E5E5           /* 编号改浅灰，不再用青当品牌 */
--ph-ok / --ph-warn / --ph-danger  仅表示探针与链路分支状态
```

DoF 的分节编号 A/B/C、双语角标、链路作视觉中心、竖版海报结构全部保留。

---

## 11. 竞品 / 前沿

数据在 `internal/producthub/manifest/landscape.json`（curated，属 S 层扩展，不是探针事实）。  
第五标签展示。LLM 可建议新条，必须标 AI，**不进健康度、不当活源**。  
改版迭代：人改 landscape 文件即可，不必改生成器。

---

## 12. 自净化边界（方案 2）

产品内：**每次 verified 后出诊断报告**（证据 / 根因 / 改进方案 / 验证方式 + 跨版本闭环）。  
自动改码：**不做**。白名单 apply（`edit_manifest` / `rebuild` / `install_asset` / `wont_fix`）写在自净化 PRD，本里程碑只落地报告 + `wont_fix` 确认。  
`fix_code` 只给文件:行。

---

## 13. 验收（生成原则本身）

1. 删掉种子里某张示范卡、产品仍有对应 Bridge/枚举 → 重建后仍有卡（模板生成）。
2. 在假 catalog 里加 `page.fake` → 重建多一张入口卡，changelog `added`。
3. 从假 catalog 去掉已有 page → 旧卡 `removed` 或弃用，不影响其它卡。
4. 种子里音乐播放的逐步讲解，在 live 刷新后**文案仍在**，探针数字会变。
5. 全量构建 ≤3s（探针并行，单探针 2s）。
6. 未解锁看不见模块。
