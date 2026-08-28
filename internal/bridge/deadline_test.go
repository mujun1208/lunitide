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
	if MeetingLiveDeadlineMS <= 60_000 {
		t.Fatalf("live meeting RPCs must outlast a 60s mock: %d", MeetingLiveDeadlineMS)
	}
}
