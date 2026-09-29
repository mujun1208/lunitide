// Package attachment defines the Attachment domain entity.
//
// An Attachment is a user-supplied file ingested into a project (and
// optionally linked to a session) whose parsed text becomes available
// as untrusted prior context for model input (ADR-005 §7: attachment
// isolation). Each attachment is owned by a project (required) and
// optionally linked to a session. File content lives in a controlled
// data directory (file_ref); only metadata and parsed text live in
// SQLite.
//
// Parse status is tracked independently so a single attachment failure
// does not block the conversation. A deleted or non-succeeded attachment
// is never injected as prior context (fail-closed readability).
package attachment

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// ParseStatus is the lifecycle state of attachment text extraction.
type ParseStatus string

const (
	StatusPending   ParseStatus = "pending"
	StatusParsing   ParseStatus = "parsing"
	StatusSucceeded ParseStatus = "succeeded"
	StatusFailed    ParseStatus = "failed"
)

// Attachment is a user-supplied file with parsed text used as prior context.
type Attachment struct {
	ID              string
	ProjectID       string
	SessionID       string // may be empty
	FileRef         string
	OriginalName    string
	MIME            string
	Size            int64
	SHA256          string
	ParseStatus     ParseStatus
	ParseErrorCode  string
	ParsedText      string
	ParsedTextBytes int64
	CreatedAt       time.Time
	DeletedAt       *time.Time
}

// Validate checks invariants for an attachment.
func (a Attachment) Validate() error {
	if !canonicalULID(a.ID) {
		return errors.New("attachment id is not a canonical ULID")
	}
	if !canonicalULID(a.ProjectID) {
		return errors.New("attachment project_id is not a canonical ULID")
	}
	if a.SessionID != "" && !canonicalULID(a.SessionID) {
		return errors.New("attachment session_id is not a canonical ULID")
	}
	if a.FileRef == "" || len(a.FileRef) > 512 {
		return errors.New("attachment file_ref must be 1-512 bytes")
	}
	if a.OriginalName == "" || len(a.OriginalName) > 256 {
		return errors.New("attachment original_name must be 1-256 bytes")
	}
	if a.MIME == "" || len(a.MIME) > 128 {
		return errors.New("attachment mime must be 1-128 bytes")
	}
	if a.Size < 0 || a.Size > 10485760 {
		return errors.New("attachment size out of range")
	}
	if len(a.SHA256) != 64 || !isHex(a.SHA256) {
		return errors.New("attachment sha256 must be 64 hex chars")
	}
	switch a.ParseStatus {
	case StatusPending, StatusParsing, StatusSucceeded, StatusFailed:
	default:
		return errors.New("attachment parse_status invalid")
	}
	if len(a.ParseErrorCode) > 64 {
		return errors.New("attachment parse_error_code exceeds 64 bytes")
	}
	if len(a.ParsedText) > 1048576 {
		return errors.New("attachment parsed_text exceeds 1MB")
	}
	if a.ParsedTextBytes < 0 || a.ParsedTextBytes > 1048576 {
		return errors.New("attachment parsed_text_bytes out of range")
	}
	if a.CreatedAt.IsZero() || a.CreatedAt.Location() != time.UTC {
		return errors.New("attachment created_at must be UTC")
	}
	if a.DeletedAt != nil && a.DeletedAt.Location() != time.UTC {
		return errors.New("attachment deleted_at must be UTC")
	}
	return nil
}

// IsReadable returns true when the attachment may be injected as prior
// context: it must not be deleted and its parse status must be succeeded.
// This is the fail-closed readability check (ADR-005 §7).
func (a Attachment) IsReadable() bool {
	return a.DeletedAt == nil && a.ParseStatus == StatusSucceeded && a.ParsedText != ""
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
