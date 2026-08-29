package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
)

// specialistRuntimeInstruction is injected on 选专家 / Expert Center / council
// turns so clipped six-section bodies cannot silently drop tools. Main chat
// already exposes office+web+skills; this forbids prompt-only answers.
func specialistRuntimeInstruction() string {
	return "\n\n[对话专家能力]\n" +
		"挂载的对话专家必须真正动手，不要只改提示词口头交差：\n" +
		"- 思考：任务过程写在思考通道，用 todo.write 记下步骤并做完。\n" +
		"- 技能：匹配目录立刻 skill.invoke，不要等用户再说“用技能”。\n" +
		"- 检索：事实、素材、行情、出处先 web.search，必要时 web.fetch 或 browser.act，禁止编造。\n" +
		"- 画图：结构/流程/架构用 markdown mermaid（节点双引号，换行 <br/>）。\n" +
		"- 成文：docx.gen / excel.gen / pptx.gen / html.gen / workspace.write；放到桌面 desktop=true。\n" +
		"- 派出子智能体时给全部只读能力（fs + web + browser + evidence），不要缩成仅网络检索。\n" +
		"不要倾倒 200 页全书或 200 条空用例。PPT 仍走产品九步流水线，禁止空页。报告走调研与章节流水线，小说走大纲与分章正文流水线，禁止跳步 docx.gen 交空稿或只有提纲的 Word。\n"
}

func specialistPersonaCapabilityLine() string {
	return "任务过程中思考；匹配技能立刻 skill.invoke；事实先 web.search（必要时 web.fetch / browser.act）；结构图画 mermaid；成文用 docx.gen / excel.gen / pptx.gen / html.gen（桌面 desktop=true）。禁止只口头交差或倾倒 200 页全书。"
}

var specialistToolAllow = map[string]bool{
	"workspace.list": true, "workspace.read": true, "workspace.write": true,
	"workspace.search": true, "workspace.edit": true,
	"todo.write": true, "user.ask": true, "command.run": true,
	"web.fetch": true, "web.search": true, "browser.act": true,
	"excel.gen": true, "excel.parse": true, "docx.gen": true, "pptx.gen": true,
	"pdf.gen": true, "html.gen": true,
	"skill.invoke":   true,
	"image.generate": true,
}

func specialistToolDefinitions(all []gateway.ToolDefinition) []gateway.ToolDefinition {
	return filterToolDefs(all, specialistToolAllow)
}

func specialistToolNames(defs []gateway.ToolDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

func hasSpecialistOfficeAndWeb(defs []gateway.ToolDefinition) bool {
	need := []string{"web.search", "web.fetch", "excel.gen", "docx.gen", "pptx.gen", "html.gen", "skill.invoke"}
	have := toolNameSet(defs)
	for _, name := range need {
		if !have[name] {
			return false
		}
	}
	return true
}

func councilChairMustUseTools(instruction string) bool {
	return strings.Contains(instruction, "web.search") &&
		strings.Contains(instruction, "docx.gen") &&
		strings.Contains(instruction, "skill.invoke")
}
