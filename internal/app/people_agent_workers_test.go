package app

import (
	"context"
	"testing"
)

func TestPeopleAgentShutdownCancelsAndJoinsRepliesBeforeStorageClose(t *testing.T) {
	e := &Engine{}
	started, done := make(chan struct{}), make(chan struct{})
	if !e.peopleAgentWorkers.start(func(ctx context.Context) { close(started); <-ctx.Done(); close(done) }) {
		t.Fatal("reply not started")
	}
	<-started
	e.StopPeopleAgentReplies()
	select {
	case <-done:
	default:
		t.Fatal("shutdown returned before reply stopped")
	}
	if e.peopleAgentWorkers.start(func(context.Context) { t.Error("reply started after shutdown") }) {
		t.Fatal("late reply accepted")
	}
	e.StopPeopleAgentReplies()
}
