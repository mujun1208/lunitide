package toolruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSystemRunStaysClosedUntilComputerControlOrFullDisk(t *testing.T) {
	r := newProductRuntime(t)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := r.Execute(context.Background(), FullAccess, session, "system.run", []byte(`{"argv":["go","env","GOOS"]}`), true)
	if err == nil || !strings.Contains(err.Error(), "电脑控制") {
		t.Fatalf("closed system.run: %v", err)
	}
}

func TestSystemRunExecutesOutsideTheAllowlist(t *testing.T) {
	r := newProductRuntime(t)
	r.SetSystemReady(func() bool { return true })
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := r.Execute(context.Background(), FullAccess, session, "command.run", []byte(`{"argv":["go","env","GOOS"]}`), true)
	if err == nil {
		t.Fatal("go env must stay off the command.run allowlist")
	}
	out, err := r.Execute(context.Background(), FullAccess, session, "system.run", []byte(`{"argv":["go","env","GOOS"]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "windows") && !strings.Contains(out.Output, "linux") && !strings.Contains(out.Output, "darwin") {
		t.Fatalf("output %q", out.Output)
	}
}

func TestSystemRunInAutoEditWaitsForApproval(t *testing.T) {
	r := newProductRuntime(t)
	r.SetSystemReady(func() bool { return true })
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := r.Execute(context.Background(), AutoEdit, session, "system.run", []byte(`{"argv":["go","env","GOOS"]}`), false)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("auto-edit without approval: %v", err)
	}
	out, err := r.Execute(context.Background(), AutoEdit, session, "system.run", []byte(`{"argv":["go","env","GOOS"]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "windows") && !strings.Contains(out.Output, "linux") && !strings.Contains(out.Output, "darwin") {
		t.Fatalf("output %q", out.Output)
	}
}

func TestSystemRunRefusesGitPushAndDiskDestruction(t *testing.T) {
	r := newProductRuntime(t)
	r.SetSystemReady(func() bool { return true })
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := r.Execute(context.Background(), FullAccess, session, "system.run", []byte(`{"argv":["git","push"]}`), true)
	if err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("git push: %v", err)
	}
	_, err = r.Execute(context.Background(), FullAccess, session, "system.run", []byte(`{"argv":["powershell","-Command","Remove-Item -Recurse C:\\Windows"]}`), true)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "refus") {
		t.Fatalf("hardline: %v", err)
	}
}

func newProductRuntime(t *testing.T) *Runtime {
	t.Helper()
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
