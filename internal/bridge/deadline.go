package bridge

// Envelope and method deadline ceilings. Most RPCs stay at 30s so a stuck
// handler cannot pin the Engine. Long-running meeting notes (hour-scale
// append/stop/summarize) and appUpdate.install are the exceptions.
const (
	DefaultMaxDeadlineMS   = 30_000
	MeetingLiveDeadlineMS  = 120_000
	MeetingNotesDeadlineMS = 600_000
	AppUpdateInstallMS     = 120_000
	PeopleFileDeadlineMS   = 120_000
	TemplateFileDeadlineMS = 120_000
)

// MaxDeadlineMS is the largest deadlineMs the Host/Engine accept for method.
func MaxDeadlineMS(method string) int {
	switch Method(method) {
	case "meetings.summarize", "meetings.catchup":
		return MeetingNotesDeadlineMS
	case "meetings.append", "meetings.audio.append", "meetings.stop", "meetings.heartbeat", "meetings.get", "meetings.export":
		return MeetingLiveDeadlineMS
	case MethodAppUpdateInstall:
		return AppUpdateInstallMS
	case MethodPeopleFileStage, MethodPeopleFilePick, MethodPeopleThreadSend:
		return PeopleFileDeadlineMS
	case MethodTemplateCreate, MethodTemplateFileStage:
		return TemplateFileDeadlineMS
	default:
		return DefaultMaxDeadlineMS
	}
}
