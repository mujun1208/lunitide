package deliverable

import (
	"errors"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft      Status = "draft"
	StatusReview     Status = "review"
	StatusApproved   Status = "approved"
	StatusImmutable  Status = "immutable"
)

var (
	ErrNotFound          = errors.New("project deliverable not found")
	ErrGateLocked        = errors.New("deliverable gate is locked")
	ErrVersionConflict   = errors.New("deliverable version conflict")
	ErrInvalidDocument   = errors.New("document type is invalid")
)

type ProjectDeliverable struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	Phase             int       `json:"phase"`
	DocumentType      string    `json:"documentType"`
	Title             string    `json:"title"`
	TemplateID        string    `json:"templateId,omitempty"`
	AttachmentID      string    `json:"attachmentId,omitempty"`
	Status            Status    `json:"status"`
	GateConfirmations int       `json:"gateConfirmations"`
	Digest            string    `json:"digest"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	Version           int64     `json:"version"`
}

type Filter struct {
	ProjectID string
	Phase     int
	Status    Status
}

func ValidStatus(s Status) bool {
	switch s {
	case StatusDraft, StatusReview, StatusApproved, StatusImmutable:
		return true
	default:
		return false
	}
}

func ValidPhase(phase int) bool {
	return phase >= 1 && phase <= 9
}

func NormalizeTitle(raw string) (string, error) {
	title := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if title == "" || len([]rune(title)) > 200 {
		return "", errors.New("deliverable title must contain 1 to 200 characters")
	}
	return title, nil
}

func CanConfirmGate(d ProjectDeliverable) bool {
	return d.Status != StatusImmutable && d.GateConfirmations < 3
}
