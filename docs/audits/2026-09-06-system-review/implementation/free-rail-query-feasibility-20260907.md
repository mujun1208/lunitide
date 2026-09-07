# 免费火车票查询 MCP 可行性核实（2026-09-07）

结论：确实存在可复用的免费开源查询候选。未取得铁路正式集成 API 文档，不能推导为没有免费开源 MCP。本轮未引入该候选：它仍依赖 HTML 初始化，且存在网络异常时启动等待的已知边界，不满足用户“不靠临时抓网页”及稳定接入的要求。

## 一手来源与准确版本

- 维护者为 **Joooook**，仓库是 [Joooook/12306-mcp](https://github.com/Joooook/12306-mcp)，不是 Jooooot。许可为 MIT。
- 本次从 npm registry 直接读取的最新发布版为 **12306-mcp@0.3.10**，发布时间 **2026-07-30T14:24:37.839Z**，发布 gitHead 为 `2ea7a601a2af0e1cc48b8235e65fa11f1af55aaf`。[npm 包](https://www.npmjs.com/package/12306-mcp)、[确切版本元数据](https://registry.npmjs.org/12306-mcp/0.3.10)
- 维护记录显示 7 月 30 日修复动态查询 URL，7 月 31 日继续维护依赖元数据。可以确认近期有维护，不能仅凭提交活跃推断生产 SLA。[维护者提交记录](https://github.com/Joooook/12306-mcp/commits/main/)
- 固定版本的标准 stdio 启动命令为 `npx -y 12306-mcp@0.3.10`；可选 HTTP 模式增加 `--host 127.0.0.1 --port 8080`。本轮没有执行这些命令。[维护者使用说明](https://github.com/Joooook/12306-mcp#readme)

## 真实查询方式与能力范围

本次除查看仓库外，还将 npm 发布包下载到内存，读取 `package/build/index.js`，没有安装或执行其中代码。

- 暴露八个查询工具：当前日期、城市内车站、城市代码、站名代码、电报码、直达余票、中转余票、列车经停。查询支持 `format=json` 等输出，未发现购票、支付、提交订单或账号登录的写入工具，也没有 API Key 配置要求。
- 余票查询读取 12306 网站的结构化 JSON 结果，不是解析余票表格 HTML；查询时取得匿名 Cookie。
- **初始化仍依赖 HTML**：从首页定位站点 JavaScript 文件，从直达查询页和中转页提取当前接口路径，再调用对应查询接口。因此不能描述为完全不抓 HTML、铁路正式承诺的纯 JSON API。[维护者实现源码](https://github.com/Joooook/12306-mcp/blob/main/src/index.ts)、[维护者原理说明](https://github.com/Joooook/12306-mcp/blob/main/docs/principle.md)

## 接入前必须解决或验证的边界

1. 发布源码中的 axios/fetch 调用未设置超时或取消信号；站点和接口路径读取位于 MCP 握手前的顶层初始化。由此推断，初始化网络卡住时可能一直无法完成握手，页面变化也可能直接启动失败。本次没有通过实际网络故障测试测定其行为，不能把源码风险写成已实测故障。
2. npm 0.3.10 将 Inspector 2.0.0 放在运行依赖中，该依赖要求 Node ≥22.19.0；Commander 14.0.3 要求 Node ≥20。主分支随后才将 Inspector 移至开发依赖并标注自身 Node ≥18，不能将这一主分支说明直接套在旧发布包上。[Inspector 元数据](https://registry.npmjs.org/@modelcontextprotocol%2Finspector/2.0.0)、[Commander 元数据](https://registry.npmjs.org/commander/14.0.3)、[主分支 package.json](https://github.com/Joooook/12306-mcp/blob/main/package.json)
3. 这是开源客户端和网站现用接口的组合，不等同于铁路向第三方承诺的稳定商用接口；匿名访问成功也不意味着永久免费、无限频次或任何网络都可用。
4. 如果后续用户接受上述数据依赖，可先在隔离目录固定版本验证 MCP 初始化、工具列表、站点解析、一次公开余票查询、JSON 字段与查询时刻、断网/超时/取消及页面变更失败。通过后才考虑可选接入；不应先装入用户配置再排查挂起。

本轮维持源代码冻结。**没有安装此 MCP、没有修改用户配置、没有访问 12306 实站、没有使用账号或验证实时余票。** 当前结论是“存在候选，但因具体约束未实施”，不是“免费火车票结构化查询绝对不可实现”。
