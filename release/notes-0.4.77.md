# Lunitide 0.4.77

把周报试用真正落到可打开的 Office 文件，并把对话输入区回执收成一行。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称邮件、共享日历、Presenton 进生产主链，或无 LibreOffice 也能把 Word/PPT/Excel 正式交付。

## 1. 周报与 office.generate

- `office.generate` 接受模型误发的 `docx.gen` / `excel.gen` / `pptx.gen` 参数形状（`path`/`blocks`/`sheets`/`kind=report`），先改写成 `{name,spec}`，不放宽全局 `decodePayload`。
- 文件已经存档但检查记录失败时，会话仍写入产物副本并给出中文警告，不再把已生成的周报吞掉。
- 后端与前端产物卡都认 `office.generate`；试用提示和周报/纪要/文档/演示目录改为优先 `office.generate`，本轮仍有 `*.gen` 时也可使用。
- 不把 `docx.gen` 加回 L2-ask 工具表。

## 2. 对话输入区

- Token 与工具回执收进发送行旁的一行小字；发明的「下一步建议」芯片不再出现。
- 可取消操作的「停止」留在可见行上，不藏进被 CSS 关掉的 details。
- 产物卡用「N个文件已更改」标题，点文件名打开。

## 3. 办公工作台与图表

- PPT 预览按幻灯片分页，页轨放在左侧文件栏；简报改成一行摘要加可选芯片。
- Mermaid 预览限高，点击放大查看；跟读暂停时不再 `scrollTo`。

## 验证

- Go：`go test ./...` 覆盖率闸 **59.4% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无生成器错误；生成文件已重跑并对齐；`vitest run` **292 files / 2258 tests** 全绿；`vite build` 通过。
- `./release/Test-OmniExcluded.ps1` 通过；`./release/Test-ReleaseTools.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.77-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.76 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
