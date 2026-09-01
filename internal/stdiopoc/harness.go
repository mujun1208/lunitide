package stdiopoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/worker"
)

// Harness drives the whole POC: it prepares the sandbox layout, spawns each
// probe child under the OS-enforced engine, collects the attacker-side
// reports, then cross-validates every claim host-side. A probe saying
// "blocked" is worthless without the host independently confirming the
// precondition and the enforcement verdict.
type Harness struct {
	// Exe is the probe child executable (the test binary in helper mode).
	Exe string
	// HelperArgs builds the argv that runs RunProbe for one probe id.
	HelperArgs func(probe string, cfg ProbeConfig) []string
	// Base is the directory the sandbox layout is created under.
	Base string

	now func() time.Time
}

// POC spawn quotas: tight enough to prove enforcement, loose enough for a
// Go test binary runtime.
const (
	pocMaxProcs  = 4
	pocMemoryCap = 192 << 20 // 192MiB job commit cap, one child
	// proctree is the one probe that deliberately fills the job up to the
	// active-process quota, so the cap has to cover pocMaxProcs copies of
	// the child rather than one.
	pocProcTreeMemoryCap = 1 << 30   // 1GiB
	pocResourceRequest   = 512 << 20 // probe asks for 512MiB
	pocSpawnTimeout      = 60 * time.Second
	pocForkCount         = 16
)

// jobMemoryCap answers the commit cap for one probe's job object.
//
// Sizing every probe for a single child was wrong for proctree: the kernel
// killed the whole job on the memory quota before the process quota that
// probe exists to prove was ever reached, and the harness read EOF where the
// report frame should have been. It only showed up under -race, where the
// child is the race-instrumented test binary and several copies of it no
// longer fit in a cap that one copy does.
func jobMemoryCap(probe string) uint64 {
	if probe == AssumptionProcTree {
		return pocProcTreeMemoryCap
	}
	return pocMemoryCap
}

func NewHarness(exe string, helper func(probe string, cfg ProbeConfig) []string, base string) *Harness {
	return &Harness{Exe: exe, HelperArgs: helper, Base: base, now: time.Now}
}

// layout is the on-disk sandbox scenario shared by the file/network probes.
type layout struct {
	root          string // sandbox root (child cwd)
	marker        string // host-side secret file OUTSIDE the root
	symlink       string // file symlink inside root -> marker (if permitted)
	junctionFile  string // file reached through a junction inside root -> host dir
	hostSecretDir string // host-side directory the junction points at
	legit         string // legitimate file inside the root (positive control)
	mountSrc      string // whitelisted mount source (positive control for mounts)
}

func (h *Harness) prepare() (*layout, error) {
	root := filepath.Join(h.Base, "sandbox")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		return nil, err
	}
	l := &layout{
		root:          root,
		marker:        filepath.Join(h.Base, "host-marker-secret.txt"),
		symlink:       filepath.Join(root, "escape-link.txt"),
		junctionFile:  filepath.Join(root, "escape-dir", "secret.txt"),
		hostSecretDir: filepath.Join(h.Base, "host-secret-dir"),
		legit:         filepath.Join(root, "sub", "legit.txt"),
		mountSrc:      filepath.Join(h.Base, "mount-src"),
	}
	secret := "HOST-SECRET-MARKER-DO-NOT-LEAK"
	if err := os.WriteFile(l.marker, []byte(secret), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(l.legit, []byte("legit"), 0o644); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(l.hostSecretDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(l.hostSecretDir, "secret.txt"), []byte(secret), 0o644); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(l.mountSrc, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(l.mountSrc, "shared.txt"), []byte("shared"), 0o644); err != nil {
		return nil, err
	}
	// Directory junction inside the root pointing at the host secret dir:
	// the classic junction escape (creatable without any privilege).
	if err := makeJunction(filepath.Join(root, "escape-dir"), l.hostSecretDir); err != nil {
		l.junctionFile = ""
	}
	// File symlink needs developer mode or privilege: optional vector.
	_ = os.Remove(l.symlink)
	if err := os.Symlink(l.marker, l.symlink); err != nil {
		l.symlink = ""
	}
	return l, nil
}

// Run executes all six probes and returns the assumptions with verdicts.
func (h *Harness) Run(ctx context.Context) ([]Assumption, error) {
	if runtime.GOOS != "windows" {
		return nil, ErrUnsupportedPlatform
	}
	l, err := h.prepare()
	if err != nil {
		return nil, fmt.Errorf("stdiopoc: prepare sandbox: %w", err)
	}
	out := make([]Assumption, 0, len(assumptionOrder))
	for _, id := range assumptionOrder {
		var a *Assumption
		var err error
		switch id {
		case AssumptionHostFile:
			a, err = h.runHostFile(ctx, l)
		case AssumptionNetwork:
			a, err = h.runNetwork(ctx, l)
		case AssumptionSecret:
			a, err = h.runSecret(ctx)
		case AssumptionProcTree:
			a, err = h.runProcTree(ctx)
		case AssumptionResource:
			a, err = h.runResource(ctx)
		case AssumptionProtocol:
			a, err = h.runProtocol(ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("stdiopoc: probe %s: %w", id, err)
		}
		out = append(out, *a)
	}
	return out, nil
}

// newAssumption stamps the metadata.
func (h *Harness) newAssumption(id string) *Assumption {
	meta, _ := assumptionByID(id)
	return &Assumption{ID: id, Title: meta.title, EnforcedBy: meta.enforcedBy, StartedAt: h.now().UTC()}
}

// spawnProbe launches one probe child and returns the proc after the ready
// frame validated.
func (h *Harness) spawnProbe(ctx context.Context, probe string, cfg ProbeConfig) (*Proc, error) {
	cfg.Probe = probe
	spec := SpawnSpec{
		Exe:            h.Exe,
		Args:           h.HelperArgs(probe, cfg),
		Dir:            cfg.Root,
		Env:            MinimalEnv(os.Environ(), []string{"SystemRoot", "LUNITIDE_STDIO_POC_PROBE"}, map[string]string{"LUNITIDE_STDIO_POC_PROBE": "1"}),
		MaxProcs:       pocMaxProcs,
		MemoryCapBytes: jobMemoryCap(probe),
		Timeout:        pocSpawnTimeout,
	}
	p, err := spawn(ctx, spec)
	if err != nil {
		return nil, err
	}
	// First frame must be a valid ready envelope for this probe.
	payload, err := p.Stdout.Read()
	if err != nil {
		p.Kill()
		return nil, fmt.Errorf("ready frame: %w", err)
	}
	env, err := ParseEnvelope(payload, probe)
	if err != nil || env.Type != EnvelopeTypeReady {
		p.Kill()
		return nil, fmt.Errorf("ready frame invalid: err=%v env=%v", err, env)
	}
	return p, nil
}

// readReport drains frames until the report envelope arrives.
func (h *Harness) readReport(p *Proc, probe string) (*probeReport, error) {
	for {
		payload, err := p.Stdout.Read()
		if err != nil {
			return nil, fmt.Errorf("report frame: %w", err)
		}
		env, err := ParseEnvelope(payload, probe)
		if err != nil {
			return nil, fmt.Errorf("unexpected bad frame: %w", err)
		}
		switch env.Type {
		case EnvelopeTypeReport:
			var rep probeReport
			if err := json.Unmarshal(env.Data, &rep); err != nil {
				return nil, fmt.Errorf("report payload: %w", err)
			}
			return &rep, nil
		case EnvelopeTypeReady, EnvelopeTypeAttack:
			continue
		default:
			return nil, fmt.Errorf("unexpected envelope type %q", env.Type)
		}
	}
}

// finish waits for a clean exit (probe exits on its own after reporting).
func (h *Harness) finish(a *Assumption, p *Proc, blockedWanted bool) {
	_ = p.Stdin.Close()
	code, err := p.Wait(context.Background())
	a.EndedAt = h.now().UTC()
	clean := err == nil && code == 0
	a.Passed = a.Passed && clean && blockedWanted
	if !clean {
		a.Summary += fmt.Sprintf(" (child exit=%d err=%v)", code, err)
	}
}

// --- host-file --------------------------------------------------------------

func (h *Harness) runHostFile(ctx context.Context, l *layout) (*Assumption, error) {
	a := h.newAssumption(AssumptionHostFile)
	profile := worker.Profile{
		WorkerID: "poc-host-file",
		Root:     l.root,
		Mounts:   []worker.Mount{{Source: l.mountSrc, Target: "mnt", ReadOnly: true}},
	}
	raw, _ := json.Marshal(profile)
	cfg := ProbeConfig{
		Root:         l.root,
		GuardProfile: raw,
		HostMarker:   l.marker,
		SymlinkPath:  l.symlink,
		JunctionFile: l.junctionFile,
		InRootFile:   l.legit,
	}
	p, err := h.spawnProbe(ctx, AssumptionHostFile, cfg)
	if err != nil {
		return nil, err
	}
	rep, err := h.readReport(p, AssumptionHostFile)
	if err != nil {
		p.Kill()
		return nil, err
	}
	a.Attacks = rep.Attacks
	a.Passed = allBlockedAsWanted(rep.Attacks)

	// Host cross-check 1: the marker really exists and the host CAN read it
	// (otherwise "blocked" would be a false positive).
	data, rerr := os.ReadFile(l.marker)
	precondOK := rerr == nil && strings.Contains(string(data), "HOST-SECRET-MARKER")
	// Host cross-check 2: an independent host-side guard rejects the very
	// same escape vectors (junction file when present, symlink when present).
	hostGuard := worker.NewGuard(profile)
	hostRejects := hostGuard.CheckPath(l.marker) != nil
	if l.junctionFile != "" && hostGuard.CheckPath(l.junctionFile) == nil {
		hostRejects = false
	}
	if l.symlink != "" && hostGuard.CheckPath(l.symlink) == nil {
		hostRejects = false
	}
	// ...and allows the positive controls (guard not brain-dead).
	hostAllows := hostGuard.CheckPath(l.legit) == nil
	a.HostCheck = HostCheck{
		Precondition: "host reads marker file and mounts are whitelisted",
		Confirmed:    precondOK && hostRejects && hostAllows,
		Detail: fmt.Sprintf("markerReadable=%v hostGuardRejectsEscapes=%v hostGuardAllowsLegit=%v junction=%q symlink=%q",
			precondOK, hostRejects, hostAllows, l.junctionFile, l.symlink),
	}
	a.Passed = a.Passed && a.HostCheck.Confirmed
	h.finish(a, p, true)
	return a, nil
}

// --- network ----------------------------------------------------------------

func (h *Harness) runNetwork(ctx context.Context, l *layout) (*Assumption, error) {
	a := h.newAssumption(AssumptionNetwork)
	profile := worker.Profile{
		WorkerID: "poc-network",
		Root:     l.root,
		NetAllowlist: []worker.NetTarget{
			{Host: "api.example.com", Port: "443"},
			{Host: "allowlisted.example.com", Port: "443"},
		},
	}
	raw, _ := json.Marshal(profile)
	cfg := ProbeConfig{
		Root:         l.root,
		GuardProfile: raw,
		DialTargets: []DialTarget{
			{Host: "169.254.169.254", Port: "80", Vector: "imds-aws", WantBlocked: true},
			{Host: "metadata.google.internal", Port: "80", Vector: "imds-gcp", WantBlocked: true},
			{Host: "10.0.0.1", Port: "3389", Vector: "intranet-rdp", WantBlocked: true},
			{Host: "127.0.0.1", Port: "8080", Vector: "host-loopback", WantBlocked: true},
			{Host: "evil.example.com", Port: "443", Vector: "not-allowlisted", WantBlocked: true},
			// DNS rebinding: the allowlisted name resolves to a private IP;
			// the host dials by resolved address, which must be denied.
			{Host: "10.0.0.99", Port: "443", Vector: "dns-rebinding-resolved-ip", WantBlocked: true},
			{Host: "api.example.com", Port: "443", Vector: "allowlisted-positive-control", WantBlocked: false},
		},
	}
	p, err := h.spawnProbe(ctx, AssumptionNetwork, cfg)
	if err != nil {
		return nil, err
	}
	rep, err := h.readReport(p, AssumptionNetwork)
	if err != nil {
		p.Kill()
		return nil, err
	}
	a.Attacks = rep.Attacks
	a.Passed = allBlockedAsWanted(rep.Attacks)

	hostGuard := worker.NewGuard(profile)
	allReject := true
	for _, t := range cfg.DialTargets {
		blocked := hostGuard.CheckDial(t.Host, t.Port) != nil
		if blocked != t.WantBlocked {
			allReject = false
		}
	}
	a.HostCheck = HostCheck{
		Precondition: "independent host guard classifies all seven targets identically",
		Confirmed:    allReject,
		Detail:       fmt.Sprintf("hostGuardAgrees=%v targets=%d", allReject, len(cfg.DialTargets)),
	}
	a.Passed = a.Passed && a.HostCheck.Confirmed
	h.finish(a, p, true)
	return a, nil
}

// --- secret -----------------------------------------------------------------

// secretMarkerVar is set ONLY in the host environment before spawning: the
// explicit child env block must not carry it.
const secretMarkerVar = "LUNITIDE_POC_SECRET_HOST"
const secretMarkerVal = "poc-host-secret-7f3a"

func (h *Harness) runSecret(ctx context.Context) (*Assumption, error) {
	a := h.newAssumption(AssumptionSecret)
	prev, hadPrev := os.LookupEnv(secretMarkerVar)
	if err := os.Setenv(secretMarkerVar, secretMarkerVal); err != nil {
		return nil, err
	}
	defer func() {
		if hadPrev {
			_ = os.Setenv(secretMarkerVar, prev)
		} else {
			_ = os.Unsetenv(secretMarkerVar)
		}
	}()
	root := filepath.Join(h.Base, "sandbox")
	cfg := ProbeConfig{
		Root: root,
		SecretVars: []string{
			secretMarkerVar,
			"AWS_SECRET_ACCESS_KEY",
			"OPENAI_API_KEY",
			"ANTHROPIC_API_KEY",
			"USERPROFILE", // host identity leakage
		},
	}
	p, err := h.spawnProbe(ctx, AssumptionSecret, cfg)
	if err != nil {
		return nil, err
	}
	rep, err := h.readReport(p, AssumptionSecret)
	if err != nil {
		p.Kill()
		return nil, err
	}
	a.Attacks = rep.Attacks
	a.Passed = allBlockedAsWanted(rep.Attacks)

	// Host cross-check: the variable IS set in the parent right now, so an
	// empty child read proves stripping, not absence.
	hostVal := os.Getenv(secretMarkerVar)
	a.HostCheck = HostCheck{
		Precondition: "host environment carries the marker secret",
		Confirmed:    hostVal == secretMarkerVal,
		Detail:       fmt.Sprintf("hostGetenv=%q", hostVal),
	}
	a.Passed = a.Passed && a.HostCheck.Confirmed
	h.finish(a, p, true)
	return a, nil
}

// --- proctree ---------------------------------------------------------------

func (h *Harness) runProcTree(ctx context.Context) (*Assumption, error) {
	a := h.newAssumption(AssumptionProcTree)
	root := filepath.Join(h.Base, "sandbox")
	// Route the sleeper argv through HelperArgs: the "sleep" pseudo-probe
	// keeps the harness agnostic of whether the child is the test binary
	// (helper mode) or the cmd/stdio-poc CLI.
	sleepArgs := append([]string{h.Exe}, h.HelperArgs("sleep", ProbeConfig{})...)
	cfg := ProbeConfig{Root: root, SpawnCount: pocForkCount, SpawnArgs: sleepArgs}
	p, err := h.spawnProbe(ctx, AssumptionProcTree, cfg)
	if err != nil {
		return nil, err
	}
	rep, err := h.readReport(p, AssumptionProcTree)
	if err != nil {
		p.Kill()
		return nil, err
	}
	a.Attacks = rep.Attacks
	quotaHeld := len(rep.Attacks) == 1 && rep.Attacks[0].Blocked
	a.Passed = quotaHeld

	// Reap the tree and verify every reported grandchild pid is gone.
	p.Kill()
	code, werr := p.Wait(context.Background())
	a.EndedAt = h.now().UTC()
	grandPids := parsePIDList(rep.Detail)
	allDead := true
	for _, pid := range grandPids {
		if pidAlive(pid) {
			allDead = false
		}
	}
	a.HostCheck = HostCheck{
		Precondition: "Kill reaps the tree; every grandchild pid is dead",
		Confirmed:    werr == nil && allDead,
		Detail:       fmt.Sprintf("rootExit=%d waitErr=%v grandchilds=%d allDead=%v pids=%v", code, werr, len(grandPids), allDead, grandPids),
	}
	a.Passed = a.Passed && a.HostCheck.Confirmed
	if n := countSpawned(rep.Attacks); n >= pocMaxProcs {
		a.Passed = false
		a.Summary += fmt.Sprintf(" (spawned %d >= quota %d: quota NOT enforced)", n, pocMaxProcs)
	}
	return a, nil
}

// --- resource ---------------------------------------------------------------

func (h *Harness) runResource(ctx context.Context) (*Assumption, error) {
	a := h.newAssumption(AssumptionResource)
	root := filepath.Join(h.Base, "sandbox")
	cfg := ProbeConfig{Root: root, MemRequestBytes: pocResourceRequest}
	p, err := h.spawnProbe(ctx, AssumptionResource, cfg)
	if err != nil {
		return nil, err
	}
	rep, err := h.readReport(p, AssumptionResource)
	if err != nil {
		p.Kill()
		return nil, err
	}
	a.Attacks = rep.Attacks
	a.Passed = allBlockedAsWanted(rep.Attacks)
	// The probe child exiting cleanly after a failed VirtualAlloc is itself
	// evidence the quota stopped the allocation rather than an OOM kill.
	a.HostCheck = HostCheck{
		Precondition: "child reports VirtualAlloc rejection below the request",
		Confirmed:    a.Passed,
		Detail:       firstAttackDetail(rep.Attacks),
	}
	h.finish(a, p, true)
	return a, nil
}

// --- protocol ---------------------------------------------------------------

func (h *Harness) runProtocol(ctx context.Context) (*Assumption, error) {
	a := h.newAssumption(AssumptionProtocol)
	root := filepath.Join(h.Base, "sandbox")
	cfg := ProbeConfig{Root: root}
	p, err := h.spawnProbe(ctx, AssumptionProtocol, cfg)
	if err != nil {
		return nil, err
	}
	defer p.Kill()
	// Follow the script: announce frame -> next read must fail with the
	// classified error.
	for _, atk := range ProtocolAttacks {
		payload, err := p.Stdout.Read()
		if err != nil {
			return nil, fmt.Errorf("announce %s: %w", atk.ID, err)
		}
		env, err := ParseEnvelope(payload, AssumptionProtocol)
		if err != nil || env.Type != EnvelopeTypeAttack {
			return nil, fmt.Errorf("announce %s invalid: %v", atk.ID, err)
		}
		var got protocolAttack
		_ = json.Unmarshal(env.Data, &got)
		if got.ID != atk.ID {
			return nil, fmt.Errorf("announce order: got %q want %q", got.ID, atk.ID)
		}
		badPayload, err := p.Stdout.Read()
		class := ""
		switch err {
		case nil:
			if _, perr := ParseEnvelope(badPayload, AssumptionProtocol); perr != nil {
				class = classifyFrameErr(perr)
			} else {
				class = "accepted" // validator took the bait: FAIL
			}
		default:
			class = classifyFrameErr(err)
		}
		ok := class == atk.Expect
		a.Attacks = append(a.Attacks, Attack{
			Vector:  atk.ID,
			Attempt: "bad frame classified as " + atk.Expect,
			Blocked: ok,
			Detail:  fmt.Sprintf("classified=%s", class),
		})
	}
	a.Passed = allBlockedAsWanted(a.Attacks)
	// The trailing report frame must still parse: the stream stayed aligned
	// through all the abuse.
	rep, err := h.readReport(p, AssumptionProtocol)
	aligned := err == nil && rep != nil
	a.HostCheck = HostCheck{
		Precondition: "stream stays aligned; trailing report frame parses",
		Confirmed:    aligned,
		Detail:       fmt.Sprintf("trailingReportErr=%v", err),
	}
	a.Passed = a.Passed && a.HostCheck.Confirmed
	h.finish(a, p, true)
	return a, nil
}

// classifyFrameErr maps an error to the assumption classification.
func classifyFrameErr(err error) string {
	switch {
	case errors.Is(err, ErrOversizeFrame):
		return "oversize"
	case errors.Is(err, ErrForgedFrame):
		return "forged"
	case errors.Is(err, ErrMalformedFrame):
		return "malformed"
	default:
		return "unexpected:" + err.Error()
	}
}

// --- small helpers ----------------------------------------------------------

func allBlockedAsWanted(as []Attack) bool {
	for _, a := range as {
		if !a.Blocked {
			return false
		}
	}
	return len(as) > 0
}

func firstAttackDetail(as []Attack) string {
	if len(as) == 0 {
		return ""
	}
	return as[0].Detail
}

func countSpawned(as []Attack) int {
	if len(as) == 0 {
		return 0
	}
	var n int
	// A malformed detail simply leaves the count at zero.
	_, _ = fmt.Sscanf(as[0].Detail, "spawned=%d", &n)
	return n
}

func parsePIDList(detail string) []int {
	rest, ok := strings.CutPrefix(detail, "pids:")
	if !ok {
		return nil
	}
	rest = strings.TrimSpace(rest)
	rest = strings.Trim(rest, "[]")
	if rest == "" {
		return nil
	}
	var pids []int
	for _, part := range strings.Fields(rest) {
		var pid int
		if _, err := fmt.Sscanf(part, "%d", &pid); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}
