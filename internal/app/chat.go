package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/oklog/ulid/v2"
)

type EventEmitter func(bridge.Event) error

func (e *Engine) HandleStreaming(ctx context.Context, request bridge.Request, emit func(bridge.Event) error) bridge.Response {
	ctx = context.WithValue(ctx, streamParentKey{}, ctx)
	return e.Handle(context.WithValue(ctx, eventEmitterKey{}, EventEmitter(emit)), request)
}

type eventEmitterKey struct{}
type streamParentKey struct{}

func handleChatStart(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ProviderID string            `json:"providerId"`
		ModelID    string            `json:"modelId"`
		Messages   []gateway.Message `json:"messages"`
	}
	if decodePayload(request.Payload, &p) != nil || len(p.Messages) < 1 || len(p.Messages) > 32 || !ulidValid(p.ProviderID) || len(p.ModelID) < 1 || len(p.ModelID) > 128 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
	}
	totalBytes := len(p.ModelID)
	for _, m := range p.Messages {
		totalBytes += len(m.Content)
		if (m.Role != gateway.RoleSystem && m.Role != gateway.RoleUser && m.Role != gateway.RoleAssistant) || strings.TrimSpace(m.Content) == "" || len(m.Content) > 16*1024 || totalBytes > 48*1024 {
			return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
		}
	}
	item, err := e.providers.Get(ctx, p.ProviderID)
	if err != nil {
		return providerFailure(request, err)
	}
	if failure := providerReadyFailure(request, item); failure != nil {
		return *failure
	}
	if !storedModel(item, p.ModelID) {
		return bridge.Failure(request.ID, request.TraceID, "MODEL_NOT_FOUND", "模型不属于该供应商", false)
	}
	emit, ok := ctx.Value(eventEmitterKey{}).(EventEmitter)
	if !ok {
		return bridge.Failure(request.ID, request.TraceID, "STREAM_UNAVAILABLE", "流事件通道不可用", true)
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
	go e.runStream(streamCtx, streamID, state, item, gateway.Request{Model: p.ModelID, Messages: p.Messages, MaxAttempts: 1}, emit)
	return bridge.Success(request.ID, map[string]any{"streamId": streamID})
}

func handleStreamCancel(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		StreamID string `json:"streamId"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.StreamID) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "stream.cancel 参数无效", false)
	}
	ok := e.cancelStream(p.StreamID)
	return bridge.Success(request.ID, map[string]any{"cancelled": ok})
}

func (e *Engine) runStream(ctx context.Context, id string, state *streamState, p provider.Provider, req gateway.Request, emit EventEmitter) {
	var seq uint64
	send := func(event bridge.Event) error {
		seq++
		event.Version = bridge.Version
		event.Kind = "event"
		event.ID = ulid.Make().String()
		event.StreamID = id
		event.Sequence = seq
		return emit(event)
	}
	err := e.withProviderLease(ctx, p, secretlease.OperationChat, func(op context.Context, credential []byte) error {
		a, adapterErr := e.adapter(op, p)
		if adapterErr != nil {
			return adapterErr
		}
		result, streamErr := a.Stream(op, credential, req, func(d gateway.Delta) error {
			if d.Text != "" {
				if err := send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: d.Text}}); err != nil {
					return err
				}
			}
			return nil
		})
		if streamErr == nil && result.Usage.TotalTokens > 0 {
			if sendErr := send(bridge.Event{Type: bridge.EventUsage, Usage: &bridge.UsageEvent{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.TotalTokens}}); sendErr != nil {
				return sendErr
			}
		}
		return streamErr
	})
	terminal := bridge.Event{Type: e.selectTerminal(id, state, err)}
	if terminal.Type == bridge.EventFailed {
		terminal.Error = &bridge.StreamError{Code: "UPSTREAM_FAILED", Message: "模型请求失败", Retryable: true}
	}
	if send(terminal) != nil {
		state.cancel()
	}
	e.finishTerminal(id, state)
}

func ulidValid(s string) bool { _, err := ulid.ParseStrict(s); return err == nil }
