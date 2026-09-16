# Lunitide 0.4.89

装不上、GLM-5.3 拒工具、桌面建文件夹只说话、办公预览把 Word 跑当成 143 页 PPT，这几件事在 0.4.88 安装包里都还在。0.4.89 把启动记账行、GLM thinking、本机磁盘动作、办公页数和 AgentHub 右侧工作区一起带上。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。

已装 0.4.88 可直接覆盖安装。`v0.4.89` 的 Setup + `latest.json` 挂到 GitHub Latest。不要覆盖 `v0.4.88` / `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84` 的 digest。

## 1. 启动

- `EnsureAccountingSession` 写入 `execution-accounting` 项目时同时补 `message_project_usage`。
- 打开库时只给这个记账项目补缺失计数；用户项目缺计数仍然 fail-closed，避免把损坏库当成正常库打开。

## 2. GLM-5.3

- 官方 GLM-5.3 不接受 `thinking.type=disabled`。L1/L2 的 DisableReasoning 改成 `enabled` + `reasoning_effort=low`。
- thinking 400 重试时保留工具定义，不再误报成「模型拒绝了工具定义」。

## 3. 本机磁盘动作

- 桌面/本机创建、删除、改名、复制、解压、下载到桌面走 L4，保留 `command.run` 和继续催促，不再停在 I'll create。
- 「写周报放到桌面 / 保存到桌面」仍走办公生成，不抢成建文件夹。删掉桌面上的周报文件仍走本机删除。
- 写光合作用文章、写一篇关于创建文件夹的文章仍走 L1。

## 4. 办公 PPT 与周报

- 「做一个 10 页的 PPT」把 `targetLength=10` 和 pptx 写入 brief，生成时按这个页数裁切，不再静默默认 12 页。
- 参考附件 / 自己思考后立刻 `office.generate`，不把 inspect 当交付，不为此强制 web.search。
- 「写周报」brief 记 docx；inspect 之后继续生成。只看附件写了什么仍不催生成。
- 预览：Word 按 XML 部件成页，不再一个 `w:t` 一页。PPT 左侧幻灯片页、中间一张 16:9；截断时仍可「下一批内容」。

## 5. AgentHub

- 组合框场景改成和「手动审批」一样的芯片，发送和麦克风不再被撑出 720px。
- 右侧挂同一套工作区（文件/代码/终端/浏览器/变更/计划），本地目录走线程 workspace API。AgentHub 和 Work 仍是两个页面。

## 6. 不做

- 不做 Memory Fabric / Paddle 下载管线 / 媒体 PRD。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 不把 draw.io 装进产品。
- 不重打 `v0.4.88` / `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84`。

## 产物

- `release/out/Lunitide-Setup-0.4.89-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
