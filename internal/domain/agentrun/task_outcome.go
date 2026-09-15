package agentrun

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	TaskSucceeded      = "succeeded"
	TaskIncomplete     = "incomplete"
	TaskPaused         = "paused"
	TaskFailed         = "failed"
	TaskCancelled      = "cancelled"
	TaskOutcomeUnknown = "outcome_unknown"

	CompletionVerified    = "verified"
	CompletionNotRequired = "not_required"
	CompletionUnverified  = "unverified"

	StreamRunning   = "running"
	StreamEnded     = "ended"
	StreamFailed    = "failed"
	StreamCancelled = "cancelled"

	ReasonMissingArtifact = "missing_artifact"
)

// ArtifactSnapshot is the immutable raw-file identity used for delivery
// verification. SHA256 and Bytes are always the complete original bytes,
// never paginated workspace.read text or an Office extract.
type ArtifactSnapshot struct {
	ID, Path, SHA256, ContentRef string
	Bytes                        int64
	SourceIdentity               string
}

type AcceptanceCheck struct {
	ID, Kind, ExpectedDigest string
	Required                 bool
	Predicate                json.RawMessage
}

type StepSpec struct {
	ID           string
	GoalRevision int64
	Required     bool
	DependsOn    []string
	Checks       []AcceptanceCheck
}

type CheckResult struct{ ID, Status, ReasonCode, EvidenceRef string }

type StepOutcome struct {
	StepID, AttemptID                       string
	GoalRevision                            int64
	State, ReasonCode                       string
	ReceiptRefs, EvidenceRefs, ArtifactRefs []string
	CheckResults                            []CheckResult
	Claim, VerifierRevision                 string
}

type TaskOutcome struct {
	TaskID                                     string
	GoalRevision, Version                      int64
	State, Completion, ReasonCode              string
	RequiredSteps, PassedSteps, RemainingSteps []string
	EvidenceRefs, ArtifactRefs                 []string
}

type TrustedReceipt struct {
	ID, OwnerScope, TaskID, RunID, StepID, AttemptID string
	GoalRevision                                     int64
	ToolName, ToolRevision, ArgumentsDigest          string
	State, OutputSnapshotRef, CreatedAt              string
	ArtifactRefs                                     []string
}

type VerificationScope struct {
	OwnerScope, TaskID string
	GoalRevision       int64
}

type CheckEvidence struct {
	ID, CheckID, ValidatorID, ValidatorRevision, SubjectSHA256 string
	OwnerScope, TaskID, ReceiptRef, Status, PredicateDigest    string
	GoalRevision                                               int64
}

type ReceiptResolver interface {
	ResolveReceipt(context.Context, VerificationScope, string) (TrustedReceipt, error)
	ResolveArtifact(context.Context, VerificationScope, string) (ArtifactSnapshot, error)
	ResolveCheckEvidence(context.Context, VerificationScope, string) (CheckEvidence, error)
}

type TaskOutcomeView struct {
	Spinner, GreenComplete, ReplyEnded, TaskComplete bool
	Tone, Label                                      string
}

type CompletedEvent struct {
	TaskID         string
	GoalRevision   int64
	OutcomeVersion int64
	Outcome        TaskOutcome
}

type OutcomeLedger struct {
	Current TaskOutcome
	History []TaskOutcome
}

func ApplyCompletedEvent(led OutcomeLedger, ev CompletedEvent) OutcomeLedger {
	if led.Current.TaskID != "" && ev.GoalRevision < led.Current.GoalRevision {
		led.History = append(led.History, ev.Outcome)
		return led
	}
	if led.Current.TaskID != "" && ev.GoalRevision == led.Current.GoalRevision && ev.OutcomeVersion <= led.Current.Version {
		return led
	}
	if led.Current.TaskID != "" && ev.GoalRevision > led.Current.GoalRevision {
		led.History = append(led.History, led.Current)
	}
	out := ev.Outcome
	if out.TaskID == "" {
		out.TaskID = ev.TaskID
	}
	out.GoalRevision = ev.GoalRevision
	out.Version = ev.OutcomeVersion
	led.Current = out
	return led
}

func EvaluateTask(ctx context.Context, scope VerificationScope, specs []StepSpec, outcomes []StepOutcome, receipts ReceiptResolver) (TaskOutcome, error) {
	latest := map[string]StepOutcome{}
	for _, out := range outcomes {
		if out.GoalRevision != scope.GoalRevision {
			continue
		}
		prev, ok := latest[out.StepID]
		if !ok || out.AttemptID >= prev.AttemptID {
			latest[out.StepID] = out
		}
	}

	result := TaskOutcome{
		TaskID:       scope.TaskID,
		GoalRevision: scope.GoalRevision,
		Version:      1,
		State:        TaskIncomplete,
		Completion:   CompletionUnverified,
		ReasonCode:   ReasonMissingArtifact,
	}
	var artifactRefs []string
	for _, spec := range specs {
		if spec.GoalRevision != 0 && spec.GoalRevision != scope.GoalRevision {
			continue
		}
		if spec.Required {
			result.RequiredSteps = append(result.RequiredSteps, spec.ID)
		}
		out, ok := latest[spec.ID]
		passed, refs := stepPassed(ctx, scope, spec, out, ok, receipts)
		artifactRefs = append(artifactRefs, refs...)
		if !spec.Required {
			continue
		}
		if passed {
			result.PassedSteps = append(result.PassedSteps, spec.ID)
			continue
		}
		result.RemainingSteps = append(result.RemainingSteps, spec.ID)
	}
	result.ArtifactRefs = artifactRefs
	if len(result.RemainingSteps) == 0 && len(result.RequiredSteps) > 0 {
		result.State = TaskSucceeded
		result.Completion = CompletionVerified
		result.ReasonCode = ""
	}
	return result, nil
}

func stepPassed(ctx context.Context, scope VerificationScope, spec StepSpec, out StepOutcome, ok bool, receipts ReceiptResolver) (bool, []string) {
	if !ok || out.State != "succeeded" && out.State != TaskSucceeded {
		return false, nil
	}
	if out.State == "skipped" {
		return false, out.ArtifactRefs
	}
	refs := append([]string{}, out.ArtifactRefs...)
	needsArtifact := false
	var l0Check *AcceptanceCheck
	for i := range spec.Checks {
		check := spec.Checks[i]
		if !check.Required {
			continue
		}
		if check.Kind == "artifact" {
			needsArtifact = true
		}
		if check.Kind == "l0" || check.Kind == "receipt" {
			c := check
			l0Check = &c
		}
	}
	if l0Check != nil {
		if len(out.ReceiptRefs) == 0 {
			return false, refs
		}
		for _, ref := range out.ReceiptRefs {
			if receipts == nil {
				return false, refs
			}
			rec, err := receipts.ResolveReceipt(ctx, scope, ref)
			if err != nil {
				return false, refs
			}
			if rec.OwnerScope != "" && rec.OwnerScope != scope.OwnerScope {
				return false, refs
			}
			if rec.TaskID != "" && rec.TaskID != scope.TaskID {
				return false, refs
			}
			if rec.GoalRevision != 0 && rec.GoalRevision != scope.GoalRevision {
				return false, refs
			}
			if l0Check.ExpectedDigest != "" && rec.ArgumentsDigest != "" && rec.ArgumentsDigest != l0Check.ExpectedDigest {
				return false, refs
			}
		}
	}
	if receipts != nil {
		for _, evRef := range out.EvidenceRefs {
			ev, err := receipts.ResolveCheckEvidence(ctx, scope, evRef)
			if err == nil && ev.ValidatorID == "model" {
				return false, refs
			}
		}
	}
	if !needsArtifact {
		return true, refs
	}
	if len(refs) == 0 {
		return false, refs
	}
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return false, refs
		}
		art, err := receipts.ResolveArtifact(ctx, scope, ref)
		if err != nil || strings.TrimSpace(art.Path) == "" {
			return false, refs
		}
	}
	return true, refs
}

func PresentTaskOutcome(outcome TaskOutcome, stream string, artifactPaths []string) TaskOutcomeView {
	view := TaskOutcomeView{Tone: "partial", Label: "部分完成"}
	switch stream {
	case StreamEnded, StreamFailed, StreamCancelled:
		view.Spinner = false
		view.ReplyEnded = true
	case StreamRunning:
		view.Spinner = true
	default:
		view.Spinner = stream != "" && stream != StreamEnded
	}
	if stream == StreamFailed {
		view.Tone = "failed"
		view.Label = "失败"
	}
	if hasMissingArtifact(outcome, artifactPaths) {
		view.GreenComplete = false
		view.TaskComplete = false
		if outcome.State == TaskFailed {
			view.Tone = "failed"
			view.Label = "失败"
			return view
		}
		view.Tone = "partial"
		view.Label = "部分完成"
		return view
	}
	if outcome.State == TaskSucceeded && outcome.Completion == CompletionVerified && len(outcome.RemainingSteps) == 0 {
		view.Tone = "complete"
		view.Label = "已验证完成"
		view.GreenComplete = true
		view.TaskComplete = true
		return view
	}
	if outcome.State == TaskSucceeded && outcome.Completion == CompletionNotRequired {
		view.Tone = "replied"
		view.Label = "已回复"
		return view
	}
	if outcome.State == TaskFailed {
		view.Tone = "failed"
		view.Label = "失败"
	}
	return view
}

func hasMissingArtifact(outcome TaskOutcome, artifactPaths []string) bool {
	hasPath := false
	for _, path := range artifactPaths {
		if strings.TrimSpace(path) == "" {
			return true
		}
		hasPath = true
	}
	for _, ref := range outcome.ArtifactRefs {
		if strings.TrimSpace(ref) == "" {
			return true
		}
	}
	if len(outcome.RemainingSteps) > 0 {
		return true
	}
	if outcome.State == TaskSucceeded && outcome.Completion == CompletionVerified && !hasPath {
		return true
	}
	return false
}
