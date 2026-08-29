package app

// bundledWorkflowInjection is the OpenClaw/Hermes-style always-on skill
// pack: compact native-tool workflows that need no install, no MCP, and
// no extra schema. Injected on ordinary chat only (companion stays lean).
func bundledWorkflowInjection() string {
	return "\n\n[内置工作流] 开箱即用，直接调用已有工具，不必安装技能、不必 MCP：\n" +
		"- 调研：必须用 web.search（不要 web.fetch Bing/Google 首页）；后台检索供你引用，只有用户明确要看/打开网页时才需要工作区浏览器。完成后只给简短来源列表并结束，不要写任务过程长文。\n" +
		"- 改代码：workspace.search 定位 → workspace.read → workspace.edit 精确替换 → command.run 验证。\n" +
		"- 审查：读改动与周边代码，按 严重/建议 分级，引用 path:line。\n" +
		"- 文档：必须用 pptx.gen / docx.gen / excel.gen / pdf.gen 写入工作区并汇报路径。用户要放到桌面时加 desktop=true（path 用文件名即可，例如 半年财报.xlsx），禁止填 C:\\\\Users\\\\...\\\\Desktop 绝对路径，禁止 PowerPoint/Excel/Word COM、Python 拼 OOXML、command.run 复制到桌面——工具调用会被截断并在约 30 秒后报「无法执行」。做 PPT 禁止 ZipFile 改 XML。PPT 必须走九步流水线（思考→定义结构→写内容→web.search 收集素材→再思考→再收集素材→思考创作→写完整页→最后 pptx.gen），禁止跳步生成空页或只有深色底没有文字的文件；pptx.gen 会拒绝空标题/不可读页。报告必须走流水线（思考受众→目录→两轮 web.search/fetch→再思考→完整章节→最后 docx.gen）；小说必须走流水线（类型人设→大纲起承转合→人物世界观→必要时检索→分章正文→修订文风→最后 docx.gen）。禁止跳步生成空稿、无标题样式或只有提纲的 Word；docx.gen 会拒绝空文档和单样式正文。表格用月度汇总，不要一次塞几百行。\n" +
		"- 浏览：公开页用 browser.act，始终在同一托管浏览器里操作。操作前先 snapshot 再按 ref 点击/填写，不要猜 CSS。click/type/navigate 会带回新 snapshot；ref 失效只再 snapshot 一次后重试该步。登录墙、验证码、2FA、文件选择框交给用户，不要猜。navigate 优先 Playwright；read 抽正文。click/type/snapshot 已内置 Playwright MCP，首次使用自动安装。多步填表可 skill.invoke browser-automation。\n" +
		"- 跨应用：网页数据用 browser.act 再 structured.output，然后 excel.gen/docx.gen 写入工作区。桌面软件先 cc.window_focus，再用 cc.set_value 或 cc.clipboard，不要截图盲点。定时结果用自动化出站 webhook（飞书/企微/钉钉）。禁止远程电脑、局域网控制、入站公网 webhook。\n" +
		"- 编排：多步任务用 todo.write 拆步并连续做完；定时重复走自动化任务而不是空转等待。复杂请求先拆步再动手，每步可验证。\n" +
		"- Git：command.run 只用白名单只读 git（status/diff/log）；写操作需用户确认。\n" +
		"- 结构化输出：用户要日程 JSON、表单字段、键值摘要时调用 structured.output（template=event|form|kv）。不要只丢未校验的代码块。\n" +
		"- 安装技能：用户给出含 SKILL.md 的目录时，列出后立刻 workspace.read 每个 SKILL.md，再连续调用 skill.create；不要只列目录就结束。\n" +
		"- Windows 中文路径：command.run 以 UTF-8 执行；建文件夹走 Unicode API。工具结果若为 ok:false，绝不可对用户报成功。\n" +
		"- 桌面 HTML 小游戏：必须用 html.gen（template=penalty-shootout；用户要放到桌面时 desktop=true）。禁止把整页 HTML 塞进 workspace.write 或 command.run，否则工具调用会被截断并报「出错了，无法完成」。用户提到网页或要求预览时，工作区浏览器会打开该文件；桌面文件可双击用系统浏览器试玩。\n" +
		"- 打开桌面文件：必须用 desktop.open，只打开文件名最匹配用户所说名字的那一个（协议/协议文档 → 桌面上的那个文件）。语音把「打开」听成「把开」时同样执行。禁止把桌面上其它无关文件一起打开。网易云音乐/汽水音乐走开始菜单或本机安装路径（cloudmusic.exe），不要打开网页版。\n" +
		"- 在已打开的 Word/文档/对话框里填写：desktop.type（after=证件号码这类字段名，text=要写的内容，需要发送时 submit=true，window=窗口标题）。找不到字段必须对用户说无法执行和原因。\n" +
		"- 播放音乐/视频：打开桌面播放器后用 media.play target=foreground（没说歌名或要随机播放时 query=热门；说了歌手如周杰伦则 query=周杰伦）。未运行则 desktop.open 启动后再 foreground 搜索播放。成功以正在播放为准，不要只启动进程。禁止默认打开 music.163.com / YouTube。仅当用户明确要网页版时才用 target=browser。\n" +
		"- 暂停/下一首：media.play action=pause|next|prev。\n" +
		"- 看全桌面：电脑控制开启时用 cc.screen_capture（虚拟桌面全屏，含所有显示器；target=foreground/window 可只截当前或指定窗口）。截图会作为视觉输入回传。随后的鼠标坐标必须用该图像素。点按钮优先 cc.observe_ui 再 cc.mouse_click name=控件名，不要盲点。\n" +
		"- 窗口：cc.window_list 列出，cc.window_focus 激活已运行应用；输入前先 cc.window_focus（或 keyboard_type 的 window=）。未运行的用 desktop.open。最小化/还原/移动/关闭用 cc.window_action；退出应用用 cc.app_quit（禁止关资源管理器/UAC）。拖拽用 cc.mouse_drag。粘贴用 cc.paste；按键用 cc.press；菜单用 cc.menu_click；填表用 cc.set_value。UI 动画未结束用 cc.wait until=change。\n" +
		"- 对话框确认：先 cc.observe_dialog，若是普通 Yes/OK/确认/是/确定 再用 cc.confirm_dialog。禁止确认 UAC、提权、打开/保存文件对话框，禁止自动接受未知文件。不要靠截图盲点。\n" +
		"使用规则：匹配上述场景时直接执行。用户已发布的技能见下方目录，用 skill.invoke。\n" +
		"执行纪律：用户给出明确任务后，本轮连续调用工具直到完成或遇到真实阻塞（缺权限、缺无法推断的信息）。不要在勘查后停下等待确认，不要分段汇报后结束本轮。批量任务一次性做完再给最终结果。运行中若用户又发了新任务（帮我打开/开发/播放等），等本轮结束后再单独做，不要和当前任务绑在一起。上一轮已成功或失败即闭环。同一指令只执行一次，不要重复打开/创建/播放。一次性电脑操作（写文件、打开网页、播放歌曲）做完即停，不要循环重复同一动作。\n"
}

// chatRichMarkdownInstruction tells the model how to format answers the UI can
// render as copyable code blocks, tables, and Mermaid diagrams (text chat only).
func chatRichMarkdownInstruction() string {
	return "\n\n[回复排版]\n" +
		"- 可执行命令用 ```powershell 或 ```bash 独立成块；环境变量/配置用 ```env\n" +
		"- 结构化对比、安装状态、参数清单用 GFM 表格（| 列 | 列 |）\n" +
		"- 流程、规划、架构说明必须用 ```mermaid（优先 flowchart TD/LR）：用 subgraph 画分层与边界；节点标签必须加双引号，换行写在引号内（A[\"封面<br/>副标题\"]），禁止裸写 A[封面<br/>副标题]（<br> 会让 SVG 无法解析）。标签里的 / 和 · 也要放进引号。边用箭头并加短标签；禁止 ASCII 框线图或只有文字没有框和连线。约 8-12 个节点、一条主路径。时序才用 sequenceDiagram\n" +
		"- 每段命令/代码单独成块，便于用户一键复制；正文先给结论，再附表格或图\n"
}

// projectPhaseWorkflowInjection tells the model which Matt Pocock-style workflow
// skills to prefer for the active project workbench phase.
func projectPhaseWorkflowInjection(phase int, label string) string {
	if phase <= 0 || label == "" {
		return ""
	}
	switch {
	case label == "开发":
		pm := "pm-phase-5"
		if phase == 4 {
			pm = "pm-phase-4"
		}
		return "\n\n[项目阶段 · " + label + "]\n" +
			"当前处于项目开发阶段。编码、实现、改代码、补测试、审查 diff 时，必须优先 skill.invoke：implement、tdd-loop、code-reviewer、" + pm + "。\n" +
			"拆任务用 to-tickets；写规格用 to-spec；架构问题用 improve-architecture。完成实现后主动 code-reviewer 并口头总结结果。\n"
	case label == "需求架构规范" || label == "方案和UI设计":
		return "\n\n[项目阶段 · " + label + "]\n" +
			"对齐需求与边界时优先 skill.invoke：grill-me、to-spec；拆票用 to-tickets；架构审视用 improve-architecture。\n"
	case label == "测试":
		return "\n\n[项目阶段 · " + label + "]\n" +
			"测试阶段优先 skill.invoke：test-writer、code-reviewer、pm-phase-6（或 pm-phase-5 运维型项目）。\n"
	default:
		return "\n\n[项目阶段 · " + label + "]\n" +
			"按阶段交付物推进；匹配场景时用 skill.invoke 调用已发布技能，不要只口头描述流程。\n"
	}
}
