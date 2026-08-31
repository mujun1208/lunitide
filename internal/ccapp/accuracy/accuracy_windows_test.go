//go:build windows

package accuracy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func requireAccuracy(t *testing.T) ccapp.Host {
	t.Helper()
	if os.Getenv("LUNITIDE_CC_ACCURACY") != "1" {
		t.Skip("set LUNITIDE_CC_ACCURACY=1 to run live Notepad/Calc/Explorer fixtures")
	}
	h := ccapp.PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	return h
}

func processMatch(got string, needles ...string) bool {
	got = strings.ToLower(strings.TrimSpace(got))
	for _, n := range needles {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" && strings.Contains(got, n) {
			return true
		}
	}
	return false
}

func appActivate(title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"(New-Object -ComObject WScript.Shell).AppActivate('"+strings.ReplaceAll(title, "'", "''")+"')")
	cmd.WaitDelay = 2 * time.Second
	_ = cmd.Run()
}

// windowMatches requires every provided constraint. Process-only used to
// steal the first explorer.exe (often release/out) on a dirty desktop.
func windowMatches(w ccapp.WindowInfo, queries, wantProc []string) bool {
	if len(wantProc) == 0 && len(queries) == 0 {
		return false
	}
	if len(wantProc) > 0 && !processMatch(w.Process, wantProc...) {
		return false
	}
	if len(queries) > 0 && !processMatch(w.Title, queries...) {
		return false
	}
	return true
}

func waitTarget(t *testing.T, h ccapp.Host, queries, wantProc []string, timeout time.Duration) ccapp.WindowInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		wins, err := h.ListWindows()
		if err != nil {
			last = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, w := range wins {
			if !windowMatches(w, queries, wantProc) {
				continue
			}
			q := w.ID
			if q == "" {
				q = w.Title
			}
			info, ferr := h.FocusWindow(q)
			if ferr != nil {
				last = ferr
				continue
			}
			appActivate(info.Title)
			_ = h.EnsureForeground()
			return info
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("window %v (process %v) not ready: %v", queries, wantProc, last)
	return ccapp.WindowInfo{}
}

func uiBlob(nodes []ccapp.UINode) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(n.Name)
		b.WriteByte(' ')
		b.WriteString(n.Value)
		b.WriteByte(' ')
	}
	return b.String()
}

func waitUIReady(t *testing.T, h ccapp.Host, minNodes int, timeout time.Duration) []ccapp.UINode {
	t.Helper()
	if minNodes < 1 {
		minNodes = 1
	}
	deadline := time.Now().Add(timeout)
	var last []ccapp.UINode
	var lastErr error
	for time.Now().Before(deadline) {
		nodes, err := h.ObserveUI(80)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		last = nodes
		if len(nodes) >= minNodes {
			return nodes
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("ui tree still empty after wait: err=%v nodes=%d", lastErr, len(last))
	return last
}

func waitUIContaining(t *testing.T, h ccapp.Host, needles []string, timeout time.Duration) []ccapp.UINode {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last []ccapp.UINode
	var lastErr error
	for time.Now().Before(deadline) {
		nodes, err := h.ObserveUI(80)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		last = nodes
		blob := uiBlob(nodes)
		for _, needle := range needles {
			if needle != "" && strings.Contains(blob, needle) {
				return nodes
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("ui tree missing %v after wait: err=%v blob=%q", needles, lastErr, uiBlob(last))
	return last
}

func TestWindowMatchesRequiresProcessAndTitle(t *testing.T) {
	release := ccapp.WindowInfo{Title: "out", Process: "explorer.exe"}
	marker := ccapp.WindowInfo{Title: "lunitide-d6-explorer", Process: "explorer.exe"}
	calc := ccapp.WindowInfo{Title: "计算器", Process: "CalculatorApp.exe"}
	if windowMatches(release, []string{"lunitide-d6-explorer"}, []string{"explorer"}) {
		t.Fatal("untitled explorer must not match a named fixture")
	}
	if !windowMatches(marker, []string{"lunitide-d6-explorer"}, []string{"explorer"}) {
		t.Fatal("titled explorer fixture must match")
	}
	if windowMatches(calc, []string{"lunitide-d6-explorer"}, []string{"explorer"}) {
		t.Fatal("calculator must not match explorer fixture")
	}
	if !windowMatches(calc, []string{"计算器", "Calculator"}, []string{"calculatorapp", "calculator"}) {
		t.Fatal("calculator title+process must match")
	}
}

func TestLiveNotepadTypesVisibleText(t *testing.T) {
	h := requireAccuracy(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "lunitide-d6-notepad.txt")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("notepad.exe", path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	waitTarget(t, h, []string{"lunitide-d6-notepad"}, []string{"notepad"}, 12*time.Second)
	marker := "lunitide-d6-notepad"
	nodes := waitUIReady(t, h, 1, 8*time.Second)
	typed := false
	for _, n := range nodes {
		role := strings.ToLower(n.Role)
		if role != "text" && role != "edit" && role != "document" {
			continue
		}
		target := strings.TrimSpace(n.Name)
		if target == "" {
			target = n.ID
		}
		if target == "" {
			continue
		}
		if err := h.SetValue(target, marker); err != nil {
			continue
		}
		typed = true
		break
	}
	if !typed {
		if err := h.KeyboardType(marker); err != nil {
			t.Fatalf("type: %v (nodes=%d)", err, len(nodes))
		}
	}
	time.Sleep(200 * time.Millisecond)
	after, err := h.ObserveUI(80)
	if err != nil {
		t.Fatalf("reobserve: %v", err)
	}
	blob := ""
	for _, n := range after {
		blob += n.Name + " " + n.Value + " "
	}
	if !strings.Contains(blob, marker) {
		t.Fatalf("notepad tree missing %q: %q", marker, blob)
	}
}

func TestLiveCalculatorNamedClick(t *testing.T) {
	h := requireAccuracy(t)
	cmd := exec.Command("calc.exe")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	waitTarget(t, h, []string{"计算器", "Calculator"}, []string{"calculatorapp", "calculator"}, 15*time.Second)
	nodes := waitUIContaining(t, h, []string{"7", "七"}, 10*time.Second)
	if err := h.InvokeUI("7"); err != nil {
		found := false
		for _, n := range nodes {
			if name := strings.TrimSpace(n.Name); name == "7" || name == "七" {
				found = true
				if inv := h.InvokeUI(n.Name); inv != nil {
					t.Fatalf("invoke node 7: %v", inv)
				}
				break
			}
		}
		if !found {
			names := make([]string, 0, len(nodes))
			for _, n := range nodes {
				if strings.TrimSpace(n.Name) != "" {
					names = append(names, n.Name)
				}
			}
			t.Fatalf("calculator has no named 7: invoke=%v nodes=%v", err, names)
		}
	}
	nodes, err := h.ObserveUI(80)
	if err != nil {
		t.Fatal(err)
	}
	blob := ""
	for _, n := range nodes {
		blob += n.Name + " "
	}
	if !strings.Contains(blob, "7") && !strings.Contains(blob, "七") {
		t.Fatalf("calculator tree missing 7 after click: %q", blob)
	}
}

func TestLiveExplorerNamedObserve(t *testing.T) {
	h := requireAccuracy(t)
	dir := t.TempDir()
	marker := "lunitide-d6-explorer"
	openDir := filepath.Join(dir, marker)
	if err := os.Mkdir(openDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(openDir, marker+".txt"), []byte("d6"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("explorer.exe", openDir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	info := waitTarget(t, h, []string{marker}, []string{"explorer"}, 12*time.Second)
	nodes := waitUIContaining(t, h, []string{marker}, 8*time.Second)
	blob := info.Title + " " + info.Process + " " + uiBlob(nodes)
	if !strings.Contains(strings.ToLower(blob), strings.ToLower(marker)) {
		t.Fatalf("explorer tree missing %q: %q", marker, blob)
	}
}

func TestLiveOptionalWPFAndElectron(t *testing.T) {
	h := requireAccuracy(t)
	wins, err := h.ListWindows()
	if err != nil {
		t.Fatal(err)
	}
	foundWPF, foundElectron := false, false
	for _, w := range wins {
		proc := strings.ToLower(w.Process)
		if strings.Contains(proc, "wpf") || strings.Contains(w.Title, "WPF") {
			foundWPF = true
		}
		if strings.Contains(proc, "electron") || strings.Contains(proc, "code") || strings.Contains(proc, "cursor") {
			foundElectron = true
		}
	}
	if !foundWPF {
		t.Log("no WPF window on this desktop; skip")
	}
	if !foundElectron {
		t.Log("no Electron window on this desktop; skip")
	}
}
