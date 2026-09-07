# ADR-019：MCP 标准远程传输与旧服务兼容

日期：2026-09-07。状态：已实现，离线集成验证通过；外部服务需账号及网络验收。

## 原因

原远程适配器仅支持产品早期约定的 GET /tools、GET /tools/<name>，不能直接连接现在常见的标准 MCP HTTPS 服务。需要凭据的预置又在保存前探测，导致用户得不到可配置凭据的端点。旧市场还有包名不存在及把令牌当启动位置参数的问题。

## 决定

增加 Streamable HTTP 适配器：initialize → initialized → tools/list → tools/call。支持 JSON 和 SSE 返回、目录分页、服务分配的会话标识、版本协商（2025-11-25、2025-06-18、2025-03-26）。不声明 sampling、elicitation、后台任务等尚未实现的能力。旧 GET 端点继续使用原调用及固定重试策略，不修改 M5 的只读调用语义。

标准工具 POST 不自动重发。超时、连接断开、回包丢失均不能证明外部操作没执行；返回失败或结果不确定，后续由用户意图和服务幂等能力决定重试。已过期会话返回明确错误，不暗中重放。

沿用原 TLS 校验、初始目标主机限制、禁止重定向、4 MiB 响应上限及调用时能力指纹核验。目录最多 16 页、512 个工具，重复游标拒绝。服务端错误只保留结构化代码，不把错误正文或凭据带回模型。标准会话在凭据租约结束前关闭。

远程凭据默认使用 Bearer；兼容需要查询参数的官方服务时，配置仅保存 token/key/api_key={{credential}} 这一公开占位符，实际令牌在请求租约内代入。拒绝将明文令牌、URL 用户信息、碎片及任意查询参数保存为端点，查询凭据的网络错误隐藏目标 URL。此版本不支持 OAuth 自动登录和任意自定义认证头。

mcp.add 新增可选 configureOnly，先持久化禁用配置，再由主机凭据窗口配置、用户连接；旧调用者行为不变。多项环境变量逐项保存时使用上一项返回的新安全版本。无需数据库迁移；旧端点 ID、能力包引用及撤销记录保留。

聊天发现返回实际参数 schema；长工具名使用稳定别名，在当前获准目录中唯一解析后调用原工具。按任务筛选不能删除已连接的 MCP；专家/同事的显式范围约束仍保留，并在搜索限额之前筛选范围。

## 验证与限制

真实本地 TLS 服务覆盖 JSON/SSE、分页、401、过期会话、取消、响应丢失、错误 ID、重定向和过大响应；生产 gateway 验证能力变化时不调用工具。真实 SQLite 测试覆盖先配置后连接及卸载重装。22 项前端测试覆盖配置、多个环境变量、丢失确认后的重试、连接和删除。

一次官方 Hugging Face 公共目录探测在本机网络阶段超时（DNS 返回的地址未建立连接），不能作为外部服务已通的证据。公共网络测试显式选择启用，不代替本地必过回归。账号权限、付费接口开通、外部服务可用性尚须验收。

## 官方依据

- [MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)
- [MCP 生命周期](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle)
- [Hugging Face 官方 MCP](https://huggingface.co/docs/hub/en/agents-mcp)
- [聚合火车查询及 MCP 接入](https://www.juhe.cn/docs/api/id/817)
- [Linear MCP 与令牌认证](https://linear.app/docs/mcp)
- [Neon 官方 MCP](https://github.com/neondatabase/mcp-server-neon)
