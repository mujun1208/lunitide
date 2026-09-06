//go:build windows

package commandworker

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"testing"
)

func TestJobAssignmentFailureReapsSuspendedProcessWithoutRunningIt(t *testing.T) {
	previous := assignProcessToJob
	defer func() { assignProcessToJob = previous }()
	failure := errors.New("injected Job Object assignment failure")
	var child uint32
	assignProcessToJob = func(_ windows.Handle, process windows.Handle) (bool, error) {
		var err error
		child, err = windows.GetProcessId(process)
		if err != nil {
			t.Fatal(err)
		}
		return false, failure
	}
	output := ""
	_, err := Run(context.Background(), helperSpec(t, t.TempDir(), "echo", "must never run"), nil, func(raw []byte) { output += string(raw) })
	if !errors.Is(err, failure) || output != "" || child == 0 {
		t.Fatalf("assignment failure ran child: pid=%d output=%q err=%v", child, output, err)
	}
	if processAlive(int(child)) {
		t.Fatalf("suspended process %d survived failed assignment", child)
	}
}
