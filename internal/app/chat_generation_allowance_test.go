package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// Thinking is discarded: never spoken, never stored as the reply. When it
// shared the deliverable bucket, one deep-reasoning pass could spend the whole
// turn before any work happened, which is what users saw as "额度已满" on jobs
// a non-reasoning model finished fine.
func TestThinkingDoesNotSpendTheReplyAllowance(t *testing.T) {
	b := turnGenerationBudget{}
	thinker := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return llmadapter.Response{}, emit(llmadapter.Delta{Reasoning: strings.Repeat("r", turnGenerationMaxBytes)})
	}}
	for pass := range 2 {
		if _, err := b.stream(context.Background(), thinker, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); err != nil {
			t.Fatalf("pass %d rejected for thinking alone: %v", pass, err)
		}
	}
	if b.bytes != 0 {
		t.Fatalf("thinking charged %d deliverable bytes", b.bytes)
	}
	// The reply allowance is still fully available after all that thinking.
	writer := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return llmadapter.Response{}, emit(llmadapter.Delta{Text: strings.Repeat("t", turnGenerationMaxBytes-1)})
	}}
	if _, err := b.stream(context.Background(), writer, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); err != nil {
		t.Fatalf("reply rejected after thinking: %v", err)
	}
}

// A runaway thinker is still stopped — the bucket is separate, not absent.
func TestRunawayThinkingStillHitsItsOwnCeiling(t *testing.T) {
	b := turnGenerationBudget{reasoning: turnReasoningMaxBytes - 10}
	a := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return llmadapter.Response{}, emit(llmadapter.Delta{Reasoning: strings.Repeat("r", 100)})
	}}
	if _, err := b.stream(context.Background(), a, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); !errors.Is(err, errTurnGenerationBudget) {
		t.Fatalf("runaway thinking not stopped: %v", err)
	}
}

// Executing real tools is the evidence that the turn is doing work rather than
// talking to itself, so the allowance that is about to run out grows.
func TestToolProgressGrowsOnlyTheAllowanceThatIsRunningOut(t *testing.T) {
	b := turnGenerationBudget{bytes: turnGenerationMaxBytes - 1024, elapsed: time.Minute}
	b.noteToolProgress()
	if got := b.byteLimit(); got != turnGenerationMaxBytes+turnGenerationByteChunk {
		t.Fatalf("byte allowance did not grow: %d", got)
	}
	if got := b.timeLimit(); got != turnGenerationMaxTime {
		t.Fatalf("untouched time allowance grew to %s", got)
	}
}

func TestToolProgressNeverGrowsPastTheHardCeiling(t *testing.T) {
	b := turnGenerationBudget{}
	for range 100 {
		b.bytes = b.byteLimit() - 1
		b.noteToolProgress()
	}
	if got := b.byteLimit(); got != turnGenerationHardBytes {
		t.Fatalf("byte allowance %d exceeded hard ceiling %d", got, turnGenerationHardBytes)
	}
}

func TestExhaustedBudgetIgnoresToolProgress(t *testing.T) {
	b := turnGenerationBudget{bytes: turnGenerationMaxBytes, exhausted: true}
	b.noteToolProgress()
	if b.byteCap != 0 {
		t.Fatalf("exhausted budget reopened to %d", b.byteCap)
	}
}

// The reserve exists so a turn that ran out mid-job can still report what it
// finished. It pays for one short reply, not for more thinking or more tools.
func TestClosingReserveFundsOneReplyAndOpensOnce(t *testing.T) {
	b := turnGenerationBudget{bytes: turnGenerationMaxBytes, reasoning: 4096, exhausted: true}
	if !b.openClosingReserve() {
		t.Fatal("reserve refused on first open")
	}
	if b.exhausted {
		t.Fatal("reserve left the budget exhausted")
	}
	if room := b.byteLimit() - b.bytes; room != turnGenerationReserveBytes {
		t.Fatalf("reserve granted %d reply bytes", room)
	}
	if room := b.reasoningLimit() - b.reasoning; room > 0 {
		t.Fatalf("reserve granted %d thinking bytes", room)
	}
	if b.openClosingReserve() {
		t.Fatal("reserve opened twice")
	}
}
