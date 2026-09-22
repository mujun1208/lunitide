---
name: lunitide-ship
description: Lunitide 仓库从「复盘未提交改动」一路到「GitHub Latest 可下载」的固定发布流程。当用户说复盘复核未提交的改动、全量充分测试、开始 CI 全链路、go 测试、同步树保持干净、发布签名保持最新版本、推 push、删除多余的版本和安装包、确定 GitHub 上是最新的 latest 时使用。Use for the full Lunitide review → gate → sign → publish → cleanup pipeline on Windows.
---

# Lunitide 发布流水线

一条固定路径，十个阶段，顺序不能换。每个阶段有明确的通过条件；不通过就停在该阶段修，不要跳到下一阶段。

仓库：`E:\Trae-Work-Projects\lunitide`（Windows / PowerShell）。远端：`mujun1208/lunitide`。

**先读这一段再动手**：本仓库经常有**另一个会话并发在改文件**。签名构建同时要求「树干净」和「构建期间源文件不变」，所以只要检测到并发活动，阶段 6 必须走隔离工作树，否则会反复失败。详见 [references/troubleshooting.md](references/troubleshooting.md)。

---

## 阶段 0 · 现状勘察（只读）

```powershell
cd E:\Trae-Work-Projects\lunitide
git status -sb
git diff --stat; git diff --cached --stat
git log --oneline -5
"VERSION=$((Get-Content VERSION -Raw).Trim())"
git rev-list --left-right --count origin/HEAD...HEAD
gh release list --limit 10
```

同时判断并发：对每个未提交文件看修改时间，和当前时间比。

```powershell
git status --porcelain | ForEach-Object {
  $p = $_.Substring(3).Trim('"')
  if (Test-Path -LiteralPath $p) { "{0}  {1}" -f (Get-Item $p).LastWriteTime.ToString('HH:mm:ss'), $p }
}
"now: $((Get-Date).ToString('HH:mm:ss'))"
```

**产出**：一份状态摘要——未提交文件清单、当前版本、远端最新 release、是否存在并发写入。并发写入的文件**不属于本次发布范围**，不要替别人提交，除非用户明确说了（历史上用户会说「他在写设计文档不影响」）。

---

## 阶段 1 · 复盘复核未提交改动

这一阶段是**读和想**，不是改。

1. **逐文件读全文 diff**，不能只看 `--stat`：
   ```powershell
   git diff -- <path>
   ```
2. 每个改动问三件事：
   - 这次改动的**意图是否完整落地**？（改了读取侧，写入侧改了吗？改了中文，英文改了吗？）
   - **同类文件有没有漏改**？用 grep 找同一模式的其他调用点，不要凭记忆。
   - **生成物要不要重跑**？前端契约和产品编目都是生成的，源变了就得重生成。
3. **周边同类扫描**。这是历史上抓到最多真问题的一步：改了某个 lint/类型/命名问题，就在全仓搜同一形状，一次改干净。
4. **生成物一致性**（秒级，先跑）：
   ```powershell
   npm --prefix web run verify:bridge
   npm --prefix web run verify:catalog
   ```

**通过条件**：能用一段话说清「这批改动做了什么、边界在哪、有哪些同类问题一并修了」。说不清就继续读。

---

## 阶段 2 · 修正

按阶段 1 的结论改。原则：

- 改完立刻重跑对应的窄闸门（改 Go 就 `go vet ./...`，改前端就 `typecheck`），不要攒到阶段 3 才发现。
- 生成物**永远不手改**，改源再 `npm --prefix web run generate:bridge` / `generate:catalog`。
- 不顺手扩大范围。看到不属于本批的问题，记下来告诉用户，不要夹带。

---

## 阶段 3 · 全量本地闸门

完整命令表、顺序理由和环境变量在 [references/gates.md](references/gates.md)。摘要（按最快失败优先排序）：

```powershell
$env:CGO_ENABLED = '0'
$env:GOEXPERIMENT = 'nogreenteagc'   # 必须：Go 1.26.6 的 GC 会让覆盖率 run 崩

npm --prefix web run verify:bridge
npm --prefix web run verify:catalog
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
go vet ./...
go build ./...
./release/Check-Coverage.ps1 -Floor 51 -Timeout 35m
golangci-lint run ./...
govulncheck ./...
npm --prefix web audit --audit-level=moderate
./release/Test-OmniExcluded.ps1
git diff --exit-code -- web/src/generated/bridge.ts internal/bridge/schema_generated.go internal/contract/schema_generated_test.go web/src/generated/productCatalog.ts internal/producthub/generated/catalog.go
```

长任务（`Check-Coverage.ps1` 几十分钟）用 `block_until_ms: 0` 后台跑，然后**循环轮询到出结果**，不要中途改成阻塞式然后放弃。

**通过条件**：以上全绿。任何一条红就回阶段 2，不许带病进入发布。

---

## 阶段 4 · 版本与发布说明

1. 决定版本号。规模自检：只修 bug / 文档 → patch；有新能力或大面积重构 → minor；破坏性变更 → major。
   ```powershell
   Set-Content -NoNewline VERSION "0.5.6`n"
   ```
   VERSION 是**唯一真源**，tag 必须是 `v<VERSION>`，CI 会硬校验（`publish-update-feed.yml` 第一步）。

2. 写 `release/notes-<version>.md`。写给**用户**看，不是给贡献者看：
   - 开头一段说清「装上上一版之后你会遇到什么，这一版把它收口了」。
   - 按能力分节，每节说用户现在能做什么。
   - 明确写出**不改什么**（历史惯例：不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方）。
   - 明确写出**兼容与覆盖安装口径**，以及「不得覆盖更早 tag 的 digest」。
   - 不吹「100%」，不承诺未验的评分。

---

## 阶段 5 · 提交 + 打 tag

```powershell
git add <明确的文件清单>        # 永远不要 git add . / -A
git commit -F .tmp-msg.txt      # 提交信息写成陈述句，说清行为变化
Remove-Item .tmp-msg.txt -Force

git tag v0.5.6 HEAD
"tag -> $(git rev-list -n1 v0.5.6)"
"VERSION at tag: $(git show v0.5.6:VERSION)"
"dirty: $(@(git status --porcelain).Count)"
```

**通过条件**：tag 指向的提交里的 VERSION 与 tag 名一致，且树干净（或只剩并发会话那几个文件）。

---

## 阶段 6 · 签名构建

`Build-Release.ps1 -RequireSignature` 有**三个硬约束**，都会直接抛错：

| 约束 | 抛错文案 | 含义 |
|---|---|---|
| 树必须干净 | `Signed releases require a clean committed checkout` | `git status --porcelain` 必须为空（含未跟踪文件） |
| 构建期源文件不变 | `Source inputs changed during the build` | 构建前后对全部跟踪文件做 SHA-256 指纹，变了就判为混合候选并丢弃 |
| 输出目录必须空 | `OutputRoot must be empty...` | `release/out` 不能有残留 |

所以：

**情形 A — 树干净、无并发**，就地构建：

```powershell
cd E:\Trae-Work-Projects\lunitide
./release/Build-Release.ps1 -RequireSignature 2>&1 | Tee-Object .tmp-relbuild.log | Select-Object -Last 22
"RELEASE_BUILD=$LASTEXITCODE"
```

**情形 B — 有并发会话在写文件**（历史上的常态），开隔离工作树：

```powershell
$ver = (Get-Content VERSION -Raw).Trim()
$wt  = "E:\lunitide-rel-$($ver -replace '\.','')"

git worktree add --detach $wt "v$ver"
cmd /c mklink /J "$wt\web\node_modules" "E:\Trae-Work-Projects\lunitide\web\node_modules"

# .release-cache 是 gitignored，不在工作树里 → signtool 解析会失败。
# 把主仓缓存里的 x64 bin 动态加进 PATH（不要硬编码 SDK 版本号）。
$st = Get-ChildItem 'E:\Trae-Work-Projects\lunitide\.release-cache\sdk-buildtools' -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
      Where-Object { $_.DirectoryName -match '\\x64$' } | Sort-Object FullName -Descending | Select-Object -First 1
if (-not $st) { throw 'cached signtool not found' }

cd $wt
$env:PATH = "$($st.DirectoryName);$env:PATH"
"signtool: $((Get-Command signtool.exe).Source)"
"dirty=$(@(git status --porcelain).Count)  commit=$(git rev-parse --short HEAD)  VERSION=$((Get-Content VERSION -Raw).Trim())"

./release/Build-Release.ps1 -RequireSignature 2>&1 | Tee-Object "E:\lunitide-rel-build.log" | Select-Object -Last 22
"RELEASE_BUILD=$LASTEXITCODE"
```

签名环境变量 `LUNITIDE_SIGN_COMMAND` / `LUNITIDE_SIGNER_THUMBPRINT` 已经装在用户级环境里（`release/Install-LocalCodeSigning.ps1` 设置的）。缺失时报 `Production signing is required`。

**产出三件资产**，缺一不可：

```
release/out/Lunitide-Setup-<version>-x64.exe
release/out/latest.json
release/out/SHA256SUMS.txt
```

---

## 阶段 7 · 发布为 Latest

用**本机签名产物**创建 release（CI 那条路只有在本机不可用时才用，它签不出生产证书）：

```powershell
cd $wt   # 或主仓，取决于阶段 6 走哪条
$ver = (Get-Content VERSION -Raw).Trim()
$out = 'release/out'
foreach ($f in @("$out/Lunitide-Setup-$ver-x64.exe", "$out/latest.json", "$out/SHA256SUMS.txt")) {
  if (-not (Test-Path -LiteralPath $f -PathType Leaf)) { throw "missing $f" }
}
gh auth status
gh release create "v$ver" `
  "$out/Lunitide-Setup-$ver-x64.exe" "$out/latest.json" "$out/SHA256SUMS.txt" `
  --title "Lunitide $ver" --notes-file "release/notes-$ver.md" --latest
"RELEASE_CREATE=$LASTEXITCODE"
```

---

## 阶段 8 · 推送

```powershell
cd E:\Trae-Work-Projects\lunitide
git push origin HEAD:refs/heads/<branch>
git push origin "refs/tags/v$ver"
```

推 tag 会触发 `publish-update-feed.yml`。该工作流发现 release 已存在时，**保留本机签名资产**并只把它标为 Latest（正是我们想要的）；release 不存在时它才自己构建一个未签名的候选。

`github.com:443` 连不上但 `api.github.com` 正常时（本机反复出现过），走本技能目录下的 Git Data API 重放脚本：

```powershell
# 先干跑确认要重放哪些提交，再去掉 -WhatIf 实际执行
powershell -NoProfile -ExecutionPolicy Bypass -File `
  .cursor\skills\lunitide-ship\scripts\Push-ViaGitDataApi.ps1 -Branch <branch> -WhatIf
```

脚本逐层重建 blob / tree / commit 并在每一步校验 SHA，不一致就中止——绝不会推出与本地不同的历史。

---

## 阶段 9 · 验证 GitHub Latest

这一步决定用户后面能不能被正确提示下载，必须实测，不能只看 `gh release create` 退出码。

```powershell
$ver = (Get-Content VERSION -Raw).Trim()
gh release view --json tagName,isLatest,assets |
  ConvertFrom-Json | ForEach-Object { "tag=$($_.tagName) latest=$($_.isLatest) assets=$(($_.assets.name) -join ', ')" }

# 应用内更新读的就是这个 URL，必须真的能取到
$feed = curl.exe -sL "https://github.com/mujun1208/lunitide/releases/latest/download/latest.json" | ConvertFrom-Json
"feed version: $($feed.version)   expected: $ver"
```

**通过条件**：`tagName = v<VERSION>`、`isLatest = true`、三个资产齐备、`latest.json` 的 version 等于 VERSION，且其中安装包哈希与 `SHA256SUMS.txt` 一致。

---

## 阶段 10 · 清理

```powershell
# 1) 多余的 GitHub release：按版本号排序，保留最新 3 个
#    ForEach-Object { $_ } 不能省：PS 5.1 的 ConvertFrom-Json 把 JSON 数组当单个
#    对象输出，直接进 Sort-Object 会让排序键拿到整个数组而报类型转换错误。
$all = gh release list --limit 100 --json tagName | ConvertFrom-Json | ForEach-Object { $_ } |
       Sort-Object {
         if ($_.tagName -match '(\d+)\.(\d+)\.(\d+)') { [version]"$($Matches[1]).$($Matches[2]).$($Matches[3])" }
         else { [version]'0.0.0' }
       } -Descending
$keep = @($all | Select-Object -First 3 -ExpandProperty tagName)
$del  = @($all | Select-Object -Skip 3 -ExpandProperty tagName)
"keep  : $($keep -join ', ')"
"delete: $(if ($del.Count) { $del -join ', ' } else { '(none)' })"
# ↑ 先把这份清单报给用户确认，确认后再执行下面这行
foreach ($t in $del) { gh release delete $t --yes; "  $t -> exit $LASTEXITCODE" }

# 2) 本地构建产物和工作树
Remove-Item -Recurse -Force release\out, release\out-* -ErrorAction SilentlyContinue
if ($wt -and (Test-Path $wt)) { git worktree remove --force $wt }
git worktree prune

# 只删自己这轮写的临时日志。不要用裸 .tmp-* ——那会连带删掉用户长期保留的
# .tmp-edge-visual 浏览器配置目录。
Remove-Item -Force .tmp-*.log, .tmp-*.txt, .tmp-*.ps1, E:\lunitide-rel-*.log -ErrorAction SilentlyContinue

# 3) 确认干净
git status --porcelain; git worktree list
```

**绝对不要删 git tag。** 发布说明明确要求不得扰动早期 tag 的 digest——删 release 不动 tag，两者是分开的。

删 release 属于不可逆的远端写操作，**执行前把具体要删的 tag 列表报给用户**。

---

## 硬约束汇总

| 禁止 | 原因 |
|---|---|
| `git add .` / `git add -A` | 会把构建产物、`.tmp-*`、别人的未完成文件一起提交 |
| 手改 `web/src/generated/*` 或 `internal/*/generated/*` | 生成物；CI 有漂移闸门会直接红 |
| 删任何 git tag | 会扰动早期 release 的 digest |
| 用 CI 产物覆盖本机签名产物 | 托管 runner 没有生产证书，签不出来 |
| tag 名与 VERSION 不一致 | 工作流第一步硬校验，必红 |
| 长任务超时后放弃 | 覆盖率闸门本来就要几十分钟，要轮询到底 |

## 失败目录

历史上真实发生过的故障及修法，逐条列在 [references/troubleshooting.md](references/troubleshooting.md)。遇到报错先查这张表，不要从零排查。
