package modelfit

import "testing"

func TestResumeMayExecuteOnlySafeClasses(t *testing.T) {
	if ResumeMayExecute(ToolOperation{State: OpFailed, EffectClass: EffectReadOnly}) != true {
		t.Fatal("failed read_only may execute reread")
	}
	if ResumeMayExecute(ToolOperation{State: OpPartial, EffectClass: EffectLocalReversible}) != true {
		t.Fatal("partial reversible may continue remaining items")
	}
	if ResumeMayExecute(ToolOperation{State: OpRunning, EffectClass: EffectRemoteTrackable, ExternalID: "job-1"}) != true {
		t.Fatal("trackable with job id may query")
	}
	if ResumeMayExecute(ToolOperation{State: OpRunning, EffectClass: EffectRemoteTrackable}) {
		t.Fatal("trackable without job id must not create")
	}
	if ResumeMayExecute(ToolOperation{State: OpUnknown, EffectClass: EffectLocalReversible}) {
		t.Fatal("unknown must stay verify-only")
	}
	if ResumeMayExecute(ToolOperation{State: OpFailed, EffectClass: EffectDesktopInteractive}) {
		t.Fatal("desktop must never auto-execute")
	}
	if ResumeMayExecute(ToolOperation{State: OpFailed, EffectClass: EffectRemoteUnknown}) {
		t.Fatal("remote_unknown must never auto-execute")
	}
	if ResumeMayExecute(ToolOperation{State: OpSucceeded, EffectClass: EffectReadOnly}) {
		t.Fatal("succeeded must not rerun")
	}
	if ResumeMayExecute(ToolOperation{State: OpPartial, EffectClass: EffectLocalReversible, CancellationRequestedAt: "now"}) {
		t.Fatal("cancelled must not revive")
	}
}

func TestRecoveryDecisionFollowsCompleteness(t *testing.T) {
	if RecoveryDecision(ContinuationEnvelope{Completeness: CompletenessNativeComplete, CodecVersion: CodecDeepSeekV1}) != RecoverNativeContinue {
		t.Fatal("native_complete")
	}
	if RecoveryDecision(ContinuationEnvelope{Completeness: CompletenessStructuredOnly}) != RecoverBusinessRebuild {
		t.Fatal("structured_only")
	}
	if RecoveryDecision(ContinuationEnvelope{Completeness: CompletenessLegacyUnknown}) != RecoverVerifyReadonly {
		t.Fatal("legacy_unknown")
	}
	if RecoveryDecision(ContinuationEnvelope{Completeness: CompletenessNativeComplete, CancellationRequestedAt: "now"}) != RecoverKeepStopped {
		t.Fatal("cancelled native")
	}
}

func TestResumeDecisionDoesNotRepeatSucceededWork(t *testing.T) {
	if got := ResumeDecision(OpSucceeded, EffectLocalReversible); got != ResumeShowExisting {
		t.Fatalf("succeeded reversible = %q, want show existing", got)
	}
	if got := ResumeDecision(OpSucceeded, EffectRemoteTrackable); got != ResumeShowExisting {
		t.Fatalf("succeeded remote = %q", got)
	}
}

func TestResumeDecisionKeepsUnknownPendingVerification(t *testing.T) {
	if got := ResumeDecision(OpUnknown, EffectRemoteUnknown); got != ResumeVerifyUnknown {
		t.Fatalf("unknown remote = %q, want verify", got)
	}
	if got := ResumeDecision(OpRunning, EffectRemoteTrackable); got != ResumeQueryExisting {
		t.Fatalf("running trackable = %q, want query", got)
	}
}

func TestResumeDecisionDoesNotReviveCancelled(t *testing.T) {
	if got := ResumeDecision(OpCancelled, EffectReadOnly); got != ResumeKeepStopped {
		t.Fatalf("cancelled read = %q, want keep stopped", got)
	}
	if got := ResumeDecision(OpCancelled, EffectDesktopInteractive); got != ResumeKeepStopped {
		t.Fatalf("cancelled desktop = %q", got)
	}
}

func TestEffectClassForToolIsConservative(t *testing.T) {
	if got := EffectClassForTool("workspace.read"); got != EffectReadOnly {
		t.Fatalf("read = %q", got)
	}
	if got := EffectClassForTool("workspace.write"); got != EffectLocalReversible {
		t.Fatalf("write = %q", got)
	}
	if got := EffectClassForTool("computer.act"); got != EffectDesktopInteractive {
		t.Fatalf("computer = %q", got)
	}
	if got := EffectClassForTool("unknown.vendor"); got != EffectRemoteUnknown {
		t.Fatalf("unknown default = %q, want remote_unknown", got)
	}
	if got := EffectClassForTool("files.status"); got != EffectReadOnly {
		t.Fatalf("files.status = %q, want read_only", got)
	}
	if got := EffectClassForTool("files.apply"); got != EffectLocalReversible {
		t.Fatalf("files.apply = %q, want local_reversible", got)
	}
	if got := EffectClassForTool("data.process"); got != EffectLocalReversible {
		t.Fatalf("data.process = %q, want local_reversible", got)
	}
	if got := EffectClassForTool("image.batch"); got != EffectLocalReversible {
		t.Fatalf("image.batch = %q, want local_reversible", got)
	}
	if got := EffectClassForTool("pdf.copy"); got != EffectLocalReversible {
		t.Fatalf("pdf.copy = %q, want local_reversible", got)
	}
}

func TestCancelDoesNotEraseUnknownOutcome(t *testing.T) {
	op := ToolOperation{State: OpUnknown, EffectClass: EffectRemoteUnknown}
	got, err := op.RequestCancel()
	if err != nil {
		t.Fatal(err)
	}
	if got.State != OpUnknown || got.CancellationRequestedAt == "" {
		t.Fatalf("cancel erased unknown outcome: %+v", got)
	}
	if action := ResumeDecisionFor(got); action != ResumeKeepStopped {
		t.Fatalf("cancel-requested unknown must stay stopped: %q", action)
	}
	if action := ResumeDecision(OpUnknown, EffectRemoteUnknown); action != ResumeVerifyUnknown {
		t.Fatalf("bare unknown must still verify: %q", action)
	}
	done := ToolOperation{State: OpSucceeded, EffectClass: EffectLocalReversible, CancellationRequestedAt: got.CancellationRequestedAt}
	if action := ResumeDecisionFor(done); action != ResumeShowExisting {
		t.Fatalf("cancel after success must keep the completed result: %q", action)
	}
	partial := ToolOperation{State: OpPartial, EffectClass: EffectLocalReversible, CancellationRequestedAt: got.CancellationRequestedAt}
	if action := ResumeDecisionFor(partial); action != ResumeKeepStopped {
		t.Fatalf("cancel-requested partial must not suggest continue: %q", action)
	}
}
