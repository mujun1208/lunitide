package m8app

import (
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

const (
	ExpertKindPromptSkill = "prompt_skill"
	ExpertKindAgent       = "agent"
)

// ExpertKindForName answers agent for the 13 conversation specialists and
// prompt_skill for everyone else (market cards, builtins, user-created).
func ExpertKindForName(name string) string {
	return ExpertKindForExpert(name, "")
}

// ResolveConversationExpert prefers the stored catalog id so a renamed
// specialist still maps to the shipped roster. Name-only matching remains
// for rows that predate catalog_item_id.
func ResolveConversationExpert(name, catalogItemID string) (CatalogItem, bool) {
	if item, ok := ConversationExpertByID(catalogItemID); ok {
		return item, true
	}
	return ConversationExpertByName(name)
}

// ExpertKindForExpert prefers the stored catalog id so a renamed specialist
// stays agent. Name-only matching remains for rows that predate catalog_item_id.
func ExpertKindForExpert(name, catalogItemID string) string {
	if _, ok := ResolveConversationExpert(name, catalogItemID); ok {
		return ExpertKindAgent
	}
	return ExpertKindPromptSkill
}

func DivisionRole(division string) string {
	switch division {
	case m8core.DivisionDesign:
		return "设计师"
	case m8core.DivisionEngineering, m8core.DivisionOperations, m8core.DivisionSecurity:
		return "工程师"
	case m8core.DivisionProduct, m8core.DivisionProjectManagement:
		return "产品"
	default:
		return "研究员"
	}
}

// ResolvedKind answers the catalog kind, defaulting conversation specialists
// to agent and everything else to prompt_skill.
func (item CatalogItem) ResolvedKind() string {
	if item.Kind == ExpertKindAgent || item.Kind == ExpertKindPromptSkill {
		return item.Kind
	}
	if _, ok := ConversationExpertByID(item.ID); ok {
		return ExpertKindAgent
	}
	return ExpertKindPromptSkill
}
