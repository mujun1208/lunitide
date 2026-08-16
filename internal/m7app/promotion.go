// M7 slice 4 application service (T-7.4.x): the promotion saga. Promote
// drives one sealed package across an environment edge through
// requested -> policy_check -> approval_check -> migrating -> deploying ->
// validating -> succeeded; migration and deployment run through internal
// adapter ports that are deliberately NOT registered as bridge handlers
// (M7-MIG-001 - only the Promotion aggregate may call plan/apply/verify/
// rollback). Every step is idempotent on promotion_id + step + canonical
// intent digest, so an interrupted saga resumes without duplicate external
// effects (scenario 30). Rollback executes append-only attempts per
// dimension and freezes the environment on failure (M7-RBK-001).
package m7app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

var (
	// ErrPromotionNotFound: referenced promotion row missing.
	ErrPromotionNotFound = errors.New("m7app: promotion not found")
	// ErrPackageNotSealed: only sealed packages may be promoted.
	ErrPackageNotSealed = errors.New("m7app: package not sealed")
	// ErrIntentChanged: the same idempotency key arrived with a different
	// canonical intent (PRM-002 - freeze execution, never redeploy).
	ErrIntentChanged = errors.New("m7app: canonical intent changed for idempotency key")
	// ErrPolicyRejected: environment edge, release window or owner-confirm
	// policy check failed (PRM-003).
	ErrPolicyRejected = errors.New("m7app: promotion policy rejected")
	// ErrApprovalInvalid: missing/malformed/forged approval, or SoD violation
	// (approver must differ from requester and package author).
	ErrApprovalInvalid = errors.New("m7app: promotion approval invalid")
	// ErrApprovalExpired: the approval window has passed (re-approve).
	ErrApprovalExpired = errors.New("m7app: promotion approval expired")
	// ErrConcurrentPromotion: another saga is still active on the same
	// package+environment edge (PRM-001 - the original promotion answers).
	ErrConcurrentPromotion = errors.New("m7app: concurrent promotion active")
	// ErrMigrationFailed: the migration adapter failed (MIG-002 - enters
	// rollback).
	ErrMigrationFailed = errors.New("m7app: migration failed")
	// ErrDeploymentFailed: dispatch or health validation failed (M7-DEP-001
	// - auto rollback to the previous digest per policy).
	ErrDeploymentFailed = errors.New("m7app: deployment or validation failed")
	// ErrRollbackFailed: a rollback attempt failed or is RESULT_UNKNOWN -
	// the environment is frozen and manual disposal is required (RBK-001).
	ErrRollbackFailed = errors.New("m7app: rollback failed")
	// ErrOutcomeUnknown: an external step dispatched without a trusted
	// receipt; the saga parks in outcome_unknown pending reconciliation.
	ErrOutcomeUnknown = errors.New("m7app: promotion outcome unknown")
	// ErrIllegalPromotionTransition: the requested state change is not in
	// the canonical state machine.
	ErrIllegalPromotionTransition = errors.New("m7app: illegal promotion transition")
	// ErrRollbackNotAllowed: the promotion state cannot start a rollback.
	ErrRollbackNotAllowed = errors.New("m7app: rollback not allowed in this state")
)

// errApprovalPending: the prod approval payload has not arrived yet. The
// saga parks at approval_check so the caller can read the canonical intent
// digest, collect the SoD approval and replay the same requestId.
var errApprovalPending = errors.New("m7app: promotion approval pending")

// PromotionTx is the slice-4 single-writer transaction. It embeds the
// slice-3 package reads (sealed-package verification + revision author) -
// agentRuntimeTx satisfies the whole set.
type PromotionTx interface {
	PutPromotion(m7flow.Promotion) error
	GetPromotion(id string) (m7flow.Promotion, error)
	FindPromotionByIdempotencyKey(key string) (m7flow.Promotion, error)
	FindActivePromotion(packageID, toEnv string) (m7flow.Promotion, error)
	FindLastSucceededByPackage(packageID string) (m7flow.Promotion, error)
	FindLastSucceededByEnv(toEnv string) (m7flow.Promotion, error)
	UpdatePromotionState(id, from, to string, updatedAt time.Time) error
	PutMigrationExecution(m7flow.MigrationExecution) error
	FindMigrationExecution(promotionID string) (m7flow.MigrationExecution, error)
	ListMigrationExecutions(promotionID string) ([]m7flow.MigrationExecution, error)
	UpdateMigrationExecution(id, from, to, rollbackRef string) error
	PutDeployment(m7flow.Deployment) error
	FindDeployment(promotionID string) (m7flow.Deployment, error)
	ListDeployments(promotionID string) ([]m7flow.Deployment, error)
	UpdateDeploymentState(id, from, to, receipt string, startedAt, completedAt *time.Time) error
	PutRollbackAttempt(m7flow.RollbackAttempt) error
	ListRollbackAttempts(promotionID string) ([]m7flow.RollbackAttempt, error)
	UpdateRollbackAttempt(id, from, to, resultJSON string, completedAt *time.Time) error
	GetReleasePackage(id string) (m7flow.ReleasePackage, error)
	GetReleaseBlob(digest string) (string, error)
	GetCRRevision(id string) (m7flow.CRRevision, error)
}

// PromotionUnitOfWork is the slice-4 single-writer boundary.
type PromotionUnitOfWork interface {
	TransactPromotion(ctx context.Context, fn func(PromotionTx) error) error
}

// MigrationAdapter is the internal migration port (never a bridge handler).
// All calls must be idempotent keyed on promotionID + intent digest.
type MigrationAdapter interface {
	Plan(ctx context.Context, packageID, blobDigest, toEnv string) (planDigest, rollbackPlanDigest string, err error)
	Apply(ctx context.Context, promotionID, planDigest, intentDigest string) (rollbackRef string, err error)
	Verify(ctx context.Context, promotionID, planDigest string) error
	Rollback(ctx context.Context, promotionID, rollbackRef string) error
}

// DeploymentAdapter is the internal deployment port (never a bridge
// handler). Dispatch returns a trusted receipt digest; an untrusted result
// must answer ErrOutcomeUnknown, never a blind retry.
type DeploymentAdapter interface {
	Plan(ctx context.Context, packageID, blobDigest, toEnv string) (planDigest string, err error)
	Dispatch(ctx context.Context, promotionID, blobDigest, intentDigest string) (receipt string, err error)
	Verify(ctx context.Context, promotionID, receipt string) error
	Rollback(ctx context.Context, promotionID, previousBlobDigest string) error
}

// ── default local adapters ──────────────────────────────────────────────────

// LocalMigrationAdapter is the deterministic in-process migration adapter:
// plans are canonical digests, apply/verify are recorded no-ops that always
// succeed, rollback references are stable per promotion. Real environments
// inject an adapter talking to the actual migration runner.
type LocalMigrationAdapter struct{}

func planDigest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (LocalMigrationAdapter) Plan(_ context.Context, packageID, blobDigest, toEnv string) (string, string, error) {
	return planDigest("mig-plan", packageID, blobDigest, toEnv),
		planDigest("mig-rollback-plan", packageID, blobDigest, toEnv), nil
}

func (LocalMigrationAdapter) Apply(_ context.Context, promotionID, planDigest, _ string) (string, error) {
	return "mig-rollback:" + promotionID + ":" + planDigest[:16], nil
}

func (LocalMigrationAdapter) Verify(context.Context, string, string) error { return nil }

func (LocalMigrationAdapter) Rollback(context.Context, string, string) error { return nil }

// LocalDeploymentAdapter is the deterministic in-process deployment adapter.
type LocalDeploymentAdapter struct{}

func (LocalDeploymentAdapter) Plan(_ context.Context, packageID, blobDigest, toEnv string) (string, error) {
	return planDigest("dep-plan", packageID, blobDigest, toEnv), nil
}

func (LocalDeploymentAdapter) Dispatch(_ context.Context, promotionID, blobDigest, _ string) (string, error) {
	return planDigest("dep-receipt", promotionID, blobDigest), nil
}

func (LocalDeploymentAdapter) Verify(context.Context, string, string) error { return nil }

func (LocalDeploymentAdapter) Rollback(context.Context, string, string) error { return nil }

// ── service ─────────────────────────────────────────────────────────────────

// PromotionService implements release.promote / release.rollback /
// release.getPromotion (slice 4).
type PromotionService struct {
	uow    PromotionUnitOfWork
	clock  Clock
	signer ReleaseSigner
	mig    MigrationAdapter
	dep    DeploymentAdapter
}

func NewPromotionService(uow PromotionUnitOfWork) *PromotionService {
	return &PromotionService{
		uow: uow, clock: systemClock{}, signer: NewLocalMACSigner(),
		mig: LocalMigrationAdapter{}, dep: LocalDeploymentAdapter{},
	}
}

func (s *PromotionService) SetClock(c Clock) { s.clock = c }

// SetAdapters substitutes the migration/deployment ports (tests, real
// runtime adapters). The adapters stay internal to the aggregate.
func (s *PromotionService) SetAdapters(m MigrationAdapter, d DeploymentAdapter) {
	s.mig, s.dep = m, d
}

// PromoteInput is the release.promote command.
type PromoteInput struct {
	PackageID     string
	TargetEnv     string
	PolicyContext map[string]any
	RequestID     string
}

// promotionPolicy is the decoded policyContext payload.
type promotionPolicy struct {
	PolicyVersion     string
	ReleaseNotBefore  *time.Time
	ReleaseNotAfter   *time.Time
	OwnerConfirm      bool
	ApproverID        string
	ApprovedAt        string
	ApprovalExpiresAt string
	ApprovalHash      string
	RequestedBy       string
}

func decodePromotionPolicy(raw map[string]any) (promotionPolicy, error) {
	var p promotionPolicy
	if raw == nil {
		return p, fmt.Errorf("%w: policyContext required", ErrPolicyRejected)
	}
	p.RequestedBy, _ = raw["requestedBy"].(string)
	if len(p.RequestedBy) < 1 || len(p.RequestedBy) > 128 {
		return p, fmt.Errorf("%w: requestedBy required", ErrPolicyRejected)
	}
	p.PolicyVersion, _ = raw["policyVersion"].(string)
	if len(p.PolicyVersion) < 1 || len(p.PolicyVersion) > 64 {
		p.PolicyVersion = "m7-policy-v1"
	}
	if ow, ok := raw["releaseWindow"].(map[string]any); ok {
		for key, dst := range map[string]**time.Time{"notBefore": &p.ReleaseNotBefore, "notAfter": &p.ReleaseNotAfter} {
			v, _ := ow[key].(string)
			if v == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return p, fmt.Errorf("%w: releaseWindow.%s malformed", ErrPolicyRejected, key)
			}
			*dst = &t
		}
	}
	if oc, ok := raw["ownerConfirm"].(bool); ok {
		p.OwnerConfirm = oc
	}
	if ap, ok := raw["approval"].(map[string]any); ok {
		p.ApproverID, _ = ap["approverId"].(string)
		p.ApprovedAt, _ = ap["approvedAt"].(string)
		p.ApprovalExpiresAt, _ = ap["expiresAt"].(string)
		p.ApprovalHash, _ = ap["hash"].(string)
	}
	return p, nil
}

// Promote creates (or idempotently resumes) the promotion of one sealed
// package to the target environment and drives the saga as far as the
// adapters allow in this call.
func (s *PromotionService) Promote(ctx context.Context, in PromoteInput) (m7flow.Promotion, error) {
	if s == nil || s.uow == nil {
		return m7flow.Promotion{}, ErrServiceUnavailable
	}
	if len(in.RequestID) < 1 || len(in.RequestID) > 128 {
		return m7flow.Promotion{}, fmt.Errorf("%w: requestId required", ErrPolicyRejected)
	}
	if m7flow.EnvRank(in.TargetEnv) < 1 {
		return m7flow.Promotion{}, fmt.Errorf("%w: targetEnv invalid", ErrPolicyRejected)
	}
	policy, err := decodePromotionPolicy(in.PolicyContext)
	if err != nil {
		return m7flow.Promotion{}, err
	}
	var out m7flow.Promotion
	var sagaErr error // typed failure already recorded on a committed saga row
	err = s.uow.TransactPromotion(ctx, func(tx PromotionTx) error {
		prm, err := s.promoteTx(ctx, tx, in, policy)
		out = prm
		if err != nil && m7SagaRecorded(out, err) {
			sagaErr = err
			return nil // commit denied/expired/failed/rolled-back audit rows
		}
		return err
	})
	if err != nil {
		return m7flow.Promotion{}, err
	}
	if sagaErr != nil {
		return out, sagaErr
	}
	return out, nil
}

// m7SagaRecorded reports whether the error outcome was durably recorded on
// the saga row (denied / expired / failed / rolled-back terminal records).
// Those rows must survive the transaction so audits and replays observe the
// truth; the typed error still surfaces on the wire after the commit.
func m7SagaRecorded(prm m7flow.Promotion, err error) bool {
	if prm.ID == "" {
		return false
	}
	switch {
	case errors.Is(err, ErrPolicyRejected),
		errors.Is(err, ErrApprovalInvalid),
		errors.Is(err, ErrApprovalExpired),
		errors.Is(err, ErrMigrationFailed),
		errors.Is(err, ErrDeploymentFailed),
		errors.Is(err, ErrRollbackFailed),
		errors.Is(err, ErrOutcomeUnknown):
		return true
	}
	return false
}

func (s *PromotionService) promoteTx(ctx context.Context, tx PromotionTx, in PromoteInput, policy promotionPolicy) (m7flow.Promotion, error) {
	// Idempotent replay: same key resumes the same saga (scenario 19/30).
	// resumeExisting mutates the saga in place so the caller observes the
	// post-drive state, not the stale parked snapshot.
	if existing, err := tx.FindPromotionByIdempotencyKey(in.RequestID); err == nil {
		return existing, s.resumeExisting(ctx, tx, &existing, in)
	} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
		return m7flow.Promotion{}, err
	}

	// Package must exist, be sealed and verify (same digest everywhere).
	pkg, err := tx.GetReleasePackage(in.PackageID)
	if err != nil {
		return m7flow.Promotion{}, ErrPackageNotFound
	}
	if pkg.State != m7flow.PkgSealed {
		return m7flow.Promotion{}, ErrPackageNotSealed
	}
	blob, err := tx.GetReleaseBlob(pkg.BlobDigest)
	if err != nil || m7flow.SHA256Hex([]byte(blob)) != pkg.BlobDigest || !s.signer.Verify(blob, pkg.Signature) {
		return m7flow.Promotion{}, fmt.Errorf("%w: package blob/signature", ErrDigestMismatch)
	}

	// Environment edge: the package climbs none -> dev -> stage|prod from
	// the highest environment it already succeeded in.
	from := m7flow.EnvNone
	if last, err := tx.FindLastSucceededByPackage(in.PackageID); err == nil {
		from = last.ToEnv
	} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
		return m7flow.Promotion{}, err
	}
	if !m7flow.LegalPromotionEdge(from, in.TargetEnv) {
		return m7flow.Promotion{}, fmt.Errorf("%w: edge %s -> %s", ErrPolicyRejected, from, in.TargetEnv)
	}
	// One active saga per package+environment edge (PRM-001).
	if active, err := tx.FindActivePromotion(in.PackageID, in.TargetEnv); err == nil {
		return m7flow.Promotion{}, fmt.Errorf("%w: promotion %s still %s", ErrConcurrentPromotion, active.ID, active.State)
	} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
		return m7flow.Promotion{}, err
	}

	// Plans are read-only; their digests freeze into the canonical intent.
	migPlan, rbkPlan, err := s.mig.Plan(ctx, in.PackageID, pkg.BlobDigest, in.TargetEnv)
	if err != nil {
		return m7flow.Promotion{}, err
	}
	depPlan, err := s.dep.Plan(ctx, in.PackageID, pkg.BlobDigest, in.TargetEnv)
	if err != nil {
		return m7flow.Promotion{}, err
	}

	// Approval expiry joins the intent BEFORE the digest is computed - the
	// approval hash then covers exactly this digest (atomic boundary).
	expiryRFC := ""
	if in.TargetEnv == m7flow.EnvProd {
		if policy.ApprovalExpiresAt == "" {
			return m7flow.Promotion{}, fmt.Errorf("%w: approval.expiresAt required for prod", ErrApprovalInvalid)
		}
		exp, err := time.Parse(time.RFC3339, policy.ApprovalExpiresAt)
		if err != nil {
			return m7flow.Promotion{}, fmt.Errorf("%w: approval.expiresAt malformed", ErrApprovalInvalid)
		}
		if !s.clock.Now().UTC().Before(exp) {
			return m7flow.Promotion{}, ErrApprovalExpired
		}
		expiryRFC = policy.ApprovalExpiresAt
	}
	intentDigest := m7flow.CanonicalIntentDigest(m7flow.CanonicalIntent{
		PackageDigest: pkg.ID, ManifestDigest: pkg.ManifestDigest, BlobDigest: pkg.BlobDigest,
		FromEnv: from, ToEnv: in.TargetEnv,
		MigrationPlanDigest: migPlan, DeploymentPlanDigest: depPlan, RollbackPlanDigest: rbkPlan,
		PolicyVersion: policy.PolicyVersion, ApprovalExpiry: expiryRFC,
	})

	var approvalExpiry *time.Time
	if expiryRFC != "" {
		exp, _ := time.Parse(time.RFC3339, expiryRFC)
		utc := exp.UTC()
		approvalExpiry = &utc
	}
	now := s.clock.Now().UTC()
	prm := m7flow.Promotion{
		ID: ulid.Make().String(), PackageID: in.PackageID, FromEnv: from, ToEnv: in.TargetEnv,
		CanonicalIntentDigest: intentDigest, PolicyVersion: policy.PolicyVersion,
		ApprovalExpiry: approvalExpiry, State: m7flow.PrmRequested,
		IdempotencyKey: in.RequestID, RequestedBy: policy.RequestedBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.PutPromotion(prm); err != nil {
		return m7flow.Promotion{}, err
	}
	return prm, s.drive(ctx, tx, &prm, policy, pkg)
}

// resumeExisting continues an interrupted saga (scenario 30) or reports the
// terminal outcome of a replayed request.
func (s *PromotionService) resumeExisting(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion, in PromoteInput) error {
	switch prm.State {
	case m7flow.PrmDenied, m7flow.PrmExpired:
		return ErrPolicyRejected
	case m7flow.PrmFailed:
		return ErrDeploymentFailed
	case m7flow.PrmRollbackFailed:
		return ErrRollbackFailed
	case m7flow.PrmSucceeded, m7flow.PrmRolledBack:
		return nil // idempotent success
	}
	policy, err := decodePromotionPolicy(in.PolicyContext)
	if err != nil {
		return err
	}
	pkg, err := tx.GetReleasePackage(prm.PackageID)
	if err != nil {
		return ErrPackageNotFound
	}
	blob, err := tx.GetReleaseBlob(pkg.BlobDigest)
	if err != nil || m7flow.SHA256Hex([]byte(blob)) != pkg.BlobDigest || !s.signer.Verify(blob, pkg.Signature) {
		return fmt.Errorf("%w: package blob/signature", ErrDigestMismatch)
	}
	// PRM-002: the package (or any intent input) may have changed while the
	// saga was parked. Freeze the replay instead of redeploying.
	digest, err := s.intentDigest(ctx, tx, pkg, prm.ToEnv, policy)
	if err != nil {
		return err
	}
	if digest != prm.CanonicalIntentDigest {
		return ErrIntentChanged
	}
	return s.drive(ctx, tx, prm, policy, pkg)
}

// intentDigest recomputes the canonical intent digest from the CURRENT
// package, environment and plan facts; a replay digest that differs from
// the stored one means some intent input changed (PRM-002).
func (s *PromotionService) intentDigest(ctx context.Context, tx PromotionTx, pkg m7flow.ReleasePackage, targetEnv string, policy promotionPolicy) (string, error) {
	from := m7flow.EnvNone
	if last, err := tx.FindLastSucceededByPackage(pkg.ID); err == nil {
		from = last.ToEnv
	} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
		return "", err
	}
	migPlan, rbkPlan, err := s.mig.Plan(ctx, pkg.ID, pkg.BlobDigest, targetEnv)
	if err != nil {
		return "", err
	}
	depPlan, err := s.dep.Plan(ctx, pkg.ID, pkg.BlobDigest, targetEnv)
	if err != nil {
		return "", err
	}
	expiryRFC := ""
	if targetEnv == m7flow.EnvProd {
		expiryRFC = policy.ApprovalExpiresAt
	}
	return m7flow.CanonicalIntentDigest(m7flow.CanonicalIntent{
		PackageDigest: pkg.ID, ManifestDigest: pkg.ManifestDigest, BlobDigest: pkg.BlobDigest,
		FromEnv: from, ToEnv: targetEnv,
		MigrationPlanDigest: migPlan, DeploymentPlanDigest: depPlan, RollbackPlanDigest: rbkPlan,
		PolicyVersion: policy.PolicyVersion, ApprovalExpiry: expiryRFC,
	}), nil
}

// advance performs one legal state transition (fail-closed on illegal ones).
func (s *PromotionService) advance(tx PromotionTx, prm *m7flow.Promotion, to string) error {
	if !m7flow.LegalPromotionTransition(prm.State, to) {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalPromotionTransition, prm.State, to)
	}
	now := s.clock.Now().UTC()
	if err := tx.UpdatePromotionState(prm.ID, prm.State, to, now); err != nil {
		return err
	}
	prm.State, prm.UpdatedAt = to, now
	return nil
}

// drive walks the saga forward from the promotion's current state. Every
// external step is idempotent (promotionID + step + intent digest), so
// re-driving a parked saga is always safe.
func (s *PromotionService) drive(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion, policy promotionPolicy, pkg m7flow.ReleasePackage) error {
	switch prm.State {
	case m7flow.PrmRequested:
		if err := s.advance(tx, prm, m7flow.PrmPolicyCheck); err != nil {
			return err
		}
		fallthrough
	case m7flow.PrmPolicyCheck:
		if err := s.checkPolicy(prm, policy); err != nil {
			if errors.Is(err, ErrPolicyRejected) {
				_ = s.advance(tx, prm, m7flow.PrmDenied)
			}
			return err
		}
		if err := s.advance(tx, prm, m7flow.PrmApprovalCheck); err != nil {
			return err
		}
		fallthrough
	case m7flow.PrmApprovalCheck:
		if err := s.checkApproval(tx, prm, policy); err != nil {
			if errors.Is(err, errApprovalPending) {
				return nil // parked for the approval handshake
			}
			if errors.Is(err, ErrApprovalExpired) {
				_ = s.advance(tx, prm, m7flow.PrmExpired)
			} else if errors.Is(err, ErrApprovalInvalid) {
				_ = s.advance(tx, prm, m7flow.PrmDenied)
			}
			return err
		}
		if err := s.advance(tx, prm, m7flow.PrmMigrating); err != nil {
			return err
		}
		fallthrough
	case m7flow.PrmMigrating:
		if err := s.runMigration(ctx, tx, prm, pkg); err != nil {
			return s.failAndRollback(ctx, tx, prm, err)
		}
		if err := s.advance(tx, prm, m7flow.PrmDeploying); err != nil {
			return err
		}
		fallthrough
	case m7flow.PrmDeploying:
		if err := s.runDeployment(ctx, tx, prm, pkg); err != nil {
			return s.failAndRollback(ctx, tx, prm, err)
		}
		if err := s.advance(tx, prm, m7flow.PrmValidating); err != nil {
			return err
		}
		fallthrough
	case m7flow.PrmValidating:
		if err := s.runValidation(ctx, tx, prm); err != nil {
			return s.failAndRollback(ctx, tx, prm, err)
		}
		return s.advance(tx, prm, m7flow.PrmSucceeded)
	case m7flow.PrmOutcomeUnknown:
		return ErrOutcomeUnknown
	case m7flow.PrmRollingBack:
		return s.executeRollback(ctx, tx, prm)
	}
	return nil
}

// checkPolicy enforces the release-window / owner-confirm rules (PRM-003).
func (s *PromotionService) checkPolicy(prm *m7flow.Promotion, policy promotionPolicy) error {
	now := s.clock.Now().UTC()
	if policy.ReleaseNotBefore != nil && now.Before(*policy.ReleaseNotBefore) ||
		policy.ReleaseNotAfter != nil && !now.Before(*policy.ReleaseNotAfter) {
		return fmt.Errorf("%w: outside release window", ErrPolicyRejected)
	}
	if prm.ToEnv == m7flow.EnvProd && prm.FromEnv == m7flow.EnvDev && !policy.OwnerConfirm {
		return fmt.Errorf("%w: dev -> prod requires ownerConfirm", ErrPolicyRejected)
	}
	return nil
}

// checkApproval verifies the SoD approval for prod promotions: approver must
// differ from the requester and the package author, the hash must bind the
// stored canonical intent digest and the window must still be open.
func (s *PromotionService) checkApproval(tx PromotionTx, prm *m7flow.Promotion, policy promotionPolicy) error {
	if prm.ToEnv != m7flow.EnvProd {
		return nil
	}
	if policy.ApproverID == "" || policy.ApprovedAt == "" || policy.ApprovalHash == "" {
		return errApprovalPending
	}
	if _, err := time.Parse(time.RFC3339, policy.ApprovedAt); err != nil {
		return fmt.Errorf("%w: approval.approvedAt malformed", ErrApprovalInvalid)
	}
	if prm.ApprovalExpiry != nil && !s.clock.Now().UTC().Before(*prm.ApprovalExpiry) {
		return ErrApprovalExpired
	}
	if policy.ApproverID == prm.RequestedBy {
		return fmt.Errorf("%w: approver equals requester", ErrApprovalInvalid)
	}
	pkg, err := tx.GetReleasePackage(prm.PackageID)
	if err != nil {
		return ErrPackageNotFound
	}
	rev, err := tx.GetCRRevision(pkg.CRRevisionID)
	if err != nil {
		return ErrRevisionNotFound
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(rev.ManifestJSON), &manifest); err != nil {
		return err
	}
	if author, _ := manifest["authorId"].(string); author == policy.ApproverID {
		return fmt.Errorf("%w: approver equals package author", ErrApprovalInvalid)
	}
	want := m7flow.ApprovalHash(prm.CanonicalIntentDigest, policy.ApproverID, policy.ApprovedAt, policy.ApprovalExpiresAt)
	if want != policy.ApprovalHash {
		return fmt.Errorf("%w: approval hash does not bind canonical intent", ErrApprovalInvalid)
	}
	return nil
}

// runMigration executes the idempotent plan->apply->verify cycle and records
// the migration_executions row (MIG-002 on failure).
func (s *PromotionService) runMigration(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion, pkg m7flow.ReleasePackage) error {
	mig, err := tx.FindMigrationExecution(prm.ID)
	if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		planDigest, _, err := s.mig.Plan(ctx, prm.PackageID, pkg.BlobDigest, prm.ToEnv)
		if err != nil {
			return err
		}
		mig = m7flow.MigrationExecution{
			ID: ulid.Make().String(), PromotionID: prm.ID,
			PlanDigest: planDigest, State: m7flow.MigPlanned, CreatedAt: s.clock.Now().UTC(),
		}
		if err := tx.PutMigrationExecution(mig); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if mig.State == m7flow.MigVerified {
		return nil
	}
	if mig.State == m7flow.MigPlanned {
		ref, err := s.mig.Apply(ctx, prm.ID, mig.PlanDigest, prm.CanonicalIntentDigest)
		if err != nil {
			_ = tx.UpdateMigrationExecution(mig.ID, mig.State, m7flow.MigFailed, mig.RollbackRef)
			return fmt.Errorf("%w: %v", ErrMigrationFailed, err)
		}
		if err := tx.UpdateMigrationExecution(mig.ID, mig.State, m7flow.MigApplied, ref); err != nil {
			return err
		}
		mig.State, mig.RollbackRef = m7flow.MigApplied, ref
	}
	if err := s.mig.Verify(ctx, prm.ID, mig.PlanDigest); err != nil {
		_ = tx.UpdateMigrationExecution(mig.ID, mig.State, m7flow.MigFailed, mig.RollbackRef)
		return fmt.Errorf("%w: verify: %v", ErrMigrationFailed, err)
	}
	return tx.UpdateMigrationExecution(mig.ID, mig.State, m7flow.MigVerified, mig.RollbackRef)
}

// runDeployment dispatches the package blob and records the trusted receipt
// (DEP-001 on failure; ErrOutcomeUnknown parks the saga).
func (s *PromotionService) runDeployment(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion, pkg m7flow.ReleasePackage) error {
	dep, err := tx.FindDeployment(prm.ID)
	if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		now := s.clock.Now().UTC()
		dep = m7flow.Deployment{
			ID: ulid.Make().String(), PromotionID: prm.ID, TargetEnv: prm.ToEnv,
			State: m7flow.DepPending, StartedAt: &now,
		}
		if err := tx.PutDeployment(dep); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if dep.State == m7flow.DepSucceeded {
		return nil
	}
	receipt, err := s.dep.Dispatch(ctx, prm.ID, pkg.BlobDigest, prm.CanonicalIntentDigest)
	if err != nil {
		if errors.Is(err, ErrOutcomeUnknown) {
			now := s.clock.Now().UTC()
			_ = tx.UpdateDeploymentState(dep.ID, dep.State, m7flow.DepOutcomeUnknown, "", nil, &now)
			_ = s.advance(tx, prm, m7flow.PrmOutcomeUnknown)
			return ErrOutcomeUnknown
		}
		now := s.clock.Now().UTC()
		_ = tx.UpdateDeploymentState(dep.ID, dep.State, m7flow.DepFailed, dep.ReceiptJSON, nil, &now)
		return fmt.Errorf("%w: dispatch: %v", ErrDeploymentFailed, err)
	}
	now := s.clock.Now().UTC()
	return tx.UpdateDeploymentState(dep.ID, dep.State, m7flow.DepSucceeded, receipt, &now, &now)
}

// runValidation performs the post-deploy health check (DEP-001).
func (s *PromotionService) runValidation(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion) error {
	dep, err := tx.FindDeployment(prm.ID)
	if err != nil {
		return err
	}
	if err := s.dep.Verify(ctx, prm.ID, dep.ReceiptJSON); err != nil {
		return fmt.Errorf("%w: health check: %v", ErrDeploymentFailed, err)
	}
	return nil
}

// failAndRollback moves a failed saga into the rollback path: the original
// typed error is returned after the rollback outcome is recorded.
func (s *PromotionService) failAndRollback(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion, cause error) error {
	if errors.Is(cause, ErrOutcomeUnknown) {
		return cause // parked: reconciler decides, no blind rollback
	}
	_ = s.advance(tx, prm, m7flow.PrmFailed)
	if err := s.startRollback(ctx, tx, prm, "saga-failure"); err != nil {
		return err
	}
	if err := s.executeRollback(ctx, tx, prm); err != nil {
		return err
	}
	return cause
}

// RollbackInput is the release.rollback command.
type RollbackInput struct {
	PromotionID string
	Reason      string
	RequestID   string
	OperatorID  string
}

// Rollback starts (or resumes) the rollback of a promotion. Attempts are
// append-only (RBK-002); any failure freezes the environment (RBK-001).
func (s *PromotionService) Rollback(ctx context.Context, in RollbackInput) (m7flow.Promotion, []m7flow.RollbackAttempt, error) {
	if s == nil || s.uow == nil {
		return m7flow.Promotion{}, nil, ErrServiceUnavailable
	}
	if len(in.PromotionID) < 1 || len(in.Reason) < 1 || len(in.Reason) > 2000 {
		return m7flow.Promotion{}, nil, fmt.Errorf("%w: promotionId/reason required", ErrPackageInvalid)
	}
	var prm m7flow.Promotion
	var sagaErr error // typed failure already recorded on a committed saga row
	err := s.uow.TransactPromotion(ctx, func(tx PromotionTx) error {
		var err error
		prm, err = tx.GetPromotion(in.PromotionID)
		if err != nil {
			return ErrPromotionNotFound
		}
		operator := in.OperatorID
		if operator == "" {
			operator = prm.RequestedBy
		}
		switch prm.State {
		case m7flow.PrmRolledBack:
			return nil // idempotent
		case m7flow.PrmRollingBack:
		err = s.executeRollbackWith(ctx, tx, &prm, operator)
		case m7flow.PrmSucceeded, m7flow.PrmFailed, m7flow.PrmOutcomeUnknown, m7flow.PrmManual:
			if err := s.startRollback(ctx, tx, &prm, in.Reason); err != nil {
				return err
			}
		err = s.executeRollbackWith(ctx, tx, &prm, operator)
		default:
			return fmt.Errorf("%w: state %s", ErrRollbackNotAllowed, prm.State)
		}
		// RBK-001: the failed attempt and the frozen state are durable
		// audit facts - commit them, then surface the typed error.
		if errors.Is(err, ErrRollbackFailed) {
			sagaErr = err
			return nil
		}
		return err
	})
	if err != nil {
		return m7flow.Promotion{}, nil, err
	}
	attempts, aerr := s.ListRollbackAttempts(ctx, in.PromotionID)
	if sagaErr != nil {
		return prm, attempts, sagaErr
	}
	return prm, attempts, aerr
}

// startRollback opens pending attempts for every dimension that applies.
func (s *PromotionService) startRollback(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion, reason string) error {
	if err := s.advance(tx, prm, m7flow.PrmRollingBack); err != nil {
		return err
	}
	existing, err := tx.ListRollbackAttempts(prm.ID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil // resumed saga keeps its recorded attempts
	}
	pkg, err := tx.GetReleasePackage(prm.PackageID)
	if err != nil {
		return ErrPackageNotFound
	}
	_, rbkPlan, err := s.mig.Plan(ctx, prm.PackageID, pkg.BlobDigest, prm.ToEnv)
	if err != nil {
		return err
	}
	dimensions := []string{m7flow.RbkBinary}
	if _, err := tx.FindMigrationExecution(prm.ID); err == nil {
		dimensions = append(dimensions, m7flow.RbkSchema)
	}
	now := s.clock.Now().UTC()
	for _, dim := range dimensions {
		attempt := m7flow.RollbackAttempt{
			ID: ulid.Make().String(), PromotionID: prm.ID, Dimension: dim,
			State: m7flow.RbkPending, PlanDigest: rbkPlan, OperatorID: prm.RequestedBy,
			ResultJSON: mustJSON(map[string]string{"reason": reason}), CreatedAt: now,
		}
		if err := tx.PutRollbackAttempt(attempt); err != nil {
			return err
		}
	}
	return nil
}

// executeRollback drives the pending attempts with the promotion requester
// as operator.
func (s *PromotionService) executeRollback(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion) error {
	return s.executeRollbackWith(ctx, tx, prm, prm.RequestedBy)
}

func (s *PromotionService) executeRollbackWith(ctx context.Context, tx PromotionTx, prm *m7flow.Promotion, operator string) error {
	attempts, err := tx.ListRollbackAttempts(prm.ID)
	if err != nil {
		return err
	}
	if len(attempts) == 0 {
		if err := s.startRollback(ctx, tx, prm, "manual"); err != nil {
			return err
		}
		if attempts, err = tx.ListRollbackAttempts(prm.ID); err != nil {
			return err
		}
	}
	previousDigest := ""
	if prev, err := tx.FindLastSucceededByEnv(prm.ToEnv); err == nil && prev.ID != prm.ID {
		if prevPkg, err := tx.GetReleasePackage(prev.PackageID); err == nil {
			previousDigest = prevPkg.BlobDigest
		}
	}
	var mig m7flow.MigrationExecution
	hasMig := false
	if m, err := tx.FindMigrationExecution(prm.ID); err == nil {
		mig, hasMig = m, true
	}
	for _, at := range attempts {
		if at.State == m7flow.RbkSucceeded || at.State == m7flow.RbkFailed {
			continue
		}
		if err := tx.UpdateRollbackAttempt(at.ID, at.State, m7flow.RbkRunning, at.ResultJSON, nil); err != nil {
			return err
		}
		var execErr error
		switch at.Dimension {
		case m7flow.RbkBinary:
			execErr = s.dep.Rollback(ctx, prm.ID, previousDigest)
		case m7flow.RbkSchema:
			if hasMig {
				execErr = s.mig.Rollback(ctx, prm.ID, mig.RollbackRef)
			}
		default:
			// data/external compensations are human-driven; record as
			// failed so the environment freezes for manual disposal.
			execErr = fmt.Errorf("dimension %s requires manual compensation", at.Dimension)
		}
		finishing := s.clock.Now().UTC()
		if execErr != nil {
			_ = tx.UpdateRollbackAttempt(at.ID, m7flow.RbkRunning, m7flow.RbkFailed,
				mustJSON(map[string]string{"error": execErr.Error()}), &finishing)
			_ = s.advance(tx, prm, m7flow.PrmRollbackFailed)
			return fmt.Errorf("%w: %s: %v", ErrRollbackFailed, at.Dimension, execErr)
		}
		if err := tx.UpdateRollbackAttempt(at.ID, m7flow.RbkRunning, m7flow.RbkSucceeded,
			mustJSON(map[string]string{"operator": operator}), &finishing); err != nil {
			return err
		}
	}
	return s.advance(tx, prm, m7flow.PrmRolledBack)
}

// ListRollbackAttempts answers the attempts of one promotion.
func (s *PromotionService) ListRollbackAttempts(ctx context.Context, promotionID string) ([]m7flow.RollbackAttempt, error) {
	var out []m7flow.RollbackAttempt
	err := s.uow.TransactPromotion(ctx, func(tx PromotionTx) error {
		var err error
		out, err = tx.ListRollbackAttempts(promotionID)
		return err
	})
	return out, err
}

// ── read projection ─────────────────────────────────────────────────────────

// TimelineStep is one entry of the promotion timeline projection.
type TimelineStep struct {
	Step        string `json:"step"`
	Actor       string `json:"actor"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
	AuditID     string `json:"auditId"`
}

// PromotionView is the release.getPromotion projection (canonical 15-state).
type PromotionView struct {
	Promotion        m7flow.Promotion         `json:"promotion"`
	Timeline         []TimelineStep           `json:"timeline"`
	Migrations       []m7flow.MigrationExecution `json:"migrations"`
	Deployments      []m7flow.Deployment      `json:"deployments"`
	RollbackAttempts []m7flow.RollbackAttempt `json:"rollbackAttempts"`
}

// GetPromotion renders the release.getPromotion projection.
func (s *PromotionService) GetPromotion(ctx context.Context, promotionID string) (PromotionView, error) {
	if s == nil || s.uow == nil {
		return PromotionView{}, ErrServiceUnavailable
	}
	var view PromotionView
	err := s.uow.TransactPromotion(ctx, func(tx PromotionTx) error {
		prm, err := tx.GetPromotion(promotionID)
		if err != nil {
			return ErrPromotionNotFound
		}
		view.Promotion = prm
		rfc := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
		nullable := func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return rfc(*t)
		}
		view.Timeline = append(view.Timeline, TimelineStep{
			Step: "requested", Actor: prm.RequestedBy,
			StartedAt: rfc(prm.CreatedAt), CompletedAt: rfc(prm.CreatedAt), AuditID: prm.ID,
		})
		if view.Migrations, err = tx.ListMigrationExecutions(prm.ID); err != nil {
			return err
		}
		for _, m := range view.Migrations {
			view.Timeline = append(view.Timeline, TimelineStep{
				Step: "migrating", Actor: "system:migration",
				StartedAt: rfc(m.CreatedAt), CompletedAt: rfc(m.CreatedAt), AuditID: m.ID,
			})
		}
		if view.Deployments, err = tx.ListDeployments(prm.ID); err != nil {
			return err
		}
		for _, d := range view.Deployments {
			view.Timeline = append(view.Timeline, TimelineStep{
				Step: "deploying", Actor: "system:deployment",
				StartedAt: nullable(d.StartedAt), CompletedAt: nullable(d.CompletedAt), AuditID: d.ID,
			})
		}
		if view.RollbackAttempts, err = tx.ListRollbackAttempts(prm.ID); err != nil {
			return err
		}
		for _, r := range view.RollbackAttempts {
			view.Timeline = append(view.Timeline, TimelineStep{
				Step: "rolling_back", Actor: r.OperatorID,
				StartedAt: rfc(r.CreatedAt), CompletedAt: nullable(r.CompletedAt), AuditID: r.ID,
			})
		}
		view.Timeline = append(view.Timeline, TimelineStep{
			Step: prm.State, Actor: prm.RequestedBy,
			StartedAt: rfc(prm.CreatedAt), CompletedAt: rfc(prm.UpdatedAt), AuditID: prm.ID,
		})
		return nil
	})
	if err != nil {
		return PromotionView{}, err
	}
	return view, nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}