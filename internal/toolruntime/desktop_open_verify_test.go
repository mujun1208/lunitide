package toolruntime

import "testing"

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

func TestConfirmDesktopOpenedRequiresForeground(t *testing.T) {
	origFG, origList, origAct, origSleep, origTries := readForegroundFn, listWindowsFn, activateWindowFn, openVerifySleep, openVerifyTries
	t.Cleanup(func() {
		readForegroundFn, listWindowsFn, activateWindowFn, openVerifySleep, openVerifyTries = origFG, origList, origAct, origSleep, origTries
	})
	openVerifyTries = 1
	openVerifySleep = func() {}
	activateWindowFn = func(string) error { return nil }
	listWindowsFn = func() []windowHint { return []windowHint{{Title: "Soda Music", Process: "sodamusic.exe"}} }
	readForegroundFn = func() (string, string, error) { return "Lunitide", "lunitide.exe", nil }
	if err := confirmDesktopOpened("汽水"); err == nil || err.Error() != "无法执行：启动了但窗口没到前台" {
		t.Fatalf("soda behind Lunitide must keep polling / fail, got %v", err)
	}
	readForegroundFn = func() (string, string, error) { return "汽水音乐", "sodamusic.exe", nil }
	if err := confirmDesktopOpened("汽水"); err != nil {
		t.Fatal(err)
	}
}
