package app

import (
	"regexp"
	"strings"
)

// Mirrors web/src/provider/modelKind.ts isCompanionFlashModelId /
// pickCompanionFlashModel. Used by plan.verify (P0) so the judge is a
// cheaper same-vendor flash id when one exists.
var (
	flashModelRE    = regexp.MustCompile(`(?i)(?:flash|air|lite|mini|haiku)`)
	flashRealtimeRE = regexp.MustCompile(`(?i)realtime(?:-preview)?|(?:^|[^A-Za-z0-9])live(?:[^A-Za-z0-9]|$)`)
)

func isFlashModelID(modelID string) bool {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return false
	}
	if flashRealtimeRE.MatchString(id) {
		return false
	}
	return flashModelRE.MatchString(id)
}

// pickFlashModelID mirrors pickCompanionFlashModel: keep current when it
// is already listed, else the first flash/air/lite/mini/haiku id.
func pickFlashModelID(current string, candidates []string) string {
	cur := strings.TrimSpace(current)
	for _, id := range candidates {
		if strings.TrimSpace(id) == cur && cur != "" {
			return cur
		}
	}
	return pickJudgeModelID(cur, candidates)
}

// pickJudgeModelID always prefers a flash-class id (D-C1). Chat model is
// the fallback when the vendor has no flash/air/lite/mini/haiku listing.
func pickJudgeModelID(chatModel string, candidates []string) string {
	for _, id := range candidates {
		if isFlashModelID(id) {
			return strings.TrimSpace(id)
		}
	}
	return strings.TrimSpace(chatModel)
}
