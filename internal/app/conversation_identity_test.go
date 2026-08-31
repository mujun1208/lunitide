package app

import "testing"

func TestTurnIntentForChatAndPeople(t *testing.T) {
	session := turnIntentForChat(false, "继续刚才的 [引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAC]", "01ARZ3NDEKTSV4RRFFQ69G5FAA", "full-access", []string{"01ARZ3NDEKTSV4RRFFQ69G5FAB"})
	if session.Surface != SurfaceSession || session.Companion || session.ProjectID == "" || session.ExecutionMode != "full-access" {
		t.Fatalf("session intent = %#v", session)
	}
	if !hasMention(session.Mentions, "expert", "PPT专家") {
		t.Fatalf("session mentions = %#v", session.Mentions)
	}
	companion := turnIntentForChat(true, "继续刚才的", "", "full-access", nil)
	if companion.Surface != SurfaceCompanion || !companion.Companion {
		t.Fatalf("companion intent = %#v", companion)
	}
	people := turnIntentForPeople("继续刚才的 @PPT专家 ")
	if people.Surface != SurfacePeople || !hasMention(people.Mentions, "member", "PPT专家") {
		t.Fatalf("people intent = %#v", people)
	}
}

func TestConversationIdentitySessionKey(t *testing.T) {
	ident := ConversationIdentity{BoundSessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAA"}
	if ident.sessionKey("fallback") != "01ARZ3NDEKTSV4RRFFQ69G5FAA" {
		t.Fatalf("sessionKey = %q", ident.sessionKey("fallback"))
	}
	empty := ConversationIdentity{}
	if empty.sessionKey("01ARZ3NDEKTSV4RRFFQ69G5FAB") != "01ARZ3NDEKTSV4RRFFQ69G5FAB" {
		t.Fatalf("fallback sessionKey = %q", empty.sessionKey("01ARZ3NDEKTSV4RRFFQ69G5FAB"))
	}
}
