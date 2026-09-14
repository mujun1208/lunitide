# Lunitide 0.4.82

装好以后可以自动发现本机升级包，点提示就能覆盖安装，不必再卸载重装。工厂工作台收口继续落地；模型/办公工程层有第一批闸门，但不能当成整产品已验收。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称高端商用，也不声称 E / Q / D 已 100%。

## 1. 应用内覆盖升级

- 安装包本来就能装到已注册位置。本版起启动后会检查 `%LOCALAPPDATA%\Lunitide\updates\latest.json`（或 `LUNITIDE_UPDATE_DIR`），发现更高版本会顶栏提示，点「立即升级」走静默 `/S` 覆盖。
- 打包脚本会把 Setup 和摘要写进上述目录，并清掉旧 Setup。摘要对不上或文件缺失则失败，不会空跑。
- 当前已装的 0.4.81 及更早没有这套提示。这次升级请直接运行 `Lunitide-Setup-0.4.82-x64.exe`，不要先卸载。装上 0.4.82 之后，再打的新包才会弹窗。
- 本版没有 GitHub/CDN 自动下载。别的电脑要把 Setup + `latest.json` 放到同一更新目录。

## 2. 工厂与办公工程层

- 项目管理工厂：需求 → 方案 → 库表核齐 → 接口确认 → 开发 → 测试 → 集成 → 打包/同步。工作台脏标记、个人会话隔离、首发走通和同步收据跳过已补上。工厂 12/12 走通记在临时盘引擎，不是已装的旧安装包。
- 办公交付：`0157`–`0159` 进仓库和测试账本。正式导出看文件完整性、源内容和锁定事实；没有逐页渲染证据的独立 PDF 保持 partial，不标正式通过。
- 任务结果、模型适配面板、执行预算与恢复按工程闸门落地。不把 Agent Hub 焊进 SessionPage。不用 M6 当接口平台。SQLite only。

## 3. 明确不做

- 不把 `layers.E` 写成 done，不写 `live_qualified`，不写 `designerReviewed=1`。
- 不自动合并到 `main`。不接 API Key，不打月汐模型账本。
- 不把工厂 100% 和整产品 100% 混为一谈。

## 验证

- Go：覆盖率闸 **59.4% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`tsc --noEmit` 通过；`vitest run` **321 files / 2445 tests** 全绿；生成 Bridge 已重跑。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。未对生产 `%LocalAppData%\Lunitide` 启引擎。未做已装 0.4.82 的横幅实点。

## 安装包

- `release/out/Lunitide-Setup-0.4.82-x64.exe`
- `release/out/SHA256SUMS.txt`
- `release/out/latest.json`（给本机更新目录用，不进 SHA256SUMS）
- 从 0.4.81 升级：直接运行 Setup，不必卸载。`release/out` 只保留本版本。
