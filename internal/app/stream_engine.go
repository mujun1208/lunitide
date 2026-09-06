package app

import (
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/bridge"
)

// streamEngine is the StreamEngine subsystem (F-06 A-01.1). It owns the stream
// lifecycle state machine: registration capacity, cancellation, and the
// finalize/terminal race linearization. It holds the only copy of the stream
// map + its mutex; other subsystems reach it through the embedding Engine, whose
// promoted methods forward here. No field or method here touches any other
// Engine state, so the subsystem is self-contained and lock ownership is single.
type streamEngine struct {
	streamsMu  sync.Mutex
	streams    map[string]*streamState
	maxStreams int
}

// CancelAllStreams terminates every stream owned by this authenticated session.
func (e *streamEngine) CancelAllStreams() {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	for _, stream := range e.streams {
		if stream.state == streamRunning {
			stream.state = streamCancelling
			stream.cancel()
		}
	}
}

func (e *streamEngine) cancelTtsStreams() {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	for _, stream := range e.streams {
		if stream.tts && stream.state == streamRunning {
			stream.state = streamCancelling
			stream.cancel()
		}
	}
}

func (e *streamEngine) cancelStream(id string) bool {
	return e.cancelStreamSpoken(id, "")
}

func (e *streamEngine) cancelStreamSpoken(id, spoken string) bool {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	stream, ok := e.streams[id]
	if !ok || stream.state != streamRunning {
		return false
	}
	if spoken = strings.TrimSpace(spoken); spoken != "" {
		stream.spokenPersist = spoken
	}
	stream.state = streamCancelling
	stream.cancel()
	return true
}

// claimStreamFinalization linearizes successful upstream completion against
// cancellation. A false result means cancellation already won, so the caller
// must skip durable persistence.
func (e *streamEngine) claimStreamFinalization(state *streamState) bool {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	if state.state != streamRunning {
		return false
	}
	state.state = streamFinalizing
	return true
}

func (e *streamEngine) selectTerminal(_ string, state *streamState, err error) bridge.EventType {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	t := bridge.EventCompleted
	if state.state == streamCancelling {
		t = bridge.EventCancelled
	} else if err != nil {
		t = bridge.EventFailed
	}
	state.state = streamTerminal
	return t
}

func (e *streamEngine) finishTerminal(id string, state *streamState) {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	if current, ok := e.streams[id]; ok && current == state {
		delete(e.streams, id)
	}
}