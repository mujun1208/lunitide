# GitHub 在线覆盖升级

日期：2026-09-14。范围：本机 `latest.json` + 静默 `/S` 覆盖，加上从 GitHub Releases 检查并下载 Setup。不改 poison / 月伴 TTS / 玉盘像素 / 星尘配方。不声称产品 100%。

## 行为

1. `appUpdate.check`：本机更新目录已有更新的 Setup 时仍用本机。否则 HTTPS GET  
   `https://github.com/mujun1208/lunitide/releases/latest/download/latest.json`  
   （可用 `LUNITIDE_UPDATE_FEED_URL` 覆盖）。解析后若版本高于当前，返回 version/digest，此时不下 Setup。
2. 点「立即升级」：本机已有匹配摘要则只校验；否则拉  
   `https://github.com/mujun1208/lunitide/releases/download/v{version}/Lunitide-Setup-{version}-x64.exe`  
   流式写入更新目录，校验 SHA-256，写 `latest.json`，再 `/S`。
3. 本机坏 `latest.json` 失败。远程失败且没有本机包：当作没有更新。
4. 安装包 URL 不从 JSON 新字段读取。只认固定 GitHub 路径，或测试用 feed 同目录文件名。
5. 只允许 `github.com` 与 `*.githubusercontent.com`。回滚仍是空操作。
6. 第一次仍须手动装上带本逻辑的版本。

## 发布

`Build-Release.ps1` 打完包后，若本机 `gh` 已登录，把 Setup 和 `latest.json` 挂到 `v{VERSION}` 并标 Latest。
