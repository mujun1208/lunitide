# Lunitide 0.4.76

把办公闭环的正式门槛说清楚，并修掉火山识别后补句末标点被丢掉的问题。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称邮件、共享日历、Presenton 进生产主链，或无 LibreOffice 也能把 Word/PPT/Excel 正式交付。

## 1. 办公正式交付

- `office.artifact.accept` 增加可选 `formal`；`formal:true` 且质量不是 `passed` 时返回 `OFFICE_DRAFT_REQUIRED`。
- 独立 PDF 按本文件后端记账：gofpdf 回退可以正式；Typst 配置了但没编过本文件，不得写成 Typst 已通过。
- 质量承诺只认真实检查 id：内容完整看 `package` / `pdf_structure`，排版已检查只认 `native_render` / `actual-render`。
- 「检查通过」不等于已接受为正式版；导出通知写明分页与 PDF/A 不保证。
- 四格式生成落入当前任务；停止检查不再取消正在生成的文件。

## 2. 识别修订

- `pickTranscriptRevision` 在去标点后字数相同时，保留更长的原文，后到的句号/问号不再被上一稿吃掉。

## 验证

- Go：`go test ./...` 覆盖率闸 **59.4% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无生成器错误；生成文件已重跑并对齐；`vitest run` **291 files / 2248 tests** 全绿；`vite build` 通过。
- `./release/Test-OmniExcluded.ps1` 通过；`./release/Test-ReleaseTools.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.76-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.75 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
