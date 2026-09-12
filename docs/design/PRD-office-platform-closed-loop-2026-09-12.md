# Lunitide 办公平台闭环 PRD

**版本：** 1.1（2026-09-12 对照代码复核后修订）  
**日期：** 2026-09-12  
**状态：** 现行产品合同。完成本文件全部条目后，办公平台按**本合同**完整可使用。  
**对照代码：** 工作区 `feat/prd-v7-s1-continuity`（含能力包 1、可用度包 U1–U4、清单包 I1–I5）。  
**旧研究稿：** [PRD-office-quality-commercial-2026-09-11.md](PRD-office-quality-commercial-2026-09-11.md) 降为研究附录，不再当验收门槛。  
**工作台设计稿：** [PRD-office-studio-v2.md](PRD-office-studio-v2.md) 仍解释入口与对象模型；与本文冲突时以本文为准。  
**配套执行计划：** [../superpowers/plans/2026-09-12-office-closed-loop.md](../superpowers/plans/2026-09-12-office-closed-loop.md)

**1.1 补记（对照二次代码审计）：** 质量承诺词表对不上后端检查 id（`structure` vs `package`）；`ValidateBrand` 仍用进程探测独立 PDF；停止检查会取消整任务；指标定位与检查定位一样只看当前页。下列 CL33 与加粗句已写入阶段 A。

**1.2 落地记录（2026-09-12）：** 阶段 A–C 已按完成定义落地。D1 脚本已写。D2 引擎主路径由 `TestOfficeClosedLoopProtocol` 覆盖（四格式、正式门槛、独立 PDF 通知、同源 PDF 不得误标、探测、Patch）。签名装机候选为 **0.4.76**；未带 ldflags 的本地 `go build` 仍报 `0.0.0-dev`。D2 真人新桌面端走查与第 17 节关闭签字仍待安装并重启 **0.4.76** 后完成。CL19–CL21 已记 n=1 基线，门槛未冻结。

**1.1 复核结论（先读；2–3 条已按 1.2 收口）：**

1. 本合同 100% ≠ 旧商业 PRD 100%，≠ 装机 0.4.75 已修，≠ 无 LibreOffice 也能把 Word/PPT/Excel **正式**交付。  
2. ~~`IndependentPDFCheck` 把「没装 Typst」当成 Formal 阻断。~~ **已收口：** 状态来自本文件后端；gofpdf 回退可正式。  
3. ~~`office.artifact.accept` 服务端不区分正式/草稿。~~ **已收口：** `formal:true` 且 `quality≠passed` 返回 `OFFICE_DRAFT_REQUIRED`。  
4. 阶段 A 原先那条合并 `-run` 会滤掉大量回归，不能当门禁。  
5. CL19–CL21 的秒数是**待测门槛**，不是已测基线；B1 先量后冻，禁止一上来 assert 导致红。n=1 基线已记，仍未冻结。  
6. 本包**不重构** `OfficeStudioPage.tsx`（约 1800 行）。结构清晰/可复用指新逻辑进现有 `officeQualityUi` / `officestudio` 函数，禁止借机拆页。

---

## 0 本文解决什么问题

上一份商业研究 PRD 同时写了「已经有什么」「希望做成 Gamma 级设计系统」「需要设计师 36 变体 / 校准 85 / 竞品盲评 / 企业审批」。后半段用人测和外部产品才能完成，代码无法单独收口，所以首发工程卡在约 83%、整份文档约 60%。

用户现在要的是另一份合同：

- 对照**当前办公工作台 + 对话里 Word / PPT / Excel / PDF 主链**写需求。
- 写出来的功能必须能落地、能用、能闭环。
- 稳定、速度快、高效。
- **无缺口**：Studio 或对话里点到的每条办公路径，要么做成、要么失败可见并保留草稿、要么写明不在范围。
- 有明确实施步骤。全部完成后，产品完整可使用。

因此本文只收录两类条目：

1. **已经做成、必须保持**的能力。  
2. **还能用工程收口、并且收口后用户就能完整用起来**的缺口。

设计师已检 36、校准视觉 85、Presenton 进生成、从成稿反推 Brief、PPT 母版还原、企业模板审批、在线协同，**不再写入本 PRD 的必须完成表**。需要时另立产品，不挡本平台交付。

装机若仍显示 0.4.75，只说明尚未用本工作区重新编译并重启。**本 PRD 完成 ≠ 旧安装包已修。**

---

## 1 产品定义

### 1.1 一句话

Lunitide 办公平台是 Windows 桌面、本地优先的成稿工作台：用户用已填简报和材料，在对话里生成可继续编辑的 PPTX / DOCX / XLSX 与 PDF，在办公工作台预览、检查、局部修改、版本回退，再导出草稿或正式交付副本。

### 1.2 不是什么

- 不是 Microsoft 365 / WPS 的完整编辑器。  
- 不是云端协同文档。  
- 不是任意旧 Office 文件的无损重建器。  
- 不是 HTML 小游戏生成器（`html.gen` 仍只允许 `penalty-shootout | timer | checklist`）。  
- 不是「装了 Word 就等于本产品已做打开验证」。

### 1.3 成功标准（本 PRD 的 100%）

同时满足下面六条，才算本产品完整可使用：

| # | 标准 | 不可用「差不多」替代 |
|---|---|---|
| S1 | 主闭环可走完 | 建任务 → 填或留空简报 → 对话生成四格式 → 工作台看见文件 → 预览 → 检查 → 改一处 → 新版本 → 导出草稿；检查通过时可正式交付 |
| S2 | 无死胡同 | 每个按钮、每条检查、每次失败都有结果、原因或「不在范围」文案；禁止空白状态、空 catch、假 passed |
| S3 | 四种格式各自可用 | PPT / Word / Excel / PDF 都能生成、打开副本、按支持矩阵修改；不把一种格式的成功说成四种都好 |
| S4 | 正式与草稿分开 | 硬问题或未完成必要检查时只能草稿；`quality=passed` 且无硬阻断才允许「作为正式交付」。Word/PPT/Excel 的必要检查包含 LibreOffice 渲染（CL32）。独立 PDF 不因缺 Typst 不能正式（CL11） |
| S5 | 稳定且可恢复 | 取消、超时、冲突、配额失败保留已成功文件；不覆盖接受版本；不虚构 Brief / 密级 / 节约率 |
| S6 | 速度可预期 | 文件生成、无渲染检查、单节点 Patch 有可测上限；慢路径（渲染、视觉模型、Typst）可选且不挡草稿 |

旧 PRD 的「超过 Gamma / 视觉 85 / 设计师已检 36 / 竞品盲评」**不是**本成功标准。

### 1.4 目标用户与首发场景

沿用旧 PRD 的用户，但收窄到本内核已经能服务的任务：

- 经营、产品、销售、咨询人员，需要对外或对管理层交一份可继续改的稿。  
- 三个场景：经营月报 / 季度复盘；销售客户方案；研究或项目正式报告。  
- 不做：政务公文、法律合同、审计报告、复杂财务模型、教育课件平台。

典型任务：

> 根据这份销售表和项目总结，做一份给管理层看的汇报，沿用已登记品牌，并给我对应的 Word 报告和 PDF。

系统只使用用户填过的受众、用途、页数、密级和事实。没填就显示「未填写」，**不写成管理层 / 经营汇报 / 12 页 / 机密**。

---

## 2 与旧文档的关系

| 文档 | 现在怎么用 |
|---|---|
| 2026-09-11 商业研究 PRD | 研究、竞品、许可、审美原则的附录。其中 FR17/FR18、Presenton 生产主链、校准 85、设计师已检 36 从本产品必须表删除 |
| Office Studio v2 | 入口、任务/版本/检查对象、Managed vs Imported 仍有效 |
| 2026-09-12 试验 / 代码收口 / 能力 / 可用度 / 清单规格 | 已落地基线。本 PRD 的「已做成」表以它们 + 当前代码为准 |
| 本文件 | **唯一现行验收合同** |

旧 FR01–FR18 到本文件条款的对照见第 15 节。

---

## 3 完整用户闭环（无缺口定义）

任何办公操作必须落到四态之一，禁止第五态（空白 / 静默 / 误导）：

```text
用户动作
  ├─ 做成：文件、预览、检查或导出按承诺出现
  ├─ 失败可见：alert / 检查回执说明原因，草稿或上一版本仍在
  ├─ 明确不在范围：一句话写清（例如不能从成稿反推简报）
  └─ 禁止：状态空白、按钮无反应、把未验证画成通过
```

### 3.1 主路径（必须）

```text
设置 → 办公菜单 → 打开办公工作台
  → 办公 → 办公工作台
  → 新建或选中与当前对话绑定的办公任务
  → 任务概要：已填保存，未填显示未填写
  → 右侧对话调用 office.generate（pptx.gen 等不是等价路径，只通过「在办公工作台查看」纳入）
  → 工作台出现对应 Artifact 与版本
  → 结构预览立刻可见；做过渲染检查后才显示「文件排版预览」
  → 检查：每条回执有中文状态词
  → 局部修改 / 换图 / 改表范围 → 新不可变版本
  → 导出草稿 或（通过后）作为正式交付再导出
  → 本机打开的是导出副本，不是存档原件
```

### 3.2 并行路径（必须同样无死胡同）

| 路径 | 做成 | 失败 | 不在范围 |
|---|---|---|---|
| 保存概要 / 改风格 / 登记品牌 | 任务修订号前进，概要回显 | `setError` 中文原因，草稿还在 | 品牌登记不还原 PPT 母版 |
| 导入已有文件 | 新版本，可白名单 Patch | 加密/宏/超限阻断并说明 | 不能从成稿重建 Spec/Brief |
| 本机打开 | 打开导出副本 | 探测失败写原因 | 安装 Word/WPS ≠ 检查里的打开验证 |
| 更新目录与页码 / 重算 | LibreOffice 可用则出新版本 | 组件缺失则 failed/missing，不标通过 | 不证明 Office/WPS 排版一致 |
| 停止检查 | 保留上一成功版本与已知回执 | 写「已停止，检查未完成」 | 不得把未检完说成已通过 |
| 成套导出 | 固定版本清单 | 未通过文件只能带草稿标记 | 通过 ≠ 已接受正式版 |
| 查找指标 | 当前任务已检节点上定位 | 找不到则空结果 + 说明 | 不是全库引用图 |

### 3.3 生成发生在哪里

工作台**没有**生成按钮。`OFFICE_GENERATE_STAGES`（整理 / 设计 / 生成 / 检查）是流程说明，不是向导进度，也不是可点步骤。

生成只在右侧对话：`office.generate` 写入当前办公任务并出版本；较简单的 `pptx.gen` / `docx.gen` / `excel.gen` / `pdf.gen` 写出会话产物，用户可「在办公工作台查看」。文案必须维持这一事实，禁止再写「可先看预览再点生成」。

---

## 4 当前已落地能力（代码事实）

以下是 2026-09-12 工作区已经存在、本 PRD 要求保持的行为。删除或回退其中任何一条都视为回归。

### 4.1 架构

| 层 | 位置 | 职责 |
|---|---|---|
| 工作台 UI | `web/src/officeStudio/` | 任务、预览、检查、版本、来源、品牌、风格、导入导出 |
| Bridge | `office.task.*` / `office.artifact.*` / `office.renderer.probe` / `office.storage.*` / `office.metric.*` / `office.bundle.*` | 工作台入口。**没有** `office.generate` Bridge 方法；生成只走对话工具。不手改 `bridge.ts` / `schema_generated.go` |
| 应用服务 | `internal/officeapp` | 任务、发布版本、Check、Export、Bundle、事实 |
| 内容内核 | `internal/officestudio` | Spec v1/v2、布局、品牌、生成、Patch、质量、PDF/A、视觉检查 |
| 文件字节 | `internal/officetools` | OOXML / Excelize / 独立 PDF |
| 渲染组件 | `internal/officerender` | LibreOffice 隔离渲染、桌面 Office/WPS 探测 |
| 对话工具 | `internal/app/office_tools.go` | `office.generate` 等；与工作台共用同一 Service |

生成主链固定为：**现有 Go 内核**。Presenton / PptxGenJS 只做对照探测，不进 `office.generate`。

### 4.2 任务与简报

- `office.task.create / get / update / list / sync / cancel`。  
- Brief 只保存用户已填字段：`audience`、`purpose`、`targetLength`、`confidentiality`、`outline`。  
- 快照、工作台、聊天证据都不把 unset 写成管理层 / 经营汇报 / 12。  
- 未填密级不写「机密」。已填密级写入 Word 页眉、PPT 备注、Excel「说明」页、独立 PDF 封面。  
- 关键数字冲突（`ValidateFactSet` / `ErrFactConflict`）拒绝保存，不静默合并。

### 4.3 品牌与模板

- 三套工程设计体系：`ops-clear`、`brand-pitch`、`editorial-report`。  
- PPT 12 种语义布局：封面、章节、结论、双栏、对比、指标、趋势、结构、流程、时间线、证据、结尾。工程变体 36 个；`designerReviewed=0`，界面写「工程变体，非设计师已检 36」。  
- Word 三类：`research-report`、`client-proposal`、`product-project`。  
- Excel 三类：`ops-ledger`、`sales-pipeline`、`project-tracker`。空表 `PlanWorkbook` 必须保留 `BrandID`。  
- 品牌一级导入：颜色、字体、授权 `AssetRecord`（来源、许可、64 位摘要）。不还原 PPT 母版。  
- 无效登记品牌按钮禁用，并提示缺编号 / 来源 / 许可 / 摘要。

### 4.4 四种格式生成

| 格式 | 内核 | 内容合同 | 上限 |
|---|---|---|---|
| PPTX | 自有 OOXML + 原生表 / 图 / 图 | Spec slides；v2 用 `metrics` / `comparison`，不用 `value\|label` | 1–30 页 |
| DOCX | 自有 OOXML 样式、标题 1–3、表、目录域、分节 | blocks + 可选 document | 1–500 块 |
| XLSX | Excelize；说明 / 原始数据 / 计算 / 看板 | 类型化单元格；`=` 文本不当公式 | 16 表、每表 5000 行、128 列、合计 50000 格 |
| PDF | 默认 gofpdf 稳定稿；有 Typst 则尝试独立排版 | `body`；与 Word 同内容版本但不保证分页像素相同 | 受正文长度与渲染器约束 |

共同规则：

- `office.generate` 只接受匹配 kind 的字段，禁止静默丢弃其他格式内容。  
- 锁定事实必须按原文出现；禁止编造节约率 / 周报完成率。  
- 导入文件走 Patch，不从 Spec 重建。  
- 生成后自动做一次不带渲染的 Check，质量初始多为 `unverified`，不是正式通过。

### 4.5 检查、修复、正式交付

Check（`office.artifact.validate` / `Service.Check`）会写入：

- 结构、品牌、几何、事实、栅格化、字体。  
- 可选：`native_render`（LibreOffice）、`pdfa`、`visual-model`、`target-*`、`independent_pdf`。  
- 状态词：通过 / 未通过 / 待验 / 未验证 / 尚未验证 / 待检查。`unsupported` / `missing` 不得空白，也不得画成通过。  
- Formal 跳过诚实的 `target-*` / `visual-model` / `pdfa` 的 unsupported 或 missing，不把它们当硬门槛。  
- 整页栅格、事实冲突、必要渲染缺失、公式/域刷新失败会阻断正式交付。  
- 有界修复默认最多 2 轮（越界 + 重叠），不改锁定事实。  
- 规则视觉分带 `visualScore=uncalibrated`，禁止写成 85 认证。

### 4.6 修改与版本

- 文本 Patch、PPT 换图、Excel 范围 Patch、图表 Patch、`office.artifact.refresh` 刷新域/缓存、恢复旧版、接受版本。  
- 对受管稿做 Patch 后，该新版本 `contentMode` 变为 `imported`，之后不得再用旧 Spec 整份重建（会丢掉补丁）。  
- 版本不可变；「最新 / 当前查看 / 已接受」分开。  
- 冲突用 `expectedRevision` + digest，禁止静默覆盖。  
- 导入成功只表示可局部修改。  
- Word `fields_update` / Excel `full_recalculation` 在未刷新前保持 missing，刷新失败不得标通过。

### 4.7 工作台诚实文案（I1–I5 已落地）

- 检查列表使用 `officeCheckStatusLabel`。  
- `office.renderer.probe` 含 go / desktop-office / libreoffice / pdfa / vision / typst / presenton / pptxgenjs。  
- 桌面 Office 文案含「不等于检查里的打开验证」。  
- Presenton / PptxGenJS 文案含「未进生产主链」。  
- Typst 未配置时写「仍可用 gofpdf 稳定稿」，禁止「独立 PDF 不可用」。  
- 目标软件：未做打开验证，仍可导出后自行打开。  
- 保存概要 / 风格 / 品牌失败走 `setError`。  
- 导入说明：不能从成稿反推简报或规格。  
- 生成说明：生成在右侧对话；预览和阶段不是生成按钮。

### 4.8 对话办公工具

必须保持并可在当前任务上走通：

| 工具 | 作用 |
|---|---|
| `office.generate` | 按 Spec 生成并发布任务版本 |
| `office.inspect` | 分页读 text / nodes / parts / chart |
| `office.patch` | 单节点文本 |
| `office.range.patch` | Excel 矩形范围 |
| `office.image.replace` | PPT 简单嵌图 |
| `office.chart.patch` | PPT 原生图表数据 |
| `office.cache.refresh` | LibreOffice 刷新 Word 域或 Excel 缓存 |
| `office.deliver` | 指标捕获 / 套用 / 成套冻结 |

较简单的 `pptx.gen` / `docx.gen` / `excel.gen` / `pdf.gen` 继续可用，但不能绕过「禁止虚构数字」和会话产物可打开约束。

### 4.9 产品硬约束（办公外也不许破）

- `token_ledger` 冻结，不改记账语义。  
- 不引入邮件、共享日历。  
- `html.gen` 枚举不扩大。  
- `DESIGN=off` 时，导入品牌的字体/颜色不得漏进输出。  
- `LUNITIDE_CHAT_LANES=off` 时，办公对话工具与工作台路径仍必须可用。  
- 生产不引入 Electron / Python 作为运行依赖。  
- 不复制 Anthropic Office skills 源码。

---

## 5 本 PRD 功能需求（全部可完成）

编号 `CL` = Closed Loop。每条都有完成定义、现状、剩余工作。没有「以后再说」的必须项。

### 5.1 闭环主链

| ID | 需求 | 现状 | 完成定义 |
|---|---|---|---|
| CL01 | 任务与诚实 Brief | 已做成 | 未填不虚构；已填可回显；冲突拒绝保存 |
| CL02 | 对话生成四格式并落入当前任务 | 已做成 | `office.generate` 四种 kind 都出不可变版本。`TestOfficeGeneratePDFFallsIntoCurrentTask` / `TestOfficeGenerateXLSXFallsIntoCurrentTask` / `TestOfficeClosedLoopProtocol` 覆盖 |
| CL03 | 工作台预览区分概念稿与文件稿 | 已做成 | 非 PDF 默认结构预览；仅当检查证据含未失效的渲染 `pdfRef` 时 `pdfReady=true`。独立 PDF 文件本身 `pdfReady=true` |
| CL04 | 检查回执完整可见 | 已做成 | 每条有中文状态；可选缺口不压成空白 `unavailable` |
| CL05 | 局部修改与回退 | 已做成 | Patch / 换图 / 改范围出新版本；Restore 不删历史 |
| CL06 | 草稿导出与正式交付 | 已做成 | UI 用 `canFormalDeliver`（quality + 阻断检查），**不读**证据里的 `formalOk`。正式接受走服务端 `quality=passed`；导出仍看 quality。不要另做一套与 quality 打架的 formalOk 展示 |
| CL07 | 本机打开副本 | 已做成 | 打开导出副本；存档只读 |
| CL08 | 导入只做局部修改 | 已做成 | 文案与实现一致：不重建 Brief/Spec |
| CL09 | 失败可见 | 已做成 | 三个表单 + 生成/检查/同步失败都 `setError` 或检查回执 |
| CL10 | 组件探测诚实 | 已做成 | probe 含可选工具。Typst 未配置时文案写「仍可用 gofpdf 稳定稿」，禁止「独立 PDF 不可用」 |

### 5.2 必须收口的剩余缺口

这些是当前主链上仍会误导用户或让闭环不完整的点。**本 PRD 把它们定为必须做完**，做完后不再留「已知误导」。

**2026-09-12：** 下表缺口均已按「必须变成」落地。回归：`officeQualityUi` / `OfficeStudioPage` / `OfficeDeliveries` vitest，以及 `TestOfficeClosedLoopProtocol`、`TestOfficeFormalAcceptRequiresPassedQuality`、`TestOfficeCacheRefreshWithoutRendererFailsInChinese`。

| ID | 缺口 | 现在会怎样 | 必须变成 |
|---|---|---|---|
| CL11 | 独立 PDF 检查看进程不看本文件 | `IndependentPDFCheck()` 只探 Typst；`ValidateBrand` 校验时也调用它，**不绑定本文件字节**。缺 Typst 为 `missing` 且 `EvaluateQuality` 当 Formal 阻断 | **状态必须来自本版本后端标签（typst\|gofpdf），禁止 env 探测当 passed。** gofpdf 回退可正式。Typst 配置但编译失败后回退：不得写 Typst 已验证。改写 `TestIndependentPDFMissingWithoutTypstBlocksFormal`；另加「Typst 在但编译失败」测试 |
| CL12 | 「排版已检查」点灯过宽 | `qualityPromiseLabels` 只要 geometry/layout 类 id 通过就点亮 | 必须 `native_render` 或 `actual-render` 为 `passed` 才点亮「排版已检查」；否则不点亮 |
| CL13 | 导出通知不区分同源 PDF | `office.artifact.export` 固定 `ExportNotice(kind, false)` | 同源 PDF（由 Office 版本渲染且 digest 绑定）走同源通知；独立 PDF 走「分页不必与 Word 像素一致、导出≠PDF/A」 |
| CL14 | 定位只搜当前预览页 | 检查「定位内容」与指标定位都只在 `preview.nodes` 里找；不在当前页则**静默无反应** | 翻页或 inspect 直到找到；找不到必须可见「不在此版本」。禁止空操作。指标面板同一规则 |
| CL15 | 通过 ≠ 已接受正式版 | 导出/成套可能让用户以为 passed 就是正式接受 | 导出区与 bundle 写清：`passed` 只表示检查通过；「已接受」只在用户点过正式交付或接受为草稿之后 |
| CL16 | 同源 PDF 失效可见 | `InvalidateSameSourcePDF` / `BindSameSourcePDF` 已有；预览在 `pdfRef` 上会标 stale | 导出、成套、预览三处都读同一绑定；失效后有「按新版本重建」入口，不得继续把旧 PDF 当当前源正式阅读稿 |
| CL17 | 生成后工作台必刷新 | 依赖聊天 activity + `untilDeliverable` 仅 4×200ms；`office.generate` 成功路径无推送 | 生成成功后 `task.get` 必含新 Artifact（Go 测，不依赖聊天 UI）。前端：对话结束立即 sync；轮询失败用同一句中文错误。禁止只靠 4×200ms |
| CL18 | 取消与恢复 | `stopCheck` 调用**任务级** `api.cancel`，可能打断同任务生成 | 「停止检查」不得取消进行中的 `office.generate`。检查区写「已停止，检查未完成」。上一版本仍在 |
| CL30 | 必要检查的词不能伪装成可选缺口 | `remapStudioStatus` 把非 pdfa/视觉的 `missing` 改成 `unsupported`，界面显示「未验证」，看起来像 target-* 诚实缺口，但 `native_render` / `fields_update` / `full_recalculation` 仍会让 `quality` 到不了 passed | 这三类显示「待验」或「缺组件，不能正式交付」；`officeCheckStatusLabel` 可按 id 区分，禁止和目标软件同一句「未验证、仍可正式」 |
| CL31 | 正式接受要服务端守门 | `office.artifact.accept` 不看 `quality`，正式/草稿按钮走同一 API | 导出正式副本继续要求 `quality=passed`。若 UI 点「作为正式交付」，服务端必须拒绝 `quality≠passed`（`OFFICE_DRAFT_REQUIRED`）。「接受为草稿 / 使用此版」仍可接受未通过版本，但接受指针不得被导出路径当成正式 |
| CL32 | Word/PPT/Excel 正式交付依赖渲染组件 | `QualityFor` 不把 `native_render` 当诚实缺口；无 LibreOffice 时 quality 到不了 passed | **产品规则，不是缺陷：** 无 LibreOffice 时这三种格式只能草稿。独立 PDF 走 CL11 的 gofpdf，不依赖 Typst 或 LibreOffice 也能正式交付 |
| CL33 | 质量承诺词对齐真实检查 id | `qualityPromiseLabels` 用 `structure` 点亮「内容完整」，后端发出的是 `package`；「关键数字有来源」找 fact/source/metric 检查 id，主链没有这些 id。现有 vitest 用假 id 把错误行为测绿 | 「内容完整」看 `package`（PDF 看 `pdf_structure` 或 package）passed。「关键数字有来源」看任务 FactSet 已覆盖或 `FindFactRefs` 有命中，禁止虚构检查 id。改掉依赖 `layout`/`structure` 假 id 的测试 |

### 5.3 稳定、速度、效率（必须达到）

| ID | 需求 | 完成定义 |
|---|---|---|
| CL19 | 文件生成时间可预期 | 在参考机、**不含模型思考时间**、12 页 PPT / 20 块 Word / 3 表且 < 5000 格的 Excel / 短独立 PDF：先用夹具**测出** P50/P95 写入 QA 记录，再冻结门槛。建议目标 P50 ≤ 8s、P95 ≤ 20s；若首测超过，先记基线，不得为了绿而放宽「超时必须中文失败、已写版本不删」 |
| CL20 | 无渲染检查要快 | 同上规模 `Check(..., render=false)` P50 ≤ 3s，P95 ≤ 8s |
| CL21 | 单节点 Patch 要快 | 单文本节点 Patch + 发布 + 无渲染检查 P50 ≤ 4s；禁止因改一页而调用外部生成器 |
| CL22 | 渲染是慢路径 | `Check(..., render=true)` 可慢，但必须可取消；LibreOffice 不在则 `native_render=missing`，不挡草稿导出 |
| CL23 | 增量优先 | 改一页 / 一个范围只重算受影响部件；预览默认先结构，不自动整本渲染 |
| CL24 | 并发与配额 | 同一任务写入串行；配额满、租约过期、版本冲突返回已有中文码；不损坏已接受版本 |
| CL25 | 不额外烧模型 | 规划与必要视觉检查才调模型；不对每个形状发起一次调用。无视觉模型时走规则检查，状态为 unsupported/missing |

测量方法：在 `internal/officestudio` / `internal/officeapp` 用固定夹具计时，不把对话 LLM 耗时算进 CL19–CL21。B1 **先打印基线再决定是否 assert**。门槛冻结后，回归不得无说明地恶化超过 20%。n=1 基线见 `docs/qa/office-perf-baseline-2026-09-12.md`（含单节点 Patch）；`SummarizePerf.Ready` 仍为 false，禁止把本次数字写成门禁 assert。

### 5.4 四种格式必须保持的产品承诺

见第 6 节。它们是 CL26–CL29。

---

## 6 四种格式专项（可使用、可验收）

### 6.1 PPT（CL26）

**必须能用：**

- 12 种语义布局可生成；指标用结构化字段。  
- 文本、原生表、常规柱/条/折/饼图可在支持矩阵内继续编辑。  
- 整页只剩图片且无正文时，`rasterized_object` 失败并阻断正式交付。  
- 备注可写密级与来源，不把关键结论只藏在备注里作为「已交付」。  
- 溢出：换密度 / 换版 / 拆页，禁止截断正文。  
- 本机有 Word/WPS 只表示用户可自行打开，检查里 `target-*` 保持未验证。

**本 PRD 不做：** 任意母版还原、动画/SmartArt 编辑、把 Presenton 结果当生产稿。

### 6.2 Word（CL27）

**必须能用：**

- 三类模板；Heading1/2/3 跟品牌主色；段落样式而不是空行充版心。  
- 目录域存在；`office.cache.refresh` 在 LibreOffice 可用时刷新域与页码。  
- 刷新失败必须 failed/missing，并说明「DOCX 里目录缓存未更新；PDF 里看起来对也不算 DOCX 已刷新」。  
- 已填密级进页眉。  
- 支持节点可 Patch；未支持对象只读。

**本 PRD 不做：** 题注与交叉引用产品化、公文红头、扫描件 OCR 编辑。

### 6.3 Excel（CL28）

**必须能用：**

- 三类经营簿：说明、原始数据、计算、看板（小任务可不凑齐四张，但输入与推导必须分开）。  
- 打印区、冻结表头、显示格式、COUNTA 统计行、日期 ISO、`t="d"`、缺失值显示为 —。  
- 密级只写在「说明」页。  
- 公式由结构生成或白名单；外部文本 `=` 保持 text。  
- 范围 Patch 使依赖缓存失效；重算走 `office.cache.refresh`。  
- 布尔属性按 Excelize 实际值处理，不能误判 `"false"`。

**本 PRD 不做：** 宏、外部工作簿、Power Query、透视缓存往返。

### 6.4 PDF（CL29）

**必须能用：**

- **独立 PDF：** 默认 gofpdf 稳定稿始终可出**且可正式交付**；Typst 是可选出版排版，不是产品内核。CL11 修完后，检查状态与真实后端一致，缺 Typst 不再阻断 Formal。  
- **同源 PDF：** 从已通过检查的 Office 版本渲染，digest 绑定；源变则失效（CL16）。  
- 导出说明：独立 PDF 不保证与 Word 像素一致；任何 PDF 导出都不等于 PDF/A 或 PDF/UA。  
- 有验证器且有当前 PDF 字节才实跑 PDF/A；否则 unsupported/missing。

**本 PRD 不做：** 把 Typst 做成完整出版产品、印刷出血、PDF/UA 认证。

---

## 7 质量模型（正式交付只看硬门槛）

### 7.1 硬问题（阻断正式交付）

打不开；正文或锁定事实丢失或被改；整页意外栅格；Word/PPT/Excel 的必要渲染/域/重算失败；事实口径冲突；未授权资源。

硬问题不能用「看起来好看」或规则高分抵消。

**独立 PDF 缺 Typst 不是硬问题。** gofpdf 回退稿可以正式交付（CL11）。Word/PPT/Excel 缺 LibreOffice 是硬问题：只能草稿（CL32）。

### 7.2 诚实缺口（不阻断正式交付，但必须显示）

| 检查 | 未配置 / 未跑 | 已跑 |
|---|---|---|
| 目标软件打开 | `unsupported` + 可自行打开 | 本 PRD **不要求**做成 passed |
| 视觉模型 | `unsupported` / `missing` | passed 仍写 uncalibrated |
| PDF/A | `unsupported` / `missing` | passed 只表示验证器通过，不是 UA |
| Typst 出版排版 | 未用 Typst 时走 gofpdf，不挡正式 | 仅当本文件确为 Typst 输出才写 Typst |

### 7.3 用户能看见的质量承诺

只据实点亮：内容完整（CL33：看 `package`/`pdf_structure`）、排版已检查（CL12：必须真实渲染）、关键数字有来源（CL33：FactSet / `FindFactRefs`）、可继续编辑、存在需处理的问题。禁止用 vitest 假检查 id 把灯点绿。

默认界面不显示 MCP、EMU、OOXML。

### 7.4 草稿与正式

| 动作 | 条件 |
|---|---|
| 接受为草稿 / 导出草稿 | 只要版本存在 |
| 作为正式交付 | UI：`canFormalDeliver`。服务端：`quality=passed`，否则 `OFFICE_DRAFT_REQUIRED`（CL31） |
| 正式导出不带草稿标记 | `quality=passed` 且未勾草稿；`passed` 本身不是「已接受」 |

---

## 8 界面与文案合同

必须保持或按 CL 条修补，禁止再引入误导句：

- 试验范围、可用范围各保留一句，不重复「已超过」。  
- 禁止竞品得分、节约率、供应商已通过等无证据句子。  
- 生成阶段 `aria-label` 为「生成流程说明」。  
- 风格旁写「工程变体，非设计师已检 36」。  
- 品牌旁写「不还原 PPT 母版」。  
- 检查区「可用状态」按检查真实状态变化，不写死 85。

---

## 9 系统对象与不可破坏约束

沿用旧 PRD 对象，但字段以代码为准：

| 对象 | 关键字段 | 约束 |
|---|---|---|
| Brief | audience, purpose, targetLength, confidentiality, outline | 只存已填 |
| Fact | factId, value, unit, period, sourceId, locator, locked | value 不能空；锁定不可改 |
| BrandProfile | brandId, colors, fonts, chartTheme, logo | L1 导入要授权摘要 |
| Spec | schemaVersion 1\|2, kind, title, templateId, facts, 对应内容 | kind 与内容字段必须匹配 |
| Version | sha256, quality, mode managed\|imported, validations | 不可变 |
| LayoutPlan | variantId, 几何, 阅读顺序 | 测量失败可回退估算，但 `measure` 不得假称 glyph |
| RenderEvidence | artifactDigest, renderer | 缺组件不能标已验证 |
| Bundle | 固定 versionIds | 源更新使草稿套件过期，已接受不变 |

不可破坏：

1. 已确认事实与锁定节点不能被修复或换版改掉。  
2. 导入文件未知部件保留。  
3. 检查证据绑定文件摘要，不能跨版本复用绿色。  
4. 不手改 generated bridge。  
5. Formal 不得因「探测到本机 Word」而放宽。

---

## 10 明确不做（写进产品，避免再被当成缺口）

| 项 | 用户可见说法 | 原因 |
|---|---|---|
| Presenton / PptxGenJS 进 Generate | 只做对照，未进生产主链 | 另立内核会破坏版本与 Patch |
| 设计师已检 36 | 工程变体 36，已检 0 | 需要设计师人审，不是工程开关 |
| 视觉 85 认证 | 规则分未校准 | 85 是旧稿试行线，不是完成度 |
| 目标软件假 passed | 未做打开验证，可自行打开 | 注册表探测 ≠ 打开核对 |
| 从成稿反推 Brief/Spec | 导入只能局部修改 | 会丢母版/宏/未知对象 |
| PPT 母版还原 | 一级品牌不还原母版 | L2 另立 |
| FR17 企业审批 | 本期不做 | 组织产品 |
| FR18 在线协同 / Univer | 本期不做 | 许可与往返未评估 |
| 声称超过 Gamma 等 | 不比较、不写得分 | 无盲评数据 |
| 扩 html.gen | 三种模板以外请走办公生成 | 产品红线 |
| 装机 0.4.75 已修 | 需本工作区编译重启 | 版本号不是验收 |

这些不是「失败」，是范围外。界面已有或必须继续有对应说明。

---

## 11 实施计划

原则：继续当前分支，不新建平台，不换生成内核。TDD。Windows 测试需要 `required_permissions: ["all"]`。逐步执行见配套计划。每阶段有门禁，前一阶段绿才能进入下一阶段。

工作量按**一名熟悉本仓库的工程师连续实施**估算，含测试，不含设计师人审和竞品采购。

**本包禁止：** 拆分 `OfficeStudioPage.tsx`、手改 generated bridge、把 Presenton 推进 Generate、把 `designerReviewed` 改成 36、为了绿而改 Formal skip 去放过 `native_render`。

### 阶段 A — 去掉剩余误导（约 3 天）

对应 CL11–CL18、CL30–CL33。做完后**已知假状态**收口。不是整份旧 PRD 做完。

| 步骤 | 做什么 | 主要位置 | 先红后绿 |
|---|---|---|---|
| A1 | 独立 PDF 按**本文件后端**检查；gofpdf 回退可正式 | `adapter.go` `IndependentPDFCheck` / `RenderIndependentPDFWithTheme`；`ValidateBrand` 不得用进程探测当 passed；`quality.go` 不再把「无 Typst」当 blocker | 改写 `TestIndependentPDFMissingWithoutTypstBlocksFormal`：无 Typst 的回退稿 FormalOK=true，文案不含「Typst 已验证」。另加：Typst 已配置但编译失败后回退 ≠ Typst passed |
| A2 | 「排版已检查」要真实渲染（CL12） | `officeQualityUi.ts` `qualityPromiseLabels` | vitest：仅 geometry/`layout` passed 不点亮；`native_render=passed` 才点亮。必须同时改掉现有「layout 即点亮」的错误绿测 |
| A3 | 导出通知传对 `sameSource` | `office_studio.go` export 约 563 行现在写死 `false`；**bundle 同步**加 `ExportNotice` | 扩 `TestExportNoticeIndependentPDFOnly`；app 测同源 PDF notice 不含「不保证分页」 |
| A4 | 定位跨预览页（检查 + 指标） | `OfficeStudioPage` / `OfficeMetricPanel`；现只搜 `preview.nodes` | vitest：节点在后页时能找到或报「不在此版本」；禁止静默空操作 |
| A5 | passed ≠ accepted | 导出回执 + bundle 文案 | 文案同时不能把 `passed` 写成已正式接受 |
| A6 | 同源 PDF 三处都读绑定 | 预览已有 stale；补 export/bundle | 改源后三处都失效或要求重建 |
| A7 | 生成后同步可测 | 已有 activity 轮询；补「成功后列表含新文件」测试 | `office_studio_test.go` 生成后 `task.get` 含 Artifact，不依赖聊天 UI |
| A8 | 停止检查不得取消生成（CL18） | `stopCheck` 现走任务级 `api.cancel` | 「停止检查」后检查区「检查未完成」；进行中的 `office.generate` 不被取消 |
| A9 | 必要检查词（CL30） | `remapStudioStatus` 或 Inspector 按 id 显示 | `native_render` missing 不是「未验证」那句可选缺口 |
| A10 | 正式 accept 服务端守门（CL31） | `office.artifact.accept` + 可选 `formal` 字段须走 `generate:bridge`，禁止手改 generated | 未通过版本点正式接受失败；接受为草稿仍成功 |
| A11 | 承诺词对齐真实检查 id（CL33） | `qualityPromiseLabels` | 「内容完整」看 `package`/`pdf_structure`；「关键数字有来源」看 FactSet/`FindFactRefs`。禁止 `structure`/`fact` 假 id 把测写绿 |

**阶段 A 门禁（必须整包，禁止用一条宽 `-run` 冒充）：**

```text
go test ./internal/officestudio ./internal/officeapp ./internal/officerender -count=1 -timeout 180s
go test ./internal/app -count=1 -timeout 180s -run "TestOffice|TestStudio|TestGenerate"
npx vitest run src/officeStudio
go build ./cmd/engine ./cmd/desktop
```

`TestTargetAppCoverageChecksStayUnsupported`、Formal skip `target-*`/`visual-model`/`pdfa`、`visualScore=uncalibrated`、`designerReviewed=0` 必须仍绿。

### 阶段 B — 速度与稳定（约 3–4 天）

对应 CL19–CL25、CL24。

| 步骤 | 做什么 | 验证 |
|---|---|---|
| B1 | 给 Generate / Check(false) / 单节点 Patch 加固定夹具计时，**先打印**当前 P50 | 写入 `docs/qa` 或测试日志；达标后再加 assert，禁止首测就按 8s 硬红 |
| B2 | Patch 路径确认不走整份 Spec 重建（导入稿保持部件级写入） | 单测 changedParts 只含目标部件 |
| B3 | 预览默认结构；仅用户点「检查此版本」才 render=true | UI 测试：未检查前不是「文件排版预览」 |
| B4 | 渲染 Check 可 cancel，取消不删版本 | 已有 cancel 测试补「检查中停止」 |
| B5 | 任务写锁与冲突回归 | 并发 update/patch 一个成功一个冲突 |

**阶段 B 门禁：** 阶段 A 门禁 + 计时夹具有记录。localStorage 的空 catch 保持（视图状态可选），禁止新增吞掉办公写入失败的空 catch。

### 阶段 C — 四格式可靠性（约 3–5 天）

对应 CL26–CL29 里仍不稳的工程点，不扩产品范围。

| 步骤 | 做什么 | 验证 |
|---|---|---|
| C1 | Word：三类模板各一份夹具，目录域存在；无 LibreOffice 时 refresh 失败诚实 | `internal/officestudio` Word 测试 |
| C2 | Excel：三类模板夹具；说明页密级；COUNTA；日期 ISO；空表保留 BrandID | 现有 wave 测试保持 + 补缺口 |
| C3 | PPT：12 布局各至少一页不截断；整页栅格阻断 Formal | **须新增**夹具测试（现有布局/栅格单测不覆盖 12 布局各一页）；命名写入 `layout_generate_test.go` 或等价文件 |
| C4 | 独立 PDF 与同源 PDF 两条夹具走完 CL11/CL16 | 新/改 Go 测试 |
| C5 | `office.cache.refresh` 无组件时中文失败，有组件才新版本 | app/office 测试 |
| C6 | 对话桥 `office.generate` 补 `kind=pdf`（建议同时补 xlsx） | `internal/app/office_studio_test.go`：`executeUserTool(..., "office.generate", {kind:pdf})` 出不可变版本 |

**阶段 C 门禁：** office 包全量 + `internal/app` Office/Studio/Generate + vitest officeStudio + build。

### 阶段 D — 端到端验收与发布准备（约 2 天）

对应 S1–S6。不包含竞品盲评、设计师签 36、校准 85。

| 步骤 | 做什么 | 完成定义 |
|---|---|---|
| D1 | **新建** `docs/qa/office-closed-loop-protocol-2026-09-12.md`，按 CL01–CL33 / S1–S6 写闭环脚本。试验协议 `office-trial-protocol-2026-09-12.md` 仍挂旧 trial-ready 规格，**不得当 D2 清单** | 新脚本每步有通过/失败；范围内失败才算本 PRD 失败 |
| D2 | 用本工作区 `go build` 出 desktop/engine，本地启动走一遍脚本 | 记录任务 ID 与四个文件；**不得用未重启的 0.4.75 当结论** |
| D3 | 更新看板：本 PRD 条目 100%；旧商业 PRD 仍不是 100% | 不写真机已修，除非 D2 用新二进制做完 |

**阶段 D 门禁：** D2 主路径通过；A–C 门禁仍绿。

### 阶段 E — 本 PRD 关闭

同时满足：

1. CL01–CL33 在第 5–7 节的完成定义上全部为真。  
2. 第 3 节每条路径都能演示到四态之一。  
3. 第 10 节不做项仍有用户可见说明。  
4. 第 12.1 节门禁命令全绿（整包，不是滤掉的 `-run`）。  
5. 用**新编译**桌面端走完闭环脚本。

关闭后的产品状态：

> 用户可以在 Lunitide 里用对话生成四种办公文件，在办公工作台检查和改稿，导出草稿或正式副本，本机自行打开。可选组件没有时草稿仍可用。这就是完整可用的办公平台。

不是：设计师签过的模板库、目标软件认证、出版内核、协同编辑。

### 建议顺序与依赖

```text
A1（先改测试再改实现）
A2 A9 A10 A11   可并行
A3 A5 A6        可并行
A4 A7 A8        可并行
     ↓
B1–B5           计时建立在 A 的正确状态上
     ↓
C1–C5           格式夹具
     ↓
D1–D3 → E
```

不要并行另开 Presenton 主链或模板设计运动。那会重新把合同做漏。

---

## 12 验收清单（给实施与人测共用）

人测只验证本清单。清单外的「不够精美 / 没有母版 / 没有审批」不是本 PRD 失败。

### 12.1 工程门禁（每次阶段结束）

```text
go test ./internal/officestudio ./internal/officetools ./internal/officeapp ./internal/officerender ./internal/domain/officestudio ./internal/contract -count=1 -timeout 180s
go test ./internal/app -count=1 -timeout 180s -run "TestOffice|TestStudio|TestGenerate"
npx vitest run src/officeStudio
go build ./cmd/engine ./cmd/desktop
```

### 12.2 产品脚本（新二进制）

1. 打开办公工作台，能读到试验范围、可用范围、导入限制、生成在对话。  
2. 新建任务，概要为未填写；填写受众/用途/页数/密级后能保存并回显。  
3. 对话生成 PPTX、DOCX、XLSX、PDF；工作台出现四个文件。  
4. 检查列表每条有中文状态；目标软件/PDF/A/视觉未配置时不是空白、不是通过。  
5. 改一处正文出现新版本，旧版本仍在。  
6. 未通过时正式交付禁用；导出草稿成功。  
7. 通过后可正式交付；导出通知不把独立 PDF 说成与 Word 像素一致。  
8. 导入一个文件，说明不能反推简报；可改支持节点。  
9. 登记无效品牌时按钮禁用；有效登记不声称还原母版。  
10. 保存概要制造失败时出现 alert，输入还在。  
11. 探测组件：无 Typst/验证器/视觉模型时为未配置且不挡草稿；Presenton 写未进主链。  
12. 停止一次检查，上一文件还在。

### 12.3 明确不算失败

目标软件未验证；视觉/PDF/A 未配置；规则分未校准；36 变体未设计师签字；没有竞品分数；企业审批与协同入口写本期不做；旧安装包 0.4.75 行为。

---

## 13 性能、成本、部署（可执行部分）

| 项目 | 本产品约定 |
|---|---|
| 运行形态 | 现有 Go 引擎 + WebView2 桌面；LibreOffice / Typst / PDF/A / 视觉模型均为可选进程 |
| 模型 | 用户已配置的供应商；办公生成的文件字节不依赖付费设计 API |
| 成本 | 对话规划消耗 token；文件生成本身是本地 CPU。不承诺无限免费生图 |
| 降级 | 任何可选组件缺失 → 草稿可用 + 检查诚实。不得假装已验证 |
| 部署 | 发布用本仓库编译的 engine/desktop。能力用已有 `LUNITIDE_OFFICE_*` 开关关闭写入，已有文件仍可看、可导出 |
| 参考机 | 记录机型、内存、是否安装 LibreOffice；计时分层：内核 / 渲染 / 模型 |

旧 PRD 里 12 页 PPT「含模型 120 秒」仍可作为**对话体验**观察值，但本 PRD 门禁用第 5.3 节的内核计时，避免把 LLM 快慢当成办公引擎回归。

---

## 14 风险（只保留仍影响本闭环的）

| 风险 | 信号 | 处理 |
|---|---|---|
| 结构预览被当成交付预览 | 用户没检查就对外发 | 文案 + `pdfReady` 规则（CL03） |
| 修复改数字 | 锁定事实消失 | BoundedRepair 事实锁；失败阻断 Formal |
| 可选组件被当成已认证 | 目标软件/视觉/PDF/A 变绿 | 禁止假 passed；Formal skip 保持 |
| 范围膨胀 | 又把 36 已检、Presenton、协同写回必须表 | 驳回，另立 PRD |
| 用旧安装包验收 | 版本号 0.4.75 | 验收记录必须写二进制来源 |

---

## 15 旧 FR01–FR18 映射

| 旧编号 | 本 PRD | 说明 |
|---|---|---|
| FR01 Brief | CL01 | 完成定义改为诚实字段，不再要求「缺偏好就默认管理层」 |
| FR02 品牌 | CL 品牌段 + 4.3 | L1 可用；母版还原不做 |
| FR03 叙事 | 生成 Spec + outline | 每页 purpose/claim 已有字段则写入；不发明未填大纲 |
| FR04 36 已检变体 | 4.3 + 第 10 节 | 工程 36 已有；已检不是本 PRD 必须项 |
| FR05 选版不截断 | CL26–CL28 | 保持测量与拆页；glyph 失败用估算并标明 |
| FR06 原生可编辑 | CL26–CL28 | 支持矩阵内可编辑；整页栅格阻断正式 |
| FR07 渲染诊断 | CL04、CL12、CL22 | 有组件才验证；无组件 missing |
| FR08 有界修复 | 4.5 | 最多 2 轮，事实锁 |
| FR09 局部修改 | CL05、CL08 | 已做成 |
| FR10 Word | CL27 | 三类模板 + 域刷新诚实 |
| FR11 Excel | CL28 | 三类经营簿 |
| FR12 同源 PDF | CL13、CL16、CL29 | 必须收口 digest 与文案 |
| FR13 跨文件指标 | 3.2 查找指标 | 当前任务/已检节点，不是全库图 |
| FR14 品牌导入 | 4.3 | L1；L2 不做 |
| FR15 独立 PDF/Typst | CL11、CL29 | Typst 可选，检查必须诚实 |
| FR16 外部生成器 | CL10 | 对照即可，不进主链 |
| FR17 | 第 10 节 | 不做 |
| FR18 | 第 10 节 | 不做 |

---

## 16 代码与文档证据索引

| 主题 | 路径 |
|---|---|
| 工作台页 | `web/src/officeStudio/OfficeStudioPage.tsx` |
| 检查与词表 | `web/src/officeStudio/OfficeInspector.tsx`、`officeQualityUi.ts` |
| Bridge 方法清单 | `api/bridge/v1/envelope.schema.json` |
| 任务/生成/检查 | `internal/officeapp/service.go` |
| Spec 生成 | `internal/officestudio/generate.go`、`prepare.go` |
| 布局与 36 变体 | `internal/officestudio/layoutplan.go`、`variants.go` |
| Word / Excel 模板 | `wordplan.go`、`workbook.go`、`templates/*.json` |
| 质量与 Formal | `internal/officestudio/quality.go` |
| PDF/A、Typst、导出说明 | `internal/officestudio/adapter.go` |
| 视觉模型 | `internal/officestudio/visual_model.go` |
| 桌面探测 | `internal/officerender/desktop_apps.go` |
| 对话工具 | `internal/app/office_tools.go` |
| Studio 路由 | `internal/app/office_studio.go` |
| 试验脚本（旧） | `docs/qa/office-trial-protocol-2026-09-12.md` |
| 闭环验收脚本（D1） | `docs/qa/office-closed-loop-protocol-2026-09-12.md` |
| 闭环引擎协议（D2 引擎） | `internal/app/office_studio_test.go` `TestOfficeClosedLoopProtocol` |
| 计时基线 | `docs/qa/office-perf-baseline-2026-09-12.md` |
| 旧研究 PRD | `docs/design/PRD-office-quality-commercial-2026-09-11.md` |

---

## 17 关闭声明

当第 11 节阶段 E 的五条都满足时，签署：

- 本 PRD 1.1 关闭。  
- 办公平台按本文定义完整可使用。无 LibreOffice 时 Word/PPT/Excel 仍只能草稿，这是门槛不是漏做。  
- 旧商业研究 PRD 仍可作设计与许可附录，但其 100% 不是本产品发布条件。  
- 其后若要做母版还原、Typst 出版产品、Presenton 第二内核、企业审批或协同，必须新开 PRD，不得悄悄改本文件完成定义。
