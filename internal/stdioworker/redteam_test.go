package stdioworker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// rtChildMain is the 5C attacker-role child entrypoint: the runtime spawns
// the test binary with argv [-- rt MODE CFGJSON]. Every mode is a real
// attack attempt (or positive control) that reports through the framed
// RESULT — never through side channels.
func rtChildMain(mode, cfgArg string) int {
	var cfg rtChildConfig
	if err := json.Unmarshal([]byte(cfgArg), &cfg); err != nil {
		return 2
	}
	if mode == "grandchild" {
		// Sleeps until the job object reaps it. The lifetime matters to what
		// the proctree record is able to prove: the active-process quota
		// bounds how many run *at once*, and the record measures it by
		// counting successful starts. A grandchild that exits during the
		// spawn loop frees its slot and lets a later start succeed without
		// the quota ever being exceeded, so a short sleep turns a correct
		// kernel into a failed record — which is what a race-instrumented
		// binary, slow enough to stretch the loop past two seconds, did.
		// Outliving the run also means the tree can only be gone afterwards
		// because the job killed it, which is the other half of the record.
		time.Sleep(10 * time.Minute)
		return 0
	}
	child, err := ChildFromEnv(os.Getenv, os.Stdout)
	if err != nil {
		return 2
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
		_ = WriteEnvelope(os.Stdout, &Envelope{SessionID: "evil-session", Seq: 0, Type: EnvHello})
		select {}
	case "seq-gap":
		_ = child.Hello()
		_ = WriteEnvelope(os.Stdout, &Envelope{SessionID: child.SessionID(), Seq: 42, Type: EnvHeartbeat})
		select {}
	case "digest-lie":
		env := Envelope{SessionID: child.SessionID(), Seq: 0, Type: EnvHello, Data: []byte(`{"specDigest":"deadbeef","protocol":"` + DefaultPolicy().ProtocolVer + `"}`)}
		_ = WriteEnvelope(os.Stdout, &env)
		select {}
	case "guard-path":
		guard := newGuardFrom(cfg.Profile)
		var attacks []RedTeamAttack
		check := func(vector, attempt, path string, wantAllowed bool) {
			err := guard.CheckPath(path)
			blocked := err != nil
			attacks = append(attacks, RedTeamAttack{
				Vector:  vector,
				Attempt: attempt,
				Blocked: blocked != wantAllowed, // refused when wanted, allowed when wanted
				Detail:  fmt.Sprintf("guard: %v", err),
			})
		}
		check("direct-host-file", "read a host secret outside the sandbox root", cfg.HostMarker, false)
		if cfg.Symlink != "" {
			check("symlink-escape", "follow a symlink planted inside the root", cfg.Symlink, false)
		}
		if cfg.Junction != "" {
			check("junction-escape", "reach a host dir through a junction inside the root", cfg.Junction, false)
		}
		check("positive-control", "legitimate file inside the root stays reachable", cfg.InRoot, true)
		_ = child.Hello()
		_ = child.Result(rtReport{Attacks: attacks})
	case "guard-dial":
		guard := newGuardFrom(cfg.Profile)
		var attacks []RedTeamAttack
		for _, t := range cfg.Dials {
			err := guard.CheckDial(t.Host, t.Port)
			gotBlocked := err != nil
			attacks = append(attacks, RedTeamAttack{
				Vector:  t.Vector,
				Attempt: fmt.Sprintf("dial %s:%s (want blocked=%v)", t.Host, t.Port, t.WantBlocked),
				Blocked: gotBlocked == t.WantBlocked,
				Detail:  fmt.Sprintf("guard: %v", err),
			})
		}
		_ = child.Hello()
		_ = child.Result(rtReport{Attacks: attacks})
	case "env-probe":
		envJSON, _ := json.Marshal(os.Environ())
		_ = child.Hello()
		_ = child.Result(rtReport{EnvJSON: envJSON})
	case "spawn-bomb":
		exe, err := os.Executable()
		if err != nil {
			return 2
		}
		var rep rtReport
		for i := 0; i < cfg.SpawnCount; i++ {
			cmd := exec.Command(exe, "-test.run", "^TestStdioWorkerChildHelper$", "--", "rt", "grandchild", "{}")
			if err := cmd.Start(); err != nil {
				rep.Rejected++
			} else {
				rep.Spawned++
				rep.Pids = append(rep.Pids, cmd.Process.Pid)
			}
		}
		_ = child.Hello()
		for i := 0; i < 10; i++ { // keep the root alive while grandchildren run
			time.Sleep(200 * time.Millisecond)
			_ = child.Heartbeat()
		}
		_ = child.Result(rep)
	case "alloc-mem":
		committed, failed, detail := rtAllocProbe(cfg.MemRequestBytes)
		_ = child.Hello()
		_ = child.Heartbeat()
		_ = child.Result(rtReport{
			AllocFailed: failed,
			AllocDetail: fmt.Sprintf("%s (requested %d MiB, committed %d MiB)", detail, cfg.MemRequestBytes>>20, committed>>20),
		})
	default:
		return 2
	}
	return 0
}

// TestRedTeamProductionEscape drives the full 5C drill — eight red-team
// record classes plus fault injection and capacity — against the real 5B
// runtime, then writes the evidence bundle. A PASS authorizes nothing by
// itself: the Gate stays closed (M6-MCP-004).
func TestRedTeamProductionEscape(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("spawn engine is windows-only")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	runner, err := NewRedTeamRunner(exe, filepath.Join(base, "rt"))
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	records, err := runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, rec := range records {
		if !rec.Passed {
			failed++
			t.Errorf("5C record %s FAILED: hostCheck=%s", rec.ID, rec.HostCheck)
			for _, atk := range rec.Attacks {
				if !atk.Blocked {
					t.Errorf("  vector %s not blocked: %s", atk.Vector, atk.Detail)
				}
			}
		}
	}
	pd, err := runner.manager.PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildRedTeamBundle(records, pd, RedTeamPlatform(), false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path, err := WriteRedTeamEvidence(filepath.Join(base, "stdio-5c"), bundle)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Verdict != VerdictPass {
		t.Fatalf("5C verdict=%s (%d/%d records failed)", bundle.Verdict, failed, len(records))
	}
	if bundle.GateOpen {
		t.Fatal("repo-produced 5C evidence must record a closed gate")
	}
	t.Logf("5C evidence: %s bundleDigest=%s policyDigest=%s", path, bundle.BundleDigest, pd)

	// durable archive: LUNITIDE_5C_EVIDENCE_DIR pins the bundle location
	// (e.g. docs/evidence/stdio-5c) so the PASS is reviewable in-repo.
	if out := os.Getenv("LUNITIDE_5C_EVIDENCE_DIR"); out != "" {
		archived, err := WriteRedTeamEvidence(out, bundle)
		if err != nil {
			t.Fatalf("archive 5C evidence to %s: %v", out, err)
		}
		t.Logf("5C evidence archived: %s", archived)
	}
}
