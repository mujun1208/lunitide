package m8core

import "testing"

func TestResolveMemoryBehaviorOffBlocksCaptureAndRecall(t *testing.T) {
	flags := CurrentProductFlags()
	off := MemoryV2Settings{CaptureMode: "off", PersonalMemoryEnabled: true, ProjectMemoryEnabled: true}
	got := ResolveMemoryBehavior(off, "user", flags)
	if got.AllowRecall || got.AllowAutoCapture || got.AllowWorking || got.AllowExplicitSave || !got.AllowManagement {
		t.Fatalf("off: %+v", got)
	}
}

func TestResolveMemoryBehaviorManualAllowsExplicitSaveOnly(t *testing.T) {
	flags := CurrentProductFlags()
	manual := MemoryV2Settings{CaptureMode: "manual", PersonalMemoryEnabled: true, ProjectMemoryEnabled: true}
	got := ResolveMemoryBehavior(manual, "user", flags)
	if !got.AllowRecall || !got.AllowExplicitSave || got.AllowAutoCapture || got.AllowWorking {
		t.Fatalf("manual: %+v", got)
	}
}

func TestResolveMemoryBehaviorClosedScopeBlocksWrites(t *testing.T) {
	flags := CurrentProductFlags()
	auto := MemoryV2Settings{CaptureMode: "auto", PersonalMemoryEnabled: false, ProjectMemoryEnabled: true}
	got := ResolveMemoryBehavior(auto, "user", flags)
	if got.AllowRecall || got.AllowAutoCapture || got.AllowExplicitSave {
		t.Fatalf("closed personal scope: %+v", got)
	}
	project := ResolveMemoryBehavior(auto, "project", flags)
	if !project.AllowAutoCapture || !project.AllowRecall {
		t.Fatalf("open project scope: %+v", project)
	}
}

func TestSettingsToV2TreatsDisabledMemoryAsOff(t *testing.T) {
	got := SettingsToV2(MemorySettings{MemoryEnabled: false, CaptureMode: "auto"})
	if got.CaptureMode != "off" {
		t.Fatalf("got %+v", got)
	}
}
