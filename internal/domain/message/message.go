package message

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	MaxSafeSequence         int64 = 9007199254740991
	MaxRunes                      = 2048
	MaxBytes                      = 8192
	MaxRunesAssistant             = 16384
	MaxBytesAssistant             = 65536
	ProjectTextQuotaBytes   int64 = 64 << 20
	WorkspaceTextQuotaBytes int64 = 256 << 20
)

type Role string
type Status string

const RoleUser Role = "user"
const RoleAssistant Role = "assistant"
const RoleTool Role = "tool"
const StatusCompleted Status = "completed"
const StatusFailed Status = "failed"

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Role      Role      `json:"role"`
	Status    Status    `json:"status"`
	Sequence  int64     `json:"sequence"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

func CanonicalULID(value string) bool {
	u, err := ulid.ParseStrict(value)
	return err == nil && u.String() == value && value[0] <= '7'
}

// normalizeLineEndings converts CRLF and bare CR to LF without trimming.
func normalizeLineEndings(raw string) string {
	return strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
}

func NormalizeText(raw string) (string, error) {
	text := normalizeLineEndings(raw)
	if strings.ContainsRune(text, '\x00') || !utf8.ValidString(text) || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > MaxRunes || len(text) > MaxBytes {
		return "", errors.New("message text must be valid UTF-8 within frozen limits")
	}
	return text, nil
}

// NormalizeAssistantText normalizes and validates assistant message text.
// Assistant text allows 1..16,384 Unicode code points and at most 65,536 UTF-8
// bytes, wider than user text because model outputs are typically longer.
func NormalizeAssistantText(raw string) (string, error) {
	text := normalizeLineEndings(raw)
	if strings.ContainsRune(text, '\x00') || !utf8.ValidString(text) || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > MaxRunesAssistant || len(text) > MaxBytesAssistant {
		return "", errors.New("assistant message text must be valid UTF-8 within frozen limits")
	}
	return text, nil
}

func (m Message) Validate() error {
	if !CanonicalULID(m.ID) || !CanonicalULID(m.SessionID) || m.Sequence < 1 || m.Sequence > MaxSafeSequence || m.CreatedAt.IsZero() || m.CreatedAt.Location() != time.UTC {
		return errors.New("message invariant violation")
	}
	switch m.Role {
	case RoleUser:
		text, err := NormalizeText(m.Text)
		if err != nil || text != m.Text || m.Status != StatusCompleted {
			return errors.New("message invariant violation")
		}
	case RoleAssistant, RoleTool:
		text, err := NormalizeAssistantText(m.Text)
		if err != nil || text != m.Text || m.Status != StatusCompleted {
			return errors.New("message invariant violation")
		}
	default:
		return errors.New("message invariant violation")
	}
	return nil
}
