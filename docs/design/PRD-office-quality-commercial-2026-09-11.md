# Lunitide 高品质办公文档平台研究与产品需求

## 1 核心结论

Lunitide 应将下一阶段的重点放在文档设计系统上：把业务内容转化为合适的叙事结构，通过经过设计的模板和布局约束生成原生可编辑文件，再以实际渲染结果完成检查和局部修复。增加模型、skills 或 MCP 可以补充能力，但无法单独保证审美、品牌一致性与交付稳定性。

**推荐路线是保留现有 Go Core、Office Studio、原生 Office 文件处理和版本体系，新增统一品牌规则、语义版式库、模板编译与视觉验收。** Presenton 作为开源 PPT 对照方案和可选适配器；PptxGenJS 作为新建复杂幻灯片的候选渲染后端；Excelize 继续承担 Excel；Word 优先扩展现有原生模板能力；PDF 区分 Office 同源导出与独立出版排版。

现阶段最有商业价值的产品承诺是：**一份资料生成品牌统一、数字一致、可以继续编辑的汇报与报告，修改一处后按需同步到其他交付物。** 对应的交付包是 PPTX、DOCX、XLSX 和 PDF。首发应聚焦经营汇报、客户方案和正式报告，先在明确场景中达到稳定质量，再扩大模板和编辑能力。

需要明确的五项决策：

1. 将“精美”定义成可检验的阅读层次、版式适配、字体、素材、数据图表和品牌规范，而非提示词中的形容词。
2. 采用“模型规划内容与选择方案，程序执行布局与生成文件”的分工；自动修复不能改写已确认数字或删除正文。
3. PPT、Word、Excel、PDF 共享事实来源与品牌，但采用各自的文档结构，避免用同一种卡片布局处理所有格式。
4. 免费方案按源码许可、运行成本、模板素材授权、外部 API 权益分别核验；免费安装不等于免费商业嵌入。
5. 以真实文件的盲评、编辑测试和兼容性测试判断是否达到竞品水平。当前不应声称已超过 Gamma、Plus AI 或 Beautiful.ai。

## 2 研究范围与证据边界

本文件同时服务产品负责人、设计负责人和研发负责人。事实核验日期为 **2026 年 9 月 11 日**，默认目标客户为中国市场的中小企业、咨询服务团队和专业个人，沿用 Lunitide 本地优先、Windows 桌面、可配置模型供应商的产品定位。

证据分为三类：官方产品与许可证说明；当前工作区代码；本报告提出的产品判断与目标。下文的功能目标、成本算例、工期与评分阈值均属于建议，不能解读为已经实现或已经测得的结果。

竞品能力来自公开官方资料，未使用付费账号执行同题生成，也未完成其 PPTX 在 PowerPoint、WPS 中的实测。因此不采用“用户量最大”“多数人首选”“导出最稳”“世界第一”等缺乏统一口径的排序。本文比较代表性领先方案及研究方向，不能据此推断全球市场份额或绝对质量排名。

代码基线为工作区在上述日期的状态，HEAD 为 `1326fb24901fc1c4bb955f9cb1271c618cea9cba`，其中已有未提交改动。代码存在表示具备实现基础，不表示发布安装包、当前用户配置和端到端体验均已验证。本报告不修改产品实现。

## 3 当前项目的真实基础与主要差距

### 3.1 已经具备的能力

当前项目比“通过提示词调用四个文件生成器”更完整。已有结构化 Spec、原生表格和图表、图片处理、局部修改、几何越界检查、LibreOffice 渲染、目录与公式更新，以及 Office Artifact 交付与来源相关模块。2026 年 9 月 7 日的原 PRD 把其中一部分列为待建设，但不能再直接照搬为今天的缺失清单。

| 能力 | 当前代码证据 | 下一步重点 |
|---|---|---|
| 桌面架构与模型接入 | Go Core、WebView2、React/TypeScript；README 与依赖清单 | 复用现有执行、权限、模型和会话体系 |
| 结构化办公内容 | `officestudio.Spec` 含 slides、blocks、sheets、document | 扩展品牌、布局语义、内容角色与适配约束 |
| PPT 语义布局 | Studio 分支支持 11 个布局枚举，含原生 table | 从固定坐标扩展到内容驱动的组合与变体 |
| 原生可编辑对象 | PPT 图片、DrawingML 表格、原生图表及内嵌工作簿 | 增加可编辑对象覆盖与目标软件回归 |
| Excel 生成与样式 | Excelize、类型化单元格、表头、列宽、打印设置、图表 | 增加输入输出语义、经营看板、统一数值与图表样式 |
| Word 结构 | 标题、三级层级、表格、目录、分节、页码等 | 完善正式文档样式体系、题注引用与跨页规则 |
| 实际排版与计算 | `officerender`、`RenderWithChecks`、原生检查回执 | 检查渲染组件的实际可用率，增加视觉问题诊断 |
| 局部修改和版本 | Node ID、Digest、Patch、版本与来源模块 | 将品牌与排版修改纳入现有局部更新和证据链 |

本地证据索引见第 22 节。上述结论来自代码阅读；本次没有重新运行产品生成、渲染或兼容性测试。

### 3.2 为什么生成结果仍可能不好看

**第一，设计规则被写死在生成函数中。** `pptx_design.go` 使用固定海军蓝、青绿、金色和灰白配色，固定标题栏、侧边装饰与页脚；字体运行固定指定 Calibri 和 Microsoft YaHei。这能保证基础一致性，却难以适配现代品牌、不同客户与不同信息密度。

**第二，内容模型仍偏重标题和条目。** Studio 已增加图片、图表和表格，但大量内容布局依然围绕 `Title / Subtitle / Bullets`。例如 comparison 与 two-column 共用按条目数量对半分配的逻辑；metrics 用 `|` 拆分数值与标签。它们能够绘制页面，却没有充分表达“比较对象、指标单位、差异结论、证据与备注”的业务关系。

**第三，有布局名称不代表拥有完整设计系统。** 同一个封面函数也用于 closing，多种正文布局共用大标题栏与固定内容区域。版式数量增加后，页面视觉仍可能高度相似，难以形成演示节奏。这是代码支持的设计局限推断，并非对用户实际生成文件的逐页审计。

**第四，几何检查覆盖的是对象边界。** `GeometryReport` 的注释明确说明没有根据 EMU 矩形推断真实文字溢出与字体度量。对象位于画布内，不代表文字能放下，也不代表图表可读或版面平衡。

**第五，真实渲染的存在尚未等于审美闭环。** 当前已有渲染回执，下一步应将字体替换、文字遮挡、表格断页、图片裁切、图表标签拥挤等结果映射回具体节点。审美低分应能触发有边界的修复，而不是只显示一张 PDF 预览。

**第六，技能资源已经不少。** 项目社区来源清单已有 frontend-design、Impeccable、Design Taste，并明确排除了受限的 Anthropic Office 源码。再安装相同技能的收益可能很小；应先检查这些设计约定是否进入 Office 生成流程、是否被结构化执行以及是否接受结果检验。

### 3.3 根因验证实验

在改动生成后端前，用同一份经营汇报材料做四组：当前输出；仅替换提示词；仅更换原创模板；模板加布局规划与渲染修复。固定模型、信息量、素材与目标页数，比较视觉盲评、关键内容保留和实际修改时间。

如果“只换提示词”进步有限，而“模板加规划”改善明显，则投资设计与布局。如果模板正确但最终 Office 文件发生位移，则投资渲染和兼容性。如果页面美观却没有决策价值，则应修复资料理解和内容叙事。这个实验比盲目更换模型或文件库更能定位瓶颈。

## 4 领先产品与研究方案对比

### 4.1 商业产品

| 方案 | 官方资料支持的能力 | 值得借鉴的产品机制 | 对 Lunitide 的边界 |
|---|---|---|---|
| Gamma | 卡片式创作、主题、PPTX/PDF 等导出、官方生成 API | 降低从空白到初稿的门槛，内容与视觉同时呈现 | 云端服务；API 需对应付费权益；Gamma 文档不等于 DOCX |
| Plus AI | PowerPoint/Google Slides 内创作；API 返回原生可编辑 PPTX，另有 PDF 和缩略图；官方 MCP | 直接满足后续编辑和原有办公流程 | 付费服务；不能把原生文件承诺当作所有复杂对象实测结论 |
| Beautiful.ai | Smart Slides、自动排版、品牌主题、共享模板、可编辑导出、官方 API | 用有边界的布局规则保证修改后仍一致 | API 文档存在早期开放及接口说明差异，需账号级验证 |
| Canva | 品牌模板、设计资源与 Connect Autofill API | 品牌素材库和模板运营，面向批量生产 | Autofill 等关键 API 要求企业组织权益；不是免费嵌入式设计引擎 |
| Microsoft Copilot | Word、Excel、PowerPoint Agents 按需求生成原生办公文件 | 跨文件业务资料、原生编辑、组织治理 | 属于 Microsoft 生态的产品能力，不是可直接内嵌的免费通用后端 |
| Google Gemini in Slides | 在 Slides 中生成及修改内容等辅助能力 | 在协作文档上下文中持续编辑 | Workspace/AI 权益与可用功能需按具体账号确认 |

资料依据：[Gamma 开发者文档](https://developers.gamma.app/)、[Gamma 导出说明](https://help.gamma.app/en/articles/8022861-what-s-the-easiest-way-to-export-my-gamma)、[Plus AI API](https://plusai.com/features/presentation-agent-api)、[Beautiful.ai 价格与功能](https://www.beautiful.ai/pricing-plans)、[Canva Autofill](https://www.canva.dev/docs/connect/autofill-guide/)、[Microsoft Agents](https://learn.microsoft.com/en-us/microsoft-365/copilot/wordexcelppt-agents)、[Google Slides 帮助](https://support.google.com/docs/answer/14355071?hl=en)。

Gamma 的关键启发是生成过程与视觉内容的紧密结合。其官方资料明确支持 PPTX，并已说明原生可编辑表格，不能笼统说“Gamma 只能导出截图”。同时，官方仍说明 DOCX 导出不受支持，部分视觉效果和字体在导出目标中可能不同。因此 Lunitide 对 Word 的方案必须独立设计。[Gamma 导出说明](https://help.gamma.app/en/articles/8022861-what-s-the-easiest-way-to-export-my-gamma)

Plus AI 的关键启发是把目标办公格式放到流程中心。其 API 明确支持生成、修改已有 PPTX 和按模板填充；这是原生可编辑交付的重要比较对象。“导出最稳”仍须由同一组测试文件验证，不能由功能说明直接推出。[Plus AI API](https://plusai.com/features/presentation-agent-api)

Beautiful.ai 的关键启发是布局约束和组织品牌控制。布局根据内容变化自动调整，有利于保持一致。Lunitide 应借鉴这种产品原则并自行实现，而不推测其内部算法或复制其专有模板。[Beautiful.ai 功能与计划](https://www.beautiful.ai/pricing-plans)

### 4.2 免费和 API 的实际情况

| 产品 | 已核实的免费或价格信息 | 集成判断 |
|---|---|---|
| Gamma | 免费计划有起始 credits；API 文档要求 Pro、Ultra、Team/Business 等权益 | 可做付费外部适配器，不能按免费 API 预算 |
| Plus AI | 7 天试用；公开年付折合 Basic 10、Pro 20、Team 30 美元/人/月 | 属于终端席位价格，不能直接用来计算平台 API 成本 |
| Beautiful.ai | 14 天试用；Pro 年付折合 12 美元/月，Team 年付折合 40 美元/人/月 | API 额度、租户隔离与代客生成权利需另行确认 |
| Canva | 品牌模板 Autofill 要求代表 Enterprise 组织成员操作 | 不适合作为免费核心依赖 |

价格为核验页面的美元标价，不含税费，不是合同报价。未公开核实的平台 API 单价、转售权利和 SLA 均不得从席位价格推导。依据：[Gamma API 权益](https://help.gamma.app/en/articles/11962420-does-gamma-have-an-api)、[Plus AI 定价](https://plusai.com/pricing)、[Beautiful.ai 定价](https://www.beautiful.ai/pricing-plans)、[Canva 开发文档](https://www.canva.dev/docs/connect/autofill-guide/)。

Beautiful.ai 帮助文章仍描述 early access，并使用 Bearer 示例；开发者导出接口页面展示 `X-Api-Key`。因此集成前必须用获准账号核验当前鉴权、接口和错误行为，不能直接把帮助文章示例当稳定生产合同。[帮助文章](https://support.beautiful.ai/hc/en-us/articles/43654071102605-Beautiful-ai-API)、[导出接口](https://docs.beautiful.ai/reference/exportpresentation-1)

### 4.3 开源完整方案与研究方向

**Presenton 是优先评估的开源 PPT 候选。** 官方仓库提供自部署、模板、PPTX/PDF 导出、模型供应商配置、API 与内置 MCP，顶层源码许可证为 Apache-2.0。它适合快速建立对照组，验证“模板与模型编排”能够达到的质量。其自身服务、第三方依赖、字体素材仍须分别核验，模型 API 与本地推理计算也不会因源码开源而免费。[仓库](https://github.com/presenton/presenton)、[许可证](https://raw.githubusercontent.com/presenton/presenton/main/LICENSE)

**PPTAgent 值得借鉴的是参考演示文稿驱动的生成与评估。** 论文将内容、设计和整体一致性纳入评估，采用分析参考页后执行编辑的方式。代码采用 MIT。建议把它作为实验路线和评测方法来源，不直接承担整个办公平台的生产主链。[论文](https://arxiv.org/abs/2501.03936)、[代码许可](https://raw.githubusercontent.com/icip-cas/PPTAgent/main/LICENSE)

**SLIDEFORGE 体现了近期结构化编辑研究方向。** 2026 年 9 月的论文把可指代组件、原生 PPTX 对象和渲染验证联系起来。这支持“对象结构与视觉组织同时保留”的架构选择。论文结果属于其自身实验，本报告不将其解释为已在 Lunitide 或中文商业文档上验证，也不将论文开放等同于代码和数据已具备商业再分发许可。[论文](https://arxiv.org/abs/2609.03109)

领先方向可以概括为：可理解的文档结构、优质参考与模板、受约束的编辑操作、真实渲染验证。需要把这些能力结合起来，而不是只生成一段更长的提示词。

## 5 可免费集成的能力清单与选择

### 5.1 必须分清的能力层次

| 层次 | 实际作用 | 不能替代的能力 |
|---|---|---|
| Skill | 规定步骤、审美约定、工具使用和验收方式 | 文件格式实现、真实排版、字体测量 |
| MCP | 让模型以统一接口调用工具和服务 | 工具内部质量、权限隔离、商业授权 |
| 插件 | 打包 skills、工具、依赖与配置 | 自动获得第三方 API 权益或模板版权 |
| 文件与排版库 | 确定性写出文档和图形对象 | 商业叙事、版式选择、审美判断 |
| 模板与设计系统 | 建立经过检验的视觉规则 | 数据真实性、复杂兼容性与运行恢复 |
| 视觉模型 | 检查图片中的布局并提出修复建议 | 精确公式运算、对象级事实一致性验证 |

在 Codex 中安装 Gamma、Canva 或其他插件，只会增加该环境的连接能力。把能力融入 Lunitide，还需要自己的连接适配器、账号授权、任务生命周期、结果接收、版本记录和服务条款。插件安装不能自动变成产品后端。

### 5.2 推荐组件矩阵

以下“适合商用”指具备相应开源许可路径，仍需遵守许可证并审核锁定版本及其依赖，不是对任意打包方式的无条件授权。

| 组件 | 类型与许可 | 推荐用途 | 主要边界 | 优先级 |
|---|---|---|---|---|
| 现有 Go OOXML 内核 | 项目自有实现 | 原生生成、已有文件保守修改 | 需扩展模板和语义样式 | 必选 |
| Excelize | Go 库，BSD-3-Clause | XLSX 生成、样式、图表、数据处理 | 不自动等同于 Excel 全功能计算与渲染 | 必选，已使用 |
| Impeccable | 设计 skill，Apache-2.0 | 字体、层次、颜色、审美检查流程 | 面向界面设计的规则需适配办公文档 | 优先复用 |
| frontend-design | skill，其目录为 Apache-2.0 | 模板创作和网页阅读版设计参考 | 不能自动保证 PPTX 或 Word 排版 | 按任务启用 |
| Presenton | 完整应用/API/MCP，Apache-2.0 核心 | PPT 对照实验、可选外部生成适配 | 需要额外运行环境与完整依赖审查 | P0 评估，P1 决策 |
| PptxGenJS | JS 库，MIT | 新建 PPT 的图形、母版和图表后端候选 | 不是通用 PPTX 导入后无损编辑器，也非任意 HTML 转换器 | P0 原型 |
| docx | JS/TS 库，MIT | 若现有 Word 生成维护成本过高，作为替代后端 | 生成与排版渲染分离；不要同时维护两套默认后端 | P1 比选 |
| Docxtemplater 核心 | MIT 或 GPLv3 双许可，可选择 MIT | 基于授权 DOCX 模板填充与循环 | 图像、HTML 等扩展有付费模块；自身不渲染 PDF | 特定客户模板 |
| LibreOffice | MPL-2.0 及发行包其他许可 | Office 实际渲染、域更新与计算 | 包体与部署；不代表 Office/WPS 全兼容 | 必选，已有接口 |
| Typst 编译器 | Apache-2.0 | 独立 PDF 的出版排版、长文档 | 不是 Word 原生编辑后端；模板许可另查 | P1 |
| WeasyPrint | BSD-3-Clause | HTML/CSS 到长篇 PDF 的候选 | CSS、字体依赖和 Windows 打包需验证 | PDF 备选 |
| Docling | MIT 代码 | 较复杂的 PDF/Office 内容解析 | 模型与依赖许可分开；解析结果不是设计成品 | P1 可选 |
| Playwright MCP | Apache-2.0 | 浏览器预览和交互验收辅助 | 浏览器截图不能证明 Office 文件兼容 | 开发与 QA |
| Lucide | ISC | 自建模板的统一线性图标 | 不适合把每段正文都装饰成图标卡片 | 已有，可复用 |
| 开放字体 | 按具体字体 OFL/其他开放许可 | 统一预览与导出字形 | 中文字体体积、子集、改名与嵌入规则需核验 | 必选 |

许可与能力依据见第 21 节来源 11—28、33—34。其中 Excelize、docx、Docxtemplater、WeasyPrint 的许可证均核验了上游文件，不能把某个工具包的顶层许可推及全部扩展。

**PptxGenJS 的位置需要克制。** 它能加速新模板开发，但目前项目已经有原生图表、图片、版本与补丁基础。先用同一份 LayoutPlan 实现一个候选后端，比较开发效率、包体、中文质量和对象保留；达到预设收益后才接入生产。已有导入文件继续走原有保守 Patch 通道。[PptxGenJS](https://github.com/gitbrent/PptxGenJS)

### 5.3 具体 MCP 候选

| MCP | 可核实的作用 | 对本项目的价值 | 建议 |
|---|---|---|---|
| Presenton 内置 MCP | 调用其演示文稿生成能力 | 快速搭建独立对照组 | 值得试验，生产接入仍统一结果合同 |
| GongRzhe Office-PowerPoint-MCP-Server | 基于 python-pptx 操作幻灯片、对象和模板；MIT | 补充实验工具、理解社区 API 设计 | 与原生能力重复较多，不作为默认主链 |
| GongRzhe Office-Word-MCP-Server | Word 文件操作；MIT | 特定 Word 自动化能力补充 | 缺口明确时再接入 |
| haris-musa excel-mcp-server | Excel 读写等自动化；MIT | 外部工具互通和原型验证 | 不替换已有 Excelize 主链 |
| Microsoft Playwright MCP | 浏览器自动化与可观察结果 | 检查模板预览与 Studio 交互 | 放在开发/受控 QA 环境 |
| Plus AI 官方 MCP | 通过其服务生成或继续处理演示文稿 | 可选付费生成通道 | MCP 本身不能把付费服务变为免费 |

来源：[PPT MCP](https://github.com/GongRzhe/Office-PowerPoint-MCP-Server)、[Word MCP](https://github.com/GongRzhe/Office-Word-MCP-Server)、[Excel MCP](https://github.com/haris-musa/excel-mcp-server)、[Playwright MCP](https://github.com/microsoft/playwright-mcp)、[Plus AI MCP](https://plusai.com/features/mcp)。社区 Office MCP 不属于 Microsoft 官方产品，不能以名称推断官方支持。

Lunitide 内部核心生成建议直接调用受控模块或固定版本工作进程。MCP 留给外部能力互通，避免一页幻灯片几十次细粒度远程调用带来的成本、延迟与故障放大。对外可提供自己的 `office.create`、`office.inspect`、`office.patch`、`office.render` 等聚合接口，但这些是拟议接口，不是已安装工具。

### 5.4 不能当作免费闭源内核的方案

**Anthropic 的 docx、pptx、xlsx、pdf skills：** 官方明确区分源码可见与开源；当前目录许可含复制、派生和第三方分发限制。不能复制入 Lunitide，也不应将其稍作改写后当成自有商业实现。可采用独立提出的通用工程方法，并基于获许可组件编写原创流程。项目已有来源清单遵守了这一点。[官方说明](https://github.com/anthropics/skills)、[PPTX skill 许可](https://raw.githubusercontent.com/anthropics/skills/main/skills/pptx/LICENSE.txt)

**ONLYOFFICE Community、HyperFormula、PyMuPDF：** 分别涉及 AGPL、GPL 或对应商业授权路径。它们并非“禁止商用”，但闭源嵌入和分发义务必须按具体集成方式审查。通过独立进程或 HTTP 调用不能自动得出免除义务的结论。需要完整在线编辑器时，可评估 ONLYOFFICE Developer 商业版，而不是把 Community 版当成免费白标组件。[ONLYOFFICE](https://helpcenter.onlyoffice.com/docs/faq/docs-community.aspx)、[HyperFormula](https://hyperformula.handsontable.com/docs/guide/license-key.html)、[PyMuPDF](https://pymupdf.readthedocs.io/en/latest/about.html)

**Univer：** 开源 SDK 使用 Apache-2.0，但官方区分 OSS 与 Pro。导入导出文档明确需要转换后端，并展示 `@univerjs-pro` 的 exchange 包；不能把前端网格能运行理解为已免费获得全部 XLSX 往返能力。图表、打印、协作等也需核验对应包和部署方式。它适合未来的在线表格编辑评估。[Univer 仓库](https://github.com/dream-num/univer)、[导入导出文档](https://docs.univer.ai/guides/sheets/features/import-export)

### 5.5 建议内建的原创技能组合

新增技能应围绕 Office 任务的输入输出合同，而不是把多个社区提示词全部塞进系统提示。下表为拟议的 Lunitide 原创技能，不代表当前已经安装或实现。

| 技能职责 | 输入 | 必须产出的结构 | 触发时机 |
|---|---|---|---|
| 资料与叙事整理 | Brief、附件、FactSet | 逐页/节目的、结论、证据与缺口 | 新建或改变受众时 |
| 品牌与设计决策 | BrandProfile、任务类型 | 模板候选、风格约束、字体与素材策略 | 首次选风格或用户换品牌时 |
| 演示文稿编排 | NarrativePlan、布局清单 | 合法的 SlidePlan，不直接拼任意坐标代码 | PPT 新建或重排时 |
| 正式文档编辑 | 段落、引文、文类规则 | DocumentPlan、样式与分页要求 | Word/长报告任务 |
| 经营表格建模 | 原始表、指标口径 | WorkbookPlan、公式来源、显示规则 | Excel 与跨文件数据任务 |
| 视觉与交付复核 | 渲染图、对象树、事实锁 | 定位明确的问题与允许的修复动作 | 实际文件生成后 |

每个技能只加载当前格式、品牌与模板的必要规则，并输出可校验结构。工具层负责文件操作，质量门槛由程序执行；模型不能通过文字声明绕过检查。Impeccable 等许可明确的设计指导可在保留许可与来源后适配，用于模板创作和复核。通用开发流程技能不应全部进入最终用户的每次文档任务。

## 6 三条架构路线的取舍

| 维度 | A 完全调用商业 API | B 直接以开源应用为内核 | C 现有内核加设计系统 |
|---|---|---|---|
| 初期速度 | 接口获准后较快 | 单一 PPT 场景较快 | 模板和规划建设需要投入 |
| 四种格式统一 | 需要多供应商拼接 | 多数方案偏 PPT | 可复用现有四格式结构 |
| 本地优先 | 通常不满足 | 取决于模型与资源配置 | 最符合现有架构 |
| 编辑与版本 | 受服务 API 限制 | 需要适配其数据结构 | 沿用现有 Artifact/Patch |
| 长期成本控制 | 受单价、额度和服务变化影响 | 运维、模型及分支维护成本 | 设计资产与引擎维护成本 |
| 品牌差异化 | 受供应商开放程度影响 | 取决于模板系统能力 | 可形成自有模板与品牌规则 |
| 推荐位置 | 可选付费服务 | 对照组和局部能力候选 | 主路线 |

选择 C，吸收 B 的可复用方法，保留 A 的适配接口。第一阶段无需重写 Runtime，也不需要把 Windows 桌面产品改成大型 Docker 平台。Presenton 或 Node 工作进程只通过现有任务与权限边界接入，不能另建会话、密钥或版本数据库。

## 7 产品定位与首发场景

### 7.1 目标用户

首发用户是经常需要对外或对管理层交付材料的经营、产品、销售与咨询人员。他们的痛点是内容散乱、公司模板难用、修改后数字不一致，以及最后仍需花大量时间整理格式。

首发三个场景：经营月报和季度复盘；销售客户方案；研究与项目正式报告。中长期再扩展教育课件、活动提案和复杂财务模型。政务公文、法律合同、审计报告等特殊规范文档应另建模板与验收，不套用营销式设计。

### 7.2 典型任务

“根据这份销售表和项目总结，做一份给管理层看的 12 页汇报，沿用公司品牌，并给我对应的 Word 报告和 PDF。”

系统先识别受众、交付格式、资料来源、目标长度、品牌和保密范围；对可合理推断的项目使用默认值。遇到数字口径冲突、来源缺失或额外上传外部服务的授权缺口时才要求补充。风格选择应可跳过，不能强迫每次先填长表单。

### 7.3 用户能理解的质量承诺

用户看到的是“内容完整”“排版已检查”“关键数字有来源”“可继续编辑”“存在需处理的问题”。默认界面不显示 MCP、EMU、OOXML 或模型推理细节。需要排查时再展开技术证据。

“可以交付”的判定必须与格式和用途关联。PPTX 需要检查可编辑对象；XLSX 需要检查公式和数据口径；PDF 需要检查字体、分页和可搜索性。不存在一个颜色为绿色的通用检查结果，就能证明所有格式全部正确。

## 8 产品功能需求与优先级

| 编号 | 需求 | 优先级 | 验收要点 |
|---|---|---|---|
| FR01 | 从一句需求与附件建立任务 Brief | P0 | 缺失普通偏好使用默认；关键数据冲突不能静默处理 |
| FR02 | 自动选择或使用既有品牌 | P0 | 应用到同一任务的全部格式；禁止混用旧品牌 |
| FR03 | 内容整理与叙事规划 | P0 | 每页/节有目的、结论、证据和来源关联 |
| FR04 | 原创模板与语义布局库 | P0 | 首发 3 套设计体系、12 种语义布局，共 36 个已检查变体 |
| FR05 | 按内容自动选版与适配 | P0 | 以文本测量、数据规模、图片比例选择；不能截断正文 |
| FR06 | 原生可编辑 Office 文件 | P0 | 文本、表格和常规数据图表按支持矩阵可编辑 |
| FR07 | 实际文件渲染与视觉诊断 | P0 | 证据绑定文件摘要；缺组件时不能标为已验证 |
| FR08 | 有界自动修复 | P0 | 默认最多 2 轮；保持事实、锁定元素和非目标内容 |
| FR09 | 局部修改与版本恢复 | P0 | 修改单页/节/范围，保留可回退版本与差异 |
| FR10 | Word 正式报告样式 | P0 | 标题层级、段落、表格、目录和分页规则一致 |
| FR11 | Excel 专业经营工作簿 | P0 | 区分数据、计算、说明、看板；计算可追溯 |
| FR12 | Office 同源 PDF 导出 | P0 | PDF 指向对应 Office 版本；重新编辑后失效并重建 |
| FR13 | 跨文件关键指标绑定 | P0 小范围 | 指标变化能找到引用位置，按用户要求更新 |
| FR14 | 品牌模板导入与有限编辑 | P1 | 明确支持对象；复杂导入保留原对象并报告限制 |
| FR15 | 独立高品质 PDF 排版 | P1 | 支持长报告、封面、目录、引用和稳定分页 |
| FR16 | 外部生成器适配与质量比较 | P1 | 外部结果进入同一检查和版本链，不绕过交付门槛 |
| FR17 | 企业共享模板、审批与权限 | P2 | 模板发布/停用、组织权限、使用记录完整 |
| FR18 | 在线自由编辑与实时协同 | P2 | 单独评估编辑 SDK、许可和格式往返；不挤占首发 |

P0 的四种格式应以少量受控文档类型上线。36 个 PPT 变体不意味着 Word 和 Excel 也要各做 36 套；Word 先做 3 类正式报告，Excel 先做 3 类经营工作簿。

## 9 端到端生成与修改体验

### 9.1 创建

输入需求和材料后，立即显示简洁的任务概要与可编辑大纲。用户可以选择“直接生成”，也可以先看封面、正文和图表三个代表性预览来选风格。预览内容应来自当前任务，不能用完全无关的精美示例制造预期。

生成按内容整理、页面设计、文件生成、排版检查四个可理解的阶段展示进度。允许边完成边预览。只有原生文件生成后渲染得到的预览，才能显示为交付预览；更快的 HTML 概念预览应有明确区别。

### 9.2 修改

用户可以说“第 4 页更简洁，但保留三个指标”“改成公司模板”“把最新销售额同步到报告”。系统定位节点或指标，给出修改范围，执行局部重排，更新受影响的预览和文件版本。

更简洁应优先通过重新组织、分组、换版式或拆页实现；不能默默删除有信息价值的内容。移动到附录或备注时，应保留关联并展示变更。已确认事实和合同类固定文本不参与自由改写。

### 9.3 交付

交付区显示文件、版本、检查范围、编辑能力与来源。将“下载草稿”和“作为正式交付”区分开：低风险的未完全检查版本可以明确标记为草稿，正式交付必须通过适用的硬门槛。不能把所有视觉建议都当阻断项，也不能把公式错误当普通美观建议。

## 10 文档设计规范

### 10.1 审美原则

高品质首先来自清楚的层级、充足的留白、准确的对齐、合适的信息密度和有意义的图像。金色、深色、大图、圆角并不天然代表高档。不同业务场景应有不同风格：经营汇报重视数字和对比；销售方案重视客户问题和证据；正式报告重视结构和长时间阅读。

首发三套原创设计体系建议为：清晰克制的白底经营汇报；深浅页面交替的品牌方案；适合长文和图表的编辑式报告。每套都应具备相同的内容覆盖能力，使切换风格不丢数据。模板必须经过设计人员实际调整，不能全部依赖模型临时生成。

### 10.2 品牌规则

BrandProfile 至少包含主色、辅助色、背景、文本色、字体族、字号阶梯、图表序列色、Logo 安全区、图片处理、标题规则和禁止项。颜色按用途命名，例如强调数据、风险提示、分组背景，而不只保存一组十六进制值。

需要同时保存预览字体、Office 字体与嵌入策略。建议优先选择经过许可核验的 Noto/思源等开放字体，并为英文、数字、中文和特殊符号建立明确回退链。网页中已经引入字体，不代表 DOCX/PPTX 接收方安装了字体；Microsoft YaHei 等系统字体也不能仅因本机可用就打包再分发。[Google Fonts](https://developers.google.com/fonts)

### 10.3 建议的初始版式参数

| 类型 | 建议起点 | 约束说明 |
|---|---|---|
| PPT 画布 | 16:9，13.333 × 7.5 英寸 | 先把这一比例做好，再扩展其他尺寸 |
| PPT 标题 | 通常 28—36 pt；封面 36—48 pt | 中文长度与布局不同，可使用经测量的变体 |
| PPT 正文 | 通常 18—24 pt | 不以无限缩小字体解决装不下的问题 |
| PPT 注释 | 通常 10—12 pt | 仅用于来源和非核心说明；关键内容不能藏在注释 |
| Word 正文 | 通常 10.5—12 pt，适度行距 | 按正式报告、提案、公文等文类分别设定 |
| Word 页面 | A4、明确页边距与段间距 | 段落样式优先，不用空行和空格模拟排版 |
| Excel 数值 | 单位、千分位、百分比与小数位一致 | 原始精度独立保存；显示四舍五入不得改原值 |
| 图表 | 一个清晰问题对应一种图表 | 排名用条形、趋势用折线；禁止仅为炫技选复杂图 |

这些是待实测调优的产品默认值，不是所有商业文档的强制标准。模板测试应包含中文长标题、中英混排、极端数值、窄列和缺少图片的情况。

## 11 系统架构与内容合同

### 11.1 核心流程

```text
需求和附件
  → Brief 与来源解析
  → FactSet 事实和指标
  → NarrativePlan 页节目的与证据
  → BrandProfile 和 TemplateCatalog
  → LayoutPlan 内容适配与几何布局
  → 各格式原生生成后端
  → 实际渲染和内容计算检查
  → 视觉诊断与有界修复
  → Artifact 版本与交付包
```

统一的是事实、品牌和任务合同，不是四种格式的全部对象树。PPT 是空间画布，Word 是可重排的文本流，Excel 是单元格和依赖图，PDF 是固定页面。应在共享语义层之下保留各自格式结构。

### 11.2 主要数据对象

| 对象 | 关键字段 | 作用 |
|---|---|---|
| Brief | audience、purpose、deliverables、language、targetLength、confidentiality | 明确生成目标和边界 |
| FactSet | factId、value、unit、period、sourceId、locator、confidence、status | 保持事实、口径和来源一致 |
| BrandProfile | brandId、version、colors、fonts、chartTheme、logoRules | 统一视觉规则 |
| NarrativePlan | nodeId、purpose、claim、evidenceRefs、density、speakerNotes | 让内容结构可判断和可修改 |
| TemplateManifest | templateId、version、license、supportedKinds、slots、constraints | 记录模板能力和使用权利 |
| LayoutPlan | variantId、nodes、boundingBoxes、readingOrder、fitEvidence | 形成可执行的布局结果 |
| RenderEvidence | artifactDigest、renderer、version、fontSetDigest、pageImages、checks | 证明检查对应哪个真实文件 |
| QualityReport | blockers、warnings、coverage、score、repairHistory | 区分结构、视觉、事实与兼容性状态 |

原有 Spec v1 保持可读。建议新增显式 v2，通过适配将旧输入映射到默认品牌和旧模板；旧 Artifact 的检查证据继续绑定原文件，不因升级自动变成新质量等级。

### 11.3 不可破坏的约束

每个事实、段落和图表有稳定 ID；已确认内容使用摘要或显式锁定。布局可以变，数字、单位、时间范围和引用不能随意改变。任何生成或修复都记录依赖的品牌版本、模板版本、模型版本和来源摘要。

导入的 Office 文件保留原始资源、关系和未知对象。有限 Patch 只修改受支持部分，不能把整个导入文件先抽成简化 Spec 再重建。对于新建受管文件，可以在自身支持范围内重新布局；两者不能混用。

## 12 智能布局与视觉修复

### 12.1 布局选择

先按业务语义筛选候选模板，例如“结论加趋势”“方案对比”“指标概览”“流程”“案例证据”。再根据文字测量、条目数量、图表类别、图片比例和品牌限制筛选容量；最后根据整份文稿的视觉节奏选择变体。

首版不需要复杂的机器学习排序。用明确规则缩小到 3 个候选，再由模型或人工选择即可。候选必须满足硬约束，不能选完漂亮模板后强行把文字塞进去。

### 12.2 超量内容的处理顺序

首先删除重复表达并保持事实；其次换成容量更合适的版式；再调整分组或拆页；必要时将展开论证移入附录或备注并保留引用。只有在字号仍满足阅读要求时才适度调整字号。不能通过截断、隐藏或压缩到极小字体取得“无越界”。

图片应使用 contain 或可解释的裁切区域，不能拉伸人物和产品。人物脸部、Logo 和图表坐标轴属于重要区域，必要时用视觉检测辅助保护。无合适图片时使用纯排版，胜过加入无关的通用图库图片。

### 12.3 检查与修复

确定性检查负责对象边界、颜色令牌、字体允许列表、数值与结构。真实渲染检查负责字体替换、文字实际溢出、对象遮挡和分页。视觉模型补充层级、平衡、图片相关性与整体节奏判断，其判断要有页码、节点和可执行建议。

默认最多执行 2 轮自动修复，优先处理确定性问题。每轮修复后重新生成受影响文件、渲染并检查事实不变量。若仍存在硬问题，保留最近可用版本并报告范围。不能形成“模型反复重做整份文件直到超时”的循环。

## 13 四种格式的专项需求

### 13.1 PPT

原生文本、表格、常规图表必须保留编辑能力。复杂图解可在产品支持范围内使用原生形状与连接线；无法完整表达的特效可以使用图像，但必须记录栅格化对象及原因。不能整页输出成图片后宣称“完全可编辑”。

首发 12 种语义布局为封面、章节、结论摘要、双栏论证、方案对比、指标概览、趋势图、结构图、流程、时间线、案例证据和结尾行动。每种布局具有容量范围和数据合同。现有原生 table 可作为多种布局的组件，不必与“页面类型”绑定死。

本地新增布局应能表达单独的标题、比较项、指标、单位和图注，不继续用带分隔符的普通字符串承载业务结构。编辑后检查母版、主题、图表源数据、备注和阅读顺序。支持矩阵明确 PowerPoint、WPS、LibreOffice 的实际验证版本。

### 13.2 Word

Word 采用原生标题和段落样式、编号、题注、交叉引用、表格标题行、分节与目录。正文重排后，分页和目录应随目标渲染器更新。标题不能单独留在页尾，表格跨页需重复表头，长单元格内容不能被固定行高裁掉。

首发模板覆盖正式研究报告、客户方案、产品/项目文档。品牌体现在文字层次、表格、封面和页边距，而不是让每页都变成 PPT 风格的卡片。需要严格原文保留的内容在输入时标注，模型只能调整外部格式。

DOCX 的目录域刷新与原文件缓存更新属于不同证据；继续沿用现有区分，不以 PDF 中目录正确推断下载 DOCX 中缓存一定正确。若选择 docx 或 Docxtemplater，新后端必须接入同一目录和分页验收。

### 13.3 Excel

Excel 的高级感主要来自可理解、可核算和可维护。建议工作簿由说明、原始数据、计算分析、经营看板组成；小任务不强制四张表，但必须区分输入数据和推导结果。

输入、公式、关键输出采用稳定且节制的样式规则。表头冻结、筛选、打印区域、单位、负数、缺失值和日期格式需一致。看板隐藏网格线并保留足够对齐；明细表应便于筛选，不因视觉设计滥用合并单元格。

公式由可信结构生成或按白名单验证；数据导入时的 `=` 等前缀先按输入类型处理，不能把外部文本自动执行为公式。重算后检查错误单元格与覆盖范围，保留“使用哪个计算引擎”的证据。

基于同一 FactSet 生成图表和汇报数字。数值缺失、异常和四舍五入规则应显示出来。首发不承诺宏、外部工作簿链接、Power Query、复杂数据模型和全部动态数组的无损往返。

### 13.4 PDF

PDF 分为两个产品入口。**同源 PDF** 从最终 PPTX、DOCX 或 XLSX 版本渲染，适合“可编辑源文件加正式阅读稿”。**独立 PDF** 从语义报告模型通过 Typst 或其他经过验证的排版器生成，适合长篇研究、手册和对外资料。

同源 PDF 需要追求与对应 Office 文件的视觉一致；独立 PDF 可以使用更适合出版阅读的排版，但应明确它与 Word 使用同一内容版本、并不保证分页完全相同。不能从“都是同一份内容”推出“像素完全一致”。

默认文本可搜索、中文可复制、字体无缺字、目录和链接可用。PDF/A、PDF/UA、印刷色彩与出血属于明确需求时的专项能力，需要适用标准和验证器检查，不因成功导出 PDF 就宣称合规。

## 14 模板与品牌资产的生产机制

模板应作为需要维护的产品资产，包含内容槽位、设计令牌、容量测试、适用语言、支持后端、授权信息和版本。业务模型只能填写允许的槽位，不能自行运行模板内任意代码。

模板发布流程建议为：设计人员制作原型；工程实现语义布局；生成标准与极端内容样本；检查真实 Office 渲染；确认素材授权；进入小规模试用；发布带版本的模板。修改模板不应悄悄改变已交付文档。

品牌导入分两级。第一级抽取颜色、字体、Logo 和基本样式，形成 BrandProfile；第二级保留并映射客户授权模板中的母版、布局和占位符。第一阶段不承诺从任意漂亮 PPT 自动还原成完整可复用模板。

模板质量需要持续运营。记录首稿接受率、改版频率和常见溢出位置；把高失败模板下线或修订。评价模板不能只看封面缩略图，应同时查看正文、图表、长表格与极端内容页。

## 15 质量指标与正式交付门槛

### 15.1 分开计算硬问题与视觉质量

**硬问题**包括打不开、正文或关键数据丢失、锁定内容被改、重要文字被遮挡、公式错误、来源口径冲突、未授权资源，以及当前要求检查但没有完成的必要项目。硬问题不能由高视觉评分抵消。

视觉评分建议满分 100：阅读层次 20，排版与留白 20，字体与中英混排 15，图表与素材 15，品牌一致性 15，整体节奏与内容适配 15。评分由规则、视觉模型和抽样人工评审共同形成，保留覆盖信息。达到 85 分可作为内部试行门槛，但必须先通过标注集校准，不应直接展示为客观行业认证。

### 15.2 建议的阶段验收指标

| 指标 | 可执行定义 | 初始目标 |
|---|---|---|
| 结构可打开率 | 固定测试集在声明目标软件中成功打开，不出现修复提示 | ≥99%；阻断样本全部修复 |
| 关键事实保留率 | 输入已确认 factId 的值、单位、期间及引用一致 | 100% |
| 可编辑覆盖率 | 支持范围内应可编辑的语义对象中通过实际编辑测试的比例 | ≥95%；核心数字和正文 100% |
| 严重视觉缺陷 | 重要文字截断、遮挡、缺字、关键图例不可读 | 正式交付中为 0 |
| 品牌合规 | 应使用品牌令牌的对象符合已确认版本 | ≥98%；Logo 和禁用项 100% |
| 修改保留率 | 非目标逻辑节点的语义与必要样式保持 | ≥99%；锁定节点 100% |
| 首稿进入局部修改比例 | 首稿未被要求全量重做即被接受或局部修改 | 灰度建立基线后，目标 ≥70% |
| 用户整理耗时 | 从首稿到用户可交付版本的中位人工分钟数 | 相对当前版本下降 ≥50% |
| 交付恢复率 | 渲染/网络中断后复用有效步骤并完成任务 | 目标 ≥95%，需故障测试 |

“可编辑覆盖率”的分母只包含应该原生编辑的对象，照片等自然栅格素材不计入；与此同时单独统计意外栅格化，防止靠把对象变成图片提高成功率。XML 存在只是初步证据，还要测试修改文本、表格和图表数据后保存重开。

## 16 与竞品比较的实测方案

本报告提供测试设计，不提供虚构跑分。第一轮用 12 份经授权材料覆盖经营汇报、销售提案、研究报告和中文长文本；PPT 对照当前 Lunitide、改进版本、Presenton，以及能够合法使用的商业竞品。Word 和 Excel 则选择相应原生办公能力对照，不把不支持该格式的产品计为生成失败。

每个系统获得相同事实材料、受众、语言、页数或长度约束和可用素材。不支持客户模板的产品另分组，不能与具备原模板导入的产品混成一个结论。记录产品计划、版本、日期、耗时、调用成本及人工操作。

分别比较首次输出与限定 10 分钟修改后的输出；复杂 Excel 另设合理操作时限。所有计划内尝试均计入成功率与成本，不能只选最好的一份。先隐藏品牌来源做视觉盲评，再提供原文件进行编辑和事实检查。

至少由 3 名具有相关工作经验的评审独立评估，报告分歧。用同一任务的成对偏好比例与置信区间表达结果，按业务场景分层；不凭一个总体平均分宣称全面领先。重复生成时记录随机性，避免单样本代表系统水平。

正式回归集建议扩充到 100 个任务：40 个 PPT、25 个 Word、25 个 Excel、10 个独立 PDF；Office 到 PDF 的同源导出附在前三类任务中检查，避免重复计算独立任务数。覆盖中文、英文、中英混排、密集表格、长文、图片、缺字体、离线和异常输入。

## 17 商用许可与资源管理需求

软件、模型、字体、图片、图标、模板分别记录来源与许可。组件采用 MIT/Apache/BSD 只是起点，发行前仍需扫描传递依赖与二进制包。尤其不能用某个开源服务外壳的许可证覆盖其中的 PDF 引擎或商业扩展。

推荐建立 AssetRecord：来源 URL、作者、获取时间、许可版本、许可证据、是否允许商用、是否允许再分发、是否需要署名、使用范围、文件摘要。人工原创模板与客户上传模板也应有权利来源说明。

Pexels 和 Unsplash 提供较宽松的图片使用许可，但这不自动解决肖像、商标、敏感用途或素材再分发问题。图库 API 的接入和展示条件还要单独满足。首发可以优先使用客户授权图片和自制图形，减少对在线图库的强依赖。[Pexels](https://www.pexels.com/license/)、[Unsplash](https://unsplash.com/license)

生成图片只能用于表达和装饰，不能冒充真实客户现场、产品实拍或研究证据。图片模型和权重的商业条件需要按具体型号核验；“本地模型”不等于模型许可无条件开放。对外服务处理客户材料时，沿用项目已有授权边界并明确数据去向。

发行包保留组件许可证、NOTICE、必要署名与修改说明。客户模板在其授权工作区内使用，不自动汇入公共模板市场。商用上线前应就最终分发方式完成许可复核，而不是依据本报告的一行分类替代具体审核。

## 18 性能 成本与部署要求

### 18.1 性能目标

在固定的参考机器、网络和模型配置上测量，建议记录 30 次以上任务的 P50/P95。参考场景为 12 页 PPT、20 页报告、5 张 Sheet 且不超过现有 50,000 单元格限制的工作簿。首发限额继续与当前文件、页面、对象限制兼容，扩容单独评估。

产品目标可先设为：大纲通常在 30 秒内出现，12 页 PPT 首稿 P50 不超过 120 秒，含一次修复的交付 P50 不超过 180 秒；P95 和失败率在第一轮基线完成后冻结。离线模型、AI 生图和大附件独立分层，不对所有模式承诺同一耗时。

优先缓存品牌、模板、字体度量、资料解析和未变更页面；编辑一页时只重算受影响布局。模型调用按内容规划、必要视觉检查分配，不对每个形状发起一次调用。没有支持图片输入的模型时，保留规则与真实渲染检查，并说明视觉审查范围，不能伪装已完成视觉模型检查。

### 18.2 成本模型

单次尝试成本应包括输入与输出 token、视觉模型、图片生成、渲染算力、存储和第三方 API。**每份被接受交付物的成本 = 同批次全部尝试及修复成本 ÷ 被接受的交付物数量。** 这个口径比只统计成功任务的第一次调用更适合商业决策。

以下仅为算式演示，不是当前模型供应商报价：假设输入 12,000 tokens、输出 6,000 tokens，输入每百万 5 元、输出每百万 20 元，文本成本为 0.18 元；假设 4 张图片每张 0.10 元、渲染与存储 0.05 元，则首次成本为 0.63 元。平均增加 20% 修复成本后为 0.756 元；若只有 80% 的尝试最终形成被接受的交付物，则约为 0.945 元/份。实际定价必须替换成供应商账单及产品数据。

“不使用付费生成 API”可以做到，但模型本地运行需要硬件与电力，模板需要设计维护，渲染需要资源。建议产品提供本地/BYOK 基础模式与可选托管增强模式，不以无限免费生图吸引无法控制成本的使用量。

### 18.3 部署与运行边界

Windows 首发继续使用现有 Go 管理受控工作进程。LibreOffice 作为排版组件处理实际安装、版本、字体和可用性；不能只写“已支持 LibreOffice”而忽略用户机器没有组件的情况。采用按需能力包或明确检测流程，并保持降级状态可见。

若采用 Node、Python 或 Typst 后端，固定运行时与依赖摘要，通过现有任务系统执行，输入输出使用任务拥有的目录。禁止加载用户模板中不受控的代码、宏或外部资源。不要依赖工程师电脑里的全局环境作为可销售产品的运行条件。

云端规模化转换可评估 Gotenberg，其文档列出 Chromium 与 LibreOffice 转换路线。但容器化服务不是 Windows 首发的必要条件，且发行镜像中的所有组件许可需单独核验。[Gotenberg](https://gotenberg.dev/docs/getting-started/installation)

## 19 实施路线与资源配置

以下为范围可控情况下的规划估算，假设 1 名产品/项目负责人、1 名有商业文档经验的设计师、2 名核心研发、1 名前端或测试兼任人员。可并行工作不代表所有功能一次上线。单人实施应减少首发模板和场景，不能照搬团队工期。

| 阶段 | 时间估算 | 交付成果 | 进入下一阶段的条件 |
|---|---|---|---|
| 0 基线与设计实验 | 第 1—2 周 | 12 份材料、当前输出基线、3 套视觉方向、组件许可清单 | 根因定位；确认主路线；确定模板内容合同 |
| 1 PPT 质量闭环 | 第 3—5 周 | BrandProfile、12 种布局的首批变体、渲染诊断、局部修复 | 硬问题清零；对照当前版本有显著实际改善 |
| 2 Word 与 Excel | 第 6—8 周 | 各 3 类模板、同源 PDF、关键指标绑定 | 正式报告和经营工作簿通过独立验收 |
| 3 商业试点 | 第 9—10 周 | 5—10 个授权试点客户、导出兼容性结果、成本与反馈 | 接受率和人工整理时间达到阶段目标 |
| 4 上线完善 | 第 11—12 周 | 36 个 PPT 变体完成、安装能力包、回退与发布清单 | 质量、许可、恢复和支持条件全部满足 |

第一轮预算应优先保证设计师参与和测试样本质量。研发增加 API 数量无法弥补模板质量不足。若第 2 周发现 Go 扩展成本明显高于候选后端，可在不改变上层合同的情况下调整渲染后端；不能因此推翻已稳定的版本、来源和 Patch 基础。

### 19.1 首批可执行研发工作项

| 工作项 | 涉及现有模块 | 实现范围 | 核心验证 |
|---|---|---|---|
| 设计令牌外置 | officetools、officestudio | 去掉新模板对固定配色和字体的依赖；旧模板兼容 | 同一内容切换品牌后数据不变 |
| Spec v2 语义结构 | officestudio/types.go 与转换层 | comparison、metric、evidence 等独立字段 | 旧版本读取；禁止静默字段丢失 |
| 模板注册与版本 | skill/资源管理与 Office 领域层 | 模板清单、摘要、许可与能力矩阵 | 版本可追溯；不执行未知代码 |
| 布局规划器 | Office 生成前步骤 | 容量约束、文字测量、候选选择 | 长中文、单位、极端内容不截断 |
| 视觉诊断 | officerender、officeapp | 页面证据到节点问题，区分已检查和未知 | 真实重叠/缺字样本能被定位 |
| 局部修复 | officestudio/patch 与版本服务 | 换版、调整位置、拆页，保持锁定内容 | 非目标节点和事实不变 |
| Studio 风格预览 | web/src/officeStudio | 任务真实样例、品牌应用、质量解释 | 用户不用理解内部工具即可完成 |
| 跨文件同步 | officeapp/facts 与 bundle | 小范围已确认指标在三格式内引用 | 来源更新、冲突和撤销均可解释 |

这是一组按依赖推进的产品工作项。开始具体实施时再拆成小规模变更与测试，不建议一次修改全部生成路径。

## 20 风险控制与发布决策

| 风险 | 早期信号 | 处理原则 |
|---|---|---|
| 浏览器预览漂亮而导出失真 | 字体、表格、图表在 Office 中位移 | 以原生文件渲染为交付预览；声明支持范围 |
| 自动修复损伤内容 | 字数突然下降、指标不一致 | 事实锁与内容差异检查；有限重排；保留前版本 |
| 模板库徒有数量 | 多页相似、正文承载能力差 | 按业务任务和极端样本评估，不按封面数量评估 |
| 引入第二套复杂平台 | 多套账号、状态、文件与密钥 | 外部后端只做适配器，统一任务与 Artifact |
| 免费组件产生许可成本 | 依赖含商业模块或强 copyleft | 锁定版本逐项核验；提供可替换后端 |
| 用户机器无法排版 | LibreOffice/字体/运行时缺失 | 能力检测、安装包策略、明确降级和恢复 |
| 模型审美评分不可信 | 高分稿仍被用户全量重做 | 用盲评和实际修改时间校准，评分不单独决策 |
| 项目范围失控 | 同时追求完整在线编辑与四格式全兼容 | 先限定场景和对象；复杂编辑单独立项 |

上线采用模板与后端级功能开关。旧生成路径保留为兼容回退，但回退结果必须呈现真实的质量范围。新版本生成失败不覆盖旧文件；升级后不追溯改动已被接受的历史 Artifact。

**最终建议：先用两周验证“原创模板加结构化布局”的质量提升，并确定真实导出质量；随后围绕现有内核完成高品质闭环。** 优先选择可维护、可验证的免费组件，把自有投入放在内容结构、设计资产、跨文件一致性和交付体验上。这些能力更可能形成 Lunitide 持续的产品优势。

## 21 外部资料与许可来源

以下均于 2026 年 9 月 11 日核验；未标发布日期的网页不推定其发布时间。链接指向产品官方文档、作者仓库或论文。主分支许可证是核验快照，实际集成应锁定发布版本或提交并保存对应许可。

1. Gamma。[开发者文档与 API 权益](https://developers.gamma.app/)。用于确认 API 存在及接入范围。
2. Gamma。[API 访问说明](https://help.gamma.app/en/articles/11962420-does-gamma-have-an-api)。用于确认计划门槛。
3. Gamma。[导出与格式限制](https://help.gamma.app/en/articles/8022861-what-s-the-easiest-way-to-export-my-gamma)。用于 PPTX、表格、字体及 DOCX 边界。
4. Plus AI。[Presentation Agent API](https://plusai.com/features/presentation-agent-api)。用于原生输出、修改与模板能力。
5. Plus AI。[定价](https://plusai.com/pricing)。用于试用、席位标价和品牌能力。
6. Plus AI。[MCP](https://plusai.com/features/mcp)。用于官方 MCP 存在性。
7. Beautiful.ai。[定价与 Smart Slides 功能](https://www.beautiful.ai/pricing-plans)。用于版式、品牌和席位价格。
8. Beautiful.ai。[API 帮助](https://support.beautiful.ai/hc/en-us/articles/43654071102605-Beautiful-ai-API)，2026-05-13 更新；[结构化创建](https://docs.beautiful.ai/reference/createpresentation-1)与[导出](https://docs.beautiful.ai/reference/exportpresentation-1)。用于 API 存在性及说明差异。
9. Canva。[Autofill guide](https://www.canva.dev/docs/connect/autofill-guide/)。用于品牌模板 API 和 Enterprise 条件。
10. Microsoft。[Word Excel PowerPoint Agents](https://learn.microsoft.com/en-us/microsoft-365/copilot/wordexcelppt-agents)；Google。[Gemini in Slides](https://support.google.com/docs/answer/14355071?hl=en)。用于原生办公流程对照。
11. Presenton。[官方仓库](https://github.com/presenton/presenton)、[Apache-2.0 LICENSE](https://raw.githubusercontent.com/presenton/presenton/main/LICENSE)。用于自部署、模板、API、MCP 和代码许可。
12. PptxGenJS。[官方仓库](https://github.com/gitbrent/PptxGenJS)、[MIT LICENSE](https://raw.githubusercontent.com/gitbrent/PptxGenJS/master/LICENSE)。用于新建原生 PPT 后端候选。
13. Zheng 等。[PPTAgent](https://arxiv.org/abs/2501.03936)，2025-01-07 首次提交，2025-02-21 修订；[MIT LICENSE](https://raw.githubusercontent.com/icip-cas/PPTAgent/main/LICENSE)。用于参考页驱动生成和评价维度。
14. Zheng 等。[SLIDEFORGE](https://arxiv.org/abs/2609.03109)，2026-09-02。用于结构化可控编辑研究方向。
15. Anthropic。[Skills 官方说明](https://github.com/anthropics/skills)、[PPTX skill 许可](https://raw.githubusercontent.com/anthropics/skills/main/skills/pptx/LICENSE.txt)。用于源码可见与再分发限制。
16. Anthropic。[frontend-design LICENSE](https://raw.githubusercontent.com/anthropics/skills/main/skills/frontend-design/LICENSE.txt)。用于该技能目录独立的 Apache-2.0 许可。
17. Paul Bakaus。[Impeccable LICENSE](https://raw.githubusercontent.com/pbakaus/impeccable/main/LICENSE)。用于 Apache-2.0 许可。
18. Dolan Miu。[docx LICENSE](https://raw.githubusercontent.com/dolanmiu/docx/master/LICENSE)。用于 MIT 许可。
19. Docxtemplater。[双许可](https://raw.githubusercontent.com/open-xml-templating/docxtemplater/master/LICENSE.md)、[产品与扩展说明](https://docxtemplater.com/)。用于核心许可、付费模块和渲染边界。
20. Excelize。[BSD-3-Clause LICENSE](https://raw.githubusercontent.com/qax-os/excelize/master/LICENSE)。用于 Go Excel 组件许可。
21. Typst。[Open Source](https://typst.app/open-source/)。用于编译器许可和模板许可区分。
22. WeasyPrint。[BSD-3-Clause LICENSE](https://raw.githubusercontent.com/Kozea/WeasyPrint/main/LICENSE)。用于 PDF 排版候选许可。
23. Docling。[MIT LICENSE](https://raw.githubusercontent.com/docling-project/docling/main/LICENSE)、[模型目录](https://github.com/docling-project/docling/blob/main/docs/usage/model_catalog.md)。用于解析能力的代码与模型边界。
24. Microsoft。[Playwright MCP](https://github.com/microsoft/playwright-mcp)、[LICENSE](https://raw.githubusercontent.com/microsoft/playwright-mcp/main/LICENSE)。用于浏览器 QA 候选。
25. GongRzhe。[PowerPoint MCP](https://github.com/GongRzhe/Office-PowerPoint-MCP-Server)、[MIT LICENSE](https://raw.githubusercontent.com/GongRzhe/Office-PowerPoint-MCP-Server/master/LICENSE)。用于社区工具功能与许可。
26. GongRzhe。[Word MCP](https://github.com/GongRzhe/Office-Word-MCP-Server)、[MIT LICENSE](https://raw.githubusercontent.com/GongRzhe/Office-Word-MCP-Server/main/LICENSE)。用于社区工具许可。
27. haris-musa。[Excel MCP](https://github.com/haris-musa/excel-mcp-server)、[MIT LICENSE](https://raw.githubusercontent.com/haris-musa/excel-mcp-server/main/LICENSE)。用于社区工具许可。
28. The Document Foundation。[LibreOffice 许可](https://www.libreoffice.org/licenses/)、[FAQ](https://www.libreoffice.org/faq/)。用于商用使用和再分发要求。
29. DreamNum。[Univer](https://github.com/dream-num/univer)、[导入导出说明](https://docs.univer.ai/guides/sheets/features/import-export)。用于 OSS/Pro 边界。
30. ONLYOFFICE。[Community 许可 FAQ](https://helpcenter.onlyoffice.com/docs/faq/docs-community.aspx)、[Developer FAQ](https://helpcenter.onlyoffice.com/docs/faq/developer.aspx)。用于闭源嵌入与白标条件。
31. Handsontable。[HyperFormula 许可](https://hyperformula.handsontable.com/docs/guide/license-key.html)。用于 GPL/商业双路径。
32. Artifex。[PyMuPDF 许可说明](https://pymupdf.readthedocs.io/en/latest/about.html)。用于 AGPL/商业许可边界。
33. Google。[Google Fonts](https://developers.google.com/fonts)。用于开放字体可商用原则，具体字体仍需逐个核验。
34. Lucide。[官网与许可说明](https://lucide.dev/)。用于 ISC 图标来源。
35. Pexels。[图片许可](https://www.pexels.com/license/)。用于图库使用边界。
36. Unsplash。[图片许可](https://unsplash.com/license)。用于图库使用边界。
37. Gotenberg。[安装与转换能力](https://gotenberg.dev/docs/getting-started/installation)、[MIT LICENSE](https://raw.githubusercontent.com/gotenberg/gotenberg/main/LICENSE)。用于可选云端转换服务；不代表全部镜像依赖都是 MIT。

## 22 项目证据索引

以下链接对应本次阅读的本地文件与当时行号。它们用于支撑现状分析，不作为新增需求已经实现的证明。

| 证据 | 文件 |
|---|---|
| 产品架构与部署 | [README](E:/Trae-Work-Projects/lunitide/README.md) |
| 原有 Office Studio PRD | [原 PRD](E:/Trae-Work-Projects/lunitide/docs/design/PRD-office-studio-v1.md) |
| 固定主题和字体 | [pptx_design.go](E:/Trae-Work-Projects/lunitide/internal/officetools/pptx_design.go:9) |
| Studio 版式分支 | [studio_pptx.go](E:/Trae-Work-Projects/lunitide/internal/officetools/studio_pptx.go:146) |
| 现有结构化 Spec | [types.go](E:/Trae-Work-Projects/lunitide/internal/officestudio/types.go:74) |
| Excel 样式与生成 | [generate.go](E:/Trae-Work-Projects/lunitide/internal/officestudio/generate.go:140) |
| 工作表宽度和打印布局 | [sheet_layout.go](E:/Trae-Work-Projects/lunitide/internal/officestudio/sheet_layout.go:13) |
| 几何检查范围 | [geometry.go](E:/Trae-Work-Projects/lunitide/internal/officestudio/geometry.go:18) |
| 渲染组件探测 | [renderer.go](E:/Trae-Work-Projects/lunitide/internal/officerender/renderer.go:108) |
| 实际域更新与重算 | [native.go](E:/Trae-Work-Projects/lunitide/internal/officerender/native.go:55) |
| 预览证据与原文件区别 | [native_checks.go](E:/Trae-Work-Projects/lunitide/internal/officeapp/native_checks.go:39) |
| 指标来源基础 | [facts.go](E:/Trae-Work-Projects/lunitide/internal/officeapp/facts.go:15) |
| 已有设计技能与限制记录 | [社区来源清单](E:/Trae-Work-Projects/lunitide/docs/design/COMMUNITY-SKILLS-SOURCES-2026-09-07.md) |
