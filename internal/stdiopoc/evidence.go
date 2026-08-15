package stdiopoc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EvidenceBundle is the durable POC artifact: the six assumption records,
// each chained by digest, plus the environment identity (build context and
// config digest placeholders the 5B/5C gates will bind harder).
type EvidenceBundle struct {
	Schema       string       `json:"schema"` // stdio-poc-evidence/1
	GeneratedAt  time.Time    `json:"generatedAt"`
	Platform     string       `json:"platform"`
	Verdict      string       `json:"verdict"` // PASS | FAIL
	Assumptions  []Assumption `json:"assumptions"`
	BundleDigest string       `json:"bundleDigest"`
	Notes        []string     `json:"notes,omitempty"`
}

// BundleNotes are always embedded: the POC verdict alone changes nothing in
// production. These lines exist so nobody can read a PASS as "stdio on".
var BundleNotes = []string{
	"POC PASS only permits entering 5B (controlled implementation) development.",
	"stdio transport stays DISABLED: M6-MCP-004 remains in force.",
	"host-file and network assumptions are enforced at the guard layer (the stdio worker runtime funnels access through the host guard); the OS-level boundary (AppContainer) is 5B scope.",
	"secret, proctree and resource assumptions are OS-enforced (explicit environment block, Job Object quotas).",
	"5A/5B/5C evidence must bind the same build/config digest before production enablement.",
}

// BuildBundle computes per-assumption digests and the bundle chain digest.
func BuildBundle(assumptions []Assumption, now time.Time, platform string) (*EvidenceBundle, error) {
	b := &EvidenceBundle{
		Schema:      "stdio-poc-evidence/1",
		GeneratedAt: now.UTC(),
		Platform:    platform,
		Assumptions: assumptions,
		Notes:       BundleNotes,
	}
	b.Verdict = VerdictPass
	chain := ""
	for i := range b.Assumptions {
		raw, err := canonicalJSON(b.Assumptions[i])
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		b.Assumptions[i].Digest = hex.EncodeToString(sum[:])
		if !b.Assumptions[i].Passed {
			b.Verdict = VerdictFail
		}
		chain += b.Assumptions[i].Digest
	}
	total := sha256.Sum256([]byte(chain))
	b.BundleDigest = hex.EncodeToString(total[:])
	return b, nil
}

// canonicalJSON marshals v with sorted, deterministic encoding.
func canonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// WriteEvidence persists the bundle (bundle.json) and the human review
// report (stdio-5a.md) under dir. It returns the written bundle path.
func WriteEvidence(dir string, b *EvidenceBundle) (string, error) {
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
	md := renderReport(b)
	if err := os.WriteFile(filepath.Join(dir, "stdio-5a.md"), []byte(md), 0o644); err != nil {
		return "", err
	}
	return bundlePath, nil
}

// renderReport builds the security-review markdown.
func renderReport(b *EvidenceBundle) string {
	var w strings.Builder
	fmt.Fprintf(&w, "# stdio Strong-Isolation POC Evidence (M6 slice 5A)\n\n")
	fmt.Fprintf(&w, "- Generated: %s\n", b.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&w, "- Platform: %s\n", b.Platform)
	fmt.Fprintf(&w, "- Schema: %s\n", b.Schema)
	fmt.Fprintf(&w, "- Bundle digest: `%s`\n", b.BundleDigest)
	fmt.Fprintf(&w, "- Verdict: **%s**\n\n", b.Verdict)
	w.WriteString("## Assumptions\n\n")
	w.WriteString("| # | assumption | enforced by | verdict | attacks | digest |\n")
	w.WriteString("|---|------------|-------------|---------|---------|--------|\n")
	for i, a := range b.Assumptions {
		verdict := "FAIL"
		if a.Passed {
			verdict = "PASS"
		}
		fmt.Fprintf(&w, "| %d | %s (`%s`) | %s | %s | %d | `%s` |\n",
			i+1, a.Title, a.ID, a.EnforcedBy, verdict, len(a.Attacks), shortDigest(a.Digest))
	}
	w.WriteString("\n## Attack detail\n\n")
	for _, a := range b.Assumptions {
		fmt.Fprintf(&w, "### %s — %s\n\n", a.ID, a.Title)
		fmt.Fprintf(&w, "- enforced by: %s\n- host check: %v (%s)\n", a.EnforcedBy, a.HostCheck.Confirmed, a.HostCheck.Detail)
		if a.Summary != "" {
			fmt.Fprintf(&w, "- summary: %s\n", a.Summary)
		}
		w.WriteString("\n| vector | blocked | observation |\n|--------|---------|-------------|\n")
		for _, atk := range a.Attacks {
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

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
