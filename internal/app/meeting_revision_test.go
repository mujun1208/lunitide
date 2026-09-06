package app

import (
	"context"
	"testing"
)

func TestMeetingClientRevisionRejectsStaleEditDeleteAndLegacyWrites(t *testing.T) {
	e, svc := newMeetingsEngine(t)
	ctx := context.Background()
	m, err := svc.Start(ctx, "revision", "")
	if err != nil {
		t.Fatal(err)
	}
	beat, err := svc.Heartbeat(ctx, m.MeetingID)
	if err != nil || beat.Revision != m.Revision {
		t.Fatalf("heartbeat invalidates editor: %v %#v", err, beat)
	}
	m, err = svc.Stop(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	first := meetingsCall(t, e, "meetings.update", map[string]any{"meetingId": m.MeetingID, "expectedRevision": m.Revision, "summary": "first editor"})
	if !first.OK {
		t.Fatal(first.Error)
	}
	for _, method := range []string{"meetings.update", "meetings.delete", "meetings.summarize", "meetings.catchup"} {
		p := map[string]any{"meetingId": m.MeetingID, "expectedRevision": m.Revision}
		if method == "meetings.update" {
			p["summary"] = "stale"
		}
		r := meetingsCall(t, e, method, p)
		if r.OK || r.Error.Code != "MEETING_CHANGED" {
			t.Fatalf("%s accepted stale revision: %#v", method, r)
		}
		delete(p, "expectedRevision")
		r = meetingsCall(t, e, method, p)
		if r.OK || r.Error.Code != "MEETING_REVISION_REQUIRED" {
			t.Fatalf("%s legacy overwrite: %#v", method, r)
		}
	}
	latest, err := svc.Get(ctx, m.MeetingID)
	if err != nil || latest.Summary != "first editor" || latest.Revision != m.Revision+1 {
		t.Fatalf("latest overwritten: %#v %v", latest, err)
	}
}
