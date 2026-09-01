package app

import (
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

const (
	talkModelUnsupportedCode = "TALK_MODEL_UNSUPPORTED"
	talkAdapterUnreadyCode   = "TALK_ADAPTER_UNREADY"
	talkSessionMissingCode   = "TALK_SESSION_MISSING"
)

// talkRealtimeRe matches contract (?i)realtime|live|realtime-preview without
// treating olive / delivery as live.
var talkRealtimeRe = regexp.MustCompile(`(?i)realtime(?:-preview)?|(?:^|[^A-Za-z0-9])live(?:[^A-Za-z0-9]|$)`)

func isTalkRealtimeModelID(modelID, displayName string) bool {
	blob := strings.TrimSpace(modelID) + " " + strings.TrimSpace(displayName)
	return talkRealtimeRe.MatchString(blob)
}

func resolveTalkModel(p provider.Provider, modelID string) (provider.Model, bool) {
	if p.Protocol != provider.ProtocolOpenAICompatible || p.Status != provider.StatusEnabled {
		return provider.Model{}, false
	}
	want := strings.TrimSpace(modelID)
	if want == "" {
		return provider.Model{}, false
	}
	for _, model := range p.Models {
		if model.ModelID != want {
			continue
		}
		if isTalkRealtimeModelID(model.ModelID, model.DisplayName) {
			return model, true
		}
		return provider.Model{}, false
	}
	return provider.Model{}, false
}
