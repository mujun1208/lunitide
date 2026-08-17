// M10 skill-center category taxonomy: the 12 fixed categories and the
// sk_category_map mapping model (migration 0073). Resolution priority is
// manual > manifest > keyword with an "other" fallback; the mapping table
// never alters the M6 skills authority tables.
package skill

import (
	"encoding/json"
	"strings"
)

// Category is one of the 12 fixed skill-center categories.
type Category string

const (
	CategoryEfficiency  Category = "efficiency"
	CategoryWriting     Category = "writing"
	CategoryDevelopment Category = "development"
	CategoryData        Category = "data"
	CategoryDesign      Category = "design"
	CategoryResearch    Category = "research"
	CategoryLifestyle   Category = "lifestyle"
	CategoryEducation   Category = "education"
	CategoryBusiness    Category = "business"
	CategoryAutomation  Category = "automation"
	CategorySecurity    Category = "security"
	CategoryOther       Category = "other"
)

// AllCategories lists the 12 categories in UI chip order.
var AllCategories = []Category{
	CategoryEfficiency, CategoryWriting, CategoryDevelopment, CategoryData,
	CategoryDesign, CategoryResearch, CategoryLifestyle, CategoryEducation,
	CategoryBusiness, CategoryAutomation, CategorySecurity, CategoryOther,
}

// ValidCategory reports whether c is one of the 12 fixed categories.
func ValidCategory(c string) bool {
	switch Category(c) {
	case CategoryEfficiency, CategoryWriting, CategoryDevelopment, CategoryData,
		CategoryDesign, CategoryResearch, CategoryLifestyle, CategoryEducation,
		CategoryBusiness, CategoryAutomation, CategorySecurity, CategoryOther:
		return true
	}
	return false
}

// CategorySource records how a mapping was produced. Priority: manual >
// manifest > keyword (design M10 SkillCategoryService).
type CategorySource string

const (
	CategorySourceManual   CategorySource = "manual"
	CategorySourceManifest CategorySource = "manifest"
	CategorySourceKeyword  CategorySource = "keyword"
)

// ValidCategorySource reports whether s is a known mapping source.
func ValidCategorySource(s string) bool {
	switch CategorySource(s) {
	case CategorySourceManual, CategorySourceManifest, CategorySourceKeyword:
		return true
	}
	return false
}

// CategoryMap is one sk_category_map row (migration 0073). One skill has at
// most one mapping (UNIQUE(skill_id)).
type CategoryMap struct {
	ID        string         `json:"id"`
	SkillID   string         `json:"skillId"`
	Category  Category       `json:"category"`
	Source    CategorySource `json:"matchSource"`
	UpdatedAt string         `json:"updatedAt"`
}

// CategoryFromManifest extracts a declared category from a skill manifest
// JSON object. Only the 12 fixed categories count as valid declarations.
func CategoryFromManifest(manifestJSON string) (Category, bool) {
	var m struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		return "", false
	}
	if !ValidCategory(m.Category) {
		return "", false
	}
	return Category(m.Category), true
}

// categoryKeywords is the keyword-rule table: first match over the folded
// name+description wins. More specific domains are ordered before general
// efficiency wording so e.g. "code formatting" resolves to development.
var categoryKeywords = []struct {
	Category Category
	Words    []string
}{
	{CategoryDevelopment, []string{"开发", "编程", "代码", "调试", "重构", "编译", "接口", "code", "coding", "program", "sql", "api", "debug", "refactor", "compile", "lint", "git"}},
	{CategoryAutomation, []string{"自动化", "定时", "流水线", "工作流", "批处理", "automation", "workflow", "pipeline", "schedule", "cron", "batch"}},
	{CategorySecurity, []string{"安全", "加密", "密码", "隐私", "漏洞", "security", "encrypt", "password", "privacy", "vulnerab", "secret"}},
	{CategoryData, []string{"数据", "分析", "报表", "图表", "统计", "excel", "csv", "data", "analytic", "chart", "metric", "report"}},
	{CategoryWriting, []string{"写作", "文案", "润色", "翻译", "摘要", "总结", "writing", "draft", "polish", "translate", "summar"}},
	{CategoryDesign, []string{"设计", "图标", "配色", "原型", "图像", "design", "icon", "palette", "prototype", "ui", "ux", "logo", "image"}},
	{CategoryResearch, []string{"研究", "检索", "论文", "文献", "调研", "research", "paper", "literature", "survey"}},
	{CategoryEducation, []string{"教育", "学习", "课程", "教学", "练习", "education", "learn", "course", "tutor", "quiz"}},
	{CategoryBusiness, []string{"商业", "运营", "营销", "销售", "客户", "business", "marketing", "sales", "crm", "invoice"}},
	{CategoryLifestyle, []string{"生活", "健康", "饮食", "旅行", "天气", "日历", "life", "health", "recipe", "travel", "weather"}},
	{CategoryEfficiency, []string{"效率", "快捷", "格式化", "转换", "模板", "效率", "efficiency", "format", "convert", "template", "snippet"}},
}

// CategoryFromKeywords maps the folded name+description onto one category
// using the keyword rules. False means no rule matched.
func CategoryFromKeywords(name, description string) (Category, bool) {
	folded := strings.ToLower(name + " " + description)
	for _, rule := range categoryKeywords {
		for _, w := range rule.Words {
			if strings.Contains(folded, w) {
				return rule.Category, true
			}
		}
	}
	return "", false
}
