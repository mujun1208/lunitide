# Work / AgentHub / 工作区 / 路由管理 设计

Date: 2026-09-15  
Status: approved for sequential 100% land (scheme A)

## Goal

把月伴说话月 DPR 修正、右侧工作区简约壳、AgentHub 对话化、Work/AgentHub 壳层、设置路由管理（含 Windows OCR / PP-OCR）全部落地。不做假能力。

## Constraints

- 共享 Composer：从 Work 抽共用输入条，AgentHub 挂同一套交互，不嵌 `SessionPage`。
- 外接 CLI 线程模型保持 `agentHub.thread.*`；语音进输入框，打断 = `threadCancel`。
- PP-OCR 不随包；未安装不可选。安装后才进下拉。Windows OCR 始终可选。
- 右侧工作区不重做浏览器内核。放大/还原绑定已有 `workspaceExpanded`。

## Surfaces

### 0. 说话月

`Strands` 的 `uResolution` / FBO 用 `gl.canvas` 设备像素。已验收。

### 1. 右侧工作区简约壳

统一顶栏：当前 Tab 名 + 小图标（放大/还原对话、关闭）。浏览器只留一条地址栏。文件：文件名 + 小下载 + 预览（MD/TXT/Excel/Word/PPT/PDF）。代码：文件树可折叠，按钮在顶栏且小。终端/计划/变更同一套 chrome。

### 2. AgentHub 对话化

左侧：已装 Agent 状态灯 + 一点连接；灯上是历史任务，替代无用「新对话」。右侧：与 Work 同款对话框。顶栏显示当前 Agent 名。`+`：附件 / 项目目录 / 产物目录，选中后旁侧小字。权限：手动/自动/完全访问下拉。任务类型（项目/代码/PPT/文档/其他）为 Hub 独有下拉。每家 Agent 独立会话与记忆压缩。

### 3. 壳层

「月汐」→ Work，「外接 Agent」→ AgentHub。伸缩钮小、贴顶栏（参考 Trae）。

### 4. 设置 · 路由管理

新分类「路由管理」：能力路由 + OCR 路由。供应商页不再嵌这两块。OCR 本机引擎：Windows OCR、PP-OCR（可点安装）。
