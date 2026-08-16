package execution

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/delegation"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

var baseTime = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

type keyFixture struct {
	keys delegation.KeyResolver
	priv ed25519.PrivateKey
	wrong ed25519.PrivateKey
}

func newKeys(t *testing.T) *keyFixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, wrong, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := delegation.KeyResolver(func(keyID string) (ed25519.PublicKey, bool) {
		if keyID == "rk1" {
			return pub, true
		}
		return nil, false
	})
	return &keyFixture{keys: keys, priv: priv, wrong: wrong}
}

var nonceCounter int

func cloudProof(now time.Time) Proof {
	nonceCounter++
	return Proof{
		RunnerKind:       RunnerKindCloud,
		TrustTier:        TrustTrusted,
		ResidencyRegions: []string{"cn-north"},
		KeyRegion:        "cn-north",
		Capabilities:     []string{"aml.scan"},
		NetworkEgress:    EgressNone,
		IssuedAt:         now.UTC().Format(time.RFC3339),
		Nonce:            fmt.Sprintf("nonce-%d", nonceCounter),
	}
}

func signed(p Proof, f *keyFixture) Proof {
	SignProof("rk1", f.priv, &p)
	return p
}

func digestOf(seed byte) string { return strings.Repeat(string(rune('a'+seed)), 64) }

func ticketFor(org string, now time.Time) Ticket {
	return Ticket{
		TicketID:            "tkt-1",
		OrgID:               org,
		PolicyDigest:        digestOf(0),
		PackageDigest:       digestOf(1),
		BudgetReservationID: "res-1",
		ExpiresAt:           now.Add(10 * time.Minute),
	}
}

func inOrgReq() RouteRequest {
	return RouteRequest{
		DataClass:            DataClassInternal,
		RequiredRegions:      []string{ResidencyInOrg},
		RequiredCapabilities: []string{"aml.scan"},
		MaxEgress:            EgressNone,
		SecretTier:           SecretTierNone,
	}
}

func TestBroker(t *testing.T) {
	t.Run("local runner defaults to trusted in-org and serves restricted in-org data", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		if err := b.Heartbeat("runner-a", baseTime); err != nil {
			t.Fatal(err)
		}
		req := inOrgReq()
		req.DataClass = DataClassRestricted
		req.SecretTier = SecretTierInOrg
		d, err := b.Route("task-1", ticketFor("01JDORG", baseTime), req, baseTime)
		if err != nil {
			t.Fatalf("restricted in-org task must dispatch: %v", err)
		}
		if d.RunnerID != "runner-a" || d.State != StageRouted {
			t.Fatalf("want runner-a/ROUTED, got %s/%s", d.RunnerID, d.State)
		}
	})

	t.Run("cloud runner with verified proof serves in-region internal data", func(t *testing.T) {
		f := newKeys(t)
		b := NewBroker(f.keys)
		if _, err := b.RegisterProven("", "cloud-1", signed(cloudProof(baseTime), f), baseTime); err != nil {
			t.Fatal(err)
		}
		if err := b.Heartbeat("cloud-1", baseTime); err != nil {
			t.Fatal(err)
		}
		req := RouteRequest{
			DataClass: DataClassInternal, RequiredRegions: []string{"cn-north"},
			RequiredCapabilities: []string{"aml.scan"}, MaxEgress: EgressNone, SecretTier: SecretTierNone,
		}
		d, err := b.Route("task-1", ticketFor("01JDORG", baseTime), req, baseTime)
		if err != nil {
			t.Fatal(err)
		}
		if d.RunnerID != "cloud-1" {
			t.Fatalf("want cloud-1, got %s", d.RunnerID)
		}
	})

	t.Run("attestation defects all refuse registration with M9-019", func(t *testing.T) {
		f := newKeys(t)
		mk := func() *Broker { return NewBroker(f.keys) }

		// wrong kind (local must use RegisterLocal)
		p := cloudProof(baseTime)
		p.RunnerKind = RunnerKindLocal
		if _, err := mk().RegisterProven("", "c", signed(p, f), baseTime); !errors.Is(err, ErrAttestationInvalid) || Code(err) != "M9-019" {
			t.Fatalf("wrong kind: want M9-019, got %v", err)
		}
		// bad signature (signed by a non-ring key)
		p = cloudProof(baseTime)
		SignProof("rk1", f.wrong, &p)
		if _, err := mk().RegisterProven("", "c", p, baseTime); Code(err) != "M9-019" {
			t.Fatalf("bad signature: want M9-019, got %v", err)
		}
		// unknown key id
		p = cloudProof(baseTime)
		SignProof("rk-missing", f.priv, &p)
		if _, err := mk().RegisterProven("", "c", p, baseTime); Code(err) != "M9-019" {
			t.Fatalf("unknown key: want M9-019, got %v", err)
		}
		// expired proof
		p = cloudProof(baseTime.Add(-2 * ProofTTL))
		if _, err := mk().RegisterProven("", "c", signed(p, f), baseTime); Code(err) != "M9-019" {
			t.Fatalf("expired: want M9-019, got %v", err)
		}
		// issued in the future (beyond skew)
		p = cloudProof(baseTime.Add(10 * time.Minute))
		if _, err := mk().RegisterProven("", "c", signed(p, f), baseTime); Code(err) != "M9-019" {
			t.Fatalf("future proof: want M9-019, got %v", err)
		}
		// malformed fields
		p = cloudProof(baseTime)
		p.Nonce = ""
		if _, err := mk().RegisterProven("", "c", signed(p, f), baseTime); Code(err) != "M9-019" {
			t.Fatalf("empty nonce: want M9-019, got %v", err)
		}
		p = cloudProof(baseTime)
		p.IssuedAt = "yesterday"
		if _, err := mk().RegisterProven("", "c", signed(p, f), baseTime); Code(err) != "M9-019" {
			t.Fatalf("bad issued_at: want M9-019, got %v", err)
		}
		// nonce replay
		p = signed(cloudProof(baseTime), f)
		b := mk()
		if _, err := b.RegisterProven("", "c1", p, baseTime); err != nil {
			t.Fatal(err)
		}
		if _, err := b.RegisterProven("", "c2", p, baseTime); Code(err) != "M9-019" {
			t.Fatalf("nonce replay: want M9-019, got %v", err)
		}
	})

	t.Run("route-time proof expiry quarantines despite heartbeats (S3: heartbeat is not a proof)", func(t *testing.T) {
		f := newKeys(t)
		b := NewBroker(f.keys)
		if _, err := b.RegisterProven("", "cloud-1", signed(cloudProof(baseTime), f), baseTime); err != nil {
			t.Fatal(err)
		}
		later := baseTime.Add(61 * time.Minute)
		if err := b.Heartbeat("cloud-1", later.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		req := RouteRequest{DataClass: DataClassPublic, RequiredRegions: []string{"cn-north"}, MaxEgress: EgressNone, SecretTier: SecretTierNone}
		if _, err := b.Route("task-1", ticketFor("01JDORG", later), req, later); !errors.Is(err, ErrAttestationInvalid) || Code(err) != "M9-019" {
			t.Fatalf("want M9-019 at route time, got %v", err)
		}
		r, ok := b.GetRunner("cloud-1")
		if !ok || !r.Quarantined {
			t.Fatal("stale runner must be quarantined")
		}
		// heartbeat cannot restore trust
		if err := b.Heartbeat("cloud-1", later); Code(err) != "M9-019" {
			t.Fatalf("quarantined heartbeat: want M9-019, got %v", err)
		}
	})

	t.Run("T-13: restricted data with only a cloud runner blocks M9-021 without cross-domain fallback", func(t *testing.T) {
		f := newKeys(t)
		b := NewBroker(f.keys)
		if _, err := b.RegisterProven("", "cloud-1", signed(cloudProof(baseTime), f), baseTime); err != nil {
			t.Fatal(err)
		}
		if err := b.Heartbeat("cloud-1", baseTime); err != nil {
			t.Fatal(err)
		}
		req := inOrgReq()
		req.DataClass = DataClassRestricted
		req.RequiredRegions = []string{"cn-north"} // even in-region: restricted never goes to cloud
		_, err := b.Route("task-1", ticketFor("01JDORG", baseTime), req, baseTime)
		if !errors.Is(err, ErrResidencyMismatch) || Code(err) != "M9-021" {
			t.Fatalf("T-13: want M9-021, got %v", err)
		}
		if d, ok := b.GetDispatch("task-1"); ok {
			t.Fatalf("blocked task must not dispatch, got %+v", d)
		}
	})

	t.Run("sole runner outside required regions yields M9-021", func(t *testing.T) {
		f := newKeys(t)
		b := NewBroker(f.keys)
		p := cloudProof(baseTime)
		p.ResidencyRegions = []string{"us-west"}
		if _, err := b.RegisterProven("", "cloud-1", signed(p, f), baseTime); err != nil {
			t.Fatal(err)
		}
		if err := b.Heartbeat("cloud-1", baseTime); err != nil {
			t.Fatal(err)
		}
		req := RouteRequest{DataClass: DataClassInternal, RequiredRegions: []string{"cn-north"}, MaxEgress: EgressNone, SecretTier: SecretTierNone}
		if _, err := b.Route("task-1", ticketFor("01JDORG", baseTime), req, baseTime); Code(err) != "M9-021" {
			t.Fatalf("want M9-021, got %v", err)
		}
	})

	t.Run("constraint misses yield M9-022 (capability, trust, egress, secret)", func(t *testing.T) {
		// capability missing
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"other.skill"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		_ = b.Heartbeat("runner-a", baseTime)
		if _, err := b.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime); Code(err) != "M9-022" {
			t.Fatalf("capability: want M9-022, got %v", err)
		}

		// trusted required but runner untrusted (validly proven)
		f := newKeys(t)
		b2 := NewBroker(f.keys)
		p := cloudProof(baseTime)
		p.TrustTier = TrustUntrusted
		if _, err := b2.RegisterProven("", "cloud-1", signed(p, f), baseTime); err != nil {
			t.Fatal(err)
		}
		_ = b2.Heartbeat("cloud-1", baseTime)
		req := inOrgReq()
		req.RequiredRegions = []string{"cn-north"}
		req.RequireTrustedProof = true
		if _, err := b2.Route("task-1", ticketFor("01JDORG", baseTime), req, baseTime); Code(err) != "M9-022" {
			t.Fatalf("trust: want M9-022, got %v", err)
		}

		// egress above ceiling (S8 first half)
		b3 := NewBroker(newKeys(t).keys)
		if _, err := b3.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressOpen); err != nil {
			t.Fatal(err)
		}
		_ = b3.Heartbeat("runner-a", baseTime)
		req2 := inOrgReq()
		req2.MaxEgress = EgressPolicy
		if _, err := b3.Route("task-1", ticketFor("01JDORG", baseTime), req2, baseTime); Code(err) != "M9-022" {
			t.Fatalf("egress ceiling: want M9-022, got %v", err)
		}

		// in-org secret must not reach an egress runner (S8 second half)
		req3 := inOrgReq()
		req3.SecretTier = SecretTierInOrg
		if _, err := b3.Route("task-2", ticketFor("01JDORG", baseTime), req3, baseTime); Code(err) != "M9-022" {
			t.Fatalf("secret/egress: want M9-022, got %v", err)
		}

		// empty registry
		b4 := NewBroker(newKeys(t).keys)
		if _, err := b4.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime); Code(err) != "M9-022" {
			t.Fatalf("empty pool: want M9-022, got %v", err)
		}
	})

	t.Run("capable runner without heartbeat yields M9-020", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime); Code(err) != "M9-020" {
			t.Fatalf("never heartbeated: want M9-020, got %v", err)
		}
	})

	t.Run("stale heartbeat counts as offline (M9-020)", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		if err := b.Heartbeat("runner-a", baseTime.Add(-5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime); Code(err) != "M9-020" {
			t.Fatalf("stale heartbeat: want M9-020, got %v", err)
		}
	})

	t.Run("T-14: UNKNOWN blocks re-dispatch, locks the runner for side effects, spares healthy runners", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		if _, err := b.RegisterLocal("01JDORG", "runner-b", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		_ = b.Heartbeat("runner-a", baseTime)
		_ = b.Heartbeat("runner-b", baseTime)

		req := inOrgReq()
		req.SideEffects = true
		d, err := b.Route("task-1", ticketFor("01JDORG", baseTime), req, baseTime)
		if err != nil {
			t.Fatal(err)
		}
		if d.RunnerID != "runner-a" {
			t.Fatalf("deterministic pick: want runner-a, got %s", d.RunnerID)
		}

		// runner loses contact mid side-effect run
		lost := baseTime.Add(time.Minute)
		if err := b.MarkUnknown("task-1", lost); err != nil {
			t.Fatal(err)
		}
		if d, _ := b.GetDispatch("task-1"); d.State != ProjUnknown {
			t.Fatalf("want UNKNOWN, got %s", d.State)
		}

		// re-dispatching the same side-effect task is refused (副作用不重复)
		if _, err := b.Route("task-1", ticketFor("01JDORG", lost), req, lost); !errors.Is(err, ErrRunnerOffline) || Code(err) != "M9-020" {
			t.Fatalf("UNKNOWN re-route: want M9-020, got %v", err)
		}

		// even a returning heartbeat cannot unlock UNKNOWN (S5 heartbeat forger)
		if err := b.Heartbeat("runner-a", lost); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Route("task-1", ticketFor("01JDORG", lost), req, lost); Code(err) != "M9-020" {
			t.Fatalf("UNKNOWN re-route after heartbeat: want M9-020, got %v", err)
		}

		// the implicated runner takes no NEW side-effect work while unresolved
		req2 := inOrgReq()
		req2.SideEffects = true
		req2.RequiredRegions = []string{ResidencyInOrg}
		// force runner-a as the only in-region candidate: unregister runner-b
		if err := b.Unregister("runner-b"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Route("task-2", ticketFor("01JDORG", lost), req2, lost); Code(err) != "M9-020" {
			t.Fatalf("UNKNOWN-locked runner new side-effect work: want M9-020, got %v", err)
		}
	})

	t.Run("duplicate Route is idempotent: same runner, no second dispatch (S6)", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		_ = b.Heartbeat("runner-a", baseTime)
		first, err := b.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime)
		if err != nil {
			t.Fatal(err)
		}
		again, err := b.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime.Add(time.Second))
		if err != nil {
			t.Fatalf("replay must not fail: %v", err)
		}
		if again != first || again.RunnerID != first.RunnerID {
			t.Fatal("replay must return the existing dispatch unchanged")
		}
	})

	t.Run("UNKNOWN recovery only via approved M6 reconciliation; conflicts fail closed", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		_ = b.Heartbeat("runner-a", baseTime)
		req := inOrgReq()
		req.SideEffects = true
		if _, err := b.Route("task-1", ticketFor("01JDORG", baseTime), req, baseTime); err != nil {
			t.Fatal(err)
		}
		_ = b.MarkUnknown("task-1", baseTime.Add(time.Minute))

		// no governance approval -> refused
		if err := b.Reconcile("task-1", M6Fact{CloudTaskState: m6supply.CloudTaskSucceeded}, false, baseTime); err == nil {
			t.Fatal("reconciliation without approval must fail")
		}
		// non-verdict fact -> fail closed, stays UNKNOWN
		if err := b.Reconcile("task-1", M6Fact{CloudTaskState: m6supply.CloudTaskCreated}, true, baseTime); err == nil {
			t.Fatal("created is not a recovery verdict")
		}
		if d, _ := b.GetDispatch("task-1"); d.State != ProjUnknown || d.UnknownSince == nil {
			t.Fatalf("must stay UNKNOWN, got %s", d.State)
		}
		// approved M6 verdict resolves the projection
		if err := b.Reconcile("task-1", M6Fact{CloudTaskState: m6supply.CloudTaskSucceeded}, true, baseTime); err != nil {
			t.Fatal(err)
		}
		d, _ := b.GetDispatch("task-1")
		if d.State != ProjSucceeded || d.UnknownSince != nil {
			t.Fatalf("want SUCCEEDED resolved, got %s", d.State)
		}
		// after resolution the runner serves side-effect work again
		_ = b.Heartbeat("runner-a", baseTime.Add(2*time.Minute))
		if _, err := b.Route("task-2", ticketFor("01JDORG", baseTime.Add(2*time.Minute)), req, baseTime.Add(2*time.Minute)); err != nil {
			t.Fatalf("runner must be unlocked after reconciliation: %v", err)
		}
	})

	t.Run("M6 projections map verbatim and fail closed on unknown facts (S7)", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDORG", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		_ = b.Heartbeat("runner-a", baseTime)
		if _, err := b.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime); err != nil {
			t.Fatal(err)
		}

		steps := []struct {
			fact M6Fact
			want string
		}{
			{M6Fact{CloudTaskState: m6supply.CloudTaskCreated}, StageRouted},       // pre-dispatch: no change
			{M6Fact{CloudTaskState: m6supply.CloudTaskQueued}, StageRouted},        // pre-dispatch: no change
			{M6Fact{CloudTaskState: m6supply.CloudTaskLeased}, ProjDispatched},
			{M6Fact{CloudTaskState: m6supply.CloudTaskRunning, CheckpointReceipt: true}, ProjCheckpointed},
			{M6Fact{CloudTaskState: m6supply.CloudTaskJoining}, ProjCheckpointed},
			{M6Fact{CloudTaskState: m6supply.CloudTaskSucceeded}, ProjSucceeded},
		}
		for _, s := range steps {
			if err := b.ProjectM6("task-1", s.fact, baseTime); err != nil {
				t.Fatalf("%s: %v", s.fact.CloudTaskState, err)
			}
			if d, _ := b.GetDispatch("task-1"); d.State != s.want {
				t.Fatalf("%s: want %s, got %s", s.fact.CloudTaskState, s.want, d.State)
			}
		}

		// compensating verdict on a fresh task
		if _, err := b.Route("task-2", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime); err != nil {
			t.Fatal(err)
		}
		if err := b.ProjectM6("task-2", M6Fact{CloudTaskState: m6supply.CloudTaskFailed, Compensating: true}, baseTime); err != nil {
			t.Fatal(err)
		}
		if d, _ := b.GetDispatch("task-2"); d.State != ProjCompensating {
			t.Fatalf("want COMPENSATING, got %s", d.State)
		}

		// fail closed: unmappable facts
		if err := b.ProjectM6("task-2", M6Fact{CloudTaskState: m6supply.CloudTaskFailed}, baseTime); err == nil {
			t.Fatal("failed-without-compensation must fail closed")
		}
		if err := b.ProjectM6("task-2", M6Fact{CloudTaskState: m6supply.CloudTaskCancelled}, baseTime); err == nil {
			t.Fatal("cancelled has no M9 projection and must fail closed")
		}
		if err := b.ProjectM6("task-2", M6Fact{CloudTaskState: "bogus"}, baseTime); err == nil {
			t.Fatal("unknown M6 state must fail closed")
		}
		// UNKNOWN dispatch refuses direct projection (reconciliation only)
		_ = b.MarkUnknown("task-2", baseTime)
		if err := b.ProjectM6("task-2", M6Fact{CloudTaskState: m6supply.CloudTaskSucceeded}, baseTime); err == nil {
			t.Fatal("projection onto UNKNOWN must fail closed")
		}
	})

	t.Run("task org comes from the ticket; foreign local runners are invisible (S4)", func(t *testing.T) {
		b := NewBroker(newKeys(t).keys)
		if _, err := b.RegisterLocal("01JDOTHER", "runner-a", []string{"aml.scan"}, EgressNone); err != nil {
			t.Fatal(err)
		}
		_ = b.Heartbeat("runner-a", baseTime)
		if _, err := b.Route("task-1", ticketFor("01JDORG", baseTime), inOrgReq(), baseTime); Code(err) != "M9-022" {
			t.Fatalf("foreign org runner: want M9-022, got %v", err)
		}

		// ticket validation: binding incomplete / expired
		tk := ticketFor("01JDORG", baseTime)
		tk.BudgetReservationID = ""
		if _, err := b.Route("task-2", tk, inOrgReq(), baseTime); err == nil {
			t.Fatal("unbound budget must refuse routing")
		}
		tk = ticketFor("01JDORG", baseTime)
		tk.ExpiresAt = baseTime.Add(-time.Second)
		if _, err := b.Route("task-3", tk, inOrgReq(), baseTime); err == nil {
			t.Fatal("expired ticket must refuse routing")
		}
	})
}
