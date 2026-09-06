//go:build windows

package commandworker

import (
	"context"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRunOutputHandleDoesNotCloseRecycledEventsDuringGC(t *testing.T) {
	for iteration := 0; iteration < 8; iteration++ {
		spec := helperSpec(t, t.TempDir(), "echo", "handle ownership")
		if _, err := Run(context.Background(), spec, nil, nil); err != nil {
			t.Fatal(err)
		}
		// Allocate kernel handles before collecting the finished worker's File.
		// Its finalizer must never close a handle now owned by another component.
		var events []windows.Handle
		for i := 0; i < 32; i++ {
			h, err := windows.CreateEvent(nil, 0, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, h)
		}
		runtime.GC()
		for _, h := range events {
			state, err := windows.WaitForSingleObject(h, 0)
			_ = windows.CloseHandle(h)
			if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
				t.Fatalf("worker finalizer closed a recycled handle: %d %v", state, err)
			}
		}
	}
}
