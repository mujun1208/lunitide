// Cloud execution application service: runner registration + lifecycle
// under the region policy, fenced leases, receipt intake and the
// reconcile walk. All mutations share the agent-runtime single-writer
// transaction with their audit rows.
//
// The receipt-loss invariant lives here: recording an outcome_unknown
// receipt and reconciling it never re-dispatches the task — the decision
// vocabulary (accepted/rejected/requeued/manual_review) draws from
// evidence, and requeued is the only path that schedules again, and only
// as an explicit, audited decision.
package m6app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrRunnerNotFound: the runner row does not exist.
	ErrRunnerNotFound = errors.New("m6app: cloud runner not found")
	// ErrRunnerExists: the workload identity is already registered.
	ErrRunnerExists = errors.New("m6app: workload identity already registered")
	// ErrAttestationRequired: only verified runners may activate or hold
	// leases.
	ErrAttestationRequired = errors.New("m6app: runner attestation not verified")
	// ErrRegionNotAllowed: the runner's region is outside the policy's
	// allowed regions.
	ErrRegionNotAllowed = errors.New("m6app: runner region not allowed by policy")
	// ErrLeaseNotFound: the lease row does not exist.
	ErrLeaseNotFound = errors.New("m6app: worker lease not found")
	// ErrLeaseExists: the task already holds a lease.
	ErrLeaseExists = errors.New("m6app: task already leased")
	// ErrReceiptNotFound: the receipt row does not exist.
	ErrReceiptNotFound = errors.New("m6app: remote receipt not found")
)

// CloudService implements the cloud execution registry use cases.
type CloudService struct {
	uow   UnitOfWork
	clock Clock
}

func NewCloudService(uow UnitOfWork) *CloudService {
	return &CloudService{uow: uow, clock: systemClock{}}
}

func (s *CloudService) SetClock(c Clock) { s.clock = c }

func (s *CloudService) available() error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return nil
}

// RegisterRunnerInput is the registration payload.
type RegisterRunnerInput struct {
	Region            string
	WorkloadIdentity  string
	AttestationDigest string
	AttestationStatus string
	MTLSFingerprint   string
	Capabilities      string // JSON
}

// RegisterRunner records a runner (cloudrunner.registered audit). A
// re-registration of the same workload identity answers the stored row.
func (s *CloudService) RegisterRunner(ctx context.Context, in RegisterRunnerInput) (m6supply.CloudRunner, error) {
	if err := s.available(); err != nil {
		return m6supply.CloudRunner{}, err
	}
	if err := m6supply.ValidateRunnerInput(in.Region, in.WorkloadIdentity, in.AttestationDigest, in.AttestationStatus, in.MTLSFingerprint, in.Capabilities); err != nil {
		return m6supply.CloudRunner{}, err
	}
	var out m6supply.CloudRunner
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		if existing, err := tx.FindM6CloudRunnerByIdentity(in.WorkloadIdentity); err == nil {
			out = existing
			return nil
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.CloudRunner{
			ID: ulid.Make().String(), Region: in.Region,
			WorkloadIdentity: in.WorkloadIdentity, AttestationDigest: in.AttestationDigest,
			AttestationStatus: in.AttestationStatus, MTLSFingerprint: in.MTLSFingerprint,
			Capabilities: in.Capabilities, State: m6supply.RunnerRegistered,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6CloudRunner(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "cloudrunner.registered",
			AggregateID: out.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				RunnerID   string `json:"runnerId"`
				Region     string `json:"region"`
				Identity   string `json:"workloadIdentity"`
				Attestation string `json:"attestationStatus"`
			}{RunnerID: out.ID, Region: out.Region, Identity: out.WorkloadIdentity, Attestation: out.AttestationStatus}),
		})
	})
	return out, err
}

// TransitionRunner moves the runner along its lifecycle. Activating an
// unverified runner is refused (ErrAttestationRequired).
func (s *CloudService) TransitionRunner(ctx context.Context, id string, expectedVersion int64, to string) (m6supply.CloudRunner, error) {
	if err := s.available(); err != nil {
		return m6supply.CloudRunner{}, err
	}
	var out m6supply.CloudRunner
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		cur, err := tx.GetM6CloudRunner(id)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrRunnerNotFound
		}
		if err != nil {
			return err
		}
		if cur.State == to {
			out = cur
			return nil
		}
		if !m6supply.RunnerTransitionAllowed(cur.State, to) {
			return m6supply.ErrInvalidTransition
		}
		if to == m6supply.RunnerActive && cur.AttestationStatus != m6supply.AttestationVerified {
			return ErrAttestationRequired
		}
		next, err := tx.TransitionM6CloudRunner(id, expectedVersion, to, s.clock.Now().UTC())
		if errors.Is(err, m6supply.ErrVersionConflict) {
			return err
		}
		if err != nil {
			return err
		}
		out = next
		return nil
	})
	return out, err
}

// PutRegionPolicy pins a new policy snapshot; versions are monotonic.
func (s *CloudService) PutRegionPolicy(ctx context.Context, allowedRegions, egressPolicy, dataClassification string) (m6supply.RegionPolicy, error) {
	if err := s.available(); err != nil {
		return m6supply.RegionPolicy{}, err
	}
	if err := m6supply.ValidateRegionPolicyInput(allowedRegions, egressPolicy, dataClassification); err != nil {
		return m6supply.RegionPolicy{}, err
	}
	var out m6supply.RegionPolicy
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		maxVersion, err := tx.MaxM6RegionPolicyVersion()
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.RegionPolicy{
			ID: ulid.Make().String(), Version: maxVersion + 1,
			AllowedRegions: allowedRegions, EgressPolicy: egressPolicy,
			DataClassification: dataClassification, CreatedAt: now,
		}
		return tx.PutM6RegionPolicy(out)
	})
	return out, err
}

// RegionAllowed decodes the latest policy's allowed regions and reports
// whether the region is schedulable. No policy at all denies — closed
// world.
func (s *CloudService) RegionAllowed(ctx context.Context, region string) (bool, error) {
	if err := s.available(); err != nil {
		return false, err
	}
	allowed := false
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		policy, err := tx.LatestM6RegionPolicy()
		if errors.Is(err, m6supply.ErrNotFound) {
			return nil // no policy: nothing is allowed
		}
		if err != nil {
			return err
		}
		var regions []string
		if err := json.Unmarshal([]byte(policy.AllowedRegions), &regions); err != nil {
			return err
		}
		for _, r := range regions {
			if r == region {
				allowed = true
				return nil
			}
		}
		return nil
	})
	return allowed, err
}

// GrantLease hands the task to a runner under a fencing epoch. The runner
// must be active, attested, and inside the allowed regions; the task must
// hold no live lease. Expired leases are swept first so their epochs
// advance.
func (s *CloudService) GrantLease(ctx context.Context, runnerID, taskID string, ttl time.Duration) (m6supply.WorkerLease, error) {
	if err := s.available(); err != nil {
		return m6supply.WorkerLease{}, err
	}
	if ttl <= 0 {
		return m6supply.WorkerLease{}, errors.New("m6app: lease ttl must be positive")
	}
	var out m6supply.WorkerLease
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		if _, err := tx.ExpireM6WorkerLeases(now); err != nil {
			return err
		}
		runner, err := tx.GetM6CloudRunner(runnerID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrRunnerNotFound
		}
		if err != nil {
			return err
		}
		if runner.State != m6supply.RunnerActive {
			return m6supply.ErrInvalidTransition
		}
		if runner.AttestationStatus != m6supply.AttestationVerified {
			return ErrAttestationRequired
		}
		allowed, err := s.regionAllowedTx(tx, runner.Region)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrRegionNotAllowed
		}
		if existing, err := tx.GetM6WorkerLeaseByTask(taskID); err == nil {
			if existing.State == m6supply.LeaseActive {
				return ErrLeaseExists
			}
			// expired/released/revoked: re-lease the same row (task_id is
			// UNIQUE) at a strictly higher fencing epoch
			next, err := tx.RenewM6WorkerLease(existing.ID, existing.Epoch+1, runnerID, now.Add(ttl), now)
			if errors.Is(err, m6supply.ErrVersionConflict) {
				return ErrLeaseExists
			}
			if err != nil {
				return err
			}
			out = next
			return nil
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		out = m6supply.WorkerLease{
			ID: ulid.Make().String(), RunnerID: runnerID, TaskID: taskID,
			Epoch: 1, ExpiresAt: now.Add(ttl), State: m6supply.LeaseActive,
			CreatedAt: now, UpdatedAt: now,
		}
		return tx.PutM6WorkerLease(out)
	})
	return out, err
}

func (s *CloudService) regionAllowedTx(tx Tx, region string) (bool, error) {
	policy, err := tx.LatestM6RegionPolicy()
	if errors.Is(err, m6supply.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var regions []string
	if err := json.Unmarshal([]byte(policy.AllowedRegions), &regions); err != nil {
		return false, err
	}
	for _, r := range regions {
		if r == region {
			return true, nil
		}
	}
	return false, nil
}

// RecordReceipt appends one runner outcome report. The runner must be
// registered; the receipt is a fact, never edited. Audited as
// cloud.reconciled only at the reconcile step — intake itself is silent
// telemetry, mirroring the call-log model.
func (s *CloudService) RecordReceipt(ctx context.Context, runnerID, taskID, outcome, resultDigest, usage string) (m6supply.RemoteReceipt, error) {
	if err := s.available(); err != nil {
		return m6supply.RemoteReceipt{}, err
	}
	if err := m6supply.ValidateReceiptInput(outcome, resultDigest, usage); err != nil {
		return m6supply.RemoteReceipt{}, err
	}
	var out m6supply.RemoteReceipt
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.GetM6CloudRunner(runnerID); err != nil {
			if errors.Is(err, m6supply.ErrNotFound) {
				return ErrRunnerNotFound
			}
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.RemoteReceipt{
			ID: ulid.Make().String(), TaskID: taskID, RunnerID: runnerID,
			Outcome: outcome, ResultDigest: resultDigest, Usage: usage,
			ReceivedAt: now, ReconcileState: m6supply.ReconcilePending,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		return tx.PutM6RemoteReceipt(out)
	})
	return out, err
}

// Reconcile draws one decision from a pending receipt and closes it
// (cloud.reconciled audit). outcome_unknown defaults to manual_review —
// an unexplained silence is never auto-accepted and never auto-rerun.
func (s *CloudService) Reconcile(ctx context.Context, receiptID string, expectedVersion int64, decision, reason string) (m6supply.ReconcileDecision, error) {
	if err := s.available(); err != nil {
		return m6supply.ReconcileDecision{}, err
	}
	if err := m6supply.ValidateReconcileInput(decision, reason); err != nil {
		return m6supply.ReconcileDecision{}, err
	}
	var out m6supply.ReconcileDecision
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		receipt, err := tx.GetM6RemoteReceipt(receiptID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrReceiptNotFound
		}
		if err != nil {
			return err
		}
		if receipt.Outcome == m6supply.ReceiptOutcomeUnknown && decision == m6supply.ReconcileAccepted {
			return errors.New("m6app: outcome_unknown cannot be auto-accepted; use manual_review after evidence")
		}
		now := s.clock.Now().UTC()
		if err := tx.SetM6ReceiptReconcileState(receiptID, expectedVersion, m6supply.ReconcileReconciled, now); err != nil {
			return err
		}
		out = m6supply.ReconcileDecision{
			ID: ulid.Make().String(), ReceiptID: receiptID, TaskID: receipt.TaskID,
			Decision: decision, Reason: reason, DecidedAt: now, CreatedAt: now,
		}
		if err := tx.PutM6ReconcileDecision(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "cloud.reconciled",
			AggregateID: receiptID, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				ReceiptID string `json:"receiptId"`
				TaskID    string `json:"taskId"`
				Outcome   string `json:"outcome"`
				Decision  string `json:"decision"`
			}{ReceiptID: receiptID, TaskID: receipt.TaskID, Outcome: receipt.Outcome, Decision: decision}),
		})
	})
	return out, err
}

// ListPendingReceipts answers the pending reconcile queue.
func (s *CloudService) ListPendingReceipts(ctx context.Context, limit int) ([]m6supply.RemoteReceipt, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	var out []m6supply.RemoteReceipt
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		var err error
		out, err = tx.ListM6PendingRemoteReceipts(limit)
		return err
	})
	return out, err
}
