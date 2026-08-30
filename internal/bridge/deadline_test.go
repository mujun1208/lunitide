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
