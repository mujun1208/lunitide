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
	ProjectTextQuotaBytes   int64 = 64 << 20
	WorkspaceTextQuotaBytes int64 = 256 << 20
)

type Role string
type Status string

const RoleUser Role = "user"
const StatusCompleted Status = "completed"

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

func NormalizeText(raw string) (string, error) {
	text := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	if strings.ContainsRune(text, '\x00') || !utf8.ValidString(text) || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > MaxRunes || len(text) > MaxBytes {
		return "", errors.New("message text must be valid UTF-8 within frozen limits")
	}
	return text, nil
}

func (m Message) Validate() error {
	text, err := NormalizeText(m.Text)
	if !CanonicalULID(m.ID) || !CanonicalULID(m.SessionID) || err != nil || text != m.Text || m.Role != RoleUser || m.Status != StatusCompleted || m.Sequence < 1 || m.Sequence > MaxSafeSequence || m.CreatedAt.IsZero() || m.CreatedAt.Location() != time.UTC {
		return errors.New("message invariant violation")
	}
	return nil
}
