package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/oklog/ulid/v2"
)

// persistRetrySentinel is a chat.start user turn that only retries a failed
// assistant write. The renderer never appends it to history.
const persistRetrySentinel = "\u2063persist-retry"

func isPersistRetryTurn(messages []gateway.Message) bool {
	return lastUserChatText(messages) == persistRetrySentinel
}

func persistFailureOnly(upstreamErr, persistErr error, generatedText string) bool {
	return persistErr != nil && upstreamErr == nil && strings.TrimSpace(generatedText) != ""
}

func selectPersistTerminal(cancelling bool, upstreamErr, persistErr error, generatedText string) bridge.EventType {
	if cancelling {
		return bridge.EventCancelled
	}
	if persistFailureOnly(upstreamErr, persistErr, generatedText) {
		return bridge.EventCompleted
	}
	if upstreamErr != nil || persistErr != nil {
		return bridge.EventFailed
	}
	return bridge.EventCompleted
}

func (e *Engine) retrySessionPersistDraft(ctx context.Context, sessionID string) (string, error) {
	if e == nil || e.messages == nil || strings.TrimSpace(sessionID) == "" {
		return "", nil
	}
	cp := e.loadTurnCheckpoint(sessionID)
	draft := strings.TrimSpace(cp.PersistDraft)
	if draft == "" {
		return "", nil
	}
	streamID := strings.TrimSpace(cp.StreamID)
	if streamID == "" || !ulidValid(streamID) {
		streamID = ulid.Make().String()
	}
	msg, err := e.messages.AppendAssistant(ctx, streamID, "engine", sessionID, draft, messageapp.AssistantUsage{})
	if err != nil {
		return "", err
	}
	cp.PersistDraft = ""
	cp.PersistFailed = false
	e.saveTurnCheckpoint(sessionID, cp)
	e.pushInboundReply(sessionID, draft)
	return msg.ID, nil
}

func (e *Engine) handlePersistRetryStart(ctx context.Context, request bridge.Request, sessionID string, emit EventEmitter) bridge.Response {
	if !ulidValid(sessionID) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 需要提供 sessionId", false)
	}
	e.streamsMu.Lock()
	if len(e.streams) >= e.maxStreams {
		e.streamsMu.Unlock()
		return bridge.Failure(request.ID, request.TraceID, "STREAM_LIMIT_REACHED", "并发流数量已达上限", true)
	}
	streamID := ulid.Make().String()
	parent, _ := ctx.Value(streamParentKey{}).(context.Context)
	if parent == nil {
		parent = ctx
	}
	streamCtx, cancel := context.WithCancel(parent)
	state := &streamState{cancel: cancel}
	e.streams[streamID] = state
	e.streamsMu.Unlock()
	go func() {
		defer e.finishTerminal(streamID, state)
		defer cancel()
		msgID, err := e.retrySessionPersistDraft(streamCtx, sessionID)
		completed := &bridge.CompletedEvent{MessageID: msgID, PersistFailed: err != nil}
		if err == nil && msgID == "" {
			cp := e.loadTurnCheckpoint(sessionID)
			completed.PersistFailed = cp.PersistFailed && strings.TrimSpace(cp.PersistDraft) != ""
		}
		seq := uint64(1)
		_ = emit(bridge.Event{
			Version:   bridge.Version,
			Kind:      "event",
			ID:        ulid.Make().String(),
			StreamID:  streamID,
			Sequence:  seq,
			Type:      bridge.EventCompleted,
			Completed: completed,
		})
	}()
	return bridge.Success(request.ID, map[string]any{"streamId": streamID})
}
