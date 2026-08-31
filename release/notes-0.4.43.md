# Lunitide 0.4.43

## 修复与改进

- **对话契约**：文字会话、月伴、同事聊天共用同一套回合状态。新增 `chat.prefer`（记住当前模型）和 `chat.turn.get`（恢复未写完的回答）。回答生成后写库失败会显示「只重试写入」，不再让人重说一遍。
- **完成事件**：`completed` 可带 `persistFailed` / `memorySummary`。空的 `completed:{}` 视为无效事件，避免界面误当成回合结束。
- **月伴插话**：思考或朗读时再说一句会排队，忙完再发；点月亮打断仍立刻生效。上一句还在发送（`CHAT_BUSY`）会自动重试，配置缺失或没记下这句话不会死循环。
- **模型偏好**：首页、设置、供应商保存后会写入 `chat.prefer`。月伴和文字会话用同一套已选模型，不再各记各的。
- **同事聊天**：线程绑到真实工作会话（工具 / 记忆 / 审计）。没配模型、@ 不到人、回复发不出去，用系统提示说明，不再假装同事说了话。同群排队有上限。
- **记忆检索**：已确认记忆和摘要走 FTS，同事按主体注入，跨面对话能接上「刚才的」。
- **工具档位**：对话可按 `minimal` / `coding` / `colleague` 收工具面；设置里的能力包决定子代理能读什么。
- **桌面寿命**：本机只跑一份网关（互斥）。关窗口不杀引擎；引擎死了才拉起来。托盘仍是同一个 `Lunitide.exe --tray`。
- **消息通道**：入站支持钉钉，并可带会话 ID；飞书 / 企微回复路径收齐。
- **界面收口**：连接器页和独立能力中心并进设置 / 插件；侧栏不再多两个空入口。

## 验证

- Quality 等价门禁连续三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（158 files / 1170 tests）、`npm --prefix web run build`、生成契约与 `verify:bridge --check` 一致
- 第 1 轮修了桥接客户端把空 `completed:{}` 收成成功结束的问题
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.43 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.43-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`be74647d4c6a726e64a2f4f1d8961756b5d2ecaa10424e3a95d04ce216dbe856`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.43 安装包与 stage
