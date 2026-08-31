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
			if !processMatch(w.Process, wantProc...) && !processMatch(w.Title, queries...) {
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
	waitTarget(t, h, []string{"lunitide-d6-notepad", "记事本", "notepad"}, []string{"notepad"}, 12*time.Second)
	marker := "lunitide-d6-notepad"
	nodes, err := h.ObserveUI(80)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
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
	if err := h.InvokeUI("7"); err != nil {
		nodes, obsErr := h.ObserveUI(80)
		if obsErr != nil {
			t.Fatalf("invoke 7: %v; observe: %v", err, obsErr)
		}
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

func TestLiveExplorerWindowListed(t *testing.T) {
	h := requireAccuracy(t)
	wins, err := h.ListWindows()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wins {
		if strings.EqualFold(w.Process, "explorer.exe") {
			return
		}
	}
	cmd := exec.Command("explorer.exe")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	waitTarget(t, h, []string{"explorer", "文件资源管理器"}, []string{"explorer"}, 8*time.Second)
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
