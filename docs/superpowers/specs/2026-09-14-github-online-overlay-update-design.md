# GitHub 在线覆盖升级

日期：2026-09-14。范围：在已有本机 `latest.json` + 静默 `/S` 覆盖上，增加从 GitHub Releases 检查并下载 Setup。不改 poison / 月伴 TTS / 玉盘像素 / 星尘配方。不声称产品 100%。不把工厂 100% 和整产品 100% 混为一谈。

## 已有

- 检查、顶栏、`appUpdate.check` / `appUpdate.install`、SHA-256、本机 `%LOCALAPPDATA%\Lunitide\updates`、NSIS `/S` 已落地。
- `latest.json` 字段只有 `version` / `channel` / `sha256` / `installer`。未知字段失败。
- `networkpolicy.Fetch` 默认 1 MiB，不能装安装包。

## 行为

1. `appUpdate.check`：本机更新目录已有更新的 Setup 时仍用本机。否则 HTTPS GET  
   `https://github.com/mujun1208/lunitide/releases/latest/download/latest.json`  
   （可用 `LUNITIDE_UPDATE_FEED_URL` 覆盖）。解析后若版本高于当前，返回 version/digest，此时不下 Setup。
2. 点「立即升级」：本机已有匹配摘要则只校验；否则拉  
   `https://github.com/mujun1208/lunitide/releases/download/v{version}/Lunitide-Setup-{version}-x64.exe`  
   流式写入更新目录，校验 SHA-256，写 `latest.json`，再 `/S`。
3. 本机坏 `latest.json` 失败。远程失败且没有本机包：当作没有更新（顶栏不出现）。
4. 安装包 URL 不从 JSON 新字段读取。只认固定 GitHub 路径，或测试用 feed 同目录文件名。
5. 只允许 `github.com` 与 `*.githubusercontent.com`。回滚仍是空操作。
6. 第一次仍须手动装上带本逻辑的版本。

## 发布

`Build-Release.ps1 -Publish` 才上传 GitHub Release，且必须签名。CI `publish-update-feed` 在打 `v{VERSION}` tag 后单独打包并标 Latest。不要覆盖已发布 digest；升版本号再发。

## 不做

WinSparkle、GitHub API 猜资产、强制更新、远程回滚、声称已在已装旧版上点通过。
