# B03 电脑控制实施进度

日期：2026-09-06。范围：CPA-10 / CPA-11 / CPA-12。此文记录本批实际修改，不代表系统已达到 4.9 分或完成真实设备验收。

## 已完成

- `internal/ccapp/execution.go`：单服务桌面动作串行化、可取消排队、授权代次失效；配置变更与急停撤销活动操作。短锁不跨 OS 调用；急停先锁存，活动操作仍未退出时明确返回 pending。已提交的 OS 调用不承诺撤销。
- `guarded_host.go`、`runhost.go`、`service.go`：所有服务层键鼠、剪贴板、窗口、菜单与可访问性动作经过执行栅栏；循环按键、等待及验证延迟响应取消。修饰键释放作为清理允许执行；急停时已有挂起按键异步清理，避免阻塞急停响应。
- 目标进程：每次前台输入前复查，命名窗口聚焦后核对返回进程；前台未知时拒绝输入；窗口操作与应用退出再次查验实际匹配目标，枚举失败、无目标、目标进程未知均拒绝。
- 审计：先持久化 `audit_events` 意图，使用既有 `cc.operation.confirmed` 事件及 `phase=prepared/outcome=pending/approved/argsDigest/operationId` 区分语义。意图不记为 executed 电脑控制账本行。结束后写一条关联的 ledger 终态和 mirror；取消后使用独立 3 秒上下文保存回执，终态区分 stopped/blocked/failed/executed。
- 意图失败不进入 OS；回执失败不返回成功，若已经 dispatch 则返回“动作可能已发生，先核对后再决定”的错误。detail 超限返回明确错误，避免截断生成非法 JSON。
- `internal/app/m10_cc_handlers.go`：区分急停锁存但尚未结束、急停状态落盘失败、回执结果未知、授权变更。落盘失败时当前服务仍保持急停，getConfig 会呈现该锁存。
- 无数据库迁移、无生产用户数据库读写、无真实键鼠/剪贴板操作。

## 验证结果

- `go test ./internal/ccapp`：通过。
- `go test ./internal/ccapp ./internal/storage/sqlite -run 'TestExecution|^TestCc' -count=1`：通过；包含原有 SQLite CC 行数、状态及事务行为验证。
- `go test -race ./internal/ccapp -count=1`：通过（17.708 秒）。
- `go test ./internal/app ./internal/toolruntime -run 'Cc|Computer' -count=1`：通过。
- `git diff --check -- internal/ccapp internal/app/m10_cc_handlers.go`：通过，仅工作区换行转换提示。
- `execution_test.go` 新增 17 个正式测试。原审计的取消前仍写、急停后仍写、回执失败仍成功、指定黑名单窗口仍输入四个场景均改为正确行为回归；另覆盖 prepared 后取消终态、已写入后取消标记、前台变化、目标枚举失败、配置撤权、排队取消、重复按键中断、Host 阻塞、按键释放阻塞、存储失败保留锁存和非法 JSON 防护。

## 尚未由本批证明 / 后续事项

- 真正 Windows 桌面上的权限、UIPI、多显示器、窗口被用户并发切换、外部应用响应及长时间运行，仍需隔离桌面人工/自动验收。
- 执行栅栏位于 Host 调用边界；一个已进入的 native Host 方法及其内部 OS 工作会完成或自行失败。当前 Host 没有可中断接口，不声称能强制终止该 OS 方法。
- 前台复查与 OS 输入之间存在操作系统层面的窗口切换竞争；此批阻断了已复现的命名黑名单绕过，并增加即时复查，未引入 HWND 绑定输入的新平台契约。
- 已持久化 prepared 但进程崩溃或回执落盘失败后保留待核对意图；本批提供可关联证据与错误语义，尚未实现启动时自动对账和恢复界面。不得自动重放可能已发生的动作。
- 全项目回归由主任务统一执行。本批测试不包含真实设备副作用，也不能作为 4.9 分已达标声明。

## 命令输出交叉复核补修

- `internal/toolruntime/command_output.go` 将管道排水与外部进度回调解耦：每次执行最多 40 条、每条 400 字的队列，排水不等待回调；收尾给予最多 50 毫秒刷新正常进度，超过期限即结束等待。进程级单槽限制外部回调，避免连续命令遇到阻塞接收方时累积无法退出的回调。槽被占用时其他命令可能丢弃进度，最终输出仍按既有 64 KiB 上限返回。
- 进度回调 panic 在工作协程内隔离并释放预算。退出后丢弃队列；已经进入的外部回调无法被 Go 强制取消，最多保留一个挂起调用，其余不再排队等待。调用方必须同步共享状态；主任务同步调整聊天进度事件发射，避免进入未加锁的 thinking 缓冲区。
- 对 16 KiB 进度分段和 64 KiB 最终输出截断保留完整 UTF-8 边界，避免最后半个中文字符触发整段 GB18030 回退解码。
- 新增真实测试子进程只向 stdout 输出 2 MB：人为阻塞进度接收后，管道继续排水，`cmd.Wait` 返回，关闭在固定期限内返回；再执行 10 个收集器不新增挂起回调。另覆盖长中文最终结果/每段进度、回调 panic、预算释放及短命令的正常进度刷新。
- `go test -race ./internal/toolruntime -run 'Command|Decode|Output|Progress' -count=1` 通过（2.634 秒）。原有短命令进度与最终输出验收保留；补充异步回调的测试数据同步。
- 聊天到命令再到进度事件的 `TestCommandRunStreamsToolOutputEvents` 原有顺序断言保持，`-race` 通过（1.517 秒）。
- 限制：上层统一事件 emitter 自身阻塞、Windows Job Object 绑定失败后的进程树清理仍需后续处理；此改动只保证命令输出排水、命令等待与收集器关闭不依赖进度接收方，不能声称整个聊天流不可能被外部 emitter 阻塞。
