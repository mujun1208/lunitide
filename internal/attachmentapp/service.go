package attachmentapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/oklog/ulid/v2"
)

// Sentinel errors for the attachment service.
var (
	// ErrAttachmentNotFound is returned when the attachment does not exist.
	ErrAttachmentNotFound = errors.New("attachment not found")
	// ErrFileTooLarge is returned when the ingested file exceeds the maximum
	// allowed size (10 MiB).
	ErrFileTooLarge = errors.New("attachment file too large")
	// ErrUnsupportedMIME is returned when the attachment MIME type is not
	// supported for text extraction.
	ErrUnsupportedMIME = errors.New("unsupported attachment MIME type")
	// ErrInvalidContent is returned when the ingested content fails validation
	// (e.g., not valid UTF-8 for text types).
	ErrInvalidContent = errors.New("attachment content invalid")
	ErrScopeMismatch  = errors.New("attachment project/session scope mismatch")
	ErrImageIntegrity = errors.New("attachment image integrity check failed")
	ErrImageBudget    = errors.New("attachment image budget exceeded")
	ErrUploadNotFound = errors.New("attachment upload not found or expired")
	ErrUploadOffset   = errors.New("attachment upload offset mismatch")
	ErrUploadDigest   = errors.New("attachment upload digest mismatch")
)

// MaxFileSize is the maximum allowed attachment file size in bytes (10 MiB).
const MaxFileSize = 10485760

// MaxParsedTextBytes is the maximum stored parsed text size in bytes (1 MiB).
const MaxParsedTextBytes = 1048576

const MaxVisionImages = 4
const MaxVisionImageBytes = 180 * 1024
const MaxVisionBatchBytes = 3670016

// Keep the complete JSON/Base64 Bridge envelope comfortably below every
// transport boundary (WebView2 -> Host -> Engine), rather than merely below
// the Host's absolute 256 KiB ceiling.
const MaxUploadChunkBytes = 32 * 1024
const UploadTTL = 15 * time.Minute

type uploadState struct {
	projectID, sessionID, originalName, mime, sha256, path string
	size, offset                                           int64
	expires                                                time.Time
	committed                                              *attachment.Attachment
	commitErr                                              error
	committing                                             *uploadCommit
}

type uploadCommit struct {
	done       chan struct{}
	attachment attachment.Attachment
	err        error
	waiters    int
}

type VisionImage struct {
	AttachmentID string
	MIME         string
	Data         []byte
}

// Store defines the storage operations for attachments.
type Store interface {
	CreateAttachment(ctx context.Context, a attachment.Attachment) error
	GetAttachment(ctx context.Context, id string) (*attachment.Attachment, error)
	GetAttachmentForDeletion(ctx context.Context, id string) (*attachment.Attachment, error)
	ListAttachmentsByProject(ctx context.Context, projectID string, limit int) ([]attachment.Attachment, error)
	ListAttachmentsBySession(ctx context.Context, sessionID string, limit int) ([]attachment.Attachment, error)
	UpdateParseResult(ctx context.Context, id string, status attachment.ParseStatus, errCode string, parsedText string, parsedTextBytes int64) error
	DeleteAttachment(ctx context.Context, id string) error
	ListPendingAttachmentFileCleanup(ctx context.Context, limit int) ([]string, error)
	CompleteAttachmentFileCleanup(ctx context.Context, fileRef string) error
}

// Service provides attachment ingestion, parsing, and query operations.
// The Engine delegates to this service for secure file handling
// (ADR-005 §7: attachment isolation).
type Service struct {
	store       Store
	fileStorage FileStorage
	idFactory   func() string
	now         func() time.Time
	uploadMu    sync.Mutex
	uploads     map[string]*uploadState
	uploadDir   string
}

// NewService creates a new attachment service.
func NewService(store Store, fileStorage FileStorage) *Service {
	return &Service{
		store:       store,
		fileStorage: fileStorage,
		idFactory:   func() string { return ulid.Make().String() },
		now:         func() time.Time { return time.Now().UTC() },
		uploads:     make(map[string]*uploadState), uploadDir: filepath.Join(os.TempDir(), "lunitide-attachment-uploads"),
	}
}

type BeginUploadRequest struct {
	ProjectID, SessionID, OriginalName, MIME, SHA256 string
	Size                                             int64
}

func (s *Service) BeginUpload(_ context.Context, req BeginUploadRequest) (string, time.Time, error) {
	if req.ProjectID == "" || req.OriginalName == "" || len(req.OriginalName) > 256 || filepath.Base(req.OriginalName) != req.OriginalName || strings.ContainsAny(req.OriginalName, "/\\:\x00") || req.Size < 0 || req.Size > MaxFileSize || len(req.SHA256) != 64 {
		return "", time.Time{}, ErrInvalidContent
	}
	if _, err := hex.DecodeString(req.SHA256); err != nil {
		return "", time.Time{}, ErrInvalidContent
	}
	if req.MIME == "" || len(req.MIME) > 128 {
		return "", time.Time{}, ErrInvalidContent
	}
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	s.expireUploadsLocked()
	if err := os.MkdirAll(s.uploadDir, 0700); err != nil {
		return "", time.Time{}, err
	}
	id := s.idFactory()
	path := filepath.Join(s.uploadDir, id+".part")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", time.Time{}, err
	}
	_ = f.Close()
	expires := s.now().Add(UploadTTL)
	s.uploads[id] = &uploadState{projectID: req.ProjectID, sessionID: req.SessionID, originalName: req.OriginalName, mime: req.MIME, sha256: strings.ToLower(req.SHA256), path: path, size: req.Size, expires: expires}
	return id, expires, nil
}
func (s *Service) UploadChunk(_ context.Context, id string, offset int64, content []byte) (int64, error) {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	s.expireUploadsLocked()
	u := s.uploads[id]
	if u == nil || u.committed != nil {
		return 0, ErrUploadNotFound
	}
	if offset != u.offset {
		return u.offset, ErrUploadOffset
	}
	if len(content) == 0 || len(content) > MaxUploadChunkBytes || u.offset+int64(len(content)) > u.size {
		return u.offset, ErrInvalidContent
	}
	f, err := os.OpenFile(u.path, os.O_WRONLY, 0600)
	if err != nil {
		return u.offset, err
	}
	_, err = f.WriteAt(content, offset)
	closeErr := f.Close()
	if err != nil {
		return u.offset, err
	}
	if closeErr != nil {
		return u.offset, closeErr
	}
	u.offset += int64(len(content))
	u.expires = s.now().Add(UploadTTL)
	return u.offset, nil
}
func (s *Service) CommitUpload(ctx context.Context, id, projectID, sessionID string) (attachment.Attachment, error) {
	s.uploadMu.Lock()
	s.expireUploadsLocked()
	u := s.uploads[id]
	if u == nil {
		s.uploadMu.Unlock()
		return attachment.Attachment{}, ErrUploadNotFound
	}
	if u.projectID != projectID || u.sessionID != sessionID {
		s.uploadMu.Unlock()
		return attachment.Attachment{}, ErrScopeMismatch
	}
	if u.committed != nil {
		a := *u.committed
		err := u.commitErr
		s.uploadMu.Unlock()
		return a, err
	}
	if attempt := u.committing; attempt != nil {
		attempt.waiters++
		s.uploadMu.Unlock()
		<-attempt.done
		return attempt.attachment, attempt.err
	}
	if u.offset != u.size {
		s.uploadMu.Unlock()
		return attachment.Attachment{}, ErrUploadOffset
	}
	attempt := &uploadCommit{done: make(chan struct{})}
	u.committing = attempt
	path := u.path
	req := IngestFileRequest{ProjectID: u.projectID, SessionID: u.sessionID, OriginalName: u.originalName, MIME: u.mime}
	wantDigest := u.sha256
	s.uploadMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		s.finishUploadCommit(id, attempt, attachment.Attachment{}, err, false)
		return attempt.attachment, attempt.err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != wantDigest {
		s.finishUploadCommit(id, attempt, attachment.Attachment{}, ErrUploadDigest, true)
		_ = os.Remove(path)
		return attempt.attachment, attempt.err
	}
	req.Content = data
	a, err := s.IngestFile(ctx, req)
	s.finishUploadCommit(id, attempt, a, err, false)
	if err == nil || a.ID != "" {
		_ = os.Remove(path)
	}
	return attempt.attachment, attempt.err
}

func (s *Service) finishUploadCommit(id string, attempt *uploadCommit, a attachment.Attachment, err error, discard bool) {
	s.uploadMu.Lock()
	attempt.attachment = a
	attempt.err = err
	if current := s.uploads[id]; current != nil && current.committing == attempt {
		if discard {
			delete(s.uploads, id)
		} else if err == nil || a.ID != "" {
			// IngestFile assigns an ID only after CreateAttachment succeeds. Even
			// if later file/parse I/O fails, retrying would persist a duplicate.
			current.committed = &a
			current.commitErr = err
			current.committing = nil
			current.expires = s.now().Add(UploadTTL)
		} else {
			current.committing = nil
			current.expires = s.now().Add(UploadTTL)
		}
	}
	close(attempt.done)
	s.uploadMu.Unlock()
}
func (s *Service) AbortUpload(_ context.Context, id, projectID, sessionID string) error {
	for {
		s.uploadMu.Lock()
		s.expireUploadsLocked()
		u := s.uploads[id]
		if u == nil {
			s.uploadMu.Unlock()
			return nil
		}
		if u.projectID != projectID || u.sessionID != sessionID {
			s.uploadMu.Unlock()
			return ErrScopeMismatch
		}
		if u.committed != nil {
			s.uploadMu.Unlock()
			return nil
		}
		if attempt := u.committing; attempt != nil {
			s.uploadMu.Unlock()
			<-attempt.done
			continue
		}
		path := u.path
		delete(s.uploads, id)
		s.uploadMu.Unlock()
		_ = os.Remove(path)
		return nil
	}
}
func (s *Service) expireUploadsLocked() {
	now := s.now()
	for id, u := range s.uploads {
		if u.committing == nil && !now.Before(u.expires) {
			_ = os.Remove(u.path)
			delete(s.uploads, id)
		}
	}
}

// IngestFileRequest defines the parameters for ingesting an attachment.
type IngestFileRequest struct {
	ProjectID    string
	SessionID    string // optional; when empty, the attachment is project-scoped
	OriginalName string
	MIME         string
	Content      []byte
}

// IngestFile ingests a user-supplied file: validates the content, computes
// SHA-256, writes to the controlled data directory, creates the metadata
// record, and synchronously parses the text. The attachment is created with
// parse_status "pending", then updated to "succeeded" or "failed" based on
// the parse outcome (ADR-005 §7).
//
// A single attachment failure (e.g., unsupported MIME) does not block the
// conversation: the attachment record is created with parse_status "failed"
// and IsReadable() returns false, so it is never injected as prior context.
func (s *Service) IngestFile(ctx context.Context, req IngestFileRequest) (attachment.Attachment, error) {
	// 1. Validate request.
	if req.ProjectID == "" {
		return attachment.Attachment{}, errors.New("project ID is required")
	}
	if req.OriginalName == "" || len(req.OriginalName) > 256 {
		return attachment.Attachment{}, errors.New("original name must be 1-256 bytes")
	}
	if len(req.Content) > MaxFileSize {
		return attachment.Attachment{}, ErrFileTooLarge
	}
	mime := req.MIME
	if mime == "" {
		mime = "application/octet-stream"
	}
	if len(mime) > 128 {
		return attachment.Attachment{}, errors.New("mime too long")
	}

	// 2. Compute SHA-256 of the content.
	digest := sha256.Sum256(req.Content)
	sha256Hex := hex.EncodeToString(digest[:])

	// 3. Generate attachment ID and file reference.
	id := s.idFactory()
	fileRef := id // file is stored by its ULID

	// 4. Create the attachment record with status "pending".
	now := s.now()
	att := attachment.Attachment{
		ID:           id,
		ProjectID:    req.ProjectID,
		SessionID:    req.SessionID,
		FileRef:      fileRef,
		OriginalName: req.OriginalName,
		MIME:         mime,
		Size:         int64(len(req.Content)),
		SHA256:       sha256Hex,
		ParseStatus:  attachment.StatusPending,
		CreatedAt:    now,
	}
	if err := att.Validate(); err != nil {
		return attachment.Attachment{}, fmt.Errorf("validate attachment: %w", err)
	}
	if err := s.store.CreateAttachment(ctx, att); err != nil {
		return attachment.Attachment{}, fmt.Errorf("create attachment record: %w", err)
	}

	// 5. Write the file to the controlled data directory.
	if s.fileStorage != nil {
		if err := s.fileStorage.WriteFile(ctx, fileRef, req.Content); err != nil {
			// Mark the attachment as failed since the file could not be stored.
			_ = s.store.UpdateParseResult(ctx, id, attachment.StatusFailed, "FILE_WRITE_FAILED", "", 0)
			att.ParseStatus = attachment.StatusFailed
			att.ParseErrorCode = "FILE_WRITE_FAILED"
			return att, fmt.Errorf("write attachment file: %w", err)
		}
	}

	// 6. Parse the file synchronously (text extraction).
	parsedText, parseErr := s.parse(mime, req.Content)
	if parseErr != nil {
		errCode := "PARSE_FAILED"
		if errors.Is(parseErr, ErrUnsupportedMIME) {
			errCode = "UNSUPPORTED_MIME"
		} else if errors.Is(parseErr, ErrInvalidContent) {
			errCode = "INVALID_CONTENT"
		}
		_ = s.store.UpdateParseResult(ctx, id, attachment.StatusFailed, errCode, "", 0)
		att.ParseStatus = attachment.StatusFailed
		att.ParseErrorCode = errCode
		return att, nil // parse failure is not an error from the caller's perspective
	}

	// 7. Truncate parsed text to the maximum allowed size.
	if len(parsedText) > MaxParsedTextBytes {
		parsedText = truncateUTF8(parsedText, MaxParsedTextBytes)
	}
	parsedTextBytes := int64(len(parsedText))

	// 8. Update the attachment with the parse result.
	if err := s.store.UpdateParseResult(ctx, id, attachment.StatusSucceeded, "", parsedText, parsedTextBytes); err != nil {
		return att, fmt.Errorf("update parse result: %w", err)
	}
	att.ParseStatus = attachment.StatusSucceeded
	att.ParsedText = parsedText
	att.ParsedTextBytes = parsedTextBytes
	return att, nil
}

// GetAttachment returns an attachment by ID for inspection.
func (s *Service) GetAttachment(ctx context.Context, id string) (*attachment.Attachment, error) {
	att, err := s.store.GetAttachment(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	if att == nil {
		return nil, ErrAttachmentNotFound
	}
	if att.DeletedAt != nil {
		return nil, ErrAttachmentNotFound
	}
	return att, nil
}

// ListByProject returns attachments for a project ordered by creation time
// descending. Soft-deleted attachments are excluded.
func (s *Service) ListByProject(ctx context.Context, projectID string, limit int) ([]attachment.Attachment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.store.ListAttachmentsByProject(ctx, projectID, limit)
}

// ListBySession returns attachments for a session ordered by creation time
// descending. Soft-deleted attachments are excluded.
func (s *Service) ListBySession(ctx context.Context, sessionID string, limit int) ([]attachment.Attachment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.store.ListAttachmentsBySession(ctx, sessionID, limit)
}

// DeleteAttachment soft-deletes an attachment and removes the underlying file
// from the data directory. Idempotent: deleting an already-deleted attachment
// is a no-op. A soft-deleted attachment is never injected as prior context
// (fail-closed readability, ADR-005 §7).
func (s *Service) DeleteAttachment(ctx context.Context, id string) error {
	att, err := s.store.GetAttachmentForDeletion(ctx, id)
	if err != nil {
		return fmt.Errorf("get attachment for delete: %w", err)
	}
	if att == nil {
		return ErrAttachmentNotFound
	}
	// Soft-delete the record (sets deleted_at, records tombstone).
	if err := s.store.DeleteAttachment(ctx, id); err != nil {
		return fmt.Errorf("delete attachment record: %w", err)
	}
	// Cleanup was durably scheduled by the store transaction. Complete it now;
	// failures remain pending for startup reconciliation.
	if s.fileStorage != nil && att.FileRef != "" {
		if err := s.fileStorage.DeleteFile(ctx, att.FileRef); err != nil {
			return fmt.Errorf("delete attachment file: %w", err)
		}
		if err := s.store.CompleteAttachmentFileCleanup(ctx, att.FileRef); err != nil {
			return fmt.Errorf("complete attachment file cleanup: %w", err)
		}
	}
	return nil
}

// ReconcileFileCleanup retries durable cleanup work recorded in SQLite.
func (s *Service) ReconcileFileCleanup(ctx context.Context) error {
	refs, err := s.store.ListPendingAttachmentFileCleanup(ctx, 100)
	if err != nil {
		return fmt.Errorf("list pending attachment cleanup: %w", err)
	}
	var failures []error
	for _, ref := range refs {
		if s.fileStorage != nil {
			if err := s.fileStorage.DeleteFile(ctx, ref); err != nil {
				failures = append(failures, fmt.Errorf("delete pending attachment file %s: %w", ref, err))
				continue
			}
		}
		if err := s.store.CompleteAttachmentFileCleanup(ctx, ref); err != nil {
			failures = append(failures, fmt.Errorf("complete attachment cleanup %s: %w", ref, err))
		}
	}
	return errors.Join(failures...)
}

// ListReadableBySession returns succeeded, non-deleted attachments for a
// session. This is the read path used by chat.start to populate
// ContextEnvelope.AttachmentExcerpts (ADR-005 §7).
func (s *Service) ListReadableBySession(ctx context.Context, sessionID string, limit int) ([]attachment.Attachment, error) {
	atts, err := s.ListBySession(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]attachment.Attachment, 0, len(atts))
	for _, a := range atts {
		if a.IsReadable() {
			result = append(result, a)
		}
	}
	return result, nil
}

// ListVisionImagesBySession returns verified session-scoped image bytes. Image
// eligibility is intentionally separate from parsed-text readability.
func (s *Service) ListVisionImagesBySession(ctx context.Context, sessionID string) ([]VisionImage, error) {
	atts, err := s.ListBySession(ctx, sessionID, 20)
	if err != nil {
		return nil, err
	}
	if s.fileStorage == nil {
		return nil, nil
	}
	result := make([]VisionImage, 0, MaxVisionImages)
	total := 0
	for _, att := range atts {
		if !isVisionMIME(att.MIME) {
			continue
		}
		if len(result) >= MaxVisionImages || att.Size <= 0 || att.Size > MaxVisionImageBytes || total+int(att.Size) > MaxVisionBatchBytes {
			return nil, ErrImageBudget
		}
		data, readErr := s.fileStorage.ReadFile(ctx, att.FileRef)
		if readErr != nil {
			return nil, fmt.Errorf("read image attachment: %w", readErr)
		}
		digest := sha256.Sum256(data)
		expected, decodeErr := hex.DecodeString(att.SHA256)
		if decodeErr != nil || len(data) != int(att.Size) || subtle.ConstantTimeCompare(digest[:], expected) != 1 || !matchesVisionMIME(att.MIME, data) {
			return nil, ErrImageIntegrity
		}
		total += len(data)
		result = append(result, VisionImage{AttachmentID: att.ID, MIME: att.MIME, Data: data})
	}
	return result, nil
}

// GetVisionImage returns one verified image attachment by ID without
// enumerating unrelated historical session images.
func (s *Service) GetVisionImage(ctx context.Context, id, sessionID string) (VisionImage, error) {
	att, err := s.GetAttachment(ctx, id)
	if err != nil {
		return VisionImage{}, err
	}
	if att.SessionID != sessionID {
		return VisionImage{}, ErrScopeMismatch
	}
	if !isVisionMIME(att.MIME) {
		return VisionImage{}, ErrUnsupportedMIME
	}
	if s.fileStorage == nil || att.FileRef == "" || att.Size <= 0 || att.Size > MaxVisionImageBytes {
		return VisionImage{}, ErrImageIntegrity
	}
	data, err := s.fileStorage.ReadFile(ctx, att.FileRef)
	if err != nil {
		return VisionImage{}, fmt.Errorf("read image attachment: %w", err)
	}
	digest := sha256.Sum256(data)
	expected, decodeErr := hex.DecodeString(att.SHA256)
	if decodeErr != nil || len(data) != int(att.Size) || subtle.ConstantTimeCompare(digest[:], expected) != 1 || !matchesVisionMIME(att.MIME, data) {
		return VisionImage{}, ErrImageIntegrity
	}
	return VisionImage{AttachmentID: att.ID, MIME: att.MIME, Data: data}, nil
}

// PreviewWorkspaceImage returns verified image bytes for the workspace pane.
// Missing or non-image attachments omit bytes without failing the get.
func (s *Service) PreviewWorkspaceImage(ctx context.Context, id string) ([]byte, bool, error) {
	att, err := s.GetAttachment(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if !isVisionMIME(att.MIME) {
		return nil, false, nil
	}
	if s.fileStorage == nil || att.FileRef == "" || att.Size <= 0 || att.Size > MaxVisionImageBytes {
		return nil, false, nil
	}
	data, err := s.fileStorage.ReadFile(ctx, att.FileRef)
	if err != nil {
		return nil, false, nil
	}
	digest := sha256.Sum256(data)
	expected, decodeErr := hex.DecodeString(att.SHA256)
	if decodeErr != nil || len(data) != int(att.Size) || subtle.ConstantTimeCompare(digest[:], expected) != 1 || !matchesVisionMIME(att.MIME, data) {
		return nil, false, nil
	}
	return data, true, nil
}

func isVisionMIME(mime string) bool {
	return mime == "image/png" || mime == "image/jpeg" || mime == "image/webp"
}

func matchesVisionMIME(mime string, data []byte) bool {
	switch mime {
	case "image/png":
		return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	case "image/jpeg":
		return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
	case "image/webp":
		return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	default:
		return false
	}
}

// parse extracts text content from the file based on its MIME type.
// For the minimal secure closed-loop, only text-based MIME types are
// supported. Binary formats (PDF, DOCX, images) require a dedicated parser
// and are marked as UNSUPPORTED_MIME.
func (s *Service) parse(mime string, content []byte) (string, error) {
	if isVisionMIME(mime) {
		return "", nil
	}
	if !isSupportedMIME(mime) {
		return "", ErrUnsupportedMIME
	}
	if !utf8.Valid(content) {
		return "", ErrInvalidContent
	}
	return string(content), nil
}

// isSupportedMIME returns true for text-based MIME types that can be directly
// extracted as UTF-8 text without a dedicated parser.
func isSupportedMIME(mime string) bool {
	switch mime {
	case "text/plain", "text/markdown", "text/csv", "text/html",
		"application/json", "application/xml", "text/xml",
		"application/javascript", "text/javascript",
		"application/x-yaml", "text/yaml":
		return true
	default:
		// Allow any text/* MIME type.
		return len(mime) >= 5 && mime[:5] == "text/"
	}
}

// truncateUTF8 truncates s to at most maxBytes bytes, ensuring the result is
// valid UTF-8 (does not cut in the middle of a multi-byte rune).
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk back from maxBytes to find a valid rune boundary.
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}
