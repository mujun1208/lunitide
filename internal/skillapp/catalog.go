// P3-2 skill catalog: a static, product-shipped template catalog the skill
// center exposes as a local "market". Templates carry no signature (they
// are seeds, not loadable code): installing materializes a draft skill
// through the normal skill.create pipeline so every governance rule
// (permissions review, publish gate, optimistic versioning) still applies.
package skillapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

// ErrTemplateUnknown answers an install request naming a catalog id that
// does not exist; ErrTemplateInstalled answers a name+version collision.
var (
	ErrTemplateUnknown   = errors.New("skillapp: template unknown")
	ErrTemplateInstalled = errors.New("skillapp: template already installed")
)

// CatalogTemplate is one installable entry in the product catalog.
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

// catalogTemplates is the frozen local market list. Entry points point at
// the builtin pipeline namespace; manifests keep the trigger keywords the
// matcher scores and the working agreement the model sees on invoke.
var catalogTemplates = []CatalogTemplate{
	{
		ID: "meeting-minutes", Name: "tpl-meeting-minutes", DisplayName: "会议纪要助手",
		Description: "把散落在会话里的讨论整理成结构化会议纪要：结论、待办、责任人、截止时间，可导出为 Word。",
		Category:    "办公协作", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
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

// EnsureBundledSkills installs every catalog template and publishes drafts
// so ordinary chat can skill.invoke them without a market click. Already
// published rows are left alone; install collisions just attempt publish.
func (s *Service) EnsureBundledSkills(ctx context.Context) (int, error) {
	if s == nil || s.write == nil {
		return 0, errors.New("skill writer unavailable")
	}
	published := 0
	for _, tpl := range catalogTemplates {
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
