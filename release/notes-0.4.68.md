# Lunitide 0.4.68

把同事全系统升级打进正式版本：对话/会议/同事投递、电脑控制、计划执行、技能 MCP、知识库与 MRO 的持久化与恢复。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。

## 1. 对话、语音、会议

- 每轮对话独立 journal，取消/回退/删除不恢复废弃草稿。
- 三语音链共用合法 PCM 分片；停止先冻采集再排空。Realtime 终态转写先落库。
- 会议摘要用条件更新保护人工改稿；音频按不可变批次 ACK。纪要带来源与分页。

## 2. 同事、电脑、计划、资产

- 同事每帧重查信任；文件按所有者/序号/摘要校验；送达走 outbox。
- 电脑控制先持久意图再下发，取消/急停复核目标，审计失败不伪报成功。
- 计划真正挂 run 与产物；项目阶段/文档冻结同事务。
- 资产上传顺序与摘要不可变；创建结果可重开确认。

## 3. 技能、MCP、知识、系统

- 技能导入走固定 SKILL.md 扫描与 rev CAS；MCP 撤销取消在途并清池。
- 知识正文先落文件，版本与装备同事务；召回绑真实主体/库/文档。
- 数据源写要独立确认；命令输出有界；配置带生效版本。迁移 0124–0141。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **53.3% ≥ 51%** 闸）。`0136_br_settings_apply.sql` 按仓库 `eol=lf` 对齐 checksum，避免 Windows 工作树 CRLF 让 CI 的 embed 校验失败。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **219 files / 1668 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.68-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.67 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
