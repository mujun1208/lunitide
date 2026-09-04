package m8app

import "strings"

// OpsColleagueIDs are the five industry-operations colleague cards that
// share the MRO workbench, cite gate, and advisory banner.
var OpsColleagueIDs = []string{
	"mro-expert",
	"uas-airworthiness-expert",
	"tooling-chemical-expert",
	"parts-expert",
	"mx-planning-expert",
}

var opsColleagueNames = map[string]string{
	"航空机务维修专家":   "mro-expert",
	"航空机务专家":     "mro-expert",
	"低空适航专家":     "uas-airworthiness-expert",
	"航空工具化工品专家": "tooling-chemical-expert",
	"工具化工品专家":   "tooling-chemical-expert",
	"航空航材专家":     "parts-expert",
	"航空维修计划专家":  "mx-planning-expert",
}

// IsOpsColleague reports whether name or catalogID is one of the five
// operations colleagues (机务修 / 低空 / 工具化工品 / 航材 / 计划).
func IsOpsColleague(name, catalogID string) bool {
	id := strings.TrimSpace(catalogID)
	if id != "" {
		for _, want := range OpsColleagueIDs {
			if id == want {
				return true
			}
		}
	}
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	if _, ok := opsColleagueNames[n]; ok {
		return true
	}
	for _, want := range OpsColleagueIDs {
		if n == want {
			return true
		}
	}
	if item, ok := ConversationExpertByName(n); ok {
		for _, want := range OpsColleagueIDs {
			if item.ID == want {
				return true
			}
		}
	}
	return false
}

func conversationExpertNameAliases(id string) []string {
	switch id {
	case "mro-expert":
		return []string{"航空机务专家"}
	case "tooling-chemical-expert":
		return []string{"工具化工品专家"}
	default:
		return nil
	}
}
