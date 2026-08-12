package hostbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

const MaxMessageBytes = 256 * 1024

type Message struct {
	SourceURL string
	TopFrame  bool
	JSON      []byte
}

type Caller interface {
	Call(context.Context, bridge.Request) (bridge.Response, error)
}
type EventSource interface{ Events() <-chan bridge.Event }

type RoutedEvent struct {
	Generation uint64
	Event      bridge.Event
	owner      *streamOwner
	ack        *eventAck
}

type eventAck struct {
	once sync.Once
	done chan bool
}

func (e RoutedEvent) Acknowledge(delivered bool) {
	if e.ack != nil {
		e.ack.once.Do(func() { e.ack.done <- delivered })
	}
}

type streamOwner struct {
	generation uint64
	sequence   uint64
	terminal   bool
	// admitted remains true until this stream's terminal has been handed to
	// the consumer. Thus buffered terminals continue to consume stream capacity.
	admitted       bool
	active         chan struct{}
	activateOnce   sync.Once
	terminalQueued bool
}

const eventQueueCapacity = 256

// Handler owns a Bridge method in the Host process. Requests dispatched to a
// Handler are never sent to the Engine Caller.
type Handler interface {
	HandleHost(context.Context, bridge.Request) bridge.Response
}

type Gateway struct {
	trustedOrigin     string
	caller            Caller
	hostHandlers      map[bridge.Method]Handler
	admission         chan struct{}
	streamsMu         sync.Mutex
	streams           map[string]*streamOwner
	startsInFlight    int
	outstanding       int
	reservations      int
	eventSourceClosed bool
	streamChanged     *sync.Cond
	generation        uint64
	generationCtx     context.Context
	generationCancel  context.CancelFunc
	events            chan RoutedEvent
	eventQueue        []RoutedEvent
	eventChanged      *sync.Cond
	consumerStopped   bool
	consumerDone      chan struct{}
	dispatchDone      chan struct{}
	consumerStopOnce  sync.Once
}

func (g *Gateway) Events() <-chan RoutedEvent {
	return g.events
}

func New(trustedOrigin string, caller Caller, handlers ...map[bridge.Method]Handler) (*Gateway, error) {
	origin, err := canonicalOrigin(trustedOrigin)
	if err != nil {
		return nil, err
	}
	if caller == nil {
		return nil, errors.New("host bridge caller is required")
	}
	local := map[bridge.Method]Handler{}
	if len(handlers) > 0 && handlers[0] != nil {
		local = handlers[0]
	}
	genCtx, genCancel := context.WithCancel(context.Background())
	g := &Gateway{trustedOrigin: origin, caller: caller, hostHandlers: local, admission: make(chan struct{}, 32), streams: make(map[string]*streamOwner), generationCtx: genCtx, generationCancel: genCancel, events: make(chan RoutedEvent), consumerDone: make(chan struct{}), dispatchDone: make(chan struct{})}
	g.streamChanged = sync.NewCond(&g.streamsMu)
	g.eventChanged = sync.NewCond(&g.streamsMu)
	go g.dispatchEvents()
	if source, ok := caller.(EventSource); ok && source.Events() != nil {
		go g.forwardEvents(source.Events())
	}
	return g, nil
}

func (g *Gateway) Generation() uint64 {
	g.streamsMu.Lock()
	defer g.streamsMu.Unlock()
	return g.generation
}

// InvalidateGeneration cancels all in-flight requests before the new document
// can acquire stream ownership.
func (g *Gateway) InvalidateGeneration(ctx context.Context) uint64 {
	g.streamsMu.Lock()
	g.generation++
	g.generationCancel()
	g.generationCtx, g.generationCancel = context.WithCancel(context.Background())
	gen := g.generation
	ids := g.snapshotAndClearStreamsLocked()
	g.streamsMu.Unlock()
	go g.cancelIDs(ctx, ids)
	return gen
}

func (g *Gateway) Handle(ctx context.Context, message Message) (bridge.Response, bool) {
	return g.HandleGeneration(ctx, g.Generation(), message)
}

func (g *Gateway) HandleGeneration(ctx context.Context, generation uint64, message Message) (bridge.Response, bool) {
	g.streamsMu.Lock()
	if generation != g.generation {
		g.streamsMu.Unlock()
		return bridge.Response{}, false
	}
	genCtx := g.generationCtx
	g.streamsMu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(genCtx, cancel)
	defer func() { stop(); cancel() }()
	if !message.TopFrame || len(message.JSON) == 0 {
		return bridge.Response{}, false
	}
	origin, err := canonicalOrigin(message.SourceURL)
	if err != nil || origin != g.trustedOrigin {
		return bridge.Response{}, false
	}
	if len(message.JSON) > MaxMessageBytes {
		var request bridge.Request
		if json.Unmarshal(message.JSON, &request) != nil {
			return bridge.Response{}, false
		}
		return failureFor(request, "BRIDGE_REQUEST_TOO_LARGE", "请求内容超过 Bridge 限制；附件请使用分块上传", false), true
	}
	var request bridge.Request
	if err := decodeStrict(message.JSON, &request); err != nil {
		return failureFor(request, "BRIDGE_SCHEMA_INVALID", "请求格式无效", false), true
	}
	if err := validateEnvelope(request, time.Now().UTC()); err != nil {
		return failureFor(request, "BRIDGE_SCHEMA_INVALID", "请求协议无效", false), true
	}
	select {
	case g.admission <- struct{}{}:
		defer func() { <-g.admission }()
	default:
		return failureFor(request, "HOST_BUSY", "桌面主机正忙，请稍后重试", true), true
	}
	metadata, known := bridge.MethodMetadataByMethod[bridge.Method(request.Method)]
	if !known || !metadata.Enabled {
		return failureFor(request, "BRIDGE_METHOD_NOT_ALLOWED", "请求的方法不在白名单中", false), true
	}
	if handler := g.hostHandlers[bridge.Method(request.Method)]; handler != nil {
		return handler.HandleHost(ctx, request), true
	}
	if !isEngineForwardable(metadata) {
		return failureFor(request, "BRIDGE_METHOD_NOT_ALLOWED", "请求的方法不在白名单中", false), true
	}
	if bridge.Method(request.Method) == bridge.MethodStreamCancel {
		var payload struct {
			StreamID string `json:"streamId"`
		}
		_ = json.Unmarshal(request.Payload, &payload)
		g.streamsMu.Lock()
		_, owned := g.streams[payload.StreamID]
		g.streamsMu.Unlock()
		if !owned {
			return failureFor(request, "STREAM_NOT_OWNED", "流不属于当前会话", false), true
		}
	}
	if method := bridge.Method(request.Method); method == bridge.MethodTerminalInput || method == bridge.MethodTerminalResize || method == bridge.MethodTerminalClose {
		var payload struct {
			TerminalID string `json:"terminalId"`
		}
		_ = json.Unmarshal(request.Payload, &payload)
		g.streamsMu.Lock()
		owner, owned := g.streams[payload.TerminalID]
		owned = owned && owner.terminal && owner.generation == generation
		g.streamsMu.Unlock()
		if !owned {
			return failureFor(request, "TERMINAL_NOT_OWNED", "终端不属于当前页面", false), true
		}
	}
	isStart := bridge.Method(request.Method) == bridge.MethodChatStart || bridge.Method(request.Method) == bridge.MethodTerminalStart
	if isStart {
		g.streamsMu.Lock()
		if g.eventSourceClosed || g.consumerStopped {
			g.streamsMu.Unlock()
			if g.consumerStopped {
				return failureFor(request, "HOST_EVENT_CONSUMER_STOPPED", "桌面主机事件消费者已停止", true), true
			}
			return failureFor(request, "ENGINE_EVENT_SOURCE_CLOSED", "核心引擎事件连接已关闭", true), true
		}
		if len(g.eventQueue)+g.reservations >= eventQueueCapacity {
			g.streamsMu.Unlock()
			return failureFor(request, "HOST_EVENT_CAPACITY", "桌面主机事件容量已满", true), true
		}
		g.outstanding++
		g.reservations++
		g.startsInFlight++
		g.streamsMu.Unlock()
	}
	response, err := g.caller.Call(ctx, request)
	if err != nil {
		if isStart {
			g.streamsMu.Lock()
			g.startsInFlight--
			g.outstanding--
			g.reservations--
			g.streamChanged.Broadcast()
			g.streamsMu.Unlock()
		}
		return failureFor(request, "ENGINE_UNAVAILABLE", "核心引擎暂时不可用", true), true
	}
	if isStart {
		raw, _ := json.Marshal(response.Payload)
		var result struct {
			StreamID   string `json:"streamId"`
			TerminalID string `json:"terminalId"`
		}
		_ = json.Unmarshal(raw, &result)
		if result.StreamID == "" {
			result.StreamID = result.TerminalID
		}
		g.streamsMu.Lock()
		g.startsInFlight--
		_, duplicate := g.streams[result.StreamID]
		current := response.OK && result.StreamID != "" && !duplicate && !g.eventSourceClosed && generation == g.generation && genCtx.Err() == nil
		if current {
			g.streams[result.StreamID] = &streamOwner{generation: generation, admitted: true, active: make(chan struct{}), terminal: bridge.Method(request.Method) == bridge.MethodTerminalStart}
		} else {
			g.outstanding--
			g.reservations--
		}
		closed := g.eventSourceClosed
		g.streamChanged.Broadcast()
		g.streamsMu.Unlock()
		if response.OK && result.StreamID != "" && !current {
			go g.cancelIDs(context.Background(), []string{result.StreamID})
			if closed {
				return failureFor(request, "ENGINE_EVENT_SOURCE_CLOSED", "核心引擎事件连接已关闭", true), true
			}
			return bridge.Response{}, false
		}
	}
	g.streamsMu.Lock()
	current := generation == g.generation && genCtx.Err() == nil
	g.streamsMu.Unlock()
	if !current {
		return bridge.Response{}, false
	}
	return response, true
}

// ActivateStream opens the response/event barrier only after chat.start has
// actually been posted on the UI thread.
func (g *Gateway) ActivateStream(generation uint64, streamID string) bool {
	g.streamsMu.Lock()
	defer g.streamsMu.Unlock()
	owner, ok := g.streams[streamID]
	if !ok || owner.generation != generation {
		return false
	}
	owner.activateOnce.Do(func() { close(owner.active) })
	g.eventChanged.Broadcast()
	return true
}

func terminalEvent(t bridge.EventType) bool {
	return t == bridge.EventCompleted || t == bridge.EventCancelled || t == bridge.EventFailed || t == bridge.EventTerminalExit
}

func (g *Gateway) forwardEvents(source <-chan bridge.Event) {
	for event := range source {
		g.streamsMu.Lock()
		if g.consumerStopped {
			g.streamsMu.Unlock()
			return
		}
		owner, owned := g.streams[event.StreamID]
		for !owned && g.startsInFlight > 0 && !g.eventSourceClosed {
			g.streamChanged.Wait()
			owner, owned = g.streams[event.StreamID]
		}
		if owned && owner.terminalQueued {
			owned = false
		} else if owned {
			owner.sequence = event.Sequence
		}
		if !owned {
			g.streamsMu.Unlock()
			continue
		}
		routed := RoutedEvent{Generation: owner.generation, Event: event, owner: owner}
		if terminalEvent(event.Type) {
			owner.terminalQueued = true
			if owner.admitted {
				g.reservations--
			}
			g.eventQueue = append(g.eventQueue, routed)
			g.eventChanged.Broadcast()
			g.streamsMu.Unlock()
			continue
		}
		if len(g.eventQueue)+g.reservations < eventQueueCapacity {
			g.eventQueue = append(g.eventQueue, routed)
			g.eventChanged.Broadcast()
			g.streamsMu.Unlock()
			continue
		}
		owner.terminalQueued = true
		if owner.admitted {
			g.reservations--
		}
		g.eventQueue = append(g.eventQueue, syntheticFailure(owner, event.StreamID, event.Sequence+1, "HOST_EVENT_OVERFLOW", "事件队列已满，流已终止"))
		g.eventChanged.Broadcast()
		g.streamsMu.Unlock()
		go g.cancelIDs(context.Background(), []string{event.StreamID})
	}
	g.streamsMu.Lock()
	if g.consumerStopped {
		g.streamsMu.Unlock()
		return
	}
	g.eventSourceClosed = true
	owners := g.streams
	g.streamChanged.Broadcast()
	for id, owner := range owners {
		if owner.terminalQueued {
			continue
		}
		owner.terminalQueued = true
		if owner.admitted {
			g.reservations--
		}
		g.eventQueue = append(g.eventQueue, syntheticFailure(owner, id, owner.sequence+1, "ENGINE_EVENT_SOURCE_CLOSED", "核心引擎事件连接已关闭"))
	}
	g.eventChanged.Broadcast()
	g.streamsMu.Unlock()
}

func syntheticFailure(owner *streamOwner, streamID string, sequence uint64, code, message string) RoutedEvent {
	if sequence == 0 {
		sequence = 1
	}
	return RoutedEvent{Generation: owner.generation, owner: owner, Event: bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: streamID, Sequence: sequence, Type: bridge.EventFailed, Error: &bridge.StreamError{Code: code, Message: message, Retryable: true}}}
}

func (g *Gateway) dispatchEvents() {
	defer close(g.dispatchDone)
	for {
		g.streamsMu.Lock()
		for len(g.eventQueue) == 0 && !g.consumerStopped {
			g.eventChanged.Wait()
		}
		if g.consumerStopped {
			g.streamsMu.Unlock()
			return
		}
		event := g.eventQueue[0]
		active := event.owner.active
		g.streamsMu.Unlock()
		select {
		case <-active:
		case <-g.consumerDone:
			return
		}
		event.ack = &eventAck{done: make(chan bool, 1)}
		select {
		case g.events <- event:
		case <-g.consumerDone:
			return
		}
		select {
		case <-event.ack.done:
		case <-g.consumerDone:
			return
		}
		g.streamsMu.Lock()
		if g.consumerStopped {
			g.streamsMu.Unlock()
			return
		}
		g.eventQueue = g.eventQueue[1:]
		if terminalEvent(event.Event.Type) {
			if current, ok := g.streams[event.Event.StreamID]; ok && current == event.owner {
				delete(g.streams, event.Event.StreamID)
			}
			if event.owner.admitted {
				event.owner.admitted = false
				g.outstanding--
			}
		}
		g.streamChanged.Broadcast()
		g.streamsMu.Unlock()
	}
}

// StopEventConsumer permanently releases dispatchEvents from activation,
// delivery, and acknowledgement waits when the host consumer exits.
func (g *Gateway) StopEventConsumer() {
	g.consumerStopOnce.Do(func() {
		g.streamsMu.Lock()
		g.consumerStopped = true
		g.generationCancel()
		g.snapshotAndClearStreamsLocked()
		g.eventQueue = nil
		close(g.consumerDone)
		g.eventChanged.Broadcast()
		g.streamChanged.Broadcast()
		g.streamsMu.Unlock()
	})
}

// CancelStreams invalidates renderer ownership first, then requests best-effort
// cancellation over the already-authenticated Engine pipe.
func (g *Gateway) CancelStreams(ctx context.Context) {
	g.streamsMu.Lock()
	ids := g.snapshotAndClearStreamsLocked()
	g.streamsMu.Unlock()
	g.cancelIDs(ctx, ids)
}

func (g *Gateway) snapshotAndClearStreamsLocked() []string {
	ids := make([]string, 0, len(g.streams))
	for id, owner := range g.streams {
		if owner.terminal {
			ids = append(ids, "terminal:"+id)
		} else {
			ids = append(ids, id)
		}
		if owner.admitted {
			g.outstanding--
			owner.admitted = false
			if !owner.terminalQueued {
				g.reservations--
			}
		}
		owner.activateOnce.Do(func() { close(owner.active) })
	}
	g.streams = make(map[string]*streamOwner)
	return ids
}

func (g *Gateway) cancelIDs(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	all, cancelAll := context.WithTimeout(ctx, time.Second)
	defer cancelAll()
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-all.Done():
				return
			}
			method, field := bridge.MethodStreamCancel, "streamId"
			if strings.HasPrefix(id, "terminal:") {
				method, field, id = bridge.MethodTerminalClose, "terminalId", strings.TrimPrefix(id, "terminal:")
			}
			request := bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: string(method), SentAt: time.Now().UTC(), Payload: json.RawMessage(`{"` + field + `":"` + id + `"}`), DeadlineMS: 1000}
			_, _ = g.caller.Call(all, request)
		}()
	}
	wg.Wait()
}

func isEngineForwardable(metadata bridge.MethodMetadata) bool {
	return metadata.Enabled && metadata.Owner == "engine"
}

func validateEnvelope(request bridge.Request, now time.Time) error {
	if request.Version != bridge.Version || request.Kind != "request" {
		return errors.New("invalid Bridge version or kind")
	}
	if _, err := ulid.ParseStrict(request.ID); err != nil {
		return errors.New("invalid request ID")
	}
	if _, err := ulid.ParseStrict(request.TraceID); err != nil {
		return errors.New("invalid trace ID")
	}
	if request.DeadlineMS < 1 || request.DeadlineMS > 30000 || len(request.IdempotencyKey) > 128 {
		return errors.New("invalid deadline or idempotency key")
	}
	if request.SentAt.IsZero() || request.SentAt.Before(now.Add(-5*time.Minute)) || request.SentAt.After(now.Add(5*time.Minute)) {
		return errors.New("invalid request time")
	}
	var payload map[string]json.RawMessage
	if err := decodeStrict(request.Payload, &payload); err != nil || payload == nil {
		return errors.New("payload must be a JSON object")
	}
	return nil
}

func canonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", errors.New("trusted WebView origin must be HTTPS")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func failureFor(request bridge.Request, code, message string, retryable bool) bridge.Response {
	requestID := request.ID
	if _, err := ulid.ParseStrict(requestID); err != nil {
		requestID = ulid.Make().String()
	}
	traceID := request.TraceID
	if _, err := ulid.ParseStrict(traceID); err != nil {
		traceID = ulid.Make().String()
	}
	return bridge.Failure(requestID, traceID, code, message, retryable)
}
