# Lunitide 0.4.78

把首页侧栏收成 Kimi / Trae 那样的干净入口，办公预览按页工作，周报类文件不再因为只有提示文字而空白。对话里点了「完全访问」后，不再为桌面路径二次弹审批。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称模型办公升级计划 T01–T20 已落地，也不声称无 LibreOffice 就能把 Word/PPT/Excel 正式交付。

## 1. 首页侧栏

- 去掉左侧月亮 Logo 和「月汐 / LUNITIDE」字标；「新对话」成为第一项并回到首页。
- 侧栏内容上提，中间月亮、极光和「今天想聊什么？」保留。

## 2. 办公工作台与周报预览

- PPT 只显示当前页，左侧页轨带缩略摘要，底部可翻页；定位不再用会抖窗口的 `scrollIntoView`。
- 参考附件可以点进主预览，不再从文件栏里消失。
- 预览可放大；对话绑定在同会话刷新时不再整页重挂。
- 会话产物里的 PDF 小于 4MB 时嵌入预览。周报 PDF / 表格 / 图片若只有「请用本机软件打开」提示，工作区仍显示这句话，不再空白或误报格式无效。

## 3. 完全访问

- 输入区已经是「完全访问」时，`office.generate`、桌面周报和 `workspace.write` 不再二次要审批。手动审批模式仍拦桌面输出。

## 4. Agent 调度台

- 文案改为三件事一键开始；不可用适配器在页内提示，不再 `alert`。
- Kimi 做 PPT 结束却没有 `.pptx` 时，详情页如实说没有文稿。

## 5. 对话与工作区

- 对话框和附件预览聚焦不再带动页面滚动。
- Mermaid 放大、产物检查器图标栏、工作区关闭按钮与办公预览放大对齐。

## 验证

- Go：覆盖率闸 **59.5% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无生成器错误；生成文件已重跑并对齐；`vitest run` **299 files / 2322 tests** 全绿；`vite build` 通过。
- `./release/Test-OmniExcluded.ps1` 通过；`./release/Test-ReleaseTools.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.78-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.77 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
