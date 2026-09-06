# Lunitide 0.4.67

把同事这轮安全、对话上下文、技能与引擎拆分打进产品版本。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。

## 1. 安全与运行时

- 身份 Ed25519 私钥改走 DPAPI 密封，不再明文躺在数据库列里。
- `command.run` 只继承白名单环境变量，子进程拿不到引擎里的密钥和令牌。
- Windows 用 Job Object 管进程树，全盘模式要本会话确认一次才放行写工具。
- 凭据授权摘要一律服务端重算；浏览器 Open 拒绝回环 / 内网 / 链路本地。
- 审计链补上 `secret.put` / `credential.submitted` / `audit.export`。

## 2. 对话、月伴、同事

- 火山听写失配时按共享前缀截断，不再把上一句拼进本轮。
- 专家注入有 token 预算；月伴只带最近 3 轮；打字对话可 @ 引用消息。
- 不支持 function calling 的推理模型不再硬塞工具配置。
- 月伴工具执行超时会口播「还在继续」；实时通话上行有界队列，链路恢复后不再回放积压。
- 同事通道可按身份做 NaCl 密封；未知公钥仍明文投递，不堵发送。

## 3. 引擎与前端结构

- 引擎装配抽到 `internal/bootstrap`；流生命周期抽成 StreamEngine。
- LLM 适配器包改名为 `llmadapter`；SQLite store 按项目/会话/消息/供应商拆文件。
- 记忆搜索走 FTS5；技能调用落库并带数值 OCC；压缩触发器改有界 LRU。
- 已知 OpenAI 系模型用离线 tiktoken 精确计数，未知模型仍回退启发式。
- 前端主题、导航、会话列表、MRO 门控收敛进 Zustand store；页面错误边界补到模型管理浮层。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **51.0% ≥ 49%** 闸）。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **202 files / 1553 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.67-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.66 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
