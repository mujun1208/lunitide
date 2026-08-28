package bridge

// Envelope and method deadline ceilings. Most RPCs stay at 30s so a stuck
// handler cannot pin the Engine. Long-running meeting notes (hour-scale
// append/stop/summarize) and appUpdate.install are the exceptions.
const (
	DefaultMaxDeadlineMS   = 30_000
	MeetingLiveDeadlineMS  = 120_000
	MeetingNotesDeadlineMS = 600_000
	AppUpdateInstallMS     = 120_000
)

// MaxDeadlineMS is the largest deadlineMs the Host/Engine accept for method.
func MaxDeadlineMS(method string) int {
	switch Method(method) {
	case "meetings.summarize":
		return MeetingNotesDeadlineMS
	case "meetings.append", "meetings.stop", "meetings.heartbeat", "meetings.get", "meetings.export":
		return MeetingLiveDeadlineMS
	case MethodAppUpdateInstall:
		return AppUpdateInstallMS
	default:
		return DefaultMaxDeadlineMS
	}
}
