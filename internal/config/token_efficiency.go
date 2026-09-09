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
