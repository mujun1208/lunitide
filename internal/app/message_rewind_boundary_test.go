package app

import (
	"context"
	"testing"
)

func TestMessageRewindRejectsSessionWithLiveTurn(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	if !e.reserveChatSession(sessionID) {
		t.Fatal("cannot reserve session")
	}
	defer e.releaseChatSession(sessionID)
	req := validRequest("message.rewind", `{"sessionId":"`+sessionID+`","messageId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`)
	req.IdempotencyKey = "live-turn-rewind"
	res := e.Handle(context.Background(), req)
	if res.OK || res.Error.Code != "STREAM_LIMIT_REACHED" {
		t.Fatalf("rewind bypassed live turn: %#v", res)
	}
}
