package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

// ── scripted adapters ───────────────────────────────────────────────────────

// scriptedMigration embeds the deterministic local adapter (so Plan digests
// stay stable across replays) and injects failures at chosen steps.
type scriptedMigration struct {
	m7app.LocalMigrationAdapter
	planErr       error
	applyFailures int // first N Apply calls fail
	applyCalls    int
	rollbackErr   error
	rollbackCalls int
}

func (m *scriptedMigration) Plan(ctx context.Context, pkg, blob, env string) (string, string, error) {
	if m.planErr != nil {
		return "", "", m.planErr
	}
	return m.LocalMigrationAdapter.Plan(ctx, pkg, blob, env)
}

func (m *scriptedMigration) Apply(ctx context.Context, promotionID, plan, intent string) (string, error) {
	m.applyCalls++
	if m.applyCalls <= m.applyFailures {
		return "", fmt.Errorf("apply failure #%d", m.applyCalls)
	}
	return m.LocalMigrationAdapter.Apply(ctx, promotionID, plan, intent)
}

func (m *scriptedMigration) Rollback(ctx context.Context, promotionID, ref string) error {
	m.rollbackCalls++
	if m.rollbackErr != nil {
		return m.rollbackErr
	}
	return m.LocalMigrationAdapter.Rollback(ctx, promotionID, ref)
}

// scriptedDeployment embeds the local adapter and injects dispatch/verify
// failures; it records the previous digest handed to Rollback (scenario 26).
type scriptedDeployment struct {
	m7app.LocalDeploymentAdapter
	dispatchFailures int // first N Dispatch calls answer ErrOutcomeUnknown
	dispatchCalls    int
	verifyErr        error
	verifyCalls      int
	rollbackErr      error
	rollbackCalls    int
	lastRollbackArg  string
}

func (d *scriptedDeployment) Dispatch(ctx context.Context, promotionID, blob, intent string) (string, error) {
	d.dispatchCalls++
	if d.dispatchCalls <= d.dispatchFailures {
		return "", m7app.ErrOutcomeUnknown
	}
	return d.LocalDeploymentAdapter.Dispatch(ctx, promotionID, blob, intent)
}

func (d *scriptedDeployment) Verify(ctx context.Context, promotionID, receipt string) error {
	d.verifyCalls++
	if d.verifyErr != nil {
		return d.verifyErr
	}
	return d.LocalDeploymentAdapter.Verify(ctx, promotionID, receipt)
}

func (d *scriptedDeployment) Rollback(ctx context.Context, promotionID, previous string) error {
	d.rollbackCalls++
	d.lastRollbackArg = previous
	if d.rollbackErr != nil {
		return d.rollbackErr
	}
	return d.LocalDeploymentAdapter.Rollback(ctx, promotionID, previous)
}

// ── harness ─────────────────────────────────────────────────────────────────

type promotionHarness struct {
	store   *sqlite.Store
	repo    *sqlite.AgentRuntimeRepository
	release *m7app.ReleaseService
	trace   *m7app.TraceService
	reviews *m7app.ReviewService
	promo   *m7app.PromotionService
	mig     *scriptedMigration
	dep     *scriptedDeployment
}

func newPromotionHarness(t *testing.T) *promotionHarness {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "m7prm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	traceSvc := m7app.NewTraceService(repo)
	h := &promotionHarness{
		store:   store,
		repo:    repo,
		release: m7app.NewReleaseService(repo),
		trace:   traceSvc,
		reviews: m7app.NewReviewService(repo, traceSvc),
		promo:   m7app.NewPromotionService(repo),
		mig:     &scriptedMigration{},
		dep:     &scriptedDeployment{},
	}
	h.promo.SetAdapters(h.mig, h.dep)
	return h
}

// sealedPackage creates one approved revision and seals it into a package.
func (h *promotionHarness) sealedPackage(t *testing.T, author, summary string) m7flow.ReleasePackage {
	t.Helper()
	ctx := context.Background()
	rev, err := h.release.CreateRevision(ctx, "CR-PRM-"+summary, map[string]any{
		"authorId": author,
		"summary":  summary,
		"members":  []any{map[string]any{"name": "a.bin", "size": 3, "sha256": strings.Repeat("aa", 32)}},
		"sbom":     map[string]any{"format": "cyclonedx", "digest": strings.Repeat("bb", 32)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.reviews.SubmitReview(ctx, m7flow.Review{
		SubjectType: "cr_revision", SubjectID: rev.ID, Verdict: m7flow.VerdictApprove,
		ReviewerID: "rev-1", Reason: "ok",
	}, author); err != nil {
		t.Fatal(err)
	}
	pkg, err := h.release.BuildPackage(ctx, rev.ID, rev.Digest)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func basePolicy(requestedBy string) map[string]any {
	return map[string]any{"requestedBy": requestedBy}
}

// ── scenario 18/23: dev -> stage -> prod carries the same blob digest ──────

func TestPromotionSagaHappyPathDev(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "happy-dev")

	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prm.State != m7flow.PrmSucceeded || prm.FromEnv != m7flow.EnvNone {
		t.Fatalf("want succeeded from none, got %s from %s", prm.State, prm.FromEnv)
	}
	if h.mig.applyCalls != 1 || h.dep.dispatchCalls != 1 {
		t.Fatalf("want exactly one migration+dispatch, got %d/%d", h.mig.applyCalls, h.dep.dispatchCalls)
	}
	view, err := h.promo.GetPromotion(ctx, prm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Migrations) != 1 || view.Migrations[0].State != m7flow.MigVerified {
		t.Fatalf("migration not verified: %+v", view.Migrations)
	}
	if len(view.Deployments) != 1 || view.Deployments[0].State != m7flow.DepSucceeded {
		t.Fatalf("deployment not succeeded: %+v", view.Deployments)
	}
	if len(view.Timeline) < 4 {
		t.Fatalf("timeline too short: %+v", view.Timeline)
	}
}

func TestPromotionSagaClimbsToProdViaApprovalHandshake(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "climb")

	// Leg 1: dev (no approval needed).
	if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-dev",
	}); err != nil {
		t.Fatal(err)
	}
	// Leg 2: stage.
	if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvStage,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-stage",
	}); err != nil {
		t.Fatal(err)
	}
	// Leg 3: prod parks at approval_check awaiting the SoD approval.
	expiry := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	prodPolicy := map[string]any{
		"requestedBy": "u-rel-1",
		"approval":    map[string]any{"expiresAt": expiry},
	}
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: prodPolicy, RequestID: "prm-prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prm.State != m7flow.PrmApprovalCheck {
		t.Fatalf("prod must park at approval_check, got %s", prm.State)
	}
	if h.dep.dispatchCalls != 2 {
		t.Fatalf("prod must not dispatch before approval: %d calls", h.dep.dispatchCalls)
	}
	// The approver signs the canonical intent digest read from the projection.
	view, err := h.promo.GetPromotion(ctx, prm.ID)
	if err != nil {
		t.Fatal(err)
	}
	approvedAt := time.Now().UTC().Format(time.RFC3339)
	prodPolicy["approval"] = map[string]any{
		"approverId": "u-approver-9",
		"approvedAt": approvedAt,
		"expiresAt":  expiry,
		"hash": m7flow.ApprovalHash(view.Promotion.CanonicalIntentDigest,
			"u-approver-9", approvedAt, expiry),
	}
	done, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: prodPolicy, RequestID: "prm-prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.State != m7flow.PrmSucceeded {
		t.Fatalf("prod promotion want succeeded, got %s", done.State)
	}
	// Scenario 18: every leg promoted the identical package blob.
	if view.Promotion.PackageID != pkg.ID || done.PackageID != pkg.ID {
		t.Fatal("legs must promote the same package")
	}
}

// ── scenario 19: duplicate request_id executes exactly once ────────────────

func TestPromotionSagaIdempotentReplay(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "idem")

	first, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-once",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-once",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.State != m7flow.PrmSucceeded {
		t.Fatalf("replay must return the original: %s/%s vs %s/%s",
			first.ID, first.State, second.ID, second.State)
	}
	if h.mig.applyCalls != 1 {
		t.Fatalf("replay must not re-apply migration: %d calls", h.mig.applyCalls)
	}
	view, _ := h.promo.GetPromotion(ctx, first.ID)
	if len(view.Migrations) != 1 {
		t.Fatalf("want exactly one migration execution, got %d", len(view.Migrations))
	}
}

// ── scenario 20: intent change freezes the replay (PRM-002) ────────────────

func TestPromotionSagaIntentChangeFreezesReplay(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "freeze")
	// Promote to dev+stage so the prod leg is stage -> prod.
	for _, env := range []string{m7flow.EnvDev, m7flow.EnvStage} {
		if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
			PackageID: pkg.ID, TargetEnv: env,
			PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-f-" + env,
		}); err != nil {
			t.Fatal(err)
		}
	}
	expiry := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	policy := map[string]any{
		"requestedBy": "u-rel-1",
		"approval":    map[string]any{"expiresAt": expiry},
	}
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: policy, RequestID: "prm-f-prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prm.State != m7flow.PrmApprovalCheck {
		t.Fatalf("want parked approval_check, got %s", prm.State)
	}
	migCalls := h.mig.applyCalls

	// The replay arrives with a different policy version: the canonical
	// intent changed, so execution freezes (never redeploys).
	changed := map[string]any{
		"requestedBy":   "u-rel-1",
		"policyVersion": "m7-policy-v9",
		"approval":      map[string]any{"expiresAt": expiry},
	}
	_, err = h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: changed, RequestID: "prm-f-prod",
	})
	if !errors.Is(err, m7app.ErrIntentChanged) {
		t.Fatalf("want ErrIntentChanged, got %v", err)
	}
	if h.mig.applyCalls != migCalls {
		t.Fatal("frozen replay must not execute new external steps")
	}
	frozen, err := h.promo.GetPromotion(ctx, prm.ID)
	if err != nil || frozen.Promotion.State != m7flow.PrmApprovalCheck {
		t.Fatalf("saga must stay parked, got %s err=%v", frozen.Promotion.State, err)
	}
}

// ── scenario 21: release-window rejection is recorded as denied ────────────

func TestPromotionSagaWindowRejectionRecordsDenied(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "window")

	policy := map[string]any{
		"requestedBy": "u-rel-1",
		"releaseWindow": map[string]any{
			"notBefore": time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		},
	}
	_, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: policy, RequestID: "prm-win",
	})
	if !errors.Is(err, m7app.ErrPolicyRejected) {
		t.Fatalf("want ErrPolicyRejected, got %v", err)
	}
	// The denied saga row survives for audit: the replay answers from the
	// committed denied record.
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: policy, RequestID: "prm-win",
	})
	if !errors.Is(err, m7app.ErrPolicyRejected) {
		t.Fatalf("replay of denied want ErrPolicyRejected, got %v", err)
	}
	if prm.State != m7flow.PrmDenied {
		t.Fatalf("denied saga must persist, got %s", prm.State)
	}
}

// ── scenario 24: migration failure never deploys and rolls back ────────────

func TestPromotionSagaMigrationFailureRollsBack(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "migfail")
	h.mig.applyFailures = 1

	_, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-mf",
	})
	if !errors.Is(err, m7app.ErrMigrationFailed) {
		t.Fatalf("want ErrMigrationFailed, got %v", err)
	}
	if h.dep.dispatchCalls != 0 {
		t.Fatalf("failed migration must never dispatch, got %d", h.dep.dispatchCalls)
	}
	// The saga rolled back: read it back through the idempotent replay.
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-mf",
	})
	if err != nil {
		t.Fatalf("replay of rolled-back saga failed: %v", err)
	}
	if prm.State != m7flow.PrmRolledBack {
		t.Fatalf("want rolled_back, got %s", prm.State)
	}
	detail, err := h.promo.GetPromotion(ctx, prm.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A migration execution row exists, so rollback covers binary+schema.
	if len(detail.RollbackAttempts) != 2 {
		t.Fatalf("want binary+schema rollback attempts, got %+v", detail.RollbackAttempts)
	}
	for _, at := range detail.RollbackAttempts {
		if at.State != m7flow.RbkSucceeded {
			t.Fatalf("rollback attempt not succeeded: %+v", at)
		}
	}
	if len(detail.Migrations) != 1 || detail.Migrations[0].State != m7flow.MigFailed {
		t.Fatalf("migration execution must record failed: %+v", detail.Migrations)
	}
}

// ── scenario 25: undeclared irreversible migration fails policy pre-check ──

func TestPromotionSagaIrreversiblePlanRejected(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "irreversible")
	h.mig.planErr = fmt.Errorf("%w: irreversible migration without declared rollback", m7app.ErrPolicyRejected)

	_, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-irr",
	})
	if !errors.Is(err, m7app.ErrPolicyRejected) {
		t.Fatalf("want ErrPolicyRejected, got %v", err)
	}
	if h.mig.applyCalls != 0 || h.dep.dispatchCalls != 0 {
		t.Fatal("policy pre-check failure must leave zero external effects")
	}
}

// ── scenario 26: health-check failure auto-rolls back to previous digest ───

func TestPromotionSagaHealthFailureRollsBackToPreviousDigest(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkgA := h.sealedPackage(t, "author-1", "pkg-a")
	pkgB := h.sealedPackage(t, "author-2", "pkg-b")
	if pkgA.BlobDigest == pkgB.BlobDigest {
		t.Fatal("packages must carry distinct blob digests")
	}
	// pkgA proves itself in dev, then succeeds in stage.
	for _, env := range []string{m7flow.EnvDev, m7flow.EnvStage} {
		if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
			PackageID: pkgA.ID, TargetEnv: env,
			PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-a-" + env,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// pkgB climbs to dev, then fails health validation in stage.
	if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkgB.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-2"), RequestID: "prm-b-dev",
	}); err != nil {
		t.Fatal(err)
	}
	h.dep.verifyErr = errors.New("health probe red")
	_, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkgB.ID, TargetEnv: m7flow.EnvStage,
		PolicyContext: basePolicy("u-rel-2"), RequestID: "prm-b-stage",
	})
	if !errors.Is(err, m7app.ErrDeploymentFailed) {
		t.Fatalf("want ErrDeploymentFailed, got %v", err)
	}
	if h.dep.lastRollbackArg != pkgA.BlobDigest {
		t.Fatalf("auto rollback must target the previous digest %s, got %q",
			pkgA.BlobDigest, h.dep.lastRollbackArg)
	}
	// Replay observes the rolled-back terminal state.
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkgB.ID, TargetEnv: m7flow.EnvStage,
		PolicyContext: basePolicy("u-rel-2"), RequestID: "prm-b-stage",
	})
	if err != nil || prm.State != m7flow.PrmRolledBack {
		t.Fatalf("want rolled_back saga, got %s err=%v", prm.State, err)
	}
}

// ── scenario 30: interrupted saga parks without duplicate work ─────────────

func TestPromotionSagaOutcomeUnknownParksWithoutDuplicateWork(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "interrupt")
	h.dep.dispatchFailures = 1 // first dispatch answers RESULT_UNKNOWN

	_, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-int",
	})
	if !errors.Is(err, m7app.ErrOutcomeUnknown) {
		t.Fatalf("want ErrOutcomeUnknown, got %v", err)
	}
	migCalls := h.mig.applyCalls
	if migCalls != 1 {
		t.Fatalf("want exactly one migration apply, got %d", migCalls)
	}
	// The replay stays parked (fail-closed): no blind re-dispatch, no
	// duplicate migration, until the reconciler resolves the remote fact.
	_, err = h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-int",
	})
	if !errors.Is(err, m7app.ErrOutcomeUnknown) {
		t.Fatalf("parked replay want ErrOutcomeUnknown, got %v", err)
	}
	if h.mig.applyCalls != migCalls || h.dep.dispatchCalls != 1 {
		t.Fatalf("parked replay must not add work: mig=%d dep=%d",
			h.mig.applyCalls, h.dep.dispatchCalls)
	}
}

// ── rollback API: append-only attempts and RBK-001 environment freeze ──────

func TestPromotionRollbackLifecycle(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "rollback-api")
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-rb",
	})
	if err != nil {
		t.Fatal(err)
	}

	done, attempts, err := h.promo.Rollback(ctx, m7app.RollbackInput{
		PromotionID: prm.ID, Reason: "manual rollback", RequestID: "rbk-1", OperatorID: "u-ops-1",
	})
	if err != nil || done.State != m7flow.PrmRolledBack {
		t.Fatalf("rollback want rolled_back, got %s err=%v", done.State, err)
	}
	// The dev promotion ran a migration, so rollback covers binary+schema.
	if len(attempts) != 2 {
		t.Fatalf("want binary+schema attempts, got %+v", attempts)
	}
	// Idempotent second rollback.
	again, attempts2, err := h.promo.Rollback(ctx, m7app.RollbackInput{
		PromotionID: prm.ID, Reason: "manual rollback", RequestID: "rbk-2",
	})
	if err != nil || again.State != m7flow.PrmRolledBack || len(attempts2) != 2 {
		t.Fatalf("rollback replay not idempotent: %s %+v %v", again.State, attempts2, err)
	}
}

func TestPromotionRollbackFailureFreezesEnvironment(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "rollback-fail")
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvDev,
		PolicyContext: basePolicy("u-rel-1"), RequestID: "prm-rbf",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.dep.rollbackErr = errors.New("artifact store offline")

	_, _, err = h.promo.Rollback(ctx, m7app.RollbackInput{
		PromotionID: prm.ID, Reason: "attempt", RequestID: "rbk-f1",
	})
	if !errors.Is(err, m7app.ErrRollbackFailed) {
		t.Fatalf("want ErrRollbackFailed, got %v", err)
	}
	// The failed attempt is append-only history.
	view, err := h.promo.GetPromotion(ctx, prm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Promotion.State != m7flow.PrmRollbackFailed {
		t.Fatalf("want rollback_failed, got %s", view.Promotion.State)
	}
	if len(view.RollbackAttempts) != 2 || view.RollbackAttempts[0].State != m7flow.RbkFailed {
		t.Fatalf("want failed attempts, got %+v", view.RollbackAttempts)
	}
	// Environment frozen: further rollbacks demand manual disposal.
	_, _, err = h.promo.Rollback(ctx, m7app.RollbackInput{
		PromotionID: prm.ID, Reason: "retry", RequestID: "rbk-f2",
	})
	if !errors.Is(err, m7app.ErrRollbackNotAllowed) {
		t.Fatalf("frozen environment want ErrRollbackNotAllowed, got %v", err)
	}
}

// ── guards ──────────────────────────────────────────────────────────────────

func TestPromotionGuards(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "guards")

	if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: "qa",
		PolicyContext: basePolicy("u"), RequestID: "g1",
	}); !errors.Is(err, m7app.ErrPolicyRejected) {
		t.Fatalf("qa env want ErrPolicyRejected, got %v", err)
	}
	if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvStage, // none -> stage is illegal
		PolicyContext: basePolicy("u"), RequestID: "g2",
	}); !errors.Is(err, m7app.ErrPolicyRejected) {
		t.Fatalf("skip dev want ErrPolicyRejected, got %v", err)
	}
	// One active saga per package+environment edge (PRM-001): park a prod
	// saga, then a second request on the same edge must be refused.
	for _, env := range []string{m7flow.EnvDev, m7flow.EnvStage} {
		if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
			PackageID: pkg.ID, TargetEnv: env,
			PolicyContext: basePolicy("u-rel-1"), RequestID: "g-" + env,
		}); err != nil {
			t.Fatal(err)
		}
	}
	expiry := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	park := map[string]any{
		"requestedBy": "u-rel-1",
		"approval":    map[string]any{"expiresAt": expiry},
	}
	first, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: park, RequestID: "g-prod-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: park, RequestID: "g-prod-2",
	})
	if !errors.Is(err, m7app.ErrConcurrentPromotion) {
		t.Fatalf("concurrent edge want ErrConcurrentPromotion, got %v", err)
	}
	if first.State != m7flow.PrmApprovalCheck {
		t.Fatalf("first saga must stay parked, got %s", first.State)
	}
	// Unknown promotion rollback.
	if _, _, err := h.promo.Rollback(ctx, m7app.RollbackInput{
		PromotionID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Reason: "x", RequestID: "g3",
	}); !errors.Is(err, m7app.ErrPromotionNotFound) {
		t.Fatalf("want ErrPromotionNotFound, got %v", err)
	}
}

// ── approval SoD: approver must differ from requester and author ────────────

func TestPromotionApprovalSoD(t *testing.T) {
	h := newPromotionHarness(t)
	ctx := context.Background()
	pkg := h.sealedPackage(t, "author-1", "sod")
	for _, env := range []string{m7flow.EnvDev, m7flow.EnvStage} {
		if _, err := h.promo.Promote(ctx, m7app.PromoteInput{
			PackageID: pkg.ID, TargetEnv: env,
			PolicyContext: basePolicy("u-rel-1"), RequestID: "sod-" + env,
		}); err != nil {
			t.Fatal(err)
		}
	}
	expiry := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	approvedAt := time.Now().UTC().Format(time.RFC3339)
	park := map[string]any{
		"requestedBy": "u-rel-1",
		"approval":    map[string]any{"expiresAt": expiry},
	}
	prm, err := h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: park, RequestID: "sod-prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := h.promo.GetPromotion(ctx, prm.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Approver equals requester: denied (SoD).
	requesterApproval := map[string]any{
		"requestedBy": "u-rel-1",
		"approval": map[string]any{
			"approverId": "u-rel-1", "approvedAt": approvedAt, "expiresAt": expiry,
			"hash": m7flow.ApprovalHash(view.Promotion.CanonicalIntentDigest, "u-rel-1", approvedAt, expiry),
		},
	}
	_, err = h.promo.Promote(ctx, m7app.PromoteInput{
		PackageID: pkg.ID, TargetEnv: m7flow.EnvProd,
		PolicyContext: requesterApproval, RequestID: "sod-prod",
	})
	if !errors.Is(err, m7app.ErrApprovalInvalid) {
		t.Fatalf("requester approval want ErrApprovalInvalid, got %v", err)
	}
	denied, err := h.promo.GetPromotion(ctx, prm.ID)
	if err != nil || denied.Promotion.State != m7flow.PrmDenied {
		t.Fatalf("saga must record denied, got %s err=%v", denied.Promotion.State, err)
	}
}
