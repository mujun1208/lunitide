package producthub

func defaultMethods(class string) []Method {
	switch class {
	case "intent-control", "media-transport", "file-open", "meeting-pipeline":
		return []Method{
			{Type: "voice", Entry: "对月伴说出该动作", Continuous: "支持连续指令"},
			{Type: "typing", Entry: "在对话框输入该动作"},
		}
	case "page-enter":
		return []Method{{Type: "menu", Entry: "左侧栏或深链进入"}}
	case "settings-toggle":
		return []Method{{Type: "settings", Entry: "设置页对应分类"}}
	default:
		return []Method{{Type: "menu", Entry: "从对应页面或对话触发"}}
	}
}

func defaultChain(class, name string) Chain {
	switch class {
	case "intent-control":
		return closedChain([]Step{
			{1, "用户输入", "语音/文字", "用户说出或打出「" + name + "」；语音经 VAD 断句。"},
			{2, "意图理解", "LLM", "解析为对应本机/媒体意图，低置信则反问。"},
			{3, "绑定底座", "工具/Bridge", "绑定脚手架里的工具与权限模式。"},
			{4, "执行", "运行时", "派发控制或打开动作。"},
			{5, "核验", "回读", "窗口/SMTC/owned runtime 确认，不把「已发送」当成成功。"},
		}, 5, "核验通过并反馈用户", "未安装、超时或权限拒绝", "换备用启动路径再试", "播报原因并给出可点的替代入口")
	case "media-transport":
		return closedChain([]Step{
			{1, "选定会话", "媒体会话", "使用当前媒体中心会话或前台播放器。"},
			{2, "发操作", "operation", "写入 " + name + " 的媒体动作，带幂等键。"},
			{3, "派发", "runtime", "自有播放器或系统媒体键，禁止双通道连发。"},
			{4, "核验", "snapshot", "SMTC 或 owned runtime 回读相位。"},
		}, 4, "相位与核验一致，界面刷新", "无会话、审批拒绝或核验失败", "按 recovery 重试一次", "界面标「已发送、未核验」，不声称已播放")
	case "file-open":
		return closedChain([]Step{
			{1, "入口", "对话/工作区/同事", "从对话、工作区或附件点开。"},
			{2, "解析目标", "assetId", "用资产或用户点选解析，渲染进程不拼盘符路径。"},
			{3, "权限", "范围", "工作区或用户选择；越权拒绝。"},
			{4, "打开", "预览/关联应用", "产品内预览或系统关联应用。"},
			{5, "核验", "可见", "预览出现或外部窗口出现。"},
		}, 5, "文件已打开", "找不到、无权限或取消对话框", "再弹出选择框", "只揭示所在文件夹")
	case "page-enter":
		return closedChain([]Step{
			{1, "导航", "侧栏/深链", "用户点入口或带参数打开。"},
			{2, "门禁", "菜单/权限", "办公菜单显隐与能力开关。"},
			{3, "渲染", "页面", "挂载对应 Page。"},
			{4, "首屏", "数据", "拉首屏列表或空态。"},
		}, 4, "页面可用", "入口被隐藏或首屏失败", "提示去办公菜单打开", "停在当前页并说明原因")
	case "settings-toggle":
		return closedChain([]Step{
			{1, "打开设置", "settings", "进入设置页。"},
			{2, "定位分类", name, "找到对应分类。"},
			{3, "改值", "持久化", "写入设置并通知。"},
		}, 3, "值已保存且界面回显", "校验失败", "保持旧值并提示", "不改动其它分类")
	case "asset-invoke":
		return closedChain([]Step{
			{1, "选择资产", "清单", "从已安装技能/MCP/专家/命令中选。"},
			{2, "授权", "治理", "按风险弹审批或 FullAccess 直过。"},
			{3, "调用", "运行", "执行并拿回执。"},
		}, 3, "回执成功", "未安装或拒绝", "提示安装或启用", "对话继续，不假装已执行")
	case "meeting-pipeline":
		return closedChain([]Step{
			{1, "拾音", "麦克风", "检查设备与权限。"},
			{2, "转写", "ASR", "本机或已配置引擎出字。"},
			{3, "落库", "会议记录", "写入转写。"},
			{4, "生成", name, "摘要或待办。"},
		}, 4, "纪要可见", "无麦或无转写", "只保留录音稍后转写", "给出原文")
	case "diagnose-only":
		return closedChain([]Step{
			{1, "扫描", "活源+种子", "对照当前产品和初版清单。"},
			{2, "规则", "诊断器", "孤儿引用、漏卡、悬空链路。"},
			{3, "出报告", "文档", "四要素写入本页报告。"},
			{4, "净化", "apply", "管理员点执行净化：本地修目录，任务书交给内部技能/模型。"},
		}, 4, "报告可点开且可执行净化", "诊断失败", "保留上一份报告", "快照仍可用")
	default:
		return closedChain([]Step{
			{1, "请求", "Bridge", "调用对应方法。"},
			{2, "校验", "schema", "检查参数。"},
			{3, "执行", "引擎", "读写产品状态。"},
			{4, "回执", "DTO", "返回结果。"},
		}, 4, "回执成功", "参数或权限失败", "按错误码重试", "界面展示错误，不写脏数据")
	}
}

func closedChain(steps []Step, from int, success, failure, retry, fallback string) Chain {
	return Chain{
		Steps: steps,
		Branches: []Branch{
			{Type: "success", FromStep: from, Name: "成功", Description: success},
			{Type: "failure", FromStep: from, Name: "失败", Description: failure,
				Retry:    &Fork{Name: "重试", Description: retry, OnSuccess: "汇入成功", OnFail: "降级"},
				Fallback: &Fork{Name: "降级", Description: fallback},
			},
		},
	}
}
