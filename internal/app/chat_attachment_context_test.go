package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
)

const (
	chatAttachmentProviderID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	chatAttachmentSessionID  = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	chatAttachmentOtherID    = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	chatAttachmentProjectID  = "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	chatAttachmentID         = "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
)

type chatAttachmentProvider struct{ providerRepositoryStub }

func (chatAttachmentProvider) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{
		ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com", CredentialRef: "credential-ref",
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "model", ContextWindow: 128000, SupportsVision: true}},
	}, nil
}

type chatAttachmentReader struct{}

func (chatAttachmentReader) ListMessages(context.Context, string, string, int) ([]contextapp.Message, error) {
	return []contextapp.Message{{ID: "message", Role: "user", Content: "durable question", Sequence: 1, TokenCount: 3}}, nil
}
func (chatAttachmentReader) SumTokens(context.Context, string, string, string, string) (int64, error) {
	return 3, nil
}

type chatAttachmentStore struct {
	mu        sync.Mutex
	listCalls int
	getCalls  int
	listErr   error
	getErr    error
	listed    []attachment.Attachment
	byID      map[string]*attachment.Attachment
}

func (s *chatAttachmentStore) CreateAttachment(context.Context, attachment.Attachment) error {
	return nil
}
func (s *chatAttachmentStore) GetAttachment(_ context.Context, id string) (*attachment.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if s.getErr != nil {
		return nil, s.getErr
	}
	if a := s.byID[id]; a != nil {
		copy := *a
		return &copy, nil
	}
	return nil, nil
}
func (s *chatAttachmentStore) GetAttachmentForDeletion(context.Context, string) (*attachment.Attachment, error) {
	return nil, nil
}
func (s *chatAttachmentStore) ListAttachmentsByProject(context.Context, string, int) ([]attachment.Attachment, error) {
	return nil, nil
}
func (s *chatAttachmentStore) ListAttachmentsBySession(context.Context, string, int) ([]attachment.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	return append([]attachment.Attachment(nil), s.listed...), s.listErr
}
func (s *chatAttachmentStore) UpdateParseResult(context.Context, string, attachment.ParseStatus, string, string, int64) error {
	return nil
}
func (s *chatAttachmentStore) DeleteAttachment(context.Context, string) error { return nil }
func (s *chatAttachmentStore) ListPendingAttachmentFileCleanup(context.Context, int) ([]string, error) {
	return nil, nil
}
func (s *chatAttachmentStore) CompleteAttachmentFileCleanup(context.Context, string) error {
	return nil
}
func (s *chatAttachmentStore) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listCalls
}
func (s *chatAttachmentStore) gets() int { s.mu.Lock(); defer s.mu.Unlock(); return s.getCalls }

type chatAttachmentAdapter struct{ requests chan gateway.Request }

type chatAttachmentFiles map[string][]byte

func (f chatAttachmentFiles) WriteFile(context.Context, string, []byte) error { return nil }
func (f chatAttachmentFiles) ReadFile(_ context.Context, name string) ([]byte, error) {
	data, ok := f[name]
	if !ok {
		return nil, errors.New("file missing")
	}
	return append([]byte(nil), data...), nil
}
func (f chatAttachmentFiles) DeleteFile(context.Context, string) error { return nil }

func (a chatAttachmentAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a chatAttachmentAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a chatAttachmentAdapter) Stream(_ context.Context, _ []byte, req gateway.Request, _ func(gateway.Delta) error) (gateway.Response, error) {
	a.requests <- req
	return gateway.Response{}, errors.New("stop after capture")
}

func readableChatAttachment(sessionID string) attachment.Attachment {
	return attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: sessionID,
		OriginalName: "notes.txt", MIME: "text/plain", ParseStatus: attachment.StatusSucceeded,
		ParsedText: "EXPLICIT ATTACHMENT CONTENT",
	}
}

func startAttachmentChat(t *testing.T, store *chatAttachmentStore, contextRefs string) (bridge.Response, <-chan gateway.Request) {
	return startAttachmentChatWithFiles(t, store, nil, contextRefs)
}

func startAttachmentChatWithFiles(t *testing.T, store *chatAttachmentStore, files chatAttachmentFiles, contextRefs string) (bridge.Response, <-chan gateway.Request) {
	t.Helper()
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, chatAttachmentReader{}, nil, "test", streamTestLease{})
	e.SetAttachmentService(attachmentapp.NewService(store, files))
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","messages":[{"role":"user","content":"current question"}]` + contextRefs + `}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	return response, requests
}

func capturedChatRequest(t *testing.T, requests <-chan gateway.Request) gateway.Request {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for captured gateway request")
		return gateway.Request{}
	}
}

func TestChatStartWithoutContextRefsSkipsHistoricalAttachmentReadAndInjection(t *testing.T) {
	store := &chatAttachmentStore{listErr: errors.New("attachment storage should not be read"), listed: []attachment.Attachment{readableChatAttachment(chatAttachmentSessionID)}}
	response, requests := startAttachmentChat(t, store, "")
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	if calls := store.calls(); calls != 0 {
		t.Fatalf("attachment list called %d times without explicit refs", calls)
	}
	req := capturedChatRequest(t, requests)
	for _, message := range req.Messages {
		if strings.Contains(message.Content, "EXPLICIT ATTACHMENT CONTENT") {
			t.Fatalf("historical attachment was injected without refs: %#v", req.Messages)
		}
	}
}

func TestChatStartExplicitAttachmentRefReadsAndInjectsOnlySelectedAttachment(t *testing.T) {
	selected := readableChatAttachment(chatAttachmentSessionID)
	unselected := selected
	unselected.ID = chatAttachmentOtherID
	unselected.ParsedText = "UNSELECTED ATTACHMENT CONTENT"
	store := &chatAttachmentStore{listErr: errors.New("must not enumerate attachments"), listed: []attachment.Attachment{unselected}, byID: map[string]*attachment.Attachment{selected.ID: &selected}}
	response, requests := startAttachmentChat(t, store, `,"contextRefs":[{"type":"attachment","id":"`+chatAttachmentID+`"}]`)
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	if calls := store.calls(); calls != 0 {
		t.Fatalf("attachment list calls = %d, want 0", calls)
	}
	if gets := store.gets(); gets != 1 {
		t.Fatalf("attachment get calls = %d, want 1", gets)
	}
	var combined strings.Builder
	for _, message := range capturedChatRequest(t, requests).Messages {
		combined.WriteString(message.Content)
	}
	if !strings.Contains(combined.String(), "EXPLICIT ATTACHMENT CONTENT") || strings.Contains(combined.String(), "UNSELECTED ATTACHMENT CONTENT") {
		t.Fatalf("unexpected assembled attachment context: %q", combined.String())
	}
}

func TestChatStartExplicitAttachmentRefRejectsCrossSessionAndUnreadable(t *testing.T) {
	tests := []struct {
		name string
		att  attachment.Attachment
		code string
	}{
		{name: "cross session", att: readableChatAttachment(chatAttachmentOtherID), code: "CONTEXT_REF_SCOPE_MISMATCH"},
		{name: "unreadable", att: func() attachment.Attachment {
			a := readableChatAttachment(chatAttachmentSessionID)
			a.ParseStatus = attachment.StatusPending
			return a
		}(), code: "CONTEXT_REF_NOT_READABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{chatAttachmentID: &test.att}}
			response, _ := startAttachmentChat(t, store, `,"contextRefs":[{"type":"attachment","id":"`+chatAttachmentID+`"}]`)
			if response.OK || response.Error == nil || response.Error.Code != test.code {
				t.Fatalf("response = %#v, want %s", response, test.code)
			}
			if calls := store.calls(); calls != 0 {
				t.Fatalf("attachment list calls = %d, want 0", calls)
			}
		})
	}
}

func TestChatStartExplicitAttachmentInternalReadErrorIsRetryable(t *testing.T) {
	store := &chatAttachmentStore{getErr: errors.New("temporary database failure")}
	response, _ := startAttachmentChat(t, store, `,"contextRefs":[{"type":"attachment","id":"`+chatAttachmentID+`"}]`)
	if response.OK || response.Error == nil || response.Error.Code != "ATTACHMENT_CONTEXT_READ_FAILED" || !response.Error.Retryable {
		t.Fatalf("response = %#v, want retryable attachment read failure", response)
	}
	if store.calls() != 0 || store.gets() != 1 {
		t.Fatalf("list/get calls = %d/%d, want 0/1", store.calls(), store.gets())
	}
}

func TestChatStartExplicitImageSuccessAndMissingFailure(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	digest := sha256.Sum256(data)
	image := attachment.Attachment{ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID, FileRef: "selected-image", OriginalName: "selected.png", MIME: "image/png", Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), ParseStatus: attachment.StatusFailed}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}, listErr: errors.New("must not enumerate images")}
	response, requests := startAttachmentChatWithFiles(t, store, chatAttachmentFiles{image.FileRef: data}, `,"contextRefs":[{"type":"attachment","id":"`+image.ID+`"}]`)
	if !response.OK {
		t.Fatalf("image chat failed: %#v", response)
	}
	req := capturedChatRequest(t, requests)
	if len(req.Images) != 1 || req.Images[0].MIME != image.MIME || string(req.Images[0].Data) != string(data) {
		t.Fatalf("images = %#v", req.Images)
	}
	if store.calls() != 0 {
		t.Fatalf("image list calls = %d, want 0", store.calls())
	}

	missing := &chatAttachmentStore{}
	response, _ = startAttachmentChat(t, missing, `,"contextRefs":[{"type":"attachment","id":"`+chatAttachmentID+`"}]`)
	if response.OK || response.Error == nil || response.Error.Code != "CONTEXT_REF_NOT_FOUND" || response.Error.Retryable {
		t.Fatalf("missing image response = %#v", response)
	}
}
