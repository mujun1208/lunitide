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
	"github.com/lunitide/lunitide/internal/m8app"
)

//go:embed bundled/skill-creator/SKILL.md
var skillCreatorSkillMD []byte

//go:embed bundled/expert-manager/SKILL.md
var expertManagerSkillMD []byte

//go:embed bundled/find-skills/SKILL.md
var findSkillsSkillMD []byte

//go:embed bundled/brainstorming/SKILL.md
var brainstormingSkillMD []byte

//go:embed bundled/pm-skill/SKILL.md
var pmSkillSkillMD []byte

//go:embed bundled/super-coders/SKILL.md
var superCodersSkillMD []byte

//go:embed bundled/frontend-design/SKILL.md
var frontendDesignSkillMD []byte

//go:embed bundled/ui-components/SKILL.md
var uiComponentsSkillMD []byte

//go:embed bundled/design-system/SKILL.md
var designSystemSkillMD []byte

//go:embed bundled/computer-control/SKILL.md
var computerControlSkillMD []byte

//go:embed bundled/browser-automation/SKILL.md
var browserAutomationSkillMD []byte

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
	Compose     bool
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
		"skill.create 成功后必须立刻用中文明确告诉用户：已创建「名称」，请到技能中心安装并发布。然后继续用户还没做完的工作。\n" +
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
		"不要用 skill.create。expert.create 成功后必须立刻用中文明确告诉用户：已创建「名称」，请到专家中心挂载到项目步骤。然后继续用户还没做完的工作。\n" +
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
	prompt := "你是插件登记助手。月汐不执行 Cordis / TypeScript 插件包，也不会加载 plugin/main.ts。\n" +
		"先判断用户真正要的能力：\n" +
		"- 可调用的工作约定 → 用 skill.create，不要 plugin.create。\n" +
		"- 现役 MCP 服务器 → 用 mcp.presets + mcp.install（Playwright / Filesystem / Fetch 等），不要 plugin.create kind=mcp。\n" +
		"- 只要在插件页留一张能力卡片（kind=tool|skill|workflow|template）→ 才调用 plugin.create。\n" +
		"plugin.create 成功后必须用中文说清：这是目录卡片，不会出现新的可执行代码；若要真正调用请 skill.create 或 mcp.install。然后继续没做完的工作。\n" +
		"失败时用中文说明原因，不要沉默结束。"
	return map[string]any{
		"triggers": []string{"创建插件", "新建插件", "写插件", "plugin-creator", "create plugin", "harness 插件"},
		"prompt":   prompt,
	}
}

func bundledManifest(raw []byte, triggers []string, lunitide string) map[string]any {
	body := stripYAMLFrontmatter(string(raw))
	prompt := strings.TrimSpace(body) + lunitide
	if len(prompt) > 60000 {
		prompt = prompt[:60000]
	}
	return map[string]any{
		"triggers": triggers,
		"prompt":   prompt,
	}
}

func findSkillsManifest() map[string]any {
	return bundledManifest(findSkillsSkillMD, []string{
		"find skill", "find-skills", "找技能", "有没有技能", "安装技能", "发现技能", "skill 推荐",
	}, "\n\n--- Lunitide 集成 ---\n优先 skill.catalog.list 浏览内置市场；匹配时用 skill.install({ templateId })，draft 需 skill.publish。\n用 skill.list 确认已发布。用中文向用户说明推荐与安装结果。")
}

func brainstormingManifest() map[string]any {
	return bundledManifest(brainstormingSkillMD, []string{
		"brainstorm", "brainstorming", "头脑风暴", "先想清楚", "产品方向", "需求发散", "从0到1",
	}, "\n\n--- Lunitide 集成 ---\n设计定稿前不要写实现代码。设计文档写入工作区 docs/plans/。用中文与用户逐段确认。")
}

func pmSkillManifest() map[string]any {
	return bundledManifest(pmSkillSkillMD, []string{
		"pm skill", "pm-skill", "产品经理", "写PRD", "PRD", "用户画像", "商业模式", "市场调研", "产品方案",
	}, "\n\n--- Lunitide 集成 ---\nPRD 与调研产物写入工作区 markdown。需要联网调研时用 web.search 并附来源。用中文输出。")
}

func superCodersManifest() map[string]any {
	return bundledManifest(superCodersSkillMD, []string{
		"super coders", "super-coders", "拆任务", "驱动实现", "写代码", "开发任务", "垂直切片", "按票实现",
	}, "\n\n--- Lunitide 集成 ---\n用 workspace 与 command.run（白名单）实现；每切片验证后再继续。可 skill.invoke code-reviewer。用中文汇报进度。")
}

func frontendDesignManifest() map[string]any {
	return bundledManifest(frontendDesignSkillMD, []string{
		"frontend design", "frontend-design", "前端设计", "页面美化", "UI 质感", "landing page", "dashboard UI",
	}, "\n\n--- Lunitide 集成 ---\n先读项目现有样式与组件库；实现用 workspace 工具。与 design-system、ui-components 技能配合。用中文说明设计方向。")
}

func uiComponentsManifest() map[string]any {
	return bundledManifest(uiComponentsSkillMD, []string{
		"ui components", "ui-components", "shadcn", "组件库", "高质量组件", "参考组件", "radix", "tailwind 组件",
	}, "\n\n--- Lunitide 集成 ---\n优先复用项目已有组件；新组件遵循 shadcn/Radix 模式。实现写入工作区。用中文说明组件选型。")
}

func designSystemManifest() map[string]any {
	return bundledManifest(designSystemSkillMD, []string{
		"design system", "design-system", "设计系统", "风格统一", "设计规范", "视觉一致", "design tokens",
	}, "\n\n--- Lunitide 集成 ---\n审计现有 CSS/Tailwind 变量；token 与规范写入工作区文档。重构时小步提交。用中文输出检查清单。")
}

func computerControlManifest() map[string]any {
	return bundledManifest(computerControlSkillMD, []string{
		"操作电脑", "电脑控制", "computer control", "computer-control", "点一下", "帮我点",
		"截个屏", "切窗口", "按回车", "粘贴", "桌面操作", "peekaboo",
	}, "\n\n--- Lunitide 集成 ---\n模型只能调用 computer.act（以及 desktop.open / desktop.type / media.play / browser.act）。不要调用 cc.* 工具名——它们不在工具列表里。启动未运行应用用 desktop.open；播歌用 media.play；网页用 browser.act。禁止确认 UAC/提权/打开保存对话框。用中文短报进度。")
}

func browserAutomationManifest() map[string]any {
	return bundledManifest(browserAutomationSkillMD, []string{
		"浏览器自动化", "browser-automation", "填表", "抓取网页", "点网页", "scraping", "填写表单",
		"browser.act", "playwright",
	}, "\n\n--- Lunitide 集成 ---\n只用 browser.act（navigate/snapshot/click/type/read）。桌面软件改走 computer-control。验证码/登录墙/文件选择停下来问用户。抽数后 structured.output 再 excel.gen/docx.gen。")
}

func mermaidDiagramsManifest() map[string]any {
	return map[string]any{
		"triggers": []string{"mermaid", "C4", "erDiagram", "结构图", "架构图", "流程图", "ER 图"},
		"prompt": "你是月汐画图技能（改编自 mermaid-skill MIT、c4-model-skill MIT；图在对话里用 mermaid 渲染，禁止另起 Draw.io / Structurizr 产品）。\n" +
			"节点必须双引号，换行用 <br/>：A[\"封面<br/>副标题\"]。\n" +
			"PPT/流程用 flowchart。系统架构：Simon Brown 黄金法则——默认只出 Context + Container，Component 仅在用户明确要求时。数据库用 erDiagram。\n" +
			"先证据后图：有仓库就 workspace.read，没有就标提案。每张图配一段说明，不要只丢裸代码块。不要倾倒 mermaid 全书。",
	}
}

func antiAIProseManifest() map[string]any {
	return map[string]any{
		"triggers": []string{"去AI味", "humanizer", "润色", "翻译腔", "anti-vibe"},
		"prompt": "你是去AI味技能（改编自 anti-vibe-writing MIT、human-readable-reports MIT 的「先结论」、以及中文 AI 痕迹规则 CC-BY-4.0，署名 Wechat-ggGitHub/chinese-ai-humanizer）。\n" +
			"结构优先：每节第一句先给结论。删赋能/打通/闭环/抓手/链路；拆掉首先/其次/最后空骨架；改翻译腔；少用不是…而是…、——、英文逗号。\n" +
			"数字、命令、专名原样。引号用中文“ ”。不要把规则全书倾倒给用户，直接改文。报告走 docx.gen kind=report；小说走 kind=novel。",
	}
}

func e2eBrowserManifest() map[string]any {
	return map[string]any{
		"triggers": []string{"E2E", "端到端", "playwright", "e2e", "浏览器测", "意图场景"},
		"prompt": "你是 E2E 技能（改编自 e2e-test-agent MIT；浏览器自动化对齐 Microsoft Playwright MCP）。\n" +
			"写 3–7 条意图场景（谁、目标、成功/失败），不要绑死 CSS 选择器。\n" +
			"若会话已连接 Playwright MCP（browser_* 或 mcp.search 能找到），优先用；否则 browser.act：navigate → snapshot → 按 ref click/type。\n" +
			"登录墙/验证码交给用户。给月汐测时走现有 vitest/go test，不要另起 Playwright 云平台。",
	}
}

func csvWorkbookManifest() map[string]any {
	return map[string]any{
		"triggers": []string{"csv", "表格", "工作簿", "xlsx", "清单", "台账"},
		"prompt": "你是表格/CSV 技能（对标 Anthropic/社区 xlsx skill 的公式优先，落地走月汐 excel.parse / excel.gen，禁止 openpyxl/pandas 旁路）。\n" +
			"已有文件先 excel.parse；定稿 excel.gen（公式以 = 开头，有表头则冻结首行）。汇总列必须是公式不是死数。\n" +
			"先定列名、类型、是否主键。用户没让自拟时禁止编造业务数字。CSV 当作单 sheet。多主题分 sheet。禁止 Excel COM / Python 拼 OOXML。桌面 desktop=true。",
	}
}

func hardwareBOMManifest() map[string]any {
	return map[string]any{
		"triggers": []string{"BOM", "硬件选型", "容量", "SKU", "服务器配置", "GPU 显存"},
		"prompt": "你是硬件 BOM 技能（改编自 it-infrastructure-equipment-selection MIT 与 LLM-Capacity-Planner MIT 的容量心态，不搬源码）。\n" +
			"先工作负载（QPS/并发/数据量/HA/IOPS/本地模型 GPU），再 2–4 档短名单，再 excel.gen 出 BOM。\n" +
			"现价必须 web.search，标待确认。禁止电气 DIY。月汐本机才谈 WebView2/ASR/TTS GPU。",
	}
}

func fictionContinuityManifest() map[string]any {
	return map[string]any{
		"triggers": []string{"连续性", "人设", "伏笔", "故事圣经", "人物卡", "continuity"},
		"prompt": "你是小说连续性技能（改编自 ArcVellum MIT 与 fiction-forge 的故事圣经心态：长篇必须记得自己，禁止搬 MCP/扫描器源码）。\n" +
			"维护人物卡（欲望/恐惧/秘密/说话方式）与已对读者做过的承诺。新细节标「暂定」，禁止无声改写人设或世界规则。\n" +
			"分章正文用 docx.gen kind=novel；一章过长就分次调用。去AI味配合 anti-ai-prose。不写未成年人的性内容。",
	}
}

// catalogTemplates is the frozen local market list. Entry points point at
// the builtin pipeline namespace; manifests keep the trigger keywords the
// matcher scores and the working agreement the model sees on invoke.
var catalogTemplates = []CatalogTemplate{
	{
		ID: "skill-creator", Name: "skill-creator", DisplayName: "skill-creator",
		Description: "Create and improve Lunitide skills stored with skill.create. Use when users want a new skill, an edit of an existing SKILL.md, or clearer triggers. Skills are prompt plus triggers; there is no built-in performance runner.",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionFileSystem, skill.PermissionShell},
		EntryPoint:  "builtin://skill-creator",
		Manifest:    skillCreatorManifest(),
		Featured:    true, Bundled: true, Source: "月汐",
	},
	{
		ID: "find-skills", Name: "find-skills", DisplayName: "find-skills",
		Description: "Discover and install agent skills from the built-in market when users need capabilities or ask which skill to use.",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionNetwork},
		EntryPoint:  "builtin://find-skills",
		Manifest:    findSkillsManifest(),
		Featured:    true, Bundled: true, Source: "Agent Skills 模板",
	},
	{
		ID: "brainstorming", Name: "brainstorming", DisplayName: "brainstorming",
		Description: "Explore user intent and design before implementation. Use before creative work, new features, or product direction changes.",
		Category:    "产品设计", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://brainstorming",
		Manifest:    brainstormingManifest(),
		Featured:    true, Bundled: true, Source: "Agent Skills 模板",
	},
	{
		ID: "pm-skill", Name: "pm-skill", DisplayName: "pm-skill",
		Description: "Product manager workflow: PRD, personas, business model, market research, and delivery checklist.",
		Category:    "产品设计", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionNetwork},
		EntryPoint:  "builtin://pm-skill",
		Manifest:    pmSkillManifest(),
		Featured:    true, Bundled: true, Source: "Agent Skills 模板",
	},
	{
		ID: "super-coders", Name: "super-coders", DisplayName: "super-coders",
		Description: "Split specs into vertical-slice dev tasks and drive implementation with tests and review.",
		Category:    "产品设计", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionShell},
		EntryPoint:  "builtin://super-coders",
		Manifest:    superCodersManifest(),
		Featured:    true, Bundled: true, Source: "Agent Skills 模板",
	},
	{
		ID: "frontend-design", Name: "frontend-design", DisplayName: "frontend-design",
		Description: "Create distinctive, production-grade frontend interfaces that avoid generic AI aesthetics.",
		Category:    "审美设计", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://frontend-design",
		Manifest:    frontendDesignManifest(),
		Featured:    true, Bundled: true, Source: "Agent Skills 模板",
	},
	{
		ID: "ui-components", Name: "ui-components", DisplayName: "ui-components",
		Description: "High-quality UI component patterns with shadcn/ui, Radix, and Tailwind for polished pages and apps.",
		Category:    "审美设计", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://ui-components",
		Manifest:    uiComponentsManifest(),
		Featured:    true, Bundled: true, Source: "Agent Skills 模板",
	},
	{
		ID: "design-system", Name: "design-system", DisplayName: "design-system",
		Description: "Keep visual language consistent — tokens, typography, spacing, and shared components across pages.",
		Category:    "审美设计", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://design-system",
		Manifest:    designSystemManifest(),
		Featured:    true, Bundled: true, Source: "Agent Skills 模板",
	},
	{
		ID: "computer-control", Name: "computer-control", DisplayName: "computer-control",
		Description: "Operate this Windows PC with computer.act: screenshot, named click, type, paste, press, windows, menus. This PC only.",
		Category:    "办公协作", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://computer-control",
		Manifest:    computerControlManifest(),
		Featured:    true, Bundled: true, Source: "月汐",
	},
	{
		ID: "browser-automation", Name: "browser-automation", DisplayName: "browser-automation",
		Description: "Fill forms, scrape, and click in the managed browser with browser.act. Snapshot before act; this PC only.",
		Category:    "办公协作", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionNetwork},
		EntryPoint:  "builtin://browser-automation",
		Manifest:    browserAutomationManifest(),
		Featured:    true, Bundled: true, Source: "OpenClaw 对齐",
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
		Description: "Register a Plugin Center capability card. Does not execute Cordis. Prefer skill.create or mcp.install for real capabilities.",
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
			"prompt":   "你是演示文稿助手。按九步做：思考受众→定义结构（mermaid）→写每页要点→web.search 收集素材→再思考→再检索→定版式→写完整页→最后才 pptx.gen。每页一句标题+3-5 条要点，禁止空页或只有深色底没有文字。",
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
			"triggers": []string{"code review", "code-review", "审查代码", "review diff"},
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
			"triggers": []string{"追问", "对齐需求", "grill", "grill-me", "先问清楚", "需求澄清"},
			"prompt":   "你是深度追问助手（改编自 Matt Pocock grilling，MIT）。把当前任务画成设计树：每个决定会分出后续决定。每轮只问「现在就能回答、不依赖未决前提」的问题；编号、给出你的推荐答案，然后等待用户。事实用工具自己查，不要问用户本可检索的信息。树走完、用户确认共识之前，不要动手实现。",
		},
	},
	{
		ID: "to-spec", Name: "tpl-to-spec", DisplayName: "写规格",
		Description: "把已对齐的共识沉淀为可执行的规格文档，明确范围、验收标准与不在范围内的事项。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://to-spec", Featured: true, Source: "工程实践",
		Manifest: map[string]any{
			"triggers": []string{"to-spec", "写规格", "写需求文档", "规格说明", "spec", "验收标准"},
			"prompt":   "你是规格助手（改编自 Matt Pocock to-spec，MIT）。在动手前输出结构化规格：背景、目标、非目标、用户故事/用例、验收标准、风险与开放问题。写入工作区 markdown；引用已有文件路径，不重复粘贴大段原文。规格定稿前不要写实现代码。",
		},
	},
	{
		ID: "to-tickets", Name: "tpl-to-tickets", DisplayName: "拆票",
		Description: "把规格拆成可独立交付的垂直切片任务（tracer bullet），而不是按技术层横切。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint: "builtin://to-tickets", Source: "工程实践",
		Manifest: map[string]any{
			"triggers": []string{"to-tickets", "拆票", "拆任务", "垂直切片", "tracer bullet", "任务清单"},
			"prompt":   "你是拆票助手（改编自 Matt Pocock to-tickets，MIT）。每张票必须端到端可演示（tracer bullet），禁止按「前端/后端/数据库」横切。每张票含：标题、目标、验收标准、依赖、预估风险。优先最小可合并增量；输出 markdown 或表格到工作区。",
		},
	},
	{
		ID: "implement", Name: "tpl-implement", DisplayName: "驱动实现",
		Description: "按规格与票据驱动实现：小步提交、先测后写、完成后自审。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionShell},
		EntryPoint: "builtin://implement", Featured: true, Source: "工程实践",
		Manifest: map[string]any{
			"triggers": []string{"implement", "开始实现", "写代码", "按票实现", "驱动实现", "开发功能"},
			"prompt":   "你是实现助手（改编自 Matt Pocock implement + tdd，MIT）。一次只完成一张垂直切片：先确认验收标准，再 workspace.edit/write 与 command.run。优先配合 tdd-loop：红-绿-重构。禁止一次性大改；每步用测试或命令验证。完成后 skill.invoke code-reviewer 或说明待审查 diff。",
		},
	},
	{
		ID: "improve-architecture", Name: "tpl-improve-architecture", DisplayName: "架构改进",
		Description: "扫描代码库寻找深模块化与边界清晰化机会，输出可执行的改进建议。",
		Category:    "研发效能", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint: "builtin://improve-architecture", Source: "工程实践",
		Manifest: map[string]any{
			"triggers": []string{"improve-architecture", "架构改进", "模块化", "重构建议", "improve architecture"},
			"prompt":   "你是架构改进助手（改编自 Matt Pocock improve-codebase-architecture，MIT）。用 workspace.search/read 扫描耦合点、重复边界与缺失抽象。输出：问题、证据（path:line）、建议改法、风险与推荐顺序。默认只读；用户确认后再改代码。",
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
	{
		ID: "mermaid-diagrams", Name: "tpl-mermaid-diagrams", DisplayName: "Mermaid 结构图",
		Description: "用月汐已有 mermaid 画流程图、C4 与 ER：节点双引号，换行 <br/>。不另起画图产品。",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://mermaid-diagrams", Featured: true, Compose: true, Source: "Mermaid / C4 skill",
		Manifest: mermaidDiagramsManifest(),
	},
	{
		ID: "anti-ai-prose", Name: "tpl-anti-ai-prose", DisplayName: "去AI味",
		Description: "压掉赋能套话、排比与翻译腔，适合报告与小说终检。",
		Category:    "写作辅助", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://anti-ai-prose", Compose: true, Source: "anti-vibe-writing / humanizer",
		Manifest: antiAIProseManifest(),
	},
	{
		ID: "e2e-browser", Name: "tpl-e2e-browser", DisplayName: "E2E 意图场景",
		Description: "按意图写 E2E：已连接 Playwright MCP 就用，否则 browser.act。不要绑死选择器。",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionNetwork},
		EntryPoint:  "builtin://e2e-browser", Compose: true, Source: "e2e-test-agent / Playwright MCP",
		Manifest: e2eBrowserManifest(),
	},
	{
		ID: "csv-workbook", Name: "tpl-csv-workbook", DisplayName: "CSV/表格工作簿",
		Description: "把清单或 CSV 做成带表头、类型与公式的 xlsx，走 excel.parse / excel.gen。",
		Category:    "数据分析", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://csv-workbook", Compose: true, Source: "月汐 office 工具",
		Manifest: csvWorkbookManifest(),
	},
	{
		ID: "hardware-bom", Name: "tpl-hardware-bom", DisplayName: "硬件 BOM",
		Description: "按工作负载出 SKU 短名单与 BOM 表，价格必须检索，禁止电气 DIY。",
		Category:    "研发效能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite, skill.PermissionNetwork},
		EntryPoint:  "builtin://hardware-bom", Compose: true, Source: "capacity-planner skills",
		Manifest: hardwareBOMManifest(),
	},
	{
		ID: "fiction-continuity", Name: "tpl-fiction-continuity", DisplayName: "小说连续性",
		Description: "人设、伏笔与世界观账本：长篇必须记得自己，禁止无声改写。",
		Category:    "内容创作", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:  "builtin://fiction-continuity", Compose: true, Source: "ArcVellum / fiction-forge",
		Manifest: fictionContinuityManifest(),
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

func composeTemplateWanted() map[string]bool {
	want := map[string]bool{}
	for _, id := range m8app.PreferredComposeTemplateIDs() {
		want[id] = true
	}
	for _, tpl := range catalogTemplates {
		if tpl.Compose {
			want[tpl.ID] = true
		}
	}
	return want
}

func (s *Service) publishCatalogTemplate(ctx context.Context, tpl CatalogTemplate) (bool, error) {
	var sk skill.Skill
	created, err := s.InstallFromCatalog(ctx, tpl.ID)
	switch {
	case err == nil:
		sk = created
	case errors.Is(err, ErrTemplateInstalled):
		existing, gerr := s.GetByNameVersion(ctx, tpl.Name, tpl.Version)
		if gerr != nil {
			return false, nil
		}
		sk = *existing
	default:
		return false, nil
	}
	if sk.Status == skill.SkillStatusPublished || sk.Status != skill.SkillStatusDraft {
		return false, nil
	}
	if err := s.Publish(ctx, sk.ID); err != nil {
		return false, nil
	}
	return true, nil
}

// EnsureComposeSkills installs and publishes templates that conversation
// specialists auto-attach (preferredSkills + Compose flag) so 选专家
// does not require hunting Skill Center.
func (s *Service) EnsureComposeSkills(ctx context.Context) (int, error) {
	if s == nil || s.write == nil {
		return 0, errors.New("skill writer unavailable")
	}
	want := composeTemplateWanted()
	published := 0
	for _, tpl := range catalogTemplates {
		if !want[tpl.ID] {
			continue
		}
		ok, err := s.publishCatalogTemplate(ctx, tpl)
		if err != nil {
			continue
		}
		if ok {
			published++
		}
	}
	return published, nil
}
