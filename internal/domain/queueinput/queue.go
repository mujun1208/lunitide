// Package queueinput holds the M10 queued-input domain model (migration
// 0074): durable session-scoped user supplements enqueued while a chat
// stream is running. Status moves are one-way (queued → injected or
// withdrawn); seq is monotonic per session and never recycled.
package queueinput

// Message is one queued user supplement row.
type Message struct {
	ID         string
	SessionID  string
	RunID      string // nullable join point for the future M4 run kernel
	Seq        int64
	Payload    string
	Status     string
	Mark       string
	RequestID  string
	ConsumedAt string
	CreatedAt  string
	UpdatedAt  string
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
	MaxPayloadChars     = 8000
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
