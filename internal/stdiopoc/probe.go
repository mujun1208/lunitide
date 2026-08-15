package stdiopoc

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/lunitide/lunitide/internal/worker"
)

// ProbeConfig is the JSON blob the harness passes to the probe child via
// argv. The probe is attacker-role: it tries every escape it can and
// honestly reports what happened.
type ProbeConfig struct {
	Probe string `json:"probe"`
	Root  string `json:"root"` // sandbox root == child cwd

	// host-file
	GuardProfile json.RawMessage `json:"guardProfile,omitempty"` // worker.Profile JSON
	HostMarker   string          `json:"hostMarker,omitempty"`   // host-side marker (outside root)
	SymlinkPath  string          `json:"symlinkPath,omitempty"`  // file symlink -> hostMarker (optional)
	JunctionFile string          `json:"junctionFile,omitempty"` // file through junction -> host dir
	InRootFile   string          `json:"inRootFile,omitempty"`   // legitimate in-root file (positive control)

	// network
	DialTargets []DialTarget `json:"dialTargets,omitempty"` // {host, port, wantBlocked}

	// secret
	SecretVars []string `json:"secretVars,omitempty"`

	// proctree
	SpawnCount int      `json:"spawnCount,omitempty"`
	SpawnArgs  []string `json:"spawnArgs,omitempty"`

	// resource
	MemRequestBytes uint64 `json:"memRequestBytes,omitempty"`
}

// DialTarget is one dial attempt the network probe makes through the guard.
type DialTarget struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	Vector     string `json:"vector"`
	WantBlocked bool  `json:"wantBlocked"`
}

// probeReport is the final data payload of the report frame.
type probeReport struct {
	Attacks []Attack `json:"attacks"`
	Detail  string   `json:"detail,omitempty"`
}

func nonceHex() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return NewNonce(b)
}

func writeEnvelope(w io.Writer, typ, probe string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	env := FrameEnvelope{Type: typ, Nonce: nonceHex(), Probe: probe, Data: raw}
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return WriteFrame(w, payload)
}

// RunProbe executes one probe's attack set inside the spawned child and
// streams framed envelopes to w (raw stdout). It never calls os.Exit: the
// helper entrypoint decides the exit code.
func RunProbe(cfg ProbeConfig, w io.Writer) error {
	if err := writeEnvelope(w, EnvelopeTypeReady, cfg.Probe, map[string]string{"root": cfg.Root}); err != nil {
		return err
	}
	var report probeReport
	var err error
	switch cfg.Probe {
	case AssumptionHostFile:
		report, err = probeHostFile(cfg)
	case AssumptionNetwork:
		report, err = probeNetwork(cfg)
	case AssumptionSecret:
		report, err = probeSecret(cfg)
	case AssumptionProcTree:
		report, err = probeProcTree(cfg)
		if err == nil {
			// report first, then hold the tree for the host to reap
			if ferr := writeEnvelope(w, EnvelopeTypeReport, cfg.Probe, report); ferr != nil {
				return ferr
			}
			time.Sleep(10 * time.Minute)
			return nil
		}
	case AssumptionResource:
		report, err = probeResource(cfg)
	case AssumptionProtocol:
		return probeProtocol(cfg, w)
	default:
		return fmt.Errorf("stdiopoc: unknown probe %q", cfg.Probe)
	}
	if err != nil {
		return err
	}
	return writeEnvelope(w, EnvelopeTypeReport, cfg.Probe, report)
}

// --- host-file ------------------------------------------------------------

func probeHostFile(cfg ProbeConfig) (probeReport, error) {
	var profile worker.Profile
	if err := json.Unmarshal(cfg.GuardProfile, &profile); err != nil {
		return probeReport{}, fmt.Errorf("guard profile: %w", err)
	}
	g := worker.NewGuard(profile)
	report := probeReport{}
	tryPath := func(vector, p string, wantBlocked bool) {
		a := Attack{Vector: vector, Attempt: p}
		err := g.CheckPath(p)
		if _, isEscape := err.(*worker.EscapeError); isEscape {
			a.Blocked = true
			a.Detail = trimForDetail(err.Error())
		} else if err != nil {
			a.Detail = trimForDetail("guard error: " + err.Error())
		} else {
			// guard allowed: for the positive control that is expected; for
			// an escape vector that means the file layer did not block.
			if data, rerr := os.ReadFile(p); rerr == nil {
				a.Detail = trimForDetail(fmt.Sprintf("guard ALLOWED and read %d bytes", len(data)))
			} else {
				a.Detail = trimForDetail("guard allowed, direct read failed: " + rerr.Error())
			}
		}
		a.Blocked = a.Blocked == wantBlocked
		report.Attacks = append(report.Attacks, a)
	}
	tryPath("absolute-host-marker", cfg.HostMarker, true)
	tryPath("parent-traversal", "..\\"+baseName(cfg.HostMarker), true)
	tryPath("deep-traversal", "..\\..\\..\\..\\"+baseName(cfg.HostMarker), true)
	if cfg.SymlinkPath != "" {
		tryPath("symlink-escape", cfg.SymlinkPath, true)
	}
	if cfg.JunctionFile != "" {
		tryPath("junction-escape", cfg.JunctionFile, true)
	}
	tryPath("in-root-positive-control", cfg.InRootFile, false)
	return report, nil
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' || p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// --- network ----------------------------------------------------------------

func probeNetwork(cfg ProbeConfig) (probeReport, error) {
	var profile worker.Profile
	if err := json.Unmarshal(cfg.GuardProfile, &profile); err != nil {
		return probeReport{}, fmt.Errorf("guard profile: %w", err)
	}
	g := worker.NewGuard(profile)
	report := probeReport{}
	for _, t := range cfg.DialTargets {
		a := Attack{Vector: t.Vector, Attempt: t.Host + ":" + t.Port}
		err := g.CheckDial(t.Host, t.Port)
		if _, isEscape := err.(*worker.EscapeError); isEscape {
			a.Blocked = true
			a.Detail = trimForDetail(err.Error())
		} else if err != nil {
			a.Detail = trimForDetail("guard error: " + err.Error())
		} else {
			a.Detail = "guard ALLOWED dial"
		}
		a.Blocked = a.Blocked == t.WantBlocked
		report.Attacks = append(report.Attacks, a)
	}
	return report, nil
}

// --- secret -----------------------------------------------------------------

func probeSecret(cfg ProbeConfig) (probeReport, error) {
	report := probeReport{}
	for _, name := range cfg.SecretVars {
		a := Attack{Vector: "env-inheritance", Attempt: "getenv " + name}
		val, present := os.LookupEnv(name)
		switch {
		case !present:
			a.Blocked = true
			a.Detail = "variable absent from child environment"
		case val == "":
			a.Blocked = true
			a.Detail = "variable present but empty"
		default:
			a.Blocked = false
			a.Detail = fmt.Sprintf("LEAKED: %s=%s", name, trimForDetail(val))
		}
		report.Attacks = append(report.Attacks, a)
	}
	return report, nil
}

// --- proctree ---------------------------------------------------------------

func probeProcTree(cfg ProbeConfig) (probeReport, error) {
	report := probeReport{}
	a := Attack{Vector: "fork-bomb", Attempt: fmt.Sprintf("spawn %d grandchildren", cfg.SpawnCount)}
	pids := make([]int, 0, cfg.SpawnCount)
	failures := 0
	// Grandchildren must NOT inherit the frame pipe on stdout: any stray
	// byte they print corrupts the length-prefixed protocol the host
	// reads. Sink their output to the null device instead.
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return report, fmt.Errorf("proctree: open null device: %w", err)
	}
	defer devnull.Close()
	for i := 0; i < cfg.SpawnCount; i++ {
		cmd := exec.Command(cfg.SpawnArgs[0], cfg.SpawnArgs[1:]...)
		cmd.Env = MinimalEnv(os.Environ(), []string{"SystemRoot", "LUNITIDE_STDIO_POC_PROBE"}, nil)
		cmd.Stdout = devnull
		cmd.Stderr = devnull
		if err := cmd.Start(); err != nil {
			failures++
			continue
		}
		pids = append(pids, cmd.Process.Pid)
	}
	// Do not wait: the grandchildren sleep; the host kills the whole job.
	a.Blocked = failures > 0
	a.Detail = fmt.Sprintf("spawned=%d rejected=%d pids=%v (job active-process quota)", len(pids), failures, pids)
	report.Attacks = append(report.Attacks, a)
	report.Detail = fmt.Sprintf("pids:%v", pids)
	return report, nil
}

// --- resource ---------------------------------------------------------------

func probeResource(cfg ProbeConfig) (probeReport, error) {
	return probeResourceOS(cfg)
}

// --- protocol ---------------------------------------------------------------

// protocolAttack describes one protocol-cheat vector the probe emits and
// the error the host reader must classify it as.
type protocolAttack struct {
	ID     string `json:"id"`
	Expect string `json:"expect"` // "oversize" | "malformed" | "forged"
}

// ProtocolAttacks is the ordered script the protocol probe plays. The
// harness reads the same list (exported for the host-side cross check).
// Every variant is stream-aligned; a real >4MiB byte stream is covered by
// unit tests (TestReadFrameOversizeReal) where alignment does not matter.
var ProtocolAttacks = []protocolAttack{
	{ID: "zero-length-frame", Expect: "malformed"},
	{ID: "garbage-payload", Expect: "malformed"},
	{ID: "forged-type", Expect: "forged"},
	{ID: "forged-nonce", Expect: "forged"},
	{ID: "forged-probe", Expect: "forged"},
	{ID: "oversize-declared", Expect: "oversize"},
}

// probeProtocol streams the attack script to w. Before every bad frame it
// emits a valid announce frame carrying the attack id, so the host reader
// can assert "next read must fail as <expect>" without losing stream
// alignment. The final frame is a regular report.
func probeProtocol(cfg ProbeConfig, w io.Writer) error {
	for _, atk := range ProtocolAttacks {
		if err := writeEnvelope(w, EnvelopeTypeAttack, AssumptionProtocol, atk); err != nil {
			return err
		}
		if err := writeBadFrame(w, atk.ID); err != nil {
			return err
		}
	}
	return writeEnvelope(w, EnvelopeTypeReport, AssumptionProtocol, probeReport{
		Attacks: nil,
		Detail:  "protocol attack script streamed",
	})
}
