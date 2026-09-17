# Lunitide 0.4.90

0.4.89 装上之后，Work 里点产物「放大」会变成一块黑屏，连「恢复对话」都找不到。AgentHub 绿灯不等于能聊、Codex 发出去像卡住、对话里的 mermaid 一滚动就重新生成。OCR 的「安装」只是选文件夹、识别仍走 Windows。个人信息页的「保存名片」被裁掉。0.4.90 把这几件事一起收掉。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。不重做办公 PPT 视觉稿。不把本机 OCR 换成 PaddleOCR-VL-1.6。

已装 0.4.89 可直接覆盖安装。`v0.4.90` 的 Setup + `latest.json` 挂到 GitHub Latest。不要覆盖 `v0.4.89` / `v0.4.88` / `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84` 的 digest。

## 1. 产物放大

- 放大预览时分隔条从网格里拿掉，只留两列：隐藏的对话 + 占满的产物栏。原先三列会把产物（含「恢复对话」）塞进 0 宽列，看起来整块黑屏。
- 截图预览在放大后占满剩余高度，不再被裁成空白。
- AgentHub 右侧工作区共用这套网格，放大/还原同一处修好。

## 2. AgentHub

- 右侧工作区可以左右拖、可以收起/展开，不再只有一块仿出来的栏。
- Cursor / Kimi 在 Windows 上只有 `.cmd`、本机没有 Node 时不再显示绿灯。聊天需要 Node 配对 ACP。
- 首页列表轮询失败不再刷成「AgentHub 操作失败」。中文/Node 提示原样显示。
- Codex 丢掉 `Reading prompt from stdin…` 这条噪音，exec 补上 `-`，不再把横幅当成回复、也不再空等。

## 3. Work 对话

- mermaid 组件身份稳定，已生成的图缓存在会话里。继续聊或滚动不再整块重挂成「图表生成中…」，也不再把窗口弹跳回去。

## 4. OCR

- 「下载安装」和语音一样：点按钮拉 RapidOCR-json zip、校验、解压。不再弹出选文件夹。装好显示「已安装」。
- 本机引擎：自动（推荐）/ PP-OCR / Windows OCR。自动＝装好 PP-OCR 就用它，否则 Windows。
- OCR 模型下拉按优先级分组：① 供应商 OCR / 视觉模型（名称带 OCR/识别的排前面，含未标视觉的 OCR 命名模型）→ ② 本机 PP-OCR → ③ Windows 兜底。
- 装完或改本机引擎会立刻写入路由（和语音一样），不必再点一次保存才能自动加载。识别真正走已安装的 exe；没再点保存也能用刚装的包。`localReady.backend` 在自动/PP-OCR 且包就绪时是 `ppocr`。本机包仍是 RapidOCR-json，不是 PaddleOCR-VL-1.6。

## 5. 个人信息

- 「保存名片」固定在底部停靠栏，滚动时仍看得见。简介高度受限。名片分组不再被 `overflow:hidden` 裁掉按钮。

## 6. 发布

- GitHub 上已经有签过名的 Latest 时，CI 不再用未签名包重打一遍。

## 7. 不做

- 不做 Memory Fabric / 媒体 PRD。
- 不把本机 OCR 换成 PaddleOCR-VL-1.6（体积、Python/GPU、超时都不适合当前安装包）。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 不把 draw.io 装进产品。
- 不重打 `v0.4.89` / `v0.4.88` / `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84`。
- 不把办公概念预览改成满分视觉稿。

## 产物

- `release/out/Lunitide-Setup-0.4.90-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
