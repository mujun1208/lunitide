package stdioworker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func TestJournalAppendAndUnrecovered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery-journal.jsonl")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	// run A: launched then completed (terminal)
	if err := j.Append(JournalRecord{RunID: "A", SpecID: "s1", State: StateLaunched, AtMS: 1}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalRecord{RunID: "A", SpecID: "s1", State: StateCompleted, AtMS: 2}); err != nil {
		t.Fatal(err)
	}
	// run B: launched, never settled (host crash)
	if err := j.Append(JournalRecord{RunID: "B", SpecID: "s2", State: StateLaunched, AtMS: 3}); err != nil {
		t.Fatal(err)
	}
	// run C: launched then revoked
	if err := j.Append(JournalRecord{RunID: "C", SpecID: "s3", State: StateLaunched, AtMS: 4}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalRecord{RunID: "C", SpecID: "s3", State: StateRevoked, AtMS: 5}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	unrec, err := UnrecoveredRuns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrec) != 1 || unrec[0].RunID != "B" {
		t.Fatalf("want only B unrecovered, got %+v", unrec)
	}
}

func TestJournalToleratesCorruptTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery-journal.jsonl")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalRecord{RunID: "D", State: StateLaunched, AtMS: 1}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	// crash mid-append: half-written line
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"runId":"E","state":"LAUN`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	unrec, err := UnrecoveredRuns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrec) != 1 || unrec[0].RunID != "D" {
		t.Fatalf("corrupt tail must not hide run D: %+v", unrec)
	}
}

func TestMarkCrashedSettles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery-journal.jsonl")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.Append(JournalRecord{RunID: "F", State: StateLaunched, AtMS: 1}); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkCrashed(JournalRecord{RunID: "F"}, time.Now().UnixMilli(), "host crash"); err != nil {
		t.Fatal(err)
	}
	unrec, err := UnrecoveredRuns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrec) != 0 {
		t.Fatalf("crashed run must be terminal: %+v", unrec)
	}
}

// TestTornTailQuarantine: a crash mid-append leaves an unterminated line;
// the next open must terminate it so subsequent records stay parseable and
// the recovery walk remains idempotent (5C red-team finding).
func TestTornTailQuarantine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery-journal.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"runId":"a","specId":"s1","endpointId":"e","state":"LAUNCHED","atMs":1}` + "\n" + `{"runId":"torn","state":"LAUN`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalRecord{RunID: "b", SpecID: "s2", Endpoint: "e", State: StateCompleted, AtMS: 2}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	recs, err := UnrecoveredRuns(path)
	if err != nil {
		t.Fatal(err)
	}
	// a stays unrecovered, b is terminal, the torn line is skipped — no
	// record merges into the corrupt tail.
	if len(recs) != 1 || recs[0].RunID != "a" {
		t.Fatalf("want exactly [a] unrecovered, got %+v", recs)
	}
	// the record appended after the quarantine must be independently
	// parseable (it must NOT be fused into the torn line)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 journal lines (a, torn, b), got %d: %q", len(lines), string(raw))
	}
	var b JournalRecord
	if err := json.Unmarshal([]byte(lines[2]), &b); err != nil || b.RunID != "b" || b.State != StateCompleted {
		t.Fatalf("tail line must be the standalone b record, got %q (%v)", lines[2], err)
	}
}
