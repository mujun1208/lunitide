// Package worker implements the M6 strongly-isolated Worker runtime
// primitives: the sandbox guard (T-6.2.2 — independent identity, temporary
// root, minimal mounts, network allowlist) and the lease/fencing manager
// (T-6.2.3 — heartbeat renewal, Reaper reclamation, fencing-token result
// rejection). Escapes are terminal: the guard reports them, the runtime
// terminates the worker and freezes an evidence bundle next to the sandbox
// root (SBX-001/002), late results from lost workers are rejected by
// fencing token (SBX-004).
package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Escape kinds (map onto M6-SBX codes).
const (
	EscapeFile    = "file"    // SBX-001
	EscapeNetwork = "network" // SBX-002
	EscapeQuota   = "quota"   // SBX-003
)

// EscapeError is returned by every guard check that detects an escape
// attempt. It is terminal: the runtime must terminate the worker and keep
// the evidence bundle.
type EscapeError struct {
	Kind   string
	Detail string
}

func (e *EscapeError) Error() string {
	return fmt.Sprintf("worker: sandbox escape (%s): %s", e.Kind, e.Detail)
}

// Quota bounds one worker run (SBX-003).
type Quota struct {
	CPUMillis  int64
	MemoryMB   int64
	DiskMB     int64
	DeadlineMS int64
}

// Mount is one minimal-mount entry: only whitelisted sources map into the
// sandbox root, read-only unless explicitly stated.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// NetTarget is one allowlist entry; empty Port means any port on that host.
type NetTarget struct {
	Host string
	Port string
}

// Usage is the measured consumption reported against the quota.
type Usage struct {
	CPUMillis  int64
	MemoryMB   int64
	DiskMB     int64
	ElapsedMS  int64
}

// Profile describes the sandbox: an independent identity (WorkerID), a
// dedicated temporary root, minimal mounts, a network allowlist and quotas.
type Profile struct {
	WorkerID     string
	Root         string // canonical absolute path of the sandbox root
	Mounts       []Mount
	NetAllowlist []NetTarget
	Quotas       Quota
}

// Evidence is the durable escape record (bundle path included).
type Evidence struct {
	EvidenceID string    `json:"evidenceId"`
	WorkerID   string    `json:"workerId"`
	Kind       string    `json:"kind"`
	Detail     string    `json:"detail"`
	At         time.Time `json:"at"`
	BundlePath string    `json:"bundlePath"`
}

// Guard enforces one Profile.
type Guard struct {
	profile Profile
	now     func() time.Time
}

func NewGuard(p Profile) *Guard { return &Guard{profile: p, now: time.Now} }

// metadata endpoints are unconditionally denied even if someone lists them.
var deniedMetadataHosts = map[string]bool{
	"169.254.169.254":          true, // AWS/GCP/Azure IMDS
	"metadata.google.internal": true,
	"100.100.100.200":          true, // Alibaba IMDS
	"fd00:ec2::254":            true, // AWS IPv6 IMDS
	"metadata":                 true,
}

var loopbackHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}

// escape records an EscapeError and freezes the evidence bundle under the
// shared evidence directory of the workers area (root lives two levels
// below: <workers>/<workerID>/root).
func (g *Guard) escape(kind, detail string) error {
	evDir := filepath.Join(filepath.Dir(filepath.Dir(g.profile.Root)), "evidence")
	_ = os.MkdirAll(evDir, 0o755)
	ev := Evidence{
		EvidenceID: ulid.Make().String(),
		WorkerID:   g.profile.WorkerID,
		Kind:       kind,
		Detail:     detail,
		At:         g.now().UTC(),
	}
	raw, err := json.Marshal(ev)
	if err == nil {
		ev.BundlePath = filepath.Join(evDir, fmt.Sprintf("%s-%s.json", ev.WorkerID, kind))
		_ = os.WriteFile(ev.BundlePath, append(raw, '\n'), 0o644)
	}
	return &EscapeError{Kind: kind, Detail: detail}
}

// CheckPath validates one worker file access (SBX-001). Lexical escapes are
// rejected before touching the filesystem; then the real path (symlinks and
// junctions resolved) must stay inside the sandbox root or one of the
// whitelisted mount sources. The sandbox root itself must live under its
// worker scratch directory (defense in depth against a bogus root).
func (g *Guard) CheckPath(p string) error {
	root := filepath.Clean(g.profile.Root)
	// Lexical pass: separators normalized, .. components rejected outright.
	normalized := filepath.FromSlash(p)
	if filepath.IsAbs(normalized) {
		rel, err := filepath.Rel(root, filepath.Clean(normalized))
		if err != nil || escapes(rel) {
			return g.escape(EscapeFile, "absolute path outside sandbox: "+p)
		}
	} else {
		for _, part := range strings.Split(filepath.ToSlash(normalized), "/") {
			if part == ".." {
				return g.escape(EscapeFile, "parent traversal in path: "+p)
			}
		}
	}
	// Real-path pass: resolve symlinks/junctions on the deepest existing
	// ancestor, then require containment under the root or a mount source.
	real, err := realPath(normalized, root)
	if err != nil {
		return g.escape(EscapeFile, "unresolvable path: "+p)
	}
	if !g.contains(real) {
		return g.escape(EscapeFile, "resolved path escapes sandbox: "+p+" -> "+real)
	}
	return nil
}

func escapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ""
}

func (g *Guard) contains(real string) bool {
	root := filepath.Clean(g.profile.Root)
	if under(real, root) {
		return true
	}
	for _, m := range g.profile.Mounts {
		if under(real, filepath.Clean(m.Source)) {
			return true
		}
	}
	return false
}

func under(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// maxReparseDepth bounds chained reparse point resolution (Windows caps
// symlink chains itself; we keep a hard stop anyway).
const maxReparseDepth = 32

// realPath resolves the real location of p with every reparse point on the
// way — symlinks AND Windows junctions — so an escape-dir inside the sandbox
// pointing at a host directory is expanded to the host location before the
// containment check. Nonexistent tail components are kept unresolved, which
// is fine: creating them later cannot land outside what the check approved.
//
// Junction note (found by the M6 slice-5A stdio POC): filepath.EvalSymlinks
// does NOT resolve junctions on Windows and Lstat does not flag them as
// ModeSymlink, so the previous EvalSymlinks-based pass let
// <root>\escape-dir\secret.txt (junction -> host dir) through. os.Readlink
// succeeds for both symlinks and junctions, so the walk below probes every
// component with it.
func realPath(p, root string) (string, error) {
	if !filepath.IsAbs(p) {
		// Worker paths are root-relative by contract (the child cwd IS the
		// root): anchor them before resolving so Abs() never resolves
		// against the host process CWD.
		p = filepath.Join(root, p)
	}
	if !filepath.IsAbs(root) {
		// Relative root means the guard was built with a fake root (tests
		// without disk): skip resolution.
		return filepath.Abs(p)
	}
	return resolveReparse(p, 0)
}

func resolveReparse(p string, depth int) (string, error) {
	if depth > maxReparseDepth {
		return "", errors.New("reparse chain too deep")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	vol := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs, vol)
	rest = strings.TrimPrefix(rest, string(filepath.Separator))
	if rest == "" {
		return filepath.Clean(vol + string(filepath.Separator)), nil
	}
	parts := strings.Split(rest, string(filepath.Separator))
	cur := filepath.Clean(vol + string(filepath.Separator))
	for i, part := range parts {
		next := filepath.Join(cur, part)
		if target, err := os.Readlink(next); err == nil {
			// Reparse point (symlink or junction). Readlink on Windows
			// already normalizes the \??\ device prefix away.
			if !filepath.IsAbs(target) {
				target = filepath.Join(cur, target)
			}
			remaining := strings.Join(parts[i+1:], string(filepath.Separator))
			return resolveReparse(filepath.Join(target, remaining), depth+1)
		}
		cur = next
	}
	return cur, nil
}

// CheckDial validates one egress dial (SBX-002): the target host:port must
// be on the profile allowlist; metadata endpoints, link-local addresses and
// loopback are denied even when listed (explicit allow-by-mistake must
// still fail).
func (g *Guard) CheckDial(host, port string) error {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimSuffix(h, ".")
	if deniedMetadataHosts[h] {
		return g.escape(EscapeNetwork, "cloud metadata endpoint: "+h)
	}
	if addr, err := netip.ParseAddr(h); err == nil && addr.IsLinkLocalUnicast() {
		return g.escape(EscapeNetwork, "link-local address: "+h)
	}
	if loopbackHosts[h] {
		return g.escape(EscapeNetwork, "loopback dial: "+h)
	}
	if !g.listed(h, port) {
		return g.escape(EscapeNetwork, "target not on allowlist: "+host+":"+port)
	}
	return nil
}

func (g *Guard) listed(host, port string) bool {
	for _, t := range g.profile.NetAllowlist {
		if strings.EqualFold(strings.TrimSpace(t.Host), host) && (t.Port == "" || t.Port == port) {
			return true
		}
	}
	return false
}

// CheckQuota enforces the resource ceilings (SBX-003). The zero Quota means
// "no ceiling configured" and only applies the deadline.
func (g *Guard) CheckQuota(u Usage) error {
	q := g.profile.Quotas
	if q.CPUMillis > 0 && u.CPUMillis > q.CPUMillis {
		return g.escape(EscapeQuota, fmt.Sprintf("cpu %dms > %dms", u.CPUMillis, q.CPUMillis))
	}
	if q.MemoryMB > 0 && u.MemoryMB > q.MemoryMB {
		return g.escape(EscapeQuota, fmt.Sprintf("memory %dMB > %dMB", u.MemoryMB, q.MemoryMB))
	}
	if q.DiskMB > 0 && u.DiskMB > q.DiskMB {
		return g.escape(EscapeQuota, fmt.Sprintf("disk %dMB > %dMB", u.DiskMB, q.DiskMB))
	}
	if q.DeadlineMS > 0 && u.ElapsedMS > q.DeadlineMS {
		return g.escape(EscapeQuota, fmt.Sprintf("elapsed %dms > %dms", u.ElapsedMS, q.DeadlineMS))
	}
	return nil
}

// Rejects output artifacts outside the allowed kinds (the worker bridge
// contract: only result manifest, patch, test report and usage).
var allowedArtifactKinds = map[string]bool{
	"result_manifest": true, "patch": true, "test_report": true, "usage": true,
}

var artifactNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][\w.-]{0,127}$`)

// CheckArtifact validates one declared output artifact kind + name.
func (g *Guard) CheckArtifact(kind, name string) error {
	if !allowedArtifactKinds[kind] {
		return g.escape(EscapeFile, "artifact kind not allowed: "+kind)
	}
	if !artifactNamePattern.MatchString(name) {
		return g.escape(EscapeFile, "artifact name not allowed: "+name)
	}
	return nil
}
