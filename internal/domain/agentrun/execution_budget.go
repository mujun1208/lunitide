package agentrun

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrExecutionBudget          = errors.New("execution budget rejected")
	ErrExecutionPolicy          = errors.New("execution budget policy invalid")
	ErrExecutionBinding         = errors.New("execution binding invalid")
	ErrExecutionReservation     = errors.New("execution reservation invalid")
	ErrV1MutatesV2Budget        = errors.New("v1 path cannot mutate policyVersion=2 budget")
	ErrExecutionReceiptConflict = errors.New("execution settlement receipt conflict")
)

type ExecutionScope struct {
	TaskID, RunID, ScopeID, ParentScopeID string
	GoalRevision                          int64
	PolicyRevision                        string
}

type ExecutionBudgetPolicy struct {
	MaxTotalTokens       *int64
	MaxOutputTokens      *int64
	MaxModelAttempts     *int64
	MaxActiveMillis      *int64
	MaxOutputBytes       *int64
	CloseoutOutputTokens int64
	Legacy               *LegacyGuards
}

type LegacyGuards struct {
	SourceBudgetDigest          string
	MaxToolCalls, MaxCostMicros *int64
	MaxRetries, MaxNoProgress   *int64
	Deadline                    *time.Time
	HardCeiling                 bool
}

type CallEstimate struct {
	CallID, AttemptID, RequestDigest                string
	InputTokensUpper, OutputTokenCap, ContextWindow int64
	SafetyMargin, OutputBytesCap                    int64
	Purpose, TokenizerRevision, ProfileDigest       string
}

type CallPermit struct {
	ReservationID, AttemptID string
	OutputTokenCap           int64
	Deadline                 time.Time
}

type UsageTotals struct {
	InputTokens, OutputTokens, CachedInputTokens int64
}

type CallSettlement struct {
	ReservationID, ReceiptID, PayloadDigest string
	SettlementRevision                      int64
	Usage                                   UsageTotals
	ModelOutputBytes, AttemptElapsedMillis  int64
	Integrity                               string
	Dispatched                              bool
	Outcome                                 string
}

type BudgetSnapshot struct {
	ConsumedTotal, ConsumedOutput, ReservedTotal, IsolatedTotal   int64
	TaskRevision                                                  int64
	Attempts, ActiveMillis                                        int64
	ConsumedOutputBytes, ReservedOutputBytes, IsolatedOutputBytes int64
	Overrun                                                       bool
	Integrity                                                     string
}

type ExecutionBudget interface {
	AdmitCall(context.Context, ExecutionScope, CallEstimate) (CallPermit, error)
	MarkDispatched(context.Context, CallPermit) error
	SettleCall(context.Context, CallSettlement) error
	ReleaseUnsent(context.Context, CallPermit, string) error
	Snapshot(context.Context, ExecutionScope) (BudgetSnapshot, error)
	TransitionActivity(context.Context, ExecutionScope, ActivityTransition) (ActivityResult, error)
}

type ActivityResult struct {
	TaskRevision, ActiveElapsedMillis int64
	Integrity                         string
}

type ActivityTransition struct {
	RuntimeEpoch, EventID, State string
	ExpectedRevision             int64
	At                           time.Time
}

type ExecutionUsageRollup struct {
	ConsumedTotal, ConsumedOutput, ReservedTotal, IsolatedTotal   int64
	ConsumedOutputBytes, ReservedOutputBytes, IsolatedOutputBytes int64
	Attempts                                                      int64
	Overrun                                                       bool
	Integrity                                                     string
}

func (b Budget) IsV2Accounting() bool {
	return b.PolicyVersion == 2
}

func (p ExecutionBudgetPolicy) ValidateEffective() error {
	if p.MaxTotalTokens == nil || p.MaxOutputTokens == nil || p.MaxModelAttempts == nil || p.MaxActiveMillis == nil {
		return fmt.Errorf("%w: effective policy must set total, output, attempts, and activeMillis", ErrExecutionPolicy)
	}
	if *p.MaxTotalTokens < 0 || *p.MaxOutputTokens < 0 || *p.MaxModelAttempts < 0 || *p.MaxActiveMillis < 0 {
		return fmt.Errorf("%w: limits must not be negative", ErrExecutionPolicy)
	}
	if p.MaxOutputBytes != nil && *p.MaxOutputBytes < 0 {
		return fmt.Errorf("%w: output bytes must not be negative", ErrExecutionPolicy)
	}
	if p.CloseoutOutputTokens < 0 {
		return fmt.Errorf("%w: closeout tokens must not be negative", ErrExecutionPolicy)
	}
	return nil
}

func (e CallEstimate) RequestedTokens() int64 {
	return e.InputTokensUpper + e.OutputTokenCap
}

func SafetyMargin(contextWindow int64) int64 {
	if contextWindow <= 0 {
		return 0
	}
	margin := int64(math.Ceil(float64(contextWindow) * 0.02))
	if margin < 1024 {
		return 1024
	}
	return margin
}

func (e CallEstimate) CheckContextWindow() error {
	if e.ContextWindow <= 0 {
		return nil
	}
	margin := e.SafetyMargin
	if margin <= 0 {
		margin = SafetyMargin(e.ContextWindow)
	}
	if e.InputTokensUpper+margin > e.ContextWindow {
		return fmt.Errorf("%w: safety margin %d makes window %d unusable for input %d", ErrExecutionBudget, margin, e.ContextWindow, e.InputTokensUpper)
	}
	return nil
}

// CanReserve reports whether an additional reservation fits the limit.
// All inputs must be nonnegative. Successive subtraction avoids overflow.
func CanReserve(limit, consumed, reserved, isolated, additional int64) bool {
	if limit < 0 || consumed < 0 || reserved < 0 || isolated < 0 || additional < 0 {
		return false
	}
	remaining := limit
	for _, commitment := range [...]int64{consumed, reserved, isolated} {
		if commitment > remaining {
			return false
		}
		remaining -= commitment
	}
	return additional <= remaining
}

func (u UsageTotals) ConsumedTokens() int64 {
	if u.CachedInputTokens > u.InputTokens {
		return u.InputTokens + u.OutputTokens
	}
	return u.InputTokens + u.OutputTokens
}
