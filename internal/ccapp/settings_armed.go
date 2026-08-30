package ccapp

import (
	"strings"
	"time"
)

const (
	// CcMaxArmMinutes is the longest optional auto-off window.
	CcMaxArmMinutes = 120
)

func armExpired(now time.Time, armedUntil string) bool {
	armedUntil = strings.TrimSpace(armedUntil)
	if armedUntil == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, armedUntil)
	if err != nil {
		return true
	}
	return !now.UTC().Before(until)
}

func nextArmedUntil(now time.Time, patch SettingsPatch, cur Settings) string {
	if patch.Enabled != nil && !*patch.Enabled {
		return ""
	}
	if patch.ArmMinutes != nil {
		if *patch.ArmMinutes <= 0 {
			return ""
		}
		minutes := *patch.ArmMinutes
		if minutes > CcMaxArmMinutes {
			minutes = CcMaxArmMinutes
		}
		return now.UTC().Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
	}
	if patch.Enabled != nil && *patch.Enabled {
		return ""
	}
	return cur.ArmedUntil
}
