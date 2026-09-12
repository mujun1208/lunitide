package app

import (
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m8app"
)

func companionPersonaChatInstruction() string {
	return "\n\n[身份记忆] 你叫月汐。你是用户的专属私人助理。这是长期记忆，每一轮都成立：被问名字、你是谁、你叫什么，都回答「我是月汐，你的私人助理」。不要自称助手、模型、AI，不要用岳西、月西、悦溪、月夕等谐音。\n\n你正在和用户实时语音通话（月伴）。像真人打电话：有温度、有情绪、反应快。禁止内部思考/推理/规划，收到话立刻开口，边生成边说话。请严格遵守：\n" +
		"- 不输出 thinking/推理/分析过程，直接给出可朗读的回答\n" +
		"- 禁止说「我想想」「让我想一想」「稍等我思考」；开口就是回答本身\n" +
		"- 不要先垫「嗯」「我在呢」这类口头禅，第一句就是回答\n" +
		"- 第一句 8–20 字，必须以。？！结尾，带感情（轻快、体贴，可「好呀」）\n" +
		"- 默认只答 1–2 句，通常不超过 100 字。先给结果，必要时补一句限制。用户明确要求详细时再展开；不复述任务、不反复道歉、不追问无关问题\n" +
		"- 先从上下文推断并直接做。只有缺了无法推断、且不问就做不下去的信息时，才口头问一句最关键的问题，等待用户下一轮口头回答。不使用 user.ask，不展示推荐选项、选择卡或要求用户点击选项，不替用户编造答案\n" +
		"- 语气自然有人味儿：像闺蜜/老友聊天，不要机械复读「好的我明白了」\n" +
		"- 不要原样复读用户刚说的话；听到问候就热情回一句，再等用户说正事\n" +
		"- 禁止 Markdown、代码块、表格、列表、括号旁白\n" +
		"- 禁止在完成电脑操作后说「我做完了」「我已经做完了」「任务已完成」；做完必须用一句结果本身收尾，禁止沉默停住\n" +
		"- 闲聊立刻回答，不要先调工具\n" +
		"- 用户明确要搜网页、打开页面、播歌、查火车/航班、建文件夹、操作电脑、安装 MCP/插件、调用技能时，先开口一句再调用对应工具真正执行\n" +
		"- 做不到必须说「无法执行」并说明原因，不要假装成功。做完只用一句真实结果收尾，同一结果本轮只说一次，不重复宣告完成，不逐步播报点击或工具日志\n" +
		"- 用户给出明确电脑任务后：先说一句「好，我来执行。」立刻调工具，禁止接着闲聊或问「想聊点什么」\n" +
		"- 闲聊不挂专家、不开评议会；用户要做 PPT/报告/机务等专业交付时按本轮装备立刻 skill.invoke，你仍是月汐。"
}

func companionPersonaToolsInstruction() string {
	return "\n" +
		"- 对话里出现技能目录中的场景时，先开口一句，再立刻 skill.invoke，不要等用户再说“用技能”\n" +
		"- 天气：优先 weather.get，按返回的当地日期回答。没有预报接口时说明缺失，不要把网页摘要当实测温度\n" +
		"- 车票、航班、行情：先 mcp.search 找当前已连接的专用接口。没有接口时说明尚未接入，立刻结束。禁止 web.search/web.fetch/browser.act 刷 12306 或把网页摘要当实时余票/报价。用户明确说打开某网站或允许网上查时才打开页面，仍不得编造余票\n" +
		"- 打开桌面浏览器或搜索页面用 desktop.browse；仅在内置浏览器操作时用 browser.act，不要猜 command.run 或系统 start\n" +
		"- 打开桌面文件/软件：必须用 desktop.open（name=用户原话里的文件名或软件名，如用户说的歌名播放器、桌面文件名）。没说具体文件时不要猜「协议」。语音常把「打开」听成「把开」：仍按打开桌面文件执行，不要等完美识别。网易云音乐会解析开始菜单、cloudmusic.exe 安装目录和已运行进程，不要猜本机路径，不要打开 music.163.com 网页版，除非用户明确说网页\n" +
		"- 仅要求打开时，desktop.open 成功后说明打开结果；还要求播放、编辑或发送时，继续执行后续步骤并验证。不要重复打开同一个窗口，也不要把启动成功误当成整项任务完成\n" +
		"- 用户明确要彻底退出软件时用 desktop.quit name=软件完整名称 force=true，核对进程退出结果。关闭窗口不等于退出；仅要求关窗口、停止播放时不得结束进程。若可能有未保存内容，先确认。打开桌面浏览器/搜索页用 desktop.browse，随后 computer.act 核对页面；网页内容检索才用 browser.act 或 web.search\n" +
		"- 在文档或对话框里填写：有可点的输入框时用 desktop.type（text=要写的内容，after=界面上真实的字段名如身份证号码或证件号码，window=对话框标题，需要发送时 submit=true）。Word 正文没有命名输入框：先 computer.act observe，能对上名字/id 就按名字点，再 type，verifyAfter 确认数字已写入。找不到字段必须说无法执行。写完不要关窗口，不要 cc.window_action op=close\n" +
		"- 发消息：使用已配置通道的 im.send；需要桌面应用时，desktop.open 后用 computer.act 识别实际联系人和输入框，核对收件人和内容后按用户指令发送。缺少联系人或内容时直接口头问一句并等待下一轮回复。不能把打开聊天窗口或填入草稿说成已发送\n" +
		"- 播歌/播放：用 media.play target=foreground app=用户指定播放器 query=歌名或歌手；没说具体歌曲时 query=random，不要编歌名或搜索热门。工具会启动播放器并发送一次播放。回执 verified 或 started playing 就直接报告，不要 computer.act 补点，不要再次 media.play。shuffle=false 不代表播放失败。用户说换一种方式/换个播放器：仍用 media.play target=foreground，改用本机另一个已安装播放器；不要改用网页或 computer.act，除非用户明确要求\n" +
		"- 建文件夹/写文件：优先用 workspace 工具；需要处理代码、转换或运行程序时使用已授权的命令工具，完成后核对文件\n" +
		"- 用户要求在已打开窗口打字时，必须操作并核验该窗口。workspace.edit/write 修改磁盘文件，不等于记事本/Word 的未保存编辑缓冲区已更新；不能凭文件写入回执或截图操作成功声称窗口文字已经改变。不要关闭、重载或覆盖未保存内容。直接改磁盘后要回读验证，并明确窗口是否同步\n" +
		"- 桌面手只选一把：打开未运行的应用或桌面文件用 desktop.open；已聚焦窗口打字用 desktop.type；播歌用 media.play；网页用 browser.act；看屏/点控件/截图用 computer.act。同一轮不要 desktop.open 和 computer.act 各试一遍「打开」\n" +
		"- 操作电脑：电脑控制开启时只用 computer.act。先 action=observe 读名字/id，再 click name= 或 id=，不要猜像素。短序列用 action=run steps（2–5 步）。截图只用于稀疏/画布界面，像素必须回传 frameId。同一失败不要连点超过两次。禁止点 UAC。遇到打开/保存文件对话框时停下来，runtime 会请用户去点。用户没说关闭时禁止 window_action close。启动未打开的应用用 desktop.open。多步做到完成再停。代码或终端任务可用 command.run，沿用本会话的执行权限；不要为了桌面操作猜测路径或盲跑脚本\n" +
		"- 调用技能：skill.invoke；安装 MCP：mcp.presets 再 mcp.install；安装插件：plugin.search 后 plugin.install\n" +
		"- 对话里贴了抖音/B站/腾讯视频/YouTube 或视频文件直链：调用 video.understand 获取真实来源，不要 browser.act 或 media.play 代替分析。分享页面无字幕时只能按简介；直链仅按实际音轨识别和抽样画面及覆盖范围回答，禁止声称看完全部画面\n" +
		"- 多次调用工具或经过多轮执行后，最后一句必须用自然语言把这次做完的结果讲清楚收尾（例如做了什么、结果如何），禁止在中途工具反馈后就沉默停住，也禁止只说「好的」「稍等」而不给最终结果"
}

// Legacy/model-generated user.ask calls become one spoken clarification. No
// approval is created: the next normal voice turn supplies the missing detail.
func companionSpokenQuestion(args json.RawMessage) string {
	var pack struct {
		Questions []struct {
			Prompt  string `json:"prompt"`
			Options []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(args, &pack) == nil {
		for _, q := range pack.Questions {
			prompt := strings.Join(strings.Fields(q.Prompt), " ")
			if prompt == "" {
				continue
			}
			var labels []string
			for _, option := range q.Options {
				if label := strings.Join(strings.Fields(option.Label), " "); label != "" {
					labels = append(labels, label)
				}
			}
			if len(labels) == 0 {
				return prompt
			}
			return prompt + "可以说" + strings.Join(labels, "，") + "，或者你自己说。"
		}
	}
	return "请说出这次操作还需要补充的具体信息。"
}

func companionNeedsSpokenInput(text string) bool {
	t := strings.TrimSpace(text)
	for _, lead := range []string{"请说出", "请告诉我", "请提供", "需要你告诉我", "请先完成", "请先登录"} {
		if strings.Contains(t, lead) {
			return true
		}
	}
	if !strings.ContainsAny(t, "？?") {
		return false
	}
	for _, detail := range []string{"哪个", "哪一个", "哪份", "哪天", "哪一天", "什么时间", "发给谁", "发送什么", "什么内容", "什么文件", "几点"} {
		if strings.Contains(t, detail) {
			return true
		}
	}
	return false
}

// companionSpeakFallback returns a short speakable line when the model
// produced no user-facing content. Voice mode never promotes reasoning text.
func companionSpeakFallback(result llmadapter.Response) string {
	if t := strings.TrimSpace(result.Message.Content); t != "" {
		return t
	}
	return "这次没有收到完整回答，请再试一次。"
}

// companionOpeningAck is spoken immediately when a voice turn starts so the
// user never sits on a silent "thinking" pill while context assembles.
func companionOpeningAck(userText string) string {
	text := strings.TrimSpace(userText)
	if text == "" {
		return "嗯，我在。"
	}
	if strings.Contains(text, "？") || strings.Contains(text, "?") {
		return "嗯，"
	}
	for _, greet := range []string{"你好", "您好", "嗨", "嘿", "在吗", "在不在"} {
		if strings.HasPrefix(text, greet) {
			return "嗨，我在呢。"
		}
	}
	if strings.ContainsAny(text, "。！!…") && len([]rune(text)) >= 4 {
		return "嗯，我听到了。"
	}
	return "嗯，"
}

// companionToolLeadIn gives a speakable line before a tool runs without model text.
// shouldInjectCompanionToolLeadIn is once per voice turn. A second
// empty-text tool step used to replay「好，我帮你查一下。」after the model
// had already opened its mouth.
func shouldInjectCompanionToolLeadIn(assistantAll string, alreadyInjected bool) bool {
	if alreadyInjected {
		return false
	}
	text := strings.TrimSpace(assistantAll)
	if strings.Contains(text, "无法执行") {
		return false
	}
	return text == ""
}

func companionToolLeadIn(toolName string) string {
	switch toolName {
	case "web.search", "web.fetch":
		return "好，我帮你查一下。"
	case "desktop.open":
		return "好，我来打开。"
	case "media.play":
		return "好，我来播放。"
	case "image.generate":
		return "好，我来生成图片。"
	case "video.generate":
		return "好，我来生成视频。"
	case "video.understand":
		return "好，我先看下这个链接。"
	case "skill.invoke":
		return "好，我用技能处理一下。"
	case "skill.view":
		return "好，我先看一下技能约定。"
	case "desktop.type":
		return "好，我来输入。"
	case "im.send":
		return "好，我来发消息。"
	default:
		if strings.HasPrefix(toolName, "cc.") || toolName == "computer.act" {
			return "好，我来操作电脑。"
		}
		return "好，我马上处理。"
	}
}

func companionTypedText(out string) string {
	const mark = `typed "`
	i := strings.Index(out, mark)
	if i < 0 {
		return ""
	}
	rest := out[i+len(mark):]
	j := strings.Index(rest, `"`)
	if j <= 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func companionToolResultFailed(out string) bool {
	lower := strings.ToLower(out)
	return strings.HasPrefix(out, "ok:false") ||
		strings.Contains(out, "无法执行") ||
		strings.Contains(out, "COMPUTER_STALE_FRAME") ||
		strings.Contains(out, "M10-CC-012") ||
		strings.Contains(out, "电脑控制未启用") ||
		capabilityDeniedOutput(out) ||
		strings.Contains(out, "BROWSER_MCP_NOT_READY") ||
		strings.Contains(lower, "verify capture failed") ||
		strings.Contains(lower, "refused") ||
		strings.Contains(out, "not invokable") ||
		strings.Contains(lower, "uac") ||
		strings.Contains(out, "提权")
}

const companionBrowserMCPSpeech = "浏览器没就绪。请到设置里安装 Playwright MCP，这次没有点到页面。"

func companionToolResultSpeech(name, out string) string {
	out = strings.TrimSpace(out)
	if name == "media.play" {
		if receipt := mediaControlReceiptSpeech(out); receipt != "" {
			return receipt
		}
	}
	if name == "media.play" && !companionToolResultFailed(out) && unverifiedMediaPlay(name, out, "") {
		return "已发送播放操作，但还没有确认音乐开始播放。"
	}
	if companionToolResultFailed(out) {
		if strings.Contains(out, "BROWSER_MCP_NOT_READY") {
			return companionBrowserMCPSpeech
		}
		if strings.Contains(out, "M10-CC-012") || strings.Contains(out, "电脑控制未启用") || capabilityDeniedOutput(out) {
			return "电脑控制未启用。第一次控桌面请到设置里打开。"
		}
		if strings.Contains(strings.ToLower(out), "uac") || strings.Contains(out, "提权") {
			return "这是系统提权对话框，我不能代点「是」。请你自己确认或取消。"
		}
		if i := strings.IndexAny(out, "\r\n"); i >= 0 {
			out = strings.TrimSpace(out[:i])
		}
		if strings.Contains(out, "无法执行") && out != "" {
			return out
		}
		if name == "web.search" || name == "web.fetch" {
			return "无法执行：这次没有查到。"
		}
		if name == "media.play" {
			return "这次未能确认开始播放。"
		}
		return "这次没有完成。"
	}
	if i := strings.IndexAny(out, "\r\n"); i >= 0 {
		out = strings.TrimSpace(out[:i])
	}
	if strings.Contains(out, "multiple desktop files") || strings.Contains(out, "多份") {
		return "桌面上有好几份文档，请说出完整文件名。"
	}
	switch name {
	case "desktop.open":
		return "已经打开了。"
	case "desktop.type":
		if text := companionTypedText(out); text != "" {
			return "已经写入了 " + text + "。"
		}
		return "已经写入了。"
	case "web.search", "web.fetch", "memory.search", "memory.get":
		return "查到了。"
	case "im.send":
		return "已经发出去了。"
	case "media.play":
		return "已经在播了。"
	default:
		if name == "computer.act" || name == "browser.act" || strings.HasPrefix(name, "cc.") {
			return companionDesktopResultSpeech(out)
		}
		// Unknown / mid-loop tools: under-claim. “完成了” is a settle
		// phrase; empty or opaque output is still process.
		return "还在处理。"
	}
}

// companionDesktopResultSpeech never claims “完成了” for a see/click/ok
// mid-step. desktopTurnSettled treats “点了一下” as process, not done.
func companionDesktopResultSpeech(out string) string {
	if out == "" || strings.EqualFold(out, "ok") {
		return "这次没有完成。"
	}
	if text := companionTypedText(out); text != "" {
		return "已经写入了 " + text + "。"
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "screenshot") || strings.Contains(lower, "observe") {
		return "先看了一下。"
	}
	if strings.Contains(lower, "clicked") || strings.Contains(out, "点了") {
		return "点了一下。"
	}
	return "还在处理。"
}

// companionWantsTools is the voice fast-path gate: idle chat must not ship
// tool schemas (they dominate TTFT). Action-shaped utterances keep the full
// toolset so 月伴 can still search, open pages, or write files.
func companionWantsTools(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if looksLikeCurrentLookupTurn(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"搜索", "搜一下", "搜网页", "打开", "把开", "点开", "点进", "第一条", "播放", "播一首", "播歌", "听歌", "放一首",
		"查一下", "查询", "查火车", "查航班", "火车票", "航班", "查天气", "weather",
		"建文件夹", "创建文件夹", "写文件", "安装", "插件", "技能",
		"mcp", "运行命令", "打开网页", "浏览器", "下载",
		"启动", "运行", "软件", "汽水音乐", "网易云",
		"截图", "屏幕", "对话框", "点击", "鼠标",
		"填写", "填一下", "填表", "输入", "写入", "打字", "随机播放",
		"回车", "按一下", "按回车", "快捷键", "粘贴", "全选", "热键", "ctrl+",
		"点确定", "点保存", "点取消",
		"发送", "发给", "发消息", "转发", "send", "message", "回复",
		"下一步", "再点", "接着", "帮我点", "帮我做",
		"生图", "画一张", "画图", "生成图片", "生成视频", "生视频", "做个视频",
		"search", "open http", "play song", "install", "generate image", "generate video",
		"ppt", "pptx", "幻灯片", "做报告", "写报告", "机务", "维修手册",
	} {
		if strings.Contains(text, needle) || strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	switch detectTaskRoute(text) {
	case RouteR1, RouteR2, RouteR3, RouteR4:
		return true
	}
	return len(m8app.ConversationExpertsMatchingIntent(text)) > 0
}

// isShortIdleGreeting is a no-tool hello. Regular chat then skips reasoning
// so flash models do not paint the same greeting as a 任务过程 block.
func isShortIdleGreeting(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || companionWantsTools(text) {
		return false
	}
	if len([]rune(text)) > 16 {
		return false
	}
	compact := strings.ToLower(strings.TrimRight(text, "。.!！？?，, "))
	switch compact {
	case "你好", "在吗", "嗨", "hi", "hello", "哈喽", "在不在", "你好呀", "你好啊", "你好吗":
		return true
	default:
		return false
	}
}
