package toolruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsAppsPythonAliasIsNotUsable(t *testing.T) {
	stub := `C:\Users\someone\AppData\Local\Microsoft\WindowsApps\python.exe`
	if !windowsAppsAlias(stub) || usableExe(stub) {
		t.Fatalf("store alias must be rejected: alias=%v usable=%v", windowsAppsAlias(stub), usableExe(stub))
	}
}

func TestRelocateScriptFindsWorkspaceCopy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "poc", "ui_sales_assistant")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "server.py")
	if err := os.WriteFile(script, []byte("print('ok')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := relocateMissingScripts(root, []string{"python", `E:\missing\poc\ui_sales_assistant\server.py`})
	if err != nil {
		t.Fatal(err)
	}
	if got[1] != script {
		t.Fatalf("relocated to %s, want %s", got[1], script)
	}
	if !isLocalServerCommand(got) {
		t.Fatal("server.py must stay running instead of being handed back to the user")
	}
}

func TestMissingScriptDoesNotAskTheUserToRunIt(t *testing.T) {
	_, err := relocateMissingScripts(t.TempDir(), []string{"python", `E:\nowhere\server.py`})
	if err == nil || !strings.Contains(err.Error(), "workspace.write") || !strings.Contains(err.Error(), "对话外") {
		t.Fatalf("missing script error = %v", err)
	}
	if _, err := findRealPython(); err != nil && strings.Contains(err.Error(), "粘贴") == false {
		t.Fatalf("python miss must forbid pasting a command: %v", err)
	}
}

func TestLiveServerStaysUpAndReportsItsURL(t *testing.T) {
	exe, err := findRealPython()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "server.py")
	body := "import time\nprint('http://127.0.0.1:8765', flush=True)\ntime.sleep(30)\n"
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	r := &Runtime{}
	msg, err := r.startLiveServer(t.Context(), "sess", dir, []string{exe, script})
	t.Cleanup(func() {
		r.liveMu.Lock()
		cmd := r.liveServers["sess"]
		r.liveMu.Unlock()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "http://127.0.0.1:8765") || !strings.Contains(msg, "不要把命令交给用户粘贴") {
		t.Fatalf("server result = %s", msg)
	}
}

func TestOneShotPythonIsNotAServer(t *testing.T) {
	if isLocalServerCommand([]string{"python", "ai_sales_poc.py", "test"}) {
		t.Fatal("a one-shot script must not be detached")
	}
	if !isLocalServerCommand([]string{"python", "-m", "http.server", "8765"}) {
		t.Fatal("http.server is a local demo server")
	}
}
