package app

import (
	"context"
	"log"
	"sync"
	"time"
)

const chatMemoryQueueCapacity = 32
const chatMemoryTaskTimeout = 8 * time.Second

// Memory closeout is optional after the durable terminal receipt. One worker
// preserves turn order, bounds resource use, and is cancelled before DB close.
type chatMemoryWorkers struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	queue  chan func(context.Context)
	wg     sync.WaitGroup
	closed bool
}

func (w *chatMemoryWorkers) enqueue(task func(context.Context)) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
		w.queue = make(chan func(context.Context), chatMemoryQueueCapacity)
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-w.ctx.Done():
					return
				case run := <-w.queue:
					if w.ctx.Err() != nil {
						return
					}
					ctx, cancel := context.WithTimeout(w.ctx, chatMemoryTaskTimeout)
					runChatMemoryTask(ctx, run)
					cancel()
				}
			}
		}()
	}
	select {
	case w.queue <- task:
		return true
	default:
		return false
	}
}

func (e *Engine) StopChatMemoryWorkers() {
	w := &e.chatMemoryWorkers
	w.mu.Lock()
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}

func (e *Engine) enqueueChatMemory(sessionID, userText, assistantText, messageID string, companion bool) {
	if !e.chatMemoryWorkers.enqueue(func(ctx context.Context) {
		// Stable user facts take priority over derived working summaries.
		if err := e.maybeAutoNominateTurn(ctx, sessionID, userText, assistantText, messageID, companion); err != nil {
			log.Printf("chat memory: closeout skipped: %v", err)
		}
		if ctx.Err() != nil {
			return
		}
		e.writeSessionLastMemory(ctx, sessionID, userText, assistantText)
		if ctx.Err() != nil {
			return
		}
		e.maybeWriteExpertTurnMemories(ctx, sessionID, userText, assistantText)
	}) {
		log.Print("chat memory: closeout queue unavailable; conversation remains saved")
	}
}

func runChatMemoryTask(ctx context.Context, run func(context.Context)) {
	defer func() {
		if recover() != nil {
			log.Print("chat memory: background closeout failed; conversation remains saved")
		}
	}()
	run(ctx)
}
