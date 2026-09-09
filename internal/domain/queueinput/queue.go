// Package queueinput holds the M10 queued-input domain model (migration
// 0074): durable session-scoped user supplements enqueued while a chat
// stream is running. Status moves are one-way (queued → injected or
// withdrawn); seq is monotonic per session and never recycled.
package queueinput

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/message"
)

type officeTaskKey struct{}

// WithOfficeTask binds queued input to one validated Office task. An empty ID
// explicitly selects ordinary conversation input, including legacy rows.
func WithOfficeTask(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, officeTaskKey{}, id)
}

func OfficeTaskID(ctx context.Context) string {
	id, _ := ctx.Value(officeTaskKey{}).(string)
	return id
}

var (
	ErrNotFound      = errors.New("queued message not found")
	ErrSettled       = errors.New("queued message already settled")
	ErrRequestReused = errors.New("queue request already settled or payload changed")
	ErrCapacity      = errors.New("queue capacity reached")
	ErrRateLimited   = errors.New("queue rate limited")
)

// Message is one queued user supplement row.
type Message struct {
	ID           string
	SessionID    string
	RunID        string // nullable join point for the future M4 run kernel
	OfficeTaskID string
	Seq          int64
	Payload      string
	Status       string
	Mark         string
	RequestID    string
	ConsumedAt   string
	CreatedAt    string
	UpdatedAt    string
}

// Status values (the only persisted set; UI grouping is a projection).
const (
	StatusQueued    = "queued"
	StatusInjected  = "injected"
	StatusWithdrawn = "withdrawn"
)

// Mark values: when the supplement should join the conversation.
const (
	MarkTurnBoundary = "turn_boundary"
	MarkWithApproval = "with_approval"
)

// Hard limits from the M10 wire contract (M10-QI-001/005/007).
const (
	MaxQueuedPerSession = 5
	MaxPayloadChars     = message.MaxRunes
	MaxPayloadBytes     = message.MaxBytes
	MaxPerMinute        = 10
)

// ValidStatusTransition enforces the one-way state machine: only queued
// rows may settle, and only once.
func ValidStatusTransition(from, to string) bool {
	switch from {
	case StatusQueued:
		return to == StatusInjected || to == StatusWithdrawn
	default:
		return false
	}
}

// ValidMark reports whether m is a known injection mark.
func ValidMark(m string) bool {
	return m == MarkTurnBoundary || m == MarkWithApproval
}
