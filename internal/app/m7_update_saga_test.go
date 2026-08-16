package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

// ── scripted installer ──────────────────────────────────────────────────────

// scriptedInstaller embeds the deterministic local installer and injects
// step failures to drive the auto-rollback path.
type scriptedInstaller struct {
	m7app.LocalUpdateInstaller
	downloadErr   error
	installErr    error
	verifyErr     error
	rollbackErr   error
	rollbackCalls int
	downloadCalls int
}

func (i *scriptedInstaller) Download(ctx context.Context, inst, pkg, digest string) error {
	i.downloadCalls++
	if i.downloadErr != nil {
		return i.downloadErr
	}
	return i.LocalUpdateInstaller.Download(ctx, inst, pkg, digest)
}

func (i *scriptedInstaller) Install(ctx context.Context, inst, pkg, digest string) error {
	if i.installErr != nil {
		return i.installErr
	}
	return i.LocalUpdateInstaller.Install(ctx, inst, pkg, digest)
}

func (i *scriptedInstaller) Verify(ctx context.Context, inst, digest string) error {
	if i.verifyErr != nil {
		return i.verifyErr
	}
	return i.LocalUpdateInstaller.Verify(ctx, inst, digest)
}

func (i *scriptedInstaller) Rollback(ctx context.Context, inst string) error {
	i.rollbackCalls++
	if i.rollbackErr != nil {
		return i.rollbackErr
	}
	return i.LocalUpdateInstaller.Rollback(ctx, inst)
}

// rekeyedSigner is a ReleaseSigner with a different key material: signatures
// minted by the default local signer fail verification against it.
type rekeyedSigner struct{}

func (rekeyedSigner) KeyID() string { return "rekeyed-v1" }
func (rekeyedSigner) Sign(doc string) string {
	sum := sha256.Sum256([]byte("rekeyed:" + doc))
	return "rekeyed-v1:" + hex.EncodeToString(sum[:])
}
func (rekeyedSigner) Verify(doc, signature string) bool { return false }

// ── fake clock ──────────────────────────────────────────────────────────────

type updateFakeClock struct{ t time.Time }

func (c *updateFakeClock) Now() time.Time { return c.t.UTC() }

// ── harness ─────────────────────────────────────────────────────────────────

type updateHarness struct {
	dbPath string
	store  *sqlite.Store
	repo   *sqlite.AgentRuntimeRepository
	svc    *m7app.UpdateService
	clock  *updateFakeClock
	inst   *scriptedInstaller
}

func newUpdateHarness(t *testing.T) *updateHarness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m7upd.db")
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	clock := &updateFakeClock{t: time.Now().UTC()}
	inst := &scriptedInstaller{}
	svc := m7app.NewUpdateService(repo)
	svc.SetClock(clock)
	svc.SetInstaller(inst)
	return &updateHarness{dbPath: path, store: store, repo: repo, svc: svc, clock: clock, inst: inst}
}

// publishVersion seals one package body on a channel with a ±1h window.
func (h *updateHarness) publishVersion(t *testing.T, channel, version, minVersion string) m7flow.UpdatePackage {
	t.Helper()
	body := "update-body-" + version
	digest := m7flow.SHA256Hex([]byte(body))
	pkg, err := h.svc.Publish(context.Background(), m7app.PublishInput{
		Channel: channel, AppVersion: version, MinVersion: minVersion,
		PackageDigest: digest, PackageBody: body,
		NotBefore: h.clock.Now().Add(-time.Hour),
		ExpiresAt: h.clock.Now().Add(time.Hour),
		Actor:     "publisher-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func installOf(pkg m7flow.UpdatePackage, device string) m7app.InstallInput {
	return m7app.InstallInput{UpdateID: pkg.ID, ExpectedDigest: pkg.PackageDigest, DeviceID: device}
}

// ── publish / check ─────────────────────────────────────────────────────────

func TestUpdatePublishCheckFlow(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	pkg := h.publishVersion(t, m7flow.ChannelStable, "2.0.0", "1.5.0")

	// Below-floor device: update available and mandatory.
	res, err := h.svc.Check(ctx, m7flow.ChannelStable, "1.4.2")
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdateID != pkg.ID || res.Version != "2.0.0" || res.Digest != pkg.PackageDigest || !res.Mandatory {
		t.Fatalf("unexpected check below floor: %+v", res)
	}
	// Above-floor device: the update is offered but optional.
	res, err = h.svc.Check(ctx, m7flow.ChannelStable, "1.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdateID != pkg.ID || res.Mandatory {
		t.Fatalf("above-floor check must be optional: %+v", res)
	}
	// Current device: up to date.
	res, err = h.svc.Check(ctx, m7flow.ChannelStable, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdateID != "" || res.Mandatory {
		t.Fatalf("up-to-date check must be empty: %+v", res)
	}
	// Beta channel is isolated from stable.
	res, err = h.svc.Check(ctx, m7flow.ChannelBeta, "1.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdateID != "" {
		t.Fatalf("beta must stay empty: %+v", res)
	}
	// Expired packages drop out of check.
	h.clock.t = h.clock.t.Add(2 * time.Hour)
	res, err = h.svc.Check(ctx, m7flow.ChannelStable, "1.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdateID != "" {
		t.Fatalf("expired package must not surface: %+v", res)
	}
}

func TestUpdatePublishRejectsBadInputs(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	cases := []struct {
		name string
		in   m7app.PublishInput
		want error
	}{
		{"bad channel", m7app.PublishInput{Channel: "canary", AppVersion: "1.0.0", MinVersion: "1.0.0", PackageDigest: strings.Repeat("aa", 32), NotBefore: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, m7app.ErrUpdateChannelInvalid},
		{"bad version", m7app.PublishInput{Channel: "stable", AppVersion: "v1.x", MinVersion: "1.0.0", PackageDigest: strings.Repeat("aa", 32), NotBefore: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, m7app.ErrUpdateDowngrade},
		{"digest mismatch", m7app.PublishInput{Channel: "stable", AppVersion: "1.0.0", MinVersion: "1.0.0", PackageDigest: strings.Repeat("bb", 32), PackageBody: "x", NotBefore: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, m7app.ErrUpdateSignature},
		{"window inverted", m7app.PublishInput{Channel: "stable", AppVersion: "1.0.0", MinVersion: "1.0.0", PackageDigest: m7flow.SHA256Hex([]byte("x")), PackageBody: "x", NotBefore: time.Now().Add(time.Hour), ExpiresAt: time.Now()}, m7app.ErrUpdateWindowClosed},
	}
	for _, tc := range cases {
		if _, err := h.svc.Publish(ctx, tc.in); !errors.Is(err, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
	// Duplicate channel+version refuses a second publish.
	pkg := h.publishVersion(t, m7flow.ChannelStable, "1.0.0", "1.0.0")
	body := "update-body-1.0.0"
	_, err := h.svc.Publish(ctx, m7app.PublishInput{
		Channel: m7flow.ChannelStable, AppVersion: "1.0.0", MinVersion: "1.0.0",
		PackageDigest: m7flow.SHA256Hex([]byte(body)), PackageBody: body,
		NotBefore: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
	})
	if !errors.Is(err, m7app.ErrUpdatePackageExists) {
		t.Fatalf("duplicate publish want ErrUpdatePackageExists, got %v", err)
	}
	_ = pkg
}

// ── install ─────────────────────────────────────────────────────────────────

func TestUpdateInstallHappyPathAndReplay(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	pkg := h.publishVersion(t, m7flow.ChannelStable, "2.0.0", "1.5.0")

	state, err := h.svc.Install(ctx, installOf(pkg, "device-1"))
	if err != nil || state != m7flow.WireInstalled {
		t.Fatalf("install want installed, got %q err %v", state, err)
	}
	if h.inst.rollbackCalls != 0 {
		t.Fatalf("happy path must not roll back: %d calls", h.inst.rollbackCalls)
	}
	// Idempotent replay answers the recorded terminal state.
	state, err = h.svc.Install(ctx, installOf(pkg, "device-1"))
	if err != nil || state != m7flow.WireInstalled {
		t.Fatalf("replay want installed, got %q err %v", state, err)
	}
	// Device now sits on 2.0.0 - the channel is up to date for it.
	res, err := h.svc.Check(ctx, m7flow.ChannelStable, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdateID != "" {
		t.Fatalf("device on latest must see no update: %+v", res)
	}
	// The audit ledger grew and still verifies.
	if err := h.svc.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("ledger must verify: %v", err)
	}
}

func TestUpdateNonceSingleConsumption(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	pkg := h.publishVersion(t, m7flow.ChannelStable, "2.0.0", "1.5.0")
	if _, err := h.svc.Install(ctx, installOf(pkg, "device-1")); err != nil {
		t.Fatal(err)
	}
	// A second device replaying the same manifest nonce is refused.
	if _, err := h.svc.Install(ctx, installOf(pkg, "device-2")); !errors.Is(err, m7app.ErrNonceReplayed) {
		t.Fatalf("want ErrNonceReplayed, got %v", err)
	}
}

func TestUpdateInstallRejectsDigestAndSignatureProblems(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	pkg := h.publishVersion(t, m7flow.ChannelStable, "2.0.0", "1.5.0")

	wrong := installOf(pkg, "device-1")
	wrong.ExpectedDigest = strings.Repeat("cc", 32)
	if _, err := h.svc.Install(ctx, wrong); !errors.Is(err, m7app.ErrUpdateSignature) {
		t.Fatalf("digest mismatch want ErrUpdateSignature, got %v", err)
	}

	// Re-key the signer after publish: the stored signature no longer verifies.
	h.svc.SetSigner(&rekeyedSigner{})
	if _, err := h.svc.Install(ctx, installOf(pkg, "device-2")); !errors.Is(err, m7app.ErrUpdateSignature) {
		t.Fatalf("bad signature want ErrUpdateSignature, got %v", err)
	}
}

func TestUpdateInstallWindowAndDowngradeGuards(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	pkg := h.publishVersion(t, m7flow.ChannelStable, "2.0.0", "1.5.0")

	// Window guard: the same package expires for a later install.
	h.clock.t = h.clock.t.Add(2 * time.Hour)
	if _, err := h.svc.Install(ctx, installOf(pkg, "device-1")); !errors.Is(err, m7app.ErrUpdateWindowClosed) {
		t.Fatalf("expired want ErrUpdateWindowClosed, got %v", err)
	}
	h.clock.t = h.clock.t.Add(-2 * time.Hour)

	// Downgrade guard: device on 2.0.0 refuses an explicit 1.9.0 install.
	if _, err := h.svc.Install(ctx, installOf(pkg, "device-2")); err != nil {
		t.Fatal(err)
	}
	older := h.publishVersion(t, m7flow.ChannelStable, "1.9.0", "1.5.0")
	if _, err := h.svc.Install(ctx, installOf(older, "device-2")); !errors.Is(err, m7app.ErrUpdateDowngrade) {
		t.Fatalf("downgrade want ErrUpdateDowngrade, got %v", err)
	}
}

func TestUpdateInstallFailureAutoRollsBack(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	pkg := h.publishVersion(t, m7flow.ChannelStable, "2.0.0", "1.5.0")
	h.inst.installErr = fmt.Errorf("disk full")

	state, err := h.svc.Install(ctx, installOf(pkg, "device-1"))
	if !errors.Is(err, m7app.ErrUpdateInstallFailed) {
		t.Fatalf("want ErrUpdateInstallFailed, got %v", err)
	}
	if state != m7flow.UpdWireRolledBack {
		t.Fatalf("wire state want rolled_back, got %q", state)
	}
	if h.inst.rollbackCalls != 1 {
		t.Fatalf("auto rollback must run exactly once: %d", h.inst.rollbackCalls)
	}
	// The failed installation is durably rolled back; replay answers it.
	state, err = h.svc.Install(ctx, installOf(pkg, "device-1"))
	if err != nil || state != m7flow.UpdWireRolledBack {
		t.Fatalf("replay want rolled_back, got %q err %v", state, err)
	}
	if h.inst.rollbackCalls != 1 {
		t.Fatalf("replay must not roll back again: %d", h.inst.rollbackCalls)
	}
	// Nonce was consumed by the first attempt - another device is refused.
	if _, err := h.svc.Install(ctx, installOf(pkg, "device-2")); !errors.Is(err, m7app.ErrNonceReplayed) {
		t.Fatalf("want ErrNonceReplayed after rollback, got %v", err)
	}
}

func TestUpdateRollbackFailureFreezes(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	pkg := h.publishVersion(t, m7flow.ChannelStable, "2.0.0", "1.5.0")
	h.inst.installErr = fmt.Errorf("disk full")
	h.inst.rollbackErr = fmt.Errorf("rollback blocked")

	state, err := h.svc.Install(ctx, installOf(pkg, "device-1"))
	if !errors.Is(err, m7app.ErrUpdateRollbackFailed) {
		t.Fatalf("want ErrUpdateRollbackFailed, got %v", err)
	}
	if state != "" {
		t.Fatalf("frozen install answers no wire state, got %q", state)
	}
}

// ── audit ledger WORM + DR freeze ───────────────────────────────────────────

func TestAuditLedgerIsWorm(t *testing.T) {
	h := newUpdateHarness(t)
	ctx := context.Background()
	h.publishVersion(t, m7flow.ChannelStable, "1.0.0", "1.0.0")

	db, err := sql.Open("sqlite", h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE m7_audit_events SET actor='mallory' WHERE seq=1`); err == nil || !strings.Contains(err.Error(), "M7-DR-001") {
		t.Fatalf("ledger UPDATE must abort M7-DR-001, got %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM m7_audit_events WHERE seq=1`); err == nil || !strings.Contains(err.Error(), "M7-DR-001") {
		t.Fatalf("ledger DELETE must abort M7-DR-001, got %v", err)
	}
	if err := h.svc.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("surviving ledger must verify: %v", err)
	}
}

// newUpdateEngineHarness wires evidence+release+promotion+update onto one
// engine for the DR-freeze bridge test.
func newUpdateEngineHarness(t *testing.T) (*Engine, *m7app.UpdateService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m7updb.db")
	store, err := sqlite.Open(context.Background(), path)
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
	upd := m7app.NewUpdateService(repo)
	e.SetM7UpdateServices(upd)
	return e, upd, path
}

func TestDrFreezeBlocksProdPromotionOnChainBreak(t *testing.T) {
	e, upd, path := newUpdateEngineHarness(t)
	ctx := context.Background()
	pkg := sealedPackageViaBridge(t, e, "CR-UPD-DR1", "dr-freeze")

	// Intact ledger: the package climbs dev -> stage first (ladder rule),
	// then prod parks at the approval handshake normally.
	for i, leg := range []string{m7flow.EnvDev, m7flow.EnvStage} {
		resp := promoteViaBridge(t, e, pkg, leg, `{"requestedBy":"u-rel-0"}`,
			fmt.Sprintf("prm-dr-leg-%d", i), fmt.Sprintf("idem-dr-leg-%d", i))
		if !resp.OK {
			t.Fatalf("leg %s on intact ledger failed: %+v", leg, resp.Error)
		}
	}
	parked := promoteViaBridge(t, e, pkg, m7flow.EnvProd,
		`{"requestedBy":"u-rel-1","approval":{"expiresAt":"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}}`,
		"prm-dr-1", "idem-dr-1")
	if !parked.OK {
		t.Fatalf("prod promote on intact ledger failed: %+v", parked.Error)
	}

	// Forge an off-chain ledger row (bypasses the Go linker via raw SQL).
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var maxSeq int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM m7_audit_events`).Scan(&maxSeq); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO m7_audit_events
		(seq,id,action,resource_type,resource_id,actor,prev_hash,event_hash,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		maxSeq+1, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "app_update.forged", "update_package",
		"01ARZ3NDEKTSV4RRFFQ69G5FAV", "mallory",
		strings.Repeat("00", 32), strings.Repeat("11", 32), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := upd.VerifyAuditChain(ctx); !errors.Is(err, audit.ErrChainBroken) {
		t.Fatalf("forged row must break the chain, got %v", err)
	}

	// M7-DR-001: prod promotions freeze; dev keeps flowing (a fresh
	// package, since the frozen one already climbed past dev).
	flow := sealedPackageViaBridge(t, e, "CR-UPD-DR2", "dr-flow")
	frozen := promoteViaBridge(t, e, flow, m7flow.EnvDev,
		`{"requestedBy":"u-rel-2"}`, "prm-dr-2", "idem-dr-2")
	if !frozen.OK || frozen.Error != nil {
		t.Fatalf("dev promote must ignore the ledger: %+v", frozen)
	}
	prod := promoteViaBridge(t, e, pkg, m7flow.EnvProd,
		`{"requestedBy":"u-rel-2"}`, "prm-dr-3", "idem-dr-3")
	if prod.OK || prod.Error == nil || prod.Error.Code != "M7-DR-001" {
		t.Fatalf("prod promote must freeze with M7-DR-001, got %+v", prod.Error)
	}
}

func TestAppUpdateBridgeCheckAndInstall(t *testing.T) {
	e, upd, _ := newUpdateEngineHarness(t)
	ctx := context.Background()

	empty := e.Handle(ctx, m7Request(bridge.MethodAppUpdateCheck,
		`{"channel":"stable","currentVersion":"1.0.0"}`, ""))
	var before struct {
		UpdateID string `json:"updateId"`
	}
	m7Decode(t, empty, &before)
	if before.UpdateID != "" {
		t.Fatalf("empty channel must answer empty updateId, got %q", before.UpdateID)
	}

	// Publish through the management service, then check via the bridge.
	body := "update-body-3.1.0"
	pkg, err := upd.Publish(ctx, m7app.PublishInput{
		Channel: m7flow.ChannelStable, AppVersion: "3.1.0", MinVersion: "3.0.0",
		PackageDigest: m7flow.SHA256Hex([]byte(body)), PackageBody: body,
		NotBefore: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		Actor: "publisher-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	res := e.Handle(ctx, m7Request(bridge.MethodAppUpdateCheck,
		`{"channel":"stable","currentVersion":"2.9.0"}`, ""))
	var check struct {
		UpdateID  string `json:"updateId"`
		Version   string `json:"version"`
		Digest    string `json:"digest"`
		Mandatory bool   `json:"mandatory"`
	}
	m7Decode(t, res, &check)
	if check.UpdateID != pkg.ID || check.Version != "3.1.0" || check.Digest != pkg.PackageDigest || !check.Mandatory {
		t.Fatalf("unexpected check projection: %+v", check)
	}

	// Above the floor the same update stays optional.

	opt := e.Handle(ctx, m7Request(bridge.MethodAppUpdateCheck,

		`{"channel":"stable","currentVersion":"3.0.9"}`, ""))

	var optCheck struct {
		UpdateID string `json:"updateId"`

		Mandatory bool `json:"mandatory"`
	}

	m7Decode(t, opt, &optCheck)

	if optCheck.UpdateID != pkg.ID || optCheck.Mandatory {

		t.Fatalf("above-floor check must be optional: %+v", optCheck)

	}
	installed := e.Handle(ctx, m7Request(bridge.MethodAppUpdateInstall,
		`{"updateId":"`+pkg.ID+`","expectedDigest":"`+pkg.PackageDigest+`","deviceId":"ws-1"}`, "idem-inst-1"))
	var install struct {
		State string `json:"state"`
	}
	m7Decode(t, installed, &install)
	if install.State != m7flow.WireInstalled {
		t.Fatalf("bridge install want installed, got %q", install.State)
	}

	// Digest pinning refuses a mismatched expectedDigest (M7-UPD-001).
	body2 := "update-body-3.2.0"
	next, err := upd.Publish(ctx, m7app.PublishInput{
		Channel: m7flow.ChannelBeta, AppVersion: "3.2.0", MinVersion: "3.0.0",
		PackageDigest: m7flow.SHA256Hex([]byte(body2)), PackageBody: body2,
		NotBefore: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		Actor: "publisher-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	bad := e.Handle(ctx, m7Request(bridge.MethodAppUpdateInstall,
		`{"updateId":"`+next.ID+`","expectedDigest":"`+strings.Repeat("cc", 32)+`"}`, "idem-inst-2"))
	if bad.OK || bad.Error == nil || bad.Error.Code != "M7-UPD-001" {
		t.Fatalf("digest mismatch must answer M7-UPD-001, got %+v", bad.Error)
	}
	// Schema-invalid payloads answer BRIDGE_SCHEMA_INVALID.
	if resp := e.Handle(ctx, m7Request(bridge.MethodAppUpdateCheck,
		`{"channel":"canary","currentVersion":"1.0.0"}`, "")); resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("bad channel must answer BRIDGE_SCHEMA_INVALID, got %+v", resp.Error)
	}
}
