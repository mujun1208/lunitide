# Lunitide 0.4.93

0.4.92 装上之后，MCP 市场里 DuckDuckGo / YouTube Transcript 仍会握手失败，Cursor 对话会报「请求超时参数无效」，媒体中心进门还是一张空表单，Work / AgentHub 两个壳层也和实际用法对不上。0.4.93 把这批已经接线的壳层、MCP、超时、媒体和会议纪要收进安装包。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。不重做办公 PPT 视觉稿。不把本机 OCR 换成 PaddleOCR-VL-1.6。

已装 0.4.92 可直接覆盖安装。`v0.4.93` 的 Setup + `latest.json` 挂到 GitHub Latest。不要覆盖 `v0.4.92` / `v0.4.91` / `v0.4.90` / `v0.4.89` / `v0.4.88` / `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84` 的 digest。不要重打 `v0.4.92`。

## 1. Chat / Work

- 顶栏两个壳层改成 Chat 和 Work。内部路由仍是 `lunitide` / `agentHub`。
- 项目管理、技能中心、专家中心、MCP、插件、资产管理在 Work 左侧红框里，可折叠，也可上下拖。
- Chat 原来的项目位改成「办公」：办公工作台、自动化、媒体中心、同事聊天、会议记录按设置显示，折叠和横向拖动还在。
- 办公菜单可单独开关媒体中心和自动化，和办公工作台、会议记录同一套本机偏好。关闭只藏导航。
- 对话里打开周报 Word / 办公产物仍进办公工作台，不依赖侧栏是否显示「办公工作台」。

## 2. MCP

- stdio 握手按 LSP `Content-Length` 帧读写，也认数字 / 字符串 JSON-RPC id 和数字版本号。
- DuckDuckGo、YouTube Transcript 走 Hermes / OpenClaw 同类无密钥包；「检查并修复」会按当前市场包重装，失败则提示卸载。
- 市场推荐一键安装的免费服务（含 Everything）；Brave / GitHub 仍不进一键市场。
- 已隔离的 MCP 也能做健康检查，修复不会被旧的拒绝挡住。

## 3. 超时与媒体

- Host / Engine 对过大的 `deadlineMs` 改为按方法上限收紧，不再回「请求超时参数无效」。
- Cursor / Agent 对话按 180 秒；本机选文件按 120 秒。旧桌面若仍只认 30 秒，前端会再试一次 30 秒。
- 媒体中心进门就是播放器（封面 / 剧场），不再先摊一张空表单。本机选文件走原生对话框，并挂到 Lunitide 窗口。

## 4. 会议纪要与 OCR

- 纪要按卡片、议题、表格、决议、待办人来读；导出 HTML 带完整逐字稿，不再是一块 `<pre>`。
- 模型按结构化 JSON 写摘要，落库仍是 `summary` / `actions`，旧纪要的「一、」「背景」也能拆成卡片。
- Windows OCR 可重新检查、一键补中英文语言包。周报一类扫描件仍按路由修订用新识别结果，不拿过期 OCR。

## 5. 发布

- GitHub Latest 指向 `v0.4.93`。旧版 tag 保留。本地只保留这一次签名安装包。

## 6. 不做

- 不安装、不自动路由 PaddleOCR-VL-1.6。
- 不把 PRD 第 12 章标成已验证。
- 不加 migration 0166。
- 不做系统媒体控件的进度条拖动和音量。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 不把 draw.io 装进产品。
- 不重打 `v0.4.92` / `v0.4.91` / `v0.4.90` / `v0.4.89` / `v0.4.88` / `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84`。
- 不声称 MCP、媒体、会议纪要已经 10 分满分。

## 产物

- `release/out/Lunitide-Setup-0.4.93-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
