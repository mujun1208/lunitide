package bridge

import "testing"

func TestMaxDeadlineMSAllowsLongMeetings(t *testing.T) {
	if MaxDeadlineMS("system.health") != DefaultMaxDeadlineMS {
		t.Fatalf("health cap = %d", MaxDeadlineMS("system.health"))
	}
	if MaxDeadlineMS("meetings.append") != MeetingLiveDeadlineMS {
		t.Fatalf("append cap = %d", MaxDeadlineMS("meetings.append"))
	}
	if MaxDeadlineMS("meetings.stop") != MeetingLiveDeadlineMS {
		t.Fatalf("stop cap = %d", MaxDeadlineMS("meetings.stop"))
	}
	if MaxDeadlineMS("meetings.heartbeat") != MeetingLiveDeadlineMS {
		t.Fatalf("heartbeat cap = %d", MaxDeadlineMS("meetings.heartbeat"))
	}
	if MaxDeadlineMS("meetings.summarize") != MeetingNotesDeadlineMS {
		t.Fatalf("summarize cap = %d", MaxDeadlineMS("meetings.summarize"))
	}
	if MaxDeadlineMS("meetings.catchup") != MeetingNotesDeadlineMS {
		t.Fatalf("catchup cap = %d", MaxDeadlineMS("meetings.catchup"))
	}
	if MaxDeadlineMS("meetings.audio.append") != MeetingLiveDeadlineMS {
		t.Fatalf("audio append cap = %d", MaxDeadlineMS("meetings.audio.append"))
	}
	if MeetingLiveDeadlineMS <= 60_000 {
		t.Fatalf("live meeting RPCs must outlast a 60s mock: %d", MeetingLiveDeadlineMS)
	}
	if MaxDeadlineMS("people.file.stage") != PeopleFileDeadlineMS {
		t.Fatalf("people.file.stage cap = %d", MaxDeadlineMS("people.file.stage"))
	}
	if MaxDeadlineMS("people.thread.send") != PeopleFileDeadlineMS {
		t.Fatalf("people.thread.send cap = %d", MaxDeadlineMS("people.thread.send"))
	}
	if MaxDeadlineMS("people.file.pick") != PeopleFileDeadlineMS {
		t.Fatalf("people.file.pick cap = %d", MaxDeadlineMS("people.file.pick"))
	}
	if MaxDeadlineMS("desktop.files.pick") != PeopleFileDeadlineMS {
		t.Fatalf("desktop.files.pick cap = %d", MaxDeadlineMS("desktop.files.pick"))
	}
	if MaxDeadlineMS("people.screen.capture") != PeopleCaptureDeadlineMS {
		t.Fatalf("people.screen.capture cap = %d", MaxDeadlineMS("people.screen.capture"))
	}
	if MaxDeadlineMS("template.file.stage") != TemplateFileDeadlineMS {
		t.Fatalf("template.file.stage cap = %d", MaxDeadlineMS("template.file.stage"))
	}
	if MaxDeadlineMS("template.create") != TemplateFileDeadlineMS {
		t.Fatalf("template.create cap = %d", MaxDeadlineMS("template.create"))
	}
	if MaxDeadlineMS("chat.start") != ChatStartDeadlineMS {
		t.Fatalf("chat.start cap = %d", MaxDeadlineMS("chat.start"))
	}
}

func TestMaxDeadlineMSAgentHubPickers(t *testing.T) {
	if MaxDeadlineMS("agentHub.dir.pick") != 600_000 {
		t.Fatalf("dir.pick cap = %d", MaxDeadlineMS("agentHub.dir.pick"))
	}
	if MaxDeadlineMS("project.root.pick") != 600_000 {
		t.Fatalf("project.root.pick cap = %d", MaxDeadlineMS("project.root.pick"))
	}
	if MaxDeadlineMS("agentHub.inbox") != 600_000 {
		t.Fatalf("inbox cap = %d", MaxDeadlineMS("agentHub.inbox"))
	}
	if MaxDeadlineMS("agentHub.detect") != DefaultMaxDeadlineMS {
		t.Fatalf("detect must stay 30s: %d", MaxDeadlineMS("agentHub.detect"))
	}
}

func TestMaxDeadlineMSAllowsCursorPromptAndMediaPick(t *testing.T) {
	if MaxDeadlineMS("agentHub.thread.create") != AgentHubPromptDeadlineMS {
		t.Fatalf("create cap = %d", MaxDeadlineMS("agentHub.thread.create"))
	}
	if MaxDeadlineMS("agentHub.thread.prompt") != AgentHubPromptDeadlineMS {
		t.Fatalf("prompt cap = %d", MaxDeadlineMS("agentHub.thread.prompt"))
	}
	if MaxDeadlineMS("agentHub.thread.respond") != AgentHubPromptDeadlineMS {
		t.Fatalf("respond cap = %d", MaxDeadlineMS("agentHub.thread.respond"))
	}
	if MaxDeadlineMS("agentHub.thread.cancel") != AgentHubPromptDeadlineMS {
		t.Fatalf("cancel cap = %d", MaxDeadlineMS("agentHub.thread.cancel"))
	}
	if MaxDeadlineMS("media.asset.pick") != PeopleFileDeadlineMS {
		t.Fatalf("media pick cap = %d", MaxDeadlineMS("media.asset.pick"))
	}
	if MaxDeadlineMS("ocr.routing.get") != OcrRoutingRepairDeadlineMS {
		t.Fatalf("ocr routing cap = %d", MaxDeadlineMS("ocr.routing.get"))
	}
	if AgentHubPromptDeadlineMS <= DefaultMaxDeadlineMS {
		t.Fatalf("Cursor prompt must outlast the 30s default: %d", AgentHubPromptDeadlineMS)
	}
}

func TestInnerDeadlineMSClampsOversizedHealth(t *testing.T) {
	if got := InnerDeadlineMS("system.health", AgentHubPromptDeadlineMS); got != DefaultMaxDeadlineMS {
		t.Fatalf("health clamp = %d", got)
	}
	if got := InnerDeadlineMS("agentHub.thread.prompt", AgentHubPromptDeadlineMS); got != AgentHubPromptDeadlineMS {
		t.Fatalf("prompt must keep 180s, got %d", got)
	}
}

func TestInnerDeadlineMSDoesNotForwardPickerCeilings(t *testing.T) {
	if got := InnerDeadlineMS("internal.media.asset.register", PeopleFileDeadlineMS); got != DefaultMaxDeadlineMS {
		t.Fatalf("register must stay at 30s, got %d", got)
	}
	if got := InnerDeadlineMS("internal.media.player.attach", 8000); got != 8000 {
		t.Fatalf("attach must keep a short deadline, got %d", got)
	}
	if got := InnerDeadlineMS("media.asset.pick", PeopleFileDeadlineMS); got != PeopleFileDeadlineMS {
		t.Fatalf("pick itself may use the picker ceiling, got %d", got)
	}
	if got := InnerDeadlineMS("internal.media.asset.register", 0); got != DefaultMaxDeadlineMS {
		t.Fatalf("zero outer must still be a valid register deadline, got %d", got)
	}
}

func TestMcpSetupDeadlineOutlastsColdStartupWithoutExtendingCalls(t *testing.T) {
	for _, method := range []string{"mcp.add", "mcp.toggle", "mcp.health"} {
		if MaxDeadlineMS(method) != 80000 {
			t.Fatal(method, MaxDeadlineMS(method))
		}
	}
	for _, method := range []string{"mcp.invoke", "mcp6.invoke", "mcp.list"} {
		if MaxDeadlineMS(method) != DefaultMaxDeadlineMS {
			t.Fatal(method, MaxDeadlineMS(method))
		}
	}
}
