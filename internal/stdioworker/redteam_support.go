// 5C red-team support: the sandbox layout shared by the guard-channel
// records, the child-side config/report contracts, and the bridge into
// worker.Guard (host-file and network are enforced at the guard layer in
// this build; the bare-channel OS boundary is recorded honestly in the
// 5A evidence).
package stdioworker

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/worker"
)

// rtSandbox is the on-disk attack scenario for one record class.
type rtSandbox struct {
	root          string // sandbox root (child working dir)
	marker        string // host secret file OUTSIDE the root
	symlink       string // symlink inside root -> marker ("" if uncreatable)
	junction      string // file through a junction inside root -> host dir
	hostSecretDir string // host-side directory the junction points at
	legit         string // legitimate file inside the root (positive control)
	mountSrc      string // whitelisted mount source (positive control)
}

// rtLayout builds the scenario under base/name.
func rtLayout(base, name string) (*rtSandbox, error) {
	root := filepath.Join(base, "rt-"+name, "root")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		return nil, err
	}
	l := &rtSandbox{
		root:          root,
		marker:        filepath.Join(base, "rt-"+name, "host-marker-secret.txt"),
		symlink:       filepath.Join(root, "escape-link.txt"),
		junction:      filepath.Join(root, "escape-dir", "secret.txt"),
		hostSecretDir: filepath.Join(base, "rt-"+name, "host-secret-dir"),
		legit:         filepath.Join(root, "sub", "legit.txt"),
		mountSrc:      filepath.Join(base, "rt-"+name, "mount-src"),
	}
	secret := "HOST-SECRET-MARKER-5C"
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
	// Directory junction inside the root pointing at the host secret dir
	// (creatable without any privilege).
	if err := makeJunction(filepath.Join(root, "escape-dir"), l.hostSecretDir); err != nil {
		l.junction = ""
	}
	// File symlink needs developer mode or privilege: optional vector.
	_ = os.Remove(l.symlink)
	if err := os.Symlink(l.marker, l.symlink); err != nil {
		l.symlink = ""
	}
	return l, nil
}

// profile builds the guard profile for the file-record class: minimal
// read-only mount, no network allowlist at all.
func (l *rtSandbox) profile(workerID string) worker.Profile {
	return worker.Profile{
		WorkerID: workerID,
		Root:     l.root,
		Mounts:   []worker.Mount{{Source: l.mountSrc, Target: "mnt", ReadOnly: true}},
		Quotas: worker.Quota{
			CPUMillis: 4_000, MemoryMB: 256, DiskMB: 128, DeadlineMS: 60_000,
		},
	}
}

// profileNet builds the guard profile for the network-record class: one
// allowlisted https target as the positive control.
func (l *rtSandbox) profileNet() worker.Profile {
	return worker.Profile{
		WorkerID: "poc5c-network",
		Root:     l.root,
		Mounts:   []worker.Mount{{Source: l.mountSrc, Target: "mnt", ReadOnly: true}},
		NetAllowlist: []worker.NetTarget{
			{Host: "api.example.com", Port: "443"},
		},
		Quotas: worker.Quota{
			CPUMillis: 4_000, MemoryMB: 256, DiskMB: 128, DeadlineMS: 60_000,
		},
	}
}

// newGuardFrom rebuilds a worker.Guard from its serialized profile.
func newGuardFrom(raw []byte) *worker.Guard {
	var p worker.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		p = worker.Profile{Root: filepath.Join(os.TempDir(), "rt-empty-root")}
	}
	return worker.NewGuard(p)
}

// rtDial is one network attack vector with the expected verdict.
type rtDial struct {
	Host        string `json:"host"`
	Port        string `json:"port"`
	Vector      string `json:"vector"`
	WantBlocked bool   `json:"wantBlocked"`
}

// rtChildConfig is the JSON blob handed to the attacker-role child.
type rtChildConfig struct {
	Mode            string          `json:"mode"`
	Profile         json.RawMessage `json:"profile,omitempty"`
	HostMarker      string          `json:"hostMarker,omitempty"`
	Symlink         string          `json:"symlink,omitempty"`
	Junction        string          `json:"junction,omitempty"`
	InRoot          string          `json:"inRoot,omitempty"`
	Dials           []rtDial        `json:"dials,omitempty"`
	SpawnCount      int             `json:"spawnCount,omitempty"`
	MemRequestBytes int64           `json:"memRequestBytes,omitempty"`
}

// rtReport is the structured RESULT payload of an attacker-role child.
type rtReport struct {
	Attacks     []RedTeamAttack `json:"attacks,omitempty"`
	EnvJSON     json.RawMessage `json:"env,omitempty"`
	Spawned     int             `json:"spawned,omitempty"`
	Rejected    int             `json:"rejected,omitempty"`
	Pids        []int           `json:"pids,omitempty"`
	AllocFailed bool            `json:"allocFailed,omitempty"`
	AllocDetail string          `json:"allocDetail,omitempty"`
}

// UnrecoveredRunsJournal scans a journal path directly (evidence helpers).
func UnrecoveredRunsJournal(path string) ([]JournalRecord, error) {
	return UnrecoveredRuns(path)
}

// rtHelperArgs builds the re-exec argv that routes the test binary into the
// red-team child entrypoint with one mode + config blob.
func rtHelperArgs(mode, cfgJSON string) []string {
	return []string{"-test.run", "^TestStdioWorkerChildHelper$", "--", "rt", mode, cfgJSON}
}
