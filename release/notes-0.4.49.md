# Lunitide 0.4.49

一次全模块安全收口包。基于对全系统（对话、语音月伴、同事聊天、会议纪要、后台设置、电脑控制、技能/插件/MCP、专家/资产、存储与权限审计）的复盘，把 7 个 P0（真实可被触发、会越权或落错数据）逐条收口，每条先补一个失败测试再改。功能面不变，行为面只在"手动审批模式"下更严。0.4.48 的月伴级联两处补口保持不变。

## 修复（P0）

- **P0-1 引擎直派工具绕过审批闸门**：MCP（`mcp.call` / `mcp_<ep>_*`）、市场安装器（`mcp.install` / `plugin.install`）、`browser.act` 的点击/输入这几类由 chat 循环自己派发、从不经过 `toolruntime` 审批闸门的工具，此前在任何模式下都直接执行。现在新增 `ungatedEngineToolDenied`：手动审批模式下这些一律以 `ok:false` 退回并说明改哪个模式；语音月伴轮里安装器与浏览器点击直接拒绝（没有可确认的界面）。只读分支（`mcp.search`、快照、导航、读页）不拦。
- **P0-2 电脑控制未进 mutating 闸门**：`ccapp` 只对自身 high/critical 风险停手，点击/输入算 medium，`toolruntime` 的 mutating 判定又从没列过 `cc.*`，于是审批模式下模型可无提示地点鼠标、敲键盘、粘贴。现在 `ccapp` 暴露 `ToolChangesMachine`，`toolruntime` 通过 `ccToolChangesMachine` 把 `cc.*` 与 `computer.act`（先 `MapComputerAct` 再判定，映射失败按会改机器处理）纳入 mutating；观察类（截图、读 UI、列窗口）保持不拦。
- **P0-3 子代理/同事代理越权**：子代理此前无论父轮什么模式都以 `FullAccess` 跑工具——从审批轮里派生一个子代理就能让写操作无人审批地落地。现在子代理继承父轮 mode（`subagentToolMode`，未知一律退到最严的审批），并把父轮 mode 一路带到 `runSubagentTool`。同事代理（入站消息驱动、无人盯）降到 auto-edit 且显式禁 `command.run`——一条外部消息够不到 shell，也进不了整盘全访问。
- **P0-4 审计日志可被改删**：`audit_events` 是事后复原"助手被允许做了什么"的唯一依据，却是本 schema 里唯一还能被任意改写/删除的证据表。迁移 `0110` 给它加上 `trg_audit_append_only` / `trg_audit_nodelete`（`M10-AUD-001`），与其余证据表（`trg_sao_*`、`trg_art_immutable_*`、`trg_evd_*`）一致。同时删掉一处事务外、吞掉自身错误的 best-effort 审计写入——审计行要么随调用方事务落地，要么就不落，不再静默丢行。
- **P0-5 IM 入站密钥明文落库**：飞书/企微/钉钉入站的 `inbound_app_secret` 此前明文存在 SQLite 列里。现在保存时经 DPAPI 凭据通道（`secret.Service`）落地，库里只留标记；`LookupSecret` 内部解密回填给入站 worker，Settings 视图仍只显示"已配置"而读不到明文。老装机的明文在首次读取时一次性懒迁移进 DPAPI 并把列抹成标记。
- **P0-6 会议相同 final 重复入库**：识别器把同一条 final 冲洗两次（重复冲洗或重启重放）此前会各自成行，读者看到重复句。现在 `resolveAgainstLastSegment` 统一把重复/增长 final 折叠进上一段；并新增 `LastSegment` 只读队尾，取代每次 append 全量 `ListSegments`——长会议 append 从二次方降为常量。
- **P0-7 `media.play` 可开任意本地程序**：地址由模型选，此前直接交给系统协议处理器，`file:` / `ms-*` / UNC 都成了"播放"外衣下的本地启动，且经 `cmd /c start` 还能被查询串注入。现在 `validateMediaURL` 只放行 http/https 且必须带主机名，`mediaOpenArgv` 走 `rundll32 url.dll,FileProtocolHandler`（Windows）无 shell 打开；本包自建的搜索 URL 逐字节保持不变。

## 不要指望本包做的

- 不动默认执行模式（仍是完全访问）；本包只让"手动审批"这一模式真的能拦住上面几类，不改产品默认
- 不动月伴 SoVITS/9880/盘符、进程黑名单、`useCompanionMachine`；火山无 realtime 仍退级联，主尺不因本包上移
- 不重写 chat.go / SessionPage / store 的体量（架构分层是后续 P1/P2，不在本包）
- 不声称对答如流、火山 5.0、本地克隆上线；会议仍是 WASAPI 环回 + 诚实标签

## 验证

- 两轮全链路（`CGO_ENABLED=0`）：`go vet ./...`、`go build ./...`、全量 `go test ./...`（含变更包与历史易抖的 voice 包 `-count=1` 复跑）、`verify:bridge` 无契约漂移、`typecheck`、`npm --prefix web test`（168 files / 1284 tests，两轮）、`npm --prefix web run build`
- 新增失败先行单测：`ungated_tools_test.go`、`cc_approval_test.go`、`unattended_authority_test.go`、`audit_append_only_test.go`、`media_url_test.go`、`append_dedup_test.go`、`internal/imapp/secret_store_test.go`（含 `imSecretRef().Validate()` 钉死 DPAPI Ref 合法性、明文永不落库、老明文懒迁移）
- 未做桌面 WebView 真机点选；单测绿 ≠ 对打过门。手动审批模式下的 MCP/浏览器拦截、DPAPI 入站密钥、会议真机收音仍需真机勾

## 安装包

- `release/out/Lunitide-Setup-0.4.49-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.48 升级
