package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/queueapp"
)

// 复现用户实测场景：长回复流式中入队带附件的追问，断言补充被折叠进
// 当前轮——第二次模型调用必须发生，且请求中包含附件的 OCR 内容。
type supplementFoldAdapter struct {
	mu         sync.Mutex
	requests   []llmadapter.Request
	calls      int
	entered    chan struct{}
	release    chan struct{}
	secondDone chan struct{}
}

func (a *supplementFoldAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (a *supplementFoldAdapter) Complete(ctx context.Context, secret []byte, r llmadapter.Request) (llmadapter.Response, error) {
	return a.Stream(ctx, secret, r, func(llmadapter.Delta) error { return nil })
}
func (a *supplementFoldAdapter) Stream(ctx context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.mu.Lock()
	a.calls++
	n := a.calls
	a.requests = append(a.requests, req)
	a.mu.Unlock()
	if n == 1 {
		close(a.entered)
		select {
		case <-a.release:
		case <-ctx.Done():
			return llmadapter.Response{}, ctx.Err()
		}
		_ = emit(llmadapter.Delta{Text: "秋天的散文正文。"})
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "秋天的散文正文。"}}, nil
	}
	defer close(a.secondDone)
	_ = emit(llmadapter.Delta{Text: "图中文字：LUNITIDE OCR 8888"})
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "图中文字：LUNITIDE OCR 8888"}}, nil
}

func TestQueueSupplementFoldedWithAttachment(t *testing.T) {
	base, store, sessionID := queueDeliveryFixture(t)
	image := attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: sessionID,
		FileRef: "queued-image", OriginalName: "ocr.png", MIME: "image/png", ParseStatus: attachment.StatusSucceeded,
		ParsedText: "LUNITIDE OCR 8888",
	}
	attStore := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}}
	e := NewEngineWithContextReader(chatAttachmentProvider{}, base.projects, base.sessions, base.messages, store.ContextReader(), store, "test", streamTestLease{})
	e.SetQueueService(queueapp.New(store))
	e.SetChatTurnJournal(store)
	e.SetAttachmentService(attachmentapp.NewService(attStore, nil))
	a := &supplementFoldAdapter{entered: make(chan struct{}), release: make(chan struct{}), secondDone: make(chan struct{})}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })

	payload := `{"sessionId":"` + sessionID + `","providerId":"` + chatAttachmentProviderID + `","modelId":"model","messages":[{"role":"user","content":"写一篇散文"}]}`
	events := make(chan bridge.Event, 400)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(v bridge.Event) error { events <- v; return nil })
		raw, _ := json.Marshal(r)
		t.Logf("chat.start result: %s", raw)
	}()

	select {
	case <-a.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("模型第一次调用未启动")
	}
	queueEnqueue(t, e, sessionID, "[attachment:"+chatAttachmentID+"|ocr.png] 这张图片里写了什么文字？")
	close(a.release)

	select {
	case <-a.secondDone:
	case <-time.After(30 * time.Second):
		a.mu.Lock()
		calls, msgs := a.calls, len(a.requests)
		a.mu.Unlock()
		t.Fatalf("第二次模型调用未发生（calls=%d requests=%d）：排队补充在长回复后被丢弃", calls, msgs)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.calls < 2 {
		t.Fatalf("model calls=%d", a.calls)
	}
	var combined strings.Builder
	for _, m := range a.requests[1].Messages {
		combined.WriteString(m.Content)
		combined.WriteString("\n")
	}
	if !strings.Contains(combined.String(), "LUNITIDE OCR 8888") {
		t.Fatalf("第二次请求缺少附件 OCR 内容:\n%s", combined.String())
	}
	if !strings.Contains(combined.String(), "这张图片里写了什么文字") {
		t.Fatalf("第二次请求缺少排队补充原文:\n%s", combined.String())
	}
}
