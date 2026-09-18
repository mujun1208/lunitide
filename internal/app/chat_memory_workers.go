package app

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

const chatMemoryQueueCapacity = 32
const chatMemoryTaskTimeout = 8 * time.Second

var chatMemoryLeaseOwner = ulid.Make().String()

type captureJobCursor struct {
	SessionID     string `json:"sessionId,omitempty"`
	UserText      string `json:"userText,omitempty"`
	AssistantText string `json:"assistantText,omitempty"`
	Companion     bool   `json:"companion,omitempty"`
}

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
	if e.memoryOps != nil && e.memoryOps.HasCaptureQueue() {
		e.persistCaptureJob(sessionID, userText, assistantText, messageID, companion)
		if !e.chatMemoryWorkers.enqueue(func(ctx context.Context) {
			e.drainCaptureJobs(ctx)
		}) {
			log.Print("chat memory: closeout queue unavailable; conversation remains saved")
		}
		return
	}
	if !e.chatMemoryWorkers.enqueue(func(ctx context.Context) {
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

func (e *Engine) persistCaptureJob(sessionID, userText, assistantText, messageID string, companion bool) {
	if e == nil || e.memoryOps == nil || messageID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !m8core.ResolveMemoryBehavior(e.chatMemoryV2(ctx), "user", m8core.CurrentProductFlags()).AllowAutoCapture {
		return
	}
	raw, _ := json.Marshal(captureJobCursor{SessionID: sessionID, UserText: userText, AssistantText: assistantText, Companion: companion})
	if _, err := e.memoryOps.EnqueueCaptureJob(ctx, m8core.MemoryCaptureJob{
		SubjectID:       e.memorySubjectID(),
		SourceMessageID: messageID,
		SourceRevision:  "1",
		SourceDigest:    m8core.DigestOf(userText),
		CursorJSON:      string(raw),
	}, m8core.MemoryCaptureHighWatermark); err != nil {
		log.Printf("chat memory: capture enqueue skipped: %v", err)
	}
}

func (e *Engine) drainCaptureJobs(ctx context.Context) {
	if e == nil || e.memoryOps == nil || !e.memoryOps.HasCaptureQueue() {
		return
	}
	if _, err := e.memoryOps.ReclaimExpiredCaptureJobs(ctx, time.Now()); err != nil {
		log.Printf("chat memory: reclaim skipped: %v", err)
	}
	e.backfillCaptureJobs(ctx)
	for ctx.Err() == nil {
		job, ok, err := e.memoryOps.ClaimCaptureJob(ctx, chatMemoryLeaseOwner, m8core.MemoryCaptureLease)
		if err != nil {
			log.Printf("chat memory: claim skipped: %v", err)
			return
		}
		if !ok {
			return
		}
		runErr := e.runCaptureJob(ctx, job)
		code := ""
		if runErr != nil {
			code = "CAPTURE_FAILED"
			log.Printf("chat memory: capture job failed: %v", runErr)
		}
		if err := e.memoryOps.CompleteCaptureJob(ctx, job.JobID, job.Fence, code); err != nil {
			log.Printf("chat memory: capture complete skipped: %v", err)
		}
	}
}

func (e *Engine) backfillCaptureJobs(ctx context.Context) {
	active, err := e.memoryOps.CountActiveCaptureJobs(ctx)
	if err != nil || active >= m8core.MemoryCaptureLowWatermark {
		return
	}
	cursor, err := e.memoryOps.CaptureCursor(ctx, e.memorySubjectID())
	if err != nil {
		return
	}
	sources, err := e.memoryOps.ListCaptureSourcesAfter(ctx, cursor.SourceMessageID, m8core.MemoryCaptureScanLimit)
	if err != nil {
		return
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return
		}
		raw, _ := json.Marshal(captureJobCursor{SessionID: src.SessionID, UserText: src.Text})
		if _, err := e.memoryOps.EnqueueCaptureJob(ctx, m8core.MemoryCaptureJob{
			SubjectID:       e.memorySubjectID(),
			SourceMessageID: src.MessageID,
			SourceRevision:  src.Revision,
			SourceDigest:    src.Digest,
			CursorJSON:      string(raw),
		}, m8core.MemoryCaptureHighWatermark); err != nil {
			log.Printf("chat memory: backfill skipped: %v", err)
			return
		}
	}
}

func (e *Engine) runCaptureJob(ctx context.Context, job m8core.MemoryCaptureJob) error {
	var meta captureJobCursor
	_ = json.Unmarshal([]byte(job.CursorJSON), &meta)
	userText := meta.UserText
	sessionID := meta.SessionID
	if userText == "" {
		return nil
	}
	if sessionID == "" {
		sessionID = job.SourceMessageID
	}
	if err := e.maybeAutoNominateTurn(ctx, sessionID, userText, meta.AssistantText, job.SourceMessageID, meta.Companion); err != nil {
		return err
	}
	e.writeSessionLastMemory(ctx, sessionID, userText, meta.AssistantText)
	e.maybeWriteExpertTurnMemories(ctx, sessionID, userText, meta.AssistantText)
	return nil
}

func runChatMemoryTask(ctx context.Context, run func(context.Context)) {
	defer func() {
		if recover() != nil {
			log.Print("chat memory: background closeout failed; conversation remains saved")
		}
	}()
	run(ctx)
}
