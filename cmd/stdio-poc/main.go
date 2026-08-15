// Command stdio-poc runs the M6 slice-5A stdio strong-isolation POC and
// writes the evidence bundle (bundle.json + stdio-5a.md) for independent
// security review. The POC verdict alone changes nothing in production:
// M6-MCP-004 keeps the stdio transport disabled.
//
// Usage:
//
//	stdio-poc [-out DIR] [-keep]
//
// The binary re-executes itself as the attacker-role probe child when the
// harness spawns it with LUNITIDE_STDIO_POC_PROBE=1 ("probe"/"sleep" argv).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/lunitide/lunitide/internal/stdiopoc"
)

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "stdio-poc: spawn engine is windows-only (Job Object + explicit environment)")
		os.Exit(2)
	}
	if os.Getenv("LUNITIDE_STDIO_POC_PROBE") == "1" && len(os.Args) > 1 {
		os.Exit(child(os.Args[1:]))
	}

	out := flag.String("out", filepath.Join("docs", "evidence", "stdio-poc-5a"), "evidence output directory")
	base := flag.String("base", "", "sandbox base directory (default: a fresh temp dir)")
	keep := flag.Bool("keep", false, "keep the sandbox base directory after the run")
	flag.Parse()

	sbx := *base
	if sbx == "" {
		d, err := os.MkdirTemp("", "stdio-poc-*")
		if err != nil {
			fmt.Fprintln(os.Stderr, "stdio-poc: temp dir:", err)
			os.Exit(2)
		}
		sbx = d
		if !*keep {
			defer os.RemoveAll(sbx)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdio-poc: executable:", err)
		os.Exit(2)
	}
	h := stdiopoc.NewHarness(exe, cliHelperArgs, sbx)
	assumptions, err := h.Run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdio-poc:", err)
		os.Exit(2)
	}
	bundle, err := stdiopoc.BuildBundle(assumptions, time.Now(), runtime.GOOS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdio-poc: bundle:", err)
		os.Exit(2)
	}
	path, err := stdiopoc.WriteEvidence(*out, bundle)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdio-poc: write evidence:", err)
		os.Exit(2)
	}
	for _, a := range bundle.Assumptions {
		mark := "FAIL"
		if a.Passed {
			mark = "PASS"
		}
		fmt.Printf("%-10s %s (%s)\n", mark, a.ID, a.Title)
	}
	fmt.Printf("verdict=%s bundle=%s digest=%s\n", bundle.Verdict, path, bundle.BundleDigest)
	if bundle.Verdict != stdiopoc.VerdictPass {
		os.Exit(1)
	}
}

// cliHelperArgs builds the child argv for the CLI binary: "sleep" parks the
// child for the proctree probe, "probe <id> <json>" runs one attack set.
func cliHelperArgs(probe string, cfg stdiopoc.ProbeConfig) []string {
	if probe == "sleep" {
		return []string{"sleep"}
	}
	raw, _ := json.Marshal(cfg)
	return []string{"probe", probe, string(raw)}
}

// child is the attacker-role entrypoint inside the sandbox.
func child(args []string) int {
	switch args[0] {
	case "sleep":
		time.Sleep(10 * time.Minute)
		return 0
	case "probe":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "probe: missing config")
			return 2
		}
		var cfg stdiopoc.ProbeConfig
		if err := json.Unmarshal([]byte(args[2]), &cfg); err != nil {
			fmt.Fprintln(os.Stderr, "probe: bad config:", err)
			return 3
		}
		if err := stdiopoc.RunProbe(cfg, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "probe:", err)
			return 4
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "probe: unknown mode", args[0])
	return 2
}
