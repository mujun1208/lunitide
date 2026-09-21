package app

import "testing"

// A spoken turn driving the desktop used to be capped lower than the identical
// typed turn, so the same job stopped halfway when asked out loud and the user
// saw it as a quota wall. Steps are about how long the work takes; the
// keep-it-short rule is about how much the assistant says.
func TestVoiceAndTypedDesktopTurnsEarnTheSameSteps(t *testing.T) {
	lane := buildLaneContract(LaneL4, RouteR2, CouncilOverlay{})
	const goal = "打开浏览器，点开第一个链接"
	if !turnMayEarnMoreSteps(false, true, goal, lane) {
		t.Fatal("typed desktop turn cannot extend")
	}
	if !turnMayEarnMoreSteps(true, true, goal, lane) {
		t.Fatal("spoken desktop turn is still capped below the typed one")
	}
}

// A spoken turn that is only chatting keeps its fixed budget: nothing is being
// driven, so more steps would only make the reply ramble.
func TestVoiceChatWithoutDesktopWorkKeepsItsFixedBudget(t *testing.T) {
	lane := buildLaneContract(LaneL4, RouteR2, CouncilOverlay{})
	if turnMayEarnMoreSteps(true, false, "跟我聊聊天气", lane) {
		t.Fatal("spoken chat turn extended without doing any desktop work")
	}
}

// Lookups that are meant to be two steps stay pinned, spoken or typed.
func TestInventoryLookupNeverEarnsMoreStepsEitherWay(t *testing.T) {
	lane := buildLaneContract(LaneL4, RouteR2, CouncilOverlay{})
	const goal = "帮我查一下明天去上海的高铁车次"
	if !inventoryLookupBlocksPublicWeb(goal) {
		t.Fatalf("test goal is no longer read as an inventory lookup: %q", goal)
	}
	for _, companion := range []bool{false, true} {
		if turnMayEarnMoreSteps(companion, true, goal, lane) {
			t.Fatalf("inventory lookup extended (companion=%v)", companion)
		}
	}
}

// A lane that forbids extension is respected regardless of channel.
func TestLaneVetoOnExtensionHoldsForVoiceToo(t *testing.T) {
	lane := buildLaneContract(LaneL1, RouteUnspecified, CouncilOverlay{})
	for _, companion := range []bool{false, true} {
		if turnMayEarnMoreSteps(companion, true, "装好这批技能", lane) {
			t.Fatalf("lane veto ignored (companion=%v)", companion)
		}
	}
}
