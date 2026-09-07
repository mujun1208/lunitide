# 并行子任务与实时状态复核（2026-09-07）

范围：同一轮真实子任务并行、每个子任务的动态行、结果展开、失败与取消收尾，以及本轮发现的权限/模型凭据边界。没有调用外部付费模型、没有安装或启用用户 MCP，没有修改用户数据。

## 已确认的问题与修复

| 问题 | 可验证原因 | 本轮实现 |
| --- | --- | --- |
| 子任务已经并行执行，但用户看不出各自进度 | 原 future 只在主循环按工具顺序等待完成后返回摘要；排在后面的真实子任务没有独立进度事件。 | 保存真实 SubagentRun 后发送进度，主流使用互斥的 rawSend 记录真实 callID/argsDigest，阶段含启动、分析、查询/工具、终态。每个工具调用展示独立可展开行，没有虚构代理或模拟百分比。 |
| 长进度/结果 JSON 被截断，刷新后无法展开 | 普通工具摘要按字符/字节裁剪，不保证子任务 JSON 完整。 | 专用受限 JSON 编码，完整 payload 不超过 512 UTF8 字节（包括转义）。主线/过程记录代理在裁剪前提取实际 run ID、终态和任务标题并保存；展开按真实 ID 读取持久化摘要。 |
| 展开结果与桥接协议不一致 | 后端 join 实际返回 summary/digests，原 x-result 未声明这些字段。 | schema 保留兼容 observations，并准确声明 summary/digests；maxSummaryBytes 对齐后端 64KiB 上限，主线已统一生成桥接。 |
| 失败被写成完成、取消后运行状态不结束 | 原执行错误被放进报告，但仍调用 Complete；父 context 取消会让结束写库也失败。 | failed/cancelled/completed 分别落库；5 分钟子任务 deadline 实际传入执行 context；结束保存使用脱离父取消但限时 3 秒的 context。执行 panic 收敛为 failed，正常释放并发额度。 |
| 普通只读子任务可获得 Office 等写工具 | 原工具定义是排除少数名称的黑名单，新增 docx/excel/pptx/pdf 写工具漏出；父 full-access 会真的放行。 | 按 profile.ReadCaps 明确映射工具；执行时再次核验，不能靠伪造 allowed map 越界。普通浏览器仅授予 navigate/read/snapshot；命令限制观察操作，禁止 commit/add/stash、输出文件参数和创建分支。 |
| 显式授权能力可能被误删 | 专家和 implementer 原有文件/文档写能力依赖既有授权。 | 保留明确 ExpertWriteTools，并继续继承父轮次审批；implementer 在父可写模式下保留 workspace.write/edit，原有 allowlist 命令能力明确登记为 command.run，不再借只读工具列表取得写权限。 |
| 浏览器子任务声明可用但实际调错执行器 | browser.act 原先直接传入不实现该方法的 toolruntime。 | 接已有浏览器执行器，并保留父审批限制；没有新增自动安装动作。 |
| 子任务指定另一 provider 后可能鉴权失败 | subagentAdapter 将租约回调内的 credential 切片存出；实际 DPAPI 凭据回调结束会清零，后续子任务使用已清零切片。 | 子任务完整执行放入对应 provider 租约回调；不复制或延长密钥生命周期。指定 provider 不可用时返回明确失败，不静默用父 provider 调另一个 model。 |

## 已验证

- Go 专项使用隔离 SQLite、真实业务服务和受控适配器；没有真实外部模型费用。
- runStream 集成：第一个结果仍阻塞时，两个独立子任务已发出各自 thinking 进度；各 run ID 不同，argsDigest 与原工具匹配，终态 JSON 可解析。
- barrier 测试验证真实并发，而非仅比较执行耗时；同轮预启动上限回归保留。
- 执行失败、panic、父取消都保存实际终态，接着可以占满新的 4 个并发名额，证明旧 run 不占住额度。
- 实际 workspace 工具开始/返回事件、超长中文与转义 JSON 完整性、只读拒绝真实写入、显式专家写入及父审批保留、浏览器审批保护。
- 假租约模拟实际清零时机：子 provider 调用期间密钥有效，执行结束后清零；失效 provider 不调用父 provider。
- 前端 7 项交互回归：各子任务行独立、实时状态变更、超长保存摘要按实际 ID 展开、读取失败重试、历史过程卸载重建后仍可打开原子任务报告。未将原始 JSON 当产品正文展示。
- 证据：`evidence/feedback-20260907/subagent-go.jsonl`、`evidence/feedback-20260907/subagent-web.json`；前端 typecheck 通过。

## 实际能力与保留边界

- 同一模型工具批次预启动最多 3 个真实子任务；持久化服务每个 root 最多 4 个存活任务。是否拆分由模型实际产生 spawn 决定；没有调用时不会伪造代理行。
- 默认单子任务 token 设置 8192、允许配置最高 50000；这是现有请求/预算记录，不声称本轮新增了所有 provider 的严格总 token 成本上限。步骤上限按 profile 为 3、8 或 16，耗尽步骤后仍尝试生成一次结果报告。
- 每个子任务保存的最终摘要最多 2000 UTF8 字节；展开读取这个真实保存报告，不声称无限长原始模型输出。失败/取消行可查看保存的原因；现有 join 仅接受 completed，未伪装失败为成功来绕过它。
- 网页搜索/抓取子任务可以并发；浏览器仍复用产品现有浏览器通道，不声称各代理有独立浏览器或独立桌面。真实 WebView 外观、网络服务和所配模型实际响应留给本机验收。
