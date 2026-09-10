package modelfit

import (
	"strings"
	"time"
)

type EffectClass string

const (
	EffectReadOnly            EffectClass = "read_only"
	EffectLocalReversible     EffectClass = "local_reversible"
	EffectRemoteTrackable     EffectClass = "remote_trackable"
	EffectRemoteUnknown       EffectClass = "remote_unknown"
	EffectDesktopInteractive  EffectClass = "desktop_interactive"
)

type OperationState string

const (
	OpPending   OperationState = "pending"
	OpRunning   OperationState = "running"
	OpSucceeded OperationState = "succeeded"
	OpPartial   OperationState = "partial"
	OpFailed    OperationState = "failed"
	OpUnknown   OperationState = "unknown"
	OpCancelled OperationState = "cancelled"
)

type ResumeAction string

const (
	ResumeShowExisting  ResumeAction = "show_existing"
	ResumeReread        ResumeAction = "reread"
	ResumeCompare       ResumeAction = "compare_and_continue"
	ResumeQueryExisting ResumeAction = "query_existing"
	ResumeVerifyUnknown ResumeAction = "verify_unknown"
	ResumeReobserve     ResumeAction = "reobserve"
	ResumeKeepStopped   ResumeAction = "keep_stopped"
)

type ToolOperation struct {
	ID                      string         `json:"id"`
	OwnerScope              string         `json:"ownerScope"`
	SessionID               string         `json:"sessionId,omitempty"`
	TurnID                  string         `json:"turnId,omitempty"`
	AutomationRunID         string         `json:"automationRunId,omitempty"`
	ToolName                string         `json:"toolName"`
	ToolVersion             string         `json:"toolVersion,omitempty"`
	TargetRef               string         `json:"targetRef,omitempty"`
	InputDigest             string         `json:"inputDigest"`
	EffectClass             EffectClass    `json:"effectClass"`
	State                   OperationState `json:"state"`
	ExpectedVersion         int            `json:"expectedVersion"`
	ExternalID              string         `json:"externalId,omitempty"`
	CheckpointRef           string         `json:"checkpointRef,omitempty"`
	EvidenceRef             string         `json:"evidenceRef,omitempty"`
	ArtifactRefs            []string       `json:"artifactRefs,omitempty"`
	Attempt                 int            `json:"attempt"`
	BudgetUsed              int64          `json:"budgetUsed,omitempty"`
	NextCheckAt             string         `json:"nextCheckAt,omitempty"`
	CreatedAt               time.Time      `json:"createdAt"`
	UpdatedAt               time.Time      `json:"updatedAt"`
	CancellationRequestedAt string         `json:"cancellationRequestedAt,omitempty"`
	ErrorKind               string         `json:"errorKind,omitempty"`
}

const (
	RecoverNativeContinue  = "native_continue"
	RecoverBusinessRebuild = "business_rebuild"
	RecoverVerifyReadonly  = "verify_readonly"
	RecoverKeepStopped     = "keep_stopped"
)

func ClassifiedRecoverAllowed(writer, decision string) bool {
	if writer != "sqlite" {
		return false
	}
	return decision == RecoverNativeContinue || decision == RecoverBusinessRebuild
}

func RecoveryDecision(env ContinuationEnvelope) string {
	if env.CancellationRequestedAt != "" {
		return RecoverKeepStopped
	}
	switch env.Completeness {
	case CompletenessNativeComplete:
		if env.CodecVersion == "" {
			return RecoverBusinessRebuild
		}
		return RecoverNativeContinue
	case CompletenessStructuredOnly:
		return RecoverBusinessRebuild
	default:
		return RecoverVerifyReadonly
	}
}

func ResumeMayExecute(op ToolOperation) bool {
	if ResumeDecisionFor(op) == ResumeKeepStopped || op.State == OpSucceeded || op.State == OpCancelled || op.State == OpUnknown {
		return false
	}
	switch op.EffectClass {
	case EffectReadOnly:
		return true
	case EffectLocalReversible:
		return op.State == OpPartial || op.State == OpFailed
	case EffectRemoteTrackable:
		return strings.TrimSpace(op.ExternalID) != ""
	default:
		return false
	}
}

func ResumeDecisionFor(op ToolOperation) ResumeAction {
	if op.CancellationRequestedAt != "" && op.State != OpSucceeded {
		return ResumeKeepStopped
	}
	return ResumeDecision(op.State, op.EffectClass)
}

func ResumeDecision(state OperationState, effect EffectClass) ResumeAction {
	if state == OpCancelled {
		return ResumeKeepStopped
	}
	if state == OpSucceeded {
		return ResumeShowExisting
	}
	if state == OpUnknown {
		return ResumeVerifyUnknown
	}
	switch effect {
	case EffectReadOnly:
		return ResumeReread
	case EffectLocalReversible:
		return ResumeCompare
	case EffectRemoteTrackable:
		return ResumeQueryExisting
	case EffectDesktopInteractive:
		return ResumeReobserve
	default:
		return ResumeVerifyUnknown
	}
}

func EffectClassForTool(name string) EffectClass {
	switch name {
	case "workspace.read", "workspace.list", "workspace.search", "excel.parse",
		"memory.search", "memory.get", "web.fetch", "web.search", "video.understand",
		"weather.get", "datasource.query", "files.status":
		return EffectReadOnly
	case "workspace.write", "workspace.edit", "todo.write", "excel.gen", "docx.gen",
		"pptx.gen", "html.gen", "files.plan", "files.apply", "files.undo",
		"data.process", "image.batch", "pdf.copy":
		return EffectLocalReversible
	case "media.generate", "image.generate", "video.generate":
		return EffectRemoteTrackable
	case "desktop.open", "desktop.quit", "desktop.browse", "desktop.type",
		"media.play", "computer.act":
		return EffectDesktopInteractive
	}
	if len(name) >= 3 && name[:3] == "cc." {
		return EffectDesktopInteractive
	}
	return EffectRemoteUnknown
}

func (op ToolOperation) RequestCancel() (ToolOperation, error) {
	op.CancellationRequestedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if op.State == OpPending || op.State == OpRunning {
		op.State = OpCancelled
	}
	return op, nil
}
