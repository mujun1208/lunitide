package config

import (
	"os"
	"strings"
)

// TokenEfficiencyEnabled is a process-wide rollout switch, shared by every
// model and conversation. Switching off restores original prompt projection;
// it does not alter provider caches, stored messages or metering.
func TokenEfficiencyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LUNITIDE_TOKEN_EFFICIENCY"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// TokenEfficiencyScope is the user-visible contract for the process switch.
// It only covers reversible request JSON compacting and exact evidence
// dedup. Independent summaries, provider caches, and call metering stay on.
type TokenEfficiencyScope struct {
	RequestJSONCompact bool
	EvidenceDedup      bool
	IndependentSummary bool
	ProviderCache      bool
	Metering           bool
	AppliesTo          string
}

func CurrentTokenEfficiencyScope() TokenEfficiencyScope {
	on := TokenEfficiencyEnabled()
	return TokenEfficiencyScope{
		RequestJSONCompact: on,
		EvidenceDedup:      on,
		AppliesTo:          "process_restart",
	}
}
