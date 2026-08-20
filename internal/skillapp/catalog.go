// P3-2 skill catalog: a static, product-shipped template catalog the skill
// center exposes as a local "market". Templates carry no signature (they
// are seeds, not loadable code): installing materializes a draft skill
// through the normal skill.create pipeline so every governance rule
// (permissions review, publish gate, optimistic versioning) still applies.
package skillapp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

//go:embed bundled/skill-creator/SKILL.md
var skillCreatorSkillMD []byte

//go:embed bundled/expert-manager/SKILL.md
var expertManagerSkillMD []byte

// ErrTemplateUnknown answers an install request naming a catalog id that
// does not exist; ErrTemplateInstalled answers a name+version collision.
var (
	ErrTemplateUnknown   = errors.New("skillapp: template unknown")
	ErrTemplateInstalled = errors.New("skillapp: template already installed")
)

// CatalogTemplate is one installable entry in the product catalog.
// Bundled templates are published at engine start; the rest stay in the
// market until the user clicks install. Featured templates appear on the
// market shelf. Source is a short provenance label for the card.
type CatalogTemplate struct {
	ID          string
	Name        string
	DisplayName string
	Description string
	Category    string
	Version     string
	Permissions []skill.PermissionLevel
	EntryPoint  string
	Manifest    map[string]any
	Featured    bool
	Bundled     bool
	Source      string
}

// manifestFor builds the stored manifest JSON: the template fields plus the
// triggers/prompt the matcher and the model-facing guidance consume.
func manifestFor(t CatalogTemplate) string {
	m := map[string]any{}
	for k, v := range t.Manifest {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Catalog answers the product-shipped template catalog.
func Catalog() []CatalogTemplate {
	return catalogTemplates
}

func stripYAMLFrontmatter(raw string) string {
	raw = strings.TrimPrefix(raw, "\uFEFF")
	if !strings.HasPrefix(raw, "---") {
		return strings.TrimSpace(raw)
	}
	if end := strings.Index(raw[3:], "\n---"); end >= 0 {
		return strings.TrimSpace(raw[3+end+4:])
	}
	return strings.TrimSpace(raw)
}

func skillCreatorManifest() map[string]any {
	body := stripYAMLFrontmatter(string(skillCreatorSkillMD))
	lunitide := "\n\n--- Lunitide 集成 ---\n" +
		"完成技能设计后，使用 skill.create 写入技能中心（name、displayName、description、permissions、entryPoint=SKILL.md、manifestJson 含 triggers 与 prompt）。\n" +
		"可先用 workspace.read/write 在工作区起草 SKILL.md 与 scripts/；用户给出现成目录时直接读取并 skill.create。\n" +
		"skill.create 成功后必须立刻用中文明确告诉用户：已创建「名称」，请到技能中心安装并发布。然后停止本轮，不要继续未完成工作，不要再调用无关工具。\n" +
		"失败时用中文说明原因（重名、权限、清单无效等），不要沉默结束。"
	prompt := strings.TrimSpace(body) + lunitide
	if len(prompt) > 60000 {
		prompt = prompt[:60000]
	}
	return map[string]any{
		"triggers": []string{"创建技能", "新建技能", "写技能", "skill creator", "skill-creator", "优化技能", "改进技能", "create skill"},
		"prompt":   prompt,
	}
}

func expertManagerManifest() map[string]any {
	body := stripYAMLFrontmatter(string(expertManagerSkillMD))
	lunitide := "\n\n--- Lunitide 集成 ---\n" +
		"完成六段式岗位说明书后，调用 expert.create（source=local，frontmatter + sixSection，requestId=新 UUID）。\n" +
		"不要用 skill.create。expert.create 成功后必须立刻用中文明确告诉用户：已创建「名称」，请到专家中心挂载到项目步骤。然后停止本轮，不要继续未完成工作。\n" +
		"失败时用中文说明原因，不要沉默结束。"
	prompt := strings.TrimSpace(body) + lunitide
	if len(prompt) > 60000 {
		prompt = prompt[:60000]
	}
	return map[string]any{
		"triggers": []string{"创建专家", "新建专家", "专家", "expert-manager", "岗位说明书", "六段式", "create expert"},
		"prompt":   prompt,
	}
}

func pluginCreatorManifest() map[string]any {
	prompt := "你是插件创建助手，按 DeepSeek Harness 方式把用户需求写成一份可安装插件。\n" +
		"先确认：插件名称（英文 pluginId 短横线）、显示名、要增强的能力、kind（tool/skill/workflow/mcp）、需要的权限。\n" +
		"然后调用 plugin.create（pluginId、name、kind、description、entrypoint、manifest）。manifest 至少含 pluginId、semver、publisher、kind、permissions。\n" +
		"不要用 skill.create。plugin.create 成功后必须立刻用中文明确告诉用户：已创建「名称」，请到插件页查看安装状态。然后停止本轮，不要继续未完成工作。\n" +
		"失败时用中文说明原因，不要沉默结束。"
	return map[string]any{
		"triggers": []string{"创建插件", "新建插件", "写插件", "plugin-creator", "create plugin", "harness 插件"},
		"prompt":   prompt,
	}
}

// catalogTemplates is the frozen local market list. Entry points point at
// the builtin pipeline namespace; manifests keep the trigger keywords the
// matcher scores and the working agreement the model sees on invoke.
var catalogTemplates = []CatalogTemplate{
	{
		ID: "skill-creator", Name: "skill-creator", DisplayName: "skill-creator",
		Description: "Create new skills, modify and improve existing skills, and measure skill performance. Use when users want to create a skill from scratch, edit, or optimize an existing skill.",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionFileSystem, skill.PermissionShell},
		EntryPoint:  "builtin://skill-creator",
		Manifest:    skillCreatorManifest(),
		Featured:    true, Bundled: true, Source: "月汐",
	},
	{
		ID: "expert-manager", Name: "expert-manager", DisplayName: "expert-manager",
		Description: "Create and refine six-section expert profiles for Lunitide. Use when users want to create a new expert persona from industry experience or optimize an existing expert.",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://expert-manager",
		Manifest:    expertManagerManifest(),
		Bundled:     true, Source: "月汐",
	},
	{
		ID: "plugin-creator", Name: "plugin-creator", DisplayName: "plugin-creator",
		Description: "Create a Harness-compatible plugin from a conversation and mount it into the plugin list.",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://plugin-creator",
		Manifest:    pluginCreatorManifest(),
		Bundled:     true, Source: "月汐",
	},
	{
		ID: "meeting-minutes", Name: "tpl-meeting-minutes", DisplayName: "会议纪要助手",
		Description: "把散落在会话里的讨论整理成结构化会议纪要：结论、待办、责任人、截止时间，可导出为 Word。",
		Category:    "办公协作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		Featured: true, Source: "月汐",
		EntryPoint: "builtin://meeting-minutes",
		Manifest: map[string]any{
			"triggers": []string{"会议纪要", "整理会议", "meeting minutes", "纪要"},
			"prompt":   "你是会议纪要助手。通读会话上下文，产出：①会议主题与时间 ②关键结论（按议题分组）③待办清单（责任人/截止时间）④遗留风险。语言精炼，使用中文；如需产出文档请调用 docx.gen 生成并汇报路径。",
		},
	},
	{
		ID: "weekly-report", Name: "tpl-weekly-report", DisplayName: "周报生成器",
		Description: "汇总本周自动化运行记录与会话产出，生成一份本周工作周报（Excel 汇总 + 摘要）。",
		Category:    "办公协作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://weekly-report",
		Manifest: map[string]any{
			"triggers": []string{"周报", "本周总结", "weekly report"},
			"prompt":   "你是周报助手。检索本周会话与自动化运行历史，归纳：①本周完成事项 ②进行中事项 ③风险与求助 ④下周计划。汇总表用 excel.gen 输出，正文用中文。",
		},
	},
	{
		ID: "excel-analyst", Name: "tpl-excel-analyst", DisplayName: "表格分析师",
		Description: "解析工作区里的 xlsx 文件，回答数据问题并生成带图表的分析报告。",
		Category:    "数据分析", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		Featured: true, Source: "月汐",
		EntryPoint: "builtin://excel-analyst",
		Manifest: map[string]any{
			"triggers": []string{"分析表格", "excel 分析", "数据汇总", "xlsx"},
			"prompt":   "你是表格分析助手。先用 excel.parse 读取用户指定的工作区表格，再做统计与趋势分析；结论用条目列出，必要时用 excel.gen 输出带图表的结果文件。",
		},
	},
	{
		ID: "docx-writer", Name: "tpl-docx-writer", DisplayName: "文档撰写",
		Description: "按给定提纲生成正式 Word 文档（方案书、说明书、交付文档），支持多章节。",
		Category:    "文档产出", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://docx-writer",
		Manifest: map[string]any{
			"triggers": []string{"写文档", "生成 word", "方案书", "docx"},
			"prompt":   "你是文档撰写助手。先与用户确认提纲与受众，再分章节撰写；定稿调用 docx.gen 产出 .docx 并在工作区产物面板提示验收。",
		},
	},
	{
		ID: "slide-builder", Name: "tpl-slide-builder", DisplayName: "演示文稿助手",
		Description: "把主题或文档转成多页 PPTX 演示文稿：每页一个要点，标题+要点列表。",
		Category:    "文档产出", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		Featured: true, Source: "月汐",
		EntryPoint: "builtin://slide-builder",
		Manifest: map[string]any{
			"triggers": []string{"做 ppt", "演示文稿", "幻灯片", "pptx"},
			"prompt":   "你是演示文稿助手。把用户材料拆成 5-10 页要点，每页一句标题+3-5 条要点，调用 pptx.gen 产出 .pptx。",
		},
	},
	{
		ID: "go-reviewer", Name: "tpl-go-reviewer", DisplayName: "Go 代码审查",
		Description: "在命令白名单内运行只读 go 命令（vet/test），结合工作区源码给出审查意见。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionShell},
		EntryPoint: "builtin://go-reviewer",
		Manifest: map[string]any{
			"triggers": []string{"代码审查", "review 代码", "go test", "go vet"},
			"prompt":   "你是 Go 代码审查助手。只使用白名单内的只读 go 命令（go vet、go test 等）与工作区读取工具；报告按 严重/建议 两级输出，引用文件与行号。",
		},
	},
	{
		ID: "web-researcher", Name: "tpl-web-researcher", DisplayName: "联网调研",
		Description: "用 web.search 检索并 web.fetch 阅读网页，产出带来源链接的调研简报。",
		Category:    "信息检索", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionNetwork},
		Featured: true, Source: "月汐",
		EntryPoint: "builtin://web-researcher",
		Manifest: map[string]any{
			"triggers": []string{"调研", "查资料", "最新", "search"},
			"prompt":   "你是调研助手。先用 web.search 检索，再用 web.fetch 阅读候选来源；结论分点陈述，每条附来源链接；无法核实的信息明确标注。",
		},
	},
	{
		ID: "translator", Name: "tpl-translator", DisplayName: "翻译润色",
		Description: "中英互译并按目标文体润色（商务/技术/口语），保留术语一致性。",
		Category:    "写作辅助", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://translator",
		Manifest: map[string]any{
			"triggers": []string{"翻译", "润色", "translate", "英文"},
			"prompt":   "你是翻译润色助手。默认中英互译；先识别文体（商务/技术/口语），译文保持术语一致，必要时给出两版供选择。",
		},
	},
	{
		ID: "code-reviewer", Name: "tpl-code-reviewer", DisplayName: "代码审查",
		Description: "对工作区改动做结构化审查：正确性、安全、可维护性，引用 path:line。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://code-reviewer",
		Manifest: map[string]any{
			"triggers": []string{"code review", "审查代码", "review diff"},
			"prompt":   "你是代码审查助手。用 workspace.search/read 收集改动与周边代码；按 严重/建议 分级，每条引用 path:line；不修改文件除非用户明确要求。",
		},
	},
	{
		ID: "debugger", Name: "tpl-debugger", DisplayName: "运行时排障",
		Description: "假设→取证→复现→修复→验证：用 command.run 与工作区日志定位无法静态看出来的问题。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionShell},
		EntryPoint: "builtin://debugger",
		Manifest: map[string]any{
			"triggers": []string{"调试", "报错", "debug", "复现失败"},
			"prompt":   "你是排障助手。先陈述假设，再用 workspace.search 与白名单 command.run 收集运行证据；修复走 workspace.edit，最后用同一命令验证。",
		},
	},
	{
		ID: "test-writer", Name: "tpl-test-writer", DisplayName: "测试补全",
		Description: "为改动补回归测试，优先复用项目现有测试命令与断言风格。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionShell},
		EntryPoint: "builtin://test-writer",
		Manifest: map[string]any{
			"triggers": []string{"补测试", "写单测", "unit test", "回归测试"},
			"prompt":   "你是测试助手。先读现有测试风格，再 workspace.edit/write 补测试；用 command.run 跑白名单测试命令，失败则修到绿。",
		},
	},
	{
		ID: "git-status", Name: "tpl-git-status", DisplayName: "Git 现状",
		Description: "只读查看仓库状态、diff 与近期提交，总结风险后再建议是否提交。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionShell},
		EntryPoint: "builtin://git-status",
		Manifest: map[string]any{
			"triggers": []string{"git status", "看 diff", "提交前检查"},
			"prompt":   "你是 Git 助手。只用白名单只读 git（status/diff/log）；总结改动与风险。写操作必须先获得用户确认。",
		},
	},
	{
		ID: "daily-brief", Name: "tpl-daily-brief", DisplayName: "每日简报",
		Description: "汇总今日会话、待办与公开网页动态，产出可执行的一日清单。",
		Category:    "办公协作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionNetwork},
		EntryPoint: "builtin://daily-brief",
		Manifest: map[string]any{
			"triggers": []string{"今日简报", "daily brief", "今天做什么"},
			"prompt":   "你是简报助手。结合会话与 todo.write 清单，必要时 web.search 补充公开动态；输出：优先事项、风险、建议下一步。",
		},
	},
	{
		ID: "security-review", Name: "tpl-security-review", DisplayName: "安全快审",
		Description: "检查密钥泄漏、注入、越权与依赖风险，只报告不静默改生产配置。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://security-review",
		Manifest: map[string]any{
			"triggers": []string{"安全审查", "有没有泄漏", "security review"},
			"prompt":   "你是安全审查助手。搜索密钥形态、命令拼接、路径穿越与鉴权缺口；发现疑似密钥只提示轮换，绝不回显完整秘密。",
		},
	},
	{
		ID: "knowledge-index", Name: "tpl-knowledge-index", DisplayName: "知识索引",
		Description: "把工作区文档做成可检索提纲，并指出过期或互相矛盾的段落。",
		Category:    "信息检索", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://knowledge-index",
		Manifest: map[string]any{
			"triggers": []string{"整理文档", "知识库", "索引仓库"},
			"prompt":   "你是知识索引助手。workspace.list/search 建立主题提纲，标出过期与冲突；需要成文时用 docx.gen。",
		},
	},
	{
		ID: "pm-phase-1", Name: "tpl-pm-phase-1", DisplayName: "需求架构规范助手",
		Description: "指导完成阶段一需求架构规范交付物：范围、架构视图、约束与非功能需求清单。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://pm-phase-1",
		Manifest: map[string]any{
			"triggers": []string{"需求架构交付", "需求架构规范", "阶段一交付", "pm phase 1"},
			"prompt":   "你是项目管理阶段一（需求架构规范）交付助手。对照项目目标梳理范围边界、干系人与约束；产出需求架构说明、关键用例与非功能需求清单；逐项标记交付物 draft/review/approved 状态并提示三关确认晋级前缺口。",
		},
	},
	{
		ID: "pm-phase-2", Name: "tpl-pm-phase-2", DisplayName: "方案和UI设计助手",
		Description: "指导完成阶段二方案与 UI 设计交付物：交互流程、界面规范与方案说明。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://pm-phase-2",
		Manifest: map[string]any{
			"triggers": []string{"方案和UI设计", "方案设计", "UI设计交付", "阶段二交付"},
			"prompt":   "你是项目管理阶段二（方案和UI设计）交付助手。基于需求架构整理业务流程、页面清单与交互说明；产出方案文档与 UI 规范要点；核对每份交付物是否已绑定附件或模板并可用于晋级评审。",
		},
	},
	{
		ID: "pm-phase-3", Name: "tpl-pm-phase-3", DisplayName: "数据库设计助手",
		Description: "指导完成阶段三数据库交付物：逻辑模型、表结构与数据字典。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://pm-phase-3",
		Manifest: map[string]any{
			"triggers": []string{"数据库交付", "数据库设计", "逻辑模型", "阶段三交付"},
			"prompt":   "你是项目管理阶段三（数据库）交付助手。梳理实体关系、表结构、索引与数据字典；标注与接口、权限相关的字段约束；输出可评审的数据库设计说明并跟踪交付物确认状态。",
		},
	},
	{
		ID: "pm-phase-4", Name: "tpl-pm-phase-4", DisplayName: "接口设计助手",
		Description: "指导完成阶段四接口交付物：API 契约、错误码与集成说明。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://pm-phase-4",
		Manifest: map[string]any{
			"triggers": []string{"接口交付", "API设计", "接口契约", "阶段四交付"},
			"prompt":   "你是项目管理阶段四（接口）交付助手。整理对内对外 API 清单、请求响应示例、鉴权与错误码；确保与数据库、前端方案一致；列出待联调项并辅助完成交付物门禁确认。",
		},
	},
	{
		ID: "pm-phase-5", Name: "tpl-pm-phase-5", DisplayName: "开发实施助手",
		Description: "指导完成阶段五开发交付物：实现说明、变更记录与代码审查要点。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionShell},
		EntryPoint: "builtin://pm-phase-5",
		Manifest: map[string]any{
			"triggers": []string{"开发交付", "开发实施", "阶段五交付", "实现说明"},
			"prompt":   "你是项目管理阶段五（开发）交付助手。对照方案与接口契约检查实现覆盖度；汇总关键模块说明、配置项与变更记录；提示测试前置条件并协助标记开发阶段交付物完成度。",
		},
	},
	{
		ID: "pm-phase-6", Name: "tpl-pm-phase-6", DisplayName: "测试验收助手",
		Description: "指导完成阶段六测试交付物：用例、执行结果与缺陷闭环。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionShell},
		EntryPoint: "builtin://pm-phase-6",
		Manifest: map[string]any{
			"triggers": []string{"测试交付", "测试验收", "用例执行", "阶段六交付"},
			"prompt":   "你是项目管理阶段六（测试）交付助手。整理测试范围、用例与执行证据；跟踪缺陷修复与回归结果；输出测试结论摘要并确认本阶段交付物是否满足晋级条件。",
		},
	},
	{
		ID: "pm-phase-7", Name: "tpl-pm-phase-7", DisplayName: "集成联调助手",
		Description: "指导完成阶段七集成交付物：联调报告、环境差异与问题清单。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://pm-phase-7",
		Manifest: map[string]any{
			"triggers": []string{"集成交付", "联调报告", "系统集成", "阶段七交付"},
			"prompt":   "你是项目管理阶段七（集成）交付助手。汇总跨模块联调结果、环境配置差异与阻塞项；明确遗留风险与缓解措施；协助完成集成阶段交付物确认与上线前检查清单。",
		},
	},
	{
		ID: "pm-phase-8", Name: "tpl-pm-phase-8", DisplayName: "发布上线助手",
		Description: "指导完成阶段八发布交付物：发布清单、回滚预案与上线验证。",
		Category:    "项目管理", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://pm-phase-8",
		Manifest: map[string]any{
			"triggers": []string{"发布交付", "上线准备", "发布清单", "阶段八交付"},
			"prompt":   "你是项目管理阶段八（发布）交付助手。核对发布窗口、变更清单、回滚步骤与值班安排；整理上线后验证项与监控关注点；确保发布相关交付物齐备后再建议项目晋级或关闭。",
		},
	},
	{
		ID: "grill-me", Name: "tpl-grill-me", DisplayName: "深度追问",
		Description: "在动手前把目标和边界问清楚：按设计树一轮一轮追问，直到没有默许的假设。",
		Category:    "办公协作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://grill-me", Featured: true, Source: "工程实践",
		Manifest: map[string]any{
			"triggers": []string{"追问", "对齐需求", "grill", "先问清楚", "需求澄清"},
			"prompt":   "你是深度追问助手（改编自 Matt Pocock grilling，MIT）。把当前任务画成设计树：每个决定会分出后续决定。每轮只问「现在就能回答、不依赖未决前提」的问题；编号、给出你的推荐答案，然后等待用户。事实用工具自己查，不要问用户本可检索的信息。树走完、用户确认共识之前，不要动手实现。",
		},
	},
	{
		ID: "tdd-loop", Name: "tpl-tdd-loop", DisplayName: "TDD 红绿循环",
		Description: "先写失败测试再写刚好够用的实现：一次一条垂直切片，测公共接口而不是内部细节。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionShell},
		EntryPoint: "builtin://tdd-loop", Source: "工程实践",
		Manifest: map[string]any{
			"triggers": []string{"TDD", "红绿重构", "test first", "先写测试"},
			"prompt":   "你是 TDD 助手（改编自 Matt Pocock tdd，MIT）。先与用户确认要测的公共缝（seam）。每轮：①写一个会失败的测试 ②只写刚好让它通过的实现 ③用白名单 command.run 跑测试。禁止一次性写完全部测试再实现；禁止测私有实现。重构放到审查阶段，不塞进红绿循环。",
		},
	},
	{
		ID: "session-handoff", Name: "tpl-session-handoff", DisplayName: "会话交接",
		Description: "把当前对话收成一份交接说明，方便下一轮对话或另一个助手接着做。",
		Category:    "办公协作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://session-handoff", Source: "工程实践",
		Manifest: map[string]any{
			"triggers": []string{"交接", "handoff", "下轮继续", "会话摘要"},
			"prompt":   "你是会话交接助手（改编自 Matt Pocock handoff，MIT）。根据当前会话写交接文档：目标、已完成、未完成、关键决定、建议下一轮使用的技能。不要重复已有产物，用路径引用。脱敏密钥与个人信息。优先写入工作区 markdown；若平台提供上下文交接工具也可以使用。",
		},
	},
	{
		ID: "content-brief", Name: "tpl-content-brief", DisplayName: "内容选题策划",
		Description: "按渠道策划选题、大纲与发布清单，适合公众号、短视频与知识帖。",
		Category:    "内容创作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionNetwork},
		EntryPoint: "builtin://content-brief", Featured: true, Source: "内容创作",
		Manifest: map[string]any{
			"triggers": []string{"选题", "内容策划", "公众号大纲", "短视频选题"},
			"prompt":   "你是内容策划助手。先确认渠道、受众与目标；必要时 web.search 看公开热点。产出：①选题（角度+为什么现在发）②大纲 ③标题备选 ④发布清单。不编造阅读数据；不确定的趋势要标明来源。",
		},
	},
	{
		ID: "short-copy", Name: "tpl-short-copy", DisplayName: "爆款文案",
		Description: "写短视频口播、标题和封面文案：钩子、冲突、行动号召，多版本对照。",
		Category:    "内容创作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://short-copy", Source: "内容创作",
		Manifest: map[string]any{
			"triggers": []string{"爆款文案", "口播", "短视频文案", "标题党改写"},
			"prompt":   "你是短内容文案助手。先问产品/观点/禁忌词。每条给出：3 秒钩子、正文结构、结尾行动号召；同时提供克制版与更冲的一版供选。不承诺播放量，不抄袭未授权作品。",
		},
	},
	{
		ID: "knowledge-qa", Name: "tpl-knowledge-qa", DisplayName: "知识库问答",
		Description: "只根据工作区文档回答问题，并标注出处；找不到就说找不到，不编造。",
		Category:    "信息检索", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://knowledge-qa", Source: "月汐",
		Manifest: map[string]any{
			"triggers": []string{"根据文档回答", "知识库问答", "RAG", "引用出处"},
			"prompt":   "你是知识库问答助手。只用 workspace.search/read 检索用户工作区；每条结论附文件路径。检索不到就明确说没有，禁止用训练记忆冒充文档内容。",
		},
	},
	{
		ID: "subagent-coord", Name: "tpl-subagent-coord", DisplayName: "多智能体协作",
		Description: "把大任务拆给子代理并行，再汇总冲突与结论，适合调研+写作+检查。",
		Category:    "自动化", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://subagent-coord", Source: "月汐",
		Manifest: map[string]any{
			"triggers": []string{"多智能体", "拆任务", "子代理", "并行调研"},
			"prompt":   "你是协作编排助手。把用户目标拆成可并行的子任务，用 subagent.spawn 派出、subagent.join 回收。汇总时列出共识、冲突与你的裁决理由；高风险步骤先征得用户同意。",
		},
	},
}

// InstallFromCatalog materializes one catalog template as a local draft
// skill. Install is idempotent per template version: an existing
// name+version answers ErrTemplateInstalled instead of a duplicate.
func (s *Service) InstallFromCatalog(ctx context.Context, templateID string) (skill.Skill, error) {
	if s == nil || s.write == nil {
		return skill.Skill{}, errors.New("skill writer unavailable")
	}
	var tpl *CatalogTemplate
	for i := range catalogTemplates {
		if catalogTemplates[i].ID == templateID {
			tpl = &catalogTemplates[i]
			break
		}
	}
	if tpl == nil {
		return skill.Skill{}, fmt.Errorf("%w: %s", ErrTemplateUnknown, templateID)
	}
	if _, err := s.GetByNameVersion(ctx, tpl.Name, tpl.Version); err == nil {
		return skill.Skill{}, fmt.Errorf("%w: %s@%s", ErrTemplateInstalled, tpl.Name, tpl.Version)
	}
	return s.Create(ctx, skill.Skill{
		Name: tpl.Name, DisplayName: tpl.DisplayName, Description: tpl.Description,
		Version: tpl.Version, Permissions: tpl.Permissions, EntryPoint: tpl.EntryPoint,
		ManifestJSON: manifestFor(*tpl),
	})
}

// EnsureBundledSkills installs and publishes only Bundled templates so
// product flows (skill-creator, expert-manager) work on a fresh engine.
// Market-only templates stay uninstalled until the user clicks install.
func (s *Service) EnsureBundledSkills(ctx context.Context) (int, error) {
	if s == nil || s.write == nil {
		return 0, errors.New("skill writer unavailable")
	}
	published := 0
	for _, tpl := range catalogTemplates {
		if !tpl.Bundled {
			continue
		}
		var sk skill.Skill
		created, err := s.InstallFromCatalog(ctx, tpl.ID)
		switch {
		case err == nil:
			sk = created
		case errors.Is(err, ErrTemplateInstalled):
			existing, gerr := s.GetByNameVersion(ctx, tpl.Name, tpl.Version)
			if gerr != nil {
				continue
			}
			sk = *existing
		default:
			continue
		}
		if sk.Status == skill.SkillStatusPublished {
			continue
		}
		if sk.Status != skill.SkillStatusDraft {
			continue
		}
		if err := s.Publish(ctx, sk.ID); err != nil {
			continue
		}
		published++
	}
	return published, nil
}
