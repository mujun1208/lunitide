# Lunitide 0.4.32

## 修复与改进

- **月伴听/说同步**：TTS 忙碌判定覆盖合成中、预取和时间线余量；扬声器未空时状态条不显示「聆听中」；免提重听等播完再开麦；暂停提交不再清空字幕。
- **识别更完整**：Windows 多假说优先更长、能接上前缀的结果；「联系电话」等字段尾不再被当成整句提交。
- **首段朗读**：问候可立刻出声，其它首段不再按 4 个字切碎。
- **项目管理决策向导**：落地 `user.ask`（编号选项 +「其他」）。完全访问、月伴自动批、hooks 授权和会话记住批准都不会替用户拍板；子代理不可用该工具。
- **SQLite 时间戳比较**：运行时时间写入固定 9 位小数，避免 `RFC3339Nano` 去掉末尾 0 后 `updated_at >= created_at` 在同一秒内误伤。
- **专家中心测试**：场景卡创建等详情栏就绪后再提交，避免 React 状态未刷新导致偶发失败。
- **本机键鼠**：`SetCursorPos` 失败且 last-error 为成功时回读光标、改走 `SendInput` 绝对坐标；全量并行测试抢桌面时该用例跳过而不是误红。

## 验证

- Quality 等价门禁连续三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`、`npm --prefix web run build`、生成契约 `git diff --exit-code`
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）

## 安装包

- `release/out/Lunitide-Setup-0.4.32-x64.exe`（Authenticode `NotSigned`：本机 `LUNITIDE_SIGN_COMMAND` / `LUNITIDE_SIGNER_THUMBPRINT` 未配置，使用 `-AllowUnsignedDevelopment`，**不是**生产签名版）
- SHA-256：`727a4e38f6b888953fbda9c987cd5c3518bdf9134e0e4d1a68c07a184570d38d`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.32 安装包与 stage
