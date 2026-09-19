package agenthub

import "testing"

func TestLunitideSessionIDReadsLatestBind(t *testing.T) {
	if LunitideSessionID(nil) != "" {
		t.Fatal("empty events")
	}
	got := LunitideSessionID([]ThreadEvent{
		{Type: "error", Detail: "no"},
		{Type: EventLunitideSession, Detail: "01ARZ3NDEKTSV4RRFFQ69G5FAV"},
	})
	if got != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("got %q", got)
	}
}

func TestAttachLunitideSessionCopiesEvent(t *testing.T) {
	detail := AttachLunitideSession(ThreadDetail{Events: []ThreadEvent{{Type: EventLunitideSession, Detail: "01ARZ3NDEKTSV4RRFFQ69G5FAW"}}})
	if detail.SessionID != "01ARZ3NDEKTSV4RRFFQ69G5FAW" {
		t.Fatalf("session=%q", detail.SessionID)
	}
}
