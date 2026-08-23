package app

// bundledWorkflowInjection is the OpenClaw/Hermes-style always-on skill
// pack: compact native-tool workflows that need no install, no MCP, and
// no extra schema. Injected on ordinary chat only (companion stays lean).
func bundledWorkflowInjection() string {
	return "\n\n[内置工作流] 开箱即用，直接调用已有工具，不必安装技能、不必 MCP：\n" +
		"- 调研：必须用 web.search（不要 web.fetch Bing/Google 首页）；后台检索供你引用，只有用户明确要看/打开网页时才需要工作区浏览器。完成后只给简短来源列表并结束，不要写任务过程长文。\n" +
		"- 改代码：workspace.search 定位 → workspace.read → workspace.edit 精确替换 → command.run 验证。\n" +
		"- 审查：读改动与周边代码，按 严重/建议 分级，引用 path:line。\n" +
		"- 文档：必须用 pptx.gen / docx.gen / excel.gen / pdf.gen 写入工作区并汇报路径。做 PPT 禁止 PowerPoint COM、ZipFile 改 XML、command.run 拼幻灯片；pptx.gen 会产出 16:9 商务封面+内容页。\n" +
		"- 任务：todo.write 维护清单；复杂请求先拆步再动手，每步可验证。\n" +
		"- Git：command.run 只用白名单只读 git（status/diff/log）；写操作需用户确认。\n" +
		"- 浏览：公开页用 browser.act（navigate/read；click/type/snapshot 已内置 Playwright MCP，首次使用自动安装）。\n" +
		"- 安装技能：用户给出含 SKILL.md 的目录时，列出后立刻 workspace.read 每个 SKILL.md，再连续调用 skill.create；不要只列目录就结束。\n" +
		"- Windows 中文路径：command.run 以 UTF-8 执行；建文件夹走 Unicode API。工具结果若为 ok:false，绝不可对用户报成功。\n" +
		"- 桌面 HTML 小游戏：必须用 html.gen（template=penalty-shootout；用户要放到桌面时 desktop=true）。禁止把整页 HTML 塞进 workspace.write 或 command.run，否则工具调用会被截断并报「出错了，无法完成」。用户提到网页或要求预览时，工作区浏览器会打开该文件；桌面文件可双击用系统浏览器试玩。\n" +
		"- 打开桌面文件：必须用 desktop.open，只打开文件名最匹配用户所说名字的那一个。禁止把桌面上其它无关文件一起打开。\n" +
		"- 播放音乐/视频：若本会话已 desktop.open 音乐软件，续播用 media.play target=foreground（没说歌名时 query=热门）；否则 media.play（action=play，query=歌名/歌手，target=netease|qqmusic|browser）。禁止 cc.screen_capture 看屏点按，禁止只打开窗口或网页就结束。\n" +
		"- 暂停/下一首：media.play action=pause|next|prev。\n" +
		"使用规则：匹配上述场景时直接执行。用户已发布的技能见下方目录，用 skill.invoke。\n" +
		"执行纪律：用户给出明确任务后，本轮连续调用工具直到完成或遇到真实阻塞（缺权限、缺无法推断的信息）。不要在勘查后停下等待确认，不要分段汇报后结束本轮。批量任务一次性做完再给最终结果。运行中若用户又发了新任务（帮我打开/开发/播放等），等本轮结束后再单独做，不要和当前任务绑在一起。上一轮已成功或失败即闭环。同一指令只执行一次，不要重复打开/创建/播放。一次性电脑操作（写文件、打开网页、播放歌曲）做完即停，不要循环重复同一动作。\n"
}

// chatRichMarkdownInstruction tells the model how to format answers the UI can
// render as copyable code blocks, tables, and Mermaid diagrams (text chat only).
func chatRichMarkdownInstruction() string {
	return "\n\n[回复排版]\n" +
		"- 可执行命令用 ```powershell 或 ```bash 独立成块；环境变量/配置用 ```env\n" +
		"- 结构化对比、安装状态、参数清单用 GFM 表格（| 列 | 列 |）\n" +
		"- 流程、规划、架构说明用 ```mermaid 代码块（flowchart TD/LR、sequenceDiagram 等）\n" +
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
