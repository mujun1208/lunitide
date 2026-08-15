//go:build windows

package stdiopoc

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// probeResourceOS commits memory in 4MiB chunks via VirtualAlloc until the
// request is satisfied or the allocation fails. Inside a Job Object with a
// commit cap, VirtualAlloc fails once the tree total crosses the cap: that
// failure is the OS-enforced resource assumption.
func probeResourceOS(cfg ProbeConfig) (probeReport, error) {
	const chunk = 4 << 20
	report := probeReport{}
	a := Attack{Vector: "memory-exhaustion", Attempt: fmt.Sprintf("commit %d bytes", cfg.MemRequestBytes)}
	var committed uint64
	var firstErr string
	for committed+chunk <= cfg.MemRequestBytes {
		if _, err := windows.VirtualAlloc(0, chunk, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE); err != nil {
			firstErr = err.Error()
			break
		}
		// MEM_COMMIT charges the Job commit limit immediately (commit
		// charge, not working set): no page touching needed, and the
		// allocation is deliberately leaked to keep the charge held.
		committed += chunk
	}
	if firstErr != "" {
		a.Blocked = true
		a.Detail = trimForDetail(fmt.Sprintf("committed %d MiB then VirtualAlloc failed: %s (job commit quota)", committed>>20, firstErr))
	} else {
		a.Blocked = false
		a.Detail = fmt.Sprintf("committed all %d MiB without hitting the quota", committed>>20)
	}
	report.Attacks = append(report.Attacks, a)
	return report, nil
}
