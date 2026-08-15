//go:build !windows

package stdiopoc

import (
	"context"
	"fmt"
)

// spawn on non-Windows builds always fails: the POC spawn engine relies on
// Windows Job Object quotas and an explicit environment block at the
// CreateProcess level. The frame protocol and guard layers stay testable
// cross-platform; the process-level assumptions (secret / proctree /
// resource) need the real engine.
func spawn(_ context.Context, spec SpawnSpec) (*Proc, error) {
	return nil, fmt.Errorf("%w: got spec exe=%q", ErrUnsupportedPlatform, spec.Exe)
}
