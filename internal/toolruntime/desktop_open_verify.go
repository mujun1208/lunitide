package toolruntime

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

type windowHint = winexec.WindowHint

var (
	readForegroundFn = winexec.ForegroundWindow
	listWindowsFn    = winexec.ListVisibleWindows
	activateWindowFn = winexec.ActivateWindowMatching
	openVerifySleep  = func() { time.Sleep(200 * time.Millisecond) }
	openVerifyTries  = 20
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

func confirmDesktopOpened(name string) error {
	queries := launchVerifyQueries(name)
	if len(queries) == 0 {
		queries = []string{strings.TrimSpace(name)}
	}
	for i := 0; i < openVerifyTries; i++ {
		for _, q := range queries {
			_ = activateWindowFn(q)
		}
		fgTitle, fgProcess, _ := readForegroundFn()
		_ = listWindowsFn()
		if openedWindowConfirmed(fgTitle, fgProcess, queries) {
			return nil
		}
		if i+1 < openVerifyTries {
			openVerifySleep()
		}
	}
	return errors.New("无法执行：启动了但窗口没到前台")
}
