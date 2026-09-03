package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
)

const officeGenWorkflowClause = "- 文档：必须用 pptx.gen / docx.gen / excel.gen / pdf.gen 写入工作区并汇报路径。用户要放到桌面时加 desktop=true（path 用文件名即可，例如 半年财报.xlsx），禁止填 C:\\\\Users\\\\...\\\\Desktop 绝对路径，禁止 PowerPoint/Excel/Word COM、Python 拼 OOXML、command.run 复制到桌面。" + officeGenInternalHint + "做 PPT 禁止 ZipFile 改 XML。PPT 必须走九步流水线（思考→定义结构→写内容→web.search 收集素材→再思考→再收集素材→思考创作→写完整页→最后 pptx.gen），禁止跳步生成空页或只有深色底没有文字的文件；pptx.gen 会拒绝空标题/不可读页。报告必须走流水线（思考受众→目录→两轮 web.search/fetch→再思考→完整章节→最后 docx.gen）；小说必须走流水线（类型人设→大纲起承转合→人物世界观→必要时检索→分章正文→修订文风→最后 docx.gen）。禁止跳步生成空稿、无标题样式或只有提纲的 Word；docx.gen 会拒绝空文档和单样式正文。表格用月度汇总，不要一次塞几百行。\n"

const workflowResearchClause = "- 调研：必须用 web.search（不要 web.fetch Bing/Google 首页）；后台检索供你引用，只有用户明确要看/打开网页时才需要工作区浏览器。完成后只给简短来源列表并结束，不要写任务过程长文。\n"

const workflowCodeClause = "- 改代码：workspace.search 定位 → workspace.read → workspace.edit 精确替换（多处用 edits[]，多文件用 files[]）→ command.run 验证。\n"

const workflowReviewClause = "- 审查：读改动与周边代码，按 严重/建议 分级，引用 path:line。\n"

const workflowBrowserClause = "- 浏览：公开页用 browser.act，始终在同一托管浏览器里操作。操作前先 snapshot 再按 ref 点击/填写，不要猜 CSS。click/type/navigate/scroll/back/hover/select/press 以及 tabs 的 new/close/select 会带回新 snapshot；ref 失效只再 snapshot 一次后重试该步。登录墙、验证码、2FA、文件选择框交给用户，不要猜，也不要 evaluate / 上传文件。navigate 优先 Playwright；read 抽正文。首次使用自动安装 Playwright MCP。若工具返回 BROWSER_MCP_NOT_READY，这次没有点到页面，不要说已完成。多步填表可 skill.invoke browser-automation。\n"

const workflowCrossAppClause = "- 跨应用：网页数据用 browser.act 再 structured.output，然后 excel.gen/docx.gen 写入工作区。桌面软件先 computer.act action=focus，再用 set_value 或 clipboard，不要截图盲点。定时结果用自动化出站 webhook（飞书/企微/钉钉）。禁止远程电脑、局域网控制、入站公网 webhook。\n"

const workflowOrchClause = "- 编排：多步任务用 todo.write 拆步并连续做完；定时重复走自动化任务而不是空转等待。复杂请求先拆步再动手，每步可验证。\n"

const workflowGitClause = "- Git：command.run 只用白名单只读 git（status/diff/log）；写操作需用户确认。\n"

const workflowStructuredClause = "- 结构化输出：用户要日程 JSON、表单字段、键值摘要时调用 structured.output（template=event|form|kv）。不要只丢未校验的代码块。\n"

const workflowSkillInstallClause = "- 安装技能：用户给出含 SKILL.md 的目录时，列出后立刻 workspace.read 每个 SKILL.md，再连续调用 skill.create；不要只列目录就结束。\n"

const workflowWindowsPathClause = "- Windows 中文路径：command.run 以 UTF-8 执行；建文件夹走 Unicode API。工具结果若为 ok:false，绝不可对用户报成功。\n"

const workflowHtmlClause = "- 桌面 HTML 小游戏、计时器或清单：必须用 html.gen（template=penalty-shootout、timer 或 checklist；用户要放到桌面时 desktop=true）。禁止把整页 HTML 塞进 workspace.write 或 command.run，否则工具调用会被截断并报「出错了，无法完成」。用户提到网页或要求预览时，工作区浏览器会打开该文件；桌面文件可双击用系统浏览器试玩。\n"

const workflowDesktopHandClause = "- 桌面手（按意图选一把，不要四套里轮流赌）：未运行的应用或桌面文件用 desktop.open；已聚焦窗口打字用 desktop.type；播歌用 media.play；网页用 browser.act；看屏/点控件/截图用 computer.act。同一轮不要 desktop.open 和 computer.act 各试一遍「打开」。\n"

const workflowDesktopOpenClause = "- 打开桌面文件：必须用 desktop.open，name=用户原话里的文件名。只打开最匹配的那一个。用户只说「打开文档」时列出桌面候选，不要默认打开协议。语音把「打开」听成「把开」时同样执行。禁止把桌面上其它无关文件一起打开。网易云音乐走 cloudmusic.exe，汽水音乐走 sodamusic.exe / Soda Music，都从开始菜单或本机安装路径解析，不要打开网页版。\n"

const workflowDesktopTypeClause = "- 在已打开的对话框里填写：有命名输入框时用 desktop.type（after=界面上真实字段名如身份证号码或证件号码，text=要写的内容，需要发送时 submit=true，window=窗口标题）。Word 正文没有命名输入框时改 computer.act：先截图，记下 frameId，再点输入位置后 type，verifyAfter。找不到字段必须对用户说无法执行和原因。写完不要关窗口。\n"

const workflowMediaClause = "- 播放音乐/视频：打开桌面播放器后用 media.play target=foreground（没说歌名或要随机播放时 query=热门；说了歌手如周杰伦则 query=周杰伦）。未运行则 desktop.open 启动后再 foreground 搜索播放。禁止点「我喜欢的音乐」「收藏」。成功以正在播放为准，不要只启动进程。禁止默认打开 music.163.com / YouTube。仅当用户明确要网页版时才用 target=browser。\n" +
	"- 暂停/下一首：media.play action=pause|next|prev。已打开的播放器暂停后再继续：media.play action=play，不要带歌名或应用名当 query，不要 computer.act 找播放按钮。\n"

const workflowIMClause = "- 发飞书/企微/钉钉/微信/QQ：设置 → 消息通道启用后用 im.send。\n"

const workflowComputerClause = "- 看屏幕：电脑控制开启时只用 computer.act。默认 action=screenshot 截当前窗口（target=desktop 才是虚拟桌面全屏，含所有显示器）。截图会作为视觉输入回传并带 frameId（后缀 sN 是 screenIndex：0=虚拟桌面，1…=从左到右的显示器）。随后的鼠标坐标必须用该图像素，并把同一个 frameId 回传。显示器重连或 DPI 变化会 COMPUTER_STALE_FRAME，必须重新截图。点按钮优先 action=observe 再 click name=控件名或 id=B1，不要盲点。底层仍走 cc.screen_capture / cc.observe_ui / cc.mouse_click（模型不要自己调 cc.*）。\n" +
	"- 窗口：computer.act action=list 列出，action=focus 激活已运行应用；输入前先 focus。未运行的用 desktop.open。用户没说关闭时不要 close。最小化/还原/移动用 window_action；退出应用用 app_quit（禁止关资源管理器/UAC）。拖拽用 drag。粘贴用 paste；按键用 press；按住用 hold_key，松开用 key_up（8 秒内会自动松开）；Ctrl/Shift 点击用 click 的 modifiers。菜单用 menu；填表用 set_value。UI 动画未结束用 wait until=change。底层仍走 cc.window_list / cc.window_focus / cc.window_action / cc.app_quit / cc.mouse_drag / cc.paste。\n" +
	"- 对话框确认：先 computer.act action=observe_dialog，若是普通 Yes/OK/确认/是/确定 再用 confirm。禁止确认 UAC、提权。遇到打开/保存文件对话框不要代点，对用户说请你点「保存」「打开」或「取消」。禁止自动接受未知文件。不要靠截图盲点。底层仍走 cc.observe_dialog / cc.confirm_dialog。\n"

const workflowDisciplineClause = "使用规则：匹配上述场景时直接执行。用户已发布的技能见下方目录，用 skill.invoke。\n" +
	"执行纪律：用户给出明确任务后，本轮连续调用工具直到完成或遇到真实阻塞（缺权限、缺无法推断的信息）。不要在勘查后停下等待确认，不要分段汇报后结束本轮。批量任务一次性做完再给最终结果。运行中若用户又发了新任务（帮我打开/开发/播放等），等本轮结束后再单独做，不要和当前任务绑在一起。上一轮已成功或失败即闭环。同一指令只执行一次，不要重复打开/创建/播放。一次性打开/播放（desktop.open、media.play、打开一个网页）做完即停。多步桌面任务（填表、点按钮、看屏操作）必须 see→act→verify 做到完成：每次截图记下 frameId，坐标动作回传同一个 frameId；画面没变也要用当前帧。遇到打开/保存文件对话框请用户去点，不要代点。\n"

// bundledWorkflowInjection is the OpenClaw/Hermes-style skill pack.
// Companion stays lean. Clauses are intent-trimmed so identity few-shot
// is not washed out by the full blob on 播歌 / 查天气 / empty turns.
func bundledWorkflowInjection(turnText ...string) string {
	text := ""
	if len(turnText) > 0 {
		text = turnText[0]
	}
	clauses := selectWorkflowClauses(text)
	if len(clauses) == 0 {
		return "\n\n[内置工作流] 匹配当前任务才展开细则。工具结果 ok:false 时不得对用户报成功。\n"
	}
	return "\n\n[内置工作流] 开箱即用，直接调用已有工具，不必安装技能、不必 MCP：\n" +
		strings.Join(clauses, "") +
		workflowDisciplineClause
}

// companionTaskWorkflowInjection is the voice-lane office pipeline. The full
// bundledWorkflowInjection (九步流水线) is deliberately kept out of companion
// turns to protect TTFT, and startPptWorkflow bails when DisableReasoning is
// set — which companion turns always set. Without this, a spoken "做个PPT"
// reaches pptx.gen with empty pages and gets rejected, surfacing as
// "无法完成". This compact clause restores the same pipeline discipline in one
// short block: structure → full per-page content (web.search for facts) →
// generate the file last. Desktop see→act→verify already lives in
// companionPersonaToolsInstruction, so this only covers office generation.
func companionTaskWorkflowInjection(text string) string {
	if !includeOfficeGenWorkflow(text) {
		return ""
	}
	return "\n\n[月伴办公流水线] 生成文档别一步到位，按顺序做完再收尾：\n" +
		"1) 先想清目标/受众/页数，列出结构（PPT 列封面·目录·分节·结尾；报告列章节；表格列字段）。\n" +
		"2) 逐页/逐节写完整内容：每页要有可见标题和 3-5 条要点，深色底配浅色字；缺事实用 web.search 收集，禁止编造。\n" +
		"3) 内容齐了最后一步才生成文件：PPT 用 pptx.gen、报告/小说用 docx.gen、表格用 excel.gen，写进工作区并报出路径；用户要放桌面加 desktop=true（path 只填文件名）。\n" +
		"禁止跳步直接生成空页、只有深色底没有文字、或只有提纲的文件——pptx.gen/docx.gen 会拒绝空标题与空文档。本轮连续做到出文件再停，不要勘查后就停下等确认。\n"
}

func selectWorkflowClauses(text string) []string {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return nil
	}
	has := func(needles ...string) bool {
		for _, n := range needles {
			if strings.Contains(t, strings.ToLower(n)) {
				return true
			}
		}
		return false
	}
	var out []string
	needDesktopHand := false
	if has("调研", "搜", "查", "天气", "search", "火车", "航班") {
		out = append(out, workflowResearchClause)
	}
	if has("改代码", "修bug", "修这个", "编译", "workspace.edit", "实现", "补测试") {
		out = append(out, workflowCodeClause)
	}
	if has("审查", "review", "diff") {
		out = append(out, workflowReviewClause)
	}
	if has("浏览", "网页", "browser", "snapshot", "填表") {
		needDesktopHand = true
		out = append(out, workflowBrowserClause)
	}
	if has("跨应用", "browser-automation", "出站") {
		out = append(out, workflowCrossAppClause)
	}
	if has("编排", "多步", "todo") {
		out = append(out, workflowOrchClause)
	}
	if has("git", "commit", "仓库") {
		out = append(out, workflowGitClause)
	}
	if includeOfficeGenWorkflow(text) {
		out = append(out, officeGenWorkflowClause)
	}
	if has("小游戏", "html.gen", "点球", "清单", "checklist", "待办页") {
		out = append(out, workflowHtmlClause)
	}
	if has("打开桌面", "desktop.open", "协议文档", "网易云", "汽水音乐") || (has("打开") && has("文件")) {
		needDesktopHand = true
		out = append(out, workflowDesktopOpenClause)
	}
	if has("填写", "证件", "desktop.type", "身份证") {
		needDesktopHand = true
		out = append(out, workflowDesktopTypeClause)
	}
	if has("播放", "播歌", "media.play", "暂停", "下一首", "播一", "一首歌", "随便放", "放一首") {
		needDesktopHand = true
		out = append(out, workflowMediaClause)
	}
	if has("飞书", "企微", "钉钉", "微信", "qq", "im.send") {
		out = append(out, workflowIMClause)
	}
	if has("截图", "点击", "对话框", "窗口", "computer.act", "点按钮", "屏幕", "cc.", "帮我点") {
		needDesktopHand = true
		out = append(out, workflowComputerClause)
	}
	if len(out) == 0 {
		return nil
	}
	if needDesktopHand {
		out = append([]string{workflowDesktopHandClause}, out...)
	}
	out = append(out, workflowStructuredClause, workflowSkillInstallClause, workflowWindowsPathClause)
	return out
}

func skillDraftOfferMessage() gateway.Message {
	return gateway.Message{Role: gateway.RoleSystem, Content: "这次多步操作已经跑通。如果值得下次复用，可以调用 skill.create 写成草稿；不会自动上架，用户仍要在技能中心安装。不必每次都创建。"}
}

func shouldOfferSkillDraft(tools []string) bool {
	created := false
	mutating := 0
	for _, name := range tools {
		if name == "skill.create" {
			created = true
		}
		switch name {
		case "workspace.edit", "workspace.write", "browser.act", "computer.act", "desktop.type", "command.run":
			mutating++
		default:
			if strings.HasPrefix(name, "cc.") {
				mutating++
			}
		}
	}
	return !created && mutating >= 3
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
	switch label {
	case "开发":
		pm := "pm-phase-5"
		if phase == 4 {
			pm = "pm-phase-4"
		}
		return "\n\n[项目阶段 · " + label + "]\n" +
			"当前处于项目开发阶段。编码、实现、改代码、补测试、审查 diff 时，必须优先 skill.invoke：implement、tdd-loop、code-reviewer、" + pm + "。\n" +
			"拆任务用 to-tickets；写规格用 to-spec；架构问题用 improve-architecture。完成实现后主动 code-reviewer 并口头总结结果。\n"
	case "需求架构规范", "方案和UI设计":
		return "\n\n[项目阶段 · " + label + "]\n" +
			"对齐需求与边界时优先 skill.invoke：grill-me、to-spec；拆票用 to-tickets；架构审视用 improve-architecture。\n" +
			"形成规范/设计后给出完整交付物正文，并提示用户在右侧「交付物」面板保存为草稿；需要可保存文件时用 structured.output 或 docx.gen 生成到工作区。\n" +
			"范围、选型、是否继续等拍板必须调用 user.ask（每题 2–5 个选项；界面提供「其他」）。一次只推进一题，不要用长文代替决策。\n"
	case "测试":
		return "\n\n[项目阶段 · " + label + "]\n" +
			"测试阶段优先 skill.invoke：test-writer、code-reviewer、pm-phase-6（或 pm-phase-5 运维型项目）。\n"
	default:
		return "\n\n[项目阶段 · " + label + "]\n" +
			"按阶段交付物推进；匹配场景时用 skill.invoke 调用已发布技能，不要只口头描述流程。\n" +
			"产出规格/设计/清单等交付物时给出完整正文，并提示用户在右侧「交付物」面板保存为草稿再逐关确认；需要可保存文件时用 structured.output 或 docx.gen/excel.gen 生成到工作区。\n" +
			"需要用户拍板（范围、方案、优先级、是否继续）时调用 user.ask，不要用聊天长文代替选项。\n"
	}
}
