package stdioworker

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAudit collects audit events for assertions.
type fakeAudit struct {
	mu     sync.Mutex
	events []struct{ action, aggregate string }
}

func (f *fakeAudit) Emit(action, aggregateID, actor string, metadata []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, struct{ action, aggregate string }{action, aggregateID})
	return nil
}

func (f *fakeAudit) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.action)
	}
	return out
}

func hasAction(list []string, action string) bool {
	for _, a := range list {
		if a == action {
			return true
		}
	}
	return false
}

// TestStdioWorkerChildHelper is the worker child entrypoint: the runtime
// spawns the test binary with argv after "--" driving the mode.
func TestStdioWorkerChildHelper(t *testing.T) {
	args := os.Args
	var rest []string
	for i, arg := range args {
		if arg == "--" {
			rest = args[i+1:]
			break
		}
	}
	if len(rest) == 0 {
		return // normal test run
	}
	if rest[0] == "rt" {
		// 5C red-team attacker child: [-- rt MODE CFGJSON]
		if len(rest) < 3 {
			os.Exit(2)
		}
		os.Exit(rtChildMain(rest[1], rest[2]))
	}
	mode := rest[0]
	child, err := ChildFromEnv(os.Getenv, os.Stdout)
	if err != nil {
		os.Exit(2)
	}
	switch mode {
	case "echo":
		_ = child.Hello()
		_ = child.Heartbeat()
		_ = child.Result(map[string]any{"ok": true, "mode": mode})
	case "silent":
		_ = child.Hello()
		select {}
	case "forever":
		_ = child.Hello()
		for {
			time.Sleep(200 * time.Millisecond)
			_ = child.Heartbeat()
		}
	case "forged-session":
		// protocol cheating: wrong session id
		_ = WriteEnvelope(os.Stdout, &Envelope{SessionID: "evil-session", Seq: 0, Type: EnvHello})
		select {}
	case "seq-gap":
		_ = child.Hello()
		_ = WriteEnvelope(os.Stdout, &Envelope{SessionID: child.SessionID(), Seq: 42, Type: EnvHeartbeat})
		select {}
	case "hello-digest-lie":
		// claims a different spec digest
		env := Envelope{SessionID: child.SessionID(), Seq: 0, Type: EnvHello, Data: []byte(`{"specDigest":"deadbeef","protocol":"` + DefaultPolicy().ProtocolVer + `"}`)}
		_ = WriteEnvelope(os.Stdout, &env)
		select {}
	}
	os.Exit(0)
}

func helperSpec(t *testing.T, mode string, mutate func(*LaunchSpec)) LaunchSpec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := FileDigest(exe)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	spec := LaunchSpec{
		SpecID:        "spec-" + mode,
		EndpointID:    "ep-test",
		Command:       exe,
		Args:          []string{"-test.run", "^TestStdioWorkerChildHelper$", "--", mode},
		ExeDigest:     digest,
		CapabilitySet: []string{"mcp.tools.read"},
		Quotas: Quotas{
			MaxProcs: 4, MemoryCapBytes: 384 << 20,
			DeadlineMS: 30_000, HeartbeatMS: 500, MaxMissedBeats: 2,
		},
		WorkingDir: filepath.Join(dir, "sbx"),
		Nonce:      NewNonce(),
		NotBefore:  time.Now().Add(-time.Minute),
		ExpiresAt:  time.Now().Add(10 * time.Minute),
		KeyID:      "k-test",
	}
	if mutate != nil {
		mutate(&spec)
	}
	return spec
}

// testKeys bundles the verification map with the matching signing key.
type testKeys struct {
	verify MapKeyStore
	priv   ed25519.PrivateKey
}

func makeTestKeys(t *testing.T) testKeys {
	t.Helper()
	pub, priv := testKeyPair(t)
	return testKeys{verify: MapKeyStore{"k-test": pub}, priv: priv}
}

func signTest(t *testing.T, tk testKeys, spec LaunchSpec) *SignedSpec {
	t.Helper()
	sp, err := Sign(spec, tk.priv, "k-test")
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func newTestManager(t *testing.T, tk testKeys, audit AuditSink) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(Gate{SignedOffBy: "test-harness"}, tk.verify, dir, audit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// TestGateClosedByDefault: zero-value gate refuses production launches.
func TestGateClosedByDefault(t *testing.T) {
	tk := makeTestKeys(t)
	dir := t.TempDir()
	closed, err := NewManager(Gate{}, tk.verify, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closed.Close()
	spec := helperSpec(t, "echo", nil)
	sp := signTest(t, tk, spec)
	if _, err := closed.Launch(context.Background(), sp); err == nil || !strings.Contains(err.Error(), "gate is closed") {
		t.Fatalf("want gate closed error, got %v", err)
	}
}

// TestLaunchEchoRoundTrip: full happy path — verify, spawn, HELLO binding,
// result, journal and audit contract.
func TestLaunchEchoRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	tk := makeTestKeys(t)
	audit := &fakeAudit{}
	m := newTestManager(t, tk, audit)
	spec := helperSpec(t, "echo", nil)
	pd, err := m.PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	spec.ConfigDigest = pd
	sp := signTest(t, tk, spec)
	r, err := m.Launch(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if r.State() != StateCompleted {
		t.Fatalf("state = %s, want COMPLETED", r.State())
	}
	var payload map[string]any
	if err := json.Unmarshal(r.Result(), &payload); err != nil {
		t.Fatalf("result payload: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("result payload = %v", payload)
	}
	actions := audit.actions()
	if !hasAction(actions, AuditLaunched) || !hasAction(actions, AuditCompleted) {
		t.Fatalf("audit actions = %v", actions)
	}
	unrec, err := UnrecoveredRuns(m.journal.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrec) != 0 {
		t.Fatalf("run must be terminal in journal, got %+v", unrec)
	}
}

// TestHeartbeatLoss: silent child → EXPIRED.
func TestHeartbeatLoss(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	tk := makeTestKeys(t)
	audit := &fakeAudit{}
	m := newTestManager(t, tk, audit)
	spec := helperSpec(t, "silent", func(s *LaunchSpec) {
		s.Quotas.HeartbeatMS = 300
		s.Quotas.MaxMissedBeats = 1
		s.Quotas.DeadlineMS = 20_000
	})
	sp := signTest(t, tk, spec)
	r, err := m.Launch(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = r.Wait(ctx)
	if r.State() != StateExpired {
		t.Fatalf("state = %s, want EXPIRED", r.State())
	}
	if !hasAction(audit.actions(), AuditExpired) {
		t.Fatalf("want expired audit, got %v", audit.actions())
	}
}

// TestDeadlineEnforced: forever child + tight deadline → EXPIRED.
func TestDeadlineEnforced(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	tk := makeTestKeys(t)
	m := newTestManager(t, tk, nil)
	spec := helperSpec(t, "forever", func(s *LaunchSpec) {
		s.Quotas.DeadlineMS = 1500
		s.Quotas.HeartbeatMS = 300
		s.Quotas.MaxMissedBeats = 20 // deadline must fire first
	})
	sp := signTest(t, tk, spec)
	r, err := m.Launch(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = r.Wait(ctx)
	if r.State() != StateExpired {
		t.Fatalf("state = %s, want EXPIRED (deadline)", r.State())
	}
}

// TestRevokeFreezesResult: revocation kills the tree and late results are
// frozen (M6-SBX-004 semantics).
func TestRevokeFreezesResult(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	tk := makeTestKeys(t)
	audit := &fakeAudit{}
	m := newTestManager(t, tk, audit)
	spec := helperSpec(t, "forever", func(s *LaunchSpec) {
		s.Quotas.DeadlineMS = 60_000
	})
	sp := signTest(t, tk, spec)
	r, err := m.Launch(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // let it hello+beat
	if err := m.Revoke(r.ID, "credential rotation"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Wait(ctx); err == nil {
		t.Fatal("revoked run must Wait with ErrRevoked")
	}
	if r.State() != StateRevoked {
		t.Fatalf("state = %s, want REVOKED", r.State())
	}
	if r.Result() != nil {
		t.Fatal("revoked run must not hold a result")
	}
	if !hasAction(audit.actions(), AuditRevoked) {
		t.Fatalf("want revoked audit, got %v", audit.actions())
	}
}

// TestSupplyChainReject: wrong exe digest never spawns.
func TestSupplyChainReject(t *testing.T) {
	tk := makeTestKeys(t)
	m := newTestManager(t, tk, nil)
	spec := helperSpec(t, "echo", func(s *LaunchSpec) {
		s.ExeDigest = strings.Repeat("00", 32)
	})
	sp := signTest(t, tk, spec)
	if _, err := m.Launch(context.Background(), sp); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("want supply-chain reject, got %v", err)
	}
}

// TestQuotaPolicyReject: quotas above the frozen policy are refused.
func TestQuotaPolicyReject(t *testing.T) {
	tk := makeTestKeys(t)
	m := newTestManager(t, tk, nil)
	spec := helperSpec(t, "echo", func(s *LaunchSpec) {
		s.Quotas.MaxProcs = DefaultPolicy().MaxQuotaProcs + 1
	})
	sp := signTest(t, tk, spec)
	if _, err := m.Launch(context.Background(), sp); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("want quota policy reject, got %v", err)
	}
}

// TestProtocolForgery: forged session id → protocol violation expiry.
func TestProtocolForgery(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	tk := makeTestKeys(t)
	m := newTestManager(t, tk, nil)
	spec := helperSpec(t, "forged-session", func(s *LaunchSpec) {
		s.Quotas.HeartbeatMS = 300
		s.Quotas.MaxMissedBeats = 1
	})
	sp := signTest(t, tk, spec)
	r, err := m.Launch(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = r.Wait(ctx)
	if r.State() != StateExpired {
		t.Fatalf("state = %s, want EXPIRED (forged session)", r.State())
	}
}

// TestSequenceGapKills: sequence gap → protocol violation expiry.
func TestSequenceGapKills(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	tk := makeTestKeys(t)
	m := newTestManager(t, tk, nil)
	spec := helperSpec(t, "seq-gap", func(s *LaunchSpec) {
		s.Quotas.HeartbeatMS = 300
		s.Quotas.MaxMissedBeats = 1
	})
	sp := signTest(t, tk, spec)
	r, err := m.Launch(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = r.Wait(ctx)
	if r.State() != StateExpired {
		t.Fatalf("state = %s, want EXPIRED (sequence gap)", r.State())
	}
}

// TestHelloDigestLie: child claims a different spec digest → killed.
func TestHelloDigestLie(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	tk := makeTestKeys(t)
	m := newTestManager(t, tk, nil)
	spec := helperSpec(t, "hello-digest-lie", func(s *LaunchSpec) {
		s.Quotas.HeartbeatMS = 300
		s.Quotas.MaxMissedBeats = 1
	})
	sp := signTest(t, tk, spec)
	r, err := m.Launch(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = r.Wait(ctx)
	if r.State() != StateExpired {
		t.Fatalf("state = %s, want EXPIRED (digest lie)", r.State())
	}
}

// TestRecoverMarksCrashed: journal-only walk (no live process needed).
func TestRecoverMarksCrashed(t *testing.T) {
	tk := makeTestKeys(t)
	audit := &fakeAudit{}
	dir := t.TempDir()
	// pre-seed a journal as if a previous host crashed mid-run
	j, err := OpenJournal(filepath.Join(dir, "recovery-journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalRecord{RunID: "ghost", SpecID: "s9", Endpoint: "ep-test", State: StateLaunched, AtMS: 1}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(Gate{SignedOffBy: "test-harness"}, tk.verify, dir, audit)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	recs, err := m.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Record.RunID != "ghost" {
		t.Fatalf("want ghost recovered, got %+v", recs)
	}
	if !hasAction(audit.actions(), AuditRecovered) {
		t.Fatalf("want recovered audit, got %v", audit.actions())
	}
	// second walk is idempotent
	recs2, err := m.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs2) != 0 {
		t.Fatalf("recover must be idempotent, got %+v", recs2)
	}
}

// TestConfigDigestBinding: a spec minted against another policy digest is
// refused (5A/5B/5C same-digest binding).
func TestConfigDigestBinding(t *testing.T) {
	tk := makeTestKeys(t)
	m := newTestManager(t, tk, nil)
	spec := helperSpec(t, "echo", func(s *LaunchSpec) {
		s.ConfigDigest = strings.Repeat("00", 32)
	})
	sp := signTest(t, tk, spec)
	if _, err := m.Launch(context.Background(), sp); err == nil || !strings.Contains(err.Error(), "config digest drift") {
		t.Fatalf("want config digest drift, got %v", err)
	}
}
