package attachmentapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/oklog/ulid/v2"
)

func TestChunkUploadBoundariesOrderingDigestAndIdempotency(t *testing.T) {
	s := NewService(newMockStore(), NewDirFileStorage(t.TempDir()))
	s.uploadDir = t.TempDir()
	seq := 0
	s.idFactory = func() string {
		seq++
		if seq == 1 {
			return "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		}
		return ulid.Make().String()
	}
	data := []byte("chunked attachment")
	sum := sha256.Sum256(data)
	req := BeginUploadRequest{ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", SessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", OriginalName: "safe.txt", MIME: "text/plain", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	id, _, err := s.BeginUpload(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UploadChunk(context.Background(), id, 1, data); !errors.Is(err, ErrUploadOffset) {
		t.Fatalf("out of order: %v", err)
	}
	if _, err = s.UploadChunk(context.Background(), id, 0, data); err != nil {
		t.Fatal(err)
	}
	a, err := s.CommitUpload(context.Background(), id, req.ProjectID, req.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CommitUpload(context.Background(), id, req.ProjectID, req.SessionID)
	if err != nil || again.ID != a.ID {
		t.Fatalf("commit not idempotent: %v", err)
	}
	bad := req
	bad.SHA256 = strings.Repeat("0", 64)
	id, _, _ = s.BeginUpload(context.Background(), bad)
	_, _ = s.UploadChunk(context.Background(), id, 0, data)
	if _, err = s.CommitUpload(context.Background(), id, bad.ProjectID, bad.SessionID); !errors.Is(err, ErrUploadDigest) {
		t.Fatalf("digest: %v", err)
	}
	tooLarge := req
	tooLarge.Size = MaxFileSize + 1
	if _, _, err = s.BeginUpload(context.Background(), tooLarge); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("10MiB+1: %v", err)
	}
	unsafe := req
	unsafe.OriginalName = "../evil.txt"
	if _, _, err = s.BeginUpload(context.Background(), unsafe); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("unsafe name: %v", err)
	}
}

// --- Mocks ---

type mockStore struct {
	mu             sync.Mutex
	attachments    map[string]*attachment.Attachment
	createErr      error
	getErr         error
	deleteErr      error
	updateErr      error
	listErr        error
	pendingCleanup []string
	completeErr    map[string]error
	completed      []string
	listCalls      int
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

func (m *mockStore) GetAttachmentForDeletion(ctx context.Context, id string) (*attachment.Attachment, error) {
	return m.GetAttachment(ctx, id)
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

func (m *mockStore) ListPendingAttachmentFileCleanup(_ context.Context, _ int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls++
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]string(nil), m.pendingCleanup...), nil
}
func (m *mockStore) CompleteAttachmentFileCleanup(_ context.Context, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.completeErr[ref]; err != nil {
		return err
	}
	m.completed = append(m.completed, ref)
	return nil
}

type mockFileStorage struct {
	mu        sync.Mutex
	files     map[string][]byte
	writeErr  error
	readErr   error
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
	// Public reads must hide the soft-deleted attachment.
	got, err := svc.GetAttachment(context.Background(), created.ID)
	if !errors.Is(err, ErrAttachmentNotFound) || got != nil {
		t.Fatalf("GetAttachment after delete = %#v, %v; want nil, ErrAttachmentNotFound", got, err)
	}
	// The store retains the tombstoned metadata.
	stored, _ := store.GetAttachment(context.Background(), created.ID)
	if stored.DeletedAt == nil || stored.IsReadable() {
		t.Error("stored deleted attachment must be tombstoned and unreadable")
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

func TestService_DeleteAttachment_LostResponseRetry(t *testing.T) {
	store := newMockStore()
	store.completeErr = map[string]error{}
	fs := newMockFileStorage()
	svc := newTestService(store, fs)
	created, _ := svc.IngestFile(context.Background(), IngestFileRequest{
		ProjectID: mustULID(), OriginalName: "notes.txt", MIME: "text/plain", Content: []byte("Hello"),
	})
	store.completeErr[created.FileRef] = errors.New("response lost")
	if err := svc.DeleteAttachment(context.Background(), created.ID); err == nil {
		t.Fatal("first delete should report the lost completion response")
	}
	delete(store.completeErr, created.FileRef)
	if err := svc.DeleteAttachment(context.Background(), created.ID); err != nil {
		t.Fatalf("retry after soft-delete failed: %v", err)
	}
}

func TestService_ReconcileFileCleanup_FirstFailureDoesNotStarveLaterSuccess(t *testing.T) {
	store := newMockStore()
	store.pendingCleanup = []string{"first", "later"}
	store.completeErr = map[string]error{"first": errors.New("database busy")}
	fs := newMockFileStorage()
	fs.files["first"] = []byte("a")
	fs.files["later"] = []byte("b")
	err := NewService(store, fs).ReconcileFileCleanup(context.Background())
	if err == nil || !strings.Contains(err.Error(), "first") {
		t.Fatalf("ReconcileFileCleanup error = %v; want aggregated first failure", err)
	}
	if len(store.completed) != 1 || store.completed[0] != "later" {
		t.Fatalf("completed = %v; want later ref completed", store.completed)
	}
	if store.listCalls != 1 {
		t.Fatalf("list calls = %d; want one bounded batch", store.listCalls)
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

func TestService_GetVisionImageByID(t *testing.T) {
	store := newMockStore()
	fs := newMockFileStorage()
	svc := newTestService(store, fs)
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	digest := sha256.Sum256(data)
	att := attachment.Attachment{ID: mustULID(), ProjectID: mustULID(), SessionID: mustULID(), FileRef: "image-ref", OriginalName: "image.png", MIME: "image/png", Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), ParseStatus: attachment.StatusFailed, CreatedAt: time.Now().UTC()}
	store.attachments[att.ID] = &att
	fs.files[att.FileRef] = data
	got, err := svc.GetVisionImage(context.Background(), att.ID, att.SessionID)
	if err != nil || got.AttachmentID != att.ID || string(got.Data) != string(data) {
		t.Fatalf("GetVisionImage = %#v, %v", got, err)
	}
	if _, err := svc.GetVisionImage(context.Background(), att.ID, mustULID()); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("cross-session error = %v, want ErrScopeMismatch", err)
	}
	if _, err := svc.GetVisionImage(context.Background(), mustULID(), att.SessionID); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("missing error = %v, want ErrAttachmentNotFound", err)
	}
}

func TestCommitUploadConcurrentCallersShareOneAttachment(t *testing.T) {
	store := newBlockingCreateStore()
	s := NewService(store, NewDirFileStorage(t.TempDir()))
	s.uploadDir = t.TempDir()
	data := []byte("one concurrent attachment")
	id, req := prepareConcurrentUpload(t, s, data)

	const callers = 12
	type result struct {
		a   attachment.Attachment
		err error
	}
	results := make(chan result, callers)
	for i := 0; i < callers; i++ {
		go func() {
			a, err := s.CommitUpload(context.Background(), id, req.ProjectID, req.SessionID)
			results <- result{a: a, err: err}
		}()
	}
	<-store.started
	close(store.release)

	var attachmentID string
	for i := 0; i < callers; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("commit %d: %v", i, r.err)
		}
		if attachmentID == "" {
			attachmentID = r.a.ID
		} else if r.a.ID != attachmentID {
			t.Fatalf("commit IDs differ: got %q, want %q", r.a.ID, attachmentID)
		}
	}
	if got := store.createCount(); got != 1 {
		t.Fatalf("attachment persisted %d times, want 1", got)
	}
}

func TestCommitUploadFailedAttemptCanBeRetried(t *testing.T) {
	store := newBlockingCreateStore()
	store.createErr = errors.New("temporary create failure")
	s := NewService(store, NewDirFileStorage(t.TempDir()))
	s.uploadDir = t.TempDir()
	id, req := prepareConcurrentUpload(t, s, []byte("retry attachment"))

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := s.CommitUpload(context.Background(), id, req.ProjectID, req.SessionID)
			results <- err
		}()
	}
	<-store.started
	deadline := time.Now().Add(time.Second)
	for {
		s.uploadMu.Lock()
		waiters := s.uploads[id].committing.waiters
		s.uploadMu.Unlock()
		if waiters == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second commit did not join in-flight attempt")
		}
		time.Sleep(time.Millisecond)
	}
	close(store.release)
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil || !strings.Contains(err.Error(), "temporary create failure") {
			t.Fatalf("concurrent failed commit %d: %v", i, err)
		}
	}
	if got := store.createCount(); got != 1 {
		t.Fatalf("failed attempt persisted %d times, want 1", got)
	}
	store.setCreateErr(nil)
	a, err := s.CommitUpload(context.Background(), id, req.ProjectID, req.SessionID)
	if err != nil || a.ID == "" {
		t.Fatalf("retry: attachment=%+v err=%v", a, err)
	}
	if got := store.createCount(); got != 2 {
		t.Fatalf("create attempts = %d, want 2", got)
	}
}

func TestAbortUploadWaitsForCommitAndPreservesSuccessfulResult(t *testing.T) {
	store := newBlockingCreateStore()
	s := NewService(store, NewDirFileStorage(t.TempDir()))
	s.uploadDir = t.TempDir()
	id, req := prepareConcurrentUpload(t, s, []byte("commit wins abort race"))
	commitResult := make(chan attachment.Attachment, 1)
	commitErr := make(chan error, 1)
	go func() {
		a, err := s.CommitUpload(context.Background(), id, req.ProjectID, req.SessionID)
		commitResult <- a
		commitErr <- err
	}()
	<-store.started
	abortDone := make(chan error, 1)
	go func() { abortDone <- s.AbortUpload(context.Background(), id, req.ProjectID, req.SessionID) }()
	select {
	case err := <-abortDone:
		t.Fatalf("abort returned while commit was in progress: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(store.release)
	a := <-commitResult
	if err := <-commitErr; err != nil {
		t.Fatal(err)
	}
	if err := <-abortDone; err != nil {
		t.Fatal(err)
	}
	again, err := s.CommitUpload(context.Background(), id, req.ProjectID, req.SessionID)
	if err != nil || again.ID != a.ID {
		t.Fatalf("commit result lost after abort race: got=%+v err=%v want ID=%q", again, err, a.ID)
	}
	if got := store.createCount(); got != 1 {
		t.Fatalf("attachment persisted %d times, want 1", got)
	}
}

func prepareConcurrentUpload(t *testing.T, s *Service, data []byte) (string, BeginUploadRequest) {
	t.Helper()
	sum := sha256.Sum256(data)
	req := BeginUploadRequest{ProjectID: ulid.Make().String(), SessionID: ulid.Make().String(), OriginalName: "concurrent.txt", MIME: "text/plain", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	id, _, err := s.BeginUpload(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadChunk(context.Background(), id, 0, data); err != nil {
		t.Fatal(err)
	}
	return id, req
}

type blockingCreateStore struct {
	*mockStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
	countMu sync.Mutex
	count   int
}

func newBlockingCreateStore() *blockingCreateStore {
	return &blockingCreateStore{mockStore: newMockStore(), started: make(chan struct{}), release: make(chan struct{})}
}

func (m *blockingCreateStore) CreateAttachment(ctx context.Context, a attachment.Attachment) error {
	m.countMu.Lock()
	m.count++
	m.countMu.Unlock()
	m.once.Do(func() { close(m.started) })
	<-m.release
	return m.mockStore.CreateAttachment(ctx, a)
}

func (m *blockingCreateStore) createCount() int {
	m.countMu.Lock()
	defer m.countMu.Unlock()
	return m.count
}

func (m *blockingCreateStore) setCreateErr(err error) {
	m.mockStore.mu.Lock()
	m.mockStore.createErr = err
	m.mockStore.mu.Unlock()
}
