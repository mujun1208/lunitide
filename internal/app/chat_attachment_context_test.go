package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/ocrapp"
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

func (p chatAttachmentProvider) List(ctx context.Context, _ provider.Filter) ([]provider.Provider, error) {
	item, err := p.Get(ctx, chatAttachmentProviderID)
	if err != nil {
		return nil, err
	}
	return []provider.Provider{item}, nil
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

type chatAttachmentAdapter struct{ requests chan llmadapter.Request }

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

func (a chatAttachmentAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a chatAttachmentAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a chatAttachmentAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, _ func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.requests <- req
	return llmadapter.Response{}, errors.New("stop after capture")
}

func readableChatAttachment(sessionID string) attachment.Attachment {
	return attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: sessionID,
		OriginalName: "notes.txt", MIME: "text/plain", ParseStatus: attachment.StatusSucceeded,
		ParsedText: "EXPLICIT ATTACHMENT CONTENT",
	}
}

func startAttachmentChat(t *testing.T, store *chatAttachmentStore, contextRefs string) (bridge.Response, <-chan llmadapter.Request) {
	return startAttachmentChatWithFiles(t, store, nil, contextRefs)
}

func startAttachmentChatWithFiles(t *testing.T, store *chatAttachmentStore, files chatAttachmentFiles, contextRefs string) (bridge.Response, <-chan llmadapter.Request) {
	return startAttachmentChatUsingReader(t, store, files, contextRefs, chatAttachmentReader{})
}

func startAttachmentChatUsingReader(t *testing.T, store *chatAttachmentStore, files chatAttachmentFiles, contextRefs string, reader contextapp.Reader) (bridge.Response, <-chan llmadapter.Request) {
	t.Helper()
	requests := make(chan llmadapter.Request, 1)
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, reader, nil, "test", streamTestLease{})
	e.SetAttachmentService(attachmentapp.NewService(store, files))
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","messages":[{"role":"user","content":"current question"}]` + contextRefs + `}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	return response, requests
}

func capturedChatRequest(t *testing.T, requests <-chan llmadapter.Request) llmadapter.Request {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for captured gateway request")
		return llmadapter.Request{}
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

func TestChatStartExplicitAttachmentRefRejectsCrossSession(t *testing.T) {
	att := readableChatAttachment(chatAttachmentOtherID)
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{chatAttachmentID: &att}}
	response, _ := startAttachmentChat(t, store, `,"contextRefs":[{"type":"attachment","id":"`+chatAttachmentID+`"}]`)
	if response.OK || response.Error == nil || response.Error.Code != "CONTEXT_REF_SCOPE_MISMATCH" {
		t.Fatalf("response = %#v, want CONTEXT_REF_SCOPE_MISMATCH", response)
	}
	if calls := store.calls(); calls != 0 {
		t.Fatalf("attachment list calls = %d, want 0", calls)
	}
}

func TestChatStartUnreadableAttachmentStillRuns(t *testing.T) {
	att := readableChatAttachment(chatAttachmentSessionID)
	att.ParseStatus = attachment.StatusPending
	att.ParsedText = ""
	att.ParseErrorCode = "PARSE_FAILED"
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{chatAttachmentID: &att}}
	response, requests := startAttachmentChat(t, store, `,"contextRefs":[{"type":"attachment","id":"`+chatAttachmentID+`"}]`)
	if !response.OK {
		t.Fatalf("unreadable attachment aborted the turn: %#v", response)
	}
	var combined strings.Builder
	for _, message := range capturedChatRequest(t, requests).Messages {
		combined.WriteString(message.Content)
	}
	if !strings.Contains(combined.String(), "notes.txt") || !strings.Contains(combined.String(), "没有抽出正文") {
		t.Fatalf("missing saved-file note: %q", combined.String())
	}
}

func TestChatStartOversizedImageUsesLocalOCR(t *testing.T) {
	data := bytes.Repeat([]byte{0}, attachmentapp.MaxVisionImageBytes+64)
	copy(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	digest := sha256.Sum256(data)
	image := attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID,
		FileRef: "shot.png", OriginalName: "shot.png", MIME: "image/png", Size: int64(len(data)),
		SHA256: hex.EncodeToString(digest[:]), ParseStatus: attachment.StatusFailed,
	}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}}
	requests := make(chan llmadapter.Request, 1)
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, chatAttachmentReader{}, nil, "test", streamTestLease{})
	e.SetAttachmentService(attachmentapp.NewService(store, chatAttachmentFiles{"shot.png": data}))
	ocr := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	ocr.SetLocalImage(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "poc node_modules"}}}, nil
	})
	e.SetOCR(ocr)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","messages":[{"role":"user","content":"看这张图"}],"contextRefs":[{"type":"attachment","id":"` + image.ID + `"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("oversized image aborted the turn: %#v", response)
	}
	var combined strings.Builder
	for _, message := range capturedChatRequest(t, requests).Messages {
		combined.WriteString(message.Content)
	}
	if !strings.Contains(combined.String(), "poc node_modules") || strings.Contains(combined.String(), "没有读出画面") {
		t.Fatalf("local OCR text missing: %q", combined.String())
	}
}

func TestChatStartUnreadImageStillRuns(t *testing.T) {
	image := attachment.Attachment{ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID, FileRef: "missing-image", OriginalName: "shot.png", MIME: "image/png", ParseStatus: attachment.StatusFailed}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}}
	response, requests := startAttachmentChat(t, store, `,"contextRefs":[{"type":"attachment","id":"`+chatAttachmentID+`"}]`)
	if !response.OK {
		t.Fatalf("unread image aborted the turn: %#v", response)
	}
	var combined strings.Builder
	for _, message := range capturedChatRequest(t, requests).Messages {
		combined.WriteString(message.Content)
	}
	if !strings.Contains(combined.String(), "shot.png") || !strings.Contains(combined.String(), "没有读出画面") {
		t.Fatalf("missing image note: %q", combined.String())
	}
}

type folderFitReader struct{ content string }

func (r folderFitReader) ListMessages(context.Context, string, string, int) ([]contextapp.Message, error) {
	return []contextapp.Message{{ID: "message", Role: "user", Content: r.content, Sequence: 1}}, nil
}

func (r folderFitReader) SumTokens(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

func readableNamedAttachment(id, name, text string) attachment.Attachment {
	return attachment.Attachment{
		ID: id, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID,
		OriginalName: name, MIME: "text/plain", ParseStatus: attachment.StatusSucceeded, ParsedText: text,
	}
}

func TestChatStartFolderSendFitsLongFileAndKeepsShortOnes(t *testing.T) {
	prior := strings.Repeat("先前对话。", 2500)
	longBody := strings.Repeat("项目日程里的一天安排。", 8000)
	files := []attachment.Attachment{
		readableNamedAttachment(chatAttachmentID, "AI 销售助手 1.1版本需求", "需求很短，保留全文。"),
		readableNamedAttachment("01ARZ3NDEKTSV4RRFFQ69G5FA0", "FAQ-Geekvape&Gec", "常见问题也很短。"),
		readableNamedAttachment("01ARZ3NDEKTSV4RRFFQ69G5FA1", "JACK 访谈总结.md", "访谈纪要很短。"),
		readableNamedAttachment("01ARZ3NDEKTSV4RRFFQ69G5FA2", "Ai 业务助手项目日程", longBody),
	}
	byID := make(map[string]*attachment.Attachment, len(files))
	refs := `,"contextRefs":[`
	for i, file := range files {
		item := file
		byID[item.ID] = &item
		if i > 0 {
			refs += ","
		}
		refs += `{"type":"attachment","id":"` + item.ID + `"}`
	}
	refs += `]`
	response, requests := startAttachmentChatUsingReader(t, &chatAttachmentStore{byID: byID}, nil, refs, folderFitReader{content: prior})
	if !response.OK {
		t.Fatalf("folder send failed: %#v", response)
	}
	var combined strings.Builder
	for _, message := range capturedChatRequest(t, requests).Messages {
		combined.WriteString(message.Content)
		combined.WriteByte('\n')
	}
	text := combined.String()
	for _, want := range []string{"需求很短，保留全文。", "常见问题也很短。", "访谈纪要很短。", "Ai 业务助手项目日程", "正文过长"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in fitted turn", want)
		}
	}
	if strings.Contains(text, longBody) {
		t.Fatal("long schedule was pasted into the prompt whole")
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
