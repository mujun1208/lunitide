package toolruntime

import (
	"strings"
	"testing"
)

func TestLaunchVerifyQueriesIncludeSodaProcess(t *testing.T) {
	got := launchVerifyQueries("汽水")
	want := map[string]bool{}
	for _, q := range got {
		want[q] = true
	}
	for _, need := range []string{"sodamusic.exe", "Soda Music", "汽水音乐"} {
		if !want[need] {
			t.Fatalf("query %q missing from %v", need, got)
		}
	}
}

func TestOpenedWindowConfirmedSodaBehindCompanion(t *testing.T) {
	queries := launchVerifyQueries("汽水")
	if openedWindowConfirmed("月伴对话 - Lunitide", "lunitide.exe", queries) {
		t.Fatal("companion foreground must not count as soda")
	}
	if openedWindowConfirmed("月伴对话 - Lunitide", "lunitide.exe", queries) {
		t.Fatal("list-only sodamusic.exe must not confirm while Lunitide is foreground")
	}
	if !openedWindowConfirmed("汽水音乐", "sodamusic.exe", queries) {
		t.Fatal("C9-8: foreground sodamusic.exe must confirm 汽水 via process alias")
	}
}

func TestConfirmDesktopOpenedAcceptsProcessWhenNotForeground(t *testing.T) {
	origFG, origList, origAct, origSleep, origTries, origProc := readForegroundFn, listWindowsFn, activateWindowFn, openVerifySleep, openVerifyTries, lookupProcessImagesFn
	t.Cleanup(func() {
		readForegroundFn, listWindowsFn, activateWindowFn, openVerifySleep, openVerifyTries, lookupProcessImagesFn = origFG, origList, origAct, origSleep, origTries, origProc
	})
	openVerifyTries = 1
	openVerifySleep = func() {}
	activateWindowFn = func(string) error { return nil }
	lookupProcessImagesFn = func([]string) []string { return nil }
	listWindowsFn = func() []windowHint { return []windowHint{{Title: "Soda Music", Process: "sodamusic.exe"}} }
	readForegroundFn = func() (string, string, error) { return "Lunitide", "lunitide.exe", nil }
	proof, err := confirmDesktopOpened("汽水")
	if err != nil || proof.Kind != "process" {
		t.Fatalf("soda behind Lunitide must count as process open, got %+v %v", proof, err)
	}
	listWindowsFn = func() []windowHint { return nil }
	if _, err := confirmDesktopOpened("汽水"); err == nil || !strings.Contains(err.Error(), "未确认目标进程") {
		t.Fatalf("no window and no process must fail, got %v", err)
	}
	readForegroundFn = func() (string, string, error) { return "汽水音乐", "sodamusic.exe", nil }
	proof, err = confirmDesktopOpened("汽水")
	if err != nil || proof.Kind != "foreground" {
		t.Fatalf("foreground soda must confirm: %+v %v", proof, err)
	}
}
