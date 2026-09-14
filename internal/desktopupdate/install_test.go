package desktopupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNsisInstallerLaunchesSilentSetup(t *testing.T) {
	dir := t.TempDir()
	body := []byte("setup-bytes")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	name := "Lunitide-Setup-0.4.84-x64.exe"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(`{"version":"0.4.84","channel":"stable","sha256":"`+digest+`","installer":"`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var got *exec.Cmd
	inst := NewNsisInstaller(&LocalFeed{Dirs: []string{dir}})
	inst.Start = func(cmd *exec.Cmd) error { got = cmd; return nil }
	if err := inst.Install(context.Background(), "inst", "pkg", digest); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Path != path || len(got.Args) < 2 || got.Args[1] != "/S" {
		t.Fatalf("cmd = %#v", got)
	}
	if err := inst.Install(context.Background(), "inst", "pkg", hex.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("unknown digest must fail")
	}
}
