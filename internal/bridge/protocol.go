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
	EventGuidance         EventType = "guidance"
	EventTtsChunk         EventType = "tts_chunk"
	EventTalkAudio        EventType = "talk_audio"
	EventTalkTranscript   EventType = "talk_transcript"
	EventTalkTool         EventType = "talk_tool"
	EventTalkError        EventType = "talk_error"
	EventTalkEnded        EventType = "talk_ended"
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
	Guidance  *GuidanceEvent  `json:"guidance,omitempty"`
	Tts       *TtsChunkEvent  `json:"tts,omitempty"`
	Talk      *TalkEvent      `json:"talk,omitempty"`
}

// TalkEvent is one talk.* stream frame. Contract names talk.audio /
// talk.transcript / talk.tool / talk.error / talk.ended map to talk_*.
type TalkEvent struct {
	AudioBase64 string `json:"audioBase64,omitempty"`
	Mime        string `json:"mime,omitempty"`
	Text        string `json:"text,omitempty"`
	Role        string `json:"role,omitempty"`
	Name        string `json:"name,omitempty"`
	Code        string `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
}
type TerminalEvent struct {
	Data     string `json:"data,omitempty"`
	ExitCode uint32 `json:"exitCode,omitempty"`
}
type GuidanceEvent struct {
	Labels []string `json:"labels"`
	Digest string   `json:"digest"`
}

type TtsChunkEvent struct {
	AudioBase64 string `json:"audioBase64"`
	Mime        string `json:"mime"`
	Index       int    `json:"index"`
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
	MessageID     string `json:"messageId,omitempty"`
	PersistFailed bool   `json:"persistFailed,omitempty"`
	MemorySummary string `json:"memorySummary,omitempty"`
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

// Fail is the request-scoped form of Failure. Handlers otherwise repeat
// `bridge.Failure(request.ID, request.TraceID, …)` on every early return;
// `request.Fail(code, message, retryable)` threads the same id and trace id
// from the request that is already in scope, so a call site can never pair the
// wrong id with the wrong trace.
func (r Request) Fail(code, message string, retryable bool) Response {
	return Failure(r.ID, r.TraceID, code, message, retryable)
}

// Ok is the request-scoped form of Success.
func (r Request) Ok(payload any) Response {
	return Success(r.ID, payload)
}
