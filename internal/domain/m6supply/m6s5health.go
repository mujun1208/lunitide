// Legacy S5 telemetry domain (migration 0053): the append-only
// HealthSample and CallLog streams plus the five-state health aggregation.
//
// Both tables are guarded by BEFORE UPDATE/DELETE triggers that raise
// M6-APPENDONLY — a sample or call record is a fact, never edited. The
// aggregation is computed over the trailing window and can therefore be
// re-derived at any time from the immutable stream.
//
// Five-state health (M6/02 §06 Legacy S5):
//
//	paused    the integration row is paused — takes priority over every
//	          sample-derived state (manual recovery gates scheduling)
//	healthy   trailing success rate >= 99%
//	degraded  trailing success rate >= 90% and < 99%
//	unhealthy trailing success rate < 90%
//	unknown   no samples in the window (or the integration missing)
package m6supply

import (
	"fmt"
	"time"
)

// Health statuses (m6_health_sample.status CHECK set).
const (
	HealthUnknown  = "unknown"
	HealthHealthy  = "healthy"
	HealthDegraded = "degraded"
	HealthUnhealthy = "unhealthy"
	HealthPaused   = "paused"
)

// Trailing health thresholds.
const (
	HealthyThreshold   = 0.99
	DegradedThreshold  = 0.90
	// DefaultHealthWindow is the default trailing window.
	DefaultHealthWindow = 24 * time.Hour
	// DefaultHealthMinSamples: below this the aggregate stays unknown even
	// if all samples succeeded — one lucky call is not health.
	DefaultHealthMinSamples = 5
)

// HealthSample is one immutable probe or call-derived sample.
type HealthSample struct {
	ID            string
	IntegrationID string
	Status        string
	Success       bool
	LatencyMS     int64
	CodeClass     string // 1xx..5xx, empty when not HTTP-derived
	SampledAt     time.Time
}

// ValidateHealthSample checks payload shape against the 0053 CHECK set.
func ValidateHealthSample(status string, latencyMS int64, codeClass string) error {
	switch status {
	case HealthUnknown, HealthHealthy, HealthDegraded, HealthUnhealthy, HealthPaused:
	default:
		return fmt.Errorf("status must be a five-state health value")
	}
	if latencyMS < 0 {
		return fmt.Errorf("latencyMs must be >= 0")
	}
	switch codeClass {
	case "", "1xx", "2xx", "3xx", "4xx", "5xx":
	default:
		return fmt.Errorf("codeClass must be a 1xx..5xx class or empty")
	}
	return nil
}

// CallOutcome values (m6_call_log.outcome CHECK set).
const (
	CallSucceeded      = "succeeded"
	CallFailed         = "failed"
	CallCancelled      = "cancelled"
	CallOutcomeUnknown = "outcome_unknown"
)

// CallEnvironments (m6_call_log.environment CHECK set) — the same key set
// as EnvironmentBindingKeys.
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// CallLog is one immutable call record. A completed call carries outcome
// succeeded/failed/cancelled; a call whose completion never arrived (or
// whose receipt was lost) is corrected by appending an outcome_unknown
// row that references it via correctionOfCallId — the original row is
// never edited.
type CallLog struct {
	ID                  string
	IntegrationID       string
	OperationID         string
	TraceID             string
	ActorID             string
	SubjectID           string
	Environment         string
	GrantID             string
	Attempt             int64
	StartedAt           time.Time
	CompletedAt         *time.Time
	RequestBytes        *int64
	ResponseBytes       *int64
	StatusClass         string
	RequestDigest       string
	ResponseDigest      string
	Outcome             string
	ErrorCode           string
	LatencyMS           *int64
	CostMicros          *int64
	RetryOfCallID       string
	CorrectionOfCallID  string
	IdempotencyKeyDigest string
	PolicyDecisionID    string
	CreatedAt           time.Time
}

// ValidateCallLog checks the payload against the 0053 CHECK set.
func ValidateCallLog(operationID, environment, outcome string, attempt int64) error {
	if len(operationID) < 1 || len(operationID) > 256 {
		return fmt.Errorf("operationId length must be 1..256")
	}
	switch environment {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		return fmt.Errorf("environment must be development|test|production")
	}
	switch outcome {
	case CallSucceeded, CallFailed, CallCancelled, CallOutcomeUnknown:
	default:
		return fmt.Errorf("outcome must be succeeded|failed|cancelled|outcome_unknown")
	}
	if attempt < 1 {
		return fmt.Errorf("attempt must be >= 1")
	}
	return nil
}

// HealthAggregate is the five-state rollup over the trailing window.
type HealthAggregate struct {
	IntegrationID string
	State         string // the five-state aggregate
	WindowStart   time.Time
	Samples       int64
	Successes     int64
	SuccessRate   float64
}

// AggregateHealth folds the trailing samples into the five-state model.
//
// Precedence (M6/02): a paused integration is paused regardless of its
// samples; otherwise the trailing success rate decides; with too few
// samples the answer is unknown — health is evidence-based, never assumed.
func AggregateHealth(integrationState string, samples []HealthSample, windowStart time.Time, minSamples int) HealthAggregate {
	agg := HealthAggregate{IntegrationID: "", State: HealthUnknown, WindowStart: windowStart}
	if minSamples < 1 {
		minSamples = 1
	}
	if integrationState == IntegrationPaused {
		agg.State = HealthPaused
		// still count the samples for observability
		for _, s := range samples {
			if !s.SampledAt.Before(windowStart) {
				agg.Samples++
				if s.Success {
					agg.Successes++
				}
			}
		}
		agg.finish()
		return agg
	}
	for _, s := range samples {
		if !s.SampledAt.Before(windowStart) {
			agg.Samples++
			if s.Success {
				agg.Successes++
			}
		}
	}
	if agg.Samples < int64(minSamples) {
		agg.State = HealthUnknown
		return agg
	}
	rate := float64(agg.Successes) / float64(agg.Samples)
	switch {
	case rate >= HealthyThreshold:
		agg.State = HealthHealthy
	case rate >= DegradedThreshold:
		agg.State = HealthDegraded
	default:
		agg.State = HealthUnhealthy
	}
	agg.finish()
	return agg
}

func (a *HealthAggregate) finish() {
	if a.Samples > 0 {
		a.SuccessRate = float64(a.Successes) / float64(a.Samples)
	} else {
		a.SuccessRate = 0
	}
}

// Schedulable reports whether the aggregate permits call scheduling.
// paused and unhealthy block (HLT-001); degraded and healthy pass; unknown
// passes only for development/test environments — production requires
// proven health. The environment decision is the caller's policy; this
// helper answers the raw gate.
func (a HealthAggregate) BlocksScheduling() bool {
	switch a.State {
	case HealthPaused, HealthUnhealthy:
		return true
	default:
		return false
	}
}
