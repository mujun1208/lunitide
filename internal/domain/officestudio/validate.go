package officestudio

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

func ValidID(s string) bool {
	v, err := ulid.ParseStrict(s)
	return err == nil && v.String() == s
}

func ValidDigest(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func ValidJSON(v json.RawMessage, limit int) bool {
	return len(v) <= limit && json.Valid(v)
}

func ValidStatus(v string) bool {
	switch v {
	case "draft", "queued", "planning", "running", "validating", "succeeded", "waiting_input", "waiting_approval", "cancelling", "cancelled", "failed", "interrupted":
		return true
	}
	return false
}

func ValidTransition(from, to string) bool {
	if from == to {
		return true
	}
	if !ValidStatus(from) || !ValidStatus(to) {
		return false
	}
	if to == "queued" {
		return from == "draft" || from == "succeeded" || from == "failed" || from == "cancelled" || from == "interrupted" || from == "waiting_input" || from == "waiting_approval"
	}
	if to == "failed" || to == "interrupted" || to == "cancelling" || to == "cancelled" {
		return from != "succeeded" && from != "cancelled" && from != "failed"
	}
	switch from {
	case "draft":
		return to == "planning" || to == "running"
	case "queued":
		return to == "planning" || to == "running"
	case "planning":
		return to == "running" || to == "waiting_input" || to == "waiting_approval"
	case "running":
		return to == "validating" || to == "succeeded" || to == "waiting_input" || to == "waiting_approval"
	case "validating":
		return to == "succeeded" || to == "running"
	case "waiting_input", "waiting_approval":
		return to == "running" || to == "planning"
	case "cancelling":
		return to == "cancelled"
	}
	return false
}

func (t Task) Validate() error {
	if !ValidID(t.ID) || !ValidID(t.SessionID) || strings.TrimSpace(t.Title) == "" || utf8.RuneCountInString(t.Title) > 200 || utf8.RuneCountInString(t.Goal) > 16000 || !ValidStatus(t.Status) || t.Revision < 1 || !ValidJSON(t.Checkpoint, 1<<20) || t.CreatedAt.IsZero() || t.UpdatedAt.Before(t.CreatedAt) {
		return ErrInvalid
	}
	if t.RunID != "" && !ValidID(t.RunID) || t.StartMessageID != "" && !ValidID(t.StartMessageID) {
		return ErrInvalid
	}
	return nil
}

func (v Version) Validate() error {
	if !ValidID(v.ID) || !ValidID(v.TaskID) || v.ArtifactID == "" || len(v.ArtifactID) > 256 || v.Size < 0 || !ValidDigest(v.SHA256) || v.CreatedAt.IsZero() || !ValidJSON(v.Spec, 4<<20) || !ValidJSON(v.Index, 4<<20) || v.ContentRef == "" || len(v.ContentRef) > 1024 || len(v.MediaType) < 3 || len(v.MediaType) > 256 {
		return ErrInvalid
	}
	if v.Name == "" || utf8.RuneCountInString(v.Name) > 256 || strings.ContainsAny(v.Name, "/\\\x00\r\n") {
		return ErrInvalid
	}
	if v.BaseVersionID != "" && !ValidID(v.BaseVersionID) {
		return ErrInvalid
	}
	if v.ContentMode != "managed" && v.ContentMode != "imported" {
		return ErrInvalid
	}
	switch v.Kind {
	case "pptx", "docx", "xlsx", "pdf":
	default:
		return ErrInvalid
	}
	return nil
}
