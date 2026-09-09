//go:build windows

package winexec

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestQuitProcessHelper(t *testing.T) {
	if os.Getenv("LUNITIDE_QUIT_TEST_HELPER") != "1" {
		return
	}
	time.Sleep(time.Minute)
}

func TestQuitProcessImagesActuallyExitsOnlyNamedFixture(t *testing.T) {
	dir := t.TempDir()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	launch := func(name string) *exec.Cmd {
		t.Helper()
		in, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		dest := filepath.Join(dir, name)
		out, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		out.Close()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dest, "-test.run=^TestQuitProcessHelper$")
		cmd.Env = append(os.Environ(), "LUNITIDE_QUIT_TEST_HELPER=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd
	}
	suffix := fmt.Sprintf("%d-%d.exe", os.Getpid(), time.Now().UnixNano())
	name := "quit-fixture-" + suffix
	launch(name)
	other := "keep-fixture-" + suffix
	launch(other)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count, err := QuitProcessImages(ctx, []string{name})
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if len(LookupProcessImages([]string{name})) != 0 {
		t.Fatal("target is still running")
	}
	if len(LookupProcessImages([]string{other})) != 1 {
		t.Fatal("unrelated process was affected")
	}
}
