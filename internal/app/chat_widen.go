package app

import (
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// widenInput is the closed set used to decide a single tool-surface widen.
// It does not grow keyword route tables.
type widenInput struct {
	AlreadyWidened      bool
	Goal                string
	TaskRoute           TaskRoute
	AssistantText       string
	SuccessfulTools     int
	PrevWasToolGoal     bool
	PrevSuccessfulTools int
}

func looksLikeShortNudge(text string) bool {
	if companionRetryActionTurn(text) || looksLikeResume(text) {
		return true
	}
	t := strings.TrimSpace(text)
	if t == "" || utf8.RuneCountInString(t) > 12 {
		return false
	}
	return strings.Contains(t, "没做") || strings.Contains(t, "继续")
}

func looksLikeToolRefusal(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	for _, n := range []string{
		"我没有这个工具", "没有这个工具", "做不到", "无法执行", "无法完成",
		"i don't have", "i do not have", "cannot do that", "can't do that",
		"我没法", "没办法做",
	} {
		if strings.Contains(t, n) || strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

func inferredWidenRoute(goal string, stated TaskRoute) TaskRoute {
	if stated != RouteUnspecified {
		return stated
	}
	if route := detectTaskRoute(goal); route != RouteUnspecified {
		return route
	}
	if looksLikeReportTask(goal) || wantsOfficeGen(goal) {
		return RouteR4
	}
	return RouteUnspecified
}

func shouldWidenAndRetry(in widenInput) bool {
	if in.AlreadyWidened || in.SuccessfulTools > 0 {
		return false
	}
	if isShortIdleGreeting(in.Goal) {
		return false
	}
	if inferredWidenRoute(in.Goal, in.TaskRoute) != RouteUnspecified && looksLikeToolRefusal(in.AssistantText) {
		return true
	}
	if looksLikeShortNudge(in.Goal) && in.PrevWasToolGoal && in.PrevSuccessfulTools == 0 {
		return true
	}
	return false
}

func widenLaneContract() LaneContract {
	return buildLaneContract(LaneL4, RouteUnspecified, CouncilOverlay{})
}

func applyWidenedTools(full []llmadapter.ToolDefinition) []llmadapter.ToolDefinition {
	return applyLaneTools(full, widenLaneContract())
}

func goalLooksLikeToolTarget(goal string) bool {
	g := chatRoutingText(goal)
	if g == "" || isShortIdleGreeting(g) {
		return false
	}
	if companionWantsTools(g) {
		return true
	}
	return detectTaskRoute(g) != RouteUnspecified
}
