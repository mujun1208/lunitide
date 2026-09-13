# Lunitide 0.4.81

把项目管理收成软件工厂主路径：需求 → 方案 → 库表核齐 → 接口确认 → 开发 → 测试 → 集成 → 打包/同步。工作台按阶段出生成条、库表编辑、工作板、发布目录同步；新确认必须有 ≥32 字节附件。社区技能目录扩到 68 包。复盘补上注册表门禁、接口清单同步、测试播种和只读写入。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称工厂已 100% 验收，也不声称 Pi / Claude / OpenCode / 三家 GUI 已接入。

## 1. 软件工厂主路径

- 阶段顺序锁定为工厂，不再是「聊天 + 九张空卡片」。方案阶段仍是 10 份交付物，不砍成 8。
- 库表只走 SQLite。开发任务硬门禁：库表 `ready` 且接口阶段已确认；完成必须回写摘要并勾选自测。
- 同步是复制到用户另选的目录，不是 `rootPath`，也不是外部生产发布。收据写在 `{dest}/.lunitide-sync.json` 和 `{root}/.lunitide/sync-receipt.json`。
- 新批准不能只绑模版；已批准的旧模版卡仍祖父兼容。清单是主数据，工作板是投影。`lastRunKind` 仍表示执行器。
- 不自动 git。不把 Agent Hub 焊进 SessionPage。不用 M6 当接口平台。

## 2. 复盘修正

- 注册表门禁改为「已选根且目录树 ready」，不再等开发检查清单。库表/接口阶段就能配。
- 接口板同步优先已有条目的 `interface_list`；绑定的 OpenAPI 会解析成 `Ixxx` 行，不再被空解析挡住或被旧 `api_list` 盖掉。
- 进入测试阶段时按接口板 + 开发板播种。`board.put` 只能改处理字段，不能改标题/方法/路径/operationId。
- 集成打开不再吞掉测试板加载失败。工厂写入拒绝已关闭项目；`schema.put` 没有根目录会要求先选根。
- 外脑线程使用项目 `rootPath`。方案阶段 `api_list` / 接口阶段 `interface_list` / 业务流程清单 / 运维 `req_task_list` 按清单编辑。
- 同步拒绝目标在项目根之内（含子目录）以及 Windows 大小写同一路径。`test.run` 必须先有根目录；单元测试必须写记录。
- 新批准清单必须是可解析 JSON。工作台可完成/自测/记录测试/退回，打开接口或测试条不再跳去开发。
- 空接口清单晋级必须勾选「无接口任务」。空库表不能保存。坏访谈/坏规范清单不再装成生命周期错误。

## 3. 社区技能与周报连带

- 产品目录 68 个社区包（含 trailofbits、LambdaTest、DuckDB、vercel 补充包、caveman、mcp-builder、skill-doctor、awesome 索引）。新包默认不自动安装。
- 不拷贝 anthropics 受限 docx/xlsx/pptx/pdf 技能。不整仓搬 388 个 alirezarezvani 技能。
- 办公预览 / 周报 PDF 必须见到 `%PDF`、完全访问不再二次审批：沿用 0.4.80，本版未改办公生成路径。覆盖率套件里 `internal/officeapp`、`internal/officestudio` 已再跑过。

## 4. 明确不做

- 不把工厂验收标成 100%。不自动合并到 `main`。不接 API Key，不打月汐模型账本。
- 后继 6 家适配器、三家官方 GUI、本机未装 CLI 的真机联调、用 WebView2 去点官方窗口。

## 验证

- Go：覆盖率闸 **59.7% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 通过；`vitest run` **312 files / 2405 tests** 全绿。生成文件已重跑并对齐。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。未对生产 `%LocalAppData%\Lunitide` 启引擎。未做工作台浏览器实点。

## 安装包

- `release/out/Lunitide-Setup-0.4.81-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.80 升级。`release/out` 只保留本版本。
