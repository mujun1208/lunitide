package agentrun

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	runULID     = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	sessionULID = "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	turnULID    = "01ARZ3NDEKTSV4RRFFQ69G5FAB"
	stepULID    = "01ARZ3NDEKTSV4RRFFQ69G5FAC"
	callULID    = "01ARZ3NDEKTSV4RRFFQ69G5FAD"
)

var testAt = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

func validBudget() Budget {
	return Budget{
		MaxModelTurns: 8, MaxToolCalls: 32, MaxTokens: 100000,
		MaxCostMicros: 500000, MaxWallClockSeconds: 3600, MaxOutputBytes: 1 << 20,
		MaxRetries: 3, MaxNoProgress: 5, HardCeiling: true,
	}
}

func validRun() AgentRun {
	return AgentRun{
		ID: runULID, SessionID: sessionULID, Status: RunQueued,
		Budget: validBudget(), Version: 1, CreatedAt: testAt, UpdatedAt: testAt,
	}
}

func TestRunStateMachine(t *testing.T) {
	r := validRun()
	// queued -> running
	r, err := r.Transition(RunRunning, testAt)
	if err != nil || r.Status != RunRunning || r.Version != 2 {
		t.Fatalf("queued->running: %v %+v", err, r)
	}
	// running <-> paused_review
	r, err = r.Transition(RunPausedReview, testAt)
	if err != nil {
		t.Fatal(err)
	}
	r, err = r.Transition(RunRunning, testAt)
	if err != nil {
		t.Fatal(err)
	}
	// running <-> paused_budget
	r, err = r.Transition(RunPausedBudget, testAt)
	if err != nil {
		t.Fatal(err)
	}
	r, err = r.Transition(RunRunning, testAt)
	if err != nil {
		t.Fatal(err)
	}
	// running -> each terminal
	for _, to := range []RunStatus{RunCompleted, RunFailed, RunCancelled, RunInterrupted, RunOutcomeUnknown} {
		r2, err := r.Transition(to, testAt)
		if err != nil || r2.Status != to {
			t.Fatalf("running->%s: %v", to, err)
		}
		// first-terminal-wins
		if _, err = r2.Transition(RunRunning, testAt); !errors.Is(err, ErrTerminal) {
			t.Fatalf("terminal %s allowed transition: %v", to, err)
		}
	}
	// illegal: queued -> completed, paused -> terminal
	if _, err = validRun().Transition(RunCompleted, testAt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("queued->completed accepted: %v", err)
	}
	paused, _ := validRun().Transition(RunRunning, testAt)
	paused, _ = paused.Transition(RunPausedReview, testAt)
	if _, err = paused.Transition(RunCompleted, testAt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("paused_review->completed accepted: %v", err)
	}
}

func TestRunValidate(t *testing.T) {
	if err := validRun().Validate(); err != nil {
		t.Fatal(err)
	}
	r := validRun()
	r.ID = "not-a-ulid"
	if err := r.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad ID accepted: %v", err)
	}
	r = validRun()
	r.Budget.MaxTokens = 0
	if err := r.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero token budget accepted: %v", err)
	}
}

func TestBudgetExceededBy(t *testing.T) {
	b := validBudget()
	if got := b.ExceededBy(Usage{}); got != "" {
		t.Fatalf("empty usage exceeded %q", got)
	}
	if got := b.ExceededBy(Usage{Tokens: 100001}); got != "tokens" {
		t.Fatalf("got %q", got)
	}
	if got := b.ExceededBy(Usage{WallClockSeconds: 3601}); got != "wallClock" {
		t.Fatalf("got %q", got)
	}
	if got := b.ExceededBy(Usage{Retries: 4}); got != "retries" {
		t.Fatalf("got %q", got)
	}
	if got := b.ExceededBy(Usage{NoProgress: 6}); got != "noProgress" {
		t.Fatalf("got %q", got)
	}
}

func TestToolCallStateMachine(t *testing.T) {
	c := ToolCall{
		ID: callULID, StepID: stepULID, ToolName: "fs.read",
		ArgsDigest: strings.Repeat("a", 64), Status: CallProposed,
		CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	path := []CallStatus{CallPolicyChecked, CallAwaitingReview, CallApproved, CallRunning, CallSucceeded}
	var err error
	for _, to := range path {
		c, err = c.Transition(to, testAt)
		if err != nil || c.Status != to {
			t.Fatalf("->%s: %v", to, err)
		}
	}
	if _, err := c.Transition(CallRunning, testAt); !errors.Is(err, ErrTerminal) {
		t.Fatalf("terminal call allowed transition: %v", err)
	}
	// awaiting_review -> denied
	d := ToolCall{ID: callULID, StepID: stepULID, ToolName: "fs.read", ArgsDigest: strings.Repeat("b", 64), Status: CallAwaitingReview, CreatedAt: testAt, UpdatedAt: testAt}
	d, err = d.Transition(CallDenied, testAt)
	if err != nil || d.Status != CallDenied {
		t.Fatalf("awaiting_review->denied: %v", err)
	}
	// illegal shortcuts
	bad := ToolCall{Status: CallProposed, CreatedAt: testAt, UpdatedAt: testAt}
	if _, err = bad.Transition(CallApproved, testAt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("proposed->approved accepted: %v", err)
	}
	running := ToolCall{Status: CallRunning, CreatedAt: testAt, UpdatedAt: testAt}
	if _, err = running.Transition(CallDenied, testAt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("running->denied accepted: %v", err)
	}
}

func TestTurnStepValidate(t *testing.T) {
	turn := AgentTurn{ID: turnULID, RunID: runULID, TurnNo: 1, Status: TurnRunning, Version: 1, CreatedAt: testAt, UpdatedAt: testAt}
	if err := turn.Validate(); err != nil {
		t.Fatal(err)
	}
	turn.TurnNo = 0
	if err := turn.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("turn_no 0 accepted: %v", err)
	}
	step := AgentStep{ID: stepULID, TurnID: turnULID, StepNo: 1, Kind: StepTool, Status: StepPending, CreatedAt: testAt, UpdatedAt: testAt}
	if err := step.Validate(); err != nil {
		t.Fatal(err)
	}
	step.Kind = "shell"
	if err := step.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("kind shell accepted: %v", err)
	}
}

func TestObservationAppendOnly(t *testing.T) {
	o := Observation{
		ID: callULID, StepID: stepULID, Kind: "fs.read.result",
		ContentDigest: strings.Repeat("c", 64), CapturedAt: testAt, CreatedAt: testAt,
	}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	o.ContentDigest = "zz"
	if err := o.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad digest accepted: %v", err)
	}
}

func TestEffectJournalResolution(t *testing.T) {
	e := EffectJournal{
		ID: callULID, RunID: runULID, EffectKey: "changeset.apply/1",
		RequestDigest: strings.Repeat("d", 64), Status: EffectPrepared,
		CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	u, err := e.Resolve(EffectOutcomeUnknown, "", testAt)
	if err != nil {
		t.Fatal(err)
	}
	// reconcile resolves outcome_unknown exactly once
	u, err = u.Resolve(EffectCommitted, "receipt-1", testAt)
	if err != nil || u.Status != EffectCommitted || u.ReceiptID != "receipt-1" {
		t.Fatalf("reconcile: %v %+v", err, u)
	}
	if _, err = u.Resolve(EffectFailed, "", testAt); !errors.Is(err, ErrTerminal) {
		t.Fatalf("committed effect resolved again: %v", err)
	}
	// prepared cannot jump to released-style states
	if _, err = e.Resolve(EffectStatus("released"), "", testAt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("prepared->released accepted: %v", err)
	}
}

func TestChangeSetStateMachine(t *testing.T) {
	cs := ChangeSet{
		ID: callULID, RunID: runULID, BaseDigest: strings.Repeat("e", 64),
		ApprovalDigest: strings.Repeat("f", 64), Status: ChangeSetDraft,
		Version: 1, CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := cs.Validate(); err != nil {
		t.Fatal(err)
	}
	var err error
	for _, to := range []ChangeSetStatus{ChangeSetPreviewed, ChangeSetApproved, ChangeSetApplied} {
		cs, err = cs.Transition(to, testAt)
		if err != nil || cs.Status != to {
			t.Fatalf("->%s: %v", to, err)
		}
	}
	if _, err := cs.Transition(ChangeSetReverted, testAt); err != nil {
		t.Fatal(err)
	}
	// conflict path from approved
	cs2 := ChangeSet{ID: callULID, RunID: runULID, BaseDigest: strings.Repeat("e", 64), ApprovalDigest: strings.Repeat("f", 64), Status: ChangeSetApproved, Version: 1, CreatedAt: testAt, UpdatedAt: testAt}
	cs2, err = cs2.Transition(ChangeSetConflicted, testAt)
	if err != nil || cs2.Status != ChangeSetConflicted {
		t.Fatalf("approved->conflicted: %v", err)
	}
	if _, err = cs2.Transition(ChangeSetApplied, testAt); !errors.Is(err, ErrTerminal) {
		t.Fatalf("conflicted set applied: %v", err)
	}
	// illegal
	draft := ChangeSet{Status: ChangeSetDraft, CreatedAt: testAt, UpdatedAt: testAt}
	if _, err = draft.Transition(ChangeSetApplied, testAt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("draft->applied accepted: %v", err)
	}
}

func TestCommandJobTransitions(t *testing.T) {
	j := CommandJob{
		ID: callULID, RunID: runULID, CommandSpecDigest: strings.Repeat("1", 64),
		Status: JobQueued, CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
	j, err := j.Transition(JobRunning, nil, testAt)
	if err != nil {
		t.Fatal(err)
	}
	exit := int64(0)
	j, err = j.Transition(JobCompleted, &exit, testAt)
	if err != nil || *j.ExitCode != 0 {
		t.Fatalf("complete: %v %+v", err, j)
	}
	// exit code only on completed/failed
	bad := CommandJob{ID: callULID, RunID: runULID, CommandSpecDigest: strings.Repeat("1", 64), Status: JobQueued, ExitCode: &exit, CreatedAt: testAt, UpdatedAt: testAt}
	if err = bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("queued job with exit code accepted: %v", err)
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	reg := WorkspaceRegistration{
		ID: callULID, CanonicalRoot: `E:\ws\demo`, RootDigest: strings.Repeat("2", 64),
		Status: RegistrationActive, Version: 1, CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	grant := WorkspaceGrant{
		ID: callULID, RegistrationID: reg.ID, Scope: []byte(`{"paths":["**"],"ops":["read"]}`),
		ExpiresAt: testAt.Add(time.Hour), Status: GrantActive, CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := grant.Validate(); err != nil {
		t.Fatal(err)
	}
	if !grant.UsableAt(testAt.Add(30 * time.Minute)) {
		t.Fatal("active grant not usable before expiry")
	}
	if grant.UsableAt(testAt.Add(2 * time.Hour)) {
		t.Fatal("grant usable after expiry")
	}
	lease := WorkspaceLease{
		ID: stepULID, GrantID: grant.ID, FencingToken: 1,
		ExpiresAt: testAt.Add(time.Minute), Status: LeaseActive, CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := lease.Validate(); err != nil {
		t.Fatal(err)
	}
	if lease.UsableAt(testAt.Add(2 * time.Minute)) {
		t.Fatal("lease usable after expiry")
	}
	lease.FencingToken = 0
	if err := lease.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fencing token 0 accepted: %v", err)
	}
}

func TestPlanEvidenceReviewValidate(t *testing.T) {
	p := RunPlan{
		ID: callULID, RunID: runULID, PlanDigest: strings.Repeat("3", 64),
		Content: []byte(`{"steps":[]}`), Version: 1, CreatedAt: testAt, UpdatedAt: testAt,
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	e := Evidence{
		ID: callULID, RunID: runULID, Kind: "web.fetch", SourceURI: "https://example.com/a",
		ContentDigest: strings.Repeat("4", 64), CapturedAt: testAt, CreatedAt: testAt,
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	r := RunReview{
		ID: callULID, RunID: runULID, ApprovalDigest: strings.Repeat("5", 64),
		Decision: ReviewApproved, DecidedBy: "owner", DecidedAt: testAt, CreatedAt: testAt,
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Decision = "maybe"
	if err := r.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("decision maybe accepted: %v", err)
	}
}

func TestRunEventValidate(t *testing.T) {
	e := RunEvent{
		ID: callULID, RunID: runULID, Sequence: 1, EventType: "AgentRunStartCompleted",
		Payload: []byte(`{"runId":"x"}`), CreatedAt: testAt,
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Sequence = 0
	if err := e.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sequence 0 accepted: %v", err)
	}
}
