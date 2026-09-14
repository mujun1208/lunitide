# Lunitide 0.4.63

系统程序复盘修正：引擎死在 hostReady 之前不再吞掉 D11 自愈、退出杀进程强制校验引擎镜像、网关互斥失败闭、MCP stdio 沙箱防逃逸与 401 类型化，并清掉证据树里多余的 engine 安装快照。

## 1. Desktop / Engine 自愈与退出安全

- `waitHostReadyForEngineDeath`：引擎在 `close(hostReady)` 前死亡时会等到 hostReady 或 hostCtx 取消，再决定是否 `--takeover`；不再非阻塞 `default` 吞事件。
- Tray / `--quit`：`stopEnginePID(..., true)`，PID 已不是 `lunitide-engine` 时拒绝 Kill。
- `CreateMutexW` / UTF16 失败改为 fail-closed（视为已有实例，拒绝双开）。
- Bootstrap 丢弃的未用 secret 清零；broker pipe 名标明协议残留。

## 2. MCP / stdioworker

- stdio 工作目录：endpoint ID 必须是 path-safe；`mcpStdioSandboxDir` + Register 双闸防 `..` / 分隔符逃逸。
- HTTP 401 → `mcp.ErrUnauthorized`（不再靠错误字符串含 `"401"`）。
- `stdioworker` 非 Windows stub 通过 `stdioHandles()` 访问，Linux 可编译；stdio 集成测在非 Windows 上 Skip。

## 3. 其它连带

- IM/MCP sidecar 持久化失败写日志，不再静默 `_ =`。
- `stdio-poc` 注释与生产 stdio 已开放一致。
- 删除 `docs/evidence/m3-rc/engine-snapshot.exe`（约 23MB）并更新 bundle 引用。
- Release candidate Quality 对齐 PR：覆盖率地板 + lint + govulncheck。

## 验证

- `CGO_ENABLED=0 GOOS=windows go test -c ./cmd/desktop ./cmd/engine` 通过。
- `go test`（可在 Linux 跑的包）+ Windows CI Quality / RC 全绿后发布。
- 安装包：`release/out/Lunitide-Setup-0.4.63-x64.exe`（Windows 签名机打）。

## 安装包

- 从 0.4.62 升级。本机有签名凭据时按生产签名打包。
