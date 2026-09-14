# Lunitide 0.4.83

本版补上周报/办公生成路由，并接上 GitHub Releases 在线覆盖升级：检查 `latest.json`，点「立即升级」再下载 Setup，校验 SHA-256 后静默 `/S` 覆盖。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%，也不把工厂 100% 和整产品 100% 混为一谈。不把 Agent Hub 焊进 SessionPage。

已装 0.4.81 及更早版本没有下载器，**第一次**仍须手动安装本版。之后才能在线点升级。GitHub 上当前 Latest 若仍是 0.4.73，要等本版 Setup + `latest.json` 发布后，检查才会指向 0.4.83。

## 1. 周报与办公路由

- 「写周报」「生成 Word」走 R4，工具表保留 `office.generate` / `docx.gen`。
- 「不打开文件」不再被当成打开意图，避免新闻+文件误进桌面 R2。
- `plan.run` 步骤继承合并后的 allow，办公工作台工具不再在计划步骤里丢掉。
- `skill.try` 在路由收缩后保留。

## 2. 在线覆盖升级

- 检查：本机 `%LOCALAPPDATA%\Lunitide\updates` 优先；否则 GET `https://github.com/mujun1208/lunitide/releases/latest/download/latest.json`。
- 点「立即升级」才下载 `releases/download/v{version}/Lunitide-Setup-{version}-x64.exe`，流式落盘、校验摘要、再 `/S`。
- 本机已有匹配摘要的包不重下。远程失败且没有本机包：顶栏不出现。摘要不一致失败，不装错包。
- `latest.json` 字段仍只有 `version` / `channel` / `sha256` / `installer`。未知字段失败。摘要接受 A-F。
- `Build-Release.ps1` 写出 `latest.json`，清理更新目录里的旧 Setup，并在 `gh` 已登录时上传到 `v{VERSION}` 且标为 Latest。

## 3. 不做

- 不做 Memory Fabric / PaddleOCR / 媒体 PRD。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 本环境不能在 Linux 上打 Windows NSIS 包；Windows 主机执行 `Build-Release.ps1` 后 GitHub Latest 才会带上 0.4.83 Setup。

## 产物

- `release/out/Lunitide-Setup-0.4.83-x64.exe`
- `release/out/latest.json`
