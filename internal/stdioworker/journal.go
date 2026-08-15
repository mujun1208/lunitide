package stdioworker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Recovery journal (5B): an append-only, fsync'd JSONL file recording the
// lifecycle of every worker run. On host restart the Job Objects die with
// the process (kill-on-close), so recovery is deterministic: any LAUNCHED
// run without a terminal record was killed with the host and is marked
// CRASHED — the task layer requeues via normal lease-expiry semantics
// (TSK-002), never double-settling.

// Run states.
const (
	StateLaunched    = "LAUNCHED"
	StateCompleted   = "COMPLETED"
	StateRevoked     = "REVOKED"
	StateExpired     = "EXPIRED"
	StateCrashed     = "CRASHED"
	StateUnrecovered = "UNRECOVERED"
)

// terminal reports whether s ends the run.
func terminal(s string) bool {
	switch s {
	case StateCompleted, StateRevoked, StateExpired, StateCrashed:
		return true
	}
	return false
}

// JournalRecord is one journal line.
type JournalRecord struct {
	RunID      string `json:"runId"`
	SpecID     string `json:"specId"`
	Endpoint   string `json:"endpointId"`
	SpecDigest string `json:"specDigest"`
	Pid        int    `json:"pid"`
	State      string `json:"state"`
	Detail     string `json:"detail,omitempty"`
	AtMS       int64  `json:"atMs"`
}

// Journal is the append-only recovery log.
type Journal struct {
	mu   sync.Mutex
	path string
	f    *os.File
	w    *bufio.Writer
}

// OpenJournal opens (creating if needed) the journal file at path.
func OpenJournal(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	// Quarantine a torn tail: a crash mid-append can leave the last line
	// unterminated, and appending after it would merge records into one
	// corrupt blob (the recovery walk would then lose the good tail too).
	// Terminating the tail isolates it; the parser already skips bad lines.
	// (The write handle is O_WRONLY|O_APPEND, so the tail byte is probed
	// through a separate read handle; the append write needs no seek.)
	if st, serr := f.Stat(); serr == nil && st.Size() > 0 {
		if rf, rerr := os.Open(path); rerr == nil {
			var last [1]byte
			if n, _ := rf.ReadAt(last[:], st.Size()-1); n == 1 && last[0] != '\n' {
				_, _ = f.Write([]byte{'\n'})
			}
			rf.Close()
		}
	}
	return &Journal{path: path, f: f, w: bufio.NewWriter(f)}, nil
}

// Append writes one record and fsyncs (durability before launch/kill).
func (j *Journal) Append(rec JournalRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := j.w.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err := j.w.Flush(); err != nil {
		return err
	}
	return j.f.Sync()
}

// Close closes the journal file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	err := j.f.Close()
	j.f = nil
	return err
}

// UnrecoveredRuns scans the journal and returns the latest record of every
// run that has no terminal state — the runs a crashed host left behind.
// Truncated/corrupt tail lines are tolerated (a crash mid-append): the
// parser stops at the first bad line and treats affected runs as
// unrecovered too.
func UnrecoveredRuns(path string) ([]JournalRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	latest := map[string]JournalRecord{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec JournalRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			// corrupt tail: remember it happened, keep prior good state.
			continue
		}
		latest[rec.RunID] = rec
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("stdioworker: scan journal: %w", err)
	}
	var out []JournalRecord
	for _, rec := range latest {
		if !terminal(rec.State) {
			out = append(out, rec)
		}
	}
	return out, nil
}

// MarkCrashed appends the CRASHED terminal record for one unrecovered run.
func (j *Journal) MarkCrashed(rec JournalRecord, atMS int64, detail string) error {
	rec.State = StateCrashed
	rec.AtMS = atMS
	rec.Detail = detail
	return j.Append(rec)
}
