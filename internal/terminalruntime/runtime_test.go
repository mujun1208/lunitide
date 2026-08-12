package terminalruntime

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigurationAndBounds(t *testing.T) {
	root := t.TempDir()
	r, e := New(Config{Workspace: root, MaxSessions: 1, MaxInputBytes: 3, MaxOutputBytes: 4})
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Start(nil, "bad/id", 80, 24); !errors.Is(e, ErrInvalid) {
		t.Fatalf("invalid id: %v", e)
	}
	if e = r.Start(nil, "x", 0, 24); !errors.Is(e, ErrInvalid) {
		t.Fatalf("invalid dimensions: %v", e)
	}
	if e = r.Write("x", []byte("1234")); !errors.Is(e, ErrInvalid) {
		t.Fatalf("input bound: %v", e)
	}
	if runtime.GOOS != "windows" {
		if e = r.Start(nil, "x", 80, 24); !errors.Is(e, ErrUnsupported) {
			t.Fatalf("unsupported: %v", e)
		}
	}
}
func TestWorkspaceMustBeControlled(t *testing.T) {
	if _, e := New(Config{Workspace: "relative"}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := New(Config{Workspace: filepath.Join(t.TempDir(), "missing")}); e == nil {
		t.Fatal("accepted missing workspace")
	}
}
func TestSanitizedEnvironment(t *testing.T) {
	e := sanitizedEnvironment(`C:\Windows`, `C:\controlled`)
	joined := strings.Join(e, "\n")
	for _, bad := range []string{"GIT_CONFIG", "PSModulePath="} {
		if strings.Contains(strings.ToUpper(joined), strings.ToUpper(bad)) {
			t.Fatalf("leaked %s", bad)
		}
	}
	if !strings.Contains(joined, "SystemRoot=") {
		t.Fatal("missing system root")
	}
	if !strings.Contains(joined, `USERPROFILE=C:\controlled`) {
		t.Fatal("profile is not controlled")
	}
	if !strings.Contains(joined, `APPDATA=C:\controlled`) {
		t.Fatal("app data is not controlled")
	}
}
func TestAuditContainsDigestsNotTranscript(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	r, e := New(Config{Workspace: t.TempDir(), AuditPath: p})
	if e != nil {
		t.Fatal(e)
	}
	r.audit("write", "s", []byte("TOP SECRET TRANSCRIPT"), nil)
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "TOP SECRET") {
		t.Fatal("audit persisted transcript")
	}
	if !strings.Contains(string(b), "digest") {
		t.Fatal("missing digest")
	}
}

func TestSessionAndOutputBounds(t *testing.T) {
	r, err := New(Config{Workspace: t.TempDir(), MaxSessions: 1, MaxOutputBytes: 4, EventBuffer: 4})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{id: "one"}
	r.sessions[s.id] = s
	if err := r.Start(nil, "two", 80, 24); !errors.Is(err, ErrLimit) {
		t.Fatalf("session limit: %v", err)
	}
	r.output(s, []byte("123456"))
	ev := <-r.Events()
	if string(ev.Data) != "1234" || s.output != 4 {
		t.Fatalf("output was not bounded: %q (%d)", ev.Data, s.output)
	}
	r.output(s, []byte("more"))
	select {
	case ev := <-r.Events():
		t.Fatalf("unexpected output after cap: %#v", ev)
	default:
	}
}
