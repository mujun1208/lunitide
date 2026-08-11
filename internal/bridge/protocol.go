package bridge

import (
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
)

type Request struct {
	Version        string          `json:"v"`
	Kind           string          `json:"kind"`
	ID             string          `json:"id"`
	TraceID        string          `json:"traceId"`
	Method         string          `json:"method"`
	SentAt         time.Time       `json:"sentAt"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	DeadlineMS     int             `json:"deadlineMs"`
}

type Error struct {
	Code          string         `json:"code"`
	Message       string         `json:"message"`
	Retryable     bool           `json:"retryable"`
	Details       map[string]any `json:"details,omitempty"`
	CorrelationID string         `json:"correlationId"`
}

type Response struct {
	Version   string `json:"v"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	RequestID string `json:"requestId"`
	OK        bool   `json:"ok"`
	Payload   any    `json:"payload,omitempty"`
	Error     *Error `json:"error,omitempty"`
}

type EventType string

const (
	EventDelta     EventType = "delta"
	EventUsage     EventType = "usage"
	EventCompleted EventType = "completed"
	EventCancelled EventType = "cancelled"
	EventFailed    EventType = "failed"
)

type Event struct {
	Version   string          `json:"v"`
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	StreamID  string          `json:"streamId"`
	Sequence  uint64          `json:"sequence"`
	Type      EventType       `json:"type"`
	Delta     *DeltaEvent     `json:"delta,omitempty"`
	Usage     *UsageEvent     `json:"usage,omitempty"`
	Completed *CompletedEvent `json:"completed,omitempty"`
	Error     *StreamError    `json:"error,omitempty"`
}
type DeltaEvent struct {
	Text string `json:"text"`
}
type UsageEvent struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}
type CompletedEvent struct {
	MessageID string `json:"messageId,omitempty"`
}
type StreamError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func Success(requestID string, payload any) Response {
	return Response{Version: Version, Kind: "response", ID: ulid.Make().String(), RequestID: requestID, OK: true, Payload: payload}
}

func Failure(requestID, traceID, code, message string, retryable bool) Response {
	return Response{Version: Version, Kind: "response", ID: ulid.Make().String(), RequestID: requestID, OK: false, Error: &Error{Code: code, Message: message, Retryable: retryable, CorrelationID: traceID}}
}
