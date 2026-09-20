package toolruntime

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

type windowHint = winexec.WindowHint

type desktopOpenProof struct {
	Kind string
}

var (
	readForegroundFn      = winexec.ForegroundWindow
	listWindowsFn         = winexec.ListVisibleWindows
	activateWindowFn      = winexec.ActivateWindowMatching
	lookupProcessImagesFn = winexec.LookupProcessImages
	openVerifySleep       = func() { time.Sleep(200 * time.Millisecond) }
	openVerifyTries       = 20
)

func launchVerifyQueries(name string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	add(launchQueryCore(name))
	add(name)
	if known, ok := matchKnownLaunchApp(name); ok {
		add(known.Canonical)
		for _, a := range known.Aliases {
			add(a)
		}
		for _, p := range known.Processes {
			add(p)
		}
	}
	return out
}

func isSelfWindow(title, process string) bool {
	p := strings.ToLower(process)
	t := title
	return strings.Contains(p, "lunitide") || strings.Contains(t, "Lunitide") || strings.Contains(t, "月伴") || strings.Contains(t, "月汐")
}

func windowHitsLaunch(title, process string, queries []string) bool {
	if isSelfWindow(title, process) {
		return false
	}
	blob := strings.ToLower(title + " " + filepath.Base(process))
	for _, q := range queries {
		q = strings.ToLower(strings.TrimSpace(q))
		if q == "" {
			continue
		}
		q = strings.TrimSuffix(q, ".lnk")
		stem := strings.TrimSuffix(q, ".exe")
		if strings.Contains(blob, q) || (stem != "" && stem != q && strings.Contains(blob, stem)) {
			return true
		}
	}
	return false
}

func openedWindowConfirmed(fgTitle, fgProcess string, queries []string) bool {
	return windowHitsLaunch(fgTitle, fgProcess, queries)
}

func openedVisibleOrProcess(name string, queries []string) bool {
	for _, w := range listWindowsFn() {
		if windowHitsLaunch(w.Title, w.Process, queries) {
			return true
		}
	}
	var names []string
	if known, ok := matchKnownLaunchApp(name); ok {
		names = append(names, known.Processes...)
	}
	for _, q := range queries {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(q)), ".exe") {
			names = append(names, q)
		}
	}
	return len(lookupProcessImagesFn(names)) > 0
}

func confirmDesktopOpened(name string) (desktopOpenProof, error) {
	queries := launchVerifyQueries(name)
	if len(queries) == 0 {
		queries = []string{strings.TrimSpace(name)}
	}
	sawProcess := false
	for i := 0; i < openVerifyTries; i++ {
		for _, q := range queries {
			_ = activateWindowFn(q)
		}
		fgTitle, fgProcess, _ := readForegroundFn()
		if openedWindowConfirmed(fgTitle, fgProcess, queries) {
			return desktopOpenProof{Kind: "foreground"}, nil
		}
		if openedVisibleOrProcess(name, queries) {
			sawProcess = true
		}
		if i+1 < openVerifyTries {
			openVerifySleep()
		}
	}
	if sawProcess {
		return desktopOpenProof{Kind: "process"}, nil
	}
	return desktopOpenProof{}, errors.New("无法执行：启动了但未确认目标进程")
}

func confirmBrowserOpened() desktopOpenProof {
	queries := []string{"msedge.exe", "chrome.exe", "firefox.exe", "iexplore.exe", "Microsoft Edge", "Google Chrome", "Edge", "Firefox"}
	tries := openVerifyTries
	if tries > 8 {
		tries = 8
	}
	for i := 0; i < tries; i++ {
		fgTitle, fgProcess, _ := readForegroundFn()
		if openedWindowConfirmed(fgTitle, fgProcess, queries) {
			return desktopOpenProof{Kind: "foreground"}
		}
		if openedVisibleOrProcess("浏览器", queries) {
			return desktopOpenProof{Kind: "process"}
		}
		if i+1 < tries {
			openVerifySleep()
		}
	}
	return desktopOpenProof{}
}
