package producthub

import "github.com/lunitide/lunitide/internal/producthub/generated"

// LiveCatalog is the product scanning itself. First edition lives in Seed().
// Each Generate() re-reads this list so a new page / setting / media action /
// plugin appears without rewriting the seed booklet.
func LiveCatalog() []Candidate {
	var out []Candidate
	out = append(out, pageCandidates()...)
	out = append(out, settingsCandidates()...)
	out = append(out, mediaActionCandidates()...)
	out = append(out, pluginCandidates()...)
	out = append(out, verbCandidates()...)
	out = append(out, extraVerbCandidates()...)
	return out
}

// pageCandidates turns every reachable frontend page into one card. The names
// and domains come from the generated catalog, which reads them out of the
// renderer's own PAGE_ATLAS: a page card's stable key is
// feature.<domain>.page.<id>, so a second copy of the domain maintained here
// would eventually disagree with the frontend and produce cards the hub can
// never match back to a page.
func pageCandidates() []Candidate {
	out := make([]Candidate, 0, len(generated.Pages))
	for _, p := range generated.Pages {
		out = append(out, Candidate{
			StableKey:  "feature." + p.Domain + ".page." + p.ID,
			Name:       "进入" + p.Name,
			NameEN:     "Open " + p.NameEN,
			Domain:     p.Domain,
			Module:     p.Module,
			Summary:    "打开「" + p.Name + "」页面。",
			Source:     "pages",
			ChainClass: "page-enter",
			Scaffold:   Scaffold{Pages: []string{p.ID}},
		})
	}
	return out
}

func settingsCandidates() []Candidate {
	var out []Candidate
	for _, it := range generated.Settings {
		out = append(out, Candidate{
			StableKey:  "feature.foundation.settings." + it.ID,
			Name:       it.Name + "设置",
			NameEN:     it.NameEN,
			Domain:     "foundation",
			Module:     "settings",
			Summary:    "调整「" + it.Name + "」设置。",
			Source:     "settings",
			ChainClass: "settings-toggle",
			Scaffold:   Scaffold{Pages: []string{"settings"}, Settings: []string{it.ID}},
		})
	}
	return out
}

func mediaActionCandidates() []Candidate {
	actions := generated.MediaActions
	names := map[string]string{
		"play": "播放", "pause": "暂停", "toggle": "播放暂停切换", "stop": "停止",
		"previous": "上一首", "next": "下一首", "seek": "跳转进度", "set_volume": "音量",
		"mute": "静音", "unmute": "取消静音", "create": "新建媒体会话", "move": "调整队列",
		"remove": "移出队列", "clear": "清空队列", "jump": "跳到队列项",
	}
	var out []Candidate
	for _, a := range actions {
		out = append(out, Candidate{
			StableKey:  "feature.office.media." + a,
			Name:       "媒体中心" + names[a],
			NameEN:     "Media " + a,
			Domain:     "office",
			Module:     "media",
			Summary:    "对媒体会话执行 " + names[a] + "。",
			Source:     "media-actions",
			ChainClass: "media-transport",
			Scaffold:   Scaffold{Pages: []string{"media"}, Bridge: []string{"media." + a}, Runtime: []string{"smtc", "owned_runtime"}},
			Attributes: Attributes{Operations: []string{"媒体控制"}, Tools: []string{"media." + a}, Capabilities: []string{"capability.media.smtc"}},
		})
	}
	out = append(out, Candidate{
		StableKey: "feature.office.media.asset-open", Name: "打开媒体资产", NameEN: "Open media asset",
		Domain: "office", Module: "media", Summary: "打开已授权的音视频资产。",
		Source: "bridge", ChainClass: "media-transport",
		Scaffold: Scaffold{Pages: []string{"media"}, Bridge: []string{"media.asset.open"}},
	})
	return out
}

func pluginCandidates() []Candidate {
	plugins := []struct{ id, title string }{
		{"llm", "LLM"}, {"session", "Session"}, {"jobs-local", "Local Jobs"},
		{"web-search-deepseek", "DeepSeek 网页搜索"}, {"tool-bash", "Bash"},
		{"tool-pwsh", "PowerShell"}, {"tool-cmd", "CMD"}, {"tool-python", "Python"},
		{"web-search", "网页搜索"}, {"web-fetch", "抓取网页"}, {"workspace", "工作区"},
		{"filesystem", "文件系统"}, {"git", "Git"}, {"browser", "浏览器"},
		{"agent-loop", "Agent 循环"}, {"thinking", "思考链"}, {"memory", "记忆"},
		{"skills", "技能"}, {"cron", "定时任务"}, {"clipboard", "剪贴板"},
		{"notification", "通知"}, {"tts", "语音合成"}, {"stt", "语音识别"},
	}
	var out []Candidate
	for _, p := range plugins {
		out = append(out, Candidate{
			StableKey: "feature.assets.plugin." + p.id, Name: "插件：" + p.title, NameEN: p.id,
			Domain: "assets", Module: "plugins", Summary: "启用或使用「" + p.title + "」插件。",
			Source: "plugins", ChainClass: "asset-invoke",
			Scaffold: Scaffold{Pages: []string{"plugins"}, Runtime: []string{"harness:" + p.id}},
			Attributes: Attributes{Operations: []string{"插件"}, Tools: []string{p.id}},
		})
	}
	return out
}

func verbCandidates() []Candidate {
	return []Candidate{
		verb("feature.dialog.companion.voice-talk", "月伴语音对话", "Companion voice", "dialog", "companion", "intent-control", []string{"home"}, []string{"voice"}, []string{"capability.stt.asr", "capability.tts.voice"}),
		verb("feature.dialog.companion.wake", "唤醒月伴", "Wake companion", "dialog", "companion", "intent-control", []string{"home"}, []string{"voice"}, nil),
		verb("feature.dialog.companion.barge-in", "语音插话", "Barge-in", "dialog", "companion", "intent-control", []string{"home"}, []string{"voice"}, nil),
		verb("feature.dialog.chat.type", "打字聊天", "Type a message", "dialog", "chat", "crud-bridge", []string{"home"}, nil, []string{"message.append"}),
		verb("feature.dialog.session.new", "新建对话", "New chat", "dialog", "session", "page-enter", []string{"home"}, nil, nil),
		verb("feature.dialog.music.open-player", "打开音乐播放软件", "Open music player", "dialog", "companion", "intent-control", []string{"home", "media"}, []string{"computer"}, []string{"computer.control"}),
		verb("feature.dialog.music.play", "放歌", "Play track", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.play"}),
		verb("feature.dialog.music.pause", "暂停播放", "Pause", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.pause"}),
		verb("feature.dialog.music.next", "下一曲", "Next track", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.next"}),
		verb("feature.dialog.music.prev", "上一曲", "Previous track", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.previous"}),
		verb("feature.dialog.music.search", "搜索并播放歌曲", "Search and play", "dialog", "companion", "intent-control", []string{"home", "media"}, nil, []string{"media.play"}),
		verb("feature.dialog.file.open", "打开文件", "Open a file", "dialog", "companion", "file-open", []string{"home", "office"}, nil, []string{"desktop.files.readChunk", "people.file.open", "agentHub.file.open"}),
		verb("feature.dialog.file.pick", "选择本地文件", "Pick local file", "dialog", "companion", "file-open", []string{"home"}, nil, []string{"people.file.pick"}),
		verb("feature.dialog.app.launch", "打开电脑应用", "Launch app", "dialog", "companion", "intent-control", []string{"home"}, []string{"computer"}, []string{"computer.control"}),
		verb("feature.office.meetings.start", "开始会议听写", "Start meeting", "office", "meetings", "meeting-pipeline", []string{"meetings"}, []string{"meetings", "voice"}, []string{"meetings.start"}),
		verb("feature.office.meetings.summary", "生成会议纪要", "Summarize meeting", "office", "meetings", "meeting-pipeline", []string{"meetings"}, []string{"meetings"}, []string{"meetings.summary.source.get"}),
		verb("feature.office.mro.search-manual", "检索机务手册", "Search MRO manual", "office", "mro", "asset-invoke", []string{"mro"}, nil, []string{"mro.manual.register"}),
		verb("feature.office.people.open-file", "打开同事发来的文件", "Open received file", "office", "people", "file-open", []string{"people"}, []string{"profile"}, []string{"people.file.open"}),
		verb("feature.assets.memory.recall", "召回记忆", "Recall memory", "assets", "memory", "asset-invoke", []string{"assets", "settings"}, []string{"personal"}, []string{"memory.search"}),
		verb("feature.assets.ocr.screenshot", "OCR 识别截图", "OCR screenshot", "assets", "ocr", "asset-invoke", []string{"assets", "settings"}, []string{"personal"}, []string{"ocr.routing.get"}),
		verb("feature.execution.command.run", "执行白名单命令", "Run command", "execution", "command", "asset-invoke", []string{"settings"}, []string{"security"}, []string{"security"}),
		verb("feature.execution.workspace.open-file", "在工作区打开文件", "Open workspace file", "execution", "workspace", "file-open", []string{"home"}, nil, []string{"desktop.files.readChunk"}),
		verb("feature.execution.browser.navigate", "浏览器打开网址", "Browser navigate", "execution", "browser", "asset-invoke", []string{"settings"}, []string{"browser"}, []string{"br.navigate"}),
		verb("feature.foundation.diagnostics.health", "查看系统健康", "System health", "foundation", "diagnostics", "diagnose-only", []string{"settings"}, []string{"diagnostics"}, []string{"system.health"}),
	}
}

func extraVerbCandidates() []Candidate {
	return []Candidate{
		verb("feature.dialog.chat.mention-skill", "对话里 @技能", "Mention a skill", "dialog", "chat", "asset-invoke", []string{"home", "skill"}, []string{"security"}, []string{"skill.invoke"}),
		verb("feature.dialog.chat.mention-expert", "对话里 @专家", "Mention an expert", "dialog", "chat", "asset-invoke", []string{"home", "expert"}, []string{"personal"}, []string{"session.experts.set"}),
		verb("feature.dialog.session.search", "搜索对话", "Search chats", "dialog", "session", "crud-bridge", []string{"home"}, nil, []string{"message.search"}),
		verb("feature.dialog.session.delete", "删除对话", "Delete chat", "dialog", "session", "crud-bridge", []string{"home"}, nil, []string{"session.delete"}),
		verb("feature.dialog.session.rename", "重命名对话", "Rename chat", "dialog", "session", "crud-bridge", []string{"home"}, nil, []string{"session.update"}),
		verb("feature.dialog.music.toggle", "播放/暂停切换", "Play/pause toggle", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.toggle"}),
		verb("feature.dialog.music.stop", "停止播放", "Stop", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.stop"}),
		verb("feature.dialog.music.seek", "跳转到进度", "Seek", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.seek"}),
		verb("feature.dialog.music.volume", "调节音量", "Set volume", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.set_volume"}),
		verb("feature.dialog.music.mute", "静音", "Mute", "dialog", "companion", "media-transport", []string{"home", "media"}, nil, []string{"media.mute"}),
		verb("feature.dialog.computer.screenshot", "截一张屏", "Screenshot", "dialog", "companion", "intent-control", []string{"home"}, []string{"computer"}, []string{"computer.control"}),
		verb("feature.dialog.computer.click", "点击屏幕位置", "Click on screen", "dialog", "companion", "intent-control", []string{"home"}, []string{"computer"}, []string{"computer.control"}),
		verb("feature.office.studio.task-create", "创建办公任务", "Create office task", "office", "office", "crud-bridge", []string{"office"}, []string{"office-menu"}, []string{"office.task.create"}),
		verb("feature.office.studio.artifact-export", "导出办公产物", "Export office artifact", "office", "office", "crud-bridge", []string{"office"}, []string{"office-menu"}, []string{"office.artifact.export"}),
		verb("feature.office.automation.job-set", "保存自动化任务", "Save automation job", "office", "automation", "crud-bridge", []string{"automation"}, []string{"office-menu"}, []string{"automation.job.set"}),
		verb("feature.office.automation.job-trigger", "立刻跑自动化", "Trigger automation", "office", "automation", "asset-invoke", []string{"automation"}, []string{"office-menu"}, []string{"automation.job.trigger"}),
		verb("feature.office.people.send-file", "给同事发文件", "Send file to colleague", "office", "people", "file-open", []string{"people"}, []string{"office-menu", "profile"}, []string{"people.thread.send"}),
		verb("feature.office.mro.plan", "生成机务计划", "Build MRO plan", "office", "mro", "crud-bridge", []string{"mro"}, []string{"office-menu"}, []string{"mro.plan.publish"}),
		verb("feature.office.meetings.transcribe", "转写会议音频", "Transcribe meeting", "office", "meetings", "meeting-pipeline", []string{"meetings"}, []string{"meetings", "voice"}, []string{"meetings.start"}),
		verb("feature.office.meetings.todo", "抽出会议待办", "Extract meeting todos", "office", "meetings", "meeting-pipeline", []string{"meetings"}, []string{"meetings"}, []string{"meetings.summary.source.get"}),
		verb("feature.assets.expert.try", "试用专家", "Try expert", "assets", "expert", "asset-invoke", []string{"expert", "home"}, []string{"personal"}, nil),
		verb("feature.assets.skill.install", "安装技能", "Install skill", "assets", "skill", "asset-invoke", []string{"skill"}, []string{"security"}, []string{"skill.package.upload.commit"}),
		verb("feature.assets.skill.invoke", "调用技能", "Invoke skill", "assets", "skill", "asset-invoke", []string{"skill", "home"}, []string{"security"}, []string{"skill.invoke"}),
		verb("feature.assets.plugin.enable", "启用插件", "Enable plugin", "assets", "plugins", "settings-toggle", []string{"plugins"}, nil, []string{"plugin.toggle"}),
		verb("feature.assets.mcp.connect", "连接 MCP", "Connect MCP", "assets", "mcp", "asset-invoke", []string{"mcp"}, []string{"security"}, []string{"mcp.security.review"}),
		verb("feature.assets.mcp.invoke", "调用 MCP 工具", "Invoke MCP tool", "assets", "mcp", "asset-invoke", []string{"mcp"}, []string{"security"}, nil),
		verb("feature.assets.memory.capture", "写入一条记忆", "Capture memory", "assets", "memory", "crud-bridge", []string{"assets", "settings"}, []string{"personal"}, []string{"memory.create"}),
		verb("feature.assets.ocr.document", "OCR 识别文档", "OCR a document", "assets", "ocr", "asset-invoke", []string{"assets", "settings"}, []string{"personal"}, []string{"ocr.routing.get"}),
		verb("feature.execution.workspace.save", "保存工作区文件", "Save workspace file", "execution", "workspace", "crud-bridge", []string{"home"}, nil, []string{"desktop.files.readChunk"}),
		verb("feature.execution.browser.snapshot", "浏览器快照", "Browser snapshot", "execution", "browser", "asset-invoke", []string{"settings"}, []string{"browser"}, []string{"br.navigate"}),
		verb("feature.execution.computer.launch", "电脑控制启动应用", "Computer launch app", "execution", "computer", "intent-control", []string{"settings", "home"}, []string{"computer"}, []string{"computer.control"}),
		verb("feature.execution.computer.mouse", "电脑控制键鼠", "Computer input", "execution", "computer", "intent-control", []string{"settings", "home"}, []string{"computer"}, []string{"computer.control"}),
		verb("feature.execution.channels.send", "发到消息通道", "Send to IM channel", "execution", "channels", "crud-bridge", []string{"settings"}, []string{"channels"}, nil),
		verb("feature.execution.subagent.spawn", "拉起子智能体", "Spawn subagent", "execution", "subagents", "asset-invoke", []string{"settings"}, []string{"subagents"}, []string{"agent.run.start"}),
		verb("feature.execution.agenthub.file-open", "Agent Hub 打开文件", "Agent Hub open file", "execution", "agenthub", "file-open", []string{"agentHub"}, nil, []string{"agentHub.file.open"}),
		verb("feature.execution.agenthub.task-start", "Agent Hub 开工", "Start Agent Hub task", "execution", "agenthub", "asset-invoke", []string{"agentHub"}, nil, []string{"agentHub.task.start"}),
		verb("feature.foundation.provider.add", "添加模型供应商", "Add provider", "foundation", "providers", "crud-bridge", []string{"providers", "settings"}, []string{"providers", "routing"}, []string{"provider.create"}),
		verb("feature.foundation.provider.test", "测试供应商连通", "Test provider", "foundation", "providers", "crud-bridge", []string{"providers", "settings"}, []string{"providers", "routing"}, []string{"provider.test"}),
		verb("feature.foundation.update.check", "检查更新", "Check for updates", "foundation", "diagnostics", "crud-bridge", []string{"settings"}, []string{"diagnostics"}, []string{"appUpdate.check"}),
		verb("feature.foundation.token.compact", "Token 精简", "Compact tokens", "foundation", "diagnostics", "crud-bridge", []string{"home", "settings"}, []string{"diagnostics"}, nil),
	}
}

func verb(key, name, nameEN, domain, module, class string, pages, settings, bridge []string) Candidate {
	return Candidate{
		StableKey: key, Name: name, NameEN: nameEN, Domain: domain, Module: module,
		Summary: name + "。", Source: "verbs", ChainClass: class,
		Scaffold:   Scaffold{Pages: pages, Settings: settings, Bridge: bridge},
		Attributes: Attributes{Operations: []string{name}, Tools: bridge},
	}
}
