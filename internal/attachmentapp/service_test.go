package attachmentapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/oklog/ulid/v2"
)

// --- Mocks ---

type mockStore struct {
	mu          sync.Mutex
	attachments map[string]*attachment.Attachment
	createErr   error
	getErr      error
	deleteErr   error
	updateErr   error
	listErr     error
}

func newMockStore() *mockStore {
	return &mockStore{attachments: make(map[string]*attachment.Attachment)}
}

func (m *mockStore) CreateAttachment(_ context.Context, a attachment.Attachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.ParseStatus == "" {
		a.ParseStatus = attachment.StatusPending
	}
	if a.MIME == "" {
		a.MIME = "application/octet-stream"
	}
	cp := a
	m.attachments[a.ID] = &cp
	return nil
}

func (m *mockStore) GetAttachment(_ context.Context, id string) (*attachment.Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	if a, ok := m.attachments[id]; ok {
		cp := *a
		return &cp, nil
	}
	return nil, nil
}

func (m *mockStore) ListAttachmentsByProject(_ context.Context, projectID string, limit int) ([]attachment.Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []attachment.Attachment
	for _, a := range m.attachments {
		if a.ProjectID == projectID && a.DeletedAt == nil {
			result = append(result, *a)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStore) ListAttachmentsBySession(_ context.Context, sessionID string, limit int) ([]attachment.Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []attachment.Attachment
	for _, a := range m.attachments {
		if a.SessionID == sessionID && a.DeletedAt == nil {
			result = append(result, *a)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStore) UpdateParseResult(_ context.Context, id string, status attachment.ParseStatus, errCode string, parsedText string, parsedTextBytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	a, ok := m.attachments[id]
	if !ok {
		return errors.New("attachment not found in mock")
	}
	a.ParseStatus = status
	a.ParseErrorCode = errCode
	a.ParsedText = parsedText
	a.ParsedTextBytes = parsedTextBytes
	return nil
}

func (m *mockStore) DeleteAttachment(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	a, ok := m.attachments[id]
	if !ok || a.DeletedAt != nil {
		return nil // idempotent
	}
	now := time.Now().UTC()
	a.DeletedAt = &now
	return nil
}

type mockFileStorage struct {
	mu       sync.Mutex
	files    map[string][]byte
	writeErr error
	readErr  error
	deleteErr error
}

func newMockFileStorage() *mockFileStorage {
	return &mockFileStorage{files: make(map[string][]byte)}
}

func (m *mockFileStorage) WriteFile(_ context.Context, name string, content []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	cp := make([]byte, len(content))
	copy(cp, content)
	m.files[name] = cp
	return nil
}

func (m *mockFileStorage) ReadFile(_ context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readErr != nil {
		return nil, m.readErr
	}
	data, ok := m.files[name]
	if !ok {
		return nil, errors.New("file not found")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

func (m *mockFileStorage) DeleteFile(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.files, name)
	return nil
}

// --- Helpers ---

func mustULID() string {
	return ulid.Make().String()
}

func newTestService(store *mockStore, fs *mockFileStorage) *Service {
	return NewService(store, fs)
}

// --- IngestFile Tests ---

func TestService_IngestFile_Success(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)
	projectID := mustULID()

	att, err := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    projectID,
		OriginalName: "notes.txt",
		MIME:         "text/plain",
		Content:      []byte("Hello, World!"),
	})
	if err != nil {
		t.Fatalf("IngestFile failed: %v", err)
	}
	if att.ID == "" {
		t.Fatal("attachment ID empty")
	}
	if att.ProjectID != projectID {
		t.Errorf("project ID = %s, want %s", att.ProjectID, projectID)
	}
	if att.ParseStatus != attachment.StatusSucceeded {
		t.Errorf("parse status = %s, want succeeded", att.ParseStatus)
	}
	if att.SHA256 == "" || len(att.SHA256) != 64 {
		t.Errorf("sha256 len = %d, want 64", len(att.SHA256))
	}
	if att.ParsedText != "Hello, World!" {
		t.Errorf("parsed text = %s, want 'Hello, World!'", att.ParsedText)
	}
	if att.ParsedTextBytes != int64(len("Hello, World!")) {
		t.Errorf("parsed text bytes = %d, want %d", att.ParsedTextBytes, len("Hello, World!"))
	}
	// Verify file was written.
	if _, ok := fs.files[att.ID]; !ok {
		t.Error("file was not written to storage")
	}
}

func TestService_IngestFile_UnsupportedMIME(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	att, err := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "image.png",
		MIME:         "image/png",
		Content:      []byte("\x89PNG\r\n\x1a\n"),
	})
	if err != nil {
		t.Fatalf("IngestFile should not return error for unsupported MIME: %v", err)
	}
	if att.ParseStatus != attachment.StatusFailed {
		t.Errorf("parse status = %s, want failed", att.ParseStatus)
	}
	if att.ParseErrorCode != "UNSUPPORTED_MIME" {
		t.Errorf("parse error code = %s, want UNSUPPORTED_MIME", att.ParseErrorCode)
	}
	if att.IsReadable() {
		t.Error("attachment with failed parse should not be readable")
	}
}

func TestService_IngestFile_InvalidContent(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	// Invalid UTF-8 in a text file.
	invalidUTF8 := []byte{0xff, 0xfe, 0xfd}
	att, err := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "bad.txt",
		MIME:         "text/plain",
		Content:      invalidUTF8,
	})
	if err != nil {
		t.Fatalf("IngestFile should not return error for invalid content: %v", err)
	}
	if att.ParseStatus != attachment.StatusFailed {
		t.Errorf("parse status = %s, want failed", att.ParseStatus)
	}
	if att.ParseErrorCode != "INVALID_CONTENT" {
		t.Errorf("parse error code = %s, want INVALID_CONTENT", att.ParseErrorCode)
	}
}

func TestService_IngestFile_FileTooLarge(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	_, err := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "big.bin",
		MIME:         "application/octet-stream",
		Content:      make([]byte, MaxFileSize+1),
	})
	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("err = %v, want ErrFileTooLarge", err)
	}
}

func TestService_IngestFile_WithSessionID(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)
	projectID := mustULID()
	sessionID := mustULID()

	att, err := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    projectID,
		SessionID:    sessionID,
		OriginalName: "session-notes.txt",
		MIME:         "text/plain",
		Content:      []byte("Session notes"),
	})
	if err != nil {
		t.Fatalf("IngestFile failed: %v", err)
	}
	if att.SessionID != sessionID {
		t.Errorf("session ID = %s, want %s", att.SessionID, sessionID)
	}
}

func TestService_IngestFile_FileWriteError(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	fs.writeErr = errors.New("disk full")
	svc := newTestService(store, fs)

	_, err := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "notes.txt",
		MIME:         "text/plain",
		Content:      []byte("Hello"),
	})
	if err == nil {
		t.Fatal("expected error when file write fails")
	}
	// The attachment should be marked as failed.
	if len(store.attachments) != 1 {
		t.Fatalf("expected 1 attachment in store, got %d", len(store.attachments))
	}
	for _, a := range store.attachments {
		if a.ParseStatus != attachment.StatusFailed {
			t.Errorf("parse status = %s, want failed", a.ParseStatus)
		}
		if a.ParseErrorCode != "FILE_WRITE_FAILED" {
			t.Errorf("parse error code = %s, want FILE_WRITE_FAILED", a.ParseErrorCode)
		}
	}
}

// --- GetAttachment Tests ---

func TestService_GetAttachment_Success(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	created, _ := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "notes.txt",
		MIME:         "text/plain",
		Content:      []byte("Hello"),
	})
	got, err := svc.GetAttachment(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetAttachment failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %s, want %s", got.ID, created.ID)
	}
}

func TestService_GetAttachment_NotFound(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	_, err := svc.GetAttachment(context.Background(), mustULID())
	if !errors.Is(err, ErrAttachmentNotFound) {
		t.Errorf("err = %v, want ErrAttachmentNotFound", err)
	}
}

// --- ListByProject / ListBySession Tests ---

func TestService_ListByProject(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)
	projectID := mustULID()
	otherProjectID := mustULID()

	for i := 0; i < 3; i++ {
		_, _ = svc.IngestFile(context.Background(), IngestFileRequest{
			ProjectID:    projectID,
			OriginalName: "file.txt",
			MIME:         "text/plain",
			Content:      []byte("content"),
		})
	}
	_, _ = svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    otherProjectID,
		OriginalName: "other.txt",
		MIME:         "text/plain",
		Content:      []byte("other"),
	})

	result, err := svc.ListByProject(context.Background(), projectID, 0)
	if err != nil {
		t.Fatalf("ListByProject failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("len = %d, want 3", len(result))
	}
}

func TestService_ListBySession(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)
	projectID := mustULID()
	sessionID := mustULID()
	otherSessionID := mustULID()

	_, _ = svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    projectID,
		SessionID:    sessionID,
		OriginalName: "s1.txt",
		MIME:         "text/plain",
		Content:      []byte("s1"),
	})
	_, _ = svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    projectID,
		SessionID:    otherSessionID,
		OriginalName: "s2.txt",
		MIME:         "text/plain",
		Content:      []byte("s2"),
	})

	result, err := svc.ListBySession(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("ListBySession failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
	if result[0].SessionID != sessionID {
		t.Errorf("session ID = %s, want %s", result[0].SessionID, sessionID)
	}
}

func TestService_ListReadableBySession_FiltersFailedAndDeleted(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)
	projectID := mustULID()
	sessionID := mustULID()

	// Succeeded attachment.
	_, _ = svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    projectID,
		SessionID:    sessionID,
		OriginalName: "good.txt",
		MIME:         "text/plain",
		Content:      []byte("good"),
	})
	// Failed attachment (unsupported MIME).
	_, _ = svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    projectID,
		SessionID:    sessionID,
		OriginalName: "bad.png",
		MIME:         "image/png",
		Content:      []byte("\x89PNG"),
	})

	readable, err := svc.ListReadableBySession(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("ListReadableBySession failed: %v", err)
	}
	if len(readable) != 1 {
		t.Errorf("readable count = %d, want 1", len(readable))
	}
	if readable[0].OriginalName != "good.txt" {
		t.Errorf("original name = %s, want good.txt", readable[0].OriginalName)
	}
}

// --- DeleteAttachment Tests ---

func TestService_DeleteAttachment_Success(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	created, _ := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "notes.txt",
		MIME:         "text/plain",
		Content:      []byte("Hello"),
	})
	if err := svc.DeleteAttachment(context.Background(), created.ID); err != nil {
		t.Fatalf("DeleteAttachment failed: %v", err)
	}
	// File should be deleted.
	if _, ok := fs.files[created.ID]; ok {
		t.Error("file was not deleted from storage")
	}
	// Attachment should be soft-deleted.
	got, _ := svc.GetAttachment(context.Background(), created.ID)
	if got.DeletedAt == nil {
		t.Error("attachment DeletedAt should be set")
	}
	if got.IsReadable() {
		t.Error("deleted attachment should not be readable")
	}
}

func TestService_DeleteAttachment_NotFound(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	err := svc.DeleteAttachment(context.Background(), mustULID())
	if !errors.Is(err, ErrAttachmentNotFound) {
		t.Errorf("err = %v, want ErrAttachmentNotFound", err)
	}
}

func TestService_DeleteAttachment_Idempotent(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	created, _ := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "notes.txt",
		MIME:         "text/plain",
		Content:      []byte("Hello"),
	})
	// First delete succeeds.
	if err := svc.DeleteAttachment(context.Background(), created.ID); err != nil {
		t.Fatalf("first delete failed: %v", err)
	}
	// Second delete should also succeed (idempotent).
	if err := svc.DeleteAttachment(context.Background(), created.ID); err != nil {
		t.Fatalf("second delete failed: %v", err)
	}
}

// --- Parsed Text Truncation Test ---

func TestService_IngestFile_TruncatesLargeParsedText(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)

	// Create content larger than MaxParsedTextBytes.
	largeContent := make([]byte, MaxParsedTextBytes+1000)
	for i := range largeContent {
		largeContent[i] = 'a'
	}

	att, err := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID:    mustULID(),
		OriginalName: "large.txt",
		MIME:         "text/plain",
		Content:      largeContent,
	})
	if err != nil {
		t.Fatalf("IngestFile failed: %v", err)
	}
	if att.ParseStatus != attachment.StatusSucceeded {
		t.Errorf("parse status = %s, want succeeded", att.ParseStatus)
	}
	if att.ParsedTextBytes > MaxParsedTextBytes {
		t.Errorf("parsed text bytes = %d, want <= %d", att.ParsedTextBytes, MaxParsedTextBytes)
	}
}
