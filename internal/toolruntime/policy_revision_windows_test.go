//go:build windows

package toolruntime

import (
	"golang.org/x/sys/windows"
	"testing"
)

func TestPolicyAtomicReplaceFailurePreservesFileAndAppliedRules(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	before := getPolicyStatus(t, r, "commands")
	path, err := windows.UTF16PtrFromString(r.userRulesPath)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if _, err = r.SetPolicyVersioned("commands", []byte(`{"commands":[],"fullAccess":false}`), before.Revision); err == nil {
		t.Fatal("expected locked destination replacement failure")
	}
	if after := getPolicyStatus(t, r, "commands"); after != before || !r.FullDiskEnabled() {
		t.Fatalf("lost original file or live state: %+v", after)
	}
}
