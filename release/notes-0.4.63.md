# Lunitide 0.4.63

把日常任务 P0/P1 与后续收口打进产品版本：五路收面、备用 Key、能力路由、GUI 回退写回、视频理解诚实失败。不改 poison / 说完再答 / 月伴 TTS / 专家表；`desktop.open` 成功仍只认前台；不伪造 `ENGINE_UNAVAILABLE`。

## 1. 日常任务 P0 / P1

- P0：任务路由、flash / judge 启发式、`plan.verify` + L0、先 observe 再坐标、登录墙 / UAC / 另存为停车、CJK 走 paste、纯打开 settle。
- P1：能力路由表、备用凭据列表、可选 Embedder（仅 `openai_compatible`）、4.9 路径与模拟机务验收。
- 电脑控制仍不自开；子代理无电脑控制。

## 2. GUI 回退写回

- SoM 选点失败写回本轮，不假装点中。
- 批量 / 过期 observe / 登录墙：回退停在诚实失败，不把 judge 模型当对话模型用。

## 3. 供应商备用 Key 与能力路由

- 备用 Key 添加 / 移除后立刻用服务端回写更新列表；过期的 `list()` 不再把备份计数打回 0。
- 浏览态给出「备用 Key 已添加 / 已移除」提示。
- 设置页能力路由可注入 `roles`，会话选择器只列出 `llm` 模型。

## 4. 视频理解诚实失败

- 空页 / 登录墙 / 验证码：不把页面 meta 当成看完了的内容。
- §8 路由契约锁天气只搜网、开汽水不开 `computer.act`（电脑控制关）、分享链不假装看完。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **50.7% ≥ 49%** 闸）。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **193 files / 1473 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。

## 安装包

- `release/out/Lunitide-Setup-0.4.63-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.62 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
