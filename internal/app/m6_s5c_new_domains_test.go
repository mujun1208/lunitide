// Integration coverage for the 0053 new domains: five-state health
// aggregation (gap-6), the governed skill import pipeline (gap-7),
// complexity routing + manifest/bundle/synthesis freezing (gap-8) and the
// cloud runner registry with fenced leases + receipt reconcile (gap-9).
package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/complexity"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newS5CServices(t *testing.T) (*storage.Store, *m6app.IntegrationService, *m6app.HealthService,
	*m6app.SkillImportService, *m6app.RoutingService, *m6app.CloudService, m6app.UnitOfWork) {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "m6.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	return store, m6app.NewIntegrationService(repo), m6app.NewHealthService(repo),
		m6app.NewSkillImportService(repo), m6app.NewRoutingService(repo),
		m6app.NewCloudService(repo), repo
}

// s5cAgentRunID provisions the project → session → agent_run chain and
// returns the run id, satisfying the m6_delegation.root_id FK.
func s5cAgentRunID(t *testing.T, store *storage.Store) string {
	t.Helper()
	ctx := context.Background()
	projects := projectapp.New(store, store)
	created, err := projects.Create(ctx, "s5c-routing-project", "test", struct {
		Name string `json:"name"`
	}{"S5C"}, project.Project{Name: "S5C"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	sess, err := sessions.Create(ctx, "s5c-routing-session", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{created.ID, "S"}, session.Session{ProjectID: created.ID, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	runs := agentrunapp.New(store.AgentRuntimeRepository())
	run, err := runs.Start(ctx, "s5c-root-run", "test", map[string]any{"goal": "routing"}, sess.ID, agentrun.Budget{
		MaxModelTurns: 8, MaxToolCalls: 32, MaxTokens: 100000, MaxCostMicros: 500000,
		MaxWallClockSeconds: 3600, MaxOutputBytes: 1048576, MaxRetries: 3, MaxNoProgress: 5, HardCeiling: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return run.ID
}

// s5cCloudTask inserts one m6_cloud_task row and returns its id, satisfying
// the m6_worker_lease / m6_remote_receipt task_id FKs.
func s5cCloudTask(t *testing.T, uow m6app.UnitOfWork, key string) string {
	t.Helper()
	id := ulid.Make().String()
	err := uow.TransactM6(context.Background(), func(tx m6app.Tx) error {
		now := time.Now().UTC()
		return tx.PutM6CloudTask(m6supply.CloudTask{
			ID: id, IdempotencyKey: key, PayloadDigest: strings.Repeat("b", 64),
			State: m6supply.CloudTaskCreated, Version: 1, CreatedAt: now, UpdatedAt: now,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// activeIntegration walks draft -> validating -> active.
func activeIntegration(t *testing.T, ctx context.Context, isvc *m6app.IntegrationService, name string) m6supply.Integration {
	t.Helper()
	ig, err := isvc.Create(ctx, goodCreateInput(name))
	if err != nil {
		t.Fatal(err)
	}
	validating, err := isvc.Transition(ctx, ig.ID, ig.Version, m6supply.IntegrationValidating)
	if err != nil {
		t.Fatal(err)
	}
	active, err := isvc.Transition(ctx, validating.ID, validating.Version, m6supply.IntegrationActive)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func sampleIn(ig string, success bool) m6app.SampleInput {
	status := m6supply.HealthHealthy
	code := "2xx"
	if !success {
		status = m6supply.HealthUnhealthy
		code = "5xx"
	}
	return m6app.SampleInput{IntegrationID: ig, Status: status, Success: success, LatencyMS: 120, CodeClass: code}
}

func TestHealthFiveStateAggregation(t *testing.T) {
	_, isvc, hsvc, _, _, _, _ := newS5CServices(t)
	ctx := context.Background()

	// unknown: no samples in the window
	ig := activeIntegration(t, ctx, isvc, "health-unknown")
	agg, err := hsvc.Aggregate(ctx, ig.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if agg.State != m6supply.HealthUnknown || agg.Samples != 0 {
		t.Fatalf("fresh aggregate: %+v", agg)
	}

	// unknown until the minimum sample count even at 100% success
	for i := 0; i < m6supply.DefaultHealthMinSamples-1; i++ {
		if _, err := hsvc.RecordSample(ctx, sampleIn(ig.ID, true)); err != nil {
			t.Fatal(err)
		}
	}
	agg, err = hsvc.Aggregate(ctx, ig.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if agg.State != m6supply.HealthUnknown {
		t.Fatalf("below min samples must stay unknown: %+v", agg)
	}
	if _, err := hsvc.RecordSample(ctx, sampleIn(ig.ID, true)); err != nil {
		t.Fatal(err)
	}
	agg, err = hsvc.Aggregate(ctx, ig.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if agg.State != m6supply.HealthHealthy || agg.SuccessRate != 1.0 {
		t.Fatalf("all-success aggregate: %+v", agg)
	}

	// degraded: 19/20 = 95%
	dg := activeIntegration(t, ctx, isvc, "health-degraded")
	for i := 0; i < 20; i++ {
		if _, err := hsvc.RecordSample(ctx, sampleIn(dg.ID, i != 7)); err != nil {
			t.Fatal(err)
		}
	}
	agg, err = hsvc.Aggregate(ctx, dg.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if agg.State != m6supply.HealthDegraded || agg.Samples != 20 || agg.Successes != 19 {
		t.Fatalf("degraded aggregate: %+v", agg)
	}

	// unhealthy: 2/5 = 40%
	ug := activeIntegration(t, ctx, isvc, "health-unhealthy")
	for i := 0; i < 5; i++ {
		if _, err := hsvc.RecordSample(ctx, sampleIn(ug.ID, i < 2)); err != nil {
			t.Fatal(err)
		}
	}
	agg, err = hsvc.Aggregate(ctx, ug.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if agg.State != m6supply.HealthUnhealthy {
		t.Fatalf("unhealthy aggregate: %+v", agg)
	}
	if !agg.BlocksScheduling() {
		t.Fatal("unhealthy must block scheduling (HLT-001)")
	}

	// paused outranks every sample-derived state
	pg := activeIntegration(t, ctx, isvc, "health-paused")
	for i := 0; i < 6; i++ {
		if _, err := hsvc.RecordSample(ctx, sampleIn(pg.ID, true)); err != nil {
			t.Fatal(err)
		}
	}
	paused, err := isvc.Transition(ctx, pg.ID, pg.Version, m6supply.IntegrationPaused)
	if err != nil {
		t.Fatal(err)
	}
	agg, err = hsvc.Aggregate(ctx, paused.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if agg.State != m6supply.HealthPaused || agg.Samples != 6 || !agg.BlocksScheduling() {
		t.Fatalf("paused aggregate: %+v", agg)
	}

	// samples for an unknown integration are refused
	if _, err := hsvc.RecordSample(ctx, sampleIn("01AAAAAAAAAAAAAAAAAAAAAAAA", true)); !errors.Is(err, m6app.ErrIntegrationNotFound) {
		t.Fatalf("unknown integration sample: %v", err)
	}
	// payload shape guards
	bad := sampleIn(ig.ID, true)
	bad.CodeClass = "200"
	if _, err := hsvc.RecordSample(ctx, bad); err == nil {
		t.Fatal("codeClass 200 must be refused")
	}

	// call log: outcome_unknown is a first-class append-only fact
	first, err := hsvc.RecordCall(ctx, m6app.CallInput{
		IntegrationID: ig.ID, OperationID: "pets.list", Environment: m6supply.EnvProduction,
		Attempt: 1, Outcome: m6supply.CallOutcomeUnknown, TraceID: "tr-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hsvc.RecordCall(ctx, m6app.CallInput{
		IntegrationID: ig.ID, OperationID: "pets.list", Environment: m6supply.EnvProduction,
		Attempt: 2, Outcome: m6supply.CallSucceeded, CorrectionOfCallID: first.ID,
	}); err != nil {
		t.Fatal(err)
	}
	badCall := m6app.CallInput{IntegrationID: ig.ID, OperationID: "pets.list", Environment: "staging", Attempt: 1, Outcome: m6supply.CallSucceeded}
	if _, err := hsvc.RecordCall(ctx, badCall); err == nil {
		t.Fatal("unknown environment must be refused")
	}
}

const goodSkillManifest = `{"schema":"lunitide.skill/v1","name":"pdf-tool","version":"1.2.0","publisher":"acme","permissions":{"tools":["fs.read"],"network":["api.example"]},"dependencies":[{"type":"skill","name":"base-io","versionConstraint":"^1.0.0"}]}`

func discoverInput(url string) m6app.DiscoverInput {
	return m6app.DiscoverInput{
		AssetType: m6supply.AssetSkill, SourceURL: url,
		ImmutableCommit: strings.Repeat("b", 40), ArchiveHash: strings.Repeat("a", 64),
		License: "MIT", NoticeRef: "NOTICE", Publisher: "acme", Signature: "sig-1",
	}
}

func TestSkillImportPipeline(t *testing.T) {
	_, _, _, ssvc, _, _, repo := newS5CServices(t)
	ctx := context.Background()

	c, err := ssvc.Discover(ctx, discoverInput("https://src.example/acme/pdf-tool"))
	if err != nil {
		t.Fatal(err)
	}
	if c.State != m6supply.ImportDiscovered || c.Version != 1 {
		t.Fatalf("fresh candidate: %+v", c)
	}
	// (sourceUrl, immutableCommit) is UNIQUE
	if _, err := ssvc.Discover(ctx, discoverInput("https://src.example/acme/pdf-tool")); !errors.Is(err, m6app.ErrCandidateExists) {
		t.Fatalf("duplicate discovery: %v", err)
	}

	// skipping pinned is refused
	if _, err := ssvc.Inspect(ctx, c.ID, c.Version, m6supply.ImportEvidence{}); !errors.Is(err, m6supply.ErrInvalidTransition) {
		t.Fatalf("discovered->inspected must be refused: %v", err)
	}

	pinned, err := ssvc.Pin(ctx, c.ID, c.Version, m6supply.ImportEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := ssvc.Inspect(ctx, pinned.ID, pinned.Version, m6supply.ImportEvidence{NoticeRef: "NOTICE", Signature: "sig-1"})
	if err != nil {
		t.Fatal(err)
	}
	// scan without scan evidence is governance theater
	if _, err := ssvc.Scan(ctx, inspected.ID, inspected.Version, m6supply.ImportEvidence{}); err == nil {
		t.Fatal("scan without scanRefs must be refused")
	}
	scanned, err := ssvc.Scan(ctx, inspected.ID, inspected.Version, m6supply.ImportEvidence{ScanRefs: `["scan://001"]`, InjectionScan: `{"verdict":"clean"}`})
	if err != nil {
		t.Fatal(err)
	}
	evaluated, err := ssvc.Evaluate(ctx, scanned.ID, scanned.Version, m6supply.ImportEvidence{EvaluationID: "eval-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ssvc.Evaluate(ctx, evaluated.ID, evaluated.Version, m6supply.ImportEvidence{}); err == nil {
		t.Fatal("evaluate without evaluationId must be refused")
	}
	submitted, err := ssvc.Submit(ctx, evaluated.ID, evaluated.Version, m6supply.ImportEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.State != m6supply.ImportAwaitingApproval {
		t.Fatalf("submitted: %+v", submitted)
	}

	// publisher mismatch refuses approval and rolls the whole step back
	evil := strings.Replace(goodSkillManifest, `"publisher":"acme"`, `"publisher":"evil"`, 1)
	if _, err := ssvc.Approve(ctx, m6app.ApproveInput{CandidateID: submitted.ID, ExpectedVersion: submitted.Version, Approval: `{"by":"local"}`, Manifest: []byte(evil)}); err == nil {
		t.Fatal("publisher mismatch must refuse approval")
	}
	stillPending, err := func() (m6supply.ImportCandidate, error) {
		var c m6supply.ImportCandidate
		err := repo.TransactM6(ctx, func(tx m6app.Tx) error {
			var gerr error
			c, gerr = tx.GetM6ImportCandidate(submitted.ID)
			return gerr
		})
		return c, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	if stillPending.State != m6supply.ImportAwaitingApproval {
		t.Fatalf("refused approval must not advance: %+v", stillPending)
	}

	approved, err := ssvc.Approve(ctx, m6app.ApproveInput{CandidateID: submitted.ID, ExpectedVersion: submitted.Version, Approval: `{"by":"local"}`, Manifest: []byte(goodSkillManifest)})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != m6supply.ImportApproved || approved.Approval == "" {
		t.Fatalf("approved: %+v", approved)
	}

	// the entity chain materialized inside the approval transaction
	var skill m6supply.Skill
	var version m6supply.SkillVersion
	var deps int
	err = repo.TransactM6(ctx, func(tx m6app.Tx) error {
		sk, err := tx.FindM6SkillByName("pdf-tool")
		if err != nil {
			return err
		}
		skill = sk
		v, err := tx.GetM6SkillVersion(sk.CurrentVersionID)
		if err != nil {
			return err
		}
		version = v
		list, err := tx.ListM6SkillDependencies(v.ID)
		if err != nil {
			return err
		}
		deps = len(list)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if skill.Status != m6supply.SkillVerified || version.Semver != "1.2.0" ||
		version.PackageHash != strings.Repeat("a", 64) || version.SignatureStatus != m6supply.SignatureVerified || deps != 1 {
		t.Fatalf("materialized chain: skill=%+v version=%+v deps=%d", skill, version, deps)
	}

	// install binds the approved version into a workspace
	install, err := ssvc.InstallSkill(ctx, version.ID, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if install.Status != m6supply.SkillInstallInstalled {
		t.Fatalf("install: %+v", install)
	}

	// rejection is reachable from every pre-approval state and terminal
	rc, err := ssvc.Discover(ctx, discoverInput("https://src.example/acme/bad"))
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := ssvc.Reject(ctx, rc.ID, rc.Version, m6supply.ImportEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State != m6supply.ImportRejected {
		t.Fatalf("rejected: %+v", rejected)
	}
	revoked, err := ssvc.Revoke(ctx, rejected.ID, rejected.Version, m6supply.ImportEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.State != m6supply.ImportRevoked {
		t.Fatalf("revoked: %+v", revoked)
	}
}

const goodPromptBundleManifest = `{"schema":"lunitide.prompt_bundle/v1","name":"expert-db","version":"1.0.0","publisher":"acme","templateRef":"lunitide-prompt.tpl","vars":{"role":"数据库专家","projectName":"Demo","phase":"3","context":"表结构评审","instructions":"输出 ER 摘要"}}`

func TestPromptBundleImportPipeline(t *testing.T) {
	_, _, _, ssvc, _, _, repo := newS5CServices(t)
	ctx := context.Background()

	c, err := ssvc.Discover(ctx, m6app.DiscoverInput{
		AssetType: m6supply.AssetPromptBundle, SourceURL: "https://src.example/acme/expert-db",
		ImmutableCommit: strings.Repeat("c", 40), ArchiveHash: strings.Repeat("b", 64),
		License: "MIT", Publisher: "acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err = ssvc.Pin(ctx, c.ID, c.Version, m6supply.ImportEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	c, err = ssvc.Inspect(ctx, c.ID, c.Version, m6supply.ImportEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	c, err = ssvc.Scan(ctx, c.ID, c.Version, m6supply.ImportEvidence{ScanRefs: `["scan-1"]`})
	if err != nil {
		t.Fatal(err)
	}
	c, err = ssvc.Evaluate(ctx, c.ID, c.Version, m6supply.ImportEvidence{EvaluationID: "eval-1"})
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := ssvc.Submit(ctx, c.ID, c.Version, m6supply.ImportEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := ssvc.Approve(ctx, m6app.ApproveInput{
		CandidateID: submitted.ID, ExpectedVersion: submitted.Version,
		Approval: `{"by":"local"}`, Manifest: []byte(goodPromptBundleManifest),
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != m6supply.ImportApproved {
		t.Fatalf("approved: %+v", approved)
	}

	var bundle m6supply.PromptBundle
	var version m6supply.PromptBundleVersion
	err = repo.TransactM6(ctx, func(tx m6app.Tx) error {
		b, err := tx.FindM6PromptBundleByName("expert-db")
		if err != nil {
			return err
		}
		bundle = b
		v, err := tx.GetM6PromptBundleVersion(b.CurrentVersionID)
		if err != nil {
			return err
		}
		version = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Publisher != "acme" || bundle.Status != m6supply.PromptBundleVerified {
		t.Fatalf("bundle: %+v", bundle)
	}
	if version.Semver != "1.0.0" || version.TemplateRef != "lunitide-prompt.tpl" {
		t.Fatalf("version: %+v", version)
	}
	if !strings.Contains(version.CompiledBody, "数据库专家") || len(version.CompiledDigest) != 64 {
		t.Fatalf("compiled: digest=%q body=%q", version.CompiledDigest, version.CompiledBody)
	}
}

func TestComplexityRoutingAndSynthesis(t *testing.T) {
	store, _, _, _, rsvc, _, repo := newS5CServices(t)
	ctx := context.Background()

	// zero signals route simple/single with the scope.simple reason
	simple, err := rsvc.Decide(ctx, "sess-1", complexity.ConversationSignals{})
	if err != nil {
		t.Fatal(err)
	}
	if simple.Tier != "simple" || simple.RoutedPath != "single" || simple.ReasonCodes != `["scope.simple"]` {
		t.Fatalf("simple decision: %+v", simple)
	}
	// deterministic replay answers the stored row (no double audit)
	replay, err := rsvc.Decide(ctx, "sess-1", complexity.ConversationSignals{})
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != simple.ID {
		t.Fatalf("replay must be idempotent: %s vs %s", replay.ID, simple.ID)
	}

	// delegation hints alone push into complex/delegated
	complex1, err := rsvc.Decide(ctx, "sess-2", complexity.ConversationSignals{DelegationHints: 5})
	if err != nil {
		t.Fatal(err)
	}
	if complex1.Tier != "complex" || complex1.RoutedPath != "delegated" {
		t.Fatalf("complex decision: %+v", complex1)
	}

	// a security-relevant moderate task escalates to high-risk
	risky, err := rsvc.Decide(ctx, "sess-3", complexity.ConversationSignals{SecurityRelevant: true, DelegationHints: 2})
	if err != nil {
		t.Fatal(err)
	}
	if risky.Tier != "high-risk" || risky.RoutedPath != "delegated" ||
		!strings.Contains(risky.ReasonCodes, "risk.security") {
		t.Fatalf("high-risk decision: %+v", risky)
	}

	// manifest/bundle/synthesis freeze under a real delegation row
	dgID := "01DE1GAAAAAAAAAAAAAAAAAAA0"
	rootID := s5cAgentRunID(t, store)
	err = repo.TransactM6(ctx, func(tx m6app.Tx) error {
		now := time.Now().UTC()
		return tx.PutM6Delegation(m6supply.Delegation{
			ID: dgID, RootID: rootID, ParentID: rootID,
			Envelope: "{}", EnvelopeDigest: strings.Repeat("c", 64),
			Nonce: "nonce-routing-000001", Depth: 1, State: m6supply.DelegationDispatched,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := rsvc.FreezeManifest(ctx, m6app.FreezeManifestInput{
		DelegationID: dgID, TaskScope: `{"goal":"fix-auth"}`,
		LockedInputs: `{"repo":"lunitide@abc"}`, Budget: `{"tokens":1000}`,
		Capabilities: `{"tools":["fs"]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.ManifestDigest) != 64 {
		t.Fatalf("manifest digest: %+v", manifest)
	}
	if _, err := rsvc.FreezeManifest(ctx, m6app.FreezeManifestInput{
		DelegationID: dgID, TaskScope: `{"goal":"other"}`, LockedInputs: `{}`,
		Budget: `{}`, Capabilities: `{}`,
	}); !errors.Is(err, m6app.ErrManifestExists) {
		t.Fatalf("re-freeze must be refused: %v", err)
	}
	// a manifest for a missing delegation is refused
	if _, err := rsvc.FreezeManifest(ctx, m6app.FreezeManifestInput{
		DelegationID: "01MISS1NGAAAAAAAAAAAAAAAAAAA", TaskScope: `{}`,
		LockedInputs: `{}`, Budget: `{}`, Capabilities: `{}`,
	}); !errors.Is(err, m6app.ErrDelegationNotFound) {
		t.Fatalf("missing delegation: %v", err)
	}

	bundle, err := rsvc.RecordBundle(ctx, m6app.BundleInput{
		DelegationID: dgID, ChildID: "child-1", Attempt: 1, BaseHead: "abc123",
		Claims: `{"files":2}`, PatchDigest: strings.Repeat("d", 64),
		TestEvidence: `{"passed":true}`, Usage: `{"tokens":800}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.ResultDigest) != 64 {
		t.Fatalf("bundle digest: %+v", bundle)
	}
	if _, err := rsvc.RecordBundle(ctx, m6app.BundleInput{
		DelegationID: dgID, ChildID: "child-1", Attempt: 1, BaseHead: "abc123",
		Claims: `{}`, PatchDigest: strings.Repeat("d", 64),
		TestEvidence: `{}`, Usage: `{}`,
	}); !errors.Is(err, m6app.ErrBundleExists) {
		t.Fatalf("duplicate attempt must be refused: %v", err)
	}

	rec, err := rsvc.Synthesize(ctx, m6app.SynthesizeInput{
		RootID: rootID, Consistent: `["child-1"]`,
		Conflicts: `[]`, MissingEvidence: `[]`, AdoptionReasons: `["digest-match"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.SynthesisDigest) != 64 {
		t.Fatalf("synthesis digest: %+v", rec)
	}
}

func TestCloudRunnerLeaseAndReconcile(t *testing.T) {
	_, _, _, _, _, csvc, uow := newS5CServices(t)
	ctx := context.Background()
	task1 := s5cCloudTask(t, uow, "s5c-task-1")
	task2 := s5cCloudTask(t, uow, "s5c-task-2")

	// closed world: with no policy snapshot nothing is schedulable
	if ok, _ := csvc.RegionAllowed(ctx, "cn-east-1"); ok {
		t.Fatal("no policy must deny every region")
	}
	if _, err := csvc.PutRegionPolicy(ctx, `["cn-east-1","us-west-2"]`, `{"mode":"restricted"}`, `{"level":"internal"}`); err != nil {
		t.Fatal(err)
	}
	if ok, _ := csvc.RegionAllowed(ctx, "cn-east-1"); !ok {
		t.Fatal("cn-east-1 must be allowed")
	}
	if ok, _ := csvc.RegionAllowed(ctx, "eu-west-1"); ok {
		t.Fatal("eu-west-1 must be denied")
	}

	runnerIn := func(region, attestation string, seq int) m6app.RegisterRunnerInput {
		return m6app.RegisterRunnerInput{
			Region: region, WorkloadIdentity: "spiffe://" + region + "/runner-" + string(rune('a'+seq)),
			AttestationDigest: strings.Repeat("e", 64), AttestationStatus: attestation,
			MTLSFingerprint: "AA:BB:CC", Capabilities: `{"arch":"amd64"}`,
		}
	}

	// an unverified runner cannot activate
	lax, err := csvc.RegisterRunner(ctx, runnerIn("cn-east-1", m6supply.AttestationUnverified, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := csvc.TransitionRunner(ctx, lax.ID, lax.Version, m6supply.RunnerActive); !errors.Is(err, m6app.ErrAttestationRequired) {
		t.Fatalf("unverified activation: %v", err)
	}

	// a verified runner activates and takes a fenced lease
	runner, err := csvc.RegisterRunner(ctx, runnerIn("cn-east-1", m6supply.AttestationVerified, 1))
	if err != nil {
		t.Fatal(err)
	}
	active, err := csvc.TransitionRunner(ctx, runner.ID, runner.Version, m6supply.RunnerActive)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := csvc.GrantLease(ctx, active.ID, task1, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Epoch != 1 || lease.State != m6supply.LeaseActive {
		t.Fatalf("lease: %+v", lease)
	}
	if _, err := csvc.GrantLease(ctx, active.ID, task1, 5*time.Minute); !errors.Is(err, m6app.ErrLeaseExists) {
		t.Fatalf("double lease: %v", err)
	}

	// a runner outside the policy regions is never scheduled
	out, err := csvc.RegisterRunner(ctx, runnerIn("eu-west-1", m6supply.AttestationVerified, 2))
	if err != nil {
		t.Fatal(err)
	}
	outActive, err := csvc.TransitionRunner(ctx, out.ID, out.Version, m6supply.RunnerActive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := csvc.GrantLease(ctx, outActive.ID, task2, 5*time.Minute); !errors.Is(err, m6app.ErrRegionNotAllowed) {
		t.Fatalf("out-of-region lease: %v", err)
	}

	// outcome_unknown is recorded as a fact, never auto-accepted
	lost, err := csvc.RecordReceipt(ctx, active.ID, task1, m6supply.ReceiptOutcomeUnknown, "", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := csvc.ListPendingReceipts(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != lost.ID {
		t.Fatalf("pending receipts: %+v", pending)
	}
	if _, err := csvc.Reconcile(ctx, lost.ID, lost.Version, m6supply.ReconcileAccepted, "looks fine"); err == nil {
		t.Fatal("outcome_unknown must not be auto-accepted")
	}
	if _, err := csvc.Reconcile(ctx, lost.ID, lost.Version, m6supply.ReconcileManualReview, "evidence pending"); err != nil {
		t.Fatalf("manual review: %v", err)
	}
	pending, err = csvc.ListPendingReceipts(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("reconciled receipt must leave the queue: %+v", pending)
	}

	// a succeeded receipt reconciles clean
	ok, err := csvc.RecordReceipt(ctx, active.ID, task1, m6supply.ReceiptSucceeded, strings.Repeat("f", 64), `{"ms":120}`)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := csvc.Reconcile(ctx, ok.ID, ok.Version, m6supply.ReconcileAccepted, "digest verified")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != m6supply.ReconcileAccepted {
		t.Fatalf("decision: %+v", decision)
	}
	// a stale version cannot reconcile twice
	if _, err := csvc.Reconcile(ctx, ok.ID, ok.Version, m6supply.ReconcileRejected, "stale"); !errors.Is(err, m6supply.ErrVersionConflict) {
		t.Fatalf("stale reconcile: %v", err)
	}
}
