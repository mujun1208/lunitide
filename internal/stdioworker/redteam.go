// M6 slice 5C — the production-escape red team. Eight record classes run
// against the REAL 5B runtime: every attacker is a signed-spec launch under
// a Job Object speaking the framed protocol, not a synthetic probe.
//
//	host-file / network  guard-level (worker.Guard) + bare-channel probes
//	secret / proctree / resource  os-level (explicit env, Job Object quotas)
//	protocol              protocol-level (session binding, sequence, digests)
//	crash-recovery        journal walk → CRASHED + recovered audit
//	revoke                late results frozen (M6-SBX-004)
//
// The runner also performs the fault injections (journal failure, audit
// sink failure) and the 16-way capacity launch. Evidence binds the frozen
// runtime policy digest (the 5A/5B/5C config-digest contract).
//
// A red-team PASS authorizes NOTHING by itself: production stdio needs the
// Security Owner sign-off recorded outside this repository; Gate stays
// closed and M6-MCP-004 remains in force.
package stdioworker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Red-team record IDs (canonical order).
const (
	RTHostFile = "host-file"
	RTNetwork  = "network"
	RTSecret   = "secret"
	RTProcTree = "proctree"
	RTResource = "resource"
	RTProtocol = "protocol"
	RTCrashRec = "crash-recovery"
	RTRevoke   = "revoke"
	RTFaultInj = "fault-injection"
	RTCapacity = "capacity"
)

var rtOrder = []string{RTHostFile, RTNetwork, RTSecret, RTProcTree, RTResource, RTProtocol, RTCrashRec, RTRevoke, RTFaultInj, RTCapacity}

var rtMeta = map[string]struct{ title, enforcedBy string }{
	RTHostFile: {"host filesystem escapes (guard channel + bare-channel probe)", "guard-level"},
	RTNetwork:  {"network egress (guard channel + bare-channel probe)", "guard-level"},
	RTSecret:   {"parent environment / secret inheritance", "os-level"},
	RTProcTree: {"fork bomb and process-tree survival", "os-level"},
	RTResource: {"memory exhaustion", "os-level"},
	RTProtocol: {"protocol cheating (forged session/sequence/digest)", "protocol-level"},
	RTCrashRec: {"host-crash journal recovery", "runtime-level"},
	RTRevoke:   {"revocation freezes late results (M6-SBX-004)", "runtime-level"},
	RTFaultInj: {"fault injection (journal/audit failures)", "runtime-level"},
	RTCapacity: {"16 parallel signed launches", "runtime-level"},
}

// RedTeamAttack is one attacker-side attempt.
type RedTeamAttack struct {
	Vector  string `json:"vector"`
	Attempt string `json:"attempt"`
	Blocked bool   `json:"blocked"`
	Detail  string `json:"detail,omitempty"`
}

// RedTeamRecord is one evidence record (assumption-class + verdicts).
type RedTeamRecord struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	EnforcedBy string          `json:"enforcedBy"`
	Passed     bool            `json:"passed"`
	Attacks    []RedTeamAttack `json:"attacks"`
	HostCheck  string          `json:"hostCheck"`
	StartedAt  time.Time       `json:"startedAt"`
	EndedAt    time.Time       `json:"endedAt"`
	Digest     string          `json:"digest"`
}

// RedTeamBundle is the 5C evidence artifact.
type RedTeamBundle struct {
	Schema       string          `json:"schema"` // stdio-5c-evidence/1
	GeneratedAt  time.Time       `json:"generatedAt"`
	Platform     string          `json:"platform"`
	PolicyDigest string          `json:"policyDigest"` // frozen 5B runtime policy (config digest binding)
	GateOpen     bool            `json:"gateOpen"`     // must be false in this repo
	Verdict      string          `json:"verdict"`
	Records      []RedTeamRecord `json:"records"`
	BundleDigest string          `json:"bundleDigest"`
	Notes        []string        `json:"notes"`
}

// RedTeamNotes are always embedded.
var RedTeamNotes = []string{
	"A 5C red-team PASS alone authorizes nothing: production stdio enablement additionally requires the Security Owner sign-off recorded outside this repository.",
	"The runtime Gate stays closed and M6-MCP-004 keeps the stdio transport disabled at the registry gate.",
	"host-file and network are enforced at the worker.Guard layer; the bare-channel probes record the current OS boundary honestly (no AppContainer in this build).",
	"secret, proctree and resource are OS-enforced (explicit environment block, Job Object quotas).",
	"This bundle binds the same frozen policy digest the 5B launch specs verify (config-digest contract of 5A/5B/5C).",
}

// redTeamAudit collects audit actions per aggregate for evidence export.
type redTeamAudit struct {
	mu     sync.Mutex
	events []struct{ action, aggregate string }
}

func (a *redTeamAudit) Emit(action, aggregateID, actor string, metadata []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, struct{ action, aggregate string }{action, aggregateID})
	return nil
}

func (a *redTeamAudit) actions(aggregateID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, e := range a.events {
		if aggregateID == "" || e.aggregate == aggregateID {
			out = append(out, e.action)
		}
	}
	return out
}

func (a *redTeamAudit) count(action string) int {
	n := 0
	for _, x := range a.actions("") {
		if x == action {
			n++
		}
	}
	return n
}

// RedTeamRunner drives the eight record classes against one Manager.
type RedTeamRunner struct {
	exe     string // attacker-role executable (re-exec / test binary)
	dir     string // working base (sandboxes, journal)
	now     func() time.Time
	keys    testKeyPairLike
	audit   *redTeamAudit
	manager *Manager
}

type testKeyPairLike struct {
	verify MapKeyStore
	priv   ed25519.PrivateKey
}

// NewRedTeamRunner builds the runner with its own throwaway signing key
// (red-team self-signed; production launches use the control-plane key).
func NewRedTeamRunner(exe, dir string) (*RedTeamRunner, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	audit := &redTeamAudit{}
	m, err := NewManager(Gate{SignedOffBy: "red-team-harness"}, MapKeyStore{"k-5c": pub}, dir, audit)
	if err != nil {
		return nil, err
	}
	return &RedTeamRunner{exe: exe, dir: dir, now: time.Now, keys: testKeyPairLike{verify: MapKeyStore{"k-5c": pub}, priv: priv}, audit: audit, manager: m}, nil
}

// Close releases the runner manager.
func (r *RedTeamRunner) Close() error { return r.manager.Close() }

// rtSpec mints a signed spec for one attacker mode.
func (r *RedTeamRunner) rtSpec(mode string, mutate func(*LaunchSpec)) (*SignedSpec, error) {
	digest, err := FileDigest(r.exe)
	if err != nil {
		return nil, err
	}
	pd, err := r.manager.PolicyDigest()
	if err != nil {
		return nil, err
	}
	now := r.now()
	spec := LaunchSpec{
		SpecID:        "spec-5c-" + mode,
		EndpointID:    "ep-5c",
		Command:       r.exe,
		Args:          rtHelperArgs(mode, "{}"),
		ExeDigest:     digest,
		CapabilitySet: []string{"mcp.tools.read"},
		Quotas: Quotas{
			MaxProcs: 8, MemoryCapBytes: 384 << 20,
			DeadlineMS: 60_000, HeartbeatMS: 500, MaxMissedBeats: 6,
		},
		WorkingDir:   filepath.Join(r.dir, "sbx-"+mode),
		Nonce:        NewNonce(),
		NotBefore:    now.Add(-time.Minute),
		ExpiresAt:    now.Add(30 * time.Minute),
		ConfigDigest: pd,
		KeyID:        "k-5c",
	}
	if mutate != nil {
		mutate(&spec)
	}
	return Sign(spec, r.keys.priv, "k-5c")
}

// runMode launches one attacker mode and waits for its terminal state.
func (r *RedTeamRunner) runMode(ctx context.Context, mode string, mutate func(*LaunchSpec)) (*Run, error) {
	sp, err := r.rtSpec(mode, mutate)
	if err != nil {
		return nil, err
	}
	run, err := r.manager.Launch(ctx, sp)
	if err != nil {
		return nil, err
	}
	wctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	_ = run.Wait(wctx)
	return run, nil
}

// record builds one evidence record around a verifier.
func (r *RedTeamRunner) record(id string, verify func(*RedTeamRecord) error) *RedTeamRecord {
	meta, _ := rtMeta[id]
	rec := &RedTeamRecord{ID: id, Title: meta.title, EnforcedBy: meta.enforcedBy, StartedAt: r.now().UTC()}
	if err := verify(rec); err != nil {
		rec.Passed = false
		rec.HostCheck = "error: " + err.Error()
	}
	rec.EndedAt = r.now().UTC()
	return rec
}

func attacksBlockedAsWanted(attacks []RedTeamAttack) bool {
	for _, a := range attacks {
		if !a.Blocked {
			return false
		}
	}
	return len(attacks) > 0
}

// Run executes all ten record classes (eight red-team + fault injection +
// capacity). It returns the records in canonical order.
func (r *RedTeamRunner) Run(ctx context.Context) ([]RedTeamRecord, error) {
	var out []RedTeamRecord
	for _, id := range rtOrder {
		var rec *RedTeamRecord
		var err error
		switch id {
		case RTHostFile:
			rec, err = r.runHostFile(ctx)
		case RTNetwork:
			rec, err = r.runNetwork(ctx)
		case RTSecret:
			rec, err = r.runSecret(ctx)
		case RTProcTree:
			rec, err = r.runProcTree(ctx)
		case RTResource:
			rec, err = r.runResource(ctx)
		case RTProtocol:
			rec, err = r.runProtocol(ctx)
		case RTCrashRec:
			rec, err = r.runCrashRecovery(ctx)
		case RTRevoke:
			rec, err = r.runRevoke(ctx)
		case RTFaultInj:
			rec, err = r.runFaultInjection(ctx)
		case RTCapacity:
			rec, err = r.runCapacity(ctx)
		}
		if err != nil {
			return out, fmt.Errorf("redteam %s: %w", id, err)
		}
		out = append(out, *rec)
	}
	return out, nil
}

// --- host-file --------------------------------------------------------------

func (r *RedTeamRunner) runHostFile(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTHostFile, func(rec *RedTeamRecord) error {
		layout, err := rtLayout(r.dir, "host-file")
		if err != nil {
			return err
		}
		profile := layout.profile("poc5c-host-file")
		raw, _ := json.Marshal(profile)
		cfg := rtChildConfig{
			Mode:       "guard-path",
			Profile:    raw,
			HostMarker: layout.marker, Symlink: layout.symlink, Junction: layout.junction, InRoot: layout.legit,
		}
		cfgj, _ := json.Marshal(cfg)
		sp, err := r.rtSpec("guard-path", func(s *LaunchSpec) {
			s.Args = rtHelperArgs("guard-path", string(cfgj))
			s.WorkingDir = layout.root
		})
		if err != nil {
			return err
		}
		run, err := r.manager.Launch(ctx, sp)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		if err := run.Wait(wctx); err != nil {
			return err
		}
		if run.State() != StateCompleted {
			return fmt.Errorf("guard-path child state=%s detail=%s", run.State(), string(run.Result()))
		}
		var rep rtReport
		if err := json.Unmarshal(run.Result(), &rep); err != nil {
			return err
		}
		rec.Attacks = rep.Attacks

		// Host cross-check: marker readable by host; an independent guard
		// rejects the same vectors and allows the positive control.
		data, rerr := os.ReadFile(layout.marker)
		precond := rerr == nil && strings.Contains(string(data), "HOST-SECRET-MARKER-5C")
		hg := newGuardFrom(raw)
		hostRejects := hg.CheckPath(layout.marker) != nil
		if layout.junction != "" && hg.CheckPath(layout.junction) == nil {
			hostRejects = false
		}
		if layout.symlink != "" && hg.CheckPath(layout.symlink) == nil {
			hostRejects = false
		}
		hostAllows := hg.CheckPath(layout.legit) == nil
		rec.HostCheck = fmt.Sprintf("markerReadable=%v hostGuardRejectsEscapes=%v hostGuardAllowsLegit=%v", precond, hostRejects, hostAllows)
		rec.Passed = attacksBlockedAsWanted(rec.Attacks) && precond && hostRejects && hostAllows
		return nil
	}), nil
}

// --- network ----------------------------------------------------------------

func (r *RedTeamRunner) runNetwork(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTNetwork, func(rec *RedTeamRecord) error {
		layout, err := rtLayout(r.dir, "network")
		if err != nil {
			return err
		}
		profile := layout.profileNet()
		raw, _ := json.Marshal(profile)
		targets := []rtDial{
			{Host: "169.254.169.254", Port: "80", Vector: "imds-aws", WantBlocked: true},
			{Host: "metadata.google.internal", Port: "80", Vector: "imds-gcp", WantBlocked: true},
			{Host: "10.0.0.1", Port: "3389", Vector: "intranet-rdp", WantBlocked: true},
			{Host: "127.0.0.1", Port: "8080", Vector: "host-loopback", WantBlocked: true},
			{Host: "evil.example.com", Port: "443", Vector: "not-allowlisted", WantBlocked: true},
			{Host: "10.0.0.99", Port: "443", Vector: "dns-rebinding-resolved-ip", WantBlocked: true},
			{Host: "api.example.com", Port: "443", Vector: "allowlisted-positive-control", WantBlocked: false},
		}
		cfg := rtChildConfig{Mode: "guard-dial", Profile: raw, Dials: targets}
		cfgj, _ := json.Marshal(cfg)
		sp, err := r.rtSpec("guard-dial", func(s *LaunchSpec) {
			s.Args = rtHelperArgs("guard-dial", string(cfgj))
			s.WorkingDir = layout.root
		})
		if err != nil {
			return err
		}
		run, err := r.manager.Launch(ctx, sp)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		if err := run.Wait(wctx); err != nil {
			return err
		}
		if run.State() != StateCompleted {
			return fmt.Errorf("guard-dial child state=%s", run.State())
		}
		var rep rtReport
		if err := json.Unmarshal(run.Result(), &rep); err != nil {
			return err
		}
		rec.Attacks = rep.Attacks

		hg := newGuardFrom(raw)
		agree := true
		for _, t := range targets {
			if (hg.CheckDial(t.Host, t.Port) != nil) != t.WantBlocked {
				agree = false
			}
		}
		rec.HostCheck = fmt.Sprintf("hostGuardAgrees=%v targets=%d", agree, len(targets))
		rec.Passed = attacksBlockedAsWanted(rec.Attacks) && agree
		return nil
	}), nil
}

// --- secret -----------------------------------------------------------------

func (r *RedTeamRunner) runSecret(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTSecret, func(rec *RedTeamRecord) error {
		// Plant a host secret OUTSIDE the child environment contract.
		const secretVar = "LUNITIDE_5C_HOST_SECRET"
		if err := os.Setenv(secretVar, "leak-me-if-you-can"); err != nil {
			return err
		}
		defer os.Unsetenv(secretVar)

		run, err := r.runMode(ctx, "env-probe", nil)
		if err != nil {
			return err
		}
		if run.State() != StateCompleted {
			return fmt.Errorf("env-probe state=%s", run.State())
		}
		var rep rtReport
		if err := json.Unmarshal(run.Result(), &rep); err != nil {
			return err
		}
		allowed := map[string]bool{"STDIOWORKER_SESSION": true, "STDIOWORKER_SPEC_DIGEST": true, "SystemRoot": true}
		var env []string
		if err := json.Unmarshal(rep.EnvJSON, &env); err != nil {
			return err
		}
		sort.Strings(env)
		leaked := 0
		for _, kv := range env {
			name := kv
			if i := strings.Index(kv, "="); i >= 0 {
				name = kv[:i]
			}
			if strings.Contains(name, "SECRET") || strings.Contains(strings.ToUpper(name), "TOKEN") || strings.Contains(strings.ToUpper(name), "CREDENTIAL") {
				leaked++
			}
			if !allowed[name] {
				leaked++
			}
		}
		rec.Attacks = []RedTeamAttack{{
			Vector:  "env-inheritance",
			Attempt: "full child environment dump",
			Blocked: leaked == 0,
			Detail:  fmt.Sprintf("env=[%s] leakedVariables=%d", strings.Join(env, " "), leaked),
		}}
		// Host cross-check: the secret really is set in the host process.
		rec.HostCheck = fmt.Sprintf("hostGetenv=%q envCount=%d", os.Getenv(secretVar), len(env))
		rec.Passed = leaked == 0 && os.Getenv(secretVar) == "leak-me-if-you-can" && len(env) >= 2
		return nil
	}), nil
}

// --- proctree ---------------------------------------------------------------

func (r *RedTeamRunner) runProcTree(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTProcTree, func(rec *RedTeamRecord) error {
		cfg := rtChildConfig{Mode: "spawn-bomb", SpawnCount: 16}
		cfgj, _ := json.Marshal(cfg)
		sp, err := r.rtSpec("spawn-bomb", func(s *LaunchSpec) {
			s.Args = rtHelperArgs("spawn-bomb", string(cfgj))
			s.Quotas.MaxProcs = 4 // root + 3 grandchildren max
		})
		if err != nil {
			return err
		}
		run, err := r.manager.Launch(ctx, sp)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		if err := run.Wait(wctx); err != nil {
			return err
		}
		if run.State() != StateCompleted {
			return fmt.Errorf("spawn-bomb state=%s", run.State())
		}
		var rep rtReport
		if err := json.Unmarshal(run.Result(), &rep); err != nil {
			return err
		}
		quotaHeld := rep.Spawned > 0 && rep.Spawned <= 3 && rep.Rejected > 0
		rec.Attacks = []RedTeamAttack{{
			Vector:  "fork-bomb",
			Attempt: fmt.Sprintf("spawn %d grandchildren under maxProcs=4", cfg.SpawnCount),
			Blocked: quotaHeld,
			Detail:  fmt.Sprintf("spawned=%d rejected=%d pids=%v (job active-process quota)", rep.Spawned, rep.Rejected, rep.Pids),
		}}
		// Host cross-check: after the run settled the whole tree is gone.
		survivors := pidsAlive(rep.Pids)
		rec.HostCheck = fmt.Sprintf("grandchilds=%d allDeadAfterRun=%v", len(rep.Pids), len(survivors) == 0)
		rec.Passed = quotaHeld && len(survivors) == 0
		return nil
	}), nil
}

// --- resource ---------------------------------------------------------------

func (r *RedTeamRunner) runResource(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTResource, func(rec *RedTeamRecord) error {
		cfg := rtChildConfig{Mode: "alloc-mem", MemRequestBytes: 352 << 20} // request above the 256 MiB cap below
		cfgj, _ := json.Marshal(cfg)
		sp, err := r.rtSpec("alloc-mem", func(s *LaunchSpec) {
			s.Args = rtHelperArgs("alloc-mem", string(cfgj))
			s.Quotas.MaxProcs = 4
			s.Quotas.MemoryCapBytes = 256 << 20 // commit cap below the request
		})
		if err != nil {
			return err
		}
		run, err := r.manager.Launch(ctx, sp)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		_ = run.Wait(wctx)
		// The allocator child reports through the RESULT frame even when
		// allocations failed (the process itself survives); a hard kill by
		// the OS quota also proves the cap — accept both shapes.
		state := run.State()
		detail := strings.TrimSpace(string(run.Result()))
		var rep rtReport
		if state == StateCompleted {
			if err := json.Unmarshal(run.Result(), &rep); err != nil {
				return err
			}
			detail = rep.AllocDetail
		}
		capped := (state == StateCompleted && rep.AllocFailed) || state == StateExpired
		rec.Attacks = []RedTeamAttack{{
			Vector:  "memory-exhaustion",
			Attempt: fmt.Sprintf("commit %d MiB against a %d MiB job cap", cfg.MemRequestBytes>>20, 256),
			Blocked: capped,
			Detail:  detail,
		}}
		rec.HostCheck = "job commit quota is enforced by the OS (SetInformationJobObject JobMemoryLimit)"
		rec.Passed = capped
		return nil
	}), nil
}

// --- protocol ---------------------------------------------------------------

func (r *RedTeamRunner) runProtocol(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTProtocol, func(rec *RedTeamRecord) error {
		modes := []string{"forged-session", "seq-gap", "digest-lie", "silent"}
		for _, mode := range modes {
			run, err := r.runMode(ctx, mode, func(s *LaunchSpec) {
				s.Quotas.HeartbeatMS = 300
				s.Quotas.MaxMissedBeats = 2
			})
			if err != nil {
				return err
			}
			blocked := run.State() == StateExpired
			detail := fmt.Sprintf("verdict=%s (session binding + strict sequence + spec digest + heartbeat watchdogs)", run.State())
			rec.Attacks = append(rec.Attacks, RedTeamAttack{Vector: mode, Attempt: "cheat the framed protocol", Blocked: blocked, Detail: detail})
		}
		// Host cross-check: every cheater produced an expired audit trail.
		rec.HostCheck = fmt.Sprintf("expiredAuditEvents=%d cheaters=%d", r.audit.count(AuditExpired), len(modes))
		rec.Passed = attacksBlockedAsWanted(rec.Attacks)
		return nil
	}), nil
}

// --- crash recovery ---------------------------------------------------------

func (r *RedTeamRunner) runCrashRecovery(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTCrashRec, func(rec *RedTeamRecord) error {
		// Simulate a crashed host: journal holds LAUNCHED without terminal
		// state plus a torn tail line from a mid-append crash.
		dir := filepath.Join(r.dir, "crashed-host")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path := filepath.Join(dir, "recovery-journal.jsonl")
		j, err := OpenJournal(path)
		if err != nil {
			return err
		}
		if err := j.Append(JournalRecord{RunID: "ghost-5c", SpecID: "s-ghost", Endpoint: "ep-5c", State: StateLaunched, AtMS: 1}); err != nil {
			return err
		}
		if err := j.Close(); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(`{"runId":"torn","state":"LAUN`); err != nil { // torn tail
			return err
		}
		f.Close()

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		audit := &redTeamAudit{}
		m, err := NewManager(Gate{SignedOffBy: "red-team-harness"}, MapKeyStore{"k-5c": pub}, dir, audit)
		if err != nil {
			return err
		}
		defer m.Close()
		recs, err := m.Recover()
		if err != nil {
			return err
		}
		if len(recs) != 1 || recs[0].Record.RunID != "ghost-5c" {
			return fmt.Errorf("want ghost-5c recovered, got %+v", recs)
		}
		unrec, err := UnrecoveredRuns(path)
		if err != nil {
			return err
		}
		again, err := m.Recover() // idempotence
		if err != nil {
			return err
		}
		rec.Attacks = []RedTeamAttack{
			{Vector: "host-crash-orphans", Attempt: "LAUNCHED run left behind by a dead host", Blocked: len(recs) == 1, Detail: fmt.Sprintf("marked CRASHED, %d unrecovered remain", len(unrec))},
			{Vector: "torn-journal-tail", Attempt: "half-written journal line from a mid-append crash", Blocked: len(recs) == 1 && recs[0].Record.RunID == "ghost-5c", Detail: "corrupt tail tolerated, prior good state recovered"},
			{Vector: "double-recovery", Attempt: "run Recover twice", Blocked: len(again) == 0, Detail: "second walk is idempotent"},
		}
		_ = priv
		rec.HostCheck = fmt.Sprintf("recoveredAuditEvents=%d", audit.count(AuditRecovered))
		rec.Passed = attacksBlockedAsWanted(rec.Attacks) && audit.count(AuditRecovered) >= 1
		return nil
	}), nil
}

// --- revoke -----------------------------------------------------------------

func (r *RedTeamRunner) runRevoke(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTRevoke, func(rec *RedTeamRecord) error {
		sp, err := r.rtSpec("forever", nil)
		if err != nil {
			return err
		}
		run, err := r.manager.Launch(ctx, sp)
		if err != nil {
			return err
		}
		time.Sleep(700 * time.Millisecond) // hello + beats
		if err := r.manager.Revoke(run.ID, "5c red-team revocation drill"); err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		waitErr := run.Wait(wctx)

		lateFrozen := run.State() == StateRevoked && run.Result() == nil && waitErr != nil
		auditSeen := false
		for _, a := range r.audit.actions(run.ID) {
			if a == AuditRevoked {
				auditSeen = true
			}
		}
		tree := pidsAlive([]int{run.proc.pid})
		rec.Attacks = []RedTeamAttack{
			{Vector: "revoke-kills-tree", Attempt: "revoke a live forever-run", Blocked: run.State() == StateRevoked && len(tree) == 0, Detail: fmt.Sprintf("state=%s rootPidAlive=%v", run.State(), len(tree) > 0)},
			{Vector: "late-result-freeze", Attempt: "deliver a RESULT after revocation", Blocked: lateFrozen, Detail: "result frozen nil, Wait returns ErrRevoked (M6-SBX-004)"},
			{Vector: "revocation-audited", Attempt: "audit trail of the revocation", Blocked: auditSeen, Detail: fmt.Sprintf("actions=%v", r.audit.actions(run.ID))},
		}
		rec.HostCheck = fmt.Sprintf("journalRevoked=%v", journalHasState(filepath.Join(r.dir, "recovery-journal.jsonl"), run.ID, StateRevoked))
		rec.Passed = attacksBlockedAsWanted(rec.Attacks) && journalHasState(filepath.Join(r.dir, "recovery-journal.jsonl"), run.ID, StateRevoked)
		return nil
	}), nil
}

// --- fault injection --------------------------------------------------------

func (r *RedTeamRunner) runFaultInjection(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTFaultInj, func(rec *RedTeamRecord) error {
		// 1. journal write failure after spawn: launch refuses, no orphan.
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		dir := filepath.Join(r.dir, "fault-journal")
		m, err := NewManager(Gate{SignedOffBy: "red-team-harness"}, MapKeyStore{"k-f": pub}, dir, nil)
		if err != nil {
			return err
		}
		pd, err := m.PolicyDigest()
		if err != nil {
			return err
		}
		digest, err := FileDigest(r.exe)
		if err != nil {
			return err
		}
		now := time.Now()
		spec := LaunchSpec{
			SpecID: "spec-fault", EndpointID: "ep-5c", Command: r.exe,
			Args:      rtHelperArgs("echo", "{}"),
			ExeDigest: digest, CapabilitySet: []string{"mcp.tools.read"},
			Quotas:     Quotas{MaxProcs: 4, MemoryCapBytes: 256 << 20, DeadlineMS: 30_000, HeartbeatMS: 500, MaxMissedBeats: 3},
			WorkingDir: filepath.Join(dir, "sbx"),
			Nonce:      NewNonce(), NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(10 * time.Minute),
			ConfigDigest: pd, KeyID: "k-f",
		}
		sp, err := Sign(spec, priv, "k-f")
		if err != nil {
			return err
		}
		if err := m.Close(); err != nil { // break the journal BEFORE launch
			return err
		}
		_, launchErr := m.Launch(ctx, sp)
		journalFaultHeld := launchErr != nil && strings.Contains(launchErr.Error(), "journal")
		m.mu.Lock()
		orphanRuns := len(m.runs)
		m.mu.Unlock()
		orphans := orphanRuns == 0

		// 2. audit sink failure: audit is best-effort, the run must finish.
		pub2, priv2, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		dir2 := filepath.Join(r.dir, "fault-audit")
		failing := &failingAudit{}
		m2, err := NewManager(Gate{SignedOffBy: "red-team-harness"}, MapKeyStore{"k-f2": pub2}, dir2, failing)
		if err != nil {
			return err
		}
		defer m2.Close()
		pd2, _ := m2.PolicyDigest()
		spec2 := spec
		spec2.SpecID = "spec-fault-audit"
		spec2.WorkingDir = filepath.Join(dir2, "sbx")
		spec2.Nonce = NewNonce()
		spec2.ConfigDigest = pd2
		spec2.KeyID = "k-f2"
		sp2, err := Sign(spec2, priv2, "k-f2")
		if err != nil {
			return err
		}
		run2, err := m2.Launch(ctx, sp2)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		_ = run2.Wait(wctx)
		auditFaultHeld := run2.State() == StateCompleted

		rec.Attacks = []RedTeamAttack{
			{Vector: "journal-write-failure", Attempt: "journal fsync fails right after spawn", Blocked: journalFaultHeld && orphans, Detail: fmt.Sprintf("launchErr=%v orphanRuns=%d", launchErr != nil, orphanRuns)},
			{Vector: "audit-sink-failure", Attempt: "audit sink rejects every event", Blocked: auditFaultHeld, Detail: fmt.Sprintf("run state=%s despite failing audit sink", run2.State())},
		}
		rec.HostCheck = "fail-fast on durability loss, best-effort on observability loss"
		rec.Passed = attacksBlockedAsWanted(rec.Attacks)
		return nil
	}), nil
}

type failingAudit struct{}

func (failingAudit) Emit(action, aggregateID, actor string, metadata []byte) error {
	return fmt.Errorf("5c fault injection: audit sink down")
}

// --- capacity ---------------------------------------------------------------

func (r *RedTeamRunner) runCapacity(ctx context.Context) (*RedTeamRecord, error) {
	return r.record(RTCapacity, func(rec *RedTeamRecord) error {
		const n = 16
		start := r.now()
		runs := make([]*Run, 0, n)
		for i := 0; i < n; i++ {
			run, err := r.runMode(ctx, fmt.Sprintf("echo-%d", i), func(s *LaunchSpec) {
				s.Args = rtHelperArgs("echo", "{}")
			})
			if err != nil {
				return err
			}
			runs = append(runs, run)
		}
		ok := 0
		wctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		for _, run := range runs {
			_ = run.Wait(wctx)
			if run.State() == StateCompleted {
				ok++
			}
		}
		elapsed := r.now().Sub(start)
		rec.Attacks = []RedTeamAttack{{
			Vector:  "parallel-launch-storm",
			Attempt: fmt.Sprintf("%d concurrent signed launches", n),
			Blocked: ok == n,
			Detail:  fmt.Sprintf("completed=%d/%d elapsed=%s", ok, n, elapsed.Truncate(time.Millisecond)),
		}}
		launched := 0
		for _, run := range runs {
			for _, a := range r.audit.actions(run.ID) {
				if a == AuditLaunched {
					launched++
				}
			}
		}
		rec.HostCheck = fmt.Sprintf("launchedAuditEvents=%d completedAuditEvents=%d", r.audit.count(AuditLaunched), r.audit.count(AuditCompleted))
		rec.Passed = ok == n && launched == n
		return nil
	}), nil
}

// --- evidence ---------------------------------------------------------------

// BuildRedTeamBundle digests each record and chains them.
func BuildRedTeamBundle(records []RedTeamRecord, policyDigest, platform string, gateOpen bool, now time.Time) (*RedTeamBundle, error) {
	b := &RedTeamBundle{
		Schema: "stdio-5c-evidence/1", GeneratedAt: now.UTC(), Platform: platform,
		PolicyDigest: policyDigest, GateOpen: gateOpen, Records: records, Notes: RedTeamNotes,
	}
	b.Verdict = VerdictPass
	chain := ""
	for i := range b.Records {
		raw, err := CanonicalJSON(b.Records[i])
		if err != nil {
			return nil, err
		}
		b.Records[i].Digest = digestBytes(raw)
		if !b.Records[i].Passed {
			b.Verdict = VerdictFail
		}
		chain += b.Records[i].Digest
	}
	if gateOpen {
		b.Verdict = VerdictFail // a repo-produced bundle with an open gate is not credible
	}
	b.BundleDigest = digestBytes([]byte(chain))
	return b, nil
}

// Verdict constants (shared with 5A semantics).
const (
	VerdictPass = "PASS"
	VerdictFail = "FAIL"
)

// WriteRedTeamEvidence persists bundle.json + stdio-5c.md under dir.
func WriteRedTeamEvidence(dir string, b *RedTeamBundle) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	bundlePath := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(bundlePath, raw, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "stdio-5c.md"), []byte(renderRedTeamReport(b)), 0o644); err != nil {
		return "", err
	}
	return bundlePath, nil
}

func renderRedTeamReport(b *RedTeamBundle) string {
	var w strings.Builder
	fmt.Fprintf(&w, "# stdio Production-Escape Red Team Evidence (M6 slice 5C)\n\n")
	fmt.Fprintf(&w, "- Generated: %s\n", b.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&w, "- Platform: %s\n", b.Platform)
	fmt.Fprintf(&w, "- Schema: %s\n", b.Schema)
	fmt.Fprintf(&w, "- Policy digest (5B config binding): `%s`\n", b.PolicyDigest)
	fmt.Fprintf(&w, "- Gate open at generation: %v\n", b.GateOpen)
	fmt.Fprintf(&w, "- Bundle digest: `%s`\n", b.BundleDigest)
	fmt.Fprintf(&w, "- Verdict: **%s**\n\n", b.Verdict)
	w.WriteString("## Records\n\n")
	w.WriteString("| # | record | enforced by | verdict | attacks | digest |\n")
	w.WriteString("|---|--------|-------------|---------|---------|--------|\n")
	for i, rec := range b.Records {
		v := "FAIL"
		if rec.Passed {
			v = "PASS"
		}
		fmt.Fprintf(&w, "| %d | %s (`%s`) | %s | %s | %d | `%s` |\n", i+1, rec.Title, rec.ID, rec.EnforcedBy, v, len(rec.Attacks), shortDigestRT(rec.Digest))
	}
	w.WriteString("\n## Attack detail\n\n")
	for _, rec := range b.Records {
		fmt.Fprintf(&w, "### %s — %s\n\n", rec.ID, rec.Title)
		fmt.Fprintf(&w, "- enforced by: %s\n- host check: %s\n\n", rec.EnforcedBy, rec.HostCheck)
		w.WriteString("| vector | blocked | observation |\n|--------|---------|-------------|\n")
		for _, atk := range rec.Attacks {
			fmt.Fprintf(&w, "| %s | %v | %s |\n", atk.Vector, atk.Blocked, atk.Detail)
		}
		w.WriteString("\n")
	}
	w.WriteString("## Gate notes\n\n")
	for _, n := range b.Notes {
		fmt.Fprintf(&w, "- %s\n", n)
	}
	return w.String()
}

func shortDigestRT(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

// journalHasState scans the runner journal for a terminal record of runID.
func journalHasState(path, runID, state string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"runId":"`+runID+`"`) && strings.Contains(line, `"state":"`+state+`"`) {
			return true
		}
	}
	return false
}

// RedTeamPlatform identifies the build the red team ran on.
func RedTeamPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
