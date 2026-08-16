package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

// newM7PromotionEngineHarness wires the slice-2/3/4 services onto one engine
// so the promotion handlers can be exercised end-to-end through the bridge.
func newM7PromotionEngineHarness(t *testing.T) *Engine {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "m7prmh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	traceSvc := m7app.NewTraceService(repo)
	e := NewEngine(nil, "test")
	e.SetM7EvidenceServices(traceSvc, m7app.NewGateService(repo), m7app.NewReviewService(repo, traceSvc))
	e.SetM7ReleaseServices(m7app.NewReleaseService(repo))
	e.SetM7PromotionServices(m7app.NewPromotionService(repo))
	return e
}

// sealedPackageViaBridge drives createRevision -> review.submit ->
// buildPackage through the bridge and answers the sealed package id.
func sealedPackageViaBridge(t *testing.T, e *Engine, crID, summary string) string {
	t.Helper()
	ctx := context.Background()
	resp := e.Handle(ctx, m7Request(bridge.MethodReleaseCreateRevision,
		`{"crId":"`+crID+`","manifest":{"authorId":"author-1","summary":"`+summary+`","members":[{"name":"a.bin","size":3,"sha256":"`+
			strings.Repeat("aa", 32)+`"}],"sbom":{"format":"cyclonedx","digest":"`+strings.Repeat("bb", 32)+`"}},"requestId":"r1"}`, "idem-cr-"+crID))
	var rev struct {
		CRRevisionID string `json:"crRevisionId"`
		Digest       string `json:"digest"`
	}
	m7Decode(t, resp, &rev)
	approved := e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"cr_revision","subjectId":"`+rev.CRRevisionID+`","verdict":"approve","reviewerId":"rev-1","authorId":"author-1","reason":"ok"}`, ""))
	if !approved.OK {
		t.Fatalf("review failed: %+v", approved.Error)
	}
	built := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+rev.CRRevisionID+`","expectedDigest":"`+rev.Digest+`"}`, "idem-bp-"+crID))
	var pkg struct {
		PackageID string `json:"packageId"`
	}
	m7Decode(t, built, &pkg)
	return pkg.PackageID
}

// promoteViaBridge issues one release.promote call.
func promoteViaBridge(t *testing.T, e *Engine, pkg, env, policy, reqID, idemKey string) bridge.Response {
	t.Helper()
	return e.Handle(context.Background(), m7Request(bridge.MethodReleasePromote,
		`{"packageId":"`+pkg+`","targetEnv":"`+env+`","policyContext":`+policy+`,"requestId":"`+reqID+`"}`, idemKey))
}

func TestReleasePromoteHandlerHappyPathAndProjection(t *testing.T) {
	e := newM7PromotionEngineHarness(t)
	ctx := context.Background()
	pkg := sealedPackageViaBridge(t, e, "CR-PRM-H1", "handler-happy")

	resp := promoteViaBridge(t, e, pkg, m7flow.EnvDev,
		`{"requestedBy":"u-rel-1"}`, "prm-h1", "idem-prm-h1")
	var out struct {
		PromotionID string `json:"promotionId"`
		State       string `json:"state"`
	}
	m7Decode(t, resp, &out)
	if len(out.PromotionID) != 26 || out.State != m7flow.WireDone {
		t.Fatalf("unexpected promote result: %+v", out)
	}

	view := e.Handle(ctx, m7Request(bridge.MethodReleaseGetPromotion,
		`{"promotionId":"`+out.PromotionID+`"}`, ""))
	var proj struct {
		Promotion struct {
			ID                    string `json:"id"`
			PackageID             string `json:"packageId"`
			FromEnv               string `json:"fromEnv"`
			ToEnv                 string `json:"toEnv"`
			CanonicalIntentDigest string `json:"canonicalIntentDigest"`
			State                 string `json:"state"`
			RequestedBy           string `json:"requestedBy"`
			CreatedAt             string `json:"createdAt"`
			UpdatedAt             string `json:"updatedAt"`
		} `json:"promotion"`
		Timeline []struct {
			Step string `json:"step"`
		} `json:"timeline"`
		Migrations []struct {
			State string `json:"state"`
		} `json:"migrations"`
		Deployments []struct {
			State string `json:"state"`
		} `json:"deployments"`
		RollbackAttempts []struct {
			Dimension string `json:"dimension"`
		} `json:"rollbackAttempts"`
	}
	m7Decode(t, view, &proj)
	if proj.Promotion.ID != out.PromotionID || proj.Promotion.PackageID != pkg ||
		proj.Promotion.FromEnv != m7flow.EnvNone || proj.Promotion.ToEnv != m7flow.EnvDev ||
		proj.Promotion.State != m7flow.PrmSucceeded || proj.Promotion.RequestedBy != "u-rel-1" ||
		len(proj.Promotion.CanonicalIntentDigest) != 64 {
		t.Fatalf("unexpected projection: %+v", proj.Promotion)
	}
	if _, err := time.Parse(time.RFC3339Nano, proj.Promotion.CreatedAt); err != nil {
		t.Fatalf("createdAt not RFC3339: %q", proj.Promotion.CreatedAt)
	}
	if len(proj.Migrations) != 1 || proj.Migrations[0].State != m7flow.MigVerified ||
		len(proj.Deployments) != 1 || proj.Deployments[0].State != m7flow.DepSucceeded {
		t.Fatalf("execution rows not projected: mig=%+v dep=%+v", proj.Migrations, proj.Deployments)
	}
	if len(proj.Timeline) < 4 {
		t.Fatalf("timeline too short: %+v", proj.Timeline)
	}

	rbk := e.Handle(ctx, m7Request(bridge.MethodReleaseRollback,
		`{"promotionId":"`+out.PromotionID+`","reason":"handler test rollback","requestId":"rbk-h1"}`, "idem-rbk-h1"))
	var rOut struct {
		RollbackRef string `json:"rollbackRef"`
		State       string `json:"state"`
	}
	m7Decode(t, rbk, &rOut)
	if len(rOut.RollbackRef) != 26 || rOut.State != m7flow.WireRolledBack {
		t.Fatalf("unexpected rollback result: %+v", rOut)
	}
	detail := e.Handle(ctx, m7Request(bridge.MethodReleaseGetPromotion,
		`{"promotionId":"`+out.PromotionID+`"}`, ""))
	var after struct {
		Promotion struct {
			State string `json:"state"`
		} `json:"promotion"`
		RollbackAttempts []struct {
			Dimension string `json:"dimension"`
			State     string `json:"state"`
		} `json:"rollbackAttempts"`
	}
	m7Decode(t, detail, &after)
	if after.Promotion.State != m7flow.PrmRolledBack || len(after.RollbackAttempts) != 2 ||
		after.RollbackAttempts[0].Dimension != m7flow.RbkBinary ||
		after.RollbackAttempts[1].Dimension != m7flow.RbkSchema {
		t.Fatalf("rollback not projected: %+v %+v", after.Promotion, after.RollbackAttempts)
	}
}

func TestReleasePromoteHandlerApprovalHandshakeOverWire(t *testing.T) {
	e := newM7PromotionEngineHarness(t)
	pkg := sealedPackageViaBridge(t, e, "CR-PRM-H2", "handler-climb")
	for _, leg := range []string{m7flow.EnvDev, m7flow.EnvStage} {
		resp := promoteViaBridge(t, e, pkg, leg, `{"requestedBy":"u-rel-1"}`, "prm-h2-"+leg, "idem-h2-"+leg)
		if !resp.OK {
			t.Fatalf("leg %s failed: %+v", leg, resp.Error)
		}
	}
	// Prod parks at approval_check -> wire state "planned".
	expiry := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	parked := promoteViaBridge(t, e, pkg, m7flow.EnvProd,
		`{"requestedBy":"u-rel-1","approval":{"expiresAt":"`+expiry+`"}}`, "prm-h2-prod", "idem-h2-prod")
	var pOut struct {
		PromotionID string `json:"promotionId"`
		State       string `json:"state"`
	}
	m7Decode(t, parked, &pOut)
	if pOut.State != m7flow.WirePlanned {
		t.Fatalf("prod must park as planned, got %s", pOut.State)
	}
	// Read the canonical intent digest, sign it, replay the same requestId.
	view := e.Handle(context.Background(), m7Request(bridge.MethodReleaseGetPromotion,
		`{"promotionId":"`+pOut.PromotionID+`"}`, ""))
	var proj struct {
		Promotion struct {
			CanonicalIntentDigest string `json:"canonicalIntentDigest"`
		} `json:"promotion"`
	}
	m7Decode(t, view, &proj)
	approvedAt := time.Now().UTC().Format(time.RFC3339)
	done := promoteViaBridge(t, e, pkg, m7flow.EnvProd,
		`{"requestedBy":"u-rel-1","approval":{"approverId":"u-approver-9","approvedAt":"`+approvedAt+
			`","expiresAt":"`+expiry+`","hash":"`+m7flow.ApprovalHash(proj.Promotion.CanonicalIntentDigest, "u-approver-9", approvedAt, expiry)+`"}}`,
		"prm-h2-prod", "idem-h2-prod")
	var dOut struct {
		State string `json:"state"`
	}
	m7Decode(t, done, &dOut)
	if dOut.State != m7flow.WireDone {
		t.Fatalf("approved prod want done, got %s", dOut.State)
	}
}

func TestReleasePromotionHandlerGuards(t *testing.T) {
	e := newM7PromotionEngineHarness(t)
	ctx := context.Background()
	pkg := sealedPackageViaBridge(t, e, "CR-PRM-H3", "handler-guards")

	// Unknown env fails schema validation before touching the service.
	if r := promoteViaBridge(t, e, pkg, "qa", `{"requestedBy":"u"}`, "g1", "idem-g1"); r.OK || r.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("qa env want BRIDGE_SCHEMA_INVALID, got %+v", r.Error)
	}
	// Missing idempotency key.
	if r := e.Handle(ctx, validRequest(string(bridge.MethodReleasePromote),
		`{"packageId":"`+pkg+`","targetEnv":"dev","policyContext":{"requestedBy":"u"},"requestId":"g2"}`),
	); r.OK || r.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("missing key want IDEMPOTENCY_KEY_REQUIRED, got %+v", r.Error)
	}
	// Unknown package.
	if r := promoteViaBridge(t, e, "01ARZ3NDEKTSV4RRFFQ69G5FAV", m7flow.EnvDev,
		`{"requestedBy":"u"}`, "g3", "idem-g3"); r.OK || r.Error.Code != "NOT_FOUND" {
		t.Fatalf("unknown package want NOT_FOUND, got %+v", r.Error)
	}
	// Skipping dev is a policy rejection (PRM-003).
	if r := promoteViaBridge(t, e, pkg, m7flow.EnvStage, `{"requestedBy":"u"}`, "g4", "idem-g4"); r.OK || r.Error.Code != "M7-PRM-003" {
		t.Fatalf("skip dev want M7-PRM-003, got %+v", r.Error)
	}
	// Release-window rejection is denied (PRM-003) and the audit row persists.
	notBefore := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if r := promoteViaBridge(t, e, pkg, m7flow.EnvDev,
		`{"requestedBy":"u","releaseWindow":{"notBefore":"`+notBefore+`"}}`, "g5", "idem-g5"); r.OK || r.Error.Code != "M7-PRM-003" {
		t.Fatalf("window want M7-PRM-003, got %+v", r.Error)
	}
	// rollback guards: unknown promotion / malformed payload.
	if r := e.Handle(ctx, m7Request(bridge.MethodReleaseRollback,
		`{"promotionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","reason":"x","requestId":"g6"}`, "idem-g6")); r.OK || r.Error.Code != "NOT_FOUND" {
		t.Fatalf("unknown rollback want NOT_FOUND, got %+v", r.Error)
	}
	if r := e.Handle(ctx, m7Request(bridge.MethodReleaseRollback,
		`{"promotionId":"`+pkg+`","reason":"","requestId":"g7"}`, "idem-g7")); r.OK || r.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("empty reason want BRIDGE_SCHEMA_INVALID, got %+v", r.Error)
	}
	// getPromotion schema guard.
	if r := e.Handle(ctx, m7Request(bridge.MethodReleaseGetPromotion, `{"promotionId":"not-a-ulid"}`, "")); r.OK || r.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("bad id want BRIDGE_SCHEMA_INVALID, got %+v", r.Error)
	}
}

func TestReleasePromotionHandlerServiceUnavailable(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "m7prmu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e := NewEngine(nil, "test") // promotion service intentionally not wired
	ctx := context.Background()
	r := e.Handle(ctx, m7Request(bridge.MethodReleasePromote,
		`{"packageId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","targetEnv":"dev","policyContext":{"requestedBy":"u"},"requestId":"x"}`, "idem-x"))
	if r.OK || r.Error.Code != "STORAGE_UNAVAILABLE" || !r.Error.Retryable {
		t.Fatalf("unwired promote want retryable STORAGE_UNAVAILABLE, got %+v", r.Error)
	}
}
