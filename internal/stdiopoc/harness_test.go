package stdiopoc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func dummyNow() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }

// TestStdioPocProbeProcess is the child entrypoint of the POC: the harness
// spawns the test binary with LUNITIDE_STDIO_POC_PROBE=1 and argv after
// "--" driving either a sleeping grandchild (proctree probe) or one probe
// run whose report goes to stdout as framed envelopes.
func TestStdioPocProbeProcess(t *testing.T) {
	if os.Getenv("LUNITIDE_STDIO_POC_PROBE") != "1" {
		return
	}
	args := os.Args
	var rest []string
	for i, arg := range args {
		if arg == "--" {
			rest = args[i+1:]
			break
		}
	}
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "probe: missing args")
		os.Exit(2)
	}
	if rest[0] == "sleep" {
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "probe: missing config")
		os.Exit(2)
	}
	var cfg ProbeConfig
	if err := json.Unmarshal([]byte(rest[1]), &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "probe: bad config:", err)
		os.Exit(3)
	}
	if err := RunProbe(cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "probe:", err)
		os.Exit(4)
	}
	os.Exit(0)
}

func pocHelperArgs(probe string, cfg ProbeConfig) []string {
	raw, _ := json.Marshal(cfg)
	return []string{"-test.run", "^TestStdioPocProbeProcess$", "--", probe, string(raw)}
}

// TestStdioPOCEndToEnd runs the full six-assumption POC on Windows: real
// spawn engine, real child processes, real guard, real frame validator,
// host-side cross validation, evidence bundle + report on disk.
func TestStdioPOCEndToEnd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("stdio POC spawn engine is windows-only (got %s)", runtime.GOOS)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	h := NewHarness(exe, pocHelperArgs, base)
	h.now = dummyNow
	assumptions, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	if len(assumptions) != len(assumptionOrder) {
		t.Fatalf("want %d assumptions, got %d", len(assumptionOrder), len(assumptions))
	}
	bundle, err := BuildBundle(assumptions, dummyNow(), runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range bundle.Assumptions {
		if !a.Passed {
			t.Errorf("assumption %s FAILED:\n  hostCheck: %+v\n  attacks:", a.ID, a.HostCheck)
			for _, atk := range a.Attacks {
				t.Errorf("    %s blocked=%v detail=%s", atk.Vector, atk.Blocked, atk.Detail)
			}
		}
	}
	if bundle.Verdict != VerdictPass {
		t.Fatalf("POC verdict = %s, want PASS (see per-assumption failures above)", bundle.Verdict)
	}
	dir := filepath.Join(base, "evidence", "stdio-poc")
	path, err := WriteEvidence(dir, bundle)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back EvidenceBundle
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("bundle.json unreadable: %v", err)
	}
	if back.BundleDigest != bundle.BundleDigest || back.Verdict != VerdictPass {
		t.Fatal("evidence bundle round-trip mismatch")
	}
	if _, err := os.Stat(filepath.Join(dir, "stdio-5a.md")); err != nil {
		t.Fatalf("stdio-5a.md missing: %v", err)
	}
	// The gate itself must remain closed regardless of the verdict.
	if !strings.Contains(strings.Join(BundleNotes, "\n"), "M6-MCP-004") {
		t.Fatal("bundle notes must state M6-MCP-004 stays in force")
	}
}
