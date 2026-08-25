//go:build windows

package winexec

import "testing"

// syscall.NewCallback draws from a fixed table that is never reclaimed, so a
// callback minted per enumeration exhausts it and panics — after a couple of
// thousand window activations, in a process that stays open all day. Nothing
// about the old code looked wrong at a glance, and no failure appears until
// the table is gone, so the single-pointer invariant is worth pinning.
func TestEnumWindowsCallbackIsMintedOnce(t *testing.T) {
	first := enumWindowsCallbackPtr()
	for i := range 4096 {
		if got := enumWindowsCallbackPtr(); got != first {
			t.Fatalf("callback #%d = %#x, want the one at %#x", i, got, first)
		}
	}
}

// The callback finds its match through a token, because a Go pointer passed
// as EnumWindows' uintptr is invisible to the collector. Tokens have to be
// handed back: otherwise the registry grows for the life of the process and
// pins every match it ever held.
func TestEnumerateWindowsReleasesItsToken(t *testing.T) {
	enumerateWindows(&windowMatch{fragment: "lunitide-window-that-does-not-exist"})

	windowMatchMu.Lock()
	n := len(windowMatches)
	windowMatchMu.Unlock()
	if n != 0 {
		t.Fatalf("registry holds %d entries after the pass, want none", n)
	}
}

// A token the registry no longer knows has nowhere to record a hit, so the
// enumeration stops instead of dereferencing anything.
func TestUnknownTokenStopsEnumeration(t *testing.T) {
	if m := lookupWindowMatch(0); m != nil {
		t.Fatalf("lookup of an unregistered token = %+v, want nil", m)
	}
	if got := enumWindowsCallback(1, 0); got != 0 {
		t.Fatalf("callback with an unregistered token = %d, want 0 to stop", got)
	}
}
