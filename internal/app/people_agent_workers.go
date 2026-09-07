package app

import (
	"context"
	"sync"
)

// Replies outlive the send receipt, but must finish before SQLite/tools close.
type peopleAgentWorkers struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

func (w *peopleAgentWorkers) start(run func(context.Context)) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
	w.wg.Add(1)
	go func() { defer w.wg.Done(); run(w.ctx) }()
	return true
}

func (e *Engine) StopPeopleAgentReplies() {
	w := &e.peopleAgentWorkers
	w.mu.Lock()
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}
