package toolruntime

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

func getPolicyStatus(t *testing.T, r *Runtime, kind string) PolicyStatus {
	t.Helper()
	raw, err := r.PolicySnapshot(kind)
	if err != nil {
		t.Fatal(err)
	}
	var status PolicyStatus
	if err = json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	return status
}
func TestPolicyRevisionRejectsStaleEditorsAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	initial := getPolicyStatus(t, r, "commands")
	if initial.State != "applied" || initial.AppliedRevision != initial.Revision {
		t.Fatalf("initial: %+v", initial)
	}
	accepted, err := r.SetPolicyVersioned("commands", []byte(`{"commands":[],"fullAccess":true}`), initial.Revision)
	if err != nil || accepted.Revision == initial.Revision || !r.FullDiskEnabled() {
		t.Fatalf("activation: %+v %v", accepted, err)
	}
	if _, err = r.SetPolicyVersioned("commands", []byte(`{"commands":[],"fullAccess":false}`), initial.Revision); !errors.Is(err, ErrPolicyRevisionConflict) {
		t.Fatalf("stale: %v", err)
	}
	if !r.FullDiskEnabled() {
		t.Fatal("stale editor changed live state")
	}
	other, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	restored := getPolicyStatus(t, other, "commands")
	if restored != accepted || !other.FullDiskEnabled() {
		t.Fatalf("restart: %+v", restored)
	}
}
func TestPolicyRevisionSerializesConcurrentEditorsAndDetectsDiskDrift(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	initial := getPolicyStatus(t, r, "commands")
	var passed, conflicts atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.SetPolicyVersioned("commands", []byte(`{"commands":[],"fullAccess":true}`), initial.Revision)
			if err == nil {
				passed.Add(1)
			} else if errors.Is(err, ErrPolicyRevisionConflict) {
				conflicts.Add(1)
			} else {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 1 || conflicts.Load() != 9 {
		t.Fatalf("writers: %d %d", passed.Load(), conflicts.Load())
	}
	live := getPolicyStatus(t, r, "commands")
	if err = os.WriteFile(r.userRulesPath, []byte(`{"commands":[],"fullAccess":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	changed := getPolicyStatus(t, r, "commands")
	if changed.State != "pending" || changed.AppliedRevision != live.Revision || changed.Revision == live.Revision || !r.FullDiskEnabled() {
		t.Fatalf("disk versus live: %+v", changed)
	}
	applied, err := r.SetPolicyVersioned("commands", []byte(`{"commands":[],"fullAccess":false}`), changed.Revision)
	if err != nil || applied.State != "applied" || r.FullDiskEnabled() {
		t.Fatalf("apply drift: %+v %v", applied, err)
	}
}
func TestPolicyRevisionAvoidsABAAndBoundsDocument(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	initial := getPolicyStatus(t, r, "hooks")
	first, err := r.SetPolicyVersioned("hooks", []byte(`{"hooks":[]}`), initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.SetPolicyVersioned("hooks", []byte(`{"hooks":[]}`), first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision == first.Revision || second.Revision == initial.Revision {
		t.Fatal("ABA revision reused")
	}
	if err = os.WriteFile(r.hooksRulesPath, make([]byte, (64<<10)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = r.PolicySnapshot("hooks"); err == nil {
		t.Fatal("unbounded policy document accepted")
	}
}
