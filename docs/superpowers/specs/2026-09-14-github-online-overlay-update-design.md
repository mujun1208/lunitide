# GitHub 在线覆盖升级

日期：2026-09-14。范围：在已有本机 `latest.json` + 静默 `/S` 覆盖上，增加从 GitHub Releases 检查并下载 Setup。不改 poison / 月伴 TTS / 玉盘像素 / 星尘配方。不声称产品 100%。不把工厂 100% 和整产品 100% 混为一谈。

## 已有

- 检查、顶栏、`appUpdate.check` / `appUpdate.install`、SHA-256、本机 `%LOCALAPPDATA%\Lunitide\updates`、NSIS `/S` 已落地。
- `latest.json` 字段只有 `version` / `channel` / `sha256` / `installer`。未知字段失败。
- `networkpolicy.Fetch` 默认 1 MiB，不能装安装包。

## 行为

1. `appUpdate.check`：本机更新目录已有更新的 Setup 时仍用本机。否则 HTTPS GET  
   `https://github.com/mujun1208/lunitide/releases/latest/download/latest.json`  
   （可用 `LUNITIDE_UPDATE_FEED_URL` 覆盖，仅测试/镜像）。解析后若版本高于当前，返回该 version/digest，**此时不下 Setup**。
2. 点「立即升级」：`Download` 若本机已有匹配摘要的 Setup 则只校验；否则按版本拉  
   `https://github.com/mujun1208/lunitide/releases/download/v{version}/Lunitide-Setup-{version}-x64.exe`  
   流式写入更新目录，校验 SHA-256，再写 `latest.json`，再 `/S`。
3. 本机坏 `latest.json` 仍失败。远程失败且没有本机包：当作没有更新（顶栏不出现）。远程失败但本机有包：只用本机。
4. 检查与安装之间 `latest.json` 变了导致摘要不一致：安装失败，不装错包。
5. 安装包 URL **不**从 JSON 新字段读取（保持 `DisallowUnknownFields`）。只认固定 GitHub 路径，或测试用 feed URL 同目录下的文件名。
6. 只允许 `github.com` 与 `*.githubusercontent.com`（GitHub 资源重定向）。测试服务器走 `AllowHTTP` + localhost。
7. 回滚仍是空操作。不做后台预下载、静默不点安装、差分包、CDN 镜像。
8. **第一次**仍须手动装上带本逻辑的版本；之后才能在线点升级。

## 发布

`Build-Release.ps1` 打完包后，若本机 `gh` 已登录，把 Setup 和 `latest.json` 挂到 `v{VERSION}` Release。发布失败不让签名包构建失败，只报告。标签格式 `v0.4.83`。

## 不做

WinSparkle、GitHub API 猜资产、强制更新、改 Banner 布局（文案可改为「正在下载并安装」）、远程回滚、声称已在已装旧版上点通过。
