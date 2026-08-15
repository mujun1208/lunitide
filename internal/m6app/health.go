// Legacy S5 telemetry application service: append-only sample/call intake
// and the five-state health aggregation query. Intake is
// governance-checked (integration must exist) but deliberately never
// blocks on the health state — a degraded integration still gets its
// failures recorded; the aggregate decides scheduling, not the recorder.
package m6app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/oklog/ulid/v2"
)

// HealthService records telemetry and answers health aggregates.
type HealthService struct {
	uow   UnitOfWork
	clock Clock
}

func NewHealthService(uow UnitOfWork) *HealthService {
	return &HealthService{uow: uow, clock: systemClock{}}
}

func (s *HealthService) SetClock(c Clock) { s.clock = c }

// SampleInput is one probe sample.
type SampleInput struct {
	IntegrationID string
	Status        string
	Success       bool
	LatencyMS     int64
	CodeClass     string
}

// RecordSample appends one health sample. The integration must exist; the
// sample is a fact about the moment, never corrected in place.
func (s *HealthService) RecordSample(ctx context.Context, in SampleInput) (m6supply.HealthSample, error) {
	if s == nil || s.uow == nil {
		return m6supply.HealthSample{}, ErrServiceUnavailable
	}
	if err := m6supply.ValidateHealthSample(in.Status, in.LatencyMS, in.CodeClass); err != nil {
		return m6supply.HealthSample{}, err
	}
	var out m6supply.HealthSample
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.GetM6Integration(in.IntegrationID); err != nil {
			if errors.Is(err, m6supply.ErrNotFound) {
				return ErrIntegrationNotFound
			}
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.HealthSample{
			ID: ulid.Make().String(), IntegrationID: in.IntegrationID,
			Status: in.Status, Success: in.Success, LatencyMS: in.LatencyMS,
			CodeClass: in.CodeClass, SampledAt: now,
		}
		return tx.PutM6HealthSample(out)
	})
	return out, err
}

// CallInput is one immutable call record.
type CallInput struct {
	IntegrationID        string
	OperationID          string
	TraceID              string
	ActorID              string
	SubjectID            string
	Environment          string
	GrantID              string
	Attempt              int64
	CompletedAt          *time.Time
	RequestBytes         *int64
	ResponseBytes        *int64
	StatusClass          string
	RequestDigest        string
	ResponseDigest       string
	Outcome              string
	ErrorCode            string
	LatencyMS            *int64
	CostMicros           *int64
	RetryOfCallID        string
	CorrectionOfCallID   string
	IdempotencyKeyDigest string
	PolicyDecisionID     string
}

// RecordCall appends one call record. outcome_unknown is a first-class
// outcome: a lost completion is recorded as the fact it is, corrected (if
// ever) by a later appended row referencing correctionOfCallId.
func (s *HealthService) RecordCall(ctx context.Context, in CallInput) (m6supply.CallLog, error) {
	if s == nil || s.uow == nil {
		return m6supply.CallLog{}, ErrServiceUnavailable
	}
	if err := m6supply.ValidateCallLog(in.OperationID, in.Environment, in.Outcome, in.Attempt); err != nil {
		return m6supply.CallLog{}, err
	}
	var out m6supply.CallLog
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.GetM6Integration(in.IntegrationID); err != nil {
			if errors.Is(err, m6supply.ErrNotFound) {
				return ErrIntegrationNotFound
			}
			return err
		}
		now := s.clock.Now().UTC()
		started := now
		if in.CompletedAt != nil {
			started = in.CompletedAt.UTC()
		}
		out = m6supply.CallLog{
			ID: ulid.Make().String(), IntegrationID: in.IntegrationID,
			OperationID: in.OperationID, TraceID: in.TraceID,
			ActorID: in.ActorID, SubjectID: in.SubjectID,
			Environment: in.Environment, GrantID: in.GrantID,
			Attempt: in.Attempt, StartedAt: started, CompletedAt: in.CompletedAt,
			RequestBytes: in.RequestBytes, ResponseBytes: in.ResponseBytes,
			StatusClass: in.StatusClass, RequestDigest: in.RequestDigest,
			ResponseDigest: in.ResponseDigest, Outcome: in.Outcome,
			ErrorCode: in.ErrorCode, LatencyMS: in.LatencyMS, CostMicros: in.CostMicros,
			RetryOfCallID: in.RetryOfCallID, CorrectionOfCallID: in.CorrectionOfCallID,
			IdempotencyKeyDigest: in.IdempotencyKeyDigest, PolicyDecisionID: in.PolicyDecisionID,
			CreatedAt: now,
		}
		return tx.PutM6CallLog(out)
	})
	return out, err
}

// Aggregate computes the five-state health rollup for one integration
// over the trailing window. window <= 0 falls back to the default.
func (s *HealthService) Aggregate(ctx context.Context, integrationID string, window time.Duration) (m6supply.HealthAggregate, error) {
	if s == nil || s.uow == nil {
		return m6supply.HealthAggregate{}, ErrServiceUnavailable
	}
	if window <= 0 {
		window = m6supply.DefaultHealthWindow
	}
	var agg m6supply.HealthAggregate
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		ig, err := tx.GetM6Integration(integrationID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrIntegrationNotFound
		}
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		windowStart := now.Add(-window)
		samples, err := tx.ListM6HealthSamples(integrationID, windowStart, 10000)
		if err != nil {
			return err
		}
		agg = m6supply.AggregateHealth(ig.State, samples, windowStart, m6supply.DefaultHealthMinSamples)
		agg.IntegrationID = integrationID
		return nil
	})
	return agg, err
}
