# 失败目录

历史上真实发生过的故障。遇到报错先在这里对号，不要从零排查。

---

## 1. `Source inputs changed during the build`

**症状**：签名构建跑到最后抛出 `Source inputs changed during the build; discard this mixed candidate and build again from a stable checkout`，重跑还是一样。

**原因**：`Build-Release.ps1` 在构建前后对**全部跟踪文件**做 SHA-256 指纹（`Get-ReleaseSourceSnapshot`）。另一个会话在构建期间改了任意一个跟踪文件，指纹就变了，候选被判为「混合」并丢弃。跟改的是不是发布相关文件无关。

**修法**：开隔离工作树（SKILL.md 阶段 6 情形 B）。共享检出下重试注定反复失败。

**顺带**：失败留下的 `release/out` 是混合候选，安全检查本身要求丢弃，下次构建前必须删掉。

---

## 2. `Signed releases require a clean committed checkout`

**症状**：加了 `-RequireSignature` 就直接抛这句，构建都没开始。

**原因**：签名构建要求 `git status --porcelain --untracked-files=normal` 为空——**包含未跟踪文件**。

曾经误判为「签名构建对树是否干净不敏感，只要求构建期间源文件不变」。这是错的，两个约束同时成立。

**修法**：要么提交/暂存，要么走工作树（工作树 checkout 天然干净）。别人未完成的文件不要替他提交，除非用户明确同意。

---

## 3. `OutputRoot must be empty for a single immutable candidate`

**原因**：`release/out` 里有上次构建的残留。设计上每个候选一次性、不可变。

**修法**：`Remove-Item -Recurse -Force release\out`，或用 `-OutputRoot release/out-<version>` 换个目录保留旧证据。

---

## 4. 工作树里 signtool 找不到

**症状**：主仓能签名成功，同一个提交在工作树里报 `signtool.exe not found`。

**原因**：`Resolve-SignTool.ps1` 的查找顺序是 PATH → Windows Kits → `$PSScriptRoot\..\.release-cache\sdk-buildtools`。本机没装 Windows Kits，靠的是 `.release-cache` 里的 NuGet SDK BuildTools 副本；而 `.release-cache` 是 gitignored，**不会出现在工作树里**。

**修法**：把主仓缓存里的 x64 bin 动态加进 PATH。不要硬编码 SDK 版本号，也不要试图把 `.release-cache` junction 进工作树（历史上被拦下过）。

```powershell
$st = Get-ChildItem '<repo>\.release-cache\sdk-buildtools' -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
      Where-Object { $_.DirectoryName -match '\\x64$' } | Sort-Object FullName -Descending | Select-Object -First 1
$env:PATH = "$($st.DirectoryName);$env:PATH"
```

---

## 5. 工作树里缺 node_modules

**原因**：`web/node_modules` 不在版本控制里。

**修法**：junction 过去，比重新 `npm ci` 快得多。

```powershell
cmd /c mklink /J "<worktree>\web\node_modules" "<repo>\web\node_modules"
```

WebView2 的 nupkg 会在工作树里重新下载（`.release-cache` 同样缺），需要网络，属正常。

---

## 6. 全新 tag 的 release 没被发布

**症状**：推了新 tag，`publish-update-feed` 工作流显示成功，但没有 release。

**原因**（已修）：该步骤用 `gh release view $tag` 判断 release 是否存在。release 不存在时 `gh` 返回非零，而 Actions 会在每个 pwsh step 末尾追加 `exit $LASTEXITCODE`——于是整个 step 以失败码结束，后面 `if: steps.release.outputs.exists != 'true'` 的构建和发布步骤全被跳过。每一个全新 tag 都发不出任何东西。

**修法**：`else` 分支里显式 `$global:LASTEXITCODE = 0`。已在 `publish-update-feed.yml` 落地，别再改回去。

---

## 7. `github.com:443` 不可达但 `api.github.com` 正常

**症状**：`git push` 报 `Failed to connect to github.com:443 after N ms`，同时 `curl https://api.github.com/rate_limit` 秒回 200。无代理环境变量。这是主机级的时段性阻断，不是仓库或凭证问题。

**先确认**（连通性会来回抖，值得先重试一次）：

```powershell
curl.exe -s -o NUL --max-time 20 -w "github.com https=%{http_code} time=%{time_total}s`n" https://github.com/mujun1208/lunitide
curl.exe -s -o NUL --max-time 15 -w "api.github.com https=%{http_code} time=%{time_total}s`n" https://api.github.com/rate_limit
Test-NetConnection -ComputerName github.com -Port 443 -WarningAction SilentlyContinue |
  Select-Object RemoteAddress, TcpTestSucceeded
```

**修法**：通了就正常 `git push`。仍不通先查**用户级代理**——浏览器能开 github.com 而 git 不能，说明浏览器走了本机代理而 git 不认 IE 设置：

```powershell
Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' |
  Select-Object ProxyEnable, ProxyServer
# ProxyEnable=1 时把 ProxyServer（形如 127.0.0.1:15715）挂给 git：
$env:HTTPS_PROXY = "http://<ProxyServer>"; $env:HTTP_PROXY = $env:HTTPS_PROXY
```

代理通了就一切照常（push、fetch、force push 全恢复）。注意 `gh auth refresh` 的设备流回调端点在 `github.com` 上，被封锁时即使浏览器完成了授权，token 也换不回来。代理也没有时才走本技能目录下的 Git Data API 重放脚本（先 `-WhatIf` 干跑）：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File `
  .cursor\skills\lunitide-ship\scripts\Push-ViaGitDataApi.ps1 -Branch <branch> -WhatIf
```

脚本的安全设计：远端 HEAD 必须是本地提交的祖先，否则拒绝（不会造成分叉）；blob / tree / commit 三层逐一比对 SHA，任一不符立即中止，绝不移动 ref。

**注意**：`git push` 报 `Everything up-to-date` 时，先 `git fetch` 再比 SHA。本地 `ahead N` 可能是过期的远端跟踪信息，不代表真有未推送提交。

---

## 8. Git Data API 重放出来的 commit SHA 不一致

**症状**：blob 和 tree 都精确命中，只有 commit SHA 不同。

**原因**：提交信息的字节不一致。`git log -1 --format=%B` 经 PowerShell 管道（尤其 `Out-String`）会把 LF 重写成 CRLF，改变了 commit 对象，SHA 自然变。

**修法**：从 commit 对象的**原始字节**里取消息——`git cat-file commit <sha>` 重定向到文件，读字节，找第一个 `\n\n` 作为头/体边界。脚本已经这么做了。

同一个坑也适用于读 blob 内容：任何二进制安全的读取都要走 `cmd /c "git cat-file ... > file"` 重定向，不能走管道。

---

## 9. 生成的编目文件在 Windows 上报假漂移

**症状**：CI 的漂移闸门对 `productCatalog.ts` / `catalog.go` 报红，但内容看起来没变。

**原因**：生成器写 LF，Windows 检出成 CRLF。

**修法**：已在 `.gitattributes` 钉死：

```
web/src/generated/productCatalog.ts text eol=lf
internal/producthub/generated/catalog.go text eol=lf
```

生成器读文件时也统一 `.replace(/\r\n/g, '\n')` 再做正则匹配，否则 `SETTINGS_CATEGORIES` 这类解析在 Windows 上会失败。

---

## 10. vitest 留下僵尸进程

**症状**：测试跑完了，下一次构建或测试莫名失败／卡住。

**修法**：

```powershell
npm --prefix web run test:stop:dry   # 先看要杀什么
npm --prefix web run test:stop
```

平时用 `npm --prefix web run test:bounded`（带超时护栏），而不是裸 `vitest run`。

---

## 11. `ConvertFrom-Json` 后排序报类型转换错误

**症状**：`Sort-Object : 无法将 System.Object[] 类型的值转换为 System.Version`，但后续命令看起来又「跑对了」。

**原因**：PowerShell 5.1 的 `ConvertFrom-Json` 把 JSON **数组当作单个对象**输出，不逐元素送进管道。于是 `Sort-Object { $_.tagName }` 里的 `$_` 是整个数组，`$_.tagName` 走成员枚举返回 `Object[]`。排序键抛错后 Sort-Object 仍按原序放行元素，所以结果「看着对」——实际没排序。

**修法**：在 `ConvertFrom-Json` 后面加一段 `ForEach-Object { $_ }` 强制展开。

```powershell
gh release list --limit 100 --json tagName | ConvertFrom-Json | ForEach-Object { $_ } | Sort-Object ...
```

单个对象的 `--json`（如 `gh release view`）不受影响。

---

## 12. coverage 合并 profile 带 BOM，`go tool cover` 拒读

**症状**：`Check-Coverage.ps1` 里 Go 测试全过，最后 `go tool cover -func` 报 profile 首行不是 `mode: set`。

**原因**：TRAE shell 预设 `$PSDefaultParameterValues['Set-Content:Encoding']='utf8'`，PowerShell 5.1 的 utf8 必带 BOM → 合并后的 coverage.out 首行变 `EF BB BF mode: set`。三个源 profile（Go 工具链写的）都是干净的，纯粹是脚本写出环节被 shell 默认值污染。

**修法**（已落地）：脚本用 `[IO.File]::WriteAllLines` + `UTF8Encoding($false)` 显式无 BOM 写入，self-test 断言首三字节不是 `EF BB BF`。教训：脚本里任何 `Set-Content`/`Out-File` 都不要依赖 shell 默认编码。

---

## 13. sqlite 测试偶发 `fatal error: fault`（Xmalloc）

**症状**：`internal/storage/sqlite` 全包测试随机崩，panic 栈落在 `modernc.org/libc.Xmalloc` → `_renameParseSql` → `_renameTableFunc`，即 migration 的 `ALTER TABLE RENAME` 解析路径。同机 4 跑 2 过 2 崩。

**原因**：modernc.org/sqlite v1.46.1 + libc v1.67.6 在 Go 1.26.6 Windows 下的内存违例，三方库问题，`GOEXPERIMENT=nogreenteagc` 已设仍偶发。非本仓代码引入。

**修法**：重跑（大概率过）。Check-Coverage.ps1 已带 fault 自动重试。失败输出必须存文件全文再分析——`Select-Object -Last N` 会截掉 panic 头导致漏判。

---

## 14. filter-repo `--replace-text` 不清洗二进制

**症状**：历史改写后全库扫描，旧版 `release/bin/*.exe` 仍含本机工作区路径（形如 `E:/<work>/<repo>` 的 Go 构建源码路径），blob SHA 改写前后完全相同。

**原因**：git-filter-repo 对前 8KB 含零字节的 blob（二进制）跳过 replace-text（源码明确注释）。Go 构建的 exe 内嵌源码路径，文本规则够不着。

**修法**：含泄漏的二进制走 `--path <目录> --invert-paths` 从全历史删除。验证必须扫**全对象库**而不是只看 tip：`git cat-file --batch-all-objects --batch` 流式扫描，且 ASCII + UTF-16LE/BE 双编码都查。改写前先 `git clone --mirror` 留回滚备份，改写后 `diff --stat` 新旧 tip 确认只有预期变化。

---

## 15. signtool 报的哈希和 SHA256SUMS 不一致（虚惊）

**症状**：构建日志里 `Hash of file (sha256)` 与 `SHA256SUMS.txt`/`latest.json` 里的 sha256 不同，疑似发布事故。

**原因**：signtool verify 报的是**签名作用域摘要**（不含证书表），`Get-FileHash` 是全文件字节。Authenticode 签名过的 exe 两个值必然不同。

**修法**：无需处理。校验和一律以 `Get-FileHash` 口径为准；要确证发布资产，从 GitHub 下载回来重算哈希对账。

---

## 16. npm audit 在 npmmirror 镜像源下必红

**症状**：`npm --prefix web audit --audit-level=moderate` 退出 1，报 `404 Not Found - POST https://registry.npmmirror.com/-/npm/v1/security/advisories/bulk - [NOT_IMPLEMENTED]`；换官方源后可能报真实漏洞（如 fast-uri 3.0.0–3.1.7 的 GHSA-hrr3-gc8f-f4qj，moderate）。

**原因**：npmmirror 只做包镜像，不实现 npm 的 security/advisories 端点，audit 请求直接 404——这既可能是"跑不了"，也可能掩盖当天新发布的通告。本机默认 registry 配的是 npmmirror。

**修法**：加 `--registry=https://registry.npmjs.org` 重跑（本机代理已开，可达）。若报真实漏洞：`npm --prefix web audit fix --registry=https://registry.npmjs.org`（锁文件最小升级，确认只动 package-lock.json 预期行数），然后重跑 typecheck / test / build / 生成物漂移四项闸门。不要为过闸门调 `--audit-level`。

---

## 17. 改了 bridge 处理器调用链后覆盖率必红（compiled hops 过期）

**症状**：`Check-Coverage.ps1` 的 rest 批次报 `--- FAIL: TestCompiledHopsMatchTheSource`（internal/producthub），diff 显示 compiled 表里某方法（如 chat.start）的 Steps 与 live 源码解析出的调用链不一致。

**原因**：`internal/producthub/call_index_compiled.go` 是从产品源码固化生成的调用链表（诊断报告用），任何改动 `internal/app` 处理器函数体内调用序列的提交都会让它过期。verify:bridge / verify:catalog 不覆盖这个生成物——阶段 1 只查前端两个生成物会漏。

**修法**：`$env:LUNITIDE_WRITE_CALLS='1'; go test ./internal/producthub/ -run TestWriteCompiledCallIndex -count=1` 重新生成，确认 diff 只动预期方法的行，`go test ./internal/producthub/` 转绿后重跑覆盖率闸门。凡是改了 handleXxx 函数体的提交，阶段 1 就该带上这一步。

---

## 清理时的两个红线

1. **删 release 不删 tag。** 发布说明明确不得扰动早期 tag 的 digest。`gh release delete <tag> --yes` 只删 release 和资产。
2. **清理命令范围要窄。** 历史上一条过宽的 `Remove-Item` 把不该删的一起扫了。只删自己这次产生的东西，列清单给用户确认后再执行远端删除。
