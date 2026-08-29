# Lunitide

本地优先的 AI 桌面工作台。生产架构为 **Go Core Engine + Windows WebView2 Host + React/TypeScript Renderer**。

`0.2.1` 的 Electron 原型已在 `0.4.01` 移除：它从未发布，唯一一台装过它的机器上的凭据也已迁入 DPAPI 存储，因此原型本体、迁移代码与其 npm 工具链一并删除。需要查阅时见 tag `m1-pre-audit` 之前的历史。

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

## 仓库布局

根目录的 `package.json` 只是任务清单，没有依赖，因此不需要在根执行安装。Renderer 是 `web/` 下的独立包，自带锁文件；`npm --prefix web ci` 是唯一需要的前端安装步骤。
