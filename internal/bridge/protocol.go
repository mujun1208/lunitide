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
	EventDelta       EventType = "delta"
	EventThinking    EventType = "thinking"
	EventUsage       EventType = "usage"
	EventToolStarted EventType = "tool_started"
	// EventToolOutput streams incremental stdout/stderr lines of a running
	// tool (P1-2): Summary carries one bounded output chunk between
	// tool_started and tool_completed. Purely additive; consumers that do
	// not know the type ignore it and see only the legacy start/complete
	// pair.
	EventToolOutput       EventType = "tool_output"
	EventToolCompleted    EventType = "tool_completed"
	EventApprovalRequired EventType = "approval_required"
	EventCompleted        EventType = "completed"
	EventCancelled        EventType = "cancelled"
	EventFailed           EventType = "failed"
	EventTerminalOutput   EventType = "terminal_output"
	EventTerminalExit     EventType = "terminal_exit"
)

type Event struct {
	Version   string          `json:"v"`
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	StreamID  string          `json:"streamId"`
	Sequence  uint64          `json:"sequence"`
	Type      EventType       `json:"type"`
	Delta     *DeltaEvent     `json:"delta,omitempty"`
	Thinking  *ThinkingEvent  `json:"thinking,omitempty"`
	Usage     *UsageEvent     `json:"usage,omitempty"`
	Completed *CompletedEvent `json:"completed,omitempty"`
	Error     *StreamError    `json:"error,omitempty"`
	Tool      *ToolEvent      `json:"tool,omitempty"`
	Terminal  *TerminalEvent  `json:"terminal,omitempty"`
}
type TerminalEvent struct {
	Data     string `json:"data,omitempty"`
	ExitCode uint32 `json:"exitCode,omitempty"`
}
type DeltaEvent struct {
	Text string `json:"text"`
}
type ThinkingEvent struct {
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
type ToolEvent struct {
	CallID     string         `json:"callId"`
	Name       string         `json:"name"`
	ArgsDigest string         `json:"argsDigest"`
	Summary    string         `json:"summary,omitempty"`
	Artifact   *ArtifactEvent `json:"artifact,omitempty"`
}

type ArtifactEvent struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Content string `json:"content"`
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
