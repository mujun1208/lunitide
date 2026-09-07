# 当前任务自动装备与视频链接回归（2026-09-07）

后续补充：公共 MP4/MOV/WebM/MKV 文件直链已增加真实本地音轨识别与最多 3 帧画面采样，详见 [直链音画补强报告](video-direct-content-20260907.md)。本文的“字幕/简介”能力边界仍适用于分享页面，不能据此将新直链支持误报为所有平台视频已完整覆盖。

## 范围与结果

本轮保留“按用户当前任务自动选专家、技能与工具”的功能，不要求用户每次手工选技能。修正显式选择被自动意图覆盖、装备提示与实际限制不一致，以及视频错误页被当作字幕的问题。没有修改这些功能的页面外观，没有安装外部 MCP、调用真实付费模型、改写用户数据库或下载私人视频。

### 自动装备

- 根因：`namesForTurn` 在解析当前已挂载专家之前先匹配关键词；`composeExpertNames` 又独立解析一次。当前挂载专家的身份/技能、给模型的提示、MCP 限制可能不一致。
- 修复：当前引用专家优先，其次有效的单个显式挂载专家，再按当前任务自动匹配；旧的多个隐式挂载仍不并入。统一使用 `turnEquipmentFor` 解析。无挂载的 Excel/PPT 等任务继续自动匹配。
- 装备显示以前只读内置目录，现在读取当前解析出的真实技能绑定与 MCP 绑定。模型提示中“已连接 MCP”只列本轮权限允许且实际就绪的连接；未连接项继续明确标为未连接。月伴可用的就绪工具集合不因此收窄。
- 技能是模型根据目录匹配后通过 `skill.invoke` 执行，目录本身不代表已经执行。能力包提供已有工具/技能/MCP 的配置，不另造一条执行引擎；不因自动匹配而自动安装、伪造连接或绕过原有执行权限。
- 相关文件：`internal/app/chat_turn_equipment.go`、`chat_expert_compose.go`、`chat.go`。显式技能超过目录数量限制的修复由同轮 MCP/技能代理负责。

### 视频链接

- 根因：HTTP 非成功响应未检查；字幕 API 的错误 JSON 或 HTML 验证码页面会被去标签后当成字幕；字幕跳转最终域名未复核；原来两个抓取各自计时，字幕不可用原因被静默吞掉。
- 修复：页面与字幕均检查 HTTP 状态、字幕最终域名与协议，拒绝登录/验证码 HTML 和未知 JSON；已知 JSON 字幕数组/`body`、XML 字幕、VTT/SRT 按正文提取，不把序号/时间轴当对白。统一 30 秒总预算，字幕正文上限 32 KiB，标题/作者/简介分别有界，保留截断标记。字幕失败返回稳定原因，仍有简介时明确降级为简介分析。
- 给打字对话加入仅针对本轮视频链接的指令：先使用现有 `video.understand` 取公开内容，再依据实际返回的来源分析；明确的浏览器操作请求继续走原浏览器路由。网页/字幕是外部资料，不是指令。
- 相关文件：`internal/videounderstand/{detect,parse,understand}.go`、`internal/app/chat_video_instruction.go`、`chat.go`。

## 验证

- `go test ./internal/videounderstand -count=1`：通过。
- `go test ./internal/videounderstand ./internal/toolruntime -run 'Test(Caption|Understand|VideoUnderstand)' -count=1`：通过。
- 应用层 `TypedVideo`、`TypedAutomaticExpert`、`MountedExpertAndVisible`、`VideoInstruction`、`TurnEquipment`、`ComposeExpert`、`ExpertCompose`、`ExpertMcpHint`、`McpNameAllowed`，以及相关月伴工具/澄清回归：通过。
- 上述新增/相关应用层测试最终 `-race` 回归通过（53.961 秒）；视频解析/获取回归 `-race` 通过。`git diff --check` 通过。
- 新增 `caption_regression_test.go` 验证错误 JSON、HTML 验证码、错误内容类型、XML/VTT/SRT 正文、HTTP 429/403、字幕跨域跳转、取消/同一总时限和 UTF-8 截断。
- 新增 `chat_routing_execution_test.go` 经真实 `HandleStreaming` 和工具运行时验证：本轮输入→工具定义/任务指令→假模型发 `video.understand`→隔离抓取函数返回公开字幕/仅简介/HTTP 429→真实工具结果回传模型→流正常结束。测试不使用真实互联网视频。
- 同文件验证：无显式选择的 Excel 任务自动获得对应专家/技能目录，模型调用 `skill.invoke`，执行器收到用户原话并执行，结果返回下一次模型调用；显式专家优先、显示真实绑定、未连接/未授权 MCP 不标成可用。
- 工具调用测试使用假的模型决策及隔离数据，证明应用执行链路和回传正确，不代表已经测得真实模型选择准确率或所有远端网站的可用率。

## 必须保留的能力边界

支持识别 B 站、抖音、腾讯视频、YouTube 的允许域名及既有分享短链。能取得多少内容取决于公开页面：当前 B 站和 YouTube 解析器会尝试读取页面中公开的字幕地址；抖音、腾讯视频主要是公开标题/简介。需要登录、验证码、限流、地区限制或不公开的字幕，不绕过这些限制。

**平台分享页无公开字幕且没有公共视频文件直链时，仍只能简介分析。** 后续已补公共 MP4/MOV/WebM/MKV 的有界下载、本地音轨转写和抽样画面链路，详见 [直链音画补强报告](video-direct-content-20260907.md)。当前模型请求仍使用文本和抽样图片，不是原生视频输入；不能声称支持任意平台链接、全片逐帧理解或视频中全部声音的识别，也不能用标题推测冒充观看。

本轮没有调用真实外部 MCP；动态工具与权限门槛沿用现有 ready snapshot/工具 ACL 回归。首次外部安装、真实账号认证、公开网站当前抓取可达性仍需独立联调，不能据隔离测试给这些环节满分。
