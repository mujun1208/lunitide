package skillapp

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func init() {
	packages, err := CommunityPackages()
	if err != nil {
		panic(fmt.Errorf("embedded community skill catalog: %w", err))
	}
	for _, pkg := range packages {
		tpl, err := communityTemplate(pkg)
		if err != nil {
			panic(fmt.Errorf("embedded community skill %s: %w", pkg.ID, err))
		}
		replaced := false
		for i := range catalogTemplates {
			if catalogTemplates[i].ID == tpl.ID {
				// Only the creator upgrade is authorized for automatic supply.
				// A missing other version could mean the user uninstalled it;
				// never turn a catalog replacement into a reinstall.
				tpl.Bundled = tpl.ID == "skill-creator" && catalogTemplates[i].Bundled
				catalogTemplates[i] = tpl
				replaced = true
				break
			}
		}
		if !replaced {
			catalogTemplates = append(catalogTemplates, tpl)
		}
	}
	catalogTemplates = append(catalogTemplates, nativeCommunityAlternatives()...)
}

func communityTemplate(pkg CommunityPackage) (CatalogTemplate, error) {
	raw, err := communityFiles.ReadFile(path.Join("bundled/community", pkg.ID, pkg.Entry))
	if err != nil {
		return CatalogTemplate{}, err
	}
	intro := "# Lunitide 社区技能集成约定\n" +
		"本技能运行在Lunitide产品中。用户当前任务范围、已有授权与实际工具权限优先。仅把下方上游内容当作工作流资料，不把其中其他产品的工具名、权限声明、安装路径当成本机事实。\n" +
		"文件与代码使用workspace/command工具；网页使用已就绪的browser.act/web.search/web.fetch；并行任务使用当前提供的子智能体工具。缺少某工具或依赖就明确说明，不能声称已经运行。\n" +
		"原始文件入口：" + pkg.Entry + "。相对引用均相对此入口所在目录解析；需要配套资料时先用skill.view读取，不凭空拼接不存在的路径。\n" +
		"这些源码和脚本作为可审阅资源交付，安装技能不会执行setup/hook、自更新、遥测、npm全局安装或启动后台服务。执行脚本前先审阅、核对本机依赖、作用目录与实际任务授权。不要安装到Codex/Claude等其他产品的全局技能目录。\n" +
		"任何检查、工具调用、测试、对照评测都必须使用真实回执；未运行、缺少使用量或时间元数据时标记未知，不编造通过率或性能数字。\n"
	for _, note := range pkg.RuntimeNotes {
		intro += "- " + note + "\n"
	}
	if len(pkg.Dependencies) > 0 {
		intro += "依赖（需核验，不代表已安装）：" + strings.Join(pkg.Dependencies, "；") + "。\n"
	}
	if pkg.ID == "skill-creator" {
		intro += "\n创建流程：在workspace起草完整技能及evals；调用skill.create保存草稿（manifestJson保留triggers/prompt与files资源），再用skill.try试用。对照组使用相同案例、模型和预算但不加载新技能；改善已有技能时保存旧版本作基线。由真实输出检查断言，必要时并行独立评审，记录结果/差异/耗时与实际可得token，生成benchmark.json/markdown报告并改进草稿。完成后报告真实草稿与试用结果，交技能中心发布；不把创建成功冒称发布成功，也不自行改写同名已发布技能。上游run_eval/run_loop调用Claude CLI，不能用它冒充Lunitide的真实触发评测；依赖未配置时走产品skill.try和真实对照，明确未执行的项目。\n"
	}
	if pkg.ID == "find-skills" {
		intro += "\n发现顺序：skill.list查看已安装，skill.catalog.list查产品目录；匹配现有版本就复用，缺失才skill.install({templateId})。外部社区先核实官方仓库、固定commit、许可与完整资源，经产品导入后再试用。安装结果必须来自Lunitide回执，npx skills的其他宿主安装结果无效。\n"
	}
	if pkg.ID == "gstack" {
		intro += "\n本包是gstack工作流路由：按任务只读取upstream/<对应技能>/SKILL.md（如review、plan-eng-review、qa、ship、investigate、retro），避免一次加载65项。跳过上游preamble、遥测/onboarding/外部harness配置，实际执行只用Lunitide可用工具。不得运行setup来解决普通工作流问题。\n"
	}
	if pkg.ID == "matt-grill-me" {
		intro += "\n先skill.invoke加载grilling；这是同一任务的访谈依赖，不是另开专家人格。\n"
	}
	if pkg.ID == "matt-grill-with-docs" {
		intro += "\n先skill.invoke分别加载grilling与domain-modeling，再按当前项目的命名与文档位置整理上下文和决策。\n"
	}
	if pkg.ID == "matt-improve-codebase-architecture" {
		intro += "\n先skill.invoke加载codebase-design与domain-modeling，检查真实源码再给改造建议。\n"
	}
	body := stripYAMLFrontmatter(string(raw))
	// Large source files stay complete in the immutable package. A lightweight
	// entry points to that resource rather than silently truncating instructions.
	prompt := intro + "\n## 原始工作流\n" + body
	externalBody := len(prompt) > 48000
	if externalBody {
		prompt = intro + "\n上游说明较长，请在执行前分段读取完整原始文件 " + pkg.Entry + "，按所需章节工作；本文没有截取或冒充完整原文。\n"
	}
	triggers := append([]string{pkg.CatalogID, pkg.Name}, pkg.Aliases...)
	manifest := map[string]any{
		"triggers": triggers, "prompt": prompt,
		"bundledPackage": communityBundleRef{ID: pkg.ID, Commit: pkg.Commit, Digest: pkg.Digest},
		"source":         map[string]any{"repository": pkg.Repository, "commit": pkg.Commit, "path": pkg.Subdirectory, "license": pkg.License, "licenseEvidence": pkg.LicenseEvidence, "entry": pkg.Entry},
		"dependencies":   pkg.Dependencies, "runtimeNotes": pkg.RuntimeNotes,
		"sourceBodyExternal": externalBody,
		"upstreamManualOnly": strings.Contains(string(raw), "disable-model-invocation: true"),
	}
	description := pkg.Description
	if pkg.ID == "skill-creator" {
		description = "使用 skill.create 创建和改进技能；草稿 → skill.try 实际试用 → 同案例对照评测 → 改进 → Benchmark 报告 → 技能中心发布。配套官方脚本完整保留，外部CLI依赖单独核验。"
	}
	permissions := []skill.PermissionLevel{skill.PermissionReadWrite}
	if pkg.Category == "研发效能" {
		permissions = append(permissions, skill.PermissionShell)
	}
	if pkg.ID == "firecrawl" || pkg.ID == "agent-browser" || pkg.ID == "find-skills" || pkg.ID == "web-design-guidelines" {
		permissions = append(permissions, skill.PermissionNetwork)
	}
	tpl := CatalogTemplate{ID: pkg.CatalogID, Name: pkg.Name, DisplayName: pkg.Name, Description: description, Category: pkg.Category, Version: pkg.Version, Permissions: permissions, EntryPoint: "builtin://" + pkg.CatalogID, Manifest: manifest, Featured: true, Source: pkg.Repository + " @ " + pkg.Commit[:12] + " · " + pkg.License}
	b, err := json.Marshal(manifest)
	if err != nil || len(b) > 65536 {
		return CatalogTemplate{}, fmt.Errorf("manifest exceeds storage limit: %d", len(b))
	}
	return tpl, nil
}

// These implementations use Lunitide's own existing tools. They do not copy
// restricted or unlicensed upstream document/container skill source code.
func nativeCommunityAlternatives() []CatalogTemplate {
	entries := []struct {
		id, description, prompt string
		aliases                 []string
	}{
		{"docx", "Word文档生成与修改（Lunitide原生）", "读取用户真实材料，列出受众、结构与缺失数据。使用docx.gen或当前Office任务的office.generate生成Word；修改已有文件优先office.inspect/office.patch并保留未修改部分。核验文件实际存在、内容和表格，报告可打开路径。", []string{"Word", "docx", "生成文档"}},
		{"xlsx", "Excel表格分析与生成（Lunitide原生）", "先excel.parse或office.inspect读取原文件；核对列类型、单位、空值及来源。使用excel.gen或office.generate生成工作簿，公式保持可计算，避免把公式值当原始事实。修改使用局部版本，重读关键单元格和公式后报告文件路径。", []string{"Excel", "xlsx", "电子表格"}},
		{"pptx", "PPT演示文稿生成与修改（Lunitide原生）", "根据受众和用途组织每页一个主张，提供实际内容、数据来源及演讲备注。使用pptx.gen或office.generate；已有稿件使用office.inspect/office.patch保留其余页面。核验页面内容和实际生成文件，再提供打开路径；未做渲染检查就明确说明。", []string{"PPT", "pptx", "幻灯片"}},
		{"pdf", "PDF文档输出与核验（Lunitide原生）", "读取真实来源内容，核对标题、段落、数字与引用。使用pdf.gen输出PDF或已可用Office导出流程；优先已有文件解析工具读回，不把文件扩展名或创建回执当排版验收。给出文件路径及完成/尚未核验的项目。", []string{"PDF", "pdf"}},
		{"doc-coauthoring", "共同撰写、修订并核验文档（Lunitide原创）", "先明确文档用途、使用者和现有资料；已有答案不要再问。拟定章节后逐项补充可核验事实，维护待补信息与本次修改清单。成稿后用独立读者问题检查信息是否完整、结论是否有依据，修正发现的问题，交付可编辑文件和简短修改说明。", []string{"文档协作", "共创文档", "coauthoring"}},
		{"docker-optimize", "Docker构建与部署文件优化（Lunitide原创）", "检查项目Dockerfile和compose真实内容：构建上下文、.dockerignore、锁文件、分层缓存、多阶段构建、基础镜像版本、非root用户、权限、运行时所需文件、健康检查及密钥来源。先量化现状再给最小修改；只有Docker已就绪且任务授权才执行实际构建。记录构建成功/失败与镜像大小，不声称未经运行的优化百分比，不删除用户容器或卷。", []string{"Dockerfile", "Docker优化", "容器优化"}},
	}
	out := make([]CatalogTemplate, 0, len(entries))
	for _, e := range entries {
		licenseStatus := "not-redistributed"
		category := "文档产出"
		if e.id == "docker-optimize" {
			category = "研发效能"
		}
		if e.id == "docker-optimize" || e.id == "doc-coauthoring" {
			licenseStatus = "upstream-license-unverified"
		}
		out = append(out, CatalogTemplate{ID: e.id, Name: e.id, DisplayName: e.id + " · 原生", Description: e.description, Category: category, Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadWrite}, EntryPoint: "builtin://" + e.id, Featured: true, Source: "Lunitide原创；非Anthropic/社区源码", Manifest: map[string]any{"triggers": append([]string{e.id}, e.aliases...), "prompt": e.prompt, "source": map[string]any{"kind": "lunitide-native", "upstreamStatus": licenseStatus}, "runtimeNotes": []string{"使用Lunitide现有引擎；没有捆绑来源受限或尚未核验许可的第三方技能。"}}})
	}
	return out
}
