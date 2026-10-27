# PRD · Office Studio 视觉架构四工程（母版继承 / 关系图 / 图标库 / 视觉QA）

日期：2026-10-27
状态：已评审，进入实施
范围：internal/officetools、internal/officestudio、internal/officeapp、internal/app（PPT 生成链）

---

## 1. 背景与目标

上一阶段已完成：九套品牌主题、16 种语义版式、原生可编辑图表（确定性色板）、引用溯源 facts、资产报错修复、全量闸门绿（覆盖率 75.6% ≥ 51%）。对标 Kimi/TRAQwerk 做 PPT 仍有四项**架构级**差距，本 PRD 将其一次补齐：

| # | 工程 | 差距（现状证据） | 目标 |
|---|------|-----------------|------|
| ① | 母版/版式继承 | `ooxml.go:93-103` slideMaster1.xml/slideLayout1.xml 硬编码 `0B1F3A` 默认色，与品牌主题脱节；整包只有 1 个版式 | 品牌+版式写入母版层；换 brandId 时 master/layouts/theme/slide 全链一致；PowerPoint 打开可看到真实的 16 个品牌版式 |
| ② | 关系图引擎 | 结构类内容只能退化成 bullets（structure→content 归一）；无原生关系图/流程图 | 新增 diagram 版式：nodes/edges JSON → 确定性分层布局 → 原生 p:sp + p:cxnSp 连接符（SmartArt 禁用，保持全可编辑） |
| ③ | 图标库 | 幻灯片只有图片+图表两种视觉对象，无矢量图标语义 | 内置 MIT Bootstrap Icons 子集（48 个），按主题色染色，纯 Go 栅格化为 PNG 注入既有 images 管线 |
| ④ | 视觉 QA | `visual_model.go:37` ExtractVisualPages 返回 "per-page raster unavailable"——PDF→PNG 渲染管线 `doctext.RenderPDFPages`（Windows WinRT）已存在但从未接通；渲染像素层零检查 | 接通逐页渲染 + 新增像素级确定性检查（页数一致/非空白/边框溢墨）+ 视觉模型可选链路 |

**非目标**：不改 9 套主题色值、不动图表 XML 字节稳定性约束（4 处调用点比对）、不引入 CGO/外部 Go 依赖、不写 SmartArt data1.xml、UI 不改。

---

## 2. 现状审计（代码证据，实施前必读）

### 2.1 生成主链
```
officestudio.Generate(spec)                        internal/officestudio/generate.go:19
  ├─ PrepareManagedSpec / ApplyNarrativePlan        prepare.go / narrative.go
  ├─ keepSlideNarrative → normalizeSemanticLayout    layoutplan.go:338（trend→metrics，process/structure/evidence→content）
  ├─ officetools.GenStudioPptxThemed                studio_pptx.go:40（版式 switch 16 枚举在此）
  │    └─ GenPptx → slideMasterXML/slideLayoutXML/themeXML  ooxml.go（母版层硬编码默认色）
  ├─ addSlideImages                                 images.go:273（ULID sourceId+SHA256 校验、rels、[Content_Types] 追加）
  ├─ addSlideCharts                                 charts.go（chartXML 4 调用点字节一致约束）
  └─ BoundedRepair                                  quality.go:162 → geometry.go 溢出+重叠修复
```

### 2.2 关键约束（不可破坏）
- `chartSeriesPalette`/`chartSeriesStyleXML` 输出必须字节稳定（chart_nodes.go:115 managedChartBytesEqual 逐字节比对）。
- 几何扫描器 `scanSlideGeometry`（geometry.go:475）把 `cxnSp`/`grpSp` 标为 unsupported（info 级，不参与自动修复）——关系图连接符依赖此行为避免被挪动。
- `ValidatePptx`（pptx_validate.go）做全包 rels/content-types 校验——新增 part 必须同步 `[Content_Types].xml` 与 master 的 `sldLayoutIdLst`。
- 前端提示词：`chat_ppt_workflow.go` 第 7/9 步列举 16 版式；工具 schema 在 `office_tools.go:40`。
- 图表/图片注入走 zip 追加而非重写：`rewritePackage`/`appendXMLChild` 保序保前缀。
- LibreOffice 本机未安装（探测已验证 MISSING）：视觉 QA 必须可无 LibreOffice 降级运行，全部测试不得依赖 soffice。

### 2.3 视觉 QA 既有资产（本次主要是"接线"）
- `officerender.Renderer.Render`：PPTX→PDF（LibreOffice headless，带宏安全隔离）——已存在，可用则用。
- `doctext.RenderPDFPages(ctx, pdf, pages)`：Windows 经 PowerShell/WinRT 渲染 PDF 页为 PNG（`pdf_render_windows.go`，有信号量+超时+大小预算）——已存在，未接通。
- `visual_model.go`：`VisualModelRequest` + 外部视觉模型可执行文件（`LUNITIDE_OFFICE_VISION`）+ 测试钩子 `SetVisualPageImagesForTest`——框架已存在，缺生产接线。
- 接线点：`officeapp/native_checks.go:76 applyUsabilityChecks(checks, pdf)`（唯一生产调用方，`Service.Check` 有 ctx）。
- import 方向安全：doctext 仅依赖 commandworker/xlsxrows，officestudio 可安全 import doctext（无环）。

---

## 3. 工程定义（数据模型 / API / 文件级改动 / 验收）

### 工程④ 视觉 QA 接线与像素级检查（最小、先行，为其余工程提供渲染证据）

**数据模型**（新，officestudio/render_qa.go）：
```go
// RenderedPageQA 用确定性像素规则检查已渲染页面 PNG。
// 输入 pages 为逐页 PNG 字节；expectedPages<=0 时跳过页数一致性。
func RenderedPageQA(pages [][]byte, expectedPages int) []Check
// 常量：renderQABorderRatio = 0.0125（边框带 1.25%）、
//       renderQAInkMin = 0.002（全页着墨率下限 0.2%）、
//       renderQABorderInkMax = 0.02（边框带着墨率上限 2%）
```
检查项（Check ID `render-pixel-qa`，可选级，不进 blocker）：
- `page-count`：渲染页数 == 幻灯片数（不一致 → failed，其余页 → 每页一行明细）。
- `blank-page`：任一页着墨率 < 0.2% → failed（空画布/渲染丢失）。
- `border-ink`：任一页外圈 1.25% 边框带着墨率 > 2% → warning（内容顶边溢出被裁切的信号）。
- 解码失败 → failed（渲染产物损坏）。

**API 变更**：
```go
// officestudio/visual_model.go
func ExtractVisualPages(ctx context.Context, pdf []byte) ([][]byte, error)
// 生产实现：officestudio 内解析 PDF 页树得 pageCount（复用 pdf_checks 的页解析），
// 调 doctext.RenderPDFPages(ctx, pdf, 1..pageCount)。测试钩子 SetVisualPageImagesForTest 优先。
```
- 调用方 `officeapp/native_checks.go applyUsabilityChecks` 签名 → `(ctx context.Context, checks []domain.Check, pdf []byte, slideCount int)`；`Service.Check` 传入 len(published.Slides)。
- 提取成功后：像素 QA 必跑；`LUNITIDE_OFFICE_VISION` 已配置时再跑视觉模型（现状不变）。
- PDF kind：slideCount=0 → 只跑非空白/边框检查。
- LibreOffice 缺失 → native_render 已是 unsupported（现状），像素 QA 无 PDF 可分析 → 输出 `unsupported`，不算失败（优雅降级闭环）。

**验收标准**：
- A4-1 `ExtractVisualPages` 在 Windows 对真实 PDF 返回 ≥1 页 PNG（用 officetools 生成的最小 PDF 集成测试；非 Windows 编译降级路径保持编译绿）。
- A4-2 像素 QA 单测：合成 PNG（纯白 1x、着墨 2%、边框着墨 3%、页数不符）四类样例分别命中。
- A4-3 无 LibreOffice/无 PDF/无视觉模型三种环境下 `Check` 流程不新增 blocker（回归 officeapp 既有测试）。
- A4-4 doctext 渲染预算约束：页数 ≤ min(幻灯片数,100)，PNG 单页 ≤ MaxInputBytes，超限返回错误不 panic。

### 工程① 母版/版式继承（品牌一处生效）

**数据模型**（officetools）：
```go
// ooxml.go：母版/版式从常量改为函数
func slideMasterXMLFor(theme SlideTheme, layouts []LayoutDef) string
func slideLayoutXMLFor(t SlideTheme, def LayoutDef, index int) string
type LayoutDef struct { Layout string; Type string } // 语义名 → ST_SlideLayoutType
// 映射表（16 语义版式）：cover→title, section→secHead, content→obj, two-column→twoColTx,
// comparison→comparison, quote→tx, metrics→obj, timeline→obj, agenda→twoColTx,
// closing→title, table→tbl, conclusion→title, trend→obj, structure→obj,
// process→obj, evidence→obj（不在 ST 枚举的一律 custom 兜底）
```

**文件级改动**：
- `ooxml.go`：`ThemeXMLFor` 保持；新增母版生成——母版 bg 用 theme.Paper、txStyles title/body/other 色用 theme.Navy/Ink、`sldLayoutIdLst` 列出全部 16 版式（rId1..rId16）、母版 rels 对应 16 个 layout part + theme；每个 layout 含 title/body 占位符 sp（几何与 studio 版式一致：标题 520000,260000,11000000,720000，正文区 520000,1900000…）、品牌色描边。
- `studio_pptx.go GenStudioPptxThemed`：replacements 增加母版/16 layout/rels/[Content_Types].xml（contentTypesPptx 参数化 layout 数）；复制循环后**追加写入** zip 中不存在的 part（layouts 2..16 及其 rels）。
- 每页 slide rels 指向其语义 layout part（cover→slideLayout1、section→slideLayout2 …按映射表序号）；`presentation.xml` 不变（layout 归母版管）。
- GenPptx(旧签名) 保持旧行为输出（经典主题），GenStudioPptxThemed 走新母版链——两者字节不同但 ValidatePptx 均须通过。
- 幻灯片正文继续显式 srgbClr（保住几何扫描/前端画布/图表字节稳定三条链）——母版层是设计承载与 PowerPoint 可编辑层，不参与运行时渲染决定。

**验收标准**：
- A1-1 生成的 pptx：`ppt/slideMasters/slideMaster1.xml` 的 bg/txStyles 色值 == ResolveTheme(brandId) 对应值；换 brandId（如 consulting-blue）后 master/layout/theme 三类 part 字节变化、且新值 == 新品牌（断言 navy=16324F）。
- A1-2 每页 slide 的 rels 指向正确语义 layout；[Content_Types].xml 有全部 16 个 layout Override；母版 rels/sldLayoutIdLst 数量一致。
- A1-3 ValidatePptx 通过；PowerPoint/WPS 打开无修复提示（以 ValidatePptx 的 rels/content-type 全量校验为准 + 结构断言）。
- A1-4 全量回归：officetools/officestudio/officeapp 三包测试绿；前端 SlideCanvases 预览不变（slide XML 未动）。
- A1-5 同 theme 输入两次生成字节一致（确定性）。

### 工程② 关系图引擎（diagram 版式）

**数据模型**（officestudio/types.go + officetools/studio_pptx.go）：
```go
type DiagramNode struct { ID, Label, Kind string }   // kind: start|process|decision|end|group（样式提示，可选）
type DiagramEdge struct { From, To, Label string }   // label ≤24 runes，可空
type DiagramSpec struct { Direction string; Nodes []DiagramNode; Edges []DiagramEdge } // auto|down|right
// Slide 增加字段：Diagram *DiagramSpec `json:"diagram,omitempty"`
// officetools.SlideSpec 增加：Diagram *SlideDiagram（已布局：节点带 x/y/w/h，边带连接侧）
```

**校验规则**（officestudio/diagram.go `validateDiagram`，ErrFormat/ErrLimit 语义化）：
- 2–12 节点、1–16 边；label ≤24 runes；From/To 必须引用存在且不重复的节点 ID；有向无环（拓扑排序失败 → 明确报错并提示改用 timeline/process 版式）；diagram 版式下禁 bullets/rows/images/charts/metrics（title/subtitle/notes 允许）。

**确定性分层布局**（officestudio/diagram.go `layoutDiagram`，纯函数）：
- 分层：源点层 0，节点层 = max(前驱)+1（最长路径分层）。
- 层内排序：一次 barycenter 传递减少交叉，同分稳定（按输入顺序 tie-break）。
- 方向：auto = 层数 ≥4 或层宽 ≤2 时用 down（纵向堆叠），否则 right（横向流程，适配 16:9）。画布区：x 520000–11672000、y 1900000–6180000；节点盒宽按 label 估宽（EstimateMeasure）clamp 到 1600000–3000000，高 620000；行列间距均分。
- 输出 positioned 节点 + 边（fromSide/toSide 由相对位置推导：目标在右侧→源 right/目标 left；下侧→bottom/top）。

**XML 生成**（officetools/studio_pptx.go 新 case "diagram"）：
- 节点：`p:sp` roundRect（start/end 用 pill `prst="roundRect"`+全圆角 or theme.Teal/Gold 填充强调，process 白底 Navy 1pt 描边，decision 用 `prst="flowChartDecision"`），文字居中 sz=1400，连接符可吸附（`<a:stCxn id="{shapeID}" idx="{side}"/><a:endCxn …/>`）。
- 边：`p:cxnSp` straightConnector1，线色 theme.Muted w=19050，箭头 `<a:tailEnd type="triangle"/>`；边 label 用中点小字（sz=1100，theme.Muted）。
- 页眉/页脚/背景与其他 content 版式一致（Header 条 + 页码）。

**管线接线**：
- `layoutplan.go normalizeSemanticLayout`：新增 "diagram" 原样保留（加 case）。
- `generate.go`：diagram 校验后透传 officetools（布局后）。
- `narrative.go ApplyNarrativePlan`：复制 Slide 时带上 Diagram（回归断言）。
- `office_tools.go:40`：layout 枚举加 "diagram"；slide properties 加 `diagram` 对象（nodes/edges/direction，带 maxItems 与描述：ID 引用、DAG、2-12 节点）。
- `chat_ppt_workflow.go` 第 7/9 步：补一句"组织架构/流程/因果链用 diagram 版式（nodes+edges）"。
- QA：连接符被几何扫描标 unsupported（info）→ 不参与重叠修复（预期行为，测试固化）；节点 sp 参与常规几何检查。

**验收标准**：
- A2-1 合成 3 层 7 节点 DAG → 输出含 7 个 roundRect sp + n 个 cxnSp；全部节点在画布内（geometryIssues 零 overflow）；ValidatePptx 通过。
- A2-2 环形输入被拒（明确错误文案）；超 12 节点被拒；diagram+bullets 组合被拒。
- A2-3 同输入两次生成字节一致；布局函数单测覆盖 down/right 两个方向与 barycenter 排序。
- A2-4 Inspect 提取文本包含全部节点 label（可被 office.inspect 读到）。
- A2-5 生成一个含 diagram 的端到端 pptx 过 BoundedRepair 无 blocker。

### 工程③ 图标库（MIT Bootstrap Icons 子集）

**数据模型**（officestudio/icondata.go + icons.go）：
```go
// 48 个内置图标（Bootstrap Icons v1.11 MIT，viewBox 0 0 16 16，filled path）
var iconCatalog = map[string]string{ "rocket": "M8…", "people": …, … }  // name → SVG path d
type SlideIcon struct { Name string; X, Y, Size int64; Color string } // Size=边长 EMU；Color 可空→theme.Teal
// Slide 增加字段：Icons []SlideIcon `json:"icons,omitempty"`
```
选定清单（48）：rocket, bullseye, graph-up-arrow, bar-chart-line, people, person-badge, shield-check, lightbulb, gear, wrench, database, cloud, globe, link-45deg, check-circle, exclamation-triangle, cash-coin, trophy, star, bookmark, box, clipboard, briefcase, building, calendar, chat, clock, compass, cpu, flag, funnel, handshake, key, layers, list-check, lock, mail, map, mic, phone, pin-map, plug, search, send, sliders, stack, tag, arrow-up-right。

**纯 Go SVG→PNG 栅格化**（officestudio/icons.go，零新依赖）：
- path 解析：M/m L/l H/h V/v C/c S/s Q/q T/t A/a Z/z；A 椭圆弧→三次贝塞尔（标准端点参数化算法）；贝塞尔自适应展平（16 段）。
- 填充：扫描线 nonzero winding fill；4x SSAA（先渲 4 倍图再 4x4 盒式降采样成 16 级 alpha）。
- 目标像素：px = EMU/9525，最小 16px 上限 256px；单图 PNG < 128KB（预算断言）。
- 染色：全前景色 = theme 色（默认 Teal），背景透明（RGBA）。
- 注入：`Generate` 中把 Icons 物化为 SlideImage（ulid.Make() 生成 SourceID、SHA256=内容摘要、fit="contain"、alt="图标 name"），**先于**用户 images 传给 addSlideImages（用户图永远在上层）——完全复用已验证的注入/去重/校验/QA 管线。

**管线接线**：
- `office_tools.go`：slide properties 加 `icons`（name 枚举在 description 里全列出，x/y/size/color）；未知图标名 → ErrFormat 报错并列出可用名。
- `chat_ppt_workflow.go` 第 9 步：补"指标/议程/流程页可用 icons（内置 48 个 MIT 图标名）点睛，size 建议 480000–960000 EMU"。
- 图标对象计入 RasterizedObjectCheck？**不计**——图标是装饰性矢量栅格化，与"整页栅格冒充排版"不同：维持 rasterized_object 语义（只统计 whole-slide 场景），但 Inspect 节点 alt 带 "图标 name" 可追溯。

**验收标准**：
- A3-1 每个 catalog 图标：栅格化成功、PNG 可解码、非全透明（有非零像素）、像素色 == 主题色（采样验证）、单图 <128KB——表驱动全 48 项测试。
- A3-2 path 解析器单测：含 A 弧/Q 曲线/多子路径的合成 path 坐标正确展平。
- A3-3 端到端：含 2 个图标的 metrics 页生成成功，media 部件/rels/alt 正确；换品牌色后图标像素色跟随变化。
- A3-4 未知名/越界坐标/超限尺寸被 ErrFormat/ErrLimit 拒绝。
- A3-5 两次生成同输入字节一致（PNG 确定性：固定 zlib 无时间戳——Go image/png 默认满足）。

---

## 4. 实施顺序（依赖论证）

```
第1段  工程④ 视觉QA接线（独立、最小；后续三段可用它做渲染自检）
第2段  工程① 母版/版式继承（改 GenStudioPptxThemed 包结构，diagram 版式要往新母版机制里挂）
第3段  工程② 关系图（依赖①的版式注册位；新增 layout 17 号）
第4段  工程③ 图标库（独立于①②，但放在其后避免 office_tools.go schema 三次改）
每段完成即跑：go build ./... + go test ./internal/officetools/ ./internal/officestudio/ ./internal/officeapp/ + go vet
全部完成：全量闸门（gates.md 顺序，覆盖率棘轮 ≥51%）
```

## 5. 测试与质量门

- 单测随实现落位：render_qa_test.go、master_test.go（officetools）、diagram_test.go、icons_test.go、native_checks 回归。
- 像素测试用 Go 合成 PNG（不落盘外部 fixture）；Windows PDF 渲染集成测试走 doctext 既有 harness。
- 不新增任何依赖（go.mod 不动）；不引入 CGO；icondata.go 头部保留 Bootstrap Icons MIT 许可声明。
- 生成物漂移终检五文件不受影响（bridge/catalog 生成物与本次无关）。

## 6. 风险与降级

| 风险 | 缓解 |
|------|------|
| 新增 layout part 破坏 ValidatePptx/WPS 兼容 | content-types/rels/master 列表三处同步生成；ValidatePptx 全量校验 + 结构断言测试 |
| cxnSp 被质量门误报 | 现有几何扫描明确标 unsupported(info)；测试固化该行为 |
| 图标栅格化边缘锯齿 | 4x SSAA + 16 级 alpha；≥16px 下限 |
| LibreOffice 缺失使视觉 QA 空转 | 像素 QA 在 PDF kind 与任何已渲染预览上仍生效；无 PDF 时输出 unsupported 不失败 |
| 图表字节约束回归 | 不触碰 charts.go 任何输出路径；跑 chart_nodes 现有测试 |
| 16 版式枚举与前端 catalog 校验 | verify:catalog 闸门纳入回归 |

## 7. 里程碑验收（Definition of Done）

- [ ] M1 ④：ExtractVisualPages 生产接线 + render-pixel-qa 上线，无 LibreOffice 环境全链不新增 blocker
- [ ] M2 ①：换 brandId 单测证明母版/版式/主题三处一致变化，PowerPoint 结构校验全绿
- [ ] M3 ②：diagram 版式端到端可用，DAG 校验+确定性布局+原生连接符
- [ ] M4 ③：48 图标全部栅格化并通过像素断言，images 管线复用零改
- [ ] M5 全量闸门绿（go 三包 + web + lint + 覆盖率 ≥51% + audit），工作树除本次改动外无漂移
