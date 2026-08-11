package attachmentapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
)

// MaxFileSize is the maximum allowed attachment file size in bytes (10 MiB).
const MaxFileSize = 10485760

// MaxParsedTextBytes is the maximum stored parsed text size in bytes (1 MiB).
const MaxParsedTextBytes = 1048576

// Store defines the storage operations for attachments.
type Store interface {
	CreateAttachment(ctx context.Context, a attachment.Attachment) error
	GetAttachment(ctx context.Context, id string) (*attachment.Attachment, error)
	ListAttachmentsByProject(ctx context.Context, projectID string, limit int) ([]attachment.Attachment, error)
	ListAttachmentsBySession(ctx context.Context, sessionID string, limit int) ([]attachment.Attachment, error)
	UpdateParseResult(ctx context.Context, id string, status attachment.ParseStatus, errCode string, parsedText string, parsedTextBytes int64) error
	DeleteAttachment(ctx context.Context, id string) error
}

// Service provides attachment ingestion, parsing, and query operations.
// The Engine delegates to this service for secure file handling
// (ADR-005 §7: attachment isolation).
type Service struct {
	store      Store
	fileStorage FileStorage
	idFactory  func() string
	now        func() time.Time
}

// NewService creates a new attachment service.
func NewService(store Store, fileStorage FileStorage) *Service {
	return &Service{
		store:       store,
		fileStorage: fileStorage,
		idFactory:   func() string { return ulid.Make().String() },
		now:         func() time.Time { return time.Now().UTC() },
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
	att, err := s.store.GetAttachment(ctx, id)
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
	// Best-effort file cleanup. A missing file is not an error (idempotent).
	if s.fileStorage != nil && att.FileRef != "" {
		_ = s.fileStorage.DeleteFile(ctx, att.FileRef)
	}
	return nil
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

// parse extracts text content from the file based on its MIME type.
// For the minimal secure closed-loop, only text-based MIME types are
// supported. Binary formats (PDF, DOCX, images) require a dedicated parser
// and are marked as UNSUPPORTED_MIME.
func (s *Service) parse(mime string, content []byte) (string, error) {
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
