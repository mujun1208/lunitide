# Lunitide 0.4.96

0.4.95 装上之后，月伴第二跳会报「无法执行。模型请求失败」；Cursor Hub 显示已连接但发不出字；MCP 市场红点点「重新连接」也失败。这三处是同一轮活修：模型错误分类、Cursor CLI 登录探测 + ACP 认证、MCP 健康态竞态和付费残留撤销。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把本职 9.5 说成已完成。

已装 0.4.95 可直接覆盖安装。本地只保留这一次签名安装包。不要覆盖 `v0.4.95` 及更早 tag 的 digest。不要重打 `v0.4.95`。GitHub Latest 除非再次点名，否则不自动改挂。

## 1. 月伴「模型请求失败」

- 第二跳 `http_status=0 stage=unknown` 时，超时、坏 JSON、工具表过大不再一律说「模型请求失败」。
- 超过 12 个 `mcp_*` 工具时，合并后改走 `mcp.search` / `mcp.call`，避免第二跳被撑爆。
- `TIMEOUT` 和 `context.DeadlineExceeded` 报「模型请求超时」。汽水随机播放、打开网页都走这条，不是桌面打开本身坏了。

## 2. Cursor Hub

- 「已连接」不再只看 `cursor-agent --version`。`status` / `whoami` 未登录时显示未登录，并提示再点一次「连接」。
- ACP `initialize` 若带回 `authMethods`，先 `authenticate` 再 `session/new`。
- `Authentication required` 映射成中文：CLI 还没登录，不要只看已连接。
- 桌面 Cursor 已登录时，Agent Hub 点「连接」会跑 `cursor-agent login`。不要从桌面拷 token。

## 3. MCP 市场

- `mcp.health` 按库里当前行写状态，不再和 hydrate 抢同一行就 `INTERNAL_ERROR`。
- Docker / 聚合查询 / Tavily / Firecrawl / Brave / `token=` 远程等要密钥或收费的残留，启动时撤销，不再占「已连接」。
- 免费 stdio（Playwright、Filesystem、Memory、Thinking、Fetch 等）仍可一键重连。能力包被关掉时，重连会诚实失败，不会假绿。

## 4. 不做

- 不重打 `v0.4.95` 及更早。
- 不加公网 Gateway，不加无人批准的 `skill_manage`。
- 不把综合尺 10 分说成本职完成。本职 9.5 仍要装上本包后再跑场验：月伴打开网页/汽水、Cursor 发出去、MCP 红点可修。

## 产物

- `release/out-0.4.96/Lunitide-Setup-0.4.96-x64.exe`
- `release/out-0.4.96/latest.json`
- `release/out-0.4.96/SHA256SUMS.txt`
