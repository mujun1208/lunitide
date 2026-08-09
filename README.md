# Lunitide 月汐

本地优先的 AI 桌面工作台。正式生产架构为 **Go Core Engine + Windows WebView2 Host + React/TypeScript Renderer**。现有 Electron/Python `0.2.1` 代码仅作为功能回归基线和迁移输入，不承载生产能力。

## Go P0/P1 开发

```powershell
go test ./...
go build -o bin/lunitide-engine.exe ./cmd/engine
go build -o bin/lunitide-desktop.exe ./cmd/desktop
New-Item -ItemType Directory -Force bin/web | Out-Null
Copy-Item -Recurse -Force web/dist bin/web/dist
bin/lunitide-desktop.exe -engine bin/lunitide-engine.exe
```

当前 Go/WebView2 生产实现已覆盖完整 P0/P1：Bridge/RPC 契约、受认证 Named Pipe、SQLite、Provider/Model CRUD、DPAPI 凭据、模型发现与诊断、两类协议 Gateway、流式与取消、Electron 安全迁移及原生 NSIS 发布。Host 固定把 exe 相邻 `web/dist` 映射为 `https://app.lunitide.local`，通过 Core 与 Frame 的不同 COM WebMessage 事件区分顶层/子 frame；只允许顶层可信来源进入安全网关。启动时不接受任意 renderer 路径覆盖。

WebView2 Host 使用精确依赖 `github.com/zzl/go-webview2 v0.0.0-20230129130204-9df4a7d166d5` 的 `wv2` 包，并要求运行时支持 `ICoreWebView2_3`、`ICoreWebView2_4` 和每个 frame 的 `ICoreWebView2Frame2`，缺失即关闭窗口（fail closed）。不使用 Bind、host object、ExecuteScript 注入或密钥方法。

独立 Renderer 验证：

```powershell
node web/scripts/generate-bridge.mjs
node_modules/.bin/tsc --noEmit -p web/tsconfig.json
node_modules/.bin/vite build web --config web/vite.config.ts
```

P0 IPC 安全门禁已关闭：Engine bootstrap 凭据通过受限继承句柄传递，不进入命令行，并具备单次消费、replay 防护和双向 PID 验证。Host 关闭已认证主会话后 Engine 会自行退出并清理 SQLite，超时才允许 Host 强制终止。详见 `docs/adr/ADR-002-ipc-security.md`。

发布布局必须包含 `Lunitide.exe` 相邻的 `web/dist/index.html` 和同架构的官方 `WebView2Loader.dll`。Host 会在调用绑定前验证 Loader 及其必需导出，并检测 Evergreen Runtime；缺失时返回受控错误，不允许上游 LazyDLL panic。源码树 smoke 应先把构建后的 `web/dist` 复制到 `bin/web/dist`，不提供运行时 renderer 路径覆盖。

当前 Windows 构建使用 `CGO_ENABLED=0`，因此本机 `go test -race` 不可用；普通单元/契约测试、`go vet` 和纯 Go 构建是当前本地门禁，race 需要在具备受控 CGO 工具链的 Windows CI 作业中执行。

本机发布与外部验收的准确状态见 `docs/implementation/P0-P1-status.md`。正式发布必须具备 Windows race、Win10/Win11 一次性环境安装生命周期、有效 Authenticode 签名/时间戳以及匹配版本 tag 的证据。

## 旧原型开发启动

```powershell
npm install
npm run setup:engine
npm run dev
```

## 验证

```powershell
npm run typecheck
npm run test:engine
npm run test:engine:smoke
npm run build
# 或一次运行全部 M2 自动验收
npm run verify:m2
```

## M2 能力

- 设置页支持 OpenAI-compatible 与 Anthropic 供应商的新增、编辑、删除和真实连接测试。
- 统一网关提供 `/v1/chat/completions`，非流式响应统一为 `content/usage`，流式事件统一为 `start/delta/usage/done/error`。
- API Key 由 Electron 主进程使用 `safeStorage` 加密；Renderer 不会读取已保存的明文 Key。
- 系统安全存储不可用时自动降级为仅会话内存，不落盘；密钥与协议和服务来源绑定。
- Base URL 默认要求 HTTPS，仅显式本机服务允许 HTTP；网关拒绝私网、保留地址和自动重定向。
- Python 引擎只绑定 `127.0.0.1` 动态端口，每次启动使用随机 Bearer Token。

## M2 手工验收

1. 打开“设置”，分别添加 OpenAI-compatible 和 Anthropic 配置。
2. 保存后点击“测试连接”，确认真实模型返回“连接成功”。
3. 重启应用，再次测试两个供应商，确认密钥仍然可用。
4. 检查 `%APPDATA%/lunitide/providers.json` 与日志，确认不存在明文 API Key。
5. 使用错误 Key、不可达 URL 或触发 429，确认界面显示归一化中文提示。
