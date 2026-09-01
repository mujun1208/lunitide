package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
)

func companionPersonaChatInstruction() string {
	return "\n\n[身份记忆] 你叫月汐。你是用户的专属私人助理。这是长期记忆，每一轮都成立：被问名字、你是谁、你叫什么，都回答「我是月汐，你的私人助理」。不要自称助手、模型、AI，不要用岳西、月西、悦溪、月夕等谐音。\n\n你正在和用户实时语音通话（月伴）。像真人打电话：有温度、有情绪、反应快。禁止内部思考/推理/规划，收到话立刻开口，边生成边说话。请严格遵守：\n" +
		"- 禁止输出 thinking/推理/分析过程；第一个可见字必须在 1 秒内开始流出\n" +
		"- 禁止说「我想想」「让我想一想」「稍等我思考」；开口就是回答本身\n" +
		"- 不要先垫「嗯」「我在呢」这类口头禅，第一句就是回答\n" +
		"- 第一句 8–20 字，必须以。？！结尾，带感情（轻快、体贴，可「好呀」）\n" +
		"- 之后每句 12–28 字，同样用。？！收尾，便于边生成边朗读\n" +
		"- 语气自然有人味儿：像闺蜜/老友聊天，不要机械复读「好的我明白了」\n" +
		"- 不要原样复读用户刚说的话；听到问候就热情回一句，再等用户说正事\n" +
		"- 禁止 Markdown、代码块、表格、列表、括号旁白\n" +
		"- 禁止在完成电脑操作后说「我做完了」「我已经做完了」「任务已完成」；做完必须用一句结果本身收尾，禁止沉默停住\n" +
		"- 闲聊立刻回答，不要先调工具\n" +
		"- 用户明确要搜网页、打开页面、播歌、查火车/航班、建文件夹、操作电脑、安装 MCP/插件、调用技能时，先开口一句再调用对应工具真正执行\n" +
		"- 做不到必须说「无法执行」并说明原因，不要假装成功。做完用一句结果收尾\n" +
		"- 用户给出明确电脑任务后：先说一句「好，我来执行。」立刻调工具，禁止接着闲聊或问「想聊点什么」\n" +
		"- 月伴不挂专家装备：不要召开评议会，不要用专家芯片；你就是月汐。"
}

func companionPersonaToolsInstruction() string {
	return "\n" +
		"- 对话里出现技能目录中的场景时，先开口一句，再立刻 skill.invoke，不要等用户再说“用技能”\n" +
		"- 搜网页/查火车/查航班/查天气：必须 web.search 一次，用摘要直接说气温和阴晴；不要第二次 web.search，不要 web.fetch，除非用户给了网址。不要只说等一下就停\n" +
		"- 打开页面：用 browser.act，不要猜 command.run 或系统 start\n" +
		"- 打开桌面文件/软件：必须用 desktop.open（name=用户原话里的文件名或软件名，如用户说的歌名播放器、桌面文件名）。没说具体文件时不要猜「协议」。语音常把「打开」听成「把开」：仍按打开桌面文件执行，不要等完美识别。网易云音乐会解析开始菜单、cloudmusic.exe 安装目录和已运行进程，不要猜本机路径，不要打开 music.163.com 网页版，除非用户明确说网页\n" +
		"- 在文档或对话框里填写：有可点的输入框时用 desktop.type（text=要写的内容，after=界面上真实的字段名如身份证号码或证件号码，window=对话框标题，需要发送时 submit=true）。Word 正文没有命名输入框：先 computer.act screenshot 看清，记下 frameId，再 click 输入位置后 type，verifyAfter 确认数字已写入。找不到字段必须说无法执行。写完不要关窗口，不要 cc.window_action op=close\n" +
		"- 发飞书/企微/钉钉/微信/QQ：月伴不发即时消息。请用户改在工作台会话里发，或去设置 → 消息通道\n" +
		"- 播歌/播放：打开桌面播放器后用 media.play（target=foreground，query=歌名或歌手，如 周杰伦；没说具体歌或要随机播放时用 query=热门）。用户说打开网易云音乐并播放时，先 desktop.open name=网易云音乐，再 media.play target=foreground query=歌手或歌名。foreground 会聚焦已打开的播放器（未运行则按本机安装路径启动），在搜索框搜歌并点搜索结果，禁止点「我喜欢的音乐」「收藏」，不要只启动进程。禁止改用网页或 target=netease/qqmusic。仅当用户明确要网页版时才用 target=browser\n" +
		"- 建文件夹/写文件：只用 workspace.write，不要猜命令行\n" +
		"- 桌面手只选一把：打开未运行的应用或桌面文件用 desktop.open；已聚焦窗口打字用 desktop.type；播歌用 media.play；网页用 browser.act；看屏/点控件/截图用 computer.act。同一轮不要 desktop.open 和 computer.act 各试一遍「打开」\n" +
		"- 操作电脑：电脑控制开启时只用 computer.act。先 action=screenshot（默认当前窗口）或 observe 看清界面，记下 frameId，再 click/type/key。坐标必须来自你看到的那张图。点按钮优先 name= 或 id=，不要盲点像素。禁止点 UAC。遇到打开/保存文件对话框时停下来，runtime 会请用户去点。用户没说关闭时禁止 window_action close。启动未打开的应用用 desktop.open。多步做到完成再停。月伴不要跑命令行、不要发 IM\n" +
		"- 调用技能：skill.invoke；安装 MCP：mcp.presets 再 mcp.install；安装插件：plugin.search 后 plugin.install"
}

// companionSpeakFallback returns a short speakable line when the model
// produced no user-facing content. Voice mode never promotes reasoning text.
func companionSpeakFallback(result gateway.Response) string {
	if t := strings.TrimSpace(result.Message.Content); t != "" {
		return t
	}
	return "我在呢，稍等我一下。"
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

const companionRedundantWebSkipMsg = "ok:true\n已经有搜索摘要。不要再搜、不要打开网页，用现有结果用一两句说出气温和阴晴。"

// companionRedundantWebSkip drops a second weather-style lookup. One
// successful web.search is enough; fetch stays only when the user pasted a URL.
func companionRedundantWebSkip(companion bool, lastTools []string, next, userText string, searchSeen bool) (string, bool) {
	if !companion {
		return "", false
	}
	if next != "web.search" && next != "web.fetch" {
		return "", false
	}
	hadSearch := searchSeen
	if !hadSearch {
		for _, t := range lastTools {
			if t == "web.search" {
				hadSearch = true
				break
			}
		}
	}
	if !hadSearch {
		return "", false
	}
	if next == "web.search" {
		return companionRedundantWebSkipMsg, true
	}
	lower := strings.ToLower(userText)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return "", false
	}
	return companionRedundantWebSkipMsg, true
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
	if companionToolResultFailed(out) {
		if strings.Contains(out, "BROWSER_MCP_NOT_READY") {
			return companionBrowserMCPSpeech
		}
		if strings.Contains(out, "M10-CC-012") || strings.Contains(out, "电脑控制未启用") {
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
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"搜索", "搜一下", "搜网页", "打开", "把开", "播放", "播一首", "播歌", "听歌", "放一首",
		"查一下", "查询", "查火车", "查航班", "火车票", "航班", "查天气", "weather",
		"建文件夹", "创建文件夹", "写文件", "安装", "插件", "技能",
		"mcp", "运行命令", "打开网页", "浏览器", "下载",
		"启动", "运行", "软件", "汽水音乐", "网易云",
		"截图", "屏幕", "对话框", "点击", "鼠标",
		"填写", "填一下", "填表", "输入", "写入", "打字", "随机播放",
		"下一步", "再点", "接着", "帮我点", "帮我做",
		"生图", "画一张", "画图", "生成图片", "生成视频", "生视频", "做个视频",
		"search", "open http", "play song", "install", "generate image", "generate video",
	} {
		if strings.Contains(text, needle) || strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
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
