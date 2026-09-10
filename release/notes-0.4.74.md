# Lunitide 0.4.74

把 0.4.73 之后已验证的 S1 衔接、控件台、OCR 路由、文件操作和电脑控制指名优先打进正式版本。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称邮件、共享日历或在线 48 场景已通。

## 1. 衔接与操作诚实

- 会话连续性、操作列表/取消/恢复、用量展示与失败文案按收据说话，不发明节省百分比。
- 知识删除、连接器撤回、商业目录不标 ready、IM 附件 `pending_external` 保持诚实门。
- 调度默认仍写 JSON；SQLite 导入/切换只走测试路径，生产不自动 Cutover。

## 2. 控件、OCR、文件与 PDF

- Widget 八类真实渲染，状态经 `widget.update` 持久化（计时器可暂停恢复）。
- OCR：已配置可用的供应商优先，否则本地兜底；设置页可查看/切换路由。
- `data.process` / `image.batch` / `files.plan|apply` 走本机文件操作；PDF 拆合是有损文本重绘，表单/签名文件拒绝。

## 3. 电脑控制与浏览器

- `computer.act` 先 observe 再按唯一 `name=` / `id=` 点，禁止空点或有名字还猜像素。
- 短序列可用 `action=run` + `steps`（2–5 步，同一套审计/限流/风险门）。
- 模型不调用时自动 `observe`，不再截月伴前台去点别的软件。
- `browser.act` 仍只用 snapshot ref，不猜 CSS 或坐标。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；覆盖率闸 **58.7% ≥ 51%**。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无生成器错误；`vitest run` **288 files / 2173 tests** 全绿；`vite build` 通过。
- `./release/Test-OmniExcluded.ps1` 通过；`./release/Test-ReleaseTools.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.74-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.73 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
