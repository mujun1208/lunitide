# MCP 连接异常复盘（2026-09-07 用户第二轮反馈）

## 已核实的现场情况

只读检查现有 SQLite 配置与本轮日志，未安装、启用或删除用户的 MCP，未读取凭据正文。
现存 14 个未删除端点中 10 个 ready，Everything、Google Drive、DuckDuckGo、MarkItDown 为 degraded。
四个失败端点都未保存“握手验证后的启动锁”。这个字段在握手与工具目录验证成功后才写入，因此没有显示锁定版本 **不等于包名解析一定失败**。
旧版本丢弃子进程 stderr，Registry.Probe 又将原因替换成通用健康检查失败；Health 最后只返回状态和耗时，因此截图不足以还原所有失败的唯一现场原因。

| 项目 | 代码与上游核查结论 | 本轮处理 | 仍需实际环境验证 |
|---|---|---|---|
| Everything | stdio 客户端声称跳过通知，实际上只读第一行并当作响应；上游初始化和工作期间会产生通知，导致合法通知被误判为 ID 不匹配。此代码缺陷已用独立子进程复现，尚无原现场报文证明所有失败只有此原因。 | 持续读取匹配响应，正确跳过日志/进度/目录变化通知；回答 ping，未声明的 roots/sampling 等请求明确返回不支持。保留响应匹配、帧大小、通知次数/总量和时间上限。 | 用户重新连接并实际调用目标工具。Everything 是上游协议演示服务，未实现的客户端可选能力仍不宣称支持。 |
| DuckDuckGo | npm 0.1.2 固定依赖 SDK 0.6.0；其最新协议为 2024-11-05。原客户端仅允许返回 2025-03-26，拒绝合法的旧版本协商。 | 支持已知 stdio 工具协议 2024-11-05、2025-03-26、2025-06-18、2025-11-25；未知版本仍拒绝。 | 成功连接后，搜索还要求网络可访问 DuckDuckGo；本轮未执行搜索，也未承诺网站网络可用。 |
| Google Drive | 真实配置仍带旧版工作目录参数，没有 GDRIVE_CREDENTIALS_PATH 凭据绑定。上游启动需要 OAuth token 文件，普通目录不提供认证；它不是无需配置即可用的服务器。 | 缺少 OAuth 文件绑定时在启动前返回具体提示。已安装卡片说明授权与目录区别并提供官方说明；不启动交互授权浏览器，不伪造凭据、不自动删除原配置。 | 用户先完成上游 OAuth，再在“凭据”填写 GDRIVE_CREDENTIALS_PATH，随后重新连接。上游是归档参考服务器，保留已有功能但不承诺上游维护。 |
| MarkItDown | 当前已改为真实 PyPI 包 markitdown-mcp，核查版本 0.0.1a4，需 Python >=3.10。首次依赖准备与前端 15/20 秒、后端 20 秒握手预算存在错位；历史具体失败原因因 stderr 未保留，不能断言只是慢。 | 设置连接的总探测预算 70 秒、首次握手最多 60 秒；前端和 Host/Engine 上限均为 80 秒。补充包/网络/Python 依赖/凭据/协议等受控诊断，卡片给出 Python 要求与官方配置入口。 | 重新连接以获得实际失败类别；本轮未替用户安装依赖或执行文件转换。若仍失败，按新诊断检查运行环境或软件源。 |

## 共享链路改动

- 交叉复核补修发送侧取消：原先先同步写入请求，再开始等待取消；已握手服务停止读取 stdin 时，大请求能堵住写管道。现在 write、flush、响应读取由同一个受取消管理的任务执行，取消后关闭隔离进程和管道，等待任务退出才返回，避免遗留 reader/writer。
- Registry 保留错误链，不再将所有探测错误抹平为一个健康检查失败。
- `mcp.health` 与 `mcp.list` 增加受控 `diagnosticCode`、`diagnosticMessage`，启动回填和重新连接均刷新，恢复后清除。只在内存保存类别和固定文案，原始 stderr 不落盘、不进入 UI。
- 子进程 stderr 使用至多 8 KiB 滚动窗口归类，不阻塞协议。HTTP 软件源 404 与版本格式错误区分显示。
- 重新连接返回 degraded 时页面显示原因，不再把“连接异常 · 2660ms”当作成功通知；按钮在等待期间显示“连接中…”。
- 只有 MCP 配置/启用/健康检查使用更长预算。停用、读取清单和正常工具调用保持原预算；取消从上层继续传递，没有后台无限重试或重装逻辑。
- 界面使用现有主题样式与固定文案，没有重设黑白主题或修改其他模块布局。

## 验证

- `go test -race -json ./internal/mcp ./internal/mcp6 ./internal/bridge -count=1 -timeout 5m`：97 个测试/子测试通过，1 个需显式配置的真实远程服务测试跳过；三个包均通过，无 DATA RACE。原始记录：`evidence/user-regressions-20260907/mcp-race.jsonl`。
- 回归覆盖真实独立子进程中的通知交错、服务器请求、连续列工具与调用、四个已知版本、未知版本拒绝、通知洪泛有界、取消不脱离父上下文。另用真实子进程在初始化后停止读取 stdin，发送 2 MiB 工具参数，分别验证 200ms deadline 与显式取消及时返回、后续新连接可继续调用。
- `go test ./internal/app -run 'TestMcp|TestLegacyGoogleDrive' -count=1 -timeout 5m` 通过，涵盖临时 SQLite 中健康诊断→列表→恢复清除、Google Drive 启动前凭据判断，以及既有生命周期测试。
- `go test ./cmd/engine -run '^TestMcp' -count=1 -timeout 5m` 通过。
- MCP 页面及连接超时桥接测试 23 项通过：模拟 60 秒准备后仍接收结果，不自动重复请求；原调用/清单上限保持；凭据和失败文案正确展示。
- 前端类型检查通过。没有将以上结果当作四个用户实例已全部连接成功的证据。

## 上游依据

- [MCP 生命周期与版本协商](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle)
- [Everything 官方参考服务器](https://github.com/modelcontextprotocol/servers/tree/main/src/everything)
- [DuckDuckGo 上游](https://github.com/zhsama/duckduckgo-mcp-server)，并只读核查 npm 发布包及其固定 SDK 0.6.0。
- [Google Drive 上游授权说明](https://github.com/modelcontextprotocol/servers-archived/tree/main/src/gdrive)
- [Microsoft MarkItDown MCP](https://github.com/microsoft/markitdown/tree/main/packages/markitdown-mcp)，并核查 [PyPI 元数据](https://pypi.org/pypi/markitdown-mcp/json)。
