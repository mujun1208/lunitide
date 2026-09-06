# 第二阶段 S01 / E09：浏览器设置、执行监管与网络边界

日期：2026-09-06。仅本地代码、受控假上游及隐藏浏览器测试；不使用录音设备、付费服务或用户已有浏览器页面。历史审计证据不改写。

## S01 浏览器设置与真实生效结果

- `br.settings.update` 必须携带 `expectedRevision`；缺失或旧版本返回 `BR_SETTINGS_CHANGED`。SQLite 迁移 `0136_br_settings_apply.sql` 为配置增加 `revision/apply_status/apply_error`，内容编辑始终递增 revision。
- 先事务保存 desired 配置和 `applying` 意图，再在事务外停止旧会话，最后保存 `applied/failed` 结果。停止失败保留原端点与连接时间，禁止把失败写成 disconnected；导航和新连接在配置未生效时拒绝。重启遇到 applying 明确标记失败并要求重试。
- 模式、路径、端口、白名单和私网策略变化均触发实际会话停止。网络代理先关闭，已有隧道立即失效；即使 CDP 停止失败，旧页面也不能继续使用旧网络连接。成功 ACK 丢失后只检查本次 owned context 是否已消失，不处理用户其他 context。
- 前端区分保存和生效，提供重试应用；冲突加载权威 revision，但保留用户路径输入。拒绝迟到的旧 bridge 加载结果；白名单按 origin 输入，路径、账号、查询参数不会误当来源规则。
- 同时最多 4 个自动化浏览器会话；owned Chrome/Edge 每次最多 15 分钟。页面文案说明此限制；实际进程已结束的持久会话在列表读取时核对并回收，退出引擎有明确 Close 钩子。
- 设置作用域明确：五种自动化连接模式共用本页策略；对话中普通网页由独立 `browser.open` 窗口使用固定 HTTPS/禁私网策略，不受本页开关影响。该独立窗口的 WebView2 接线和验证由根代理负责。

## E09 执行、终端与桥接输出

- `toolruntime command.run` 复用 `commandworker` 的 suspended → Job → resume 路径，移除启动后 best-effort 监管；Job 创建、配置、分配失败均不运行命令。允许经过上层授权的脚本显式使用 16 KiB 参数预算，签名 CommandSpec 默认参数限制仍为 1 KiB。
- 修复 commandworker 和终端的 Assign 失败遗留挂起进程；终端设置进程数/内存预算，正常退出也关闭 Job 与 ConPTY，旧退出或旧输入超时不会关闭同 ID 的新会话。
- 实际竞态测试触发过 `runtime.semasleep wait_failed errno=6` 致命崩溃：输出管道被 `os.NewFile` 包装后仍由 raw CloseHandle 关闭，其后 File finalizer 可能关闭复用句柄。已改为一次性转移句柄归属，由 `os.File.Close` 唯一关闭；正式测试在每次执行后创建新事件句柄并强制 GC，检查新句柄不受影响。
- commandworker 输出回调使用进程级 8 个投递槽、固定队列与 100 ms 结束等待；卡住或异常的接收方不能阻止进程树回收，结果明确返回输出不可用。无回调的浏览器 worker 不占投递槽。
- 终端输入等待最长 3 秒，可接受桥请求更短的取消期限；单会话只保留一个在途写入，超时关闭相同实例的进程。关闭不持输入写锁，真实 ConPTY 大输入阻塞测试确认能回收。
- 终端输出超预算明确提示、发布失败并回收进程；摘要使用流式 SHA256，不按输出分片无限积累摘要数组。终端 bridge 发事件不持 ownership 锁；卡住回调受全局 8 个槽和 250 ms 上限约束，失败后撤销相应终端。

## E09 浏览器真实网络与文件边界

- 新共享包 `internal/browsernetwork` 提供 loopback-only HTTP/HTTPS 代理：每个连接先验证完整 origin、端口和整组 DNS 答案，再只对已检查的字面 IP 拨号。重定向由浏览器发起下一次请求并重新校验，子资源、CONNECT、WebSocket 升级同样经过边界。
- 修复原 allowlist 的字符串前缀绕过，`example.com.evil`、不同协议/端口、账号字段均不能冒充来源。
- 代理限制 64 个请求、128 个连接、32 KiB 请求头、16 MiB 请求体、64 MiB 响应/隧道方向、HTTP 2 分钟和隧道 5 分钟；关闭代理回收空闲、活动及已 hijack 的连接。超限响应不能伪装完整响应。
- owned Chrome/Edge 使用独立哈希 profile、受限环境变量、Job 全树回收，禁 QUIC 和非代理 WebRTC UDP。扩展浏览器只有在实际 command line 可验证相同安全参数时才接入；无法证明则明确拒绝，并提示使用应用管理的模式。
- CDP version 响应最多 64 KiB，端点只能返回相同 127.0.0.1 端口；禁止环境代理继承、HTTP 重定向和任意 ws 地址。
- 文件统计/清理使用受管目录边界、拒绝链接/junction、2 秒/10 万项扫描预算；删除还使用固定目录句柄及 `os.Root.Remove`，防父目录替换越界。取消、读取、删除和审计错误均传播；清理审计使用真正 SHA256，修复原来忽略十进制 after_digest 违反数据库约束的问题。

## 原始验证证据

- `evidence/phase2-execution-core.jsonl`：brapp / browsernetwork / commandworker / terminalruntime / toolruntime 五包正式测试共 237 个 test/subtest pass、0 fail、退出 0。默认跳过两个显式 native 用例，已分别独立执行通过。
- `evidence/phase2-browser-settings.jsonl`：SQLite 和 App 设置/终端桥接定向 14 个 test/subtest pass，0 fail，退出 0。
- `evidence/phase2-browser-web.json`：BrowserPanel 12 项通过，0 fail。
- `evidence/phase2-execution-race.log`：commandworker / terminalruntime / browsernetwork / brapp 的定向竞态套件。
- `evidence/phase2-browser-native.log`：真实隐藏 Edge，仅本地伪上游；页面请求 2、重定向 1、被阻断私网请求 3、禁止上游命中 0、file fetch 被拒绝。用例 0.61 秒，退出 0。
- `evidence/phase2-browser-managed-native.log`：实际 LocalHost 创建受管浏览器及私有 context，确认全进程树停止耗时 92.6859 ms；用例 0.48 秒，退出 0。
- 独立 WebView2 的真实验证另见根代理 `phase2-webview-native.log`，不与以上 Edge 证据混计。

复现 native：设置 `LUNITIDE_BROWSER_TEST_EXECUTABLE` 为本机 Edge/Chrome 绝对路径后，分别运行 `go test ./internal/browsernetwork -run '^TestNativeBrowserProxy' -count=1 -v`、`go test ./internal/brapp -run '^TestNativeManagedBrowser' -count=1 -v`。常规测试不依赖安装浏览器。

## 下一项

S01/E09 本批源码已交回根做全局集成。继续根分配的 E07/E08 知识来源主动刷新、失效标记、版本化索引，以及解析/解压/PDF预算与失败恢复；不据本批测试宣称该后续工作已完成。
