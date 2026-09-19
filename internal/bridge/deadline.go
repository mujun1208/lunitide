package bridge

// Envelope and method deadline ceilings. Most RPCs stay at 30s so a stuck
// handler cannot pin the Engine. Long-running meeting notes, people file
// transfer, people region snip (180s), app updates, and provider diagnostics
// (which may wait for asynchronous video generation) are exceptions.
const (
	DefaultMaxDeadlineMS    = 30_000
	MeetingLiveDeadlineMS   = 120_000
	MeetingNotesDeadlineMS  = 600_000
	AppUpdateInstallMS      = 120_000
	PeopleFileDeadlineMS    = 120_000
	PeopleCaptureDeadlineMS = 180_000
	TemplateFileDeadlineMS  = 120_000
	ChatStartDeadlineMS        = 120_000
	McpSetupDeadlineMS         = 80_000
	ProviderTestDeadlineMS     = 360_000
	AgentHubPickDeadlineMS     = 600_000
	AgentHubPromptDeadlineMS   = 180_000
	OcrRoutingRepairDeadlineMS = 180_000
)

// MaxDeadlineMS is the largest deadlineMs the Host/Engine accept for method.
func MaxDeadlineMS(method string) int {
	switch Method(method) {
	case MethodProviderTest:
		return ProviderTestDeadlineMS
	case "office.artifact.validate", "office.artifact.refresh":
		return 120_000
	case "meetings.summarize", "meetings.catchup":
		return MeetingNotesDeadlineMS
	case "meetings.append", "meetings.audio.append", "meetings.stop", "meetings.heartbeat", "meetings.get", "meetings.export":
		return MeetingLiveDeadlineMS
	case MethodAppUpdateInstall:
		return AppUpdateInstallMS
	case MethodPeopleFileStage, MethodPeopleFilePick, MethodPeopleThreadSend, MethodDesktopFilesPick:
		return PeopleFileDeadlineMS
	case MethodPeopleScreenCapture:
		return PeopleCaptureDeadlineMS
	case MethodTemplateCreate, MethodTemplateFileStage:
		return TemplateFileDeadlineMS
	case MethodMcpAdd, MethodMcpToggle, MethodMcpHealth:
		return McpSetupDeadlineMS
	case MethodChatStart:
		return ChatStartDeadlineMS
	case MethodAgentHubDirPick, MethodAgentHubInbox, MethodAgentHubInstall, "project.root.pick":
		return AgentHubPickDeadlineMS
	case MethodAgentHubThreadCreate, MethodAgentHubThreadPrompt, MethodAgentHubThreadRespond, MethodAgentHubThreadCancel:
		return AgentHubPromptDeadlineMS
	case MethodMediaAssetPick:
		return PeopleFileDeadlineMS
	case MethodOcrRoutingGet:
		return OcrRoutingRepairDeadlineMS
	default:
		return DefaultMaxDeadlineMS
	}
}

// InnerDeadlineMS clamps a host-forwarded outer deadline to the inner method's
// own ceiling. Picker RPCs may wait minutes; register/attach must not inherit that.
func InnerDeadlineMS(method string, outer int) int {
	cap := MaxDeadlineMS(method)
	if outer < 1 {
		if cap < DefaultMaxDeadlineMS {
			return cap
		}
		return DefaultMaxDeadlineMS
	}
	if outer > cap {
		return cap
	}
	return outer
}
