package winexec

import (
	"errors"
	"strings"
)

var (
	ErrSMTCSeekVolumeDisabled        = errors.New("smtc seek/volume not enabled")
	errUnsupportedMediaSessionAction = errors.New("unsupported media session action")
)

type MediaSessionResult struct {
	App          string   `json:"app"`
	Status       string   `json:"status"`
	Title        string   `json:"title"`
	Artist       string   `json:"artist"`
	Verified     bool     `json:"verified"`
	Shuffle      bool     `json:"shuffle"`
	SessionKey   string   `json:"sessionKey"`
	Capabilities []string `json:"capabilities"`
	PositionMs   int64    `json:"positionMs,omitempty"`
	DurationMs   int64    `json:"durationMs,omitempty"`
}

func ValidateMediaSessionAction(action string) error {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "status", "play", "pause", "next", "prev", "stop":
		return nil
	case "seek", "set_volume", "volume":
		return ErrSMTCSeekVolumeDisabled
	default:
		return errUnsupportedMediaSessionAction
	}
}

func SMTCCapabilityAllowed(capabilities []string, action string) bool {
	if action == "status" {
		return true
	}
	want := action
	if action == "prev" {
		want = "previous"
	}
	for _, item := range capabilities {
		if item == want {
			return true
		}
	}
	return false
}

func sanitizeSMTCCapabilities(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, item := range in {
		switch item {
		case "play", "pause", "stop", "next", "previous":
			if !seen[item] {
				seen[item] = true
				out = append(out, item)
			}
		}
	}
	return out
}
