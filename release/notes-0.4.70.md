# Lunitide 0.4.70

把同事 9 月 7 日六类能力与回归整改打进正式版本：检索/天气、电脑 GUI、浏览器、文档与文件系统、自动化调度，以及对话/语音/子代理修复。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。

## 1. 检索、天气、文档

- 新增 weather.get：离线城市定位 + MET 预报；搜索增加短缓存、取消和失败识别。
- 天气传输只打已配置的 MET 接口，并带条件缓存头。
- Word/PPT/Excel 走真实正文；中文 PDF 嵌入 Lunitide Sans SC。
- 工作区可读 Office/PDF 文本并分页；单文件替换失败保留原件。

## 2. 电脑、浏览器、调度、子代理

- 电脑控制补右击/双击/组合键与命中检查；浏览器动作与快照同源。
- 自动化可取消指定运行，支持 IANA 时区；不误停其他任务。
- 独立检索子代理真实并行，状态行来自实际执行。

## 3. 对话、语音、记忆

- 附件、入队补充、过程折叠与选择技能不再被目录截断。
- 三语音完整句及时开答；转写快照不被迟到包覆盖。
- 记忆采集模式独立于总开关（迁移 0142：auto/manual/off）。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **54.9% ≥ 51%** 闸）。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **231 files / 1793 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.70-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.69 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
