# Lunitide 0.4.48

0.4.47 之后的补口包：堵住月伴级联的两处真实漏口 —— 火山通话时回合结束的「完成冲洗」仍会二次朗读（叠声），以及暂停后只说「播放」被改写成搜「热门」而不是续播。其余 0.4.47 的三卡级联 / talk 形状适配 / 会议复读 / 首页唤醒拆除保持不变。用 0.4.47 及更早版本勾这两条不算数。

## 修复

- **火山叠声（完成冲洗路径）**：回合结束时的 `player.enqueue` 冲洗此前未经通话互斥，talk PCM 还在占用播放器时会把同一段回复再朗读一遍（两张嘴）。现在冲洗前统一走 `companionCascadeSpeechBlocked`：talk 存活或挂起时不再入队级联；仅工具交接置 `talkSuppressPlay` 后才放行工具结果朗读。互斥判定抽成纯函数并单测钉住。
- **暂停后续播被搜成「热门」**：`resolveMediaPlayArgs` 之前把空 query 一律补成「热门」，导致模型 `action=play` 不带歌名（暂停后继续）会去搜热门而不是按播放键续播。现在 `action=play` 空 query 保持为空（走媒体键续播），只有 `open_and_play`（首次播放、没点歌）才补「热门」。网易云 / QQ / 前台三条改写分支同源处理。
- **月伴会话提示**：音乐软件上下文注入改口——点名歌手/歌名 `query=歌名`；随机或没点歌 `query=热门`；暂停后继续或只说「播放」不换歌时 `action=play` 不带 query、不要 `computer.act` 找按钮。

## 不要指望本包做的

- 仍不改 `E:\GPT-SoVITS\start-api-cpu.bat`、9880、盘符，不动默认进程黑名单，不重写 `useCompanionMachine`
- 火山没有可用 realtime 时仍退级联，级联顶 **4.3**；本包只补叠声与续播两处，不声称对答如流、火山 5.0、主尺 4.80
- 本地克隆能力未变（灯诚实，引擎不因此上线）；会议仍是 WASAPI 环回 + 诚实标签，不保证一定收进腾讯/飞书系统声
- 打开桌面播放器仍依赖模型 + 前台窗口，不保证每次都进入播放
- 仍不把火山聋静默切 sherpa，不复活 MiniCPM-o / `omni.*`

## 验证

- Quality 等价门禁三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go vet ./...`、`go build ./...`、`go test -timeout 20m -count=1 ./...`、`generate:bridge` 无契约漂移、`typecheck`、`npm --prefix web test`（168 files / 1284 tests）、`npm --prefix web run build`
- 新增 / 更新单测：`companion_context_test.go`（空 query 续播 / `open_and_play` 仍搜热门 / netease 空 query 续播 / 会话注入含「不要带 query」）、`companionTalk.test.ts`（`companionCascadeSpeechBlocked` 真值表）、`CompanionStage.talkmutex.test.tsx`（通话存活时完成冲洗不入队级联）
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 会跑）
- 未做桌面 WebView 真机点选；单测绿 ≠ 对打过门。火山通话 / 汽水前台续播 / SoVITS `/docs` / WASAPI 能量仍需真机勾

## 安装包

- `release/out/Lunitide-Setup-0.4.48-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.47 升级
