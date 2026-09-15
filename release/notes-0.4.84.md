# Lunitide 0.4.84

把本机两批未单独升版的改动并进 GitHub Latest：AgentHub 对话页、工作区简约壳、路由管理/OCR，以及标题/下载/周报芯片/发布门闩和测试桌面隔离。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。

已装 0.4.83 可直接覆盖安装。`v0.4.84` 的 Setup + `latest.json` 由 `publish-update-feed` 挂到 GitHub Latest。不要覆盖 `v0.4.83` 的 digest。

## 1. AgentHub / Work / 工作区

- 主菜单「月汐」→ Work，「外接 Agent」→ AgentHub。伸缩钮更小，贴顶栏。
- AgentHub 与 Work 共用输入条：`+` 附件 / 项目目录 / 产物目录；权限下拉；任务类型（项目/代码/PPT/文档/其他）；语音听写；打断走 `threadCancel`。
- 顶栏只显示已选中的 Agent。未选中时写 `AgentHub`，不再冒充第一家 CLI。
- 「写周报 Markdown」只出现在文档/其他。Work 里「写周报」仍走办公生成；写明「不要生成 Office」时不再自动出 Word。
- 工作区：浏览器一条地址栏；文件名 + 小下载（无原字节的 Office/PDF 不下载假文件）；代码树可折叠。
- 说话月 HiDPI 按设备像素居中。

## 2. 路由管理与 OCR

- 设置新分类「路由管理」：能力路由 + OCR。供应商页只留目录。
- Windows OCR 始终可选。PP-OCR 不是随包；空目录不算已装。本机识别目前只走 Windows OCR。

## 3. 测试不再写真桌面

- `go test` 里 `desktop=true` 写到运行时 `.testdesktop`，不再往用户桌面掉 `介绍.pptx` / `weekly.docx`。
- 用户明确说「放到桌面」的正式路径不变。

## 4. 发布与 CI

- `Build-Release.ps1 -Publish` 必须签名，且不再 `--clobber` 已有 tag。
- Windows CI 继续隔离 `internal/app` 覆盖率和 CGO race，避免 Go 1.26.6 HashTrieMap / shrinkstack 把整闸打红。

## 5. 不做

- 不做 Memory Fabric / Paddle 下载管线 / 媒体 PRD。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。

## 产物

- `release/out/Lunitide-Setup-0.4.84-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
