package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The refusal floor is only worth having if it holds when every other gate is
// open, so these cases go through the same full-access path a companion voice
// turn takes.
func TestHardlineRefusesUnrecoverableCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"posix root wipe", []string{"rm", "-rf", "/"}},
		{"posix root glob", []string{"rm", "-rf", "/*"}},
		{"drive root wipe", []string{"rm", "-rf", `C:\`}},
		{"windows dir wipe", []string{"rd", "/s", "/q", `C:\Windows`}},
		{"system32 wipe", []string{"del", "/f", "/s", "/q", `C:\Windows\System32`}},
		{"home wipe", []string{"rm", "-rf", "~"}},
		{"users wipe", []string{"remove-item", "-recurse", "-force", `C:\Users`}},
		{"drive format", []string{"format", "c:", "/fs:ntfs"}},
		{"mkfs", []string{"mkfs.ext4", "/dev/sda1"}},
		{"diskpart", []string{"diskpart", "/s", "wipe.txt"}},
		{"free space wipe", []string{"cipher", "/w", `C:\`}},
		{"raw block write", []string{"dd", "if=/dev/zero", "of=/dev/sda"}},
		{"physical drive write", []string{"dd", "if=/dev/zero", `of=\\.\PhysicalDrive0`}},
		{"shadow copy delete", []string{"vssadmin", "delete", "shadows", "/all"}},
		{"backup delete", []string{"wbadmin", "delete", "catalog", "-quiet"}},
		{"boot config delete", []string{"bcdedit", "/delete", "{current}"}},
		{"shutdown", []string{"shutdown", "/s", "/t", "0"}},
		{"restart cmdlet", []string{"Restart-Computer", "-Force"}},
		{"registry hive delete", []string{"reg", "delete", `HKLM\SYSTEM`, "/f"}},
		{"system acl takeover", []string{"icacls", `C:\`, "/grant", "everyone:F", "/t"}},
		{"firewall off", []string{"netsh", "advfirewall", "set", "allprofiles", "state", "off"}},
		{"fork bomb", []string{"bash", "-c", ":(){ :|:& };:"}},
		// The interpreter forms matter most: this is what a model actually
		// emits, and an opaque one-argument script would otherwise sail past.
		{"powershell wrapped", []string{"powershell", "-Command", `Remove-Item -Recurse -Force C:\Windows`}},
		{"chained behind a cd", []string{"bash", "-c", "cd /tmp && rm -rf /"}},
		{"hidden after cmd /c", []string{"cmd", "/c", "shutdown", "/r", "/f"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if reason := hardlineRefusal(tc.argv); reason == "" {
				t.Fatalf("allowed %v", tc.argv)
			}
		})
	}
}

// The other half of the contract, and the half that breaks people's work if it
// regresses: ordinary developer commands must stay untouched.
func TestHardlineAllowsOrdinaryWork(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"dependency cleanup", []string{"rm", "-rf", "node_modules"}},
		{"build output cleanup", []string{"rm", "-rf", "./dist"}},
		{"nested path delete", []string{"rm", "-rf", `C:\Users\mu\project\build`}},
		{"windows temp file", []string{"del", "/q", `C:\Windows\Temp\build.log`}},
		{"git clean", []string{"git", "clean", "-fdx"}},
		{"npm format script", []string{"npm", "run", "format"}},
		{"go build", []string{"go", "build", "./..."}},
		{"disk image write", []string{"dd", "if=/dev/zero", "of=test.img", "bs=1M", "count=10"}},
		{"application registry key", []string{"reg", "delete", `HKLM\SOFTWARE\Lunitide`, "/f"}},
		{"abort a shutdown", []string{"shutdown", "/a"}},
		{"scoped acl change", []string{"icacls", `C:\Users\mu\project`, "/grant", "mu:F", "/t"}},
		{"powershell project cleanup", []string{"powershell", "-Command", "Remove-Item -Recurse -Force .\\dist"}},
		{"chained project commands", []string{"bash", "-c", "cd web && rm -rf node_modules && npm ci"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if reason := hardlineRefusal(tc.argv); reason != "" {
				t.Fatalf("refused %v: %s", tc.argv, reason)
			}
		})
	}
}

// Full access plus an approved call is the most permissive combination the
// runtime offers; the floor still has to hold there, and say why.
func TestHardlineHoldsUnderFullAccess(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"argv":["rm","-rf","/"]}`)
	_, err = r.Execute(context.Background(), FullAccess, session, "command.run", args, true)
	if err == nil {
		t.Fatal("full access ran a root wipe")
	}
	if !strings.Contains(err.Error(), "cannot be undone") {
		t.Fatalf("refusal did not explain itself: %v", err)
	}
}
