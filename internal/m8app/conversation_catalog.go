package m8app

import (
	_ "embed"
	"encoding/json"
	"strings"
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

// ConversationExpertCapabilityClause is appended to every specialist's rules
// so catalog seeds instruct think / skills / tools / draw / write. Runtime
// chat also injects the same contract (clipped six-section cannot drop it).
const ConversationExpertCapabilityClause = "任务过程中必须思考（todo.write）；需要技能立刻 skill.invoke；事实与素材先 web.search（必要时 web.fetch / browser.act）；结构/流程/架构用 mermaid 画图（节点双引号，换行 <br/>）；成文调用 docx.gen / excel.gen / pptx.gen / html.gen / workspace.write（桌面 desktop=true）。不要只口头交差，不要倾倒 200 页全书。"

func withConversationExpertCapabilities(item CatalogItem) CatalogItem {
	item.SixSection.Identity = honestColleagueExpertIdentity(item.SixSection.Identity)
	body := item.SixSection.Rules + "\n" + item.SixSection.Workflow + "\n" + item.SixSection.Mission
	if strings.Contains(body, "skill.invoke") && strings.Contains(body, "web.search") && strings.Contains(body, "mermaid") {
		return item
	}
	item.SixSection.Rules = strings.TrimSpace(item.SixSection.Rules) + "\n" + ConversationExpertCapabilityClause
	return item
}

// Catalog seeds used to say「独立智能体」. That is a persona on the same
// engine, not a process. Keep the recipe; change the lie.
func honestColleagueExpertIdentity(identity string) string {
	identity = strings.ReplaceAll(identity, "你是独立智能体：", "你是同事专家（同一月汐引擎，不是独立进程）：")
	identity = strings.ReplaceAll(identity, "你是独立智能体，", "你是同事专家（同一月汐引擎，不是独立进程），")
	identity = strings.ReplaceAll(identity, "独立智能体", "同事专家")
	return identity
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
			out = append(out, withConversationExpertCapabilities(item))
		}
	}
	return out
}

func loadConversationExpertItems() []CatalogItem {
	var file catalogFile
	if err := json.Unmarshal(conversationExpertsJSON, &file); err != nil {
		return nil
	}
	for i := range file.Items {
		file.Items[i] = withConversationExpertCapabilities(file.Items[i])
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
