//go:build !windows

package stdiopoc

import "fmt"

// probeResourceOS is Windows-only: the memory-exhaustion probe relies on
// VirtualAlloc failing inside a Job Object commit cap. The harness refuses
// to run the POC off Windows anyway (see spawn_other.go).
func probeResourceOS(cfg ProbeConfig) (probeReport, error) {
	return probeReport{}, fmt.Errorf("%w: resource probe needs the windows job engine", ErrUnsupportedPlatform)
}
