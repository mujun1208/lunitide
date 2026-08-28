package m8app

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog/conversation_experts.json
var conversationExpertsJSON []byte

// ConversationExpertIDs are the product-shipped 对话 specialists seeded into
// the expert library so the composer picker can select them like builtins.
var ConversationExpertIDs = []string{
	"ppt-expert", "report-writer", "novel-writer", "excel-maker", "ui-designer",
	"pm-expert", "architect-expert", "db-expert", "repo-expert", "standards-expert",
	"test-expert", "hardware-expert", "dev-expert",
}

// ConversationExperts answers the 对话 specialists (PPT / 报告 / 小说 /
// Excel / UI / 产品经理 / 系统架构师 / 数据库设计 / 系统项目结构 / 开发规范 /
// 系统测试 / 硬件配置 / 开发) in catalog order.
func ConversationExperts() []CatalogItem {
	items := loadConversationExpertItems()
	want := make(map[string]bool, len(ConversationExpertIDs))
	for _, id := range ConversationExpertIDs {
		want[id] = true
	}
	out := make([]CatalogItem, 0, len(ConversationExpertIDs))
	for _, item := range items {
		if want[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

func loadConversationExpertItems() []CatalogItem {
	var file catalogFile
	if err := json.Unmarshal(conversationExpertsJSON, &file); err != nil {
		return nil
	}
	return file.Items
}

func appendConversationExperts(base []CatalogItem) []CatalogItem {
	extra := loadConversationExpertItems()
	if len(extra) == 0 {
		return base
	}
	out := make([]CatalogItem, 0, len(base)+len(extra))
	out = append(out, base...)
	return append(out, extra...)
}
