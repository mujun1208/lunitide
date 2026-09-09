// Package officestudio defines durable Office task and immutable file contracts.
// Execution, validation and user acceptance are deliberately independent.
package officestudio

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("OFFICE_NOT_FOUND")
	ErrConflict = errors.New("OFFICE_VERSION_CONFLICT")
	ErrInvalid  = errors.New("OFFICE_INVALID_INPUT")
	ErrScope    = errors.New("OFFICE_SCOPE_MISMATCH")
)

type scopeKey struct{}

// WithScope is called only by the verified desktop identity boundary. Payloads
// must never select their own organization. Empty means the personal partition.
func WithScope(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, scopeKey{}, orgID)
}

func Scope(ctx context.Context) string {
	v, _ := ctx.Value(scopeKey{}).(string)
	return v
}

type Task struct {
	ID             string          `json:"id"`
	SessionID      string          `json:"sessionId"`
	Title          string          `json:"title"`
	Goal           string          `json:"goal"`
	Status         string          `json:"status"`
	Revision       int64           `json:"revision"`
	RunID          string          `json:"runId,omitempty"`
	StartMessageID string          `json:"startMessageId,omitempty"`
	Checkpoint     json.RawMessage `json:"checkpoint"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type Version struct {
	ID            string          `json:"id"`
	TaskID        string          `json:"taskId"`
	ArtifactID    string          `json:"artifactId"`
	VersionNo     int64           `json:"versionNo"`
	Kind          string          `json:"kind"`
	Name          string          `json:"name"`
	ContentRef    string          `json:"contentRef"`
	SHA256        string          `json:"sha256"`
	Size          int64           `json:"size"`
	MediaType     string          `json:"mediaType"`
	ContentMode   string          `json:"contentMode"`
	BaseVersionID string          `json:"baseVersionId,omitempty"`
	Spec          json.RawMessage `json:"spec"`
	Index         json.RawMessage `json:"index"`
	Quality       string          `json:"quality"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type Head struct {
	TaskID            string `json:"taskId"`
	ArtifactID        string `json:"artifactId"`
	LatestVersionID   string `json:"latestVersionId"`
	AcceptedVersionID string `json:"acceptedVersionId,omitempty"`
	Revision          int64  `json:"revision"`
}

// PublishRequest commits a file already durably staged by the file service.
// ExpectedHeadRevision is zero for a new artifact, otherwise a CAS revision.
// BaseVersionID may select historical content; the expected head still guards
// publication against an intervening write. A retry key binds the full payload.
type PublishRequest struct {
	Version              Version
	ExpectedHeadRevision int64
	IdempotencyKey       string
	CreatedBy            string
	Evidence             []EvidenceEdge
	BlobLeaseID          string // internal durable-file receipt; not part of request idempotency
}

type Check struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Detail   string `json:"detail,omitempty"`
}

type Validation struct {
	ID          string          `json:"id"`
	VersionID   string          `json:"versionId"`
	SHA256      string          `json:"sha256"`
	Validator   string          `json:"validator"`
	Quality     string          `json:"quality"`
	Checks      []Check         `json:"checks"`
	Evidence    json.RawMessage `json:"evidence"`
	CreatedAt   time.Time       `json:"createdAt"`
	BlobLeaseID string          `json:"-"`
}

// QualityFor never interprets an empty check list as a completed validation.
func QualityFor(checks []Check) string {
	if len(checks) == 0 {
		return "unverified"
	}
	partial, pending := false, false
	for _, c := range checks {
		if c.Status == "failed" && c.Required {
			return "blocked"
		}
		if c.Status == "pending" {
			pending = true
		}
		if c.Status != "passed" {
			partial = true
		}
	}
	if pending {
		return "checking"
	}
	if partial {
		return "partial"
	}
	return "passed"
}

type StepReceipt struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"taskId"`
	RunID          string          `json:"runId"`
	StepKey        string          `json:"stepKey"`
	IdempotencyKey string          `json:"idempotencyKey"`
	InputDigest    string          `json:"inputDigest"`
	State          string          `json:"state"`
	Result         json.RawMessage `json:"result"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type EvidenceEdge struct {
	ID              string          `json:"id"`
	TaskID          string          `json:"taskId"`
	SourceVersionID string          `json:"sourceVersionId"`
	TargetVersionID string          `json:"targetVersionId"`
	SourceNode      string          `json:"sourceNode"`
	TargetNode      string          `json:"targetNode"`
	Metric          json.RawMessage `json:"metric"`
	CreatedAt       time.Time       `json:"createdAt"`
}

type Event struct {
	ID        string          `json:"id"`
	TaskID    string          `json:"taskId"`
	VersionID string          `json:"versionId,omitempty"`
	Action    string          `json:"action"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"createdAt"`
}

type Store interface {
	CreateOfficeTask(context.Context, Task, string) (Task, error)
	FindOfficeTaskByKey(context.Context, string, string) (Task, error)
	GetOfficeTask(context.Context, string) (Task, error)
	ListOfficeTasks(context.Context, string, int) ([]Task, error)
	UpdateOfficeTask(context.Context, Task, int64) (Task, error)
	PublishOfficeVersion(context.Context, PublishRequest) (Version, error)
	GetOfficeVersion(context.Context, string) (Version, error)
	ListOfficeVersions(context.Context, string, string) ([]Version, error)
	ListOfficeHeads(context.Context, string) ([]Head, error)
	AcceptOfficeVersion(context.Context, string, string, string, int64) (Head, error)
	AddOfficeValidation(context.Context, Validation) (Validation, error)
	ListOfficeValidations(context.Context, string) ([]Validation, error)
	AppendOfficeStepReceipt(context.Context, StepReceipt) error
	ListOfficeStepReceipts(context.Context, string, string) ([]StepReceipt, error)
	AddOfficeEvidenceEdge(context.Context, EvidenceEdge) error
	ListOfficeEvidenceEdges(context.Context, string) ([]EvidenceEdge, error)
	MarkOfficeDependentsStale(context.Context, string) (int64, error)
	ListOfficeEvents(context.Context, string, int) ([]Event, error)
}

// OperationFinisher atomically persists task outcome and its terminal receipt.
// It prevents a crash between these writes from leaving a false completion.
type OperationFinisher interface {
	FinishOfficeTask(context.Context, Task, int64, StepReceipt) (Task, error)
}
