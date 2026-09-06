package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/queueapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func queueDeliveryFixture(t *testing.T) (*Engine, *storage.Store, string) {
	t.Helper()
	e, _, sessionID, path := messageEngine(t)
	store, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e.SetQueueService(queueapp.New(store))
	e.SetChatTurnJournal(store)
	return e, store, sessionID
}
func queueEnqueue(t *testing.T, e *Engine, sessionID, text string) {
	t.Helper()
	if _, err := e.queue.Enqueue(context.Background(), sessionID, "", text, "", ulid.Make().String()); err != nil {
		t.Fatal(err)
	}
}
func queueConsume(t *testing.T, e *Engine, sessionID string) *queueDeliveryDTO {
	t.Helper()
	r := e.Handle(context.Background(), validRequest("run.queueConsume", `{"sessionId":"`+sessionID+`"}`))
	if !r.OK {
		t.Fatalf("consume: %+v", r.Error)
	}
	var out struct {
		Delivery *queueDeliveryDTO `json:"delivery"`
	}
	raw, _ := json.Marshal(r.Payload)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Delivery
}

type lostQueueAppendAck struct {
	MessageService
	lose bool
}

func (s *lostQueueAppendAck) Append(ctx context.Context, key, actor string, request any, value message.Message) (message.Message, error) {
	m, err := s.MessageService.Append(ctx, key, actor, request, value)
	if err == nil && s.lose {
		s.lose = false
		return message.Message{}, errors.New("lost commit acknowledgement")
	}
	return m, err
}

func TestQueueDeliveryLostAckLongTextAndRestartReuseMessages(t *testing.T) {
	e, store, sessionID := queueDeliveryFixture(t)
	text := strings.Repeat("🙂", 8000)
	queueEnqueue(t, e, sessionID, text)
	writer := e.messages
	e.messages = &lostQueueAppendAck{MessageService: writer, lose: true}
	failed := e.Handle(context.Background(), validRequest("run.queueConsume", `{"sessionId":"`+sessionID+`"}`))
	if failed.OK {
		t.Fatal("lost commit ACK hidden")
	}
	pending, err := store.PendingQueueDelivery(context.Background(), sessionID)
	if err != nil || pending.State != "claimed" {
		t.Fatalf("claim lost: %+v %v", pending, err)
	}
	e.messages = writer
	first := queueConsume(t, e, sessionID)
	if first == nil || first.State != "prepared" || len(first.MessageIDs) != 4 {
		t.Fatalf("long input: %+v", first)
	}
	next := NewEngineWithMessages(nil, e.projects, e.sessions, writer, "test", nil)
	next.SetQueueService(queueapp.New(store))
	next.SetChatTurnJournal(store)
	replay := queueConsume(t, next, sessionID)
	if replay.ID != first.ID || strings.Join(replay.MessageIDs, ",") != strings.Join(first.MessageIDs, ",") {
		t.Fatalf("replay changed identity: %+v", replay)
	}
	page, err := writer.List(context.Background(), messageapp.PageRequest{SessionID: sessionID})
	if err != nil || len(page.Items) != 4 {
		t.Fatalf("duplicated message parts: %d %v", len(page.Items), err)
	}
	var recovered strings.Builder
	for _, m := range page.Items {
		recovered.WriteString(m.Text)
	}
	if recovered.String() != text {
		t.Fatal("long input was truncated")
	}
	if _, err := store.GetQueueDelivery(context.Background(), ulid.Make().String(), first.ID); !errors.Is(err, queueapp.ErrNotFound) {
		t.Fatalf("cross-session receipt: %v", err)
	}
}

func TestQueueDeliveryConcurrentStartUnknownResumeAndRewind(t *testing.T) {
	e, store, sessionID := queueDeliveryFixture(t)
	queueEnqueue(t, e, sessionID, "说明必须保留")
	d := queueConsume(t, e, sessionID)
	ctx := context.Background()
	streamID := ulid.Make().String()
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for range 12 {
		wg.Go(func() {
			err := store.StartQueueDelivery(ctx, sessionID, d.ID, streamID)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, queueapp.ErrDeliveryBusy) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("start CAS winners=%d", accepted.Load())
	}
	actual, err := store.GetQueueDelivery(ctx, sessionID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	actual, err = e.reconcileQueueDelivery(ctx, actual)
	if err != nil || actual.State != "unknown" {
		t.Fatalf("restart did not expose uncertainty: %+v %v", actual, err)
	}
	if err := e.validateQueueChatStart(ctx, sessionID, d.ID); !errors.Is(err, queueapp.ErrDeliveryBusy) {
		t.Fatal("unknown delivery automatically replayable")
	}
	actual, err = store.RecoverQueueDelivery(ctx, sessionID, d.ID, "resume")
	if err != nil {
		t.Fatal(err)
	}
	actual, err = e.prepareQueueDelivery(ctx, actual)
	if err != nil || actual.State != "prepared" || actual.MessageIDs[0] != d.MessageIDs[0] {
		t.Fatalf("explicit resume duplicated messages: %+v %v", actual, err)
	}
	rewind := e.messages.(messageRewindService)
	if _, err := rewind.Rewind(ctx, "queue-test-rewind", "test", sessionID, d.MessageIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := e.prepareQueueDelivery(ctx, actual); err == nil {
		t.Fatal("rewound delivery recreated deleted message")
	}
}

type queueJournalFailure struct{ ChatTurnJournal }

func (queueJournalFailure) PutChatTurn(context.Context, string, string, []byte, bool) error {
	return errors.New("checkpoint storage unavailable")
}
func TestQueueDeliveryMidTurnCheckpointFailureRetainsDelivery(t *testing.T) {
	e, store, sessionID := queueDeliveryFixture(t)
	queueEnqueue(t, e, sessionID, "做好了没有")
	cp := chatTurnCheckpoint{StreamID: ulid.Make().String(), Status: turnStatusRunning}
	e.SetChatTurnJournal(queueJournalFailure{store})
	if note, _, err := e.pullQueuedSupplements(context.Background(), sessionID, &cp); err == nil || note != "" {
		t.Fatalf("unsaved checkpoint reached generation: %q %v", note, err)
	}
	d, err := store.PendingQueueDelivery(context.Background(), sessionID)
	if err != nil || d.State != "prepared" {
		t.Fatalf("delivery lost on checkpoint failure: %+v %v", d, err)
	}
	e.SetChatTurnJournal(store)
	note, _, err := e.pullQueuedSupplements(context.Background(), sessionID, &cp)
	if err != nil || !strings.Contains(note, "做好了没有") || len(cp.Injected) != 1 || len(cp.QueueDeliveries) != 1 {
		t.Fatalf("checkpoint retry: %q %+v %v", note, cp, err)
	}
	saved := e.loadTurnCheckpoint(sessionID)
	if len(saved.QueueDeliveries) != 1 || saved.QueueDeliveries[0] != d.ID {
		t.Fatal("durable checkpoint missing delivery")
	}
	d, err = store.GetQueueDelivery(context.Background(), sessionID, d.ID)
	if err != nil || d.State != "started" || d.StreamID != cp.StreamID {
		t.Fatalf("turn binding: %+v %v", d, err)
	}
	if err := store.FinishQueueDeliveries(context.Background(), sessionID, cp.StreamID, true); err != nil {
		t.Fatal(err)
	}
	d, err = store.PendingQueueDelivery(context.Background(), sessionID)
	if err != nil || d.ID != "" {
		t.Fatalf("confirmed delivery still pending: %+v %v", d, err)
	}
}

type queueStartAdapter struct {
	entered, release chan struct{}
	calls            atomic.Int32
}

func (a *queueStartAdapter) Stream(ctx context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls.Add(1)
	close(a.entered)
	select {
	case <-a.release:
	case <-ctx.Done():
		return llmadapter.Response{}, ctx.Err()
	}
	if err := emit(llmadapter.Delta{Text: "收到补充，已完成。"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "收到补充，已完成。"}}, nil
}
func (a *queueStartAdapter) Complete(ctx context.Context, credentials []byte, r llmadapter.Request) (llmadapter.Response, error) {
	return a.Stream(ctx, credentials, r, func(llmadapter.Delta) error { return nil })
}
func (*queueStartAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}

func TestQueueDeliveryActualChatStartCannotRepeatAfterLostAck(t *testing.T) {
	base, store, sessionID := queueDeliveryFixture(t)
	queueEnqueue(t, base, sessionID, "请结合补充继续这项工作")
	d := queueConsume(t, base, sessionID)
	e := NewEngineWithContextReader(chatAttachmentProvider{}, base.projects, base.sessions, base.messages, store.ContextReader(), store, "test", streamTestLease{})
	e.SetQueueService(queueapp.New(store))
	e.SetChatTurnJournal(store)
	a := &queueStartAdapter{entered: make(chan struct{}), release: make(chan struct{})}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	r := validRequest("chat.start", `{"sessionId":"`+sessionID+`","providerId":"`+chatAttachmentProviderID+`","modelId":"model","queueDeliveryId":"`+d.ID+`"}`)
	events := make(chan bridge.Event, 100)
	first := e.HandleStreaming(context.Background(), r, func(v bridge.Event) error { events <- v; return nil })
	if !first.OK {
		t.Fatalf("start: %+v", first.Error)
	}
	select {
	case <-a.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model never started")
	}
	if replay := e.HandleStreaming(context.Background(), r, func(bridge.Event) error { return nil }); replay.OK {
		t.Fatal("lost start ACK duplicated active inference")
	}
	close(a.release)
	if terminal := terminalEvent(t, events); terminal.Type != bridge.EventCompleted {
		t.Fatalf("terminal: %+v", terminal)
	}
	var started struct {
		StreamID string `json:"streamId"`
	}
	raw, _ := json.Marshal(first.Payload)
	_ = json.Unmarshal(raw, &started)
	waitForStreamCleanup(t, e, started.StreamID)
	if replay := e.HandleStreaming(context.Background(), r, func(bridge.Event) error { return nil }); replay.OK {
		t.Fatal("completed delivery started another inference")
	}
	if a.calls.Load() != 1 {
		t.Fatalf("inference count=%d", a.calls.Load())
	}
	actual, err := store.GetQueueDelivery(context.Background(), sessionID, d.ID)
	if err != nil || actual.State != "confirmed" {
		t.Fatalf("receipt not confirmed: %+v %v", actual, err)
	}
}

type queuePivotBetweenReadAndClaim struct {
	*storage.Store
	inject bool
}

func (s *queuePivotBetweenReadAndClaim) ListQueued(ctx context.Context, id string) ([]queueinput.Message, error) {
	items, err := s.Store.ListQueued(ctx, id)
	if err == nil && s.inject {
		s.inject = false
		_, err = queueapp.New(s.Store).Enqueue(ctx, id, "", "帮我打开桌面协议的文件", "", "pivot-between-read-and-claim")
	}
	return items, err
}
func TestQueueDeliveryConcurrentPivotUsesActualClaimedPayload(t *testing.T) {
	e, store, sessionID := queueDeliveryFixture(t)
	queueEnqueue(t, e, sessionID, "做好了没有")
	e.SetQueueService(queueapp.New(&queuePivotBetweenReadAndClaim{Store: store, inject: true}))
	cp := chatTurnCheckpoint{StreamID: ulid.Make().String(), Status: turnStatusRunning}
	note, _, err := e.pullQueuedSupplements(context.Background(), sessionID, &cp)
	if err != nil || note != "" {
		t.Fatalf("new task injected into old task: %q %v", note, err)
	}
	d, err := store.PendingQueueDelivery(context.Background(), sessionID)
	if err != nil || d.Consumer != "renderer" || len(d.Items) != 2 || d.State != "claimed" {
		t.Fatalf("handoff lost actual batch: %+v %v", d, err)
	}
	prepared := queueConsume(t, e, sessionID)
	if prepared.State != "prepared" || len(prepared.MessageIDs) != 2 {
		t.Fatalf("handoff cannot continue normally: %+v", prepared)
	}
}
