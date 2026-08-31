package m8app

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

//go:embed catalog/agency_agents.json
var agencyAgentsJSON []byte

const (
	CatalogUsageChat    = "chat"
	CatalogUsageProject = "project"
	CatalogUsageBoth    = "both"
	catalogSkillPrefix  = "aa-"
	catalogListDescMax  = 280
)

// CatalogItem is one installable agency-agents role.
type CatalogItem struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Division    string            `json:"division"`
	Origin      string            `json:"origin"`
	Usage       string            `json:"usage"`
	Scene       string            `json:"scene"`
	Emoji           string            `json:"emoji"`
	Version         string            `json:"version"`
	PreferredSkills []string          `json:"preferredSkills,omitempty"`
	RequiredTools   []string          `json:"requiredTools,omitempty"`
	PreferredMcp    []string          `json:"preferredMcp,omitempty"`
	McpFallback     string            `json:"mcpFallback,omitempty"`
	Kind            string            `json:"kind,omitempty"`
	SixSection      m8core.SixSection `json:"sixSection"`
}

// CatalogSummary is the market-card projection of one catalog item.
type CatalogSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Division    string `json:"division"`
	Origin      string `json:"origin"`
	Usage       string `json:"usage"`
	Scene       string `json:"scene"`
	Emoji       string `json:"emoji"`
	Version     string `json:"version"`
	Kind        string `json:"kind,omitempty"`
	Installed   bool   `json:"installed"`
}

type catalogFile struct {
	Items []CatalogItem `json:"items"`
}

var (
	catalogOnce  sync.Once
	catalogItems []CatalogItem
)

// AgencyAgentsCatalog answers the product-shipped agency-agents market plus
// the conversation specialists (PPT / 报告 / 小说 / Excel / UI / 产品经理 /
// 系统架构师 / 数据库设计 / 系统项目结构 / 开发规范 / 系统测试 / 硬件配置 / 开发).
func AgencyAgentsCatalog() []CatalogItem {
	catalogOnce.Do(func() {
		var file catalogFile
		if err := json.Unmarshal(agencyAgentsJSON, &file); err != nil {
			return
		}
		catalogItems = appendConversationExperts(file.Items)
	})
	out := make([]CatalogItem, len(catalogItems))
	copy(out, catalogItems)
	return out
}

// SkillName is the local skill name materialized for chat-usable roles.
func (item CatalogItem) SkillName() string {
	return catalogSkillPrefix + item.ID
}

// NeedsChat answers whether install should publish a chat skill.
// Independent agents keep skills on the expert (bindings), never as aa-* chips.
func (item CatalogItem) NeedsChat() bool {
	if item.ResolvedKind() == ExpertKindAgent {
		return false
	}
	return item.Usage == CatalogUsageChat || item.Usage == CatalogUsageBoth
}

// NeedsProject answers whether install should create a project expert.
func (item CatalogItem) NeedsProject() bool {
	if item.ResolvedKind() == ExpertKindAgent {
		return true
	}
	return item.Usage == CatalogUsageProject || item.Usage == CatalogUsageBoth
}

// Summary projects the market card, clipping the description for the list.
func (item CatalogItem) Summary(installed bool) CatalogSummary {
	display := item.DisplayName
	if display == "" {
		display = item.Name
	}
	return CatalogSummary{
		ID: item.ID, Name: item.Name, DisplayName: display,
		Description: clipRunes(item.Description, catalogListDescMax),
		Category:    item.Category, Division: item.Division, Origin: item.Origin,
		Usage: item.Usage, Scene: item.Scene, Emoji: item.Emoji, Version: item.Version,
		Kind: item.ResolvedKind(), Installed: installed,
	}
}

func clipRunes(s string, max int) string {
	if max < 1 || utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
