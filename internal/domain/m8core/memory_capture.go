package m8core

import "time"

const (
	MemoryCaptureHighWatermark int64 = 10000
	MemoryCaptureLowWatermark  int64 = 8000
	MemoryCaptureScanLimit           = 100
	MemoryCaptureMaxAttempts         = 5
)

var MemoryCaptureLease = 60 * time.Second

var MemoryCaptureRetry = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
	120 * time.Second,
	600 * time.Second,
}

// MemoryCaptureJob is one durable extract unit keyed by source revision.
type MemoryCaptureJob struct {
	JobID           string
	SubjectID       string
	SourceMessageID string
	SourceRevision  string
	SourceDigest    string
	Priority        int64
	State           string
	Attempt         int64
	NextAttemptAt   string
	CursorJSON      string
	LeaseOwner      string
	LeaseUntil      string
	HeartbeatAt     string
	Fence           int64
	ErrorCode       string
	CreatedAt       string
	UpdatedAt       string
}

// MemoryCaptureCursor is the last scanned user-turn watermark per subject.
type MemoryCaptureCursor struct {
	SubjectID       string
	SourceMessageID string
	SourceRevision  string
	CursorJSON      string
	UpdatedAt       string
}

// MemoryCaptureSource is a persisted user message eligible for capture.
type MemoryCaptureSource struct {
	MessageID string
	SessionID string
	Revision  string
	Digest    string
	Text      string
}
