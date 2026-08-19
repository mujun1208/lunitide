package app

// bundledWorkflowInjection is the OpenClaw/Hermes-style always-on skill
// pack: compact native-tool workflows that need no install, no MCP, and
// no extra schema. Injected on ordinary chat only (companion stays lean).
func bundledWorkflowInjection() string {
	return "\n\n[内置工作流] 开箱即用，直接调用已有工具，不必安装技能、不必 MCP：\n" +
		"- 调研：web.search 检索，再 web.fetch 阅读候选页；结论分点并带来源链接。\n" +
		"- 改代码：workspace.search 定位 → workspace.read → workspace.edit 精确替换 → command.run 验证。\n" +
		"- 审查：读改动与周边代码，按 严重/建议 分级，引用 path:line。\n" +
		"- 文档：docx.gen / pptx.gen / excel.gen / pdf.gen 写入工作区，并汇报路径。\n" +
		"- 任务：todo.write 维护清单；复杂请求先拆步再动手，每步可验证。\n" +
		"- Git：command.run 只用白名单只读 git（status/diff/log）；写操作需用户确认。\n" +
		"- 浏览：公开页用 browser.act（navigate/read，与 web.fetch 同一套 SSRF 防护）；要点击或填表时在设置启用 Playwright MCP，或用工作区「浏览器」标签打开独立窗口。\n" +
		"使用规则：匹配上述场景时直接执行。用户已发布的技能见下方目录，用 skill.invoke。\n"
}
