package app

import (
	"context"
	"testing"
	"time"
)

func TestChatMemoryQueueBoundedAndShutdownCancels(t *testing.T) {
	e := NewEngine(nil, "test")
	started := make(chan struct{})
	cancelled := make(chan struct{})
	if !e.chatMemoryWorkers.enqueue(func(ctx context.Context) { close(started); <-ctx.Done(); close(cancelled) }) {
		t.Fatal("enqueue")
	}
	<-started
	for i := 0; i < chatMemoryQueueCapacity; i++ {
		if !e.chatMemoryWorkers.enqueue(func(context.Context) { t.Error("queued work ran after shutdown") }) {
			t.Fatal("unexpected capacity")
		}
	}
	if e.chatMemoryWorkers.enqueue(func(context.Context) {}) {
		t.Fatal("unbounded queue")
	}
	stopped := make(chan struct{})
	go func() { e.StopChatMemoryWorkers(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown failed to cancel/join")
	}
	<-cancelled
	if e.chatMemoryWorkers.enqueue(func(context.Context) {}) {
		t.Fatal("enqueue after shutdown")
	}
}
func TestChatMemoryQueueKeepsTurnOrderAndUsesOwnDeadline(t *testing.T) {
	e := NewEngine(nil, "test")
	defer e.StopChatMemoryWorkers()
	results := make(chan int, 3)
	for i := 0; i < 3; i++ {
		i := i
		e.chatMemoryWorkers.enqueue(func(ctx context.Context) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > chatMemoryTaskTimeout {
				t.Error("missing task deadline")
			}
			results <- i
		})
	}
	for i := 0; i < 3; i++ {
		select {
		case got := <-results:
			if got != i {
				t.Fatalf("order=%d want%d", got, i)
			}
		case <-time.After(time.Second):
			t.Fatal("worker stalled")
		}
	}
}
