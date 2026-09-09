# Lunitide 0.4.71

把同事在 0.4.70 之后完成的办公工作台、专家/技能生命周期、Token 计量、会议多端采集和模型交付修复打进正式版本。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。OCR 与生图代理通道按既有配置保留，不把单元测试当成上游可用。

## 1. 办公工作台与文档

- 任务列表进出、恢复/重试、分栏、参考附件与交付物分离。
- Word / PPT / Excel / PDF 生成、预览、图表与图片替换走同一套产物契约。
- 中文 PDF 字体与分页预览保留；明确用户指定的格式不被挂载专家覆盖。

## 2. 专家、技能、社区包

- 手动专家：创建默认停用、试用、启用、带归属的删除。
- 技能草稿/试用/发布、本地 .skill 包上传、社区目录嵌入。
- 同事专家可绑定本机 Codex / Claude Code 大脑；失败回落月汐。

## 3. 对话、语音、会议、计量

- 视觉模型实际拒图后转已配置 OCR；生图落成真实 PNG，已提交不自动换模型重付。
- 会议采集覆盖当前活动输出端点加麦克风。
- 前台显示真实 usage 与缓存读写；未知不算零成本。默认精简可 `LUNITIDE_TOKEN_EFFICIENCY=off` 回退。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；覆盖率闸 **57.4% ≥ 51%**。
- 前端：`npm audit` **0**（vitest **4.1.11**）；`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **263 files / 2005 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.71-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.70 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
