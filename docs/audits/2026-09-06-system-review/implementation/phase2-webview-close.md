# 隔离 WebView COM / 关闭流程交叉复核

本轮只修改 BrowserHost 的两个接口 getter、私有窗口关闭顺序与创建取消回收，未改第三方 module cache、未重写全库 COM。

## 诊断归因

原 `phase2-webview-native.log` 的两行 `?` 实际不是乱码，也不是已证实的 HeapFree 失败。对 go-com v1.5.0 的独立临时副本分别在 HeapFree 异常分支和 AddToScope 未识别分支加类型诊断，原生产流程连续运行三次得到：

- `COM_SCOPE_UNSUPPORTED value=**wv2.ICoreWebView2`
- `COM_SCOPE_UNSUPPORTED value=**wv2.ICoreWebView2Settings`
- 没有 `COM_HEAP_FREE` 异常行。

`go-webview2` 的 GetCoreWebView2 / GetSettings 将输出参数的二级指针传给 `com.AddToScope`，而 go-com 仅识别 COM 对象本身，因而在 scope.go:102 打印 `?`。BrowserHost 原来已经手工 Release 这两个结果，因此不能据此认定接口泄漏。现在局部 getter 调用相同 ABI 并明确沿用手工 Release 所有权，避免错误的自动 scope 尝试。

此外，go-com impl.go:233–235 确有“HeapFree 成功仍检查残留 LastError”的旧库误判条件，但本次真实日志未走该分支，没有以清零错误码或过滤 stderr 的方式隐藏错误。[HeapFree 官方返回值说明](https://learn.microsoft.com/en-us/windows/win32/api/heapapi/nf-heapapi-heapfree)。

环境 options 的字符串每次均由 CoTaskMemAlloc 分配独立 UTF-16 内存，调用者拥有 CoTaskMemFree 权利；已有正式测试验证独立分配、QueryInterface 引用和禁止 SSO / 改写。没有发现新环境 options 的 ABI 或字符串所有权错误。

`Chrome_WidgetWin_0 ... 1411` 是 Chromium 自有窗口类在运行时退出阶段报告不存在。该类不是本项目注册的窗口类；修复前再次运行三轮即未出现，说明它并非稳定复现的本项目内存损坏证据。修复后的最终九轮也未出现，但不能仅凭消失就证明运行时诊断的全部原因。

## 确定缺陷与修复

原流程由默认 WM_CLOSE 先 DestroyWindow，退出消息循环后才 Controller.Close，末尾又 DestroyWindow 同一 HWND。现 WM_CLOSE 先执行幂等 closeSTA：移除自有事件、关闭控制器、释放接口，最后销毁父窗口；重复关闭和 COM 重入不会重复 Release。清理后的指针全部置空，过程关闭后迟到的创建/网络校验回包不会再次导航。[控制器 Close 官方生命周期](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2controller?view=webview2-1.0.3856.49)。

新增取消启动回归实证：旧式已取消 context 仍触发环境创建，测试父进程退出后曾留下其私有浏览器子进程。该次受控父 PID 15128 / 子 PID 42404 已按精确父子归属及唯一测试目录核对后回收。

现在已取消 context 不启动环境；创建过程中取消会保留 STA 消息循环直到环境/控制器回调到达并回收借用结果。Manager 原有 5 秒关闭请求预算与“超时保留唯一 current host、不假报 closed”继续生效，避免重复启动产生无界在途主机。窗口过程回调地址也改为一次分配，避免每次打开注册一个不可回收的 syscall callback。

## 正式验证

- `go test -race ./internal/webviewhost ./internal/browserapp -count=1` 两包通过，含原有网络策略、所有权、Manager 超时以及新增 COM 重入清理测试。日志：`evidence/phase2-webview-close-race.log`。
- 使用原始第三方依赖、真实 WebView2Loader.dll 编译 `-race` 隐藏测试程序；三种场景各运行三轮，共 9 次执行全部通过：实际 flags / 拒绝代理导航与关闭、预取消不启动、创建中的取消等待回调。
- 正常关闭断言清理时父 HWND 仍有效、退出后 HWND 无效、所有自有接口均已置空；创建中取消实际命中 controller pending，完成耗时 183–198 ms。
- 最终受控进程 PID 5828，退出码 0，退出后没有它的存活直接子进程；stdout 无异常行，stderr 为空。没有访问外网或打开可见窗口。
- 原始日志：`evidence/phase2-webview-close-native.log`、`.stderr.log`、`-process.log`。诊断副本日志：`evidence/phase2-webview-com-before.log` 与 `phase2-webview-com-after.log`；最终结论以未替换依赖的正式 native 日志为准。
