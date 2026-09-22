# 全量本地闸门

与 `.github/workflows/quality.yml` 和 `release-candidate.yml` 一一对应。本地跑全绿，CI 才不会在推送后才暴露问题。

## 环境变量（先设，否则闸门会误红）

```powershell
$env:CGO_ENABLED  = '0'
$env:GOEXPERIMENT = 'nogreenteagc'
```

`nogreenteagc` 不是可选项。Go 1.26.6 的 Green Tea GC 会让覆盖率 run 以 `fatal error: index out of range in tryDeferToSpanScan` 崩掉；CI 里同样设了这个逃生开关。race 闸门还额外要 `GODEBUG=gcshrinkstackoff=1`。

## 顺序（最快失败优先）

| # | 命令 | 量级 | 管什么 |
|---|---|---|---|
| 1 | `npm --prefix web run verify:bridge` | 秒 | Bridge / RPC 契约生成物是否陈旧 |
| 2 | `npm --prefix web run verify:catalog` | 秒 | 产品页编目是否与 Page union / 设置导航 / office 菜单 / 媒体契约同步 |
| 3 | `npm --prefix web run typecheck` | 十几秒 | 渲染层类型 |
| 4 | `npm --prefix web test` | 分钟 | 渲染层单测（vitest） |
| 5 | `npm --prefix web run build` | 分钟 | 渲染层产物能不能出来 |
| 6 | `go vet ./...` | 分钟 | Go 静态检查 |
| 7 | `go build ./...` | 分钟 | 无 CGO 能否编译 |
| 8 | `./release/Check-Coverage.ps1 -Floor 51 -Timeout 35m` | **几十分钟** | Go 全量测试 + 覆盖率棘轮（下限 51） |
| 9 | `golangci-lint run ./...` | 分钟 | govet / ineffassign / staticcheck / unused，零告警 |
| 10 | `govulncheck ./...` | 分钟 | 可达的已知漏洞 |
| 11 | `npm --prefix web audit --audit-level=moderate` | 秒 | 渲染层依赖 |
| 12 | `./release/Test-OmniExcluded.ps1` | 秒 | 安装包里不得含 MiniCPM-o / Comni |
| 13 | 生成物漂移终检（见下） | 秒 | 跑完所有生成步骤后树是否仍干净 |
| 14 | `./release/Check-Race.ps1 -Timeout 120m -AppTimeout 60m` | **一小时级** | 竞态检测（CGO=1），本地可交给 CI |

## 固定版本的工具

版本是钉死的，不要用 `@latest`——新版本会加检查，让绿树无预警变红。

```powershell
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
```

## 生成物漂移终检

```powershell
git diff --exit-code -- `
  web/src/generated/bridge.ts `
  internal/bridge/schema_generated.go `
  internal/contract/schema_generated_test.go `
  web/src/generated/productCatalog.ts `
  internal/producthub/generated/catalog.go
```

这五个文件是生成的。`.gitattributes` 已对后两个强制 `text eol=lf`，否则 Windows 上的 CRLF 会造成假漂移。

## 长任务怎么跑

`Check-Coverage.ps1` 和 `Check-Race.ps1` 属于几十分钟到一小时的任务。

```powershell
./release/Check-Coverage.ps1 -Floor 51 -Timeout 35m 2>&1 |
  Tee-Object .tmp-gocover.log | Select-Object -Last 30
"COVER=$LASTEXITCODE"
```

用 `block_until_ms: 0` 放后台，然后每约 3 分钟轮询一次直到出结果。每轮报进度（哪些包过了、有没有失败）。**不要**在轮询超时后改成阻塞式然后宣告放弃。

## vitest 僵尸进程

vitest 偶尔留下 node 进程占着文件锁，表现为下一次构建莫名失败。

```powershell
npm --prefix web run test:bounded   # 带超时护栏的跑法，等价于根目录 npm run test:web
npm --prefix web run test:stop      # 清僵尸；先看清单用 test:stop:dry
```
