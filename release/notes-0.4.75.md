# Lunitide 0.4.75

把 0.4.74 之后已验证的会话收尾、图表等待、专家点名和周报试运行补丁打进正式版本。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称邮件、共享日历或在线 48 场景已通。

## 1. 会话收尾与图表

- 只在 `chatStatus === 'streaming'` 时钉底；结束后 Mermaid 布局不再把滚动拽回底部。
- `ctxStatus` / 输入队列 JSON 不变不 `setState`，避免对话结束后每隔几秒闪一下。
- Mermaid 只在围栏尚未闭合时显示「图表生成中」；回合结束后未就绪源码直接渲染或回退，不再永久等待。

## 2. 专家点名

- `剧本专家` / `编剧专家` 解析到 `novel-writer`，不再因为弱打分装上航空维修专家。
- 已挂载专家只在当前轮点名另一位专家时让位；「分析 Excel」这类弱匹配不会挤掉已装备专家。
- 意图匹配只用本轮文本，避免上一轮航空维修泄漏到剧本轮。

## 3. Skill 分页与周报试运行

- `skill.view` 工具仍分页；引擎在首次查看后把剩余页组装进同一模型载荷（有上限），避免来回 continue 拖到十几分钟。
- `skill.manage` / `skill.create` 成功后停轮，不再为再读一遍 SKILL.md 空转。
- `sanitizeProviderReplay` 丢掉回合中途的 system，并合并连续 assistant，避免 `CONTEXT_SEQUENCE_INVALID`。
- 序列无效错误走显式 chat 回退；试运行「生成周报」仍走 `docx.gen`，不只读 SKILL.md。

## 验证

- Go：`go test ./...` 覆盖率闸 **58.8% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无生成器错误；生成文件无漂移；`vitest run` **288 files / 2176 tests** 全绿；`vite build` 通过。
- `./release/Test-OmniExcluded.ps1` 通过；`./release/Test-ReleaseTools.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.75-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.74 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
