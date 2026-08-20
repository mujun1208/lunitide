// Package projectattachment defines phase-scoped project file attachments
// stored under the controlled data directory (project-attachments).
package projectattachment

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	MaxFileSize   = 10485760
	CategoryPhase = "phase_doc"
)

var (
	ErrNotFound      = errors.New("project attachment not found")
	ErrFileTooLarge  = errors.New("project attachment file too large")
	ErrInvalidPhase  = errors.New("project attachment phase invalid")
	ErrInvalidName   = errors.New("project attachment file name invalid")
	ErrInvalidMIME   = errors.New("project attachment mime type invalid")
	ErrInvalidDigest = errors.New("project attachment digest invalid")
)

// Attachment is a project phase document stored on disk with metadata in SQLite.
type Attachment struct {
	ID        string
	ProjectID string
	Phase     int
	Category  string
	FileName  string
	MimeType  string
	FilePath  string
	Digest    string
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Filter struct {
	ProjectID string
	Phase     int
}

func ValidPhase(phase int) bool {
	return phase >= 1 && phase <= 9
}

func NormalizeFileName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	base := filepath.Base(name)
	if base == "" || base == "." || base == ".." || base != name {
		return "", ErrInvalidName
	}
	if strings.ContainsAny(base, "/\\:\x00") {
		return "", ErrInvalidName
	}
	if len(base) > 260 {
		return "", ErrInvalidName
	}
	return base, nil
}

func NormalizeCategory(raw string) string {
	category := strings.TrimSpace(raw)
	if category == "" {
		return CategoryPhase
	}
	return category
}

func (a Attachment) Validate() error {
	if !canonicalULID(a.ID) {
		return errors.New("project attachment id is not a canonical ULID")
	}
	if !canonicalULID(a.ProjectID) {
		return errors.New("project attachment project_id is not a canonical ULID")
	}
	if !ValidPhase(a.Phase) {
		return ErrInvalidPhase
	}
	if a.Category == "" || len(a.Category) > 64 {
		return errors.New("project attachment category must be 1-64 bytes")
	}
	if _, err := NormalizeFileName(a.FileName); err != nil {
		return err
	}
	if a.MimeType == "" || len(a.MimeType) > 128 {
		return ErrInvalidMIME
	}
	if a.FilePath == "" || len(a.FilePath) > 512 {
		return errors.New("project attachment file_path must be 1-512 bytes")
	}
	if len(a.Digest) != 64 || !isHex(a.Digest) {
		return ErrInvalidDigest
	}
	if a.Version < 1 {
		return errors.New("project attachment version must be positive")
	}
	if a.CreatedAt.IsZero() || a.CreatedAt.Location() != time.UTC {
		return errors.New("project attachment created_at must be UTC")
	}
	if a.UpdatedAt.IsZero() || a.UpdatedAt.Location() != time.UTC {
		return errors.New("project attachment updated_at must be UTC")
	}
	return nil
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
