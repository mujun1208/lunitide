package producthub

func Seed() []Card {
	out := append([]Card{}, briefCards()...)
	out = append(out, detailedCards()...)
	out = append(out, landscapeCards()...)
	return out
}

func brief(key, name, nameEN, domain, module, class, summary string) Card {
	return Card{
		StableKey: key, Name: name, NameEN: nameEN, Domain: domain, Module: module,
		Summary: summary, ChainClass: class, Source: "seed", Provenance: "seed", Version: "seed.v1",
	}
}

func briefCards() []Card {
	return []Card{
		brief("feature.dialog.companion.voice-talk", "月伴语音对话", "Companion voice", "dialog", "companion", "intent-control", "唤醒后用语音连续对话。"),
		brief("feature.dialog.companion.wake", "唤醒月伴", "Wake companion", "dialog", "companion", "intent-control", "语音唤醒词打开月伴。"),
		brief("feature.dialog.companion.barge-in", "语音插话", "Barge-in", "dialog", "companion", "intent-control", "播报中插入新指令。"),
		brief("feature.dialog.chat.type", "打字聊天", "Type a message", "dialog", "chat", "crud-bridge", "在对话框输入并发送。"),
		brief("feature.dialog.chat.mention-skill", "对话里 @技能", "Mention a skill", "dialog", "chat", "asset-invoke", "用 @ 挂载技能。"),
		brief("feature.dialog.chat.mention-expert", "对话里 @专家", "Mention an expert", "dialog", "chat", "asset-invoke", "用 @ 挂载专家。"),
		brief("feature.dialog.session.new", "新建对话", "New chat", "dialog", "session", "page-enter", "开一条新会话。"),
		brief("feature.dialog.session.search", "搜索对话", "Search chats", "dialog", "session", "crud-bridge", "按标题或内容搜会话。"),
		brief("feature.dialog.session.delete", "删除对话", "Delete chat", "dialog", "session", "crud-bridge", "删除一条会话。"),
		brief("feature.dialog.session.rename", "重命名对话", "Rename chat", "dialog", "session", "crud-bridge", "改会话标题。"),
		brief("feature.dialog.music.toggle", "播放/暂停切换", "Play/pause toggle", "dialog", "companion", "media-transport", "切换播放与暂停。"),
		brief("feature.dialog.music.prev", "上一曲", "Previous track", "dialog", "companion", "media-transport", "切到上一首。"),
		brief("feature.dialog.music.stop", "停止播放", "Stop", "dialog", "companion", "media-transport", "停止当前曲目。"),
		brief("feature.dialog.music.seek", "跳转到进度", "Seek", "dialog", "companion", "media-transport", "跳到指定进度。"),
		brief("feature.dialog.music.volume", "调节音量", "Set volume", "dialog", "companion", "media-transport", "调节播放音量。"),
		brief("feature.dialog.music.mute", "静音", "Mute", "dialog", "companion", "media-transport", "将当前会话静音。"),
		brief("feature.dialog.file.pick", "选择本地文件", "Pick local file", "dialog", "companion", "file-open", "弹出本机文件选择框。"),
		brief("feature.dialog.computer.screenshot", "截一张屏", "Screenshot", "dialog", "companion", "intent-control", "截取当前屏幕。"),
		brief("feature.dialog.computer.click", "点击屏幕位置", "Click on screen", "dialog", "companion", "intent-control", "按坐标点击本机界面。"),
		brief("feature.office.studio.task-create", "创建办公任务", "Create office task", "office", "office", "crud-bridge", "在办公工作台新建任务。"),
		brief("feature.office.studio.artifact-export", "导出办公产物", "Export office artifact", "office", "office", "crud-bridge", "导出文档或表格产物。"),
		brief("feature.office.automation.job-set", "保存自动化任务", "Save automation job", "office", "automation", "crud-bridge", "写入一条自动化。"),
		brief("feature.office.automation.job-trigger", "立刻跑自动化", "Trigger automation", "office", "automation", "asset-invoke", "立即触发已保存任务。"),
		brief("feature.office.people.send-file", "给同事发文件", "Send file to colleague", "office", "people", "file-open", "把本机文件发到同事会话。"),
		brief("feature.office.mro.plan", "生成机务计划", "Build MRO plan", "office", "mro", "crud-bridge", "根据手册与状态生成计划。"),
		brief("feature.office.meetings.transcribe", "转写会议音频", "Transcribe meeting", "office", "meetings", "meeting-pipeline", "把会议录音转成文字。"),
		brief("feature.office.meetings.todo", "抽出会议待办", "Extract meeting todos", "office", "meetings", "meeting-pipeline", "从纪要抽出待办。"),
		brief("feature.assets.expert.try", "试用专家", "Try expert", "assets", "expert", "asset-invoke", "打开专家试用会话。"),
		brief("feature.assets.skill.install", "安装技能", "Install skill", "assets", "skill", "asset-invoke", "安装一个技能包。"),
		brief("feature.assets.skill.invoke", "调用技能", "Invoke skill", "assets", "skill", "asset-invoke", "在对话中执行技能。"),
		brief("feature.assets.plugin.enable", "启用插件", "Enable plugin", "assets", "plugins", "settings-toggle", "打开或关闭插件。"),
		brief("feature.assets.mcp.connect", "连接 MCP", "Connect MCP", "assets", "mcp", "asset-invoke", "接入一个 MCP 端点。"),
		brief("feature.assets.mcp.invoke", "调用 MCP 工具", "Invoke MCP tool", "assets", "mcp", "asset-invoke", "调用已连接的工具。"),
		brief("feature.assets.memory.capture", "写入一条记忆", "Capture memory", "assets", "memory", "crud-bridge", "把事实写入记忆库。"),
		brief("feature.assets.ocr.document", "OCR 识别文档", "OCR a document", "assets", "ocr", "asset-invoke", "把文档页打成字。"),
		brief("feature.execution.workspace.save", "保存工作区文件", "Save workspace file", "execution", "workspace", "crud-bridge", "把编辑器内容写回工作区。"),
		brief("feature.execution.browser.snapshot", "浏览器快照", "Browser snapshot", "execution", "browser", "asset-invoke", "抓当前页可访问树。"),
		brief("feature.execution.computer.launch", "电脑控制启动应用", "Computer launch app", "execution", "computer", "intent-control", "用电脑控制打开应用。"),
		brief("feature.execution.computer.mouse", "电脑控制键鼠", "Computer input", "execution", "computer", "intent-control", "键鼠操作本机界面。"),
		brief("feature.execution.channels.send", "发到消息通道", "Send to IM channel", "execution", "channels", "crud-bridge", "把消息发到已配置通道。"),
		brief("feature.execution.subagent.spawn", "拉起子智能体", "Spawn subagent", "execution", "subagents", "asset-invoke", "分派一个子智能体任务。"),
		brief("feature.execution.agenthub.file-open", "Agent Hub 打开文件", "Agent Hub open file", "execution", "agenthub", "file-open", "在 Agent Hub 打开工作区文件。"),
		brief("feature.execution.agenthub.task-start", "Agent Hub 开工", "Start Agent Hub task", "execution", "agenthub", "asset-invoke", "启动一条 Agent Hub 任务。"),
		brief("feature.foundation.provider.add", "添加模型供应商", "Add provider", "foundation", "providers", "crud-bridge", "登记一个模型供应商。"),
		brief("feature.foundation.provider.test", "测试供应商连通", "Test provider", "foundation", "providers", "crud-bridge", "探测供应商是否可用。"),
		brief("feature.foundation.update.check", "检查更新", "Check for updates", "foundation", "diagnostics", "crud-bridge", "检查产品更新。"),
		brief("feature.foundation.token.compact", "Token 精简", "Compact tokens", "foundation", "diagnostics", "crud-bridge", "压缩长对话上下文。"),
	}
}

func landscapeCards() []Card {
	comp := closedChain([]Step{
		{1, "选定对照维", "语音本机控制 / 媒体核验 / 技能MCP / 知识自描述", "只比较可核验的产品行为，不比营销口号。"},
		{2, "收集出处", "公开文档或实测", "每条结论必须带来源与日期。"},
		{3, "写成槽位卡", "landscape", "写入本模块图景，不计入健康度。"},
	}, 3, "对照表可阅读且每行有出处", "缺出处或无法复测", "补来源再写", "标为未证实，不进核心清单")
	front := closedChain([]Step{
		{1, "观察前沿", "媒体核验", "以 SMTC / owned runtime 为播放真相。"},
		{2, "对照本产品", "探针", "看现有媒体链路是否仍把「键已发送」当成功。"},
		{3, "记入图景", "landscape", "给后续迭代，不自动改业务代码。"},
	}, 3, "观察写成可验证条目", "无实测锚点", "补 winexec/SMTC 引用", "保持槽位，不写无出处结论")
	return []Card{
		{
			StableKey: "landscape.competitor.desktop-assistants", Name: "竞品对照：桌面助手", NameEN: "Competitive desktop assistants",
			Domain: "foundation", Module: "landscape", Source: "landscape", ChainClass: "diagnose-only",
			Summary: "对照 Copilot / ChatGPT Desktop / Claude Desktop：本机优先、媒体核验、技能MCP、知识自描述。",
			Description: "2026-09-21 初版对照（来源：各产品公开桌面端说明与 Lunitide 活源扫描，非营销排名）。1) 本机优先：Lunitide 引擎与 SQLite 在本机，竞品多以云会话为主。2) 媒体核验：Lunitide 以 SMTC/owned runtime 回读 playing，竞品常见「键已发送」。3) 技能/MCP：三家均有插件或 MCP 入口；Lunitide 把技能、专家、MCP、插件收成同一资产域并进知识卡。4) 知识自描述：本模块用种子+活源生成说明书；竞品无对等的产品本体中枢。结论必须带来源与日期，禁止无出处排名。不计入健康分。",
			Methods: []Method{{Type: "menu", Entry: "图景页阅读对照表"}},
			Chain: comp, Provenance: "seed", Version: "seed.v1", Tags: []string{"status:实验", "kind:竞品"},
			Principle: "只比较可核验行为，不比口号。",
			Logic:     "选定四维 → 收集公开出处 → 写成图景卡。",
			Tech:      "对照维：语音本机控制 / 媒体核验 / 技能MCP / 知识自描述。",
			Analysis:  "2026-09-21：Lunitide 差异在本机核验与产品自描述；云助手强在通用问答与生态分发。",
		},
		{
			StableKey: "landscape.frontier.smtc-verification", Name: "前沿：播放核验", NameEN: "Frontier SMTC verification",
			Domain: "foundation", Module: "landscape", Source: "landscape", ChainClass: "diagnose-only",
			Summary: "以 SMTC/owned runtime 为播放真相，而不是键发了就算成功。",
			Description: "2026-09-21 观察（来源：Windows SMTC 文档与本产品 media operation 核验路径）。桌面助手普遍把 VK_MEDIA_PLAY 派发当成已播放。前沿做法是回读 System Media Transport Controls 会话或自有播放器事件（verified_playing）。Lunitide 媒体链路已按此设计；诊断报告盯的是「仅 command_dispatched」缺口。本槽位驱动迭代，不自动改 winexec。",
			Methods: []Method{{Type: "menu", Entry: "图景页阅读观察"}},
			Chain: front, Provenance: "seed", Version: "seed.v1", Tags: []string{"status:实验", "kind:前沿"},
			Principle: "播放成功 = 相位回读，不是按键回执。",
			Logic:     "观察前沿 → 对照本产品探针 → 记入图景。",
			Tech:      "SMTC、owned runtime、media.play 核验状态机。",
			Analysis:  "下一步：诊断报告里把未核验播放标为 warn，交给内部模型出补丁计划。",
		},
		{
			StableKey: "landscape.frontier.self-heal-agents", Name: "前沿：自净化与知识图谱", NameEN: "Frontier self-heal graph",
			Domain: "foundation", Module: "landscape", Source: "landscape", ChainClass: "diagnose-only",
			Summary: "产品用本体+图谱描述自己，再用诊断报告驱动内部模型/技能修复。",
			Description: "2026-09-21 探索（来源：本模块实现与业界 agent self-heal / docs-as-code 方向）。同类技术从静态文档走向「系统自描述 + 只读诊断 + 人机闭环补丁」。本里程碑落地诊断报告与可复制给内部模型/技能的任务书，不自动改 Go/TS。下次生成把已修复条目标 fixed，形成迭代。",
			Methods: []Method{{Type: "menu", Entry: "诊断页复制给内部模型"}},
			Chain: front, Provenance: "seed", Version: "seed.v1", Tags: []string{"status:实验", "kind:前沿"},
			Principle: "先看见自己，再改自己。",
			Logic:     "扫描活源 → 出诊断 → 复制给模型/技能 → 人工落地 → 再生成验证。",
			Tech:      "13 类本体、stable_key 合并、PH_* 诊断码、Bridge productHub.*。",
			Analysis:  "2026-09-21：中枢已执行本地净化（标签/入口方法/状态）并把任务书交给已发布技能；不自动改写 Go/TS。",
		},
	}
}
